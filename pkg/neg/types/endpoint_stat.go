/*
Copyright 2020 The Kubernetes Authors.

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

type State string

func (s State) String() string {
	return string(s)
}

const (
	EPMissingNodeName = State("endpointMissingNodeName")
	EPMissingPod      = State("endpointMissingPod")
	EPMissingZone     = State("endpointMissingZone")
	EPMissingField    = State("endpointMissingField")
	EPDuplicate       = State("endpointDuplicate")
	EPTotal           = State("endpointTotal")
)

func StateForEP() []State {
	return []State{EPMissingNodeName, EPMissingPod, EPMissingZone, EPMissingField, EPDuplicate, EPTotal}
}

// SyncerEPStat contains endpoint and endpointslice status related to a syncer
type SyncerEPStat struct {
	EPState EndpointState
}

// EndpointState contains all endpoint state related to a syncer
type EndpointState map[State]int

func NewSyncerEPStat() *SyncerEPStat {
	return &SyncerEPStat{
		EPState: make(map[State]int),
	}
}
