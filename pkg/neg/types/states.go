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
	PodInvalid             = State("PodInvalid")
	PodTerminal            = State("PodTerminal")
	PodLabelMismatch       = State("PodLabelMismatch")
	NodeMissing            = State("NodeMissing")
	NodeNotFound           = State("NodeNotFound")
	ZoneMissing            = State("ZoneMissing")
	IPInvalid              = State("IPInvalid")
	IPNotFromPod           = State("IPNotFromPod")
	IPOutOfPodCIDR         = State("IPOutOfPodCIDR")
	OtherError             = State("OtherError")
	Duplicate              = State("Duplicate")
	DualStackMigration     = State("DualStackMigration") // Total number of endpoints which require migration.
	NodeInNonDefaultSubnet = State("NodeInNonDefaultSubnet")
	Total                  = State("Total")
)

// StateCountMap collect the count of instances in different states
type StateCountMap map[State]int

// StatesForEndpointMetrics gets all states for endpoint and endpoint slice
func StatesForEndpointMetrics() []State {
	return []State{PodInvalid, PodTerminal, PodLabelMismatch, NodeMissing, NodeNotFound,
		ZoneMissing, IPInvalid, IPNotFromPod, IPOutOfPodCIDR, OtherError, Duplicate, Total}
}
