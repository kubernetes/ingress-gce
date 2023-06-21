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
	negControllerSubsystem = "neg_controller"

	resultSuccess = "success"
	resultError   = "error"

	GCProcess   = "GC"
	SyncProcess = "Sync"

	NotInDegradedEndpoints  = "not_in_degraded_endpoints"
	OnlyInDegradedEndpoints = "only_in_degraded_endpoints"

	gceServerError = "GCE_server_error"
	k8sServerError = "K8s_server_error"
	ignoredError   = "ignored_error"
	otherError     = "other_error"
	totalNegError  = "total_neg_error"

	// Classification of API Requests
	GetRequest            = "Get"
	CreateRequest         = "Create"
	DeleteRequest         = "Delete"
	UpdateRequest         = "Update"
	PatchRequest          = "Patch"
	ListRequest           = "List"
	AggregatedListRequest = "AggregatedList"
	AttachNERequest       = "AttachNE"
	DetachNERequest       = "Detach"
	ListNERequest         = "ListNE"
	ListNEHealthRequest   = "ListNEHealth"
)

type syncType string

var (
	NegOperationLatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Subsystem: negControllerSubsystem,
			Name:      "neg_operation_duration_seconds",
			Help:      "Latency of a NEG Operation",
			// custom buckets - [1s, 2s, 4s, 8s, 16s, 32s, 64s, 128s, 256s(~4min), 512s(~8min), 1024s(~17min), 2048 (~34min), 4096(~68min), +Inf]
			Buckets: prometheus.ExponentialBuckets(1, 2, 13),
		},
		[]string{
			"operation",   // endpoint operation
			"neg_type",    // type of neg
			"api_version", // GCE API version
			"result",      // result of the sync
		},
	)

	NegOperationEndpoints = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Subsystem: negControllerSubsystem,
			Name:      "neg_operation_endpoints",
			Help:      "Number of Endpoints during an NEG Operation",
			// custom buckets - [1, 2, 4, 8, 16, 32, 64, 128, 256, 512, 1024, 2048, 4096, +Inf]
			Buckets: prometheus.ExponentialBuckets(1, 2, 13),
		},
		[]string{
			"operation", // endpoint operation
			"neg_type",  // type of neg
			"result",    // result of the sync
		},
	)

	SyncerSyncLatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Subsystem: negControllerSubsystem,
			Name:      "syncer_sync_duration_seconds",
			Help:      "Sync latency for NEG Syncer",
			// custom buckets - [1s, 2s, 4s, 8s, 16s, 32s, 64s, 128s, 256s(~4min), 512s(~8min), 1024s(~17min), 2048 (~34min), 4096(~68min), +Inf]
			Buckets: prometheus.ExponentialBuckets(1, 2, 13),
		},
		[]string{
			"neg_type",                 //type of neg
			"endpoint_calculator_mode", // type of endpoint calculator used
			"result",                   // result of the sync
		},
	)

	ManagerProcessLatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Subsystem: negControllerSubsystem,
			Name:      "manager_process_duration_seconds",
			Help:      "Process latency for NEG Manager",
			// custom buckets - [1s, 2s, 4s, 8s, 16s, 32s, 64s, 128s, 256s(~4min), 512s(~8min), 1024s(~17min), 2048 (~34min), 4096(~68min), +Inf]
			Buckets: prometheus.ExponentialBuckets(1, 2, 13),
		},
		[]string{
			"process", // type of manager process loop
			"result",  // result of the process
		},
	)

	InitializationLatency = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Subsystem: negControllerSubsystem,
			Name:      "neg_initialization_duration_seconds",
			Help:      "Initialization latency of a NEG",
			// custom buckets - [1s, 2s, 4s, 8s, 16s, 32s, 64s, 128s, 256s(~4min), 512s(~8min), 1024s(~17min), 2048 (~34min), 4096(~68min), +Inf]
			Buckets: prometheus.ExponentialBuckets(1, 2, 13),
		},
	)

	LastSyncTimestamp = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Subsystem: negControllerSubsystem,
			Name:      "sync_timestamp",
			Help:      "The timestamp of the last execution of NEG controller sync loop.",
		},
	)

	// SyncerStaleness tracks for every syncer, how long since the syncer last syncs
	SyncerStaleness = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Subsystem: negControllerSubsystem,
			Name:      "syncer_staleness",
			Help:      "The duration of a syncer since it last syncs",
			// custom buckets - [1s, 2s, 4s, 8s, 16s, 32s, 64s, 128s, 256s(~4min), 512s(~8min), 1024s(~17min), 2048 (~34min), 4096(~68min), 8192(~136min), +Inf]
			Buckets: prometheus.ExponentialBuckets(1, 2, 14),
		},
	)

	// EPSStaleness tracks for every endpoint slice, how long since it was last processed
	EPSStaleness = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Subsystem: negControllerSubsystem,
			Name:      "endpointslice_staleness",
			Help:      "The duration for an endpoint slice since it was last processed by syncer",
			// custom buckets - [1s, 2s, 4s, 8s, 16s, 32s, 64s, 128s, 256s(~4min), 512s(~8min), 1024s(~17min), 2048 (~34min), 4096(~68min), 8192(~136min), +Inf]
			Buckets: prometheus.ExponentialBuckets(1, 2, 14),
		},
	)

	DegradeModeCorrectness = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Subsystem: negControllerSubsystem,
			Name:      "degraded_mode_correctness",
			Help:      "Number of endpoints differed between current endpoint calculation and degraded mode calculation",
			// custom buckets - [1, 2, 4, 8, 16, 32, 64, 128, 256, 512, 1024, 2048, 4096, 8192, 16384, 32768, 65536, 131072, 262144, 524288, +Inf]
			Buckets: prometheus.ExponentialBuckets(1, 2, 20),
		},
		[]string{
			"neg_type",      // type of neg
			"endpoint_type", // type of endpoint
		},
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

	LabelNumber = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Subsystem: negControllerSubsystem,
			Name:      "label_number_per_endpoint",
			Help:      "The number of labels per endpoint",
			// custom buckets - [1, 2, 4, 8, 16, 32, 64, 128, 256, 512, 1024, 2048, 4096, +Inf]
			Buckets: prometheus.ExponentialBuckets(1, 2, 13),
		},
	)

	AnnotationSize = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Subsystem: negControllerSubsystem,
			Name:      "annotation_size_per_endpoint",
			Help:      "The size in byte of endpoint annotations per endpoint",
			// custom buckets - [1, 2, 4, 8, 16, 32, 64, 128, 256, 512, 1024, 2048, 4096, +Inf]
			Buckets: prometheus.ExponentialBuckets(1, 2, 13),
		},
	)

	LabelPropagationError = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: negControllerSubsystem,
			Name:      "label_propagation_error_count",
			Help:      "the number of errors occurred for label propagation",
		},
		[]string{"error_type"},
	)

	// GCERequestCount tracks the number of GCE requests the neg controller sends to the NEG API
	GCERequestCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: negControllerSubsystem,
			Name:      "gce_request_count",
			Help:      "Number of requests sent by NEG Controller to Arcus.",
		},
		[]string{"request", "result"},
	)

	// GCERequestLatency tracks the latency of GCE requests the neg controller sends to the NEG API
	GCERequestLatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Subsystem: negControllerSubsystem,
			Name:      "gce_request_latency",
			Help:      "Observed request latency for requests sent by NEG Controller to Arcus.",
			// custom buckets - [0.001, 0.01, 0.1, 1, 2, 4, 8, 16, 32, 64, 128, 256, 512, 1024, 2048, 4096, 8192, 16384, 32768, 65536, 131072, 262144, 524288, +Inf]
			Buckets: append([]float64{0.001, 0.01, 0.1}, prometheus.ExponentialBuckets(1, 2, 20)...),
		},
		[]string{"request", "result"},
	)

	// K8sRequestCount tracks the number of K8s requests the neg controller sends to the K8s API
	K8sRequestCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: negControllerSubsystem,
			Name:      "k8s_request_count",
			Help:      "Number of requests sent by NEG Controller to Kubernetes API Server.",
		},
		[]string{"request", "result"},
	)

	// K8sRequestLatency tracks the latency of K8s requests the neg controller sends to the K8s API
	K8sRequestLatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Subsystem: negControllerSubsystem,
			Name:      "k8s_request_latency",
			Help:      "Observed request latency for requests sent by NEG Controller to Kubernetes API Server.",
			// custom buckets - [0.001, 0.01, 0.1, 1, 2, 4, 8, 16, 32, 64, 128, 256, 512, 1024, 2048, 4096, 8192, 16384, 32768, 65536, 131072, 262144, 524288, +Inf]
			Buckets: append([]float64{0.001, 0.01, 0.1}, prometheus.ExponentialBuckets(1, 2, 20)...),
		},
		[]string{"request", "result"},
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
		prometheus.MustRegister(LabelPropagationError)
		prometheus.MustRegister(LabelNumber)
		prometheus.MustRegister(AnnotationSize)
		prometheus.MustRegister(DegradeModeCorrectness)
		prometheus.MustRegister(NegControllerErrorCount)
		prometheus.MustRegister(GCERequestCount)
		prometheus.MustRegister(GCERequestLatency)
		prometheus.MustRegister(K8sRequestCount)
		prometheus.MustRegister(K8sRequestLatency)
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

// PublishLabelPropagationError publishes error occured during label propagation.
func PublishLabelPropagationError(errType string) {
	LabelPropagationError.WithLabelValues(errType).Inc()
}

// PublishAnnotationMetrics publishes collected metrics for endpoint annotations.
func PublishAnnotationMetrics(annotationSize int, labelNumber int) {
	AnnotationSize.Observe(float64(annotationSize))
	LabelNumber.Observe(float64(labelNumber))
}

// PublishGCERequestCountMetrics publishes collected metrics for GCE Request Counts
func PublishGCERequestCountMetrics(start time.Time, requestType string, err error) {
	var result string
	if err == nil {
		result = resultSuccess
	} else {
		if utils.IsGCEServerError(err) {
			result = gceServerError
		} else {
			result = otherError
		}
	}
	GCERequestLatency.WithLabelValues(requestType, result).Observe(time.Since(start).Seconds())
	GCERequestCount.WithLabelValues(requestType, result).Inc()
}

// PublishK8sRequestCountMetrics publishes collected metrics for K8s Request Counts
func PublishK8sRequestCountMetrics(start time.Time, requestType string, err error) {
	var result string
	if err == nil {
		result = resultSuccess
	} else {
		if utils.IsK8sServerError(err) {
			result = k8sServerError
		} else {
			result = otherError
		}
	}
	K8sRequestLatency.WithLabelValues(requestType, result).Observe(time.Since(start).Seconds())
	K8sRequestCount.WithLabelValues(requestType, result).Inc()
}

func getResult(err error) string {
	if err != nil {
		return resultError
	}
	return resultSuccess
}

func getErrorLabel(err error, isIgnored bool) string {
	if utils.IsGCEServerError(err) {
		return gceServerError
	}
	if utils.IsK8sServerError(err) {
		return k8sServerError
	}
	if isIgnored {
		return ignoredError
	}
	return otherError
}
