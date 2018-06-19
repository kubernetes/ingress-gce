/*
Copyright 2017 The Kubernetes Authors.

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

package neg

import (
	"fmt"

	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/ingress-gce/pkg/annotations"
)

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
		if _, ok := p2[svcPort]; !ok {
			result[svcPort] = targetPort
		}
	}

	return result
}

// NEGServicePorts returns the parsed ServicePorts from the annotation.
// knownPorts represents the known Port:TargetPort attributes of servicePorts
// that already exist on the service. This function returns an error if
// any of the parsed ServicePorts from the annotation is not in knownPorts.
func NEGServicePorts(exposedNegPortMap annotations.ExposeNegAnnotation, knownPorts PortNameMap) (PortNameMap, error) {
	portSet := make(PortNameMap)
	var errList []error
	for port, _ := range exposedNegPortMap {
		// TODO: also validate ServicePorts in the exposed NEG annotation via webhook
		if _, ok := knownPorts[port]; !ok {
			errList = append(errList, fmt.Errorf("ServicePort %v doesn't exist on Service", port))
		}
		portSet[port] = knownPorts[port]
	}

	return portSet, utilerrors.NewAggregate(errList)
}

// NegServiceState contains name and zone of the Network Endpoint Group
// resources associated with this service
type NegServiceState struct {
	// NetworkEndpointGroups returns the mapping between service port and NEG
	// resource. key is service port, value is the name of the NEG resource.
	NetworkEndpointGroups PortNameMap `json:"network_endpoint_groups,omitempty"`
	Zones                 []string    `json:"zones,omitempty"`
}

// GenNegServiceState generates a NegServiceState denoting the current NEGs
// associated with the given ports.
// NetworkEndpointGroups is a mapping between ServicePort and NEG name
// Zones is a list of zones where the NEGs exist.
func GenNegServiceState(zones []string, portToNegs PortNameMap) NegServiceState {
	res := NegServiceState{}
	res.NetworkEndpointGroups = make(PortNameMap)
	res.Zones = zones
	res.NetworkEndpointGroups = portToNegs
	return res
}
