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
	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/klog/v2"
)

const (
	AddOperationTypeLabel    = "Add"
	RemoveOperationTypeLabel = "Remove"
)

var (
	instanceGroupEventSize = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "instance_group_event_size",
			Help:    "Size of adding or removing events attempted on instance groups",
			Buckets: prometheus.ExponentialBuckets(1, 2, 15),
		},
		[]string{"operation_type"},
	)
	instanceGroupEventCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "instance_group_event_count",
			Help: "Count of adding or removing events attempted on instance groups",
		},
		[]string{"operation_type"},
	)
)

// init metrics.
func init() {
	klog.V(3).Infof("Registering Instance Group event size metric: %v", instanceGroupEventSize)
	prometheus.MustRegister(instanceGroupEventSize)
	klog.V(3).Infof("Registering Instance Group event count metric: %v", instanceGroupEventCount)
	prometheus.MustRegister(instanceGroupEventCount)
}

// PublishInstanceGroupAdd counts how many times with attempt to add nodes to an instance group and the number of nodes present in each attempt.
func PublishInstanceGroupAdd(count int) {
	instanceGroupEventSize.WithLabelValues(AddOperationTypeLabel).Observe((float64(count)))
	instanceGroupEventCount.WithLabelValues(AddOperationTypeLabel).Inc()
}

// PublishInstanceGroupRemove counts how many times with attempt to remove nodes to an instance group and the number of nodes present in each attempt.
func PublishInstanceGroupRemove(count int) {
	instanceGroupEventSize.WithLabelValues(RemoveOperationTypeLabel).Observe((float64(count)))
	instanceGroupEventCount.WithLabelValues(RemoveOperationTypeLabel).Inc()
}
