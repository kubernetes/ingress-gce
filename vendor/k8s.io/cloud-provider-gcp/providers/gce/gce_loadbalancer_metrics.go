//go:build !providerless
// +build !providerless

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

package gce

import (
	"strconv"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/component-base/metrics"
	"k8s.io/component-base/metrics/legacyregistry"
	"k8s.io/klog/v2"
)

const (
	label = "feature"
)

var (
	metricsInterval = 10 * time.Minute
	l4ILBCount      = metrics.NewGaugeVec(
		&metrics.GaugeOpts{
			Name:           "number_of_l4_ilbs",
			Help:           "Number of L4 ILBs",
			StabilityLevel: metrics.ALPHA,
		},
		[]string{label},
	)
	l4NetLBCount = metrics.NewGaugeVec(
		&metrics.GaugeOpts{
			Name: "number_of_l4_netlbs",
			Help: "Metric containing the number of NetLBs that can be filtered by feature labels and status",
		},
		[]string{"status", "deny_firewall"},
	)
	ccmFeatureGateInfo = metrics.NewGaugeVec(
		&metrics.GaugeOpts{
			Name:           "ccm_feature_gate_info",
			Help:           "Information about GKE Cloud Controller Manager feature gates.",
			StabilityLevel: metrics.ALPHA,
		},
		[]string{"name", "enabled"},
	)
)

// init registers L4 internal loadbalancer usage metrics.
func init() {
	klog.V(3).Infof("Registering Service Controller loadbalancer usage metrics %v", l4ILBCount)
	legacyregistry.MustRegister(l4ILBCount)
	klog.V(3).Infof("Registering Service Controller loadbalancer usage metrics %v", l4NetLBCount)
	legacyregistry.MustRegister(l4NetLBCount)
	klog.V(3).Infof("Registering CCM feature gate info metrics %v", ccmFeatureGateInfo)
	legacyregistry.MustRegister(ccmFeatureGateInfo)
}

// RecordFeatureGateMetrics records the status of feature gates at CCM startup.
func RecordFeatureGateMetrics(enableFineGrainedLocks bool) {
	ccmFeatureGateInfo.WithLabelValues("finegrainedlock", strconv.FormatBool(enableFineGrainedLocks)).Set(1.0)
}

// LoadBalancerMetrics is a cache that contains loadbalancer service resource
// states for computing usage metrics.
type LoadBalancerMetrics struct {
	// l4ILBServiceMap is a map of service key and L4 ILB service state.
	l4ILBServiceMap map[string]L4ILBServiceState
	l4NetLBMap      map[string]L4NetLBServiceState

	sync.Mutex
}

type feature string

func (f feature) String() string {
	return string(f)
}

const (
	l4ILBService      = feature("L4ILBService")
	l4ILBGlobalAccess = feature("L4ILBGlobalAccess")
	l4ILBCustomSubnet = feature("L4ILBCustomSubnet")
	// l4ILBInSuccess feature specifies that ILB VIP is configured.
	l4ILBInSuccess = feature("L4ILBInSuccess")
	// l4ILBInInError feature specifies that an error had occurred for this service
	// in ensureInternalLoadbalancer method.
	l4ILBInError = feature("L4ILBInError")
)

// L4ILBServiceState contains Internal Loadbalancer feature states as specified
// in k8s Service.
type L4ILBServiceState struct {
	// EnabledGlobalAccess specifies if Global Access is enabled.
	EnabledGlobalAccess bool
	// EnabledCustomSubNet specifies if Custom Subnet is enabled.
	EnabledCustomSubnet bool
	// InSuccess specifies if the ILB service VIP is configured.
	InSuccess bool
}

// loadbalancerMetricsCollector is an interface to update/delete L4 loadbalancer
// states in the cache that is used for computing L4 Loadbalancer usage metrics.
type loadbalancerMetricsCollector interface {
	// Run starts a goroutine to compute and export metrics a periodic interval.
	Run(stopCh <-chan struct{})
	// SetL4ILBService adds/updates L4 ILB service state for given service key.
	SetL4ILBService(svcKey string, state L4ILBServiceState)
	// DeleteL4ILBService removes the given L4 ILB service key.
	DeleteL4ILBService(svcKey string)
	// SetL4NetLBService adds/updates L4 NetLB service state for given service key.
	SetL4NetLBService(svcKey string, state L4NetLBServiceState)
	// DeleteL4NetLBService removes the given L4 NetLB service key.
	DeleteL4NetLBService(svcKey string)
}

// newLoadBalancerMetrics initializes LoadBalancerMetrics and starts a goroutine
// to compute and export metrics periodically.
func newLoadBalancerMetrics() loadbalancerMetricsCollector {
	return &LoadBalancerMetrics{
		l4ILBServiceMap: make(map[string]L4ILBServiceState),
		l4NetLBMap:      make(map[string]L4NetLBServiceState),
	}
}

// Run implements loadbalancerMetricsCollector.
func (lm *LoadBalancerMetrics) Run(stopCh <-chan struct{}) {
	klog.V(3).Infof("Loadbalancer Metrics initialized. Metrics will be exported at an interval of %v", metricsInterval)
	// Compute and export metrics periodically.
	go func() {
		// Wait for service states to be populated in the cache before computing metrics.
		time.Sleep(metricsInterval)
		wait.Until(lm.export, metricsInterval, stopCh)
	}()
	<-stopCh
}

// SetL4ILBService implements loadbalancerMetricsCollector.
func (lm *LoadBalancerMetrics) SetL4ILBService(svcKey string, state L4ILBServiceState) {
	lm.Lock()
	defer lm.Unlock()

	if lm.l4ILBServiceMap == nil {
		klog.Fatalf("Loadbalancer Metrics failed to initialize correctly.")
	}
	lm.l4ILBServiceMap[svcKey] = state
}

// DeleteL4ILBService implements loadbalancerMetricsCollector.
func (lm *LoadBalancerMetrics) DeleteL4ILBService(svcKey string) {
	lm.Lock()
	defer lm.Unlock()

	delete(lm.l4ILBServiceMap, svcKey)
}

// export computes and exports loadbalancer usage metrics.
func (lm *LoadBalancerMetrics) export() {
	lm.exportILBMetrics()
	lm.exportNetLBMetrics()
}

func (lm *LoadBalancerMetrics) exportILBMetrics() {
	ilbCount := lm.computeL4ILBMetrics()
	klog.V(5).Infof("Exporting L4 ILB usage metrics: %#v", ilbCount)
	for feature, count := range ilbCount {
		l4ILBCount.With(map[string]string{label: feature.String()}).Set(float64(count))
	}
	klog.V(5).Infof("L4 ILB usage metrics exported.")
}

// computeL4ILBMetrics aggregates L4 ILB metrics in the cache.
func (lm *LoadBalancerMetrics) computeL4ILBMetrics() map[feature]int {
	lm.Lock()
	defer lm.Unlock()
	klog.V(4).Infof("Computing L4 ILB usage metrics from service state map: %#v", lm.l4ILBServiceMap)
	counts := map[feature]int{
		l4ILBService:      0,
		l4ILBGlobalAccess: 0,
		l4ILBCustomSubnet: 0,
		l4ILBInSuccess:    0,
		l4ILBInError:      0,
	}

	for key, state := range lm.l4ILBServiceMap {
		klog.V(6).Infof("ILB Service %s has EnabledGlobalAccess: %t, EnabledCustomSubnet: %t, InSuccess: %t", key, state.EnabledGlobalAccess, state.EnabledCustomSubnet, state.InSuccess)
		counts[l4ILBService]++
		if !state.InSuccess {
			counts[l4ILBInError]++
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

// L4ServiceStatus denotes the status of the service
type L4ServiceStatus string

// L4ServiceStatus denotes the status of the service
const (
	StatusSuccess         = L4ServiceStatus("Success")
	StatusUserError       = L4ServiceStatus("UserError")
	StatusError           = L4ServiceStatus("Error")
	StatusPersistentError = L4ServiceStatus("PersistentError")
)

// DenyFirewallStatus represents IP stack used when the deny firewalls are provisioned.
type DenyFirewallStatus string

// DenyFirewallStatus represents IP stack used when the deny firewalls are provisioned.
const (
	DenyFirewallStatusUnknown  = DenyFirewallStatus("UNKNOWN")  // Shouldn't happen, but if it does something is wrong.
	DenyFirewallStatusNone     = DenyFirewallStatus("")         // Case when no firewalls have been provisioned yet or when the feature has not been enabled explicitly
	DenyFirewallStatusDisabled = DenyFirewallStatus("DISABLED") // Case to mark when the feature has been enabled then explicitly disabled - for example when the feature is rolled back
	DenyFirewallStatusIPv4     = DenyFirewallStatus("IPv4")
)

type L4NetLBServiceState struct {
	Status       L4ServiceStatus
	DenyFirewall DenyFirewallStatus
}

// SetL4NetLBService patches information about L4 NetLB
func (lm *LoadBalancerMetrics) SetL4NetLBService(svcKey string, state L4NetLBServiceState) {
	lm.Lock()
	defer lm.Unlock()

	lm.l4NetLBMap[svcKey] = state
}

// DeleteL4NetLBService removes the given L4 NetLB service key.
func (lm *LoadBalancerMetrics) DeleteL4NetLBService(svcKey string) {
	lm.Lock()
	defer lm.Unlock()

	delete(lm.l4NetLBMap, svcKey)
}

// exportNetLBMetrics computes and exports loadbalancer usage metrics.
func (lm *LoadBalancerMetrics) exportNetLBMetrics() {
	lm.Lock()
	defer lm.Unlock()

	klog.Info("Exporting L4 NetLB usage metrics for services", "serviceCount", len(lm.l4NetLBMap))

	l4NetLBCount.Reset()
	for _, svcState := range lm.l4NetLBMap {
		l4NetLBCount.WithLabelValues(string(svcState.Status), string(svcState.DenyFirewall)).Inc()
	}
	klog.Info("L4 NetLB usage metrics exported")
}
