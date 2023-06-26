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
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/klog/v2"
	"k8s.io/utils/clock"
)

var register sync.Once

// RegisterSyncerMetrics registers syncer related metrics
func RegisterMetrics() {
	register.Do(func() {
		prometheus.MustRegister(SyncerCountBySyncResult)
		prometheus.MustRegister(syncerEndpointState)
		prometheus.MustRegister(syncerEndpointSliceState)
		prometheus.MustRegister(NumberOfEndpoints)
		prometheus.MustRegister(DualStackMigrationFinishedDurations)
		prometheus.MustRegister(DualStackMigrationLongestUnfinishedDuration)
		prometheus.MustRegister(DualStackMigrationServiceCount)
		prometheus.MustRegister(SyncerCountByEndpointType)
		prometheus.MustRegister(syncerSyncResult)
		prometheus.MustRegister(negsManagedCount)
	})
}

type SyncerMetricsCollector interface {
	// UpdateSyncerStatusInMetrics update the status of corresponding syncer based on the sync error
	UpdateSyncerStatusInMetrics(key negtypes.NegSyncerKey, err error, inErrorState bool)
	// UpdateSyncerEPMetrics update the endpoint and endpointSlice count for the given syncer
	UpdateSyncerEPMetrics(key negtypes.NegSyncerKey, endpointCount, endpointSliceCount negtypes.StateCountMap)
	SetLabelPropagationStats(key negtypes.NegSyncerKey, labelstatLabelPropagationStats LabelPropagationStats)
	// Updates the number of negs per syncer per zone
	UpdateSyncerNegCount(key negtypes.NegSyncerKey, negByLocation map[string]int)
}

type negLocTypeKey struct {
	location     string
	endpointType string
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

	// logger logs message related to NegMetricsCollector
	logger klog.Logger
}

// NewNEGMetricsCollector initializes SyncerMetrics and starts a go routine to compute and export metrics periodically.
func NewNegMetricsCollector(exportInterval time.Duration, logger klog.Logger) *SyncerMetrics {
	return &SyncerMetrics{
		syncerStateMap:              make(map[negtypes.NegSyncerKey]syncerState),
		syncerEndpointStateMap:      make(map[negtypes.NegSyncerKey]negtypes.StateCountMap),
		syncerEndpointSliceStateMap: make(map[negtypes.NegSyncerKey]negtypes.StateCountMap),
		syncerLabelProagationStats:  make(map[negtypes.NegSyncerKey]LabelPropagationStats),
		dualStackMigrationStartTime: make(map[negtypes.NegSyncerKey]time.Time),
		dualStackMigrationEndTime:   make(map[negtypes.NegSyncerKey]time.Time),
		endpointsCountPerType:       make(map[negtypes.NegSyncerKey]map[string]int),
		syncerNegCount:              make(map[negtypes.NegSyncerKey]map[string]int),
		clock:                       clock.RealClock{},
		metricsInterval:             exportInterval,
		logger:                      logger.WithName("NegMetricsCollector"),
	}
}

// FakeSyncerMetrics creates new NegMetricsCollector with fixed 5 second metricsInterval, to be used in tests
func FakeSyncerMetrics() *SyncerMetrics {
	return NewNegMetricsCollector(5*time.Second, klog.TODO())
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
	NumberOfEndpoints.WithLabelValues(totalEndpoints).Set(float64(lpMetrics.NumberOfEndpoints))
	NumberOfEndpoints.WithLabelValues(epWithAnnotation).Set(float64(lpMetrics.EndpointsWithAnnotation))

	stateCount, syncerCount := sm.computeSyncerStateMetrics()
	PublishSyncerStateMetrics(stateCount)

	epStateCount, epsStateCount, epCount, epsCount := sm.computeEndpointStateMetrics()
	for state, count := range epStateCount {
		syncerEndpointState.WithLabelValues(string(state)).Set(float64(count))
	}
	for state, count := range epsStateCount {
		syncerEndpointSliceState.WithLabelValues(string(state)).Set(float64(count))
	}

	negCounts := sm.computeNegCounts()
	//Clear existing metrics (ensures that keys that don't exist anymore are reset)
	negsManagedCount.Reset()
	for key, count := range negCounts {
		negsManagedCount.WithLabelValues(key.location, key.endpointType).Set(float64(count))
	}

	sm.logger.V(3).Info("Exporting syncer related metrics", "Syncer count", syncerCount,
		"Network Endpoint Count", lpMetrics.NumberOfEndpoints,
		"Endpoint Count From EPS", epCount,
		"Endpoint Slice Count", epsCount,
		"NEG Count", negCounts,
	)

	finishedDurations, longestUnfinishedDurations := sm.computeDualStackMigrationDurations()
	for _, duration := range finishedDurations {
		DualStackMigrationFinishedDurations.Observe(float64(duration))
	}
	DualStackMigrationLongestUnfinishedDuration.Set(float64(longestUnfinishedDurations))

	syncerCountByEndpointType, migrationEndpointCount, migrationServicesCount := sm.computeDualStackMigrationCounts()
	for endpointType, count := range syncerCountByEndpointType {
		SyncerCountByEndpointType.WithLabelValues(endpointType).Set(float64(count))
	}
	syncerEndpointState.WithLabelValues(string(negtypes.DualStackMigration)).Set(float64(migrationEndpointCount))
	DualStackMigrationServiceCount.Set(float64(migrationServicesCount))

	sm.logger.V(3).Info("Exported DualStack Migration metrics")
}

// UpdateSyncerStatusInMetrics update the status of syncer based on the error
func (sm *SyncerMetrics) UpdateSyncerStatusInMetrics(key negtypes.NegSyncerKey, err error, inErrorState bool) {
	reason := negtypes.ReasonSuccess
	if err != nil {
		syncErr := negtypes.ClassifyError(err)
		reason = syncErr.Reason
	}
	syncerSyncResult.WithLabelValues(string(reason)).Inc()
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
func (sm *SyncerMetrics) CollectDualStackMigrationMetrics(key negtypes.NegSyncerKey, committedEndpoints map[string]negtypes.NetworkEndpointSet, migrationCount int) {
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

func (sm *SyncerMetrics) updateEndpointsCountPerType(key negtypes.NegSyncerKey, committedEndpoints map[string]negtypes.NetworkEndpointSet, migrationCount int) {
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

func PublishSyncerStateMetrics(stateCount syncerStateCount) {
	// Iterate to initialize all possible syncer state values.
	for _, syncerState := range listAllSyncerStates() {
		SyncerCountBySyncResult.WithLabelValues(
			string(syncerState.lastSyncResult), strconv.FormatBool(syncerState.inErrorState)).
			Set(float64(stateCount[syncerState]))
	}
}
