package metrics

import (
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

var (
	l4ILBDualStackCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "number_of_l4_dual_stack_ilbs",
			Help: "Number of L4 ILBs with DualStack enabled",
		},
		[]string{"ip_families", "ip_family_policy", "status"},
	)
	l4NetLBDualStackCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "number_of_l4_dual_stack_netlbs",
			Help: "Number of L4 NetLBs with DualStack enabled",
		},
		[]string{"ip_families", "ip_family_policy", "status"},
	)
)

func InitServiceDualStackMetricsState(svc *corev1.Service, startTime *time.Time) L4DualStackServiceState {
	state := L4DualStackServiceState{}

	state.IPFamilies = ipFamiliesToString(svc.Spec.IPFamilies)

	state.IPFamilyPolicy = ""
	if svc.Spec.IPFamilyPolicy != nil {
		state.IPFamilyPolicy = string(*svc.Spec.IPFamilyPolicy)
	}

	// Always init status with error, and update with Success when service was provisioned
	state.Status = StatusError
	state.FirstSyncErrorTime = startTime
	return state
}

func (im *ControllerMetrics) exportL4ILBDualStackMetrics() {
	// need to reset, otherwise metrics counted on previous exports will be still stored in a prometheus state
	l4ILBDualStackCount.Reset()

	ilbDualStackCount := im.computeL4ILBDualStackMetrics()
	klog.V(3).Infof("Exporting L4 ILB DualStack usage metrics: %#v", ilbDualStackCount)
	for state, count := range ilbDualStackCount {
		l4ILBDualStackCount.With(prometheus.Labels{
			"ip_families":      state.IPFamilies,
			"ip_family_policy": state.IPFamilyPolicy,
			"status":           string(state.Status),
		}).Set(float64(count))
	}
	klog.V(3).Infof("L4 ILB DualStack usage metrics exported.")
}

func (im *ControllerMetrics) exportL4NetLBDualStackMetrics() {
	// need to reset, otherwise metrics counted on previous exports will be still stored in a prometheus state
	l4NetLBDualStackCount.Reset()

	netlbDualStackCount := im.computeL4NetLBDualStackMetrics()
	klog.V(3).Infof("Exporting L4 NetLB DualStack usage metrics: %#v", netlbDualStackCount)
	for state, count := range netlbDualStackCount {
		l4NetLBDualStackCount.With(prometheus.Labels{
			"ip_families":      state.IPFamilies,
			"ip_family_policy": state.IPFamilyPolicy,
			"status":           string(state.Status),
		}).Set(float64(count))
	}
	klog.V(3).Infof("L4 Netlb DualStack usage metrics exported.")
}

// SetL4ILBDualStackService implements L4ILBMetricsCollector.
func (im *ControllerMetrics) SetL4ILBDualStackService(svcKey string, state L4DualStackServiceState) {
	im.Lock()
	defer im.Unlock()

	if im.l4ILBDualStackServiceMap == nil {
		klog.Fatalf("L4 ILB DualStack Metrics failed to initialize correctly.")
	}
	if state.Status == StatusError {
		if previousState, ok := im.l4ILBDualStackServiceMap[svcKey]; ok && previousState.FirstSyncErrorTime != nil {
			// If service is in Error state and retry timestamp was set then do not update it.
			state.FirstSyncErrorTime = previousState.FirstSyncErrorTime
		}
	}
	im.l4ILBDualStackServiceMap[svcKey] = state
}

// DeleteL4ILBDualStackService implements L4ILBMetricsCollector.
func (im *ControllerMetrics) DeleteL4ILBDualStackService(svcKey string) {
	im.Lock()
	defer im.Unlock()

	delete(im.l4ILBDualStackServiceMap, svcKey)
}

// SetL4NetLBDualStackService implements L4NetLBMetricsCollector.
func (im *ControllerMetrics) SetL4NetLBDualStackService(svcKey string, state L4DualStackServiceState) {
	im.Lock()
	defer im.Unlock()

	if im.l4NetLBDualStackServiceMap == nil {
		klog.Fatalf("L4 NetLB DualStack Metrics failed to initialize correctly.")
	}

	if state.Status == StatusError {
		if previousState, ok := im.l4NetLBDualStackServiceMap[svcKey]; ok && previousState.FirstSyncErrorTime != nil {
			// If service is in Error state and retry timestamp was set then do not update it.
			state.FirstSyncErrorTime = previousState.FirstSyncErrorTime
		}
	}
	im.l4NetLBDualStackServiceMap[svcKey] = state
}

// DeleteL4NetLBDualStackService implements L4NetLBMetricsCollector.
func (im *ControllerMetrics) DeleteL4NetLBDualStackService(svcKey string) {
	im.Lock()
	defer im.Unlock()

	delete(im.l4NetLBDualStackServiceMap, svcKey)
}

// computeL4ILBDualStackMetrics aggregates L4 ILB DualStack metrics in the cache.
func (im *ControllerMetrics) computeL4ILBDualStackMetrics() map[L4DualStackServiceLabels]int {
	im.Lock()
	defer im.Unlock()
	klog.V(4).Infof("Computing L4 DualStack ILB usage metrics from service state map: %#v", im.l4ILBDualStackServiceMap)
	counts := map[L4DualStackServiceLabels]int{}

	for key, state := range im.l4ILBDualStackServiceMap {
		klog.V(6).Infof("ILB Service %s has IPFamilies: %v, IPFamilyPolicy: %t, Status: %v", key, state.IPFamilies, state.IPFamilyPolicy, state.Status)
		if state.Status == StatusError &&
			state.FirstSyncErrorTime != nil &&
			time.Since(*state.FirstSyncErrorTime) >= persistentErrorThresholdTime {
			state.Status = StatusPersistentError
		}
		counts[state.L4DualStackServiceLabels]++
	}
	klog.V(4).Info("L4 ILB usage metrics computed.")
	return counts
}

// computeL4NetLBDualStackMetrics aggregates L4 NetLB DualStack metrics in the cache.
func (im *ControllerMetrics) computeL4NetLBDualStackMetrics() map[L4DualStackServiceLabels]int {
	im.Lock()
	defer im.Unlock()
	klog.V(4).Infof("Computing L4 DualStack NetLB usage metrics from service state map: %#v", im.l4NetLBDualStackServiceMap)
	counts := map[L4DualStackServiceLabels]int{}

	for key, state := range im.l4NetLBDualStackServiceMap {
		klog.V(6).Infof("NetLB Service %s has IPFamilies: %v, IPFamilyPolicy: %t, Status: %v", key, state.IPFamilies, state.IPFamilyPolicy, state.Status)
		if state.Status == StatusError &&
			state.FirstSyncErrorTime != nil &&
			time.Since(*state.FirstSyncErrorTime) >= persistentErrorThresholdTime {
			state.Status = StatusPersistentError
		}
		counts[state.L4DualStackServiceLabels]++
	}
	klog.V(4).Info("L4 NetLB usage metrics computed.")
	return counts
}

func ipFamiliesToString(ipFamilies []corev1.IPFamily) string {
	var ipFamiliesStrings []string
	for _, ipFamily := range ipFamilies {
		ipFamiliesStrings = append(ipFamiliesStrings, string(ipFamily))
	}
	return strings.Join(ipFamiliesStrings, ",")
}
