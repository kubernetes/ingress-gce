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

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/ingress-gce/pkg/annotations"
)

// SvcPortMap is a map of ServicePort:TargetPort
type SvcPortMap map[int32]string

// PortInfo contains information associated with service port
type PortInfo struct {
	// TargetPort is the target port of the service port
	// This can be port number or a named port
	TargetPort string
	// NegName is the name of the NEG
	NegName string
	// ReadinessGate indicates if the NEG associated with the port has NEG readiness gate enabled
	// This is enabled with service port is reference by ingress.
	// If the service port is only exposed as stand alone NEG, it should not be enbled.
	ReadinessGate bool
}

// PortInfoMap is a map of ServicePort:PortInfo
type PortInfoMap map[int32]PortInfo

func NewPortInfoMap(namespace, name string, svcPortMap SvcPortMap, namer NetworkEndpointGroupNamer, readinessGate bool) PortInfoMap {
	ret := PortInfoMap{}
	for svcPort, targetPort := range svcPortMap {
		ret[svcPort] = PortInfo{
			TargetPort:    targetPort,
			NegName:       namer.NEG(namespace, name, svcPort),
			ReadinessGate: readinessGate,
		}
	}
	return ret
}

// Merge merges p2 into p1 PortInfoMap
// It assumes the same key (service port) will have the same target port and negName
// If not, it will throw error
// If a key in p1 or p2 has readiness gate enabled, the merged port info will also has readiness gate enabled
func (p1 PortInfoMap) Merge(p2 PortInfoMap) error {
	var err error
	for svcPort, portInfo := range p2 {
		mergedInfo := PortInfo{}
		if existingPortInfo, ok := p1[svcPort]; ok {
			if existingPortInfo.TargetPort != portInfo.TargetPort {
				return fmt.Errorf("for service port %d, target port in existing map is %q, but the merge map has %q", svcPort, existingPortInfo.TargetPort, portInfo.TargetPort)
			}
			if existingPortInfo.NegName != portInfo.NegName {
				return fmt.Errorf("for service port %d, NEG name in existing map is %q, but the merge map has %q", svcPort, existingPortInfo.NegName, portInfo.NegName)
			}
			mergedInfo.ReadinessGate = existingPortInfo.ReadinessGate
		}
		mergedInfo.TargetPort = portInfo.TargetPort
		mergedInfo.NegName = portInfo.NegName
		mergedInfo.ReadinessGate = mergedInfo.ReadinessGate || portInfo.ReadinessGate
		p1[svcPort] = mergedInfo
	}
	return err
}

// Difference returns the entries of PortInfoMap which satisfies one of the following 2 conditions:
// 1. portInfo entry is not the present in p1
// 2. or the portInfo entry is not the same in p1 and p2.
func (p1 PortInfoMap) Difference(p2 PortInfoMap) PortInfoMap {
	result := make(PortInfoMap)
	for svcPort, p1PortInfo := range p1 {
		p2PortInfo, ok := p2[svcPort]
		if ok && reflect.DeepEqual(p1[svcPort], p2PortInfo) {
			continue
		}
		result[svcPort] = p1PortInfo
	}
	return result
}

func (p1 PortInfoMap) ToPortNegMap() annotations.PortNegMap {
	ret := annotations.PortNegMap{}
	for svcPort, portInfo := range p1 {
		ret[strconv.Itoa(int(svcPort))] = portInfo.NegName
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
}

func (key NegSyncerKey) String() string {
	return fmt.Sprintf("%s/%s-%v/%s", key.Namespace, key.Name, key.Port, key.TargetPort)
}

// EndpointPodMap is a map from network endpoint to a namespaced name of a pod
type EndpointPodMap map[NetworkEndpoint]types.NamespacedName
