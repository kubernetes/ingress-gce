/*
Copyright 2018 The Kubernetes Authors.

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
)

const (
	negControllerSubsystem   = "neg_controller"
	syncerLatencyKey         = "syncer_sync_duration_seconds"
	managerProcessLatencyKey = "manager_process_duration_seconds"
	initLatencyKey           = "neg_initialization_duration_seconds"
	negOpLatencyKey          = "neg_operation_duration_seconds"
	negOpEndpointsKey        = "neg_operation_endpoints"
	lastSyncTimestampKey     = "sync_timestamp"

	resultSuccess = "success"
	resultError   = "error"

	GCProcess   = "GC"
	SyncProcess = "Sync"
)

type syncType string

var (
	negOpLatencyMetricsLabels = []string{
		"operation",   // endpoint operation
		"neg_type",    // type of neg
		"api_version", // GCE API version
		"result",      // result of the sync
	}

	negOpEndpointsMetricsLabels = []string{
		"operation", // endpoint operation
		"neg_type",  // type of neg
		"result",    // result of the sync
	}

	negProcessMetricsLabels = []string{
		"process", // type of manager process loop
		"result",  // result of the process
	}

	syncerMetricsLabels = []string{
		"neg_type",                 //type of neg
		"endpoint_calculator_mode", // type of endpoint calculator used
		"result",                   // result of the sync
	}

	NegOperationLatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Subsystem: negControllerSubsystem,
			Name:      negOpLatencyKey,
			Help:      "Latency of a NEG Operation",
			// custom buckets - [1s, 2s, 4s, 8s, 16s, 32s, 64s, 128s, 256s(~4min), 512s(~8min), 1024s(~17min), 2048 (~34min), 4096(~68min), +Inf]
			Buckets: prometheus.ExponentialBuckets(1, 2, 13),
		},
		negOpLatencyMetricsLabels,
	)

	NegOperationEndpoints = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Subsystem: negControllerSubsystem,
			Name:      negOpEndpointsKey,
			Help:      "Number of Endpoints during an NEG Operation",
		},
		negOpEndpointsMetricsLabels,
	)

	SyncerSyncLatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Subsystem: negControllerSubsystem,
			Name:      syncerLatencyKey,
			Help:      "Sync latency for NEG Syncer",
			// custom buckets - [1s, 2s, 4s, 8s, 16s, 32s, 64s, 128s, 256s(~4min), 512s(~8min), 1024s(~17min), 2048 (~34min), 4096(~68min), +Inf]
			Buckets: prometheus.ExponentialBuckets(1, 2, 13),
		},
		syncerMetricsLabels,
	)

	ManagerProcessLatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Subsystem: negControllerSubsystem,
			Name:      managerProcessLatencyKey,
			Help:      "Process latency for NEG Manager",
			// custom buckets - [1s, 2s, 4s, 8s, 16s, 32s, 64s, 128s, 256s(~4min), 512s(~8min), 1024s(~17min), 2048 (~34min), 4096(~68min), +Inf]
			Buckets: prometheus.ExponentialBuckets(1, 2, 13),
		},
		negProcessMetricsLabels,
	)

	InitializationLatency = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Subsystem: negControllerSubsystem,
			Name:      initLatencyKey,
			Help:      "Initialization latency of a NEG",
			// custom buckets - [1s, 2s, 4s, 8s, 16s, 32s, 64s, 128s, 256s(~4min), 512s(~8min), 1024s(~17min), 2048 (~34min), 4096(~68min), +Inf]
			Buckets: prometheus.ExponentialBuckets(1, 2, 13),
		},
	)

	LastSyncTimestamp = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Subsystem: negControllerSubsystem,
			Name:      lastSyncTimestampKey,
			Help:      "The timestamp of the last execution of NEG controller sync loop.",
		},
	)
)

var register sync.Once

func RegisterMetrics() {
	register.Do(func() {
		prometheus.MustRegister(NegOperationLatency)
		prometheus.MustRegister(NegOperationEndpoints)
		prometheus.MustRegister(ManagerProcessLatency)
		prometheus.MustRegister(SyncerSyncLatency)
		prometheus.MustRegister(LastSyncTimestamp)
		prometheus.MustRegister(InitializationLatency)
	})
}

// PublishNegOperationMetrics publishes collected metrics for neg operations
func PublishNegOperationMetrics(operation, negType, apiVersion string, err error, numEndpoints int, start time.Time) {
	result := getResult(err)

	NegOperationLatency.WithLabelValues(operation, negType, apiVersion, result).Observe(time.Since(start).Seconds())
	NegOperationEndpoints.WithLabelValues(operation, negType, result).Observe(float64(numEndpoints))
}

// PublishNegSyncMetrics publishes collected metrics for the sync of NEG
func PublishNegSyncMetrics(negType, endpointCalculator string, err error, start time.Time) {
	result := getResult(err)

	SyncerSyncLatency.WithLabelValues(negType, endpointCalculator, result).Observe(time.Since(start).Seconds())
}

// PublishNegManagerProcessMetrics publishes collected metrics for the neg manager loops
func PublishNegManagerProcessMetrics(process string, err error, start time.Time) {
	result := getResult(err)
	ManagerProcessLatency.WithLabelValues(process, result).Observe(time.Since(start).Seconds())
}

// PublishNegInitializationMetrics publishes collected metrics for time from request to initialization of NEG
func PublishNegInitializationMetrics(latency time.Duration) {
	InitializationLatency.Observe(latency.Seconds())
}

func getResult(err error) string {
	if err != nil {
		return resultError
	}
	return resultSuccess
}
