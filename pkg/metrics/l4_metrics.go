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
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/klog/v2"
)

var (
	l4ILBCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "number_of_l4_ilbs",
			Help: "Number of L4 ILBs",
		},
		[]string{label},
	)
	l4NetLBCount = prometheus.NewGaugeVec(
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
	l4NetLBCount.With(prometheus.Labels{label: l4NetLBService.String()}).Set(float64(netlbCount.service))
	l4NetLBCount.With(prometheus.Labels{label: l4NetLBStaticIP.String()}).Set(float64(netlbCount.success))
	l4NetLBCount.With(prometheus.Labels{label: l4NetLBPremiumNetworkTier.String()}).Set(float64(netlbCount.premiumNetworkTier))
	l4NetLBCount.With(prometheus.Labels{label: l4NetLBManagedStaticIP.String()}).Set(float64(netlbCount.managedStaticIP))
	l4NetLBCount.With(prometheus.Labels{label: l4NetLBInSuccess.String()}).Set(float64(netlbCount.success))
	l4NetLBCount.With(prometheus.Labels{label: l4NetLBInUserError.String()}).Set(float64(netlbCount.inUserError))
	l4NetLBCount.With(prometheus.Labels{label: l4NetLBInError.String()}).Set(float64(netlbCount.inError))
}

// l4ControllerMetrics is a struct that holds L4 related metrics
type l4ControllerMetrics struct {
	sync.Mutex
	// l4ILBServiceMap is a map between service key and L4 ILB service state.
	l4ILBServiceMap map[string]L4ILBServiceState
	// l4NetLBServiceMap is a map between service key and L4 NetLB service state.
	l4NetLBServiceMap map[string]L4NetLBServiceState
	// l4NetLBProvisionDeadline is a time after which a failing NetLB will be marked as persistent error.
	l4NetLBProvisionDeadline time.Duration
}

func newL4ontrollerMetrics(l4NetLBProvisionDeadline time.Duration) *l4ControllerMetrics {
	return &l4ControllerMetrics{
		l4ILBServiceMap:          make(map[string]L4ILBServiceState),
		l4NetLBServiceMap:        make(map[string]L4NetLBServiceState),
		l4NetLBProvisionDeadline: l4NetLBProvisionDeadline,
	}
}

// SetL4ILBService implements L4ILBMetricsCollector.
func (im *ControllerMetrics) SetL4ILBService(svcKey string, state L4ILBServiceState) {
	im.Lock()
	defer im.Unlock()

	if im.l4ControllerMetrics.l4ILBServiceMap == nil {
		klog.Fatalf("Ingress Metrics failed to initialize correctly.")
	}
	im.l4ControllerMetrics.l4ILBServiceMap[svcKey] = state
}

// DeleteL4ILBService implements L4ILBMetricsCollector.
func (im *ControllerMetrics) DeleteL4ILBService(svcKey string) {
	im.Lock()
	defer im.Unlock()

	delete(im.l4ControllerMetrics.l4ILBServiceMap, svcKey)
}

// SetL4NetLBService adds metric state for given service to map.
func (im *ControllerMetrics) SetL4NetLBService(svcKey string, state L4NetLBServiceState) {
	im.Lock()
	defer im.Unlock()

	if im.l4ControllerMetrics.l4NetLBServiceMap == nil {
		klog.Fatalf("L4 Net LB Metrics failed to initialize correctly.")
	}

	if !state.InSuccess {
		if previousState, ok := im.l4ControllerMetrics.l4NetLBServiceMap[svcKey]; ok && previousState.FirstSyncErrorTime != nil {
			// If service is in Error state and retry timestamp was set then do not update it.
			state.FirstSyncErrorTime = previousState.FirstSyncErrorTime
		}
	}
	im.l4ControllerMetrics.l4NetLBServiceMap[svcKey] = state
}

// DeleteL4NetLBService deletes service from metrics map.
func (im *ControllerMetrics) DeleteL4NetLBService(svcKey string) {
	im.Lock()
	defer im.Unlock()

	delete(im.l4ControllerMetrics.l4NetLBServiceMap, svcKey)
}

// computeL4ILBMetrics aggregates L4 ILB metrics in the cache.
func (l4 *l4ControllerMetrics) computeL4ILBMetrics() map[feature]int {
	l4.Lock()
	defer l4.Unlock()
	klog.V(4).Infof("Computing L4 ILB usage metrics from service state map: %#v", l4.l4ILBServiceMap)
	counts := map[feature]int{
		l4ILBService:      0,
		l4ILBGlobalAccess: 0,
		l4ILBCustomSubnet: 0,
		l4ILBInSuccess:    0,
		l4ILBInError:      0,
		l4ILBInUserError:  0,
	}

	for key, state := range l4.l4ILBServiceMap {
		klog.V(6).Infof("ILB Service %s has EnabledGlobalAccess: %t, EnabledCustomSubnet: %t, InSuccess: %t", key, state.EnabledGlobalAccess, state.EnabledCustomSubnet, state.InSuccess)
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
	klog.V(4).Info("L4 ILB usage metrics computed.")
	return counts
}

// computeL4NetLBMetrics aggregates L4 NetLB metrics in the cache.
func (l4 *l4ControllerMetrics) computeL4NetLBMetrics() netLBFeatureCount {
	l4.Lock()
	defer l4.Unlock()
	klog.V(4).Infof("Computing L4 NetLB usage metrics from service state map: %#v", l4.l4NetLBServiceMap)
	var counts netLBFeatureCount

	for key, state := range l4.l4NetLBServiceMap {
		klog.V(6).Infof("NetLB Service %s has metrics %+v", key, state)
		counts.service++
		if state.IsUserError {
			counts.inUserError++
			// Skip counting other features if the service is in error state.
			continue
		}
		if !state.InSuccess {
			if time.Since(*state.FirstSyncErrorTime) >= l4.l4NetLBProvisionDeadline {
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
	klog.V(4).Info("L4 NetLB usage metrics computed.")
	return counts
}

func (l4 *l4ControllerMetrics) export() {
	ilbCount := l4.computeL4ILBMetrics()
	klog.V(3).Infof("Exporting L4 ILB usage metrics: %#v", ilbCount)
	for feature, count := range ilbCount {
		l4ILBCount.With(prometheus.Labels{label: feature.String()}).Set(float64(count))
	}
	klog.V(3).Infof("L4 ILB usage metrics exported.")

	netlbCount := l4.computeL4NetLBMetrics()
	klog.V(3).Infof("Exporting L4 NetLB usage metrics: %#v", netlbCount)
	netlbCount.record()
	klog.V(3).Infof("L4 NetLB usage metrics exported.")
}
