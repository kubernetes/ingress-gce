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
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/klog/v2"
)

var (
	// Legacy L4 ILB metric. It has only one label "feature" which makes it impossible to intersect on this label and count number of services that are using combination of different features.
	l4ILBLegacyCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "number_of_l4_ilbs",
			Help: "Number of L4 ILBs",
		},
		[]string{label},
	)
	// Legacy L4 NetLB metric. It has only one label "feature" which makes it impossible to intersect on this label and count number of services that are using combination of different features.
	l4NetLBLegacyCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "number_of_l4_netlbs",
			Help: "Number of L4 NetLBs",
		},
		[]string{label},
	)
)

// netLBFeatureCount define metric feature count for L4NetLB controller
type netLBFeatureCount struct {
	service            int
	managedStaticIP    int
	premiumNetworkTier int
	success            int
	inUserError        int
	inError            int
}

func (netlbCount *netLBFeatureCount) record() {
	l4NetLBLegacyCount.With(prometheus.Labels{label: l4NetLBService.String()}).Set(float64(netlbCount.service))
	l4NetLBLegacyCount.With(prometheus.Labels{label: l4NetLBStaticIP.String()}).Set(float64(netlbCount.success))
	l4NetLBLegacyCount.With(prometheus.Labels{label: l4NetLBPremiumNetworkTier.String()}).Set(float64(netlbCount.premiumNetworkTier))
	l4NetLBLegacyCount.With(prometheus.Labels{label: l4NetLBManagedStaticIP.String()}).Set(float64(netlbCount.managedStaticIP))
	l4NetLBLegacyCount.With(prometheus.Labels{label: l4NetLBInSuccess.String()}).Set(float64(netlbCount.success))
	l4NetLBLegacyCount.With(prometheus.Labels{label: l4NetLBInUserError.String()}).Set(float64(netlbCount.inUserError))
	l4NetLBLegacyCount.With(prometheus.Labels{label: l4NetLBInError.String()}).Set(float64(netlbCount.inError))
}

// SetL4ILBServiceForLegacyMetric adds service to the L4 ILB Legacy Metrics states map.
func (im *ControllerMetrics) SetL4ILBServiceForLegacyMetric(svcKey string, state L4ILBServiceLegacyState) {
	im.Lock()
	defer im.Unlock()

	if im.l4ILBServiceLegacyMap == nil {
		klog.Fatalf("L4 ILB Legacy Metrics failed to initialize correctly.")
	}
	im.l4ILBServiceLegacyMap[svcKey] = state
}

// DeleteL4ILBServiceForLegacyMetric deletes service from L4 ILB Legacy Metrics states map.
func (im *ControllerMetrics) DeleteL4ILBServiceForLegacyMetric(svcKey string) {
	im.Lock()
	defer im.Unlock()

	delete(im.l4ILBServiceLegacyMap, svcKey)
}

// SetL4NetLBServiceForLegacyMetric adds service to the L4 NetLB Legacy Metrics states map.
func (im *ControllerMetrics) SetL4NetLBServiceForLegacyMetric(svcKey string, state L4NetLBServiceLegacyState) {
	im.Lock()
	defer im.Unlock()

	if im.l4NetLBServiceLegacyMap == nil {
		klog.Fatalf("L4 Net LB Legacy Metrics failed to initialize correctly.")
	}

	if !state.InSuccess {
		if previousState, ok := im.l4NetLBServiceLegacyMap[svcKey]; ok && previousState.FirstSyncErrorTime != nil {
			// If service is in Error state and retry timestamp was set then do not update it.
			state.FirstSyncErrorTime = previousState.FirstSyncErrorTime
		}
	}
	im.l4NetLBServiceLegacyMap[svcKey] = state
}

// DeleteL4NetLBServiceForLegacyMetric deletes service from L4 ILB Legacy Metrics states map.
func (im *ControllerMetrics) DeleteL4NetLBServiceForLegacyMetric(svcKey string) {
	im.Lock()
	defer im.Unlock()

	delete(im.l4NetLBServiceLegacyMap, svcKey)
}

// computeL4ILBLegacyMetrics aggregates L4 ILB metrics in the cache.
func (im *ControllerMetrics) computeL4ILBLegacyMetrics() map[feature]int {
	im.Lock()
	defer im.Unlock()
	im.logger.V(4).Info("Computing L4 ILB usage legacy metrics from service state map", "serviceStateMap", im.l4ILBServiceLegacyMap)
	counts := map[feature]int{
		l4ILBService:      0,
		l4ILBGlobalAccess: 0,
		l4ILBCustomSubnet: 0,
		l4ILBInSuccess:    0,
		l4ILBInError:      0,
		l4ILBInUserError:  0,
	}

	for key, state := range im.l4ILBServiceLegacyMap {
		im.logger.V(6).Info("Got ILB Service", "serviceKey", key, "enabledGlobalAccess", fmt.Sprintf("%t", state.EnabledGlobalAccess),
			"enabledCustomSubnet", fmt.Sprintf("%t", state.EnabledCustomSubnet), "inSuccess", fmt.Sprintf("%t", state.InSuccess))
		counts[l4ILBService]++
		if !state.InSuccess {
			if state.IsUserError {
				counts[l4ILBInUserError]++
			} else {
				counts[l4ILBInError]++
			}
			// Skip counting other features if the service is in error state.
			continue
		}
		counts[l4ILBInSuccess]++
		if state.EnabledGlobalAccess {
			counts[l4ILBGlobalAccess]++
		}
		if state.EnabledCustomSubnet {
			counts[l4ILBCustomSubnet]++
		}
	}
	im.logger.V(4).Info("L4 ILB usage legacy metrics computed")
	return counts
}

// computeL4NetLBLegacyMetrics aggregates L4 NetLB metrics in the cache.
func (im *ControllerMetrics) computeL4NetLBLegacyMetrics() netLBFeatureCount {
	im.Lock()
	defer im.Unlock()
	im.logger.V(4).Info("Computing L4 NetLB usage legacy metrics from service state map", "serviceStateMap", im.l4NetLBServiceLegacyMap)
	var counts netLBFeatureCount

	for key, state := range im.l4NetLBServiceLegacyMap {
		im.logger.V(6).Info("NetLB Service", "serviceKey", key, "state", fmt.Sprintf("%+v", state))
		counts.service++
		if state.IsUserError {
			counts.inUserError++
			// Skip counting other features if the service is in error state.
			continue
		}
		if !state.InSuccess {
			if time.Since(*state.FirstSyncErrorTime) >= im.l4NetLBProvisionDeadlineForLegacyMetric {
				counts.inError++
			}
			// Skip counting other features if the service is in error state.
			continue
		}
		counts.success++
		if state.IsManagedIP {
			counts.managedStaticIP++
		}
		if state.IsPremiumTier {
			counts.premiumNetworkTier++
		}
	}
	im.logger.V(4).Info("L4 NetLB usage legacy metrics computed")
	return counts
}

func (im *ControllerMetrics) exportL4LegacyMetrics() {
	ilbCount := im.computeL4ILBLegacyMetrics()
	im.logger.V(3).Info("Exporting L4 ILB usage legacy metrics", "ilbCount", ilbCount)
	for feature, count := range ilbCount {
		l4ILBLegacyCount.With(prometheus.Labels{label: feature.String()}).Set(float64(count))
	}
	im.logger.V(3).Info("L4 ILB usage legacy metrics exported")

	netlbCount := im.computeL4NetLBLegacyMetrics()
	im.logger.V(3).Info("Exporting L4 NetLB usage legacy metrics", "netLBCount", netlbCount)
	netlbCount.record()
	im.logger.V(3).Info("L4 NetLB usage legacy metrics exported")
}
