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

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"

	istioV1alpha3 "istio.io/api/networking/v1alpha3"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/ingress-gce/pkg/annotations"
)

type NetworkEndpointType string

const (
	VMNetworkEndpointType     = NetworkEndpointType("GCE_VM_IP_PORT")
	NonGCPPrivateEndpointType = NetworkEndpointType("NON_GCP_PRIVATE_IP_PORT")
)

// SvcPortMap is a map of ServicePort:TargetPort
type SvcPortMap map[int32]string

// PortInfo contains information associated with service port
type PortInfo struct {
	// TargetPort is the target port of the service port
	// This can be port number or a named port
	TargetPort string

	// Subset name, subset is defined in Istio:DestinationRule, this is only used
	// when --enable-csm=true.
	Subset string

	// Subset label, should set together with Subset.
	SubsetLabels string

	// NegName is the name of the NEG
	NegName string
	// ReadinessGate indicates if the NEG associated with the port has NEG readiness gate enabled
	// This is enabled with service port is reference by ingress.
	// If the service port is only exposed as stand alone NEG, it should not be enbled.
	ReadinessGate bool
}

// PortInfoMapKey is the Key of PortInfoMap
type PortInfoMapKey struct {
	ServicePort int32

	// Istio:DestinationRule Subset, only used when --enable-csm=true
	Subset string
}

// PortInfoMap is a map of PortInfoMapKey:PortInfo
type PortInfoMap map[PortInfoMapKey]PortInfo

func NewPortInfoMap(namespace, name string, svcPortMap SvcPortMap, namer NetworkEndpointGroupNamer, readinessGate bool) PortInfoMap {
	ret := PortInfoMap{}
	for svcPort, targetPort := range svcPortMap {
		ret[PortInfoMapKey{svcPort, ""}] = PortInfo{
			TargetPort:    targetPort,
			NegName:       namer.NEG(namespace, name, svcPort),
			ReadinessGate: readinessGate,
		}
	}
	return ret
}

// NewPortInfoMapWithDestinationRule create PortInfoMap based on a gaven DesinationRule.
// Return error message if the DestinationRule contains duplicated subsets.
func NewPortInfoMapWithDestinationRule(namespace, name string, svcPortMap SvcPortMap, namer NetworkEndpointGroupNamer, readinessGate bool,
	destinationRule *istioV1alpha3.DestinationRule) (PortInfoMap, error) {
	ret := PortInfoMap{}
	var duplicateSubset []string
	for _, subset := range destinationRule.Subsets {
		for svcPort, targetPort := range svcPortMap {
			key := PortInfoMapKey{svcPort, subset.Name}
			if _, ok := ret[key]; ok {
				duplicateSubset = append(duplicateSubset, subset.Name)
			}
			ret[key] = PortInfo{
				TargetPort:    targetPort,
				NegName:       namer.NEGWithSubset(namespace, name, subset.Name, svcPort),
				ReadinessGate: readinessGate,
				Subset:        subset.Name,
				SubsetLabels:  labels.Set(subset.Labels).String(),
			}
		}
	}
	if len(duplicateSubset) != 0 {
		return ret, fmt.Errorf("Duplicated subsets: %s", strings.Join(duplicateSubset, ", "))
	}
	return ret, nil
}

// Merge merges p2 into p1 PortInfoMap
// It assumes the same key (service port) will have the same target port and negName
// If not, it will throw error
// If a key in p1 or p2 has readiness gate enabled, the merged port info will also has readiness gate enabled
func (p1 PortInfoMap) Merge(p2 PortInfoMap) error {
	var err error
	for mapKey, portInfo := range p2 {
		mergedInfo := PortInfo{}
		if existingPortInfo, ok := p1[mapKey]; ok {
			if existingPortInfo.TargetPort != portInfo.TargetPort {
				return fmt.Errorf("for service port %v, target port in existing map is %q, but the merge map has %q", mapKey, existingPortInfo.TargetPort, portInfo.TargetPort)
			}
			if existingPortInfo.NegName != portInfo.NegName {
				return fmt.Errorf("for service port %v, NEG name in existing map is %q, but the merge map has %q", mapKey, existingPortInfo.NegName, portInfo.NegName)
			}
			if existingPortInfo.Subset != portInfo.Subset {
				return fmt.Errorf("for service port %v, Subset name in existing map is %q, but the merge map has %q", mapKey, existingPortInfo.Subset, portInfo.Subset)
			}
			mergedInfo.ReadinessGate = existingPortInfo.ReadinessGate
		}
		mergedInfo.TargetPort = portInfo.TargetPort
		mergedInfo.NegName = portInfo.NegName
		mergedInfo.ReadinessGate = mergedInfo.ReadinessGate || portInfo.ReadinessGate
		mergedInfo.Subset = portInfo.Subset
		mergedInfo.SubsetLabels = portInfo.SubsetLabels

		p1[mapKey] = mergedInfo
	}
	return err
}

// Difference returns the entries of PortInfoMap which satisfies one of the following 2 conditions:
// 1. portInfo entry is not the present in p1
// 2. or the portInfo entry is not the same in p1 and p2.
func (p1 PortInfoMap) Difference(p2 PortInfoMap) PortInfoMap {
	result := make(PortInfoMap)
	for mapKey, p1PortInfo := range p1 {
		p2PortInfo, ok := p2[mapKey]
		if ok && reflect.DeepEqual(p1[mapKey], p2PortInfo) {
			continue
		}
		result[mapKey] = p1PortInfo
	}
	return result
}

func (p1 PortInfoMap) ToPortNegMap() annotations.PortNegMap {
	ret := annotations.PortNegMap{}
	for mapKey, portInfo := range p1 {
		ret[strconv.Itoa(int(mapKey.ServicePort))] = portInfo.NegName
	}
	return ret
}

func (p1 PortInfoMap) ToPortSubsetNegMap() annotations.PortSubsetNegMap {
	ret := annotations.PortSubsetNegMap{}
	for mapKey, portInfo := range p1 {
		if m, ok := ret[mapKey.Subset]; ok {
			m[strconv.Itoa(int(mapKey.ServicePort))] = portInfo.NegName
		} else {
			ret[mapKey.Subset] = map[string]string{strconv.Itoa(int(mapKey.ServicePort)): portInfo.NegName}
		}
	}
	return ret
}

// NegsWithReadinessGate returns the NegNames which has readiness gate enabled
func (p1 PortInfoMap) NegsWithReadinessGate() sets.String {
	ret := sets.NewString()
	for _, info := range p1 {
		if info.ReadinessGate {
			ret.Insert(info.NegName)
		}
	}
	return ret
}

// NegSyncerKey includes information to uniquely identify a NEG syncer
type NegSyncerKey struct {
	// Namespace of service
	Namespace string
	// Name of service
	Name string
	// Service port
	Port int32
	// Service target port
	TargetPort string

	// Subset name, subset is defined in Istio:DestinationRule, this is only used
	// when --enable-csm=true.
	Subset string

	// Subset label, should set together with Subset.
	SubsetLabels string
}

func (key NegSyncerKey) String() string {
	return fmt.Sprintf("%s/%s-%s-%v/%s", key.Namespace, key.Name, key.Subset, key.Port, key.TargetPort)
}

// EndpointPodMap is a map from network endpoint to a namespaced name of a pod
type EndpointPodMap map[NetworkEndpoint]types.NamespacedName
