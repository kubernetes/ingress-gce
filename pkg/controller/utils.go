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

	compute "google.golang.org/api/compute/v1"
	api_v1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/networking/v1"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/utils"
)

// setInstanceGroupsAnnotation sets the instance-groups annotation with names of the given instance groups.
func setInstanceGroupsAnnotation(existing map[string]string, igs []*compute.InstanceGroup) error {
	type Value struct {
		Name string
		Zone string
	}
	var instanceGroups []Value
	for _, ig := range igs {
		instanceGroups = append(instanceGroups, Value{Name: ig.Name, Zone: ig.Zone})
	}
	jsonValue, err := json.Marshal(instanceGroups)
	if err != nil {
		return err
	}
	existing[annotations.InstanceGroupsAnnotationKey] = string(jsonValue)
	return nil
}

// uniq returns an array of unique service ports from the given array.
func uniq(svcPorts []utils.ServicePort) []utils.ServicePort {
	portMap := map[string]utils.ServicePort{}
	for _, p := range svcPorts {
		portMap[fmt.Sprintf("%q-%d", p.ID.Service.String(), p.Port)] = p
	}
	svcPorts = make([]utils.ServicePort, 0, len(portMap))
	for _, sp := range portMap {
		svcPorts = append(svcPorts, sp)
	}
	return svcPorts
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

func convert(ings []*v1.Ingress) (retVal []interface{}) {
	for _, ing := range ings {
		retVal = append(retVal, ing)
	}
	return
}

// nodePorts returns the list of uniq NodePort from the input ServicePorts.
// Only NonNEG service backend need NodePort.
func nodePorts(svcPorts []utils.ServicePort) []int64 {
	ports := []int64{}
	for _, p := range uniq(svcPorts) {
		if !p.NEGEnabled {
			ports = append(ports, p.NodePort)
		}
	}
	return ports
}
