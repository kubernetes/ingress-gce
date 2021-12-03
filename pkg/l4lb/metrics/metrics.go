/*
Copyright 2021 The Kubernetes Authors.

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
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/klog"
)

const (
	statusSuccess = "success"
	statusError   = "error"
)

var (
	l4ILBSyncLatencyMetricsLabels = []string{
		"sync_result", // result of the sync
		"sync_type",   // whether this is a new service, update or delete
	}
	l4ILBSyncErrorMetricLabels = []string{
		"sync_type",    // whether this is a new service, update or delete
		"gce_resource", // The GCE resource whose update caused the error
		// max number of values for error_type = 18 k8s error reasons + 60 http status errors.
		// In production, we will see much fewer number, since many of the error codes are not applicable.
		"error_type", // what type of error it was
	}
	l4ILBSyncLatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "l4_ilb_sync_duration_seconds",
			Help: "Latency of an L4 ILB Sync",
			// custom buckets - [30s, 60s, 120s, 240s(4min), 480s(8min), 960s(16m), +Inf]
			Buckets: prometheus.ExponentialBuckets(30, 2, 6),
		},
		l4ILBSyncLatencyMetricsLabels,
	)
	l4ILBSyncErrorCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "l4_ilb_sync_error_count",
			Help: "Count of L4 ILB Sync errors",
		},
		l4ILBSyncErrorMetricLabels,
	)
)

// init registers l4 ilb sync metrics.
func init() {
	klog.V(3).Infof("Registering L4 ILB controller metrics %v, %v", l4ILBSyncLatency, l4ILBSyncErrorCount)
	prometheus.MustRegister(l4ILBSyncLatency, l4ILBSyncErrorCount)
}

// PublishL4ILBSyncMetrics exports metrics related to the L4 ILB sync.
func PublishILBSyncMetrics(success bool, syncType, gceResource, errType string, startTime time.Time) {
	publishL4ILBSyncLatency(success, syncType, startTime)
	if !success {
		publishL4ILBSyncErrorCount(syncType, gceResource, errType)
	}
}

// publishL4ILBSyncLatency exports the given sync latency datapoint.
func publishL4ILBSyncLatency(success bool, syncType string, startTime time.Time) {
	status := statusSuccess
	if !success {
		status = statusError
	}
	l4ILBSyncLatency.WithLabelValues(status, syncType).Observe(time.Since(startTime).Seconds())
}

// publishL4ILBSyncLatency exports the given sync latency datapoint.
func publishL4ILBSyncErrorCount(syncType, gceResource, errorType string) {
	l4ILBSyncErrorCount.WithLabelValues(syncType, gceResource, errorType).Inc()
}
