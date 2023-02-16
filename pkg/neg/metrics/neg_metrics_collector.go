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

	"k8s.io/apimachinery/pkg/util/wait"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/klog/v2"
)

type SyncerMetricsCollector interface {
	UpdateSyncer(key negtypes.NegSyncerKey, result *negtypes.NegSyncResult)
}

type SyncerMetrics struct {
	// syncerStatusMap tracks the status of each syncer
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

// RegisterSyncerMetrics registers syncer related metrics
func RegisterSyncerMetrics() {
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
}

// UpdateSyncer update the status of corresponding syncer based on the syncResult.
func (sm *SyncerMetrics) UpdateSyncer(key negtypes.NegSyncerKey, syncResult *negtypes.NegSyncResult) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if sm.syncerStatusMap == nil {
		sm.syncerStatusMap = make(map[negtypes.NegSyncerKey]string)
		sm.logger.V(3).Info("Syncer Metrics failed to initialize correctly, reinitializing syncerStatusMap: %v", sm.syncerStatusMap)
	}
	sm.syncerStatusMap[key] = syncResult.Result
}
