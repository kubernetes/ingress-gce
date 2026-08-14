/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
you may obtain a copy of the License at

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
	"sync"
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

var register sync.Once

// RegisterMetrics is now a no-op as metrics are registered via the MetricFactory. (cache invalidation)
func RegisterMetrics() {
	// No-op
}

type NegMetrics struct {
	negOperationLatency     mtmetrics.ObserverVec
	negOperationEndpoints   mtmetrics.ObserverVec
	syncerSyncLatency       mtmetrics.ObserverVec
	managerProcessLatency   mtmetrics.ObserverVec
	initializationLatency   mtmetrics.ObserverVec
	lastSyncTimestamp       mtmetrics.GaugeVec
	syncerStaleness         mtmetrics.ObserverVec
	epsStaleness            mtmetrics.ObserverVec
	degradeModeCorrectness  mtmetrics.ObserverVec
	negControllerErrorCount mtmetrics.CounterVec
	labelNumber             mtmetrics.ObserverVec
	annotationSize          mtmetrics.ObserverVec
	labelPropagationError   mtmetrics.CounterVec
	gceRequestCount         mtmetrics.CounterVec
	gceRequestLatency       mtmetrics.ObserverVec
	k8sRequestCount         mtmetrics.CounterVec
	k8sRequestLatency       mtmetrics.ObserverVec
}

// NewNegMetrics returns a NegMetrics initialized with the default global registry.
func NewNegMetrics() *NegMetrics {
	m, err := NewNegMetricsWithFactory(mtmetrics.NewStdMetricFactory(prometheus.DefaultRegisterer))
	if err != nil {
		klog.Errorf("Failed to initialize NegMetrics: %v", err)
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
			Buckets:   prometheus.ExponentialBuckets(1, 2, 13),
		},
		[]string{"operation", "neg_type", "api_version", "result"},
	); err != nil {
		return nil, fmt.Errorf("failed to create negOperationLatency: %w", err)
	}

	if m.negOperationEndpoints, err = factory.NewHistogramVec(
		prometheus.HistogramOpts{
			Subsystem: negControllerSubsystem,
			Name:      "neg_operation_endpoints",
			Help:      "Number of Endpoints during an NEG Operation",
			Buckets:   prometheus.ExponentialBuckets(1, 2, 13),
		},
		[]string{"operation", "neg_type", "result"},
	); err != nil {
		return nil, fmt.Errorf("failed to create negOperationEndpoints: %w", err)
	}

	if m.syncerSyncLatency, err = factory.NewHistogramVec(
		prometheus.HistogramOpts{
			Subsystem: negControllerSubsystem,
			Name:      "syncer_sync_duration_seconds",
			Help:      "Sync latency for NEG Syncer",
			Buckets:   prometheus.ExponentialBuckets(1, 2, 13),
		},
		[]string{"neg_type", "endpoint_calculator_mode", "result"},
	); err != nil {
		return nil, fmt.Errorf("failed to create syncerSyncLatency: %w", err)
	}

	if m.managerProcessLatency, err = factory.NewHistogramVec(
		prometheus.HistogramOpts{
			Subsystem: negControllerSubsystem,
			Name:      "manager_process_duration_seconds",
			Help:      "Process latency for NEG Manager",
			Buckets:   prometheus.ExponentialBuckets(1, 2, 13),
		},
		[]string{"process", "result"},
	); err != nil {
		return nil, fmt.Errorf("failed to create managerProcessLatency: %w", err)
	}

	if m.initializationLatency, err = factory.NewHistogramVec(
		prometheus.HistogramOpts{
			Subsystem: negControllerSubsystem,
			Name:      "neg_initialization_duration_seconds",
			Help:      "Initialization latency of a NEG",
			Buckets:   prometheus.ExponentialBuckets(1, 2, 13),
		},
		[]string{},
	); err != nil {
		return nil, fmt.Errorf("failed to create initializationLatency: %w", err)
	}

	if m.lastSyncTimestamp, err = factory.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: negControllerSubsystem,
			Name:      "sync_timestamp",
			Help:      "The timestamp of the last execution of NEG controller sync loop.",
		},
		[]string{},
	); err != nil {
		return nil, fmt.Errorf("failed to create lastSyncTimestamp: %w", err)
	}

	if m.syncerStaleness, err = factory.NewHistogramVec(
		prometheus.HistogramOpts{
			Subsystem: negControllerSubsystem,
			Name:      "syncer_staleness",
			Help:      "The duration of a syncer since it last syncs",
			Buckets:   prometheus.ExponentialBuckets(1, 2, 14),
		},
		[]string{},
	); err != nil {
		return nil, fmt.Errorf("failed to create syncerStaleness: %w", err)
	}

	if m.epsStaleness, err = factory.NewHistogramVec(
		prometheus.HistogramOpts{
			Subsystem: negControllerSubsystem,
			Name:      "endpointslice_staleness",
			Help:      "The duration for an endpoint slice since it was last processed by syncer",
			Buckets:   prometheus.ExponentialBuckets(1, 2, 14),
		},
		[]string{},
	); err != nil {
		return nil, fmt.Errorf("failed to create epsStaleness: %w", err)
	}

	if m.degradeModeCorrectness, err = factory.NewHistogramVec(
		prometheus.HistogramOpts{
			Subsystem: negControllerSubsystem,
			Name:      "degraded_mode_correctness",
			Help:      "Number of endpoints differed between current endpoint calculation and degraded mode calculation",
			Buckets:   append([]float64{0}, prometheus.ExponentialBuckets(1, 2, 20)...),
		},
		[]string{"neg_type", "endpoint_type"},
	); err != nil {
		return nil, fmt.Errorf("failed to create degradeModeCorrectness: %w", err)
	}

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

	if m.labelNumber, err = factory.NewHistogramVec(
		prometheus.HistogramOpts{
			Subsystem: negControllerSubsystem,
			Name:      "label_number_per_endpoint",
			Help:      "The number of labels per endpoint",
			Buckets:   prometheus.ExponentialBuckets(1, 2, 13),
		},
		[]string{},
	); err != nil {
		return nil, fmt.Errorf("failed to create labelNumber: %w", err)
	}

	if m.annotationSize, err = factory.NewHistogramVec(
		prometheus.HistogramOpts{
			Subsystem: negControllerSubsystem,
			Name:      "annotation_size_per_endpoint",
			Help:      "The size in byte of endpoint annotations per endpoint",
			Buckets:   prometheus.ExponentialBuckets(1, 2, 13),
		},
		[]string{},
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

	if m.gceRequestLatency, err = factory.NewHistogramVec(
		prometheus.HistogramOpts{
			Subsystem: negControllerSubsystem,
			Name:      "gce_request_latency",
			Help:      "Observed request latency for requests sent by NEG Controller to Arcus.",
			Buckets:   append([]float64{0.001, 0.01, 0.1}, prometheus.ExponentialBuckets(1, 2, 20)...),
		},
		[]string{"request", "result"},
	); err != nil {
		return nil, fmt.Errorf("failed to create gceRequestLatency: %w", err)
	}

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

	if m.k8sRequestLatency, err = factory.NewHistogramVec(
		prometheus.HistogramOpts{
			Subsystem: negControllerSubsystem,
			Name:      "k8s_request_latency",
			Help:      "Observed request latency for requests sent by NEG Controller to Kubernetes API Server.",
			Buckets:   append([]float64{0.001, 0.01, 0.1}, prometheus.ExponentialBuckets(1, 2, 20)...),
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
	m.initializationLatency.WithLabelValues().Observe(latency.Seconds())
}

func (m *NegMetrics) PublishNegSyncerStalenessMetrics(syncerStaleness time.Duration) {
	m.syncerStaleness.WithLabelValues().Observe(syncerStaleness.Seconds())
}

func (m *NegMetrics) PublishNegEPSStalenessMetrics(epsStaleness time.Duration) {
	m.epsStaleness.WithLabelValues().Observe(epsStaleness.Seconds())
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
	m.annotationSize.WithLabelValues().Observe(float64(annotationSize))
	m.labelNumber.WithLabelValues().Observe(float64(labelNumber))
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
	m.lastSyncTimestamp.WithLabelValues().Set(float64(t.UTC().UnixNano()))
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
