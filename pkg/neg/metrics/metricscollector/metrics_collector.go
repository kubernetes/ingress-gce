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

// RegisterMetrics registers syncer related metrics
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
		prometheus.MustRegister(networkEndpointGroupCount)
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

type NEGMetricsCollector interface {
	// SetNegService adds/updates neg state for given service key.
	SetNegService(svcKey string, negState NegServiceState)
	// DeleteNegService removes the given service key.
	DeleteNegService(svcKey string)
}

type NEGControllerMetrics struct {
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
}

// NewNegControllerMetrics initializes SyncerMetrics and starts a go routine to compute and export metrics periodically.
func NewNegControllerMetrics(exportInterval time.Duration, logger klog.Logger) *NEGControllerMetrics {
	return &NEGControllerMetrics{
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
	}
}

// FakeNEGControllerMetrics creates new NegMetricsCollector with fixed 5 second metricsInterval, to be used in tests
func FakeNEGControllerMetrics() *NEGControllerMetrics {
	return NewNegControllerMetrics(5*time.Second, klog.TODO())
}

func (cm *NEGControllerMetrics) Run(stopCh <-chan struct{}) {
	cm.logger.V(3).Info("Syncer Metrics initialized.", "exportInterval", cm.metricsInterval)
	// Compute and export metrics periodically.
	go func() {
		time.Sleep(cm.metricsInterval)
		wait.Until(cm.export, cm.metricsInterval, stopCh)
	}()
	<-stopCh
}

// export exports syncer metrics.
func (cm *NEGControllerMetrics) export() {
	lpMetrics := cm.computeLabelMetrics()
	NumberOfEndpoints.WithLabelValues(totalEndpoints).Set(float64(lpMetrics.NumberOfEndpoints))
	NumberOfEndpoints.WithLabelValues(epWithAnnotation).Set(float64(lpMetrics.EndpointsWithAnnotation))

	stateCount, syncerCount := cm.computeSyncerStateMetrics()
	//Reset metric so non-existent keys are now 0
	SyncerCountBySyncResult.Reset()
	for syncerState, count := range stateCount {
		SyncerCountBySyncResult.WithLabelValues(string(syncerState.lastSyncResult), strconv.FormatBool(syncerState.inErrorState)).Set(float64(count))
	}

	epStateCount, epsStateCount, epCount, epsCount := cm.computeEndpointStateMetrics()
	for state, count := range epStateCount {
		syncerEndpointState.WithLabelValues(string(state)).Set(float64(count))
	}
	for state, count := range epsStateCount {
		syncerEndpointSliceState.WithLabelValues(string(state)).Set(float64(count))
	}

	negCounts := cm.computeNegCounts()
	//Clear existing metrics (ensures that keys that don't exist anymore are reset)
	negsManagedCount.Reset()
	for key, count := range negCounts {
		negsManagedCount.WithLabelValues(key.location, key.endpointType).Set(float64(count))
	}

	cm.logger.V(3).Info("Exporting syncer related metrics", "Syncer count", syncerCount,
		"Network Endpoint Count", lpMetrics.NumberOfEndpoints,
		"Endpoint Count From EPS", epCount,
		"Endpoint Slice Count", epsCount,
		"NEG Count", fmt.Sprintf("%+v", negCounts),
	)

	finishedDurations, longestUnfinishedDurations := cm.computeDualStackMigrationDurations()
	for _, duration := range finishedDurations {
		DualStackMigrationFinishedDurations.Observe(float64(duration))
	}
	DualStackMigrationLongestUnfinishedDuration.Set(float64(longestUnfinishedDurations))

	syncerCountByEndpointType, migrationEndpointCount, migrationServicesCount := cm.computeDualStackMigrationCounts()
	for endpointType, count := range syncerCountByEndpointType {
		SyncerCountByEndpointType.WithLabelValues(endpointType).Set(float64(count))
	}
	syncerEndpointState.WithLabelValues(string(negtypes.DualStackMigration)).Set(float64(migrationEndpointCount))
	DualStackMigrationServiceCount.Set(float64(migrationServicesCount))

	cm.logger.V(3).Info("Exported DualStack Migration metrics")

	negCount := cm.computeNegMetrics()
	for feature, count := range negCount {
		networkEndpointGroupCount.WithLabelValues(feature.String()).Set(float64(count))
	}
	cm.logger.V(3).Info("Exported NEG usage metrics", "NEG count", fmt.Sprintf("%#v", negCount))
}

// UpdateSyncerStatusInMetrics update the status of syncer based on the error
func (cm *NEGControllerMetrics) UpdateSyncerStatusInMetrics(key negtypes.NegSyncerKey, err error, inErrorState bool) {
	reason := negtypes.ReasonSuccess
	if err != nil {
		syncErr := negtypes.ClassifyError(err)
		reason = syncErr.Reason
	}
	syncerSyncResult.WithLabelValues(string(reason)).Inc()
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.syncerStateMap == nil {
		cm.syncerStateMap = make(map[negtypes.NegSyncerKey]syncerState)
		cm.logger.V(3).Info("Syncer Metrics failed to initialize correctly, reinitializing syncerStateMap: %v", cm.syncerStateMap)
	}
	cm.syncerStateMap[key] = syncerState{lastSyncResult: reason, inErrorState: inErrorState}
}

func (cm *NEGControllerMetrics) UpdateSyncerEPMetrics(key negtypes.NegSyncerKey, endpointCount, endpointSliceCount negtypes.StateCountMap) {
	cm.logger.V(3).Info("Updating syncer endpoint", "syncerKey", key)
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.syncerEndpointStateMap == nil {
		cm.syncerEndpointStateMap = make(map[negtypes.NegSyncerKey]negtypes.StateCountMap)
		cm.logger.V(3).Info("Syncer Metrics failed to initialize correctly, reinitializing syncerEndpointStateMap")
	}
	cm.syncerEndpointStateMap[key] = endpointCount

	if cm.syncerEndpointSliceStateMap == nil {
		cm.syncerEndpointSliceStateMap = make(map[negtypes.NegSyncerKey]negtypes.StateCountMap)
		cm.logger.V(3).Info("Syncer Metrics failed to initialize correctly, reinitializing syncerEndpointSliceStateMap")
	}
	cm.syncerEndpointSliceStateMap[key] = endpointSliceCount
}

func (cm *NEGControllerMetrics) SetLabelPropagationStats(key negtypes.NegSyncerKey, labelstatLabelPropagationStats LabelPropagationStats) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.syncerLabelProagationStats == nil {
		cm.syncerLabelProagationStats = make(map[negtypes.NegSyncerKey]LabelPropagationStats)
		cm.logger.V(3).Info("Syncer Metrics failed to initialize correctly, reinitializing syncerLabelProagationStats")
	}
	cm.syncerLabelProagationStats[key] = labelstatLabelPropagationStats
}

// DeleteSyncer will reset any metrics for the syncer corresponding to `key`. It
// should be invoked when a Syncer has been stopped.
func (cm *NEGControllerMetrics) DeleteSyncer(key negtypes.NegSyncerKey) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	delete(cm.syncerStateMap, key)
	delete(cm.syncerEndpointStateMap, key)
	delete(cm.syncerEndpointSliceStateMap, key)
	delete(cm.syncerLabelProagationStats, key)
	delete(cm.dualStackMigrationStartTime, key)
	delete(cm.dualStackMigrationEndTime, key)
	delete(cm.endpointsCountPerType, key)
	delete(cm.syncerNegCount, key)
}

// computeLabelMetrics aggregates label propagation metrics.
func (cm *NEGControllerMetrics) computeLabelMetrics() LabelPropagationMetrics {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	lpMetrics := LabelPropagationMetrics{}
	for _, stats := range cm.syncerLabelProagationStats {
		lpMetrics.EndpointsWithAnnotation += stats.EndpointsWithAnnotation
		lpMetrics.NumberOfEndpoints += stats.NumberOfEndpoints
	}
	return lpMetrics
}

func (cm *NEGControllerMetrics) computeSyncerStateMetrics() (syncerStateCount, int) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	stateCount := make(syncerStateCount)
	syncerCount := 0
	for _, syncerState := range cm.syncerStateMap {
		stateCount[syncerState] += 1
		syncerCount++
	}
	return stateCount, syncerCount
}

// computeSyncerEndpointStateMetrics aggregates endpoint and endpoint slice counts from all syncers
func (cm *NEGControllerMetrics) computeEndpointStateMetrics() (negtypes.StateCountMap, negtypes.StateCountMap, int, int) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	var epCount, epsCount int
	epStateCount := negtypes.StateCountMap{}
	epsStateCount := negtypes.StateCountMap{}
	// collect count from each syncer
	for _, epState := range cm.syncerEndpointStateMap {
		for _, state := range negtypes.StatesForEndpointMetrics() {
			epStateCount[state] += epState[state]
			epCount += epState[state]
		}
	}
	for _, epsState := range cm.syncerEndpointSliceStateMap {
		for _, state := range negtypes.StatesForEndpointMetrics() {
			epsStateCount[state] += epsState[state]
			epsCount += epsState[state]
		}
	}
	return epStateCount, epsStateCount, epCount, epsCount
}

// CollectDualStackMigrationMetrics will be used by dualstack.Migrator to export
// metrics.
func (cm *NEGControllerMetrics) CollectDualStackMigrationMetrics(key negtypes.NegSyncerKey, committedEndpoints map[string]negtypes.NetworkEndpointSet, migrationCount int) {
	cm.updateMigrationStartAndEndTime(key, migrationCount)
	cm.updateEndpointsCountPerType(key, committedEndpoints, migrationCount)
}

func (cm *NEGControllerMetrics) updateMigrationStartAndEndTime(key negtypes.NegSyncerKey, migrationCount int) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	_, hasStartTime := cm.dualStackMigrationStartTime[key]
	_, hasEndTime := cm.dualStackMigrationEndTime[key]

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
		cm.dualStackMigrationEndTime[key] = cm.clock.Now()
		return
	}

	//
	// Migration has started or it was already in progress.
	//
	if hasEndTime {
		// A previous migration was completed but there are still migrating
		// endpoints so extend the previous migration time.
		delete(cm.dualStackMigrationEndTime, key)
	}
	if hasStartTime {
		// Migration was already started in some previous invocation.
		return
	}
	cm.dualStackMigrationStartTime[key] = cm.clock.Now()
}

func (cm *NEGControllerMetrics) updateEndpointsCountPerType(key negtypes.NegSyncerKey, committedEndpoints map[string]negtypes.NetworkEndpointSet, migrationCount int) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

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
	cm.endpointsCountPerType[key] = map[string]int{
		ipv4EndpointType:      ipv4OnlyCount,
		ipv6EndpointType:      ipv6OnlyCount,
		dualStackEndpointType: dualStackCount,
		migrationEndpointType: migrationCount,
	}
}

func (cm *NEGControllerMetrics) computeDualStackMigrationDurations() ([]int, int) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	finishedDurations, longestUnfinishedDuration := make([]int, 0), 0
	for key, startTime := range cm.dualStackMigrationStartTime {
		endTime, ok := cm.dualStackMigrationEndTime[key]
		if !ok {
			if curUnfinishedDuration := int(cm.clock.Since(startTime).Seconds()); curUnfinishedDuration > longestUnfinishedDuration {
				longestUnfinishedDuration = curUnfinishedDuration
			}
			continue
		}
		finishedDurations = append(finishedDurations, int(endTime.Sub(startTime).Seconds()))
		// Prevent metrics from being re-emitted by deleting the syncer key whose
		// migrations have finished.
		delete(cm.dualStackMigrationStartTime, key)
		delete(cm.dualStackMigrationEndTime, key)
	}

	return finishedDurations, longestUnfinishedDuration
}

func (cm *NEGControllerMetrics) computeDualStackMigrationCounts() (map[string]int, int, int) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

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

	for syncerKey, syncerEndpointsCountPerType := range cm.endpointsCountPerType {
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

func (cm *NEGControllerMetrics) UpdateSyncerNegCount(key negtypes.NegSyncerKey, negsByLocation map[string]int) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.syncerNegCount[key] = negsByLocation
}

func (cm *NEGControllerMetrics) computeNegCounts() map[negLocTypeKey]int {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	negCountByLocation := make(map[negLocTypeKey]int)

	for syncerKey, syncerNegCount := range cm.syncerNegCount {
		for location, count := range syncerNegCount {
			key := negLocTypeKey{location: location, endpointType: string(syncerKey.NegType)}
			negCountByLocation[key] += count
		}
	}

	return negCountByLocation
}

// SetNegService implements NegMetricsCollector.
func (cm *NEGControllerMetrics) SetNegService(svcKey string, negState NegServiceState) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.negMap == nil {
		klog.Fatalf("Ingress Metrics failed to initialize correctly.")
	}
	cm.negMap[svcKey] = negState
}

// DeleteNegService implements NegMetricsCollector.
func (cm *NEGControllerMetrics) DeleteNegService(svcKey string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	delete(cm.negMap, svcKey)
}

// computeNegMetrics aggregates NEG metrics in the cache
func (cm *NEGControllerMetrics) computeNegMetrics() map[feature]int {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	klog.V(4).Infof("Computing NEG usage metrics from neg state map: %#v", cm.negMap)

	counts := map[feature]int{
		standaloneNeg:  0,
		ingressNeg:     0,
		asmNeg:         0,
		neg:            0,
		vmIpNeg:        0,
		vmIpNegLocal:   0,
		vmIpNegCluster: 0,
		customNamedNeg: 0,
		negInSuccess:   0,
		negInError:     0,
	}

	for key, negState := range cm.negMap {
		klog.V(6).Infof("For service %s, it has standaloneNegs:%d, ingressNegs:%d, asmNeg:%d and vmPrimaryNeg:%v",
			key, negState.StandaloneNeg, negState.IngressNeg, negState.AsmNeg, negState.VmIpNeg)
		counts[standaloneNeg] += negState.StandaloneNeg
		counts[ingressNeg] += negState.IngressNeg
		counts[asmNeg] += negState.AsmNeg
		counts[neg] += negState.AsmNeg + negState.StandaloneNeg + negState.IngressNeg
		counts[customNamedNeg] += negState.CustomNamedNeg
		counts[negInSuccess] += negState.SuccessfulNeg
		counts[negInError] += negState.ErrorNeg
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
