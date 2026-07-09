/*
Copyright 2026 The Kubernetes Authors.

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
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/klog/v2"
)

// L4StandaloneNEGServiceState tracks the state of an L4 Standalone NEG Service.
type L4StandaloneNEGServiceState struct {
	Status                      L4ServiceStatus
	LBSchemeExternal            bool
	LBSchemeExternalPassthrough bool
	LBSchemeInternal            bool
	FirstSyncErrorTime          *time.Time
}

var (
	l4StandaloneNEGCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "l4_standalone_neg_services_count",
			Help: "Metric containing the number of L4 Standalone NEG Services that can be filtered by scheme labels and status",
		},
		[]string{"status", "lb_scheme_external", "lb_scheme_external_passthrough", "lb_scheme_internal"},
	)
)

// SetL4StandaloneNEGService adds/updates L4 Standalone NEG service state for given service key.
func (c *Collector) SetL4StandaloneNEGService(svcKey string, state L4StandaloneNEGServiceState) {
	c.Lock()
	defer c.Unlock()

	if c.l4StandaloneNEGServiceMap == nil {
		klog.Fatalf("L4 Standalone NEG Metrics failed to initialize correctly.")
	}

	if state.Status == StatusError {
		if previousState, ok := c.l4StandaloneNEGServiceMap[svcKey]; ok && previousState.FirstSyncErrorTime != nil {
			// If service is in Error state and retry timestamp was set then do not update it.
			state.FirstSyncErrorTime = previousState.FirstSyncErrorTime
		}
	}
	c.l4StandaloneNEGServiceMap[svcKey] = state
}

// DeleteL4StandaloneNEGService removes the given L4 Standalone NEG service key.
func (c *Collector) DeleteL4StandaloneNEGService(svcKey string) {
	c.Lock()
	defer c.Unlock()

	delete(c.l4StandaloneNEGServiceMap, svcKey)
}

// StandaloneNEGServiceState returns the state of the given service key (used for testing).
func (c *Collector) StandaloneNEGServiceState(svcKey string) (L4StandaloneNEGServiceState, bool) {
	c.Lock()
	defer c.Unlock()

	state, ok := c.l4StandaloneNEGServiceMap[svcKey]
	return state, ok
}

func (c *Collector) exportStandaloneNEG() {
	c.Lock()
	defer c.Unlock()
	c.logger.V(3).Info("Exporting L4 Standalone NEG usage metrics for services", "serviceCount", len(c.l4StandaloneNEGServiceMap))
	l4StandaloneNEGCount.Reset()
	for _, svcState := range c.l4StandaloneNEGServiceMap {
		l4StandaloneNEGCount.With(prometheus.Labels{
			"status":                         string(getStandaloneStatusConsideringPersistentError(&svcState)),
			"lb_scheme_external":             strconv.FormatBool(svcState.LBSchemeExternal),
			"lb_scheme_external_passthrough": strconv.FormatBool(svcState.LBSchemeExternalPassthrough),
			"lb_scheme_internal":             strconv.FormatBool(svcState.LBSchemeInternal),
		}).Inc()
	}
	c.logger.V(3).Info("L4 Standalone NEG usage metrics exported")
}

func getStandaloneStatusConsideringPersistentError(state *L4StandaloneNEGServiceState) L4ServiceStatus {
	if state.Status == StatusError &&
		state.FirstSyncErrorTime != nil &&
		time.Since(*state.FirstSyncErrorTime) >= persistentErrorThresholdTime {
		return StatusPersistentError
	}
	return state.Status
}
