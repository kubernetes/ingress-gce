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

const (
	syncResultLabel = "result"
	syncResultKey   = "sync_result"

	syncerStatusLabel = "status"
	syncerStatusKey   = "syncer_status"
)

var (
	// syncerSyncerStatus tracks the count of syncer in different statuses
	syncerSyncerStatus = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: negControllerSubsystem,
			Name:      syncerStatusKey,
			Help:      "Current count of syncers in each status",
		},
		[]string{syncerStatusLabel},
	)

	// syncerSyncResult tracks the count for each sync result
	syncerSyncResult = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: negControllerSubsystem,
			Name:      syncResultKey,
			Help:      "Current count for each sync result",
		},
		[]string{syncResultLabel},
	)
)

type SyncerMetricsCollector interface {
	UpdateSyncer(key negtypes.NegSyncerKey, result *negtypes.NegSyncResult)
}

type SyncerMetrics struct {
	// syncerStatusmap tracks the status of each syncer
	syncerStatusMap map[negtypes.NegSyncerKey]string
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
		syncerStatusMap: make(map[negtypes.NegSyncerKey]string),
		metricsInterval: exportInterval,
		logger:          logger.WithName("NegMetricsCollector"),
	}
}

// FakeSyncerMetrics creates new NegMetricsCollector with fixed 5 second metricsInterval, to be used in tests
func FakeSyncerMetrics() *SyncerMetrics {
	return NewNegMetricsCollector(5*time.Second, klog.TODO())
}

func RegisterSyncerMetrics() {
	prometheus.MustRegister(syncerSyncResult)
	prometheus.MustRegister(syncerSyncerStatus)
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
	statusCount, syncerCount := sm.computeSyncerStatusMetrics()
	sm.logger.V(3).Info("Exporting syncer status metrics.", "Syncer count", syncerCount)
	for syncerStatus, count := range statusCount {
		syncerSyncerStatus.WithLabelValues(syncerStatus).Set(float64(count))
	}
}

// UpdateSyncer updates the count of sync results based on the result/error of sync
func (sm *SyncerMetrics) UpdateSyncer(key negtypes.NegSyncerKey, syncResult *negtypes.NegSyncResult) {
	syncerSyncResult.WithLabelValues(syncResult.Result).Inc()
	syncerStatus := negtypes.GetSyncerStatus(syncResult.Result)

	sm.mu.Lock()
	defer sm.mu.Unlock()
	if sm.syncerStatusMap == nil {
		sm.syncerStatusMap = make(map[negtypes.NegSyncerKey]string)
		sm.logger.V(3).Info("Syncer Metrics failed to initialize correctly, reinitializing syncerStatusMap: %v", sm.syncerStatusMap)
	}
	sm.syncerStatusMap[key] = syncerStatus
}

func (sm *SyncerMetrics) computeSyncerStatusMetrics() (map[string]int, int) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.logger.V(3).Info("computing syncer status metrics")

	statusCount := map[string]int{
		negtypes.SyncerEPCountsDiffer:         0,
		negtypes.SyncerEPMissingNodeName:      0,
		negtypes.SyncerNodeNotFound:           0,
		negtypes.SyncerEPMissingZone:          0,
		negtypes.SyncerEPSEndpointCountZero:   0,
		negtypes.SyncerEPCalculationCountZero: 0,
		negtypes.SyncerInvalidEPAttach:        0,
		negtypes.SyncerInvalidEPDetach:        0,
		negtypes.SyncerNegNotFound:            0,
		negtypes.SyncerCurrentEPNotFound:      0,
		negtypes.SyncerEPSNotFound:            0,
		negtypes.SyncerOtherError:             0,
		negtypes.SyncerSuccess:                0,
	}
	syncerCount := 0
	for _, syncerStatus := range sm.syncerStatusMap {
		statusCount[syncerStatus] += 1
		syncerCount += 1
	}
	return statusCount, syncerCount
}
