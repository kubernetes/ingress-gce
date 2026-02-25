package metrics

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func TestPublishILBSyncMetrics(t *testing.T) {
	// No t.Parallel(), those are global variables
	l4ILBSyncLatency.Reset()
	l4ILBSyncErrorCount.Reset()

	testValues := []struct {
		success                 bool
		syncType                string
		gceResource             string
		errType                 string
		isResync                bool
		isWeightedLB            bool
		protocol                L4ProtocolType
		isZonalAffinityLB       bool
		isLoggingControlEnabled bool
	}{
		{success: true, syncType: "new", isResync: false, isWeightedLB: true, protocol: L4ProtocolTypeMixed, isZonalAffinityLB: false, isLoggingControlEnabled: true},
		{success: true, syncType: "update", isResync: false, isWeightedLB: false, protocol: L4ProtocolTypeUDP, isZonalAffinityLB: true, isLoggingControlEnabled: false},
		{success: true, syncType: "update", isResync: true, isWeightedLB: true, protocol: L4ProtocolTypeUDP, isZonalAffinityLB: false, isLoggingControlEnabled: true},
		{success: true, protocol: L4ProtocolTypeTCP, isLoggingControlEnabled: false},
		{success: true, syncType: "delete", isResync: false, isWeightedLB: false, protocol: L4ProtocolTypeTCP, isZonalAffinityLB: true, isLoggingControlEnabled: true},
		{success: false, syncType: "delete", protocol: L4ProtocolTypeTCP, errType: http.StatusText(http.StatusNotFound), gceResource: "BackendService", isLoggingControlEnabled: false},
		{success: false, syncType: "create", protocol: L4ProtocolTypeTCP, errType: http.StatusText(http.StatusInternalServerError), gceResource: "ForwardingRule"},
		{success: false, syncType: "update", protocol: L4ProtocolTypeMixed, errType: "k8s Forbidden"},
	}

	wantDurationsCount := []struct {
		labels prometheus.Labels
		count  int
	}{
		{
			labels: prometheus.Labels{
				"sync_result": "success",
			},
			count: 5,
		},
		{
			labels: prometheus.Labels{
				"sync_result": "error",
			},
			count: 3,
		},
		{
			labels: prometheus.Labels{
				"protocol": "TCP",
			},
			count: 4,
		},
		{
			labels: prometheus.Labels{
				"protocol":                     "MIXED",
				"sync_result":                  "success",
				"periodic_resync":              "false",
				"sync_type":                    "new",
				"l4_weighted_lb_pods_per_node": "true",
				"zonal_affinity":               "false",
			},
			count: 1,
		},
		{
			labels: prometheus.Labels{
				"protocol":  "UDP",
				"sync_type": "update",
			},
			count: 2,
		},
		{
			labels: prometheus.Labels{
				"zonal_affinity": "true",
			},
			count: 2,
		},
		{
			labels: prometheus.Labels{
				"logging_control_enabled": "true",
			},
			count: 3,
		},
		{
			labels: prometheus.Labels{
				"logging_control_enabled": "false",
			},
			count: 5,
		},
	}

	wantErrors := []struct {
		labels prometheus.Labels
		count  int
	}{
		{
			labels: prometheus.Labels{},
			count:  3,
		},
		{
			labels: prometheus.Labels{
				"error_type": "k8s Forbidden",
			},
			count: 1,
		},
		{
			labels: prometheus.Labels{
				"protocol": "TCP",
			},
			count: 2,
		},
		{
			labels: prometheus.Labels{
				"protocol": "MIXED",
			},
			count: 1,
		},
		{
			labels: prometheus.Labels{
				"protocol": "UDP",
			},
			count: 0,
		},
	}

	// Act
	for _, v := range testValues {
		PublishILBSyncMetrics(v.success, v.syncType, v.gceResource, v.errType, time.Now(), v.isResync, v.isWeightedLB, v.protocol, v.isZonalAffinityLB, v.isLoggingControlEnabled)
	}

	// Assert
	t.Run("l4_ilb_sync_duration_seconds", func(t *testing.T) {
		for _, want := range wantDurationsCount {
			got, err := countHistogram(l4ILBSyncLatency, want.labels)
			if err != nil {
				t.Errorf("failed to fetch count for %v: %v", want.labels, err)
				continue
			}

			if want.count != got {
				t.Errorf("for %v expected %d but got %d", want.labels, want.count, got)
			}
		}
	})

	t.Run("l4_ilb_sync_error_count", func(t *testing.T) {
		for _, want := range wantErrors {
			got, err := countCounter(l4ILBSyncErrorCount, want.labels)
			if err != nil {
				t.Errorf("failed to fetch count for %v: %v", want.labels, err)
				continue
			}

			if want.count != got {
				t.Errorf("for %v expected %d but got %d", want.labels, want.count, got)
			}
		}
	})
}

func TestPublishNetLBSyncMetrics(t *testing.T) {
	// No t.Parallel(), those are global variables
	l4NetLBSyncLatency.Reset()
	l4NetLBSyncErrorCount.Reset()

	testValues := []struct {
		success                 bool
		syncType                string
		gceResource             string
		errType                 string
		isResync                bool
		isWeightedLB            bool
		protocol                L4ProtocolType
		backendType             L4BackendType
		isLoggingControlEnabled bool
	}{
		{success: true, syncType: "new", isResync: false, isWeightedLB: true, protocol: L4ProtocolTypeMixed, backendType: L4BackendTypeNEG, isLoggingControlEnabled: true},
		{success: true, syncType: "update", isResync: false, isWeightedLB: false, protocol: L4ProtocolTypeUDP, backendType: L4BackendTypeInstanceGroup, isLoggingControlEnabled: false},
		{success: true, syncType: "update", isResync: true, isWeightedLB: true, protocol: L4ProtocolTypeUDP, backendType: L4BackendTypeNEG, isLoggingControlEnabled: true},
		{success: true, protocol: L4ProtocolTypeTCP, backendType: L4BackendTypeNEG, isLoggingControlEnabled: false},
		{success: true, syncType: "delete", isResync: false, isWeightedLB: false, protocol: L4ProtocolTypeTCP, backendType: L4BackendTypeInstanceGroup},
		{success: false, syncType: "delete", protocol: L4ProtocolTypeTCP, errType: http.StatusText(http.StatusNotFound), gceResource: "BackendService"},
		{success: false, syncType: "create", protocol: L4ProtocolTypeTCP, errType: http.StatusText(http.StatusInternalServerError), gceResource: "ForwardingRule"},
		{success: false, syncType: "update", protocol: L4ProtocolTypeMixed, errType: "k8s Forbidden"},
	}

	wantDurationsCount := []struct {
		labels prometheus.Labels
		count  int
	}{
		{
			labels: prometheus.Labels{
				"sync_result": "success",
			},
			count: 5,
		},
		{
			labels: prometheus.Labels{
				"sync_result": "error",
			},
			count: 3,
		},
		{
			labels: prometheus.Labels{
				"protocol": "TCP",
			},
			count: 4,
		},
		{
			labels: prometheus.Labels{
				"protocol":                     "MIXED",
				"sync_result":                  "success",
				"periodic_resync":              "false",
				"sync_type":                    "new",
				"l4_weighted_lb_pods_per_node": "true",
				"backend_type":                 "NEG",
			},
			count: 1,
		},
		{
			labels: prometheus.Labels{
				"protocol":  "UDP",
				"sync_type": "update",
			},
			count: 2,
		},
		{
			labels: prometheus.Labels{
				"backend_type": "NEG",
			},
			count: 3,
		},
		{
			labels: prometheus.Labels{
				"logging_control_enabled": "true",
			},
			count: 2,
		},
		{
			labels: prometheus.Labels{
				"logging_control_enabled": "false",
			},
			count: 6,
		},
	}

	wantErrors := []struct {
		labels prometheus.Labels
		count  int
	}{
		{
			labels: prometheus.Labels{},
			count:  3,
		},
		{
			labels: prometheus.Labels{
				"error_type": "k8s Forbidden",
			},
			count: 1,
		},
		{
			labels: prometheus.Labels{
				"protocol": "TCP",
			},
			count: 2,
		},
		{
			labels: prometheus.Labels{
				"protocol": "MIXED",
			},
			count: 1,
		},
		{
			labels: prometheus.Labels{
				"protocol": "UDP",
			},
			count: 0,
		},
	}

	// Act
	for _, v := range testValues {
		PublishNetLBSyncMetrics(v.success, v.syncType, v.gceResource, v.errType, time.Now(), v.isResync, v.isWeightedLB, v.protocol, v.backendType, v.isLoggingControlEnabled)
	}

	// Assert
	t.Run("l4_netlb_sync_duration_seconds", func(t *testing.T) {
		for _, want := range wantDurationsCount {
			got, err := countHistogram(l4NetLBSyncLatency, want.labels)
			if err != nil {
				t.Errorf("failed to fetch count for %v: %v", want.labels, err)
				continue
			}

			if want.count != got {
				t.Errorf("for %v expected %d but got %d", want.labels, want.count, got)
			}
		}
	})

	t.Run("l4_netlb_sync_error_count", func(t *testing.T) {
		for _, want := range wantErrors {
			got, err := countCounter(l4NetLBSyncErrorCount, want.labels)
			if err != nil {
				t.Errorf("failed to fetch count for %v: %v", want.labels, err)
				continue
			}

			if want.count != got {
				t.Errorf("for %v expected %d but got %d", want.labels, want.count, got)
			}
		}
	})

}

// countHistogram retrieves the total SampleCount for histograms in the Vec
// whose labels are a superset of and match the values in 'labelsToMatch'.
func countHistogram(h *prometheus.HistogramVec, labelsToMatch prometheus.Labels) (int, error) {
	metricChan := make(chan prometheus.Metric)
	go func() {
		h.Collect(metricChan)
		close(metricChan)
	}()

	totalCount := 0

	for metric := range metricChan {
		dtoMetric := &dto.Metric{}
		if err := metric.Write(dtoMetric); err != nil {
			return 0, fmt.Errorf("failed to write metric DTO: %v", err)
		}

		if !matchLabels(dtoMetric, labelsToMatch) {
			continue
		}

		if dtoMetric.GetHistogram() != nil {
			totalCount += int(dtoMetric.GetHistogram().GetSampleCount())
		}
	}
	return totalCount, nil
}

// countCounter retrieves the total value for counters in the Vec
// whose labels are a superset of and match the values in 'labelsToMatch'.
func countCounter(c *prometheus.CounterVec, labelsToMatch prometheus.Labels) (int, error) {
	metricChan := make(chan prometheus.Metric)
	go func() {
		c.Collect(metricChan)
		close(metricChan)
	}()

	totalValue := 0.0

	for metric := range metricChan {
		dtoMetric := &dto.Metric{}
		if err := metric.Write(dtoMetric); err != nil {
			return 0, fmt.Errorf("failed to write metric DTO: %v", err)
		}

		if !matchLabels(dtoMetric, labelsToMatch) {
			continue
		}

		if dtoMetric.GetCounter() != nil {
			totalValue += dtoMetric.GetCounter().GetValue()
		}
	}
	return int(totalValue), nil
}

// matchLabels checks if the labels in dtoMetric are a superset of and match labelsToMatch.
func matchLabels(dtoMetric *dto.Metric, labelsToMatch prometheus.Labels) bool {
	metricLabels := make(map[string]string)
	for _, lp := range dtoMetric.GetLabel() {
		metricLabels[lp.GetName()] = lp.GetValue()
	}

	for labelName, labelValue := range labelsToMatch {
		val, ok := metricLabels[labelName]
		if !ok || val != labelValue {
			return false
		}
	}
	return true
}
