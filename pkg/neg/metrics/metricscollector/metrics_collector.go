/*
Copyright 2023 The Kubernetes Authors.

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

package metricscollector

import (
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/GoogleCloudPlatform/gke-enterprise-mt/pkg/mtmetrics"
	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/klog/v2"
	"k8s.io/utils/clock"
)

var register sync.Once

// RegisterMetrics is a no-op because metrics are registered via the factory.
func RegisterMetrics() {
}

type SyncerMetricsCollector interface {
	// UpdateSyncerStatusInMetrics update the status of corresponding syncer based on the sync error
	UpdateSyncerStatusInMetrics(key negtypes.NegSyncerKey, err error, inErrorState bool)
	// UpdateSyncerEPMetrics update the endpoint and endpointSlice count for the given syncer
	UpdateSyncerEPMetrics(key negtypes.NegSyncerKey, endpointCount, endpointSliceCount negtypes.StateCountMap)
	SetLabelPropagationStats(key negtypes.NegSyncerKey, labelstatLabelPropagationStats LabelPropagationStats)
	// Updates the number of negs per syncer per zone
	UpdateSyncerNegCount(key negtypes.NegSyncerKey, negByLocation map[string]int)
	// SetNegService adds/updates neg state for given service key.
	SetNegService(svcKey string, negState NegServiceState)
	// DeleteNegService removes the given service key.
	DeleteNegService(svcKey string)
}

type SyncerMetrics struct {
	clock clock.Clock
	// duration between metrics exports
	metricsInterval time.Duration

	mu sync.Mutex
	// syncerStateMap tracks the status of each syncer
	syncerStateMap map[negtypes.NegSyncerKey]syncerState
	// syncerEndpointStateMap is a map between syncer and endpoint state counts.
	syncerEndpointStateMap map[negtypes.NegSyncerKey]negtypes.StateCountMap
	// syncerEndpointSliceStateMap is a map between syncer and endpoint slice state counts.
	syncerEndpointSliceStateMap map[negtypes.NegSyncerKey]negtypes.StateCountMap
	// syncerLabelProagationStats is a map between syncer and label propagation stats.
	syncerLabelProagationStats map[negtypes.NegSyncerKey]LabelPropagationStats
	// Stores the time when the migration started for each Syncer.
	dualStackMigrationStartTime map[negtypes.NegSyncerKey]time.Time
	// Stores the time when the migration finished for each Syncer.
	dualStackMigrationEndTime map[negtypes.NegSyncerKey]time.Time
	// Stores the count of various kinds of endpoints which each syncer manages.
	// Refer neg/metrics.go for the kinds of endpoints.
	endpointsCountPerType map[negtypes.NegSyncerKey]map[string]int
	//Stores the number of NEGs the NEG controller is managed based on location
	syncerNegCount map[negtypes.NegSyncerKey]map[string]int
	// negMap is a map between service key to neg state
	negMap map[string]NegServiceState

	// logger logs message related to NegMetricsCollector
	logger klog.Logger

	// Syncer metrics scoped to struct fields
	syncerCountBySyncResult                     mtmetrics.GaugeVec
	syncerEndpointState                         mtmetrics.GaugeVec
	syncerEndpointSliceState                    mtmetrics.GaugeVec
	numberOfEndpoints                           mtmetrics.GaugeVec
	dualStackMigrationFinishedDurations         mtmetrics.ObserverVec
	dualStackMigrationLongestUnfinishedDuration mtmetrics.GaugeVec
	syncerCountByEndpointType                   mtmetrics.GaugeVec
	dualStackMigrationServiceCount              mtmetrics.GaugeVec
	syncerSyncResult                            mtmetrics.CounterVec
	negsManagedCount                            mtmetrics.GaugeVec
	networkEndpointGroupCount                   mtmetrics.GaugeVec
}

// NewNegMetricsCollector initializes SyncerMetrics and starts a go routine to compute and export metrics periodically.
func NewNegMetricsCollector(exportInterval time.Duration, factory mtmetrics.MetricFactory, logger klog.Logger) (*SyncerMetrics, error) {
	syncerCountBySyncResult, err := factory.NewGaugeVec(prometheus.GaugeOpts{
		Subsystem: negControllerSubsystem,
		Name:      "syncer_count",
		Help:      "Current count of syncers in each state",
	}, []string{"last_sync_result", "in_error_state"})
	if err != nil {
		return nil, fmt.Errorf("failed to create syncer_count: %w", err)
	}

	syncerEndpointState, err := factory.NewGaugeVec(prometheus.GaugeOpts{
		Subsystem: negControllerSubsystem,
		Name:      "syncer_endpoint_state",
		Help:      "Current count of endpoints in each state",
	}, []string{"state"})
	if err != nil {
		return nil, fmt.Errorf("failed to create syncer_endpoint_state: %w", err)
	}

	syncerEndpointSliceState, err := factory.NewGaugeVec(prometheus.GaugeOpts{
		Subsystem: negControllerSubsystem,
		Name:      "syncer_endpoint_slice_state",
		Help:      "Current count of endpoint slices in each state",
	}, []string{"state"})
	if err != nil {
		return nil, fmt.Errorf("failed to create syncer_endpoint_slice_state: %w", err)
	}

	numberOfEndpoints, err := factory.NewGaugeVec(prometheus.GaugeOpts{
		Subsystem: negControllerSubsystem,
		Name:      "number_of_endpoints",
		Help:      "The total number of endpoints",
	}, []string{"feature"})
	if err != nil {
		return nil, fmt.Errorf("failed to create number_of_endpoints: %w", err)
	}

	dualStackMigrationFinishedDurations, err := factory.NewHistogramVec(prometheus.HistogramOpts{
		Subsystem: negControllerSubsystem,
		Name:      "dual_stack_migration_finished_durations_seconds",
		Help:      "Time taken to migrate all endpoints within all NEGs for a service port",
		// Buckets ~= [1s, 1.85s, 3.42s, 6s, 11s, 21s, 40s, 1m14s, 2m17s, 4m13s, 7m49s, 14m28s, 26m47s, 49m33s, 1h31m40s, 2h49m35s, 5h13m45s, 9h40m27s, +Inf]
		Buckets: prometheus.ExponentialBuckets(1, 1.85, 18),
	}, []string{})
	if err != nil {
		return nil, fmt.Errorf("failed to create dual_stack_migration_finished_durations_seconds: %w", err)
	}

	dualStackMigrationLongestUnfinishedDuration, err := factory.NewGaugeVec(prometheus.GaugeOpts{
		Subsystem: negControllerSubsystem,
		Name:      "dual_stack_migration_longest_unfinished_duration_seconds",
		Help:      "Longest time elapsed since a migration was started which hasn't yet completed",
	}, []string{})
	if err != nil {
		return nil, fmt.Errorf("failed to create dual_stack_migration_longest_unfinished_duration_seconds: %w", err)
	}

	syncerCountByEndpointType, err := factory.NewGaugeVec(prometheus.GaugeOpts{
		Subsystem: negControllerSubsystem,
		Name:      "syncer_count_by_endpoint_type",
		Help:      "Number of Syncers managing NEGs containing endpoint of a particular kind",
	}, []string{"endpoint_type"})
	if err != nil {
		return nil, fmt.Errorf("failed to create syncer_count_by_endpoint_type: %w", err)
	}

	dualStackMigrationServiceCount, err := factory.NewGaugeVec(prometheus.GaugeOpts{
		Subsystem: negControllerSubsystem,
		Name:      "dual_stack_migration_service_count",
		Help:      "Number of Services which have migration endpoints",
	}, []string{})
	if err != nil {
		return nil, fmt.Errorf("failed to create dual_stack_migration_service_count: %w", err)
	}

	syncerSyncResult, err := factory.NewCounterVec(prometheus.CounterOpts{
		Subsystem: negControllerSubsystem,
		Name:      "sync_result",
		Help:      "Current count for each sync result",
	}, []string{"result"})
	if err != nil {
		return nil, fmt.Errorf("failed to create sync_result: %w", err)
	}

	negsManagedCount, err := factory.NewGaugeVec(prometheus.GaugeOpts{
		Subsystem: negControllerSubsystem,
		Name:      "managed_neg_count",
		Help:      "Number of NEGs the Neg Controller Manages",
	}, []string{"location", "endpoint_type"})
	if err != nil {
		return nil, fmt.Errorf("failed to create managed_neg_count: %w", err)
	}

	networkEndpointGroupCount, err := factory.NewGaugeVec(prometheus.GaugeOpts{
		Name: "number_of_negs",
		Help: "Number of NEGs",
	}, []string{"feature"})
	if err != nil {
		return nil, fmt.Errorf("failed to create number_of_negs: %w", err)
	}

	return &SyncerMetrics{
		syncerStateMap:              make(map[negtypes.NegSyncerKey]syncerState),
		syncerEndpointStateMap:      make(map[negtypes.NegSyncerKey]negtypes.StateCountMap),
		syncerEndpointSliceStateMap: make(map[negtypes.NegSyncerKey]negtypes.StateCountMap),
		syncerLabelProagationStats:  make(map[negtypes.NegSyncerKey]LabelPropagationStats),
		dualStackMigrationStartTime: make(map[negtypes.NegSyncerKey]time.Time),
		dualStackMigrationEndTime:   make(map[negtypes.NegSyncerKey]time.Time),
		endpointsCountPerType:       make(map[negtypes.NegSyncerKey]map[string]int),
		syncerNegCount:              make(map[negtypes.NegSyncerKey]map[string]int),
		negMap:                      make(map[string]NegServiceState),
		clock:                       clock.RealClock{},
		metricsInterval:             exportInterval,
		logger:                      logger.WithName("NegMetricsCollector"),

		syncerCountBySyncResult:                     syncerCountBySyncResult,
		syncerEndpointState:                         syncerEndpointState,
		syncerEndpointSliceState:                    syncerEndpointSliceState,
		numberOfEndpoints:                           numberOfEndpoints,
		dualStackMigrationFinishedDurations:         dualStackMigrationFinishedDurations,
		dualStackMigrationLongestUnfinishedDuration: dualStackMigrationLongestUnfinishedDuration,
		syncerCountByEndpointType:                   syncerCountByEndpointType,
		dualStackMigrationServiceCount:              dualStackMigrationServiceCount,
		syncerSyncResult:                            syncerSyncResult,
		negsManagedCount:                            negsManagedCount,
		networkEndpointGroupCount:                   networkEndpointGroupCount,
	}, nil
}

// FakeSyncerMetrics creates new NegMetricsCollector with fixed 5 second metricsInterval, to be used in tests
func FakeSyncerMetrics() *SyncerMetrics {
	sm, err := NewNegMetricsCollector(5*time.Second, mtmetrics.NewStdMetricFactory(prometheus.NewRegistry()), klog.TODO())
	if err != nil {
		klog.Errorf("Failed to create FakeSyncerMetrics: %v", err)
	}
	return sm
}

func (sm *SyncerMetrics) Run(stopCh <-chan struct{}) {
	sm.logger.V(3).Info("Syncer Metrics initialized.", "exportInterval", sm.metricsInterval)
	// Compute and export metrics periodically.
	go func() {
		time.Sleep(sm.metricsInterval)
		wait.Until(sm.export, sm.metricsInterval, stopCh)
	}()
	<-stopCh
}

// export exports syncer metrics.
func (sm *SyncerMetrics) export() {
	lpMetrics := sm.computeLabelMetrics()
	sm.numberOfEndpoints.WithLabelValues(totalEndpoints).Set(float64(lpMetrics.NumberOfEndpoints))
	sm.numberOfEndpoints.WithLabelValues(epWithAnnotation).Set(float64(lpMetrics.EndpointsWithAnnotation))

	stateCount, syncerCount := sm.computeSyncerStateMetrics()
	//Reset metric so non-existent keys are now 0
	sm.syncerCountBySyncResult.Reset()
	for syncerState, count := range stateCount {
		sm.syncerCountBySyncResult.WithLabelValues(string(syncerState.lastSyncResult), strconv.FormatBool(syncerState.inErrorState)).Set(float64(count))
	}

	epStateCount, epsStateCount, epCount, epsCount := sm.computeEndpointStateMetrics()
	for state, count := range epStateCount {
		sm.syncerEndpointState.WithLabelValues(string(state)).Set(float64(count))
	}
	for state, count := range epsStateCount {
		sm.syncerEndpointSliceState.WithLabelValues(string(state)).Set(float64(count))
	}

	negCounts := sm.computeNegCounts()
	//Clear existing metrics (ensures that keys that don't exist anymore are reset)
	sm.negsManagedCount.Reset()
	for key, count := range negCounts {
		sm.negsManagedCount.WithLabelValues(key.location, key.endpointType).Set(float64(count))
	}

	sm.logger.V(3).Info("Exporting syncer related metrics", "Syncer count", syncerCount,
		"Network Endpoint Count", lpMetrics.NumberOfEndpoints,
		"Endpoint Count From EPS", epCount,
		"Endpoint Slice Count", epsCount,
		"NEG Count", fmt.Sprintf("%+v", negCounts),
	)

	finishedDurations, longestUnfinishedDurations := sm.computeDualStackMigrationDurations()
	for _, duration := range finishedDurations {
		sm.dualStackMigrationFinishedDurations.WithLabelValues().Observe(float64(duration))
	}
	sm.dualStackMigrationLongestUnfinishedDuration.WithLabelValues().Set(float64(longestUnfinishedDurations))

	syncerCountByEndpointType, migrationEndpointCount, migrationServicesCount := sm.computeDualStackMigrationCounts()
	for endpointType, count := range syncerCountByEndpointType {
		sm.syncerCountByEndpointType.WithLabelValues(endpointType).Set(float64(count))
	}
	sm.syncerEndpointState.WithLabelValues(string(negtypes.DualStackMigration)).Set(float64(migrationEndpointCount))
	sm.dualStackMigrationServiceCount.WithLabelValues().Set(float64(migrationServicesCount))

	sm.logger.V(3).Info("Exported DualStack Migration metrics")

	negCount := sm.computeNegMetrics()
	for feature, count := range negCount {
		sm.networkEndpointGroupCount.WithLabelValues(feature.String()).Set(float64(count))
	}
	sm.logger.V(3).Info("Exported NEG usage metrics", "NEG count", fmt.Sprintf("%#v", negCount))
}

// UpdateSyncerStatusInMetrics update the status of syncer based on the error
func (sm *SyncerMetrics) UpdateSyncerStatusInMetrics(key negtypes.NegSyncerKey, err error, inErrorState bool) {
	reason := negtypes.ReasonSuccess
	if err != nil {
		syncErr := negtypes.ClassifyError(err)
		reason = syncErr.Reason
	}
	sm.syncerSyncResult.WithLabelValues(string(reason)).Inc()
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if sm.syncerStateMap == nil {
		sm.syncerStateMap = make(map[negtypes.NegSyncerKey]syncerState)
		sm.logger.V(3).Info("Syncer Metrics failed to initialize correctly, reinitializing syncerStateMap: %v", sm.syncerStateMap)
	}
	sm.syncerStateMap[key] = syncerState{lastSyncResult: reason, inErrorState: inErrorState}
}

func (sm *SyncerMetrics) UpdateSyncerEPMetrics(key negtypes.NegSyncerKey, endpointCount, endpointSliceCount negtypes.StateCountMap) {
	sm.logger.V(3).Info("Updating syncer endpoint", "syncerKey", key)
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if sm.syncerEndpointStateMap == nil {
		sm.syncerEndpointStateMap = make(map[negtypes.NegSyncerKey]negtypes.StateCountMap)
		sm.logger.V(3).Info("Syncer Metrics failed to initialize correctly, reinitializing syncerEndpointStateMap")
	}
	sm.syncerEndpointStateMap[key] = endpointCount

	if sm.syncerEndpointSliceStateMap == nil {
		sm.syncerEndpointSliceStateMap = make(map[negtypes.NegSyncerKey]negtypes.StateCountMap)
		sm.logger.V(3).Info("Syncer Metrics failed to initialize correctly, reinitializing syncerEndpointSliceStateMap")
	}
	sm.syncerEndpointSliceStateMap[key] = endpointSliceCount
}

func (sm *SyncerMetrics) SetLabelPropagationStats(key negtypes.NegSyncerKey, labelstatLabelPropagationStats LabelPropagationStats) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if sm.syncerLabelProagationStats == nil {
		sm.syncerLabelProagationStats = make(map[negtypes.NegSyncerKey]LabelPropagationStats)
		sm.logger.V(3).Info("Syncer Metrics failed to initialize correctly, reinitializing syncerLabelProagationStats")
	}
	sm.syncerLabelProagationStats[key] = labelstatLabelPropagationStats
}

// DeleteSyncer will reset any metrics for the syncer corresponding to `key`. It
// should be invoked when a Syncer has been stopped.
func (sm *SyncerMetrics) DeleteSyncer(key negtypes.NegSyncerKey) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	delete(sm.syncerStateMap, key)
	delete(sm.syncerEndpointStateMap, key)
	delete(sm.syncerEndpointSliceStateMap, key)
	delete(sm.syncerLabelProagationStats, key)
	delete(sm.dualStackMigrationStartTime, key)
	delete(sm.dualStackMigrationEndTime, key)
	delete(sm.endpointsCountPerType, key)
	delete(sm.syncerNegCount, key)
}

// computeLabelMetrics aggregates label propagation metrics.
func (sm *SyncerMetrics) computeLabelMetrics() LabelPropagationMetrics {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	lpMetrics := LabelPropagationMetrics{}
	for _, stats := range sm.syncerLabelProagationStats {
		lpMetrics.EndpointsWithAnnotation += stats.EndpointsWithAnnotation
		lpMetrics.NumberOfEndpoints += stats.NumberOfEndpoints
	}
	return lpMetrics
}

func (sm *SyncerMetrics) computeSyncerStateMetrics() (syncerStateCount, int) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	stateCount := make(syncerStateCount)
	syncerCount := 0
	for _, syncerState := range sm.syncerStateMap {
		stateCount[syncerState] += 1
		syncerCount++
	}
	return stateCount, syncerCount
}

// computeSyncerEndpointStateMetrics aggregates endpoint and endpoint slice counts from all syncers
func (sm *SyncerMetrics) computeEndpointStateMetrics() (negtypes.StateCountMap, negtypes.StateCountMap, int, int) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	var epCount, epsCount int
	epStateCount := negtypes.StateCountMap{}
	epsStateCount := negtypes.StateCountMap{}
	// collect count from each syncer
	for _, epState := range sm.syncerEndpointStateMap {
		for _, state := range negtypes.StatesForEndpointMetrics() {
			epStateCount[state] += epState[state]
			epCount += epState[state]
		}
	}
	for _, epsState := range sm.syncerEndpointSliceStateMap {
		for _, state := range negtypes.StatesForEndpointMetrics() {
			epsStateCount[state] += epsState[state]
			epsCount += epsState[state]
		}
	}
	return epStateCount, epsStateCount, epCount, epsCount
}

// CollectDualStackMigrationMetrics will be used by dualstack.Migrator to export
// metrics.
func (sm *SyncerMetrics) CollectDualStackMigrationMetrics(key negtypes.NegSyncerKey, committedEndpoints map[negtypes.NEGLocation]negtypes.NetworkEndpointSet, migrationCount int) {
	sm.updateMigrationStartAndEndTime(key, migrationCount)
	sm.updateEndpointsCountPerType(key, committedEndpoints, migrationCount)
}

func (sm *SyncerMetrics) updateMigrationStartAndEndTime(key negtypes.NegSyncerKey, migrationCount int) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	_, hasStartTime := sm.dualStackMigrationStartTime[key]
	_, hasEndTime := sm.dualStackMigrationEndTime[key]

	if migrationCount == 0 {
		//
		// Migration has finished or it never started.
		//
		if !hasStartTime {
			// Migration was never started.
			return
		}
		if hasEndTime {
			// Migration was already finished in some previous invocation.
			return
		}
		sm.dualStackMigrationEndTime[key] = sm.clock.Now()
		return
	}

	//
	// Migration has started or it was already in progress.
	//
	if hasEndTime {
		// A previous migration was completed but there are still migrating
		// endpoints so extend the previous migration time.
		delete(sm.dualStackMigrationEndTime, key)
	}
	if hasStartTime {
		// Migration was already started in some previous invocation.
		return
	}
	sm.dualStackMigrationStartTime[key] = sm.clock.Now()
}

func (sm *SyncerMetrics) updateEndpointsCountPerType(key negtypes.NegSyncerKey, committedEndpoints map[negtypes.NEGLocation]negtypes.NetworkEndpointSet, migrationCount int) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	ipv4OnlyCount, ipv6OnlyCount, dualStackCount := 0, 0, 0
	for _, endpointSet := range committedEndpoints {
		for endpoint := range endpointSet {
			if endpoint.IP != "" && endpoint.IPv6 != "" {
				dualStackCount++
				continue
			}
			if endpoint.IP != "" {
				ipv4OnlyCount++
			}
			if endpoint.IPv6 != "" {
				ipv6OnlyCount++
			}
		}
	}
	sm.endpointsCountPerType[key] = map[string]int{
		ipv4EndpointType:      ipv4OnlyCount,
		ipv6EndpointType:      ipv6OnlyCount,
		dualStackEndpointType: dualStackCount,
		migrationEndpointType: migrationCount,
	}
}

func (sm *SyncerMetrics) computeDualStackMigrationDurations() ([]int, int) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	finishedDurations, longestUnfinishedDuration := make([]int, 0), 0
	for key, startTime := range sm.dualStackMigrationStartTime {
		endTime, ok := sm.dualStackMigrationEndTime[key]
		if !ok {
			if curUnfinishedDuration := int(sm.clock.Since(startTime).Seconds()); curUnfinishedDuration > longestUnfinishedDuration {
				longestUnfinishedDuration = curUnfinishedDuration
			}
			continue
		}
		finishedDurations = append(finishedDurations, int(endTime.Sub(startTime).Seconds()))
		// Prevent metrics from being re-emitted by deleting the syncer key whose
		// migrations have finished.
		delete(sm.dualStackMigrationStartTime, key)
		delete(sm.dualStackMigrationEndTime, key)
	}

	return finishedDurations, longestUnfinishedDuration
}

func (sm *SyncerMetrics) computeDualStackMigrationCounts() (map[string]int, int, int) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// It's important to explicitly initialize all types to zero so that their
	// counts get reset when the metrics are published.
	syncerCountByEndpointType := map[string]int{
		ipv4EndpointType:      0,
		ipv6EndpointType:      0,
		dualStackEndpointType: 0,
		migrationEndpointType: 0,
	}
	migrationEndpointCount := 0
	migrationServices := sets.NewString()

	for syncerKey, syncerEndpointsCountPerType := range sm.endpointsCountPerType {
		for endpointType, count := range syncerEndpointsCountPerType {
			if count != 0 {
				syncerCountByEndpointType[endpointType]++
			}
		}

		if count := syncerEndpointsCountPerType[migrationEndpointType]; count != 0 {
			migrationServices.Insert(fmt.Sprintf("%s/%s", syncerKey.Namespace, syncerKey.Name))
			migrationEndpointCount += count
		}
	}
	return syncerCountByEndpointType, migrationEndpointCount, migrationServices.Len()
}

func (sm *SyncerMetrics) UpdateSyncerNegCount(key negtypes.NegSyncerKey, negsByLocation map[string]int) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.syncerNegCount[key] = negsByLocation
}

func (sm *SyncerMetrics) computeNegCounts() map[negLocTypeKey]int {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	negCountByLocation := make(map[negLocTypeKey]int)

	for syncerKey, syncerNegCount := range sm.syncerNegCount {
		for location, count := range syncerNegCount {
			key := negLocTypeKey{location: location, endpointType: string(syncerKey.NegType)}
			negCountByLocation[key] += count
		}
	}

	return negCountByLocation
}

// SetNegService implements NegMetricsCollector.
func (sm *SyncerMetrics) SetNegService(svcKey string, negState NegServiceState) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.negMap == nil {
		klog.Fatalf("Ingress Metrics failed to initialize correctly.")
	}
	if existing, ok := sm.negMap[svcKey]; ok {
		negState.NegBindingNeg = existing.NegBindingNeg
		negState.BindingSuccessfulNeg = existing.BindingSuccessfulNeg
		negState.BindingErrorNeg = existing.BindingErrorNeg
	}
	sm.negMap[svcKey] = negState
}

// DeleteNegService implements NegMetricsCollector.
func (sm *SyncerMetrics) DeleteNegService(svcKey string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	existing, ok := sm.negMap[svcKey]
	if !ok {
		return
	}
	if existing.NegBindingNeg == 0 {
		delete(sm.negMap, svcKey)
	} else {
		sm.negMap[svcKey] = NegServiceState{
			NegBindingNeg:        existing.NegBindingNeg,
			BindingSuccessfulNeg: existing.BindingSuccessfulNeg,
			BindingErrorNeg:      existing.BindingErrorNeg,
		}
	}
}

// GetNegService returns the NegServiceState for a given service key.
func (sm *SyncerMetrics) GetNegService(svcKey string) (NegServiceState, bool) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	state, ok := sm.negMap[svcKey]
	return state, ok
}

// SetNegBindingService sets or updates NegBindingNeg counts for a service key without overwriting other neg types.
func (sm *SyncerMetrics) SetNegBindingService(svcKey string, negState NegServiceState) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.negMap == nil {
		klog.Fatalf("Ingress Metrics failed to initialize correctly.")
	}
	existing, ok := sm.negMap[svcKey]
	if !ok {
		if negState.NegBindingNeg == 0 {
			return
		}
		sm.negMap[svcKey] = negState
		return
	}
	existing.NegBindingNeg = negState.NegBindingNeg
	existing.BindingSuccessfulNeg = negState.BindingSuccessfulNeg
	existing.BindingErrorNeg = negState.BindingErrorNeg
	sm.negMap[svcKey] = existing
}

// DeleteNegBindingService resets NegBindingNeg for a service key and removes the service if no other NEGs exist.
func (sm *SyncerMetrics) DeleteNegBindingService(svcKey string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	existing, ok := sm.negMap[svcKey]
	if !ok {
		return
	}

	if existing.SuccessfulNeg != 0 || existing.ErrorNeg != 0 || existing.StandaloneNeg != 0 || existing.IngressNeg != 0 || existing.VmIpNeg != nil {
		existing.NegBindingNeg = 0
		existing.BindingSuccessfulNeg = 0
		existing.BindingErrorNeg = 0
		sm.negMap[svcKey] = existing
	} else {
		delete(sm.negMap, svcKey)
	}
}

// computeNegMetrics aggregates NEG metrics in the cache
func (sm *SyncerMetrics) computeNegMetrics() map[feature]int {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	klog.V(4).Infof("Computing NEG usage metrics from neg state map: %#v", sm.negMap)

	counts := map[feature]int{
		standaloneNeg:     0,
		ingressNeg:        0,
		neg:               0,
		vmIpNeg:           0,
		vmIpNegLocal:      0,
		vmIpNegCluster:    0,
		customNamedNeg:    0,
		preprovisionedNeg: 0,
		negBindingNeg:     0,
		negInSuccess:      0,
		negInError:        0,
	}

	for key, negState := range sm.negMap {
		klog.V(6).Infof("For service %s, it has standaloneNegs:%d, ingressNegs:%d, negBindingNegs:%d and vmPrimaryNeg:%v",
			key, negState.StandaloneNeg, negState.IngressNeg, negState.NegBindingNeg, negState.VmIpNeg)
		counts[standaloneNeg] += negState.StandaloneNeg
		counts[ingressNeg] += negState.IngressNeg
		counts[negBindingNeg] += negState.NegBindingNeg
		counts[neg] += negState.StandaloneNeg + negState.IngressNeg + negState.NegBindingNeg
		counts[customNamedNeg] += negState.CustomNamedNeg
		counts[preprovisionedNeg] += negState.PreprovisionedNeg
		counts[negInSuccess] += negState.SuccessfulNeg + negState.BindingSuccessfulNeg
		counts[negInError] += negState.ErrorNeg + negState.BindingErrorNeg
		if negState.VmIpNeg != nil {
			counts[neg] += 1
			counts[vmIpNeg] += 1
			if negState.VmIpNeg.trafficPolicyLocal {
				counts[vmIpNegLocal] += 1
			} else {
				counts[vmIpNegCluster] += 1
			}
		}
	}
	klog.V(4).Info("NEG usage metrics computed.")
	return counts
}
