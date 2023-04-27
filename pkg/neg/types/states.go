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
	NodeMissing            = State("NodeMissing")
	PodMissing             = State("PodMissing")
	PodNotFound            = State("PodNotFound")
	PodTypeAssertionFailed = State("PodTypeAssertionFailed")
	ZoneMissing            = State("ZoneMissing")
	InvalidField           = State("InvalidField")
	Duplicate              = State("Duplicate")
	Total                  = State("Total")

	// these states are for degraded mode only
	NodeNotFound            = State("NodeNotFound")
	NodeTypeAssertionFailed = State("NodeTypeAssertionFailed")
	PodTerminal             = State("PodTerminal")
	PodLabelMismatch        = State("PodLabelMismatch")
	IPInvalid               = State("IPInvalid")
	IPNotFromPod            = State("IPNotFromPod")
	IPOutOfPodCIDR          = State("IPOutOfPodCIDR")
	ServiceNotFound         = State("ServiceNotFound")
)

// StateCountMap collect the count of instances in different states
type StateCountMap map[State]int

// StatesForEndpointMetrics gets all states for endpoint and endpoint slice
func StatesForEndpointMetrics() []State {
	return []State{NodeMissing, PodMissing, PodNotFound, PodTypeAssertionFailed, ZoneMissing, InvalidField, Duplicate, Total,
		NodeNotFound, NodeTypeAssertionFailed, PodTerminal, PodLabelMismatch, IPNotFromPod, IPOutOfPodCIDR, ServiceNotFound}
}
