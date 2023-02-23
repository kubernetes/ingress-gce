/*
Copyright 2023 The Kubernetes Authors.

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

const (
	EPMissingNodeName = State("EndpointMissingNodeName")
	EPMissingPod      = State("EndpointMissingPod")
	EPMissingZone     = State("EndpointMissingZone")
	EPMissingField    = State("EndpointMissingField")
	EPDuplicate       = State("EndpointDuplicate")
	EPTotal           = State("EndpointTotal")

	EPSWithMissingNodeName = State("EndpointsliceWithMissingNodeNameEP")
	EPSWithMissingPod      = State("EndpointsliceWithMissingPodEP")
	EPSWithMissingZone     = State("EndpointsliceWithMissingZoneEP")
	EPSWithMissingField    = State("EndpointsliceWithMissingFieldEP")
	EPSWithDuplicate       = State("EndpointsliceWithDuplicateEP")
	EPSTotal               = State("EndpointsliceTotal")
)

// SyncerEPStat contains endpoint and endpointslice status related to a syncer
type SyncerEPStat struct {
	EndpointStateCount      StateCountMap
	EndpointSliceStateCount StateCountMap
}

// StateCountMap collect the count of instances in different states
type StateCountMap map[State]int

func NewSyncerEPStat() *SyncerEPStat {
	return &SyncerEPStat{
		EndpointStateCount:      make(map[State]int),
		EndpointSliceStateCount: make(map[State]int),
	}
}
