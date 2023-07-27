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

package metricscollector

import (
	"github.com/prometheus/client_golang/prometheus"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
)

const (
	negControllerSubsystem = "neg_controller"

	// Label values for Label Propagation Metrics
	epWithAnnotation = "with_annotation"
	totalEndpoints   = "total"

	// Classification of endpoints within a NEG.
	ipv4EndpointType      = "IPv4"
	ipv6EndpointType      = "IPv6"
	dualStackEndpointType = "DualStack"
	migrationEndpointType = "Migration"
)

var (
	// SyncerCountBySyncResult tracks the count of syncer in different states
	SyncerCountBySyncResult = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: negControllerSubsystem,
			Name:      "syncer_count",
			Help:      "Current count of syncers in each state",
		},
		[]string{"last_sync_result", "in_error_state"},
	)

	// syncerEndpointState tracks the count of endpoints in different states
	syncerEndpointState = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: negControllerSubsystem,
			Name:      "syncer_endpoint_state",
			Help:      "Current count of endpoints in each state",
		},
		[]string{"state"},
	)

	// syncerEndpointSliceState tracks the count of endpoint slices in different states
	syncerEndpointSliceState = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: negControllerSubsystem,
			Name:      "syncer_endpoint_slice_state",
			Help:      "Current count of endpoint slices in each state",
		},
		[]string{"state"},
	)

	NumberOfEndpoints = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: negControllerSubsystem,
			Name:      "number_of_endpoints",
			Help:      "The total number of endpoints",
		},
		[]string{"feature"},
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

	SyncerCountByEndpointType = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: negControllerSubsystem,
			Name:      "syncer_count_by_endpoint_type",
			Help:      "Number of Syncers managing NEGs containing endpoint of a particular kind",
		},
		[]string{"endpoint_type"},
	)

	DualStackMigrationServiceCount = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Subsystem: negControllerSubsystem,
			Name:      "dual_stack_migration_service_count",
			Help:      "Number of Services which have migration endpoints",
		},
	)

	// syncerSyncResult tracks the count for each sync result
	syncerSyncResult = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: negControllerSubsystem,
			Name:      "sync_result",
			Help:      "Current count for each sync result",
		},
		[]string{"result"},
	)

	negsManagedCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: negControllerSubsystem,
			Name:      "managed_neg_count",
			Help:      "Number of NEGs the Neg Controller Manages",
		},
		[]string{"location", "endpoint_type"},
	)
)

type syncerState struct {
	lastSyncResult negtypes.Reason
	inErrorState   bool
}

type syncerStateCount map[syncerState]int

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
