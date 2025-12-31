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
	"k8s.io/ingress-gce/pkg/forwardingrules"
	"k8s.io/klog/v2"
)

const (
	l4LabelStatus                = "status"
	l4LabelMultinet              = "multinet"
	l4LabelStrongSessionAffinity = "strong_session_affinity"
	l4LabelWeightedLBPodsPerNode = "weighted_lb_pods_per_node"
	l4LabelZonalAffinity         = "zonal_affinity"
	l4LabelBackendType           = "backend_type"
	l4LabelProtocol              = "protocol" // Either "TCP", "UDP", or "MIXED". The default "" means that there is an error.
	l4LabelLoggingEnabled        = "logging_enabled"
	labelDenyFirewall            = "deny_firewall"
)

var (
	l4ILBCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "l4_ilbs_count",
			Help: "Metric containing the number of ILBs that can be filtered by feature labels and status",
		},
		[]string{l4LabelStatus, l4LabelMultinet, l4LabelWeightedLBPodsPerNode, l4LabelZonalAffinity, l4LabelProtocol, l4LabelLoggingEnabled},
	)

	l4NetLBCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "l4_netlbs_count",
			Help: "Metric containing the number of NetLBs that can be filtered by feature labels and status",
		},
		[]string{l4LabelStatus, l4LabelMultinet, l4LabelStrongSessionAffinity, l4LabelWeightedLBPodsPerNode, l4LabelBackendType, l4LabelProtocol, l4LabelLoggingEnabled, labelDenyFirewall},
	)
)

func (c *Collector) exportIPv4() {
	c.exportL4ILBsMetrics()
	c.exportL4NetLBsMetrics()
}

func InitServiceMetricsState(svc *corev1.Service, startTime *time.Time, isMultinetwork bool, enabledStrongSessionAffinity bool, isWeightedLBPodsPerNode bool, isLBWithZonalAffinity bool, backendType L4BackendType) L4ServiceState {
	state := L4ServiceState{
		L4DualStackServiceLabels: L4DualStackServiceLabels{
			IPFamilies: ipFamiliesToString(svc.Spec.IPFamilies),
		},
		L4FeaturesServiceLabels: L4FeaturesServiceLabels{
			Multinetwork:          isMultinetwork,
			StrongSessionAffinity: enabledStrongSessionAffinity,
			WeightedLBPodsPerNode: isWeightedLBPodsPerNode,
			BackendType:           backendType,
			ZonalAffinity:         isLBWithZonalAffinity,
			Protocol:              protocolTypeFrom(svc),
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

func protocolTypeFrom(svc *corev1.Service) L4ProtocolType {
	switch {
	case forwardingrules.NeedsMixed(svc.Spec.Ports):
		return L4ProtocolTypeMixed
	case forwardingrules.NeedsUDP(svc.Spec.Ports):
		return L4ProtocolTypeUDP
	case forwardingrules.NeedsTCP(svc.Spec.Ports):
		return L4ProtocolTypeTCP
	}
	return L4ProtocolTypeUnknown
}

// SetL4ILBService adds L4 ILB service state to L4 NetLB Metrics states map.
func (c *Collector) SetL4ILBService(svcKey string, state L4ServiceState) {
	c.Lock()
	defer c.Unlock()

	if c.l4ILBServiceMap == nil {
		klog.Fatalf("L4 ILB Metrics failed to initialize correctly.")
	}
	if state.Status == StatusError {
		if previousState, ok := c.l4ILBServiceMap[svcKey]; ok && previousState.FirstSyncErrorTime != nil {
			// If service is in Error state and retry timestamp was set then do not update it.
			state.FirstSyncErrorTime = previousState.FirstSyncErrorTime
		}
	}
	c.l4ILBServiceMap[svcKey] = state
}

// DeleteL4ILBService deletes L4 ILB service state from L4 NetLB Metrics states map.
func (im *Collector) DeleteL4ILBService(svcKey string) {
	im.Lock()
	defer im.Unlock()

	delete(im.l4ILBServiceMap, svcKey)
}

// SetL4NetLBService adds L4 NetLB service state to L4 NetLB Metrics states map.
func (c *Collector) SetL4NetLBService(svcKey string, state L4ServiceState) {
	c.Lock()
	defer c.Unlock()

	if c.l4NetLBServiceMap == nil {
		klog.Fatalf("L4 NetLB Metrics failed to initialize correctly.")
	}

	if state.Status == StatusError {
		if previousState, ok := c.l4NetLBServiceMap[svcKey]; ok && previousState.FirstSyncErrorTime != nil {
			// If service is in Error state and retry timestamp was set then do not update it.
			state.FirstSyncErrorTime = previousState.FirstSyncErrorTime
		}
	}
	c.l4NetLBServiceMap[svcKey] = state
}

// DeleteL4NetLBService deletes L4 NetLB service state from L4 NetLB Metrics states map.
func (c *Collector) DeleteL4NetLBService(svcKey string) {
	c.Lock()
	defer c.Unlock()

	delete(c.l4NetLBServiceMap, svcKey)
}

func ipFamiliesToString(ipFamilies []corev1.IPFamily) string {
	var ipFamiliesStrings []string
	for _, ipFamily := range ipFamilies {
		ipFamiliesStrings = append(ipFamiliesStrings, string(ipFamily))
	}
	return strings.Join(ipFamiliesStrings, ",")
}

func (c *Collector) exportL4ILBsMetrics() {
	c.Lock()
	defer c.Unlock()
	c.logger.V(3).Info("Exporting L4 ILB usage metrics for services", "serviceCount", len(c.l4ILBServiceMap))
	l4ILBCount.Reset()
	for _, svcState := range c.l4ILBServiceMap {
		l4ILBCount.With(prometheus.Labels{
			l4LabelStatus:                string(getStatusConsideringPersistentError(&svcState)),
			l4LabelMultinet:              strconv.FormatBool(svcState.Multinetwork),
			l4LabelWeightedLBPodsPerNode: strconv.FormatBool(svcState.WeightedLBPodsPerNode),
			l4LabelZonalAffinity:         strconv.FormatBool(svcState.ZonalAffinity),
			l4LabelProtocol:              string(svcState.Protocol),
			l4LabelLoggingEnabled:        strconv.FormatBool(svcState.LoggingEnabled),
		}).Inc()
	}
	c.logger.V(3).Info("L4 ILB usage metrics exported")
}

func (c *Collector) exportL4NetLBsMetrics() {
	c.Lock()
	defer c.Unlock()
	c.logger.V(3).Info("Exporting L4 NetLB usage metrics for services", "serviceCount", len(c.l4NetLBServiceMap))
	l4NetLBCount.Reset()
	for _, svcState := range c.l4NetLBServiceMap {
		l4NetLBCount.With(prometheus.Labels{
			l4LabelStatus:                string(getStatusConsideringPersistentError(&svcState)),
			l4LabelMultinet:              strconv.FormatBool(svcState.Multinetwork),
			l4LabelStrongSessionAffinity: strconv.FormatBool(svcState.StrongSessionAffinity),
			l4LabelWeightedLBPodsPerNode: strconv.FormatBool(svcState.WeightedLBPodsPerNode),
			l4LabelBackendType:           string(svcState.BackendType),
			l4LabelProtocol:              string(svcState.Protocol),
			l4LabelLoggingEnabled:        strconv.FormatBool(svcState.LoggingEnabled),
			labelDenyFirewall:            string(svcState.DenyFirewallStatus),
		}).Inc()
	}
	c.logger.V(3).Info("L4 NetLB usage metrics exported")
}

func getStatusConsideringPersistentError(state *L4ServiceState) L4ServiceStatus {
	if state.Status == StatusError &&
		state.FirstSyncErrorTime != nil &&
		time.Since(*state.FirstSyncErrorTime) >= persistentErrorThresholdTime {
		return StatusPersistentError
	}
	return state.Status
}
