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
	"k8s.io/klog/v2"
)

const (
	statusSuccess                                  = "success"
	statusError                                    = "error"
	L4ilbLatencyMetricName                         = "l4_ilb_sync_duration_seconds"
	L4ilbErrorMetricName                           = "l4_ilb_sync_error_count"
	L4netlbLatencyMetricName                       = "l4_netlb_sync_duration_seconds"
	L4netlbErrorMetricName                         = "l4_netlb_sync_error_count"
	L4netlbLegacyToRBSMigrationPreventedMetricName = "l4_netlb_legacy_to_rbs_migration_prevented_count"
	l4failedHealthCheckName                        = "l4_failed_healthcheck_count"
	l4ControllerHealthCheckName                    = "l4_controller_healthcheck"
)

var (
	l4LBSyncLatencyMetricsLabels = []string{
		"sync_result", // result of the sync
		"sync_type",   // whether this is a new service, update or delete
	}
	l4LBSyncErrorMetricLabels = []string{
		"sync_type",    // whether this is a new service, update or delete
		"gce_resource", // The GCE resource whose update caused the error
		// max number of values for error_type = 18 k8s error reasons + 60 http status errors.
		// In production, we will see much fewer number, since many of the error codes are not applicable.
		"error_type", // what type of error it was
	}
	l4ILBSyncLatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: L4ilbLatencyMetricName,
			Help: "Latency of an L4 ILB Sync",
			// custom buckets - [0.9375s, 1.875s, 3.75s, 7.5s, 30s, 60s, 120s, 240s(4min), 480s(8min), 960s(16m), 3840s(64min), 7680s(128m) +Inf]
			// using funny starter bucket, 0.9375s will only add buckets to existing metric, this is a safe operation in most time series db
			Buckets: prometheus.ExponentialBuckets(0.9375, 2, 12),
		},
		l4LBSyncLatencyMetricsLabels,
	)
	l4ILBSyncErrorCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: L4ilbErrorMetricName,
			Help: "Count of L4 ILB Sync errors",
		},
		l4LBSyncErrorMetricLabels,
	)
	// l4ILBSyncLatency is a metric that represents the time spent processing L4NetLB service.
	// The metric is labeled with synchronization type and its result.
	l4NetLBSyncLatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: L4netlbLatencyMetricName,
			Help: "Latency of an L4 NetLB Sync",
			// custom buckets - [0.9375s, 1.875s, 3.75s, 7.5s, 30s, 60s, 120s, 240s(4min), 480s(8min), 960s(16m), 3840s(64min), 7680s(128m) +Inf]
			// using funny starter bucket, 0.9375s will only add buckets to existing metric, this is a safe operation in most time series db
			Buckets: prometheus.ExponentialBuckets(0.9375, 2, 12),
		},
		l4LBSyncLatencyMetricsLabels,
	)
	// l4NetLBSyncErrorCount is a metric that counts number of L4NetLB services in Error state.
	// The metric is labeled with synchronization type, the type of error and the name of gce resource that is in error.
	l4NetLBSyncErrorCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: L4netlbErrorMetricName,
			Help: "Count of L4 NetLB Sync errors",
		},
		l4LBSyncErrorMetricLabels,
	)
	l4FailedHealthCheckCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: l4failedHealthCheckName,
			Help: "Count l4 controller healthcheck failures",
		},
		[]string{"controller_name"},
	)
	l4ControllerHealthCheck = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: l4ControllerHealthCheckName,
			Help: "Count l4 controller healthcheck",
		},
		[]string{"controller_name", "status"},
	)
	l4NetLBLegacyToRBSPrevented = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: L4netlbLegacyToRBSMigrationPreventedMetricName,
			Help: "Count of times legacy to rbs migration was prevented",
		},
		[]string{"type"}, // currently, can be migration or race
	)
)

// init registers l4 ilb and netlb sync metrics.
func init() {
	klog.V(3).Infof("Registering L4 ILB controller metrics %v, %v", l4ILBSyncLatency, l4ILBSyncErrorCount)
	prometheus.MustRegister(l4ILBSyncLatency, l4ILBSyncErrorCount)
	klog.V(3).Infof("Registering L4 NetLB controller metrics %v, %v", l4NetLBSyncLatency, l4NetLBSyncErrorCount)
	prometheus.MustRegister(l4NetLBSyncLatency, l4NetLBSyncErrorCount)
	klog.V(3).Infof("Registering L4 healthcheck failures count metric: %v", l4FailedHealthCheckCount)
	prometheus.MustRegister(l4FailedHealthCheckCount)
	klog.V(3).Infof("Registering L4 controller healthcheck metric: %v", l4ControllerHealthCheck)
	prometheus.MustRegister(l4ControllerHealthCheck)
}

// PublishILBSyncMetrics exports metrics related to the L4 ILB sync.
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

// PublishL4NetLBSyncSuccess exports latency metrics for L4 NetLB service after successful sync.
func PublishL4NetLBSyncSuccess(syncType string, startTime time.Time) {
	l4NetLBSyncLatency.WithLabelValues(statusSuccess, syncType).Observe(time.Since(startTime).Seconds())
}

// PublishL4NetLBSyncError exports latency and error count metrics for L4 NetLB after error sync.
func PublishL4NetLBSyncError(syncType, gceResource, errType string, startTime time.Time) {
	l4NetLBSyncLatency.WithLabelValues(statusError, syncType).Observe(time.Since(startTime).Seconds())
	l4NetLBSyncErrorCount.WithLabelValues(syncType, gceResource, errType).Inc()
}

// PublishL4FailedHealthCheckCount observers failed health check from controller.
func PublishL4FailedHealthCheckCount(controllerName string) {
	l4FailedHealthCheckCount.WithLabelValues(controllerName).Inc()
}

type L4ControllerHealthCheckStatus string

const ControllerHealthyStatus = L4ControllerHealthCheckStatus("Healthy")
const ControllerUnhealthyStatus = L4ControllerHealthCheckStatus("Unhealthy")

// PublishL4ControllerHealthCheckStatus stores health state of the controller.
func PublishL4ControllerHealthCheckStatus(controllerName string, status L4ControllerHealthCheckStatus) {
	l4ControllerHealthCheck.WithLabelValues(controllerName, string(status)).Inc()
}

// IncreaseL4NetLBLegacyToRBSMigrationAttempts increases l4NetLBLegacyToRBSPrevented metric for stopped migration
func IncreaseL4NetLBLegacyToRBSMigrationAttempts() {
	l4NetLBLegacyToRBSPrevented.WithLabelValues("migration").Inc()
}

// IncreaseL4NetLBTargetPoolRaceWithRBS increases l4NetLBLegacyToRBSPrevented metric for race condition between controllers
func IncreaseL4NetLBTargetPoolRaceWithRBS() {
	l4NetLBLegacyToRBSPrevented.WithLabelValues("race").Inc()
}
