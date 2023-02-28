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
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/apimachinery/pkg/util/wait"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/klog/v2"
)

var (
	syncResultLabel = "result"
	syncResultKey   = "sync_result"

	syncerStateLabel = "state"
	syncerStateKey   = "syncer_state"

	// syncerSyncResult tracks the count for each sync result
	syncerSyncResult = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: negControllerSubsystem,
			Name:      syncResultKey,
			Help:      "Current count for each sync result",
		},
		[]string{syncResultLabel},
	)

	// syncerSyncerState tracks the count of syncer in different states
	syncerSyncerState = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: negControllerSubsystem,
			Name:      syncerStateKey,
			Help:      "Current count of syncers in each state",
		},
		[]string{syncerStateLabel},
	)
)

type SyncerMetricsCollector interface {
	UpdateSyncer(key negtypes.NegSyncerKey, result *negtypes.NegSyncResult)
	SetSyncerEPMetrics(key negtypes.NegSyncerKey, epState *negtypes.SyncerEPStat)
}

type SyncerMetrics struct {
	// syncerStateMap tracks the state of each syncer
	syncerStateMap map[negtypes.NegSyncerKey]string
	// syncerEndpointStateMap is a map between syncer and endpoint state counts
	syncerEndpointStateMap map[negtypes.NegSyncerKey]negtypes.StateCountMap
	// syncerEPSStateMap is a map between syncer and endpoint slice state counts
	syncerEPSStateMap map[negtypes.NegSyncerKey]negtypes.StateCountMap
	// mu avoid race conditions and ensure correctness of metrics
	mu sync.Mutex
	// duration between metrics exports
	metricsInterval time.Duration
	// logger logs message related to NegMetricsCollector
	logger klog.Logger
}

// NewNEGMetricsCollector initializes SyncerMetrics and starts a go routine to compute and export metrics periodically.
func NewNegMetricsCollector(exportInterval time.Duration, logger klog.Logger) *SyncerMetrics {
	return &SyncerMetrics{
		syncerStateMap:         make(map[negtypes.NegSyncerKey]string),
		syncerEndpointStateMap: make(map[negtypes.NegSyncerKey]negtypes.StateCountMap),
		syncerEPSStateMap:      make(map[negtypes.NegSyncerKey]negtypes.StateCountMap),
		metricsInterval:        exportInterval,
		logger:                 logger.WithName("NegMetricsCollector"),
	}
}

// FakeSyncerMetrics creates new NegMetricsCollector with fixed 5 second metricsInterval, to be used in tests
func FakeSyncerMetrics() *SyncerMetrics {
	return NewNegMetricsCollector(5*time.Second, klog.TODO())
}

// RegisterSyncerMetrics registers syncer related metrics
func RegisterSyncerMetrics() {
	prometheus.MustRegister(syncerSyncResult)
	prometheus.MustRegister(syncerSyncerState)
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
	stateCount, syncerCount := sm.computeSyncerStateMetrics()
	sm.logger.V(3).Info("Exporting syncer state metrics.", "Syncer count", syncerCount)
	for syncerState, count := range stateCount {
		syncerSyncerState.WithLabelValues(syncerState).Set(float64(count))
	}
}

// UpdateSyncer update the state of corresponding syncer based on the syncResult.
func (sm *SyncerMetrics) UpdateSyncer(key negtypes.NegSyncerKey, syncResult *negtypes.NegSyncResult) {
	if syncResult.Result == negtypes.ResultInProgress {
		return
	}
	syncerSyncResult.WithLabelValues(syncResult.Result).Inc()

	sm.mu.Lock()
	defer sm.mu.Unlock()
	if sm.syncerStateMap == nil {
		sm.syncerStateMap = make(map[negtypes.NegSyncerKey]string)
		sm.logger.V(3).Info("Syncer Metrics failed to initialize correctly, reinitializing syncerStateMap: %v", sm.syncerStateMap)
	}
	sm.syncerStateMap[key] = syncResult.Result
}

// SetSyncerEPMetrics update the endpoint count based on the endpointStat
func (sm *SyncerMetrics) SetSyncerEPMetrics(key negtypes.NegSyncerKey, endpointStat *negtypes.SyncerEPStat) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if sm.syncerEndpointStateMap == nil {
		sm.syncerEndpointStateMap = make(map[negtypes.NegSyncerKey]negtypes.StateCountMap)
		sm.logger.V(3).Info("Syncer Metrics failed to initialize correctly, reinitializing syncerEPStateMap: %v", sm.syncerEndpointStateMap)
	}
	sm.syncerEndpointStateMap[key] = endpointStat.EndpointStateCount

	if sm.syncerEPSStateMap == nil {
		sm.syncerEPSStateMap = make(map[negtypes.NegSyncerKey]negtypes.StateCountMap)
		sm.logger.V(3).Info("Syncer Metrics failed to initialize correctly, reinitializing syncerEPSStateMap: %v", sm.syncerEPSStateMap)
	}
	sm.syncerEPSStateMap[key] = endpointStat.EndpointSliceStateCount
}

func (sm *SyncerMetrics) computeSyncerStateMetrics() (map[string]int, int) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.logger.V(3).Info("computing syncer state metrics")

	stateCount := map[string]int{
		negtypes.ResultEPCountsDiffer:         0,
		negtypes.ResultEPMissingNodeName:      0,
		negtypes.ResultNodeNotFound:           0,
		negtypes.ResultEPMissingZone:          0,
		negtypes.ResultEPSEndpointCountZero:   0,
		negtypes.ResultEPCalculationCountZero: 0,
		negtypes.ResultInvalidAPIResponse:     0,
		negtypes.ResultInvalidEPAttach:        0,
		negtypes.ResultInvalidEPDetach:        0,
		negtypes.ResultNegNotFound:            0,
		negtypes.ResultCurrentEPNotFound:      0,
		negtypes.ResultEPSNotFound:            0,
		negtypes.ResultOtherError:             0,
		negtypes.ResultSuccess:                0,
	}
	syncerCount := 0
	for _, syncerState := range sm.syncerStateMap {
		stateCount[syncerState] += 1
		syncerCount += 1
	}
	return stateCount, syncerCount
}
