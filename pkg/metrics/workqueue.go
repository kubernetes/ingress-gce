/*
Copyright 2022 The Kubernetes Authors.

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
	"k8s.io/client-go/util/workqueue"
)

// This file sets the workqueue DefaultMetricsFactory to produce
// prometheus metrics for workqueue.

// Metrics subsystem and keys used by the workqueue.
const (
	workQueueSubsystem         = "workqueue"
	depthKey                   = "depth"
	addsKey                    = "adds_total"
	queueLatencyKey            = "queue_duration_seconds"
	workDurationKey            = "work_duration_seconds"
	unfinishedWorkKey          = "unfinished_work_seconds"
	longestRunningProcessorKey = "longest_running_processor_seconds"
	retriesKey                 = "retries_total"
	nameLabel                  = "name"
)

var (
	depth = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Subsystem: workQueueSubsystem,
		Name:      depthKey,
		Help:      "Current size of workqueue",
	}, []string{nameLabel})

	adds = prometheus.NewCounterVec(prometheus.CounterOpts{
		Subsystem: workQueueSubsystem,
		Name:      addsKey,
		Help:      "Total number of adds handled by workqueue",
	}, []string{nameLabel})

	latency = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Subsystem: workQueueSubsystem,
		Name:      queueLatencyKey,
		Help:      "Time item remains in the workqueue before processing (seconds).",
		// custom buckets - [1.75s,3.06s, 5.35s, 9.37s, 16.41s, 28.72s, 50.26s, 87.96s(1,5min), 153.93s(2,5min), 269.38s(4,5min),	471.43s(~8min), 825s(~14), 1443.75s(~24min), 2526.57s(~42min), 4421.51s(~73min), 7737.64s(~129min), 13540.87s(225min), +Inf]
		Buckets: prometheus.ExponentialBuckets(1, 1.75, 18),
	}, []string{nameLabel})

	workDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Subsystem: workQueueSubsystem,
		Name:      workDurationKey,
		Help:      "Time item takes to reconcile after dequeuing from the workqueue (seconds).",
		// custom buckets - [1.75s,3.06s, 5.35s, 9.37s, 16.41s, 28.72s, 50.26s, 87.96s(1,5min), 153.93s(2,5min), 269.38s(4,5min),	471.43s(~8min), 825s(~14), 1443.75s(~24min), 2526.57s(~42min), 4421.51s(~73min), 7737.64s(~129min), 13540.87s(225min), +Inf]
		Buckets: prometheus.ExponentialBuckets(1, 1.75, 18),
	}, []string{nameLabel})

	unfinished = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Subsystem: workQueueSubsystem,
		Name:      unfinishedWorkKey,
		Help: "How many seconds of work has done that " +
			"is in progress and hasn't been observed by work_duration. Large " +
			"values indicate stuck threads. One can deduce the number of stuck " +
			"threads by observing the rate at which this increases.",
	}, []string{nameLabel})

	longestRunningProcessor = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Subsystem: workQueueSubsystem,
		Name:      longestRunningProcessorKey,
		Help: "How many seconds has the longest running " +
			"processor for workqueue been running.",
	}, []string{nameLabel})

	retries = prometheus.NewCounterVec(prometheus.CounterOpts{
		Subsystem: workQueueSubsystem,
		Name:      retriesKey,
		Help:      "Total number of retries handled by workqueue",
	}, []string{nameLabel})
)

// workqueueMetricsProvider implements workqueue.MetricsProvider interface
type workqueueMetricsProvider struct {
}

func init() {
	prometheus.MustRegister(depth, adds, latency, workDuration, unfinished, longestRunningProcessor, retries)
	workqueue.SetProvider(workqueueMetricsProvider{})
}

func (workqueueMetricsProvider) NewDepthMetric(name string) workqueue.GaugeMetric {
	return depth.WithLabelValues(name)
}

func (workqueueMetricsProvider) NewAddsMetric(name string) workqueue.CounterMetric {
	return adds.WithLabelValues(name)
}

func (workqueueMetricsProvider) NewLatencyMetric(name string) workqueue.HistogramMetric {
	return latency.WithLabelValues(name)
}

func (workqueueMetricsProvider) NewWorkDurationMetric(name string) workqueue.HistogramMetric {
	return workDuration.WithLabelValues(name)
}

func (workqueueMetricsProvider) NewUnfinishedWorkSecondsMetric(name string) workqueue.SettableGaugeMetric {
	return unfinished.WithLabelValues(name)
}

func (workqueueMetricsProvider) NewLongestRunningProcessorSecondsMetric(name string) workqueue.SettableGaugeMetric {
	return longestRunningProcessor.WithLabelValues(name)
}

func (workqueueMetricsProvider) NewRetriesMetric(name string) workqueue.CounterMetric {
	return retries.WithLabelValues(name)
}
