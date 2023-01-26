/*
Copyright 2018 The Kubernetes Authors.

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
	"errors"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/apimachinery/pkg/util/wait"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/klog/v2"
)

const (
	syncerStatusLabel = "status"
	syncResultLabel   = "result"
	syncerStatusKey   = "syncer_status"
	syncResultKey     = "sync_result"

	EPSDup     = "EPSWithDuplicateEP"
	EPSMissing = "EPSWithMissingEP"
	EPSTotal   = "TotalEPS"
)

var (
	syncerSyncerStatus = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: negControllerSubsystem,
			Name:      syncerStatusKey,
			Help:      "Current count of syncers in each status",
		},
		[]string{syncerStatusLabel},
	)

	syncerSyncResult = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: negControllerSubsystem,
			Name:      syncResultKey,
			Help:      "Current count for each sync error",
		},
		[]string{syncResultLabel},
	)
)

type SyncerMetricsCollector interface {
	UpdateSyncer(key negtypes.NegSyncerKey, err error)
}

type SyncerMetrics struct {
	// syncerStatusMap tracks the status of each syncer
	syncerStatusMap map[negtypes.NegSyncerKey]syncerStatus
	// countSinceLastExport tracks the count of errors occured since last export
	countSinceLastExport map[syncError]int
	// mu avoid race conditions and ensure correctness of metrics
	mu sync.Mutex
	// duration between metrics exports
	metricsInterval time.Duration
}

// init registers ingress usage metrics.
func init() {
	klog.V(3).Infof("Registering sync result metrics %v", syncerSyncResult)
	prometheus.MustRegister(syncerSyncResult)
	klog.V(3).Infof("Registering syncer status metrics %v", syncerSyncerStatus)
	prometheus.MustRegister(syncerSyncerStatus)

}

// NewNEGMetricsCollector initializes SyncerMetrics and starts a go routine to compute and export metrics periodically.
func NewNegMetricsCollector(exportInterval time.Duration) *SyncerMetrics {
	return &SyncerMetrics{
		countSinceLastExport: map[syncError]int{
			ErrEPCountsDiffer:         0,
			ErrEPMissingNodeName:      0,
			ErrEPMissingZone:          0,
			ErrInvalidEPAttach:        0,
			ErrInvalidEPDetach:        0,
			ErrEPSEndpointCountZero:   0,
			ErrEPCalculationCountZero: 0,
			ErrNegNotFound:            0,
			ErrCurrentEPNotFound:      0,
			ErrEPSNotFound:            0,
			ErrNodeNotFound:           0,
			ErrOtherError:             0,
			Success:                   0,
		},
		syncerStatusMap: make(map[negtypes.NegSyncerKey]syncerStatus),
		metricsInterval: exportInterval,
	}
}

// FakeSyncerMetrics creates new NegMetricsCollector with fixed 5 second metricsInterval, to be used in tests
func FakeSyncerMetrics() *SyncerMetrics {
	return NewNegMetricsCollector(5 * time.Second)
}

// UpdateSyncer updates the count of sync results based on the result/error of sync
func (im *SyncerMetrics) UpdateSyncer(key negtypes.NegSyncerKey, err error) {
	im.mu.Lock()
	defer im.mu.Unlock()
	if im.syncerStatusMap == nil {
		klog.Fatalf("Syncer Metrics failed to initialize correctly, syncerStatusMap: %v", im.syncerStatusMap)
	}
	if im.countSinceLastExport == nil {
		klog.Fatalf("Syncer Metrics failed to initialize correctly, countSinceLastExport: %v", im.countSinceLastExport)
	}
	if err == nil {
		im.syncerStatusMap[key] = syncerInSuccess
		im.countSinceLastExport[Success] += 1
	} else {
		syncErr := errors.Unwrap(err).(syncError)
		status := getSyncerStatus(syncErr)
		im.syncerStatusMap[key] = status
		im.countSinceLastExport[syncErr] += 1
	}
}

func (im *SyncerMetrics) Run(stopCh <-chan struct{}) {
	klog.V(3).Infof("Syncer Metrics initialized. Metrics will be exported at an interval of %v", im.metricsInterval)
	// Compute and export metrics periodically.
	go func() {
		time.Sleep(im.metricsInterval)
		wait.Until(im.export, im.metricsInterval, stopCh)
	}()
	<-stopCh
}

// export exports syncer metrics.
func (im *SyncerMetrics) export() {
	statusCount, syncerCount := im.computeSyncerStatusMetrics()
	klog.V(3).Infof("Exporting syncer status metrics. Syncer count: %d", syncerCount)
	for syncerStatus, count := range statusCount {
		syncerSyncerStatus.WithLabelValues(syncerStatus.String()).Set(float64(count))
	}

	klog.V(3).Infof("Exporting sync result metrics.")
	for syncError, increment := range im.countSinceLastExport {
		syncerSyncResult.WithLabelValues(syncError.Reason).Add(float64(increment))
		im.countSinceLastExport[syncError] = 0
	}
}

func (im *SyncerMetrics) computeSyncerStatusMetrics() (map[syncerStatus]int, int) {
	im.mu.Lock()
	defer im.mu.Unlock()
	statusCount := map[syncerStatus]int{
		syncerEPCountsDiffer:         0,
		syncerEPMissingNodeName:      0,
		syncerEPMissingZone:          0,
		syncerInvalidEPAttach:        0,
		syncerInvalidEPDetach:        0,
		syncerEPSEndpointCountZero:   0,
		syncerEPCalculationCountZero: 0,
		syncerNegNotFound:            0,
		syncerCurrentEPNotFound:      0,
		syncerEPSNotFound:            0,
		syncerNodeNotFound:           0,
		syncerOtherError:             0,
		syncerInSuccess:              0,
	}
	syncerCount := 0
	for _, syncerStatus := range im.syncerStatusMap {
		statusCount[syncerStatus] += 1
		syncerCount += 1
	}
	return statusCount, syncerCount
}
