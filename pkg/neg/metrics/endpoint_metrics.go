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

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

const (
	endpointState      = "endpoint_state"
	endpointSliceState = "endpoint_slice_state"

	endpointStateKey      = "neg_sync_endpoint_state"
	endpointSliceStateKey = "neg_sync_endpoint_slice_state"
)

var (
	// syncerEndpointState tracks the count of endpoints in different states
	syncerEndpointState = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: negControllerSubsystem,
			Name:      endpointStateKey,
			Help:      "Current count of endpoints in each state",
		},
		[]string{endpointState},
	)

	// syncerEndpointSliceState tracks the count of endpoint slices in different states
	syncerEndpointSliceState = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: negControllerSubsystem,
			Name:      endpointSliceStateKey,
			Help:      "Current count of endpoint slices in each state",
		},
		[]string{endpointSliceState},
	)
)
