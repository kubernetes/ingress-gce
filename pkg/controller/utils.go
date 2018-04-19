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

package controller

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/golang/glog"

	compute "google.golang.org/api/compute/v1"

	api_v1 "k8s.io/api/core/v1"
	extensions "k8s.io/api/extensions/v1beta1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"

	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/backends"
	"k8s.io/ingress-gce/pkg/utils"
)

// StoreToEndpointLister makes a Store that lists Endpoints.
type StoreToEndpointLister struct {
	cache.Indexer
}

func (s *StoreToEndpointLister) ListEndpointTargetPorts(namespace, name, targetPort string) []int {
	// if targetPort is integer, no need to translate to endpoint ports
	if i, err := strconv.Atoi(targetPort); err == nil {
		return []int{i}
	}

	ep, exists, err := s.Indexer.Get(
		&api_v1.Endpoints{
			ObjectMeta: meta_v1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
		},
	)

	if !exists {
		glog.Errorf("Endpoint object %v/%v does not exist.", namespace, name)
		return []int{}
	}
	if err != nil {
		glog.Errorf("Failed to retrieve endpoint object %v/%v: %v", namespace, name, err)
		return []int{}
	}

	ret := []int{}
	for _, subset := range ep.(*api_v1.Endpoints).Subsets {
		for _, port := range subset.Ports {
			if port.Protocol == api_v1.ProtocolTCP && port.Name == targetPort {
				ret = append(ret, int(port.Port))
			}
		}
	}
	return ret
}

type InstanceGroupsAnnotationValue struct {
	Name string
	Zone string
}

// setInstanceGroupsAnnotation sets the instance-groups annotation with names of the given instance groups.
func setInstanceGroupsAnnotation(existing map[string]string, igs []*compute.InstanceGroup) error {
	var instanceGroups []InstanceGroupsAnnotationValue
	for _, ig := range igs {
		instanceGroups = append(instanceGroups, InstanceGroupsAnnotationValue{Name: ig.Name, Zone: ig.Zone})
	}
	jsonValue, err := json.Marshal(instanceGroups)
	if err != nil {
		return err
	}
	existing[annotations.InstanceGroupsAnnotationKey] = string(jsonValue)
	return nil
}

// instanceGroupsAnnotation returns the values for the instance-groups annotation on an ingress.
func instanceGroupsAnnotation(ing *extensions.Ingress) ([]InstanceGroupsAnnotationValue, error) {
	var igs []InstanceGroupsAnnotationValue
	if val, ok := ing.Annotations[annotations.InstanceGroupsAnnotationKey]; ok {
		if err := json.Unmarshal([]byte(val), &igs); err != nil {
			return nil, fmt.Errorf("error in parsing instance group annotation: %v", err)
		}
	}
	return igs, nil
}

// uniq returns an array of unique service ports from the given array.
func uniq(nodePorts []backends.ServicePort) []backends.ServicePort {
	portMap := map[int64]backends.ServicePort{}
	for _, p := range nodePorts {
		portMap[p.NodePort] = p
	}
	nodePorts = make([]backends.ServicePort, 0, len(portMap))
	for _, sp := range portMap {
		nodePorts = append(nodePorts, sp)
	}
	return nodePorts
}

// getReadyNodeNames returns names of schedulable, ready nodes from the node lister.
func getReadyNodeNames(lister listers.NodeLister) ([]string, error) {
	var nodeNames []string
	nodes, err := lister.ListWithPredicate(utils.NodeIsReady)
	if err != nil {
		return nodeNames, err
	}
	for _, n := range nodes {
		if n.Spec.Unschedulable {
			continue
		}
		nodeNames = append(nodeNames, n.Name)
	}
	return nodeNames, nil
}

func nodeStatusChanged(old, cur *api_v1.Node) bool {
	if old.Spec.Unschedulable != cur.Spec.Unschedulable {
		return true
	}
	if utils.NodeIsReady(old) != utils.NodeIsReady(cur) {
		return true
	}
	return false
}
