/*
Copyright 2023 The Kubernetes Authors.

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
	"github.com/prometheus/client_golang/prometheus"
)

const (
	labelNumber       = "label_number_per_endpoint"
	annotationSize    = "annotation_size_per_endpoint"
	labelErrorNumber  = "label_propagation_error_count"
	numberOfEndpoints = "number_of_endpoints"
)

var (
	labelPropagationErrorLabels = []string{
		"error_type",
	}

	endpointAnnotationLabels = []string{
		"with_annotation",
	}

	NumberOfEndpoints = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: negControllerSubsystem,
			Name:      numberOfEndpoints,
			Help:      "The total number of endpoints",
		},
		endpointAnnotationLabels,
	)

	LabelNumber = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Subsystem: negControllerSubsystem,
			Name:      labelNumber,
			Help:      "The number of labels per endpoint",
		},
	)

	AnnotationSize = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Subsystem: negControllerSubsystem,
			Name:      annotationSize,
			Help:      "The size of endpoint annotations per endpoint",
		},
	)

	LabelPropagationError = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: negControllerSubsystem,
			Name:      labelErrorNumber,
			Help:      "the number of errors occurred for label propagation",
		},
		labelPropagationErrorLabels,
	)
)

// LabelPropagationStat contains stats related to label propagation.
type LabelPropagationStats struct {
	EndpointsWithAnnotation int
	NumberOfEndpoints       int
}

// LabelPropagationMetrics contains aggregated label propagation related metrics.
type LabelPropagationMetrics struct {
	EndpointsWithAnnotation int
	NumberOfEndpoints       int
}
