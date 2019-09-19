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

package instances

import (
	"fmt"
	"net/http"

	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/klog"

	"google.golang.org/api/compute/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	"k8s.io/ingress-gce/pkg/utils"
)

const (
	// State string required by gce library to list all instances.
	allInstances = "ALL"
)

// Instances implements NodePool.
type Instances struct {
	cloud InstanceGroups
	ZoneLister
	namer *namer.Namer
}

// NewNodePool creates a new node pool.
// - cloud: implements InstanceGroups, used to sync Kubernetes nodes with
//   members of the cloud InstanceGroup.
func NewNodePool(cloud InstanceGroups, namer *namer.Namer) NodePool {
	return &Instances{
		cloud: cloud,
		namer: namer,
	}
}

// Init initializes the instance pool. The given zoneLister is used to list
// all zones that require an instance group, and to lookup which zone a
// given Kubernetes node is in so we can add it to the right instance group.
func (i *Instances) Init(zl ZoneLister) {
	i.ZoneLister = zl
}

// EnsureInstanceGroupsAndPorts creates or gets an instance group if it doesn't exist
// and adds the given ports to it. Returns a list of one instance group per zone,
// all of which have the exact same named ports.
func (i *Instances) EnsureInstanceGroupsAndPorts(name string, ports []int64) (igs []*compute.InstanceGroup, err error) {
	zones, err := i.ListZones()
	if err != nil {
		return nil, err
	}

	for _, zone := range zones {
		ig, err := i.ensureInstanceGroupAndPorts(name, zone, ports)
		if err != nil {
			return nil, err
		}

		igs = append(igs, ig)
	}
	return igs, nil
}

func (i *Instances) ensureInstanceGroupAndPorts(name, zone string, ports []int64) (*compute.InstanceGroup, error) {
	ig, err := i.Get(name, zone)
	if err != nil && !utils.IsHTTPErrorCode(err, http.StatusNotFound) {
		klog.Errorf("Failed to get instance group %v/%v, err: %v", zone, name, err)
		return nil, err
	}

	if ig == nil {
		klog.V(3).Infof("Creating instance group %v/%v.", zone, name)
		if err = i.cloud.CreateInstanceGroup(&compute.InstanceGroup{Name: name}, zone); err != nil {
			// Error may come back with StatusConflict meaning the instance group was created by another controller
			// possibly the Service Controller for internal load balancers.
			if utils.IsHTTPErrorCode(err, http.StatusConflict) {
				klog.Warningf("Failed to create instance group %v/%v due to conflict status, but continuing sync. err: %v", zone, name, err)
			} else {
				klog.Errorf("Failed to create instance group %v/%v, err: %v", zone, name, err)
				return nil, err
			}
		}
		ig, err = i.cloud.GetInstanceGroup(name, zone)
		if err != nil {
			klog.Errorf("Failed to get instance group %v/%v after ensuring existence, err: %v", zone, name, err)
			return nil, err
		}
	} else {
		klog.V(5).Infof("Instance group %v/%v already exists.", zone, name)
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
			klog.V(5).Infof("Instance group %v/%v already has named port %v", zone, ig.Name, p)
			continue
		}
		newPorts = append(newPorts, p)
	}

	// Build slice of NamedPorts for adding
	var newNamedPorts []*compute.NamedPort
	for _, port := range newPorts {
		newNamedPorts = append(newNamedPorts, &compute.NamedPort{Name: i.namer.NamedPort(port), Port: port})
	}

	if len(newNamedPorts) > 0 {
		klog.V(3).Infof("Instance group %v/%v does not have ports %+v, adding them now.", zone, name, newPorts)
		if err := i.cloud.SetNamedPortsOfInstanceGroup(ig.Name, zone, append(ig.NamedPorts, newNamedPorts...)); err != nil {
			return nil, err
		}
	}

	return ig, nil
}

// DeleteInstanceGroup deletes the given IG by name, from all zones.
func (i *Instances) DeleteInstanceGroup(name string) error {
	errs := []error{}

	zones, err := i.ListZones()
	if err != nil {
		return err
	}
	for _, zone := range zones {
		if err := i.cloud.DeleteInstanceGroup(name, zone); err != nil {
			if utils.IsNotFoundError(err) {
				klog.V(3).Infof("Instance group %v in zone %v did not exist", name, zone)
			} else if utils.IsInUsedByError(err) {
				klog.V(3).Infof("Could not delete instance group %v in zone %v because it's still in use. Ignoring: %v", name, zone, err)
			} else {
				errs = append(errs, err)
			}
		} else {
			klog.V(3).Infof("Deleted instance group %v in zone %v", name, zone)
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("%v", errs)
}

// list lists all instances in all zones.
func (i *Instances) list(name string) (sets.String, error) {
	nodeNames := sets.NewString()
	zones, err := i.ListZones()
	if err != nil {
		return nodeNames, err
	}

	for _, zone := range zones {
		instances, err := i.cloud.ListInstancesInInstanceGroup(name, zone, allInstances)
		if err != nil {
			return nodeNames, err
		}
		for _, ins := range instances {
			name, err := utils.KeyName(ins.Instance)
			if err != nil {
				return nodeNames, err
			}
			nodeNames.Insert(name)
		}
	}
	return nodeNames, nil
}

// Get returns the Instance Group by name.
func (i *Instances) Get(name, zone string) (*compute.InstanceGroup, error) {
	ig, err := i.cloud.GetInstanceGroup(name, zone)
	if err != nil {
		return nil, err
	}

	return ig, nil
}

// List lists the names of all InstanceGroups belonging to this cluster.
func (i *Instances) List() ([]string, error) {
	var igs []*compute.InstanceGroup

	zones, err := i.ListZones()
	if err != nil {
		return nil, err
	}

	for _, zone := range zones {
		igsForZone, err := i.cloud.ListInstanceGroups(zone)
		if err != nil {
			return nil, err
		}

		for _, ig := range igsForZone {
			igs = append(igs, ig)
		}
	}

	var names []string
	for _, ig := range igs {
		if i.namer.NameBelongsToCluster(ig.Name) {
			names = append(names, ig.Name)
		}
	}

	return names, nil
}

// splitNodesByZones takes a list of node names and returns a map of zone:node names.
// It figures out the zones by asking the zoneLister.
func (i *Instances) splitNodesByZone(names []string) map[string][]string {
	nodesByZone := map[string][]string{}
	for _, name := range names {
		zone, err := i.GetZoneForNode(name)
		if err != nil {
			klog.Errorf("Failed to get zones for %v: %v, skipping", name, err)
			continue
		}
		if _, ok := nodesByZone[zone]; !ok {
			nodesByZone[zone] = []string{}
		}
		nodesByZone[zone] = append(nodesByZone[zone], name)
	}
	return nodesByZone
}

// Add adds the given instances to the appropriately zoned Instance Group.
func (i *Instances) Add(groupName string, names []string) error {
	errs := []error{}
	for zone, nodeNames := range i.splitNodesByZone(names) {
		klog.V(1).Infof("Adding nodes %v to %v in zone %v", nodeNames, groupName, zone)
		if err := i.cloud.AddInstancesToInstanceGroup(groupName, zone, i.cloud.ToInstanceReferences(zone, nodeNames)); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("%v", errs)
}

// Remove removes the given instances from the appropriately zoned Instance Group.
func (i *Instances) Remove(groupName string, names []string) error {
	errs := []error{}
	for zone, nodeNames := range i.splitNodesByZone(names) {
		klog.V(1).Infof("Removing nodes %v from %v in zone %v", nodeNames, groupName, zone)
		if err := i.cloud.RemoveInstancesFromInstanceGroup(groupName, zone, i.cloud.ToInstanceReferences(zone, nodeNames)); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("%v", errs)
}

// Sync syncs kubernetes instances with the instances in the instance group.
func (i *Instances) Sync(nodes []string) (err error) {
	klog.V(4).Infof("Syncing nodes %v", nodes)

	defer func() {
		// The node pool is only responsible for syncing nodes to instance
		// groups. It never creates/deletes, so if an instance groups is
		// not found there's nothing it can do about it anyway. Most cases
		// this will happen because the backend pool has deleted the instance
		// group, however if it happens because a user deletes the IG by mistake
		// we should just wait till the backend pool fixes it.
		if utils.IsHTTPErrorCode(err, http.StatusNotFound) {
			klog.Infof("Node pool encountered a 404, ignoring: %v", err)
			err = nil
		}
	}()

	pool, err := i.List()
	if err != nil {
		return err
	}

	for _, igName := range pool {
		gceNodes := sets.NewString()
		gceNodes, err = i.list(igName)
		if err != nil {
			return err
		}
		kubeNodes := sets.NewString(nodes...)

		// A node deleted via kubernetes could still exist as a gce vm. We don't
		// want to route requests to it. Similarly, a node added to kubernetes
		// needs to get added to the instance group so we do route requests to it.

		removeNodes := gceNodes.Difference(kubeNodes).List()
		addNodes := kubeNodes.Difference(gceNodes).List()
		if len(removeNodes) != 0 {
			klog.V(4).Infof("Removing nodes from IG: %v", removeNodes)
			if err = i.Remove(igName, removeNodes); err != nil {
				return err
			}
		}

		if len(addNodes) != 0 {
			klog.V(4).Infof("Adding nodes to IG: %v", addNodes)
			if err = i.Add(igName, addNodes); err != nil {
				return err
			}
		}
	}
	return nil
}
