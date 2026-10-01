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
	"fmt"
	"time"

	"github.com/GoogleCloudPlatform/gke-enterprise-mt/pkg/mtmetrics"
	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog/v2"
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

func init() {
	// PublishLastSyncTimestamp is event-driven (called when a service or node/topology
	// item is dequeued from the NEG controller workqueue, rather than on a periodic ticker).
	// StrategyMax is used so that a single idle tenant with no service/endpoint churn does
	// not pin the global /metrics gauge to a stale timestamp and trigger false-positive
	// staleness alerts. Per-tenant sync timestamps remain observable via /metrics/multitenancy.
	mtmetrics.DefaultGlobalTracker.SetAggregationStrategy("neg_controller_sync_timestamp", mtmetrics.StrategyMax)
}

// NegMetrics holds the Prometheus metrics for the NEG controller.
// Instances must be initialized via NewNegMetrics, NewNegMetricsWithFactory, or FakeNegMetrics,
// which guarantees all metric fields are non-nil so Publish* methods do not need nil checks.
type NegMetrics struct {
	negOperationLatency     mtmetrics.ObserverVec
	negOperationEndpoints   mtmetrics.ObserverVec
	syncerSyncLatency       mtmetrics.ObserverVec
	managerProcessLatency   mtmetrics.ObserverVec
	initializationLatency   prometheus.Histogram
	lastSyncTimestamp       prometheus.Gauge
	syncerStaleness         prometheus.Histogram
	epsStaleness            prometheus.Histogram
	degradeModeCorrectness  mtmetrics.ObserverVec
	negControllerErrorCount mtmetrics.CounterVec
	labelNumber             prometheus.Histogram
	annotationSize          prometheus.Histogram
	labelPropagationError   mtmetrics.CounterVec
	gceRequestCount         mtmetrics.CounterVec
	gceRequestLatency       mtmetrics.ObserverVec
	k8sRequestCount         mtmetrics.CounterVec
	k8sRequestLatency       mtmetrics.ObserverVec
}

// NewNegMetrics creates a NegMetrics using the default Prometheus registerer.
func NewNegMetrics() (*NegMetrics, error) {
	return NewNegMetricsWithFactory(mtmetrics.NewStdMetricFactory(prometheus.DefaultRegisterer))
}

// FakeNegMetrics creates a NegMetrics with an isolated Prometheus registry for testing.
func FakeNegMetrics() *NegMetrics {
	m, err := NewNegMetricsWithFactory(mtmetrics.NewStdMetricFactory(prometheus.NewRegistry()))
	if err != nil {
		klog.Errorf("Failed to initialize fake NegMetrics: %v", err)
	}
	return m
}

func NewNegMetricsWithFactory(factory mtmetrics.MetricFactory) (*NegMetrics, error) {
	var err error
	m := &NegMetrics{}

	if m.negOperationLatency, err = factory.NewHistogramVec(
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
	); err != nil {
		return nil, fmt.Errorf("failed to create negOperationLatency: %w", err)
	}

	if m.negOperationEndpoints, err = factory.NewHistogramVec(
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
	); err != nil {
		return nil, fmt.Errorf("failed to create negOperationEndpoints: %w", err)
	}

	if m.syncerSyncLatency, err = factory.NewHistogramVec(
		prometheus.HistogramOpts{
			Subsystem: negControllerSubsystem,
			Name:      "syncer_sync_duration_seconds",
			Help:      "Sync latency for NEG Syncer",
			// custom buckets - [1s, 2s, 4s, 8s, 16s, 32s, 64s, 128s, 256s(~4min), 512s(~8min), 1024s(~17min), 2048 (~34min), 4096(~68min), +Inf]
			Buckets: prometheus.ExponentialBuckets(1, 2, 13),
		},
		[]string{
			"neg_type",                 // type of neg
			"endpoint_calculator_mode", // type of endpoint calculator used
			"result",                   // result of the sync
		},
	); err != nil {
		return nil, fmt.Errorf("failed to create syncerSyncLatency: %w", err)
	}

	if m.managerProcessLatency, err = factory.NewHistogramVec(
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
	); err != nil {
		return nil, fmt.Errorf("failed to create managerProcessLatency: %w", err)
	}

	if m.initializationLatency, err = factory.NewHistogram(
		prometheus.HistogramOpts{
			Subsystem: negControllerSubsystem,
			Name:      "neg_initialization_duration_seconds",
			Help:      "Initialization latency of a NEG",
			// custom buckets - [1s, 2s, 4s, 8s, 16s, 32s, 64s, 128s, 256s(~4min), 512s(~8min), 1024s(~17min), 2048 (~34min), 4096(~68min), +Inf]
			Buckets: prometheus.ExponentialBuckets(1, 2, 13),
		},
	); err != nil {
		return nil, fmt.Errorf("failed to create initializationLatency: %w", err)
	}

	if m.lastSyncTimestamp, err = factory.NewGauge(
		prometheus.GaugeOpts{
			Subsystem: negControllerSubsystem,
			Name:      "sync_timestamp",
			Help:      "The timestamp of the last execution of NEG controller sync loop.",
		},
	); err != nil {
		return nil, fmt.Errorf("failed to create lastSyncTimestamp: %w", err)
	}

	// SyncerStaleness tracks for every syncer, how long since the syncer last syncs
	if m.syncerStaleness, err = factory.NewHistogram(
		prometheus.HistogramOpts{
			Subsystem: negControllerSubsystem,
			Name:      "syncer_staleness",
			Help:      "The duration of a syncer since it last syncs",
			// custom buckets - [1s, 2s, 4s, 8s, 16s, 32s, 64s, 128s, 256s(~4min), 512s(~8min), 1024s(~17min), 2048 (~34min), 4096(~68min), 8192(~136min), +Inf]
			Buckets: prometheus.ExponentialBuckets(1, 2, 14),
		},
	); err != nil {
		return nil, fmt.Errorf("failed to create syncerStaleness: %w", err)
	}

	// EPSStaleness tracks for every endpoint slice, how long since it was last processed
	if m.epsStaleness, err = factory.NewHistogram(
		prometheus.HistogramOpts{
			Subsystem: negControllerSubsystem,
			Name:      "endpointslice_staleness",
			Help:      "The duration for an endpoint slice since it was last processed by syncer",
			// custom buckets - [1s, 2s, 4s, 8s, 16s, 32s, 64s, 128s, 256s(~4min), 512s(~8min), 1024s(~17min), 2048 (~34min), 4096(~68min), 8192(~136min), +Inf]
			Buckets: prometheus.ExponentialBuckets(1, 2, 14),
		},
	); err != nil {
		return nil, fmt.Errorf("failed to create epsStaleness: %w", err)
	}

	if m.degradeModeCorrectness, err = factory.NewHistogramVec(
		prometheus.HistogramOpts{
			Subsystem: negControllerSubsystem,
			Name:      "degraded_mode_correctness",
			Help:      "Number of endpoints differed between current endpoint calculation and degraded mode calculation",
			// custom buckets - [0, 1, 2, 4, 8, 16, 32, 64, 128, 256, 512, 1024, 2048, 4096, 8192, 16384, 32768, 65536, 131072, 262144, 524288, +Inf]
			Buckets: append([]float64{0}, prometheus.ExponentialBuckets(1, 2, 20)...),
		},
		[]string{
			"neg_type",      // type of neg
			"endpoint_type", // type of endpoint
		},
	); err != nil {
		return nil, fmt.Errorf("failed to create degradeModeCorrectness: %w", err)
	}

	// NegControllerErrorCount tracks the count of server errors(GCE/K8s) and
	// all errors from NEG controller.
	if m.negControllerErrorCount, err = factory.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: negControllerSubsystem,
			Name:      "error_count",
			Help:      "Counts of server errors and NEG controller errors.",
		},
		[]string{"error_type"},
	); err != nil {
		return nil, fmt.Errorf("failed to create negControllerErrorCount: %w", err)
	}

	if m.labelNumber, err = factory.NewHistogram(
		prometheus.HistogramOpts{
			Subsystem: negControllerSubsystem,
			Name:      "label_number_per_endpoint",
			Help:      "The number of labels per endpoint",
			// custom buckets - [1, 2, 4, 8, 16, 32, 64, 128, 256, 512, 1024, 2048, 4096, +Inf]
			Buckets: prometheus.ExponentialBuckets(1, 2, 13),
		},
	); err != nil {
		return nil, fmt.Errorf("failed to create labelNumber: %w", err)
	}

	if m.annotationSize, err = factory.NewHistogram(
		prometheus.HistogramOpts{
			Subsystem: negControllerSubsystem,
			Name:      "annotation_size_per_endpoint",
			Help:      "The size in byte of endpoint annotations per endpoint",
			// custom buckets - [1, 2, 4, 8, 16, 32, 64, 128, 256, 512, 1024, 2048, 4096, +Inf]
			Buckets: prometheus.ExponentialBuckets(1, 2, 13),
		},
	); err != nil {
		return nil, fmt.Errorf("failed to create annotationSize: %w", err)
	}

	if m.labelPropagationError, err = factory.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: negControllerSubsystem,
			Name:      "label_propagation_error_count",
			Help:      "the number of errors occurred for label propagation",
		},
		[]string{"error_type"},
	); err != nil {
		return nil, fmt.Errorf("failed to create labelPropagationError: %w", err)
	}

	// GCERequestCount tracks the number of GCE requests the neg controller sends to the NEG API
	if m.gceRequestCount, err = factory.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: negControllerSubsystem,
			Name:      "gce_request_count",
			Help:      "Number of requests sent by NEG Controller to Arcus.",
		},
		[]string{"request", "result"},
	); err != nil {
		return nil, fmt.Errorf("failed to create gceRequestCount: %w", err)
	}

	// GCERequestLatency tracks the latency of GCE requests the neg controller sends to the NEG API
	if m.gceRequestLatency, err = factory.NewHistogramVec(
		prometheus.HistogramOpts{
			Subsystem: negControllerSubsystem,
			Name:      "gce_request_latency",
			Help:      "Observed request latency for requests sent by NEG Controller to Arcus.",
			// custom buckets - [0.001, 0.01, 0.1, 1, 2, 4, 8, 16, 32, 64, 128, 256, 512, 1024, 2048, 4096, 8192, 16384, 32768, 65536, 131072, 262144, 524288, +Inf]
			Buckets: append([]float64{0.001, 0.01, 0.1}, prometheus.ExponentialBuckets(1, 2, 20)...),
		},
		[]string{"request", "result"},
	); err != nil {
		return nil, fmt.Errorf("failed to create gceRequestLatency: %w", err)
	}

	// K8sRequestCount tracks the number of K8s requests the neg controller sends to the K8s API
	if m.k8sRequestCount, err = factory.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: negControllerSubsystem,
			Name:      "k8s_request_count",
			Help:      "Number of requests sent by NEG Controller to Kubernetes API Server.",
		},
		[]string{"request", "result"},
	); err != nil {
		return nil, fmt.Errorf("failed to create k8sRequestCount: %w", err)
	}

	// K8sRequestLatency tracks the latency of K8s requests the neg controller sends to the K8s API
	if m.k8sRequestLatency, err = factory.NewHistogramVec(
		prometheus.HistogramOpts{
			Subsystem: negControllerSubsystem,
			Name:      "k8s_request_latency",
			Help:      "Observed request latency for requests sent by NEG Controller to Kubernetes API Server.",
			// custom buckets - [0.001, 0.01, 0.1, 1, 2, 4, 8, 16, 32, 64, 128, 256, 512, 1024, 2048, 4096, 8192, 16384, 32768, 65536, 131072, 262144, 524288, +Inf]
			Buckets: append([]float64{0.001, 0.01, 0.1}, prometheus.ExponentialBuckets(1, 2, 20)...),
		},
		[]string{"request", "result"},
	); err != nil {
		return nil, fmt.Errorf("failed to create k8sRequestLatency: %w", err)
	}

	return m, nil
}

// PublishNegOperationMetrics publishes collected metrics for neg operations
func (m *NegMetrics) PublishNegOperationMetrics(operation, negType, apiVersion string, err error, numEndpoints int, start time.Time) {
	result := getResult(err)

	m.negOperationLatency.WithLabelValues(operation, negType, apiVersion, result).Observe(time.Since(start).Seconds())
	m.negOperationEndpoints.WithLabelValues(operation, negType, result).Observe(float64(numEndpoints))
}

// PublishNegSyncMetrics publishes collected metrics for the sync of NEG
func (m *NegMetrics) PublishNegSyncMetrics(negType, endpointCalculator string, err error, start time.Time) {
	result := getResult(err)

	m.syncerSyncLatency.WithLabelValues(negType, endpointCalculator, result).Observe(time.Since(start).Seconds())
}

// PublishNegManagerProcessMetrics publishes collected metrics for the neg manager loops
func (m *NegMetrics) PublishNegManagerProcessMetrics(process string, err error, start time.Time) {
	result := getResult(err)
	m.managerProcessLatency.WithLabelValues(process, result).Observe(time.Since(start).Seconds())
}

// PublishNegInitializationMetrics publishes collected metrics for time from request to initialization of NEG
func (m *NegMetrics) PublishNegInitializationMetrics(latency time.Duration) {
	m.initializationLatency.Observe(latency.Seconds())
}

func (m *NegMetrics) PublishNegSyncerStalenessMetrics(syncerStaleness time.Duration) {
	m.syncerStaleness.Observe(syncerStaleness.Seconds())
}

func (m *NegMetrics) PublishNegEPSStalenessMetrics(epsStaleness time.Duration) {
	m.epsStaleness.Observe(epsStaleness.Seconds())
}

// PublishDegradedModeCorrectnessMetrics publishes collected metrics
// of the correctness of degraded mode calculations compared with the current one
func (m *NegMetrics) PublishDegradedModeCorrectnessMetrics(count int, endpointType string, negType string) {
	m.degradeModeCorrectness.WithLabelValues(negType, endpointType).Observe(float64(count))
}

// PublishNegControllerErrorCountMetrics publishes collected metrics
// for neg controller errors.
func (m *NegMetrics) PublishNegControllerErrorCountMetrics(err error, isIgnored bool) {
	if err == nil {
		return
	}
	m.negControllerErrorCount.WithLabelValues(totalNegError).Inc()
	m.negControllerErrorCount.WithLabelValues(getErrorLabel(err, isIgnored)).Inc()
}

// PublishLabelPropagationError publishes error occurred during label propagation.
func (m *NegMetrics) PublishLabelPropagationError(errType string) {
	m.labelPropagationError.WithLabelValues(errType).Inc()
}

// PublishAnnotationMetrics publishes collected metrics for endpoint annotations.
func (m *NegMetrics) PublishAnnotationMetrics(annotationSize int, labelNumber int) {
	m.annotationSize.Observe(float64(annotationSize))
	m.labelNumber.Observe(float64(labelNumber))
}

// PublishGCERequestCountMetrics publishes collected metrics for GCE Request Counts
func (m *NegMetrics) PublishGCERequestCountMetrics(start time.Time, requestType string, err error) {
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
	m.gceRequestLatency.WithLabelValues(requestType, result).Observe(time.Since(start).Seconds())
	m.gceRequestCount.WithLabelValues(requestType, result).Inc()
}

// PublishK8sRequestCountMetrics publishes collected metrics for K8s Request Counts
func (m *NegMetrics) PublishK8sRequestCountMetrics(start time.Time, requestType string, err error) {
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
	m.k8sRequestLatency.WithLabelValues(requestType, result).Observe(time.Since(start).Seconds())
	m.k8sRequestCount.WithLabelValues(requestType, result).Inc()
}

func (m *NegMetrics) PublishLastSyncTimestamp(t time.Time) {
	m.lastSyncTimestamp.Set(float64(t.UTC().UnixNano()))
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
