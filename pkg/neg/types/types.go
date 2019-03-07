/*
Copyright 2018 The Kubernetes Authors.

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

package types

import "k8s.io/kubernetes/staging/src/k8s.io/apimachinery/pkg/util/json"

// PortNameMap is a map of ServicePort:TargetPort.
type PortNameMap map[int32]string

// Union returns the union of the Port:TargetPort mappings
func (p1 PortNameMap) Union(p2 PortNameMap) PortNameMap {
	result := make(PortNameMap)
	for svcPort, targetPort := range p1 {
		result[svcPort] = targetPort
	}

	for svcPort, targetPort := range p2 {
		result[svcPort] = targetPort
	}

	return result
}

// Difference returns the set of Port:TargetPorts in p2 that aren't present in p1
func (p1 PortNameMap) Difference(p2 PortNameMap) PortNameMap {
	result := make(PortNameMap)
	for svcPort, targetPort := range p1 {
		if p1[svcPort] != p2[svcPort] {
			result[svcPort] = targetPort
		}
	}
	return result
}

// NegStatus contains name and zone of the Network Endpoint Group
// resources associated with this service
type NegStatus struct {
	// NetworkEndpointGroups returns the mapping between service port and NEG
	// resource. key is service port, value is the name of the NEG resource.
	NetworkEndpointGroups PortNameMap `json:"network_endpoint_groups,omitempty"`
	Zones                 []string    `json:"zones,omitempty"`
}

// ParseNegStatus parses the given annotation into NEG status struct
func ParseNegStatus(annotation string) (NegStatus, error){
	ret := &NegStatus{}
	err := json.Unmarshal([]byte(annotation), ret)
	return *ret, err
}