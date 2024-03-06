/*
Copyright 2015 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package instancegroups

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"google.golang.org/api/compute/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/ingress-gce/pkg/events"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/ingress-gce/pkg/utils/zonegetter"
	"k8s.io/klog/v2"

	core "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	"k8s.io/ingress-gce/pkg/utils"
)

const (
	// State string required by gce library to list all instances.
	allInstances = "ALL"
)

// manager implements Manager.
type manager struct {
	cloud              Provider
	ZoneGetter         *zonegetter.ZoneGetter
	namer              namer.BackendNamer
	recorder           record.EventRecorder
	instanceLinkFormat string
	maxIGSize          int

	logger klog.Logger
}

type recorderSource interface {
	Recorder(ns string) record.EventRecorder
}

// ManagerConfig is used for Manager constructor.
type ManagerConfig struct {
	// Cloud implements Provider, used to sync Kubernetes nodes with members of the cloud InstanceGroup.
	Cloud      Provider
	Namer      namer.BackendNamer
	Recorders  recorderSource
	BasePath   string
	ZoneGetter *zonegetter.ZoneGetter
	MaxIGSize  int
}

// NewManager creates a new node pool using ManagerConfig.
func NewManager(config *ManagerConfig, logger klog.Logger) Manager {
	return &manager{
		cloud:              config.Cloud,
		namer:              config.Namer,
		recorder:           config.Recorders.Recorder(""), // No namespace
		instanceLinkFormat: config.BasePath + "zones/%s/instances/%s",
		ZoneGetter:         config.ZoneGetter,
		maxIGSize:          config.MaxIGSize,
		logger:             logger.WithName("InstanceGroupsManager"),
	}
}

// EnsureInstanceGroupsAndPorts creates or gets an instance group if it doesn't exist
// and adds the given ports to it. Returns a list of one instance group per zone,
// all of which have the exact same named ports.
func (m *manager) EnsureInstanceGroupsAndPorts(name string, ports []int64) (igs []*compute.InstanceGroup, err error) {
	// Instance groups need to be created only in zones that have ready nodes.
	zones, err := m.ZoneGetter.List(zonegetter.CandidateNodesFilter, m.logger)
	if err != nil {
		return nil, err
	}

	for _, zone := range zones {
		ig, err := m.ensureInstanceGroupAndPorts(name, zone, ports)
		if err != nil {
			return nil, err
		}

		igs = append(igs, ig)
	}
	return igs, nil
}

func (m *manager) ensureInstanceGroupAndPorts(name, zone string, ports []int64) (*compute.InstanceGroup, error) {
	m.logger.V(3).Info("Ensuring instance group", "name", name, "zone", zone, "ports", ports)

	ig, err := m.Get(name, zone)
	if err != nil && !utils.IsHTTPErrorCode(err, http.StatusNotFound) {
		m.logger.Error(err, "Failed to get instance group", "key", klog.KRef(zone, name))
		return nil, err
	}

	if ig == nil {
		m.logger.V(3).Info("Creating instance group", "key", klog.KRef(zone, name))
		if err = m.cloud.CreateInstanceGroup(&compute.InstanceGroup{Name: name}, zone); err != nil {
			// Error may come back with StatusConflict meaning the instance group was created by another controller
			// possibly the Service Manager for internal load balancers.
			if utils.IsHTTPErrorCode(err, http.StatusConflict) {
				m.logger.Info("Failed to create instance group due to conflict status, but continuing sync", "key", klog.KRef(zone, name), "err", err)
			} else {
				m.logger.Error(err, "Failed to create instance group", "key", klog.KRef(zone, name))
				return nil, err
			}
		}
		ig, err = m.Get(name, zone)
		if err != nil {
			m.logger.Error(err, "Failed to get instance group after ensuring existence", "key", klog.KRef(zone, name))
			return nil, err
		}
	} else {
		m.logger.V(2).Info("Instance group already exists", "key", klog.KRef(zone, name))
	}

	// Build map of existing ports
	existingPorts := map[int64]bool{}
	for _, np := range ig.NamedPorts {
		existingPorts[np.Port] = true
	}

	// Determine which ports need to be added
	var newPorts []int64
	for _, p := range ports {
		if existingPorts[p] {
			m.logger.V(5).Info("Instance group already has named port", "key", klog.KRef(zone, ig.Name), "port", p)
			continue
		}
		newPorts = append(newPorts, p)
	}

	// Build slice of NamedPorts for adding
	var newNamedPorts []*compute.NamedPort
	for _, port := range newPorts {
		newNamedPorts = append(newNamedPorts, &compute.NamedPort{Name: m.namer.NamedPort(port), Port: port})
	}

	if len(newNamedPorts) > 0 {
		m.logger.V(3).Info("Instance group does not have ports, adding them now", "key", klog.KRef(zone, name), "ports", fmt.Sprintf("%+v", newPorts))
		if err := m.cloud.SetNamedPortsOfInstanceGroup(ig.Name, zone, append(ig.NamedPorts, newNamedPorts...)); err != nil {
			return nil, err
		}
	}

	return ig, nil
}

// DeleteInstanceGroup deletes the given IG by name, from all zones.
func (m *manager) DeleteInstanceGroup(name string) error {
	var errs []error

	zones, err := m.ZoneGetter.List(zonegetter.AllNodesFilter, m.logger)
	if err != nil {
		return err
	}
	for _, zone := range zones {
		if err := m.cloud.DeleteInstanceGroup(name, zone); err != nil {
			if utils.IsNotFoundError(err) {
				m.logger.V(3).Info("Instance group in zone did not exist", "name", name, "zone", zone)
			} else if utils.IsInUsedByError(err) {
				m.logger.V(3).Info("Could not delete instance group in zone because it's still in use. Ignoring", "name", name, "zone", zone, "err", err)
			} else {
				errs = append(errs, err)
			}
		} else {
			m.logger.V(3).Info("Deleted instance group in zone", "name", name, "zone", zone)
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("%v", errs)
}

// listIGInstances lists all instances of provided instance group name in all zones.
// The return format will be a set of nodes in the instance group and
// a map from node name to zone.
func (m *manager) listIGInstances(name string) (sets.String, map[string]string, error) {
	nodeNames := sets.NewString()
	nodeZoneMap := make(map[string]string)
	zones, err := m.ZoneGetter.List(zonegetter.AllNodesFilter, m.logger)
	if err != nil {
		return nodeNames, nodeZoneMap, err
	}

	for _, zone := range zones {
		instances, err := m.cloud.ListInstancesInInstanceGroup(name, zone, allInstances)
		if err != nil {
			return nodeNames, nodeZoneMap, err
		}
		for _, ins := range instances {
			name, err := utils.KeyName(ins.Instance)
			if err != nil {
				return nodeNames, nodeZoneMap, err
			}
			nodeNames.Insert(name)
			nodeZoneMap[name] = zone
		}
	}
	return nodeNames, nodeZoneMap, nil
}

// Get returns the Instance Group by name.
func (m *manager) Get(name, zone string) (*compute.InstanceGroup, error) {
	ig, err := m.cloud.GetInstanceGroup(name, zone)
	if err != nil {
		return nil, err
	}

	return ig, nil
}

// List lists the names of all Instance Groups belonging to this cluster.
// It will return only the names of the groups without the zone. If a group
// with the same name exists in more than one zone it will be returned only once.
func (m *manager) List() ([]string, error) {
	var igs []*compute.InstanceGroup

	zones, err := m.ZoneGetter.List(zonegetter.AllNodesFilter, m.logger)
	if err != nil {
		return nil, err
	}

	for _, zone := range zones {
		igsForZone, err := m.cloud.ListInstanceGroups(zone)
		if err != nil {
			return nil, err
		}

		for _, ig := range igsForZone {
			igs = append(igs, ig)
		}
	}

	names := sets.New[string]()

	for _, ig := range igs {
		if m.namer.NameBelongsToCluster(ig.Name) {
			names.Insert(ig.Name)
		}
	}
	return names.UnsortedList(), nil
}

// splitNodesByZones takes a list of node names and returns a map of zone:node names.
// It figures out the zones by asking the zoneLister.
func (m *manager) splitNodesByZone(names []string) map[string][]string {
	nodesByZone := map[string][]string{}
	for _, name := range names {
		zone, err := m.ZoneGetter.ZoneForNode(name, m.logger)
		if err != nil {
			m.logger.Error(err, "Failed to get zones for instance node, skipping", "name", name)
			continue
		}
		if _, ok := nodesByZone[zone]; !ok {
			nodesByZone[zone] = []string{}
		}
		nodesByZone[zone] = append(nodesByZone[zone], name)
	}
	return nodesByZone
}

// getInstanceReferences creates and returns the instance references by generating the
// expected instance URLs
func (m *manager) getInstanceReferences(zone string, nodeNames []string) (refs []*compute.InstanceReference) {
	for _, nodeName := range nodeNames {
		refs = append(refs, &compute.InstanceReference{Instance: fmt.Sprintf(m.instanceLinkFormat, zone, canonicalizeInstanceName(nodeName))})
	}
	return refs
}

// Add adds the given instances to the appropriately zoned Instance Group.
func (m *manager) add(groupName string, names []string) error {
	events.GlobalEventf(m.recorder, core.EventTypeNormal, events.AddNodes, "Adding %s to InstanceGroup %q", events.TruncatedStringList(names), groupName)
	var errs []error
	for zone, nodeNames := range m.splitNodesByZone(names) {
		m.logger.V(1).Info("Adding nodes to instance group in zone", "nodeCount", len(nodeNames), "name", groupName, "zone", zone)
		if err := m.cloud.AddInstancesToInstanceGroup(groupName, zone, m.getInstanceReferences(zone, nodeNames)); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) == 0 {
		return nil
	}

	err := fmt.Errorf("AddInstances: %v", errs)
	events.GlobalEventf(m.recorder, core.EventTypeWarning, events.AddNodes, "Error adding %s to InstanceGroup %q: %v", events.TruncatedStringList(names), groupName, err)
	return err
}

// Remove removes the given instances from the appropriately zoned Instance Group.
func (m *manager) remove(groupName string, names []string, nodeZoneMap map[string]string) error {
	events.GlobalEventf(m.recorder, core.EventTypeNormal, events.RemoveNodes, "Removing %s from InstanceGroup %q", events.TruncatedStringList(names), groupName)
	var errs []error

	// Get the zone information from nameZoneMap instead of ZoneGetter.
	// Since the ZoneGetter is based on k8s nodes but in most remove cases,
	// k8s nodes do not exist. It will be impossible to get zone infromation.
	nodesByZone := map[string][]string{}
	for _, name := range names {
		zone, ok := nodeZoneMap[name]
		if !ok {
			m.logger.Error(nil, "Failed to get zones for node, skipping", "name", name)
			continue
		}
		if _, ok := nodesByZone[zone]; !ok {
			nodesByZone[zone] = []string{}
		}
		nodesByZone[zone] = append(nodesByZone[zone], name)
	}

	for zone, nodeNames := range nodesByZone {
		m.logger.V(1).Info("Removing nodes from instance group in zone", "nodeCount", len(nodeNames), "name", groupName, "zone", zone)
		if err := m.cloud.RemoveInstancesFromInstanceGroup(groupName, zone, m.getInstanceReferences(zone, nodeNames)); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) == 0 {
		return nil
	}

	err := fmt.Errorf("RemoveInstances: %v", errs)
	events.GlobalEventf(m.recorder, core.EventTypeWarning, events.RemoveNodes, "Error removing nodes %s from InstanceGroup %q: %v", events.TruncatedStringList(names), groupName, err)
	return err
}

// Sync nodes with the instances in the instance group.
func (m *manager) Sync(nodes []string) (err error) {
	m.logger.V(2).Info("Syncing nodes", "nodes", nodes)

	defer func() {
		// The node pool is only responsible for syncing nodes to instance
		// groups. It never creates/deletes, so if an instance groups is
		// not found there's nothing it can do about it anyway. Most cases
		// this will happen because the backend pool has deleted the instance
		// group, however if it happens because a user deletes the IG by mistake
		// we should just wait till the backend pool fixes it.
		if utils.IsHTTPErrorCode(err, http.StatusNotFound) {
			m.logger.Info("Node pool encountered a 404, ignoring", "err", err)
			err = nil
		}
	}()

	pool, err := m.List()
	if err != nil {
		m.logger.Error(err, "List error")
		return err
	}

	for _, igName := range pool {
		// Keep the zone information for each node in this map.
		// This will be used as a reference to get zone information
		// when removing nodes.
		gceNodes, gceNodeZoneMap, err := m.listIGInstances(igName)
		if err != nil {
			m.logger.Error(err, "listIGInstances error", "name", igName)
			return err
		}
		kubeNodes := sets.NewString(nodes...)

		// Individual InstanceGroup has a limit for 1000 instances in it.
		// As a result, it's not possible to add more to it.
		if len(kubeNodes) > m.maxIGSize {
			// List() will return a sorted list so the kubeNodesList truncation will have a stable set of nodes.
			kubeNodesList := kubeNodes.List()

			// Store first 10 truncated nodes for logging
			truncateForLogs := func(nodes []string) []string {
				maxLogsSampleSize := 10
				if len(nodes) <= maxLogsSampleSize {
					return nodes
				}
				return nodes[:maxLogsSampleSize]
			}

			m.logger.Info(fmt.Sprintf("Total number of kubeNodes: %d, truncating to maximum Instance Group size = %d. Instance group name: %s. First truncated instances: %v", len(kubeNodesList), m.maxIGSize, igName, truncateForLogs(nodes[m.maxIGSize:])))
			kubeNodes = sets.NewString(kubeNodesList[:m.maxIGSize]...)
		}

		// A node deleted via kubernetes could still exist as a gce vm. We don't
		// want to route requests to it. Similarly, a node added to kubernetes
		// needs to get added to the instance group so we do route requests to it.

		removeNodes := gceNodes.Difference(kubeNodes).List()
		addNodes := kubeNodes.Difference(gceNodes).List()

		m.logger.V(2).Info("Removing nodes", "removeNodes", removeNodes)
		m.logger.V(2).Info("Adding nodes", "addNodes", addNodes)

		start := time.Now()
		if len(removeNodes) != 0 {
			err = m.remove(igName, removeNodes, gceNodeZoneMap)
			m.logger.V(2).Info("Remove finished", "name", igName, "err", err, "timeTaken", time.Now().Sub(start), "removeNodes", removeNodes)
			if err != nil {
				return err
			}
		}

		start = time.Now()
		if len(addNodes) != 0 {
			err = m.add(igName, addNodes)
			m.logger.V(2).Info("Add finished", "name", igName, "err", err, "timeTaken", time.Now().Sub(start), "addNodes", addNodes)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// canonicalizeInstanceName take a GCE instance 'hostname' and break it down
// to something that can be fed to the GCE API client library.  Basically
// this means reducing 'kubernetes-node-2.c.my-proj.internal' to
// 'kubernetes-node-2' if necessary.
// Helper function is copied from legacy-cloud-provider gce_utils.go
func canonicalizeInstanceName(name string) string {
	ix := strings.Index(name, ".")
	if ix != -1 {
		name = name[:ix]
	}
	return name
}
