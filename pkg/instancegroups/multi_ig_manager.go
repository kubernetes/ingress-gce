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

	"google.golang.org/api/compute/v1"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/klog/v2"

	"k8s.io/apimachinery/pkg/util/sets"

	"k8s.io/ingress-gce/pkg/utils"
)

// multiIGManager implements Manager.
type multiIGManager struct {
	*manager
	maxNumberOfIGs int
}

// MultiIGManagerConfig is used for MultiIGManager constructor.
type MultiIGManagerConfig struct {
	// Cloud implements Provider, used to sync Kubernetes nodes with members of the cloud InstanceGroup.
	Cloud      Provider
	Namer      namer.BackendNamer
	Recorders  recorderSource
	BasePath   string
	ZoneLister ZoneLister
	MaxIGSize  int
}

// NewMultiIGManager creates a new Multi Instance Group Manager using MultiIGManagerConfig.
func NewMultiIGManager(config *MultiIGManagerConfig) Manager {
	return &multiIGManager{
		manager: &manager{
			cloud:              config.Cloud,
			namer:              config.Namer,
			recorder:           config.Recorders.Recorder(""), // No namespace
			instanceLinkFormat: config.BasePath + "zones/%s/instances/%s",
			ZoneLister:         config.ZoneLister,
			maxIGSize:          config.MaxIGSize,
		},
		maxNumberOfIGs: 15,
	}
}

// DeleteInstanceGroup deletes all possible cluster IGs (indexed 1-15) from all zones
func (migm *multiIGManager) DeleteInstanceGroup() error {
	var errs []error

	zones, err := migm.ListZones(utils.AllNodesPredicate)
	if err != nil {
		return fmt.Errorf("error listing zones on instance groups deletion %w", err)
	}

	for igIndex := 0; igIndex < migm.maxNumberOfIGs; igIndex++ {
		name := migm.namer.InstanceGroupByIndex(igIndex)
		for _, zone := range zones {
			if err := migm.cloud.DeleteInstanceGroup(name, zone); err != nil {
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
	}
	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("failed to delete instance groups %v", errs)
}

func (migm *multiIGManager) Sync(nodes []string) (err error) {
	clusterIGs, err := migm.List()
	if err != nil {
		return fmt.Errorf("failed to list cluster Instance Groups: %w", err)
	}
	if len(clusterIGs) == 0 {
		// if no instance groups were created yet, it means no Load Balancers were created,
		// so we don't need to sync
		return nil
	}

	klog.V(2).Infof("Got %d cluster instance groups, syncing nodes", len(clusterIGs))
	for zone, zonalNodeNames := range migm.splitNodesByZone(nodes) {
		if err = migm.syncZone(zone, zonalNodeNames); err != nil {
			return fmt.Errorf("failed to sync instance groups for zone %s, error %w", zone, err)
		}
	}
	return nil
}

func (migm *multiIGManager) syncZone(zone string, zonalNodeNames []string) error {
	err, nodesToIG, igSizes, nodesToRemove, nodesToAdd := migm.calculateDataPerZone(zone, zonalNodeNames)
	if err != nil {
		return err
	}

	klog.V(2).Infof("Removing %d, adding %d nodes", len(nodesToRemove), len(nodesToAdd))

	if err = migm.removeNodes(nodesToRemove, zone, nodesToIG); err != nil {
		return fmt.Errorf("failed to remove nodes from instance groups in zone %s, error %w", zone, err)
	}
	if err = migm.addNodes(nodesToAdd, zone, igSizes); err != nil {
		return fmt.Errorf("failed to add nodes to instance groups in zone %s, error %w", zone, err)
	}
	return nil
}

func (migm *multiIGManager) calculateDataPerZone(zone string, zonalNodeNames []string) (err error, nodesToIG map[string]*compute.InstanceGroup, igSizes map[string]int, nodesToRemove []string, nodesToAdd []string) {
	allIGsForZone, err := migm.cloud.ListInstanceGroups(zone)
	if err != nil {
		return fmt.Errorf("migm.cloud.ListInstanceGroups(%s) returned error %w", zone, err), nil, nil, nil, nil
	}

	nodesToIG = make(map[string]*compute.InstanceGroup)
	igSizes = make(map[string]int)
	gceNodes := sets.NewString()

	for _, ig := range allIGsForZone {
		if migm.namer.NameBelongsToCluster(ig.Name) {
			instancesInIG, err := migm.cloud.ListInstancesInInstanceGroup(ig.Name, zone, allInstances)
			if err != nil {
				return fmt.Errorf("migm.cloud.ListInstancesInInstanceGroup(%s, %s, %s) returned error %w", ig.Name, zone, allInstances, err), nil, nil, nil, nil
			}

			igSizes[ig.Name] += len(instancesInIG)

			for _, i := range instancesInIG {
				instanceName, err := utils.KeyName(i.Instance)
				if err != nil {
					return fmt.Errorf("utils.KeyName(%s) returned error %w", i.Instance, err), nil, nil, nil, nil
				}
				nodesToIG[instanceName] = ig
				gceNodes.Insert(instanceName)
			}
		}
	}
	kubeNodes := sets.NewString(zonalNodeNames...)

	// A node deleted via kubernetes could still exist as a gce vm. We don't
	// want to route requests to it. Similarly, a node added to kubernetes
	// needs to get added to the instance group, so we do route requests to it.
	nodesToRemove = gceNodes.Difference(kubeNodes).List()
	nodesToAdd = kubeNodes.Difference(gceNodes).List()

	return nil, nodesToIG, igSizes, nodesToRemove, nodesToAdd
}

func (migm *multiIGManager) removeNodes(nodesToRemove []string, zone string, nodesToIG map[string]*compute.InstanceGroup) error {
	igToRemoveNodes := make(map[string]sets.String)
	for _, node := range nodesToRemove {
		if _, ok := igToRemoveNodes[nodesToIG[node].Name]; !ok {
			igToRemoveNodes[nodesToIG[node].Name] = sets.NewString()
		}
		igToRemoveNodes[nodesToIG[node].Name].Insert(node)
	}

	for igName, nodes := range igToRemoveNodes {
		if err := migm.cloud.RemoveInstancesFromInstanceGroup(igName, zone, migm.getInstanceReferences(zone, nodes.List())); err != nil {
			return fmt.Errorf("failed to remove instance group %s in zone %s, error %w", igName, zone, err)
		}
	}
	return nil
}

func (migm *multiIGManager) addNodes(nodesToAdd []string, zone string, igSizes map[string]int) error {
	var igIndex int
	for igIndex = 0; len(nodesToAdd) != 0 && igIndex < migm.maxNumberOfIGs; igIndex++ {
		indexedIGName := migm.namer.InstanceGroupByIndex(igIndex)
		if _, exist := igSizes[indexedIGName]; !exist {
			err := migm.cloud.CreateInstanceGroup(&compute.InstanceGroup{Name: indexedIGName}, zone)
			if err != nil {
				return fmt.Errorf("failed to create instance group %s, in zone %s, error: %w", indexedIGName, zone, err)
			}
			igSizes[indexedIGName] = 0
		}
		notUsedIGSpace := migm.maxIGSize - igSizes[indexedIGName]

		currentNodesToAdd := nodesToAdd
		if notUsedIGSpace < len(currentNodesToAdd) {
			currentNodesToAdd = currentNodesToAdd[:notUsedIGSpace]
		}

		if err := migm.cloud.AddInstancesToInstanceGroup(indexedIGName, zone, migm.getInstanceReferences(zone, currentNodesToAdd)); err != nil {
			return fmt.Errorf("failed to add instances to instance group %s in zone %s, error: %w", indexedIGName, zone, err)
		}

		if len(nodesToAdd) > notUsedIGSpace {
			nodesToAdd = nodesToAdd[notUsedIGSpace:]
		} else {
			nodesToAdd = []string{}
		}
	}
	return nil
}
