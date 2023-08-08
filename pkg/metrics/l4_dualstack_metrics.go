package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
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

func (im *ControllerMetrics) exportL4ILBDualStackMetrics() {
	// Do not report any dual stack information if it is not enabled.
	if !im.enableILBDualStack {
		return
	}
	im.Lock()
	defer im.Unlock()
	// need to reset, otherwise metrics counted on previous exports will be still stored in a prometheus state
	l4ILBDualStackCount.Reset()

	klog.V(3).Infof("Exporting L4 ILB DualStack usage metrics")

	for key, state := range im.l4ILBServiceMap {
		klog.V(6).Infof("ILB Service %s has IPFamilies: %v, IPFamilyPolicy: %t, Status: %v", key, state.IPFamilies, state.IPFamilyPolicy, state.Status)
		l4ILBDualStackCount.With(prometheus.Labels{
			"ip_families":      state.IPFamilies,
			"ip_family_policy": state.IPFamilyPolicy,
			"status":           string(getStatusConsideringPersistentError(&state)),
		}).Inc()
	}

	klog.V(3).Infof("L4 ILB DualStack usage metrics exported.")
}

func (im *ControllerMetrics) exportL4NetLBDualStackMetrics() {
	// Do not report any dual stack information if it is not enabled.
	if !im.enableNetLBDualStack {
		return
	}
	im.Lock()
	defer im.Unlock()
	// need to reset, otherwise metrics counted on previous exports will be still stored in a prometheus state
	l4NetLBDualStackCount.Reset()

	klog.V(3).Infof("Exporting L4 NetLB DualStack usage metrics")

	for key, state := range im.l4NetLBServiceMap {
		klog.V(6).Infof("NetLB Service %s has IPFamilies: %v, IPFamilyPolicy: %t, Status: %v", key, state.IPFamilies, state.IPFamilyPolicy, state.Status)
		l4NetLBDualStackCount.With(prometheus.Labels{
			"ip_families":      state.IPFamilies,
			"ip_family_policy": state.IPFamilyPolicy,
			"status":           string(getStatusConsideringPersistentError(&state)),
		}).Inc()
	}
	klog.V(3).Infof("L4 Netlb DualStack usage metrics exported.")
}
