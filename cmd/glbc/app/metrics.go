/*
Copyright 2024 The Kubernetes Authors.

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

package app

import (
	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/klog/v2"
)

func init() {
	klog.V(3).Infof("Registering lock availability metrics %v", LockAvailability)
	prometheus.MustRegister(LockAvailability)
}

var (
	// LockAvailability tracks the how long has the container been running with the corresponding resource lock
	LockAvailability = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "lock_availability",
			Help: "Time in second since the container has been running with the corresponding resource lock",
		},
		[]string{"lock_name", "cluster_type"},
	)
)

func PublishLockAvailabilityMetrics(lockName string, clusterType string) {
	LockAvailability.WithLabelValues(lockName, clusterType).Inc()
}
