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
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

const (
	l4LabelStatus   = "status"
	l4LabelMultinet = "multinet"
)

var (
	l4ILBCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "l4_ilbs_count",
			Help: "Metric containing the number of ILBs that can be filtered by feature labels and status",
		},
		[]string{l4LabelStatus, l4LabelMultinet},
	)

	l4NetLBCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "l4_netlbs_count",
			Help: "Metric containing the number of NetLBs that can be filtered by feature labels and status",
		},
		[]string{l4LabelStatus, l4LabelMultinet},
	)
)

func (im *ControllerMetrics) exportL4Metrics() {
	im.exportL4ILBsMetrics()
	im.exportL4NetLBsMetrics()
}

func InitServiceMetricsState(svc *corev1.Service, startTime *time.Time, isMultinetwork bool) L4ServiceState {
	state := L4ServiceState{
		L4DualStackServiceLabels: L4DualStackServiceLabels{
			IPFamilies: ipFamiliesToString(svc.Spec.IPFamilies),
		},
		L4FeaturesServiceLabels: L4FeaturesServiceLabels{
			Multinetwork: isMultinetwork,
		},
		// Always init status with error, and update with Success when service was provisioned
		Status:             StatusError,
		FirstSyncErrorTime: startTime,
	}
	if svc.Spec.IPFamilyPolicy != nil {
		state.IPFamilyPolicy = string(*svc.Spec.IPFamilyPolicy)
	}

	return state
}

// SetL4ILBService adds L4 ILB service state to L4 NetLB Metrics states map.
func (im *ControllerMetrics) SetL4ILBService(svcKey string, state L4ServiceState) {
	im.Lock()
	defer im.Unlock()

	if im.l4ILBServiceMap == nil {
		klog.Fatalf("L4 ILB Metrics failed to initialize correctly.")
	}
	if state.Status == StatusError {
		if previousState, ok := im.l4ILBServiceMap[svcKey]; ok && previousState.FirstSyncErrorTime != nil {
			// If service is in Error state and retry timestamp was set then do not update it.
			state.FirstSyncErrorTime = previousState.FirstSyncErrorTime
		}
	}
	im.l4ILBServiceMap[svcKey] = state
}

// DeleteL4ILBService deletes L4 ILB service state from L4 NetLB Metrics states map.
func (im *ControllerMetrics) DeleteL4ILBService(svcKey string) {
	im.Lock()
	defer im.Unlock()

	delete(im.l4ILBServiceMap, svcKey)
}

// SetL4NetLBService adds L4 NetLB service state to L4 NetLB Metrics states map.
func (im *ControllerMetrics) SetL4NetLBService(svcKey string, state L4ServiceState) {
	im.Lock()
	defer im.Unlock()

	if im.l4NetLBServiceMap == nil {
		klog.Fatalf("L4 NetLB Metrics failed to initialize correctly.")
	}

	if state.Status == StatusError {
		if previousState, ok := im.l4NetLBServiceMap[svcKey]; ok && previousState.FirstSyncErrorTime != nil {
			// If service is in Error state and retry timestamp was set then do not update it.
			state.FirstSyncErrorTime = previousState.FirstSyncErrorTime
		}
	}
	im.l4NetLBServiceMap[svcKey] = state
}

// DeleteL4NetLBService deletes L4 NetLB service state from L4 NetLB Metrics states map.
func (im *ControllerMetrics) DeleteL4NetLBService(svcKey string) {
	im.Lock()
	defer im.Unlock()

	delete(im.l4NetLBServiceMap, svcKey)
}

func ipFamiliesToString(ipFamilies []corev1.IPFamily) string {
	var ipFamiliesStrings []string
	for _, ipFamily := range ipFamilies {
		ipFamiliesStrings = append(ipFamiliesStrings, string(ipFamily))
	}
	return strings.Join(ipFamiliesStrings, ",")
}

func (im *ControllerMetrics) exportL4ILBsMetrics() {
	im.Lock()
	defer im.Unlock()
	klog.V(3).Infof("Exporting L4 ILB usage metrics for %d services.", len(im.l4ILBServiceMap))
	l4ILBCount.Reset()
	for _, svcState := range im.l4ILBServiceMap {
		l4ILBCount.With(prometheus.Labels{
			l4LabelStatus:   string(getStatusConsideringPersistentError(&svcState)),
			l4LabelMultinet: strconv.FormatBool(svcState.Multinetwork),
		}).Inc()
	}
	klog.V(3).Infof("L4 ILB usage metrics exported.")
}

func (im *ControllerMetrics) exportL4NetLBsMetrics() {
	im.Lock()
	defer im.Unlock()
	klog.V(3).Infof("Exporting L4 NetLB usage metrics for %d services.", len(im.l4NetLBServiceMap))
	l4NetLBCount.Reset()
	for _, svcState := range im.l4NetLBServiceMap {
		l4NetLBCount.With(prometheus.Labels{
			l4LabelStatus:   string(getStatusConsideringPersistentError(&svcState)),
			l4LabelMultinet: strconv.FormatBool(svcState.Multinetwork),
		}).Inc()
	}
	klog.V(3).Infof("L4 NetLB usage metrics exported.")
}

func getStatusConsideringPersistentError(state *L4ServiceState) L4ServiceStatus {
	if state.Status == StatusError &&
		state.FirstSyncErrorTime != nil &&
		time.Since(*state.FirstSyncErrorTime) >= persistentErrorThresholdTime {
		return StatusPersistentError
	}
	return state.Status
}
