package metrics

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
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

func (c *Collector) exportDualStack() {
	c.exportL4ILBDualStackMetrics()
	c.exportL4NetLBDualStackMetrics()
}

func (im *Collector) exportL4ILBDualStackMetrics() {
	// Do not report any dual stack information if it is not enabled.
	if !im.enableILBDualStack {
		return
	}
	im.Lock()
	defer im.Unlock()
	// need to reset, otherwise metrics counted on previous exports will be still stored in a prometheus state
	l4ILBDualStackCount.Reset()

	im.logger.V(3).Info("Exporting L4 ILB DualStack usage metrics")

	for key, state := range im.l4ILBServiceMap {
		im.logger.V(6).Info("Got ILB Service with IPFamilies, IPFamilyPolicy and Status",
			"serviceKey", key, "ipFamilies", state.IPFamilies, "ipFamilyPolicy", fmt.Sprintf("%T", state.IPFamilyPolicy), "status", state.Status)
		l4ILBDualStackCount.With(prometheus.Labels{
			"ip_families":      state.IPFamilies,
			"ip_family_policy": state.IPFamilyPolicy,
			"status":           string(getStatusConsideringPersistentError(&state)),
		}).Inc()
	}

	im.logger.V(3).Info("L4 ILB DualStack usage metrics exported")
}

func (im *Collector) exportL4NetLBDualStackMetrics() {
	// Do not report any dual stack information if it is not enabled.
	if !im.enableNetLBDualStack {
		return
	}
	im.Lock()
	defer im.Unlock()
	// need to reset, otherwise metrics counted on previous exports will be still stored in a prometheus state
	l4NetLBDualStackCount.Reset()

	im.logger.V(3).Info("Exporting L4 NetLB DualStack usage metrics")

	for key, state := range im.l4NetLBServiceMap {
		im.logger.V(6).Info("Got NetLB Service with IPFamilies, IPFamilyPolicy and Status",
			"serviceKey", key, "ipFamilies", state.IPFamilies, "ipFamilyPolicy", state.IPFamilyPolicy, "status", state.Status)
		l4NetLBDualStackCount.With(prometheus.Labels{
			"ip_families":      state.IPFamilies,
			"ip_family_policy": state.IPFamilyPolicy,
			"status":           string(getStatusConsideringPersistentError(&state)),
		}).Inc()
	}
	im.logger.V(3).Info("L4 NetLB DualStack usage metrics exported")
}
