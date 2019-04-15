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
	"k8s.io/ingress-gce/pkg/annotations"
	"reflect"
	"strconv"
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
}

// PortInfoMap is a map of ServicePort:PortInfo
type PortInfoMap map[int32]PortInfo

func NewPortInfoMap(namespace, name string, svcPortMap SvcPortMap, namer NetworkEndpointGroupNamer) PortInfoMap {
	ret := PortInfoMap{}
	for svcPort, targetPort := range svcPortMap {
		ret[svcPort] = PortInfo{
			TargetPort: targetPort,
			NegName:    namer.NEG(namespace, name, svcPort),
		}
	}
	return ret
}

// Merge merges p2 into p1 PortInfoMap
// It assumes the same key will have the same PortInfo
// If not, it will throw error
func (p1 PortInfoMap) Merge(p2 PortInfoMap) error {
	var err error
	for svcPort, portInfo := range p2 {
		if existingPortInfo, ok := p1[svcPort]; ok {
			if !reflect.DeepEqual(existingPortInfo, portInfo) {
				return fmt.Errorf("key %d in PortInfoMaps has different values. Existing value %v while new value: %v", svcPort, existingPortInfo, portInfo)
			}
		}
		p1[svcPort] = portInfo
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
