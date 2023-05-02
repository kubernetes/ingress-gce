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
	"k8s.io/ingress-gce/pkg/utils"
)

const (
	negControllerSubsystem     = "neg_controller"
	syncerLatencyKey           = "syncer_sync_duration_seconds"
	managerProcessLatencyKey   = "manager_process_duration_seconds"
	initLatencyKey             = "neg_initialization_duration_seconds"
	negOpLatencyKey            = "neg_operation_duration_seconds"
	negOpEndpointsKey          = "neg_operation_endpoints"
	lastSyncTimestampKey       = "sync_timestamp"
	syncerStalenessKey         = "syncer_staleness"
	epsStalenessKey            = "endpointslice_staleness"
	degradedModeCorrectnessKey = "degraded_mode_correctness"

	resultSuccess = "success"
	resultError   = "error"

	GCProcess   = "GC"
	SyncProcess = "Sync"

	NotInDegradedEndpoints  = "not_in_degraded_endpoints"
	OnlyInDegradedEndpoints = "only_in_degraded_endpoints"

	// Classification of endpoints within a NEG.
	ipv4EndpointType      = "IPv4"
	ipv6EndpointType      = "IPv6"
	dualStackEndpointType = "DualStack"
	migrationEndpointType = "Migration"

	gceServerError = "GCE_server_error"
	k8sServerError = "K8s_server_error"
	ignoredError   = "ignored_error"
	otherError     = "other_error"
	totalNegError  = "total_neg_error"
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

	degradedModeCorrectnessLabels = []string{
		"neg_type",      // type of neg
		"endpoint_type", // type of endpoint
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
			// custom buckets - [1, 2, 4, 8, 16, 32, 64, 128, 256, 512, 1024, 2048, 4096, +Inf]
			Buckets: prometheus.ExponentialBuckets(1, 2, 13),
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

	// SyncerStaleness tracks for every syncer, how long since the syncer last syncs
	SyncerStaleness = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Subsystem: negControllerSubsystem,
			Name:      syncerStalenessKey,
			Help:      "The duration of a syncer since it last syncs",
		},
	)

	// EPSStaleness tracks for every endpoint slice, how long since it was last processed
	EPSStaleness = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Subsystem: negControllerSubsystem,
			Name:      epsStalenessKey,
			Help:      "The duration for an endpoint slice since it was last processed by syncer",
			// custom buckets - [1s, 2s, 4s, 8s, 16s, 32s, 64s, 128s, 256s(~4min), 512s(~8min), 1024s(~17min), 2048 (~34min), 4096(~68min), 8192(~136min), +Inf]
			Buckets: prometheus.ExponentialBuckets(1, 2, 14),
		},
	)

	DegradeModeCorrectness = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Subsystem: negControllerSubsystem,
			Name:      degradedModeCorrectnessKey,
			Help:      "Number of endpoints differed between current endpoint calculation and degraded mode calculation",
			// custom buckets - [1, 2, 4, 8, 16, 32, 64, 128, 256, 512, 1024, 2048, 4096, 8192, 16384, 32768, 65536, 131072, 262144, 524288, +Inf]
			Buckets: prometheus.ExponentialBuckets(1, 2, 20),
		},
		degradedModeCorrectnessLabels,
	)

	DualStackMigrationFinishedDurations = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Subsystem: negControllerSubsystem,
			Name:      "dual_stack_migration_finished_durations_seconds",
			Help:      "Time taken to migrate all endpoints within all NEGs for a service port",
			// Buckets ~= [1s, 1.85s, 3.42s, 6s, 11s, 21s, 40s, 1m14s, 2m17s, 4m13s, 7m49s, 14m28s, 26m47s, 49m33s, 1h31m40s, 2h49m35s, 5h13m45s, 9h40m27s, +Inf]
			Buckets: prometheus.ExponentialBuckets(1, 1.85, 18),
		},
	)

	// A zero value for this metric means that there are no ongoing migrations.
	DualStackMigrationLongestUnfinishedDuration = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Subsystem: negControllerSubsystem,
			Name:      "dual_stack_migration_longest_unfinished_duration_seconds",
			Help:      "Longest time elapsed since a migration was started which hasn't yet completed",
		},
	)

	DualStackMigrationServiceCount = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Subsystem: negControllerSubsystem,
			Name:      "dual_stack_migration_service_count",
			Help:      "Number of Services which have migration endpoints",
		},
	)

	SyncerCountByEndpointType = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: negControllerSubsystem,
			Name:      "syncer_count_by_endpoint_type",
			Help:      "Number of Syncers managing NEGs containing endpoint of a particular kind",
		},
		[]string{"endpoint_type"},
	)

	// NegControllerErrorCount tracks the count of server errors(GCE/K8s) and
	// all errors from NEG controller.
	NegControllerErrorCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: negControllerSubsystem,
			Name:      "error_count",
			Help:      "Counts of server errors and NEG controller errors.",
		},
		[]string{"error_type"},
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
		prometheus.MustRegister(SyncerStaleness)
		prometheus.MustRegister(EPSStaleness)
		prometheus.MustRegister(NumberOfEndpoints)
		prometheus.MustRegister(LabelPropagationError)
		prometheus.MustRegister(LabelNumber)
		prometheus.MustRegister(AnnotationSize)
		prometheus.MustRegister(DegradeModeCorrectness)
		prometheus.MustRegister(DualStackMigrationFinishedDurations)
		prometheus.MustRegister(DualStackMigrationLongestUnfinishedDuration)
		prometheus.MustRegister(DualStackMigrationServiceCount)
		prometheus.MustRegister(SyncerCountByEndpointType)
		prometheus.MustRegister(NegControllerErrorCount)

		RegisterSyncerMetrics()
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

func PublishNegSyncerStalenessMetrics(syncerStaleness time.Duration) {
	SyncerStaleness.Observe(syncerStaleness.Seconds())
}

func PublishNegEPSStalenessMetrics(epsStaleness time.Duration) {
	EPSStaleness.Observe(epsStaleness.Seconds())
}

// PublishDegradedModeCorrectnessMetrics publishes collected metrics
// of the correctness of degraded mode calculations compared with the current one
func PublishDegradedModeCorrectnessMetrics(count int, endpointType string, negType string) {
	DegradeModeCorrectness.WithLabelValues(negType, endpointType).Observe(float64(count))
}

// PublishNegControllerErrorCountMetrics publishes collected metrics
// for neg controller errors.
func PublishNegControllerErrorCountMetrics(err error, isIgnored bool) {
	if err == nil {
		return
	}
	NegControllerErrorCount.WithLabelValues(totalNegError).Inc()
	NegControllerErrorCount.WithLabelValues(getErrorLabel(err, isIgnored)).Inc()
}

func getResult(err error) string {
	if err != nil {
		return resultError
	}
	return resultSuccess
}

func getErrorLabel(err error, isIgnored bool) string {
	if isIgnored {
		return ignoredError
	}
	if utils.IsGCEServerError(err) {
		return gceServerError
	}
	if utils.IsK8sServerError(err) {
		return k8sServerError
	}
	return otherError
}
