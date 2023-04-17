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
	// UpdateSyncerStatusInMetrics update the status of corresponding syncer based on the sync error
	UpdateSyncerStatusInMetrics(key negtypes.NegSyncerKey, err error)

	SetSyncerEPMetrics(key negtypes.NegSyncerKey, epState *negtypes.SyncerEPStat)
	SetLabelPropagationStats(key negtypes.NegSyncerKey, labelstatLabelPropagationStats LabelPropagationStats)
}

type SyncerMetrics struct {
	// syncerStatusMap tracks the status of each syncer
	syncerStatusMap map[negtypes.NegSyncerKey]negtypes.Reason
	// syncerEndpointStateMap is a map between syncer and endpoint state counts
	syncerEndpointStateMap map[negtypes.NegSyncerKey]negtypes.StateCountMap
	// syncerEPSStateMap is a map between syncer and endpoint slice state counts
	syncerEPSStateMap map[negtypes.NegSyncerKey]negtypes.StateCountMap
	// syncerLabelProagationStats is a map between syncer and label propagation stats.
	syncerLabelProagationStats map[negtypes.NegSyncerKey]LabelPropagationStats
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
		syncerStatusMap:            make(map[negtypes.NegSyncerKey]negtypes.Reason),
		syncerEndpointStateMap:     make(map[negtypes.NegSyncerKey]negtypes.StateCountMap),
		syncerEPSStateMap:          make(map[negtypes.NegSyncerKey]negtypes.StateCountMap),
		syncerLabelProagationStats: make(map[negtypes.NegSyncerKey]LabelPropagationStats),
		metricsInterval:            exportInterval,
		logger:                     logger.WithName("NegMetricsCollector"),
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
	lpMetrics := sm.computeLabelMetrics()
	NumberOfEndpoints.WithLabelValues(totalEndpoints).Set(float64(lpMetrics.NumberOfEndpoints))
	NumberOfEndpoints.WithLabelValues(epWithAnnotation).Set(float64(lpMetrics.EndpointsWithAnnotation))
	sm.logger.V(3).Info("Exporting syncer related metrics", "Number of Endpoints", lpMetrics.NumberOfEndpoints)
}

// UpdateSyncerStatusInMetrics update the status of syncer based on the error
func (sm *SyncerMetrics) UpdateSyncerStatusInMetrics(key negtypes.NegSyncerKey, err error) {
	reason := negtypes.ReasonSuccess
	if err != nil {
		syncErr := negtypes.ClassifyError(err)
		reason = syncErr.Reason
	}
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if sm.syncerStatusMap == nil {
		sm.syncerStatusMap = make(map[negtypes.NegSyncerKey]negtypes.Reason)
		sm.logger.V(3).Info("Syncer Metrics failed to initialize correctly, reinitializing syncerStatusMap: %v", sm.syncerStatusMap)
	}
	sm.syncerStatusMap[key] = reason
}

// SetSyncerEPMetrics update the endpoint count based on the endpointStat
func (sm *SyncerMetrics) SetSyncerEPMetrics(key negtypes.NegSyncerKey, endpointStat *negtypes.SyncerEPStat) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if sm.syncerEndpointStateMap == nil {
		sm.syncerEndpointStateMap = make(map[negtypes.NegSyncerKey]negtypes.StateCountMap)
		sm.logger.V(3).Info("Syncer Metrics failed to initialize correctly, reinitializing syncerEPStateMap")
	}
	sm.syncerEndpointStateMap[key] = endpointStat.EndpointStateCount

	if sm.syncerEPSStateMap == nil {
		sm.syncerEPSStateMap = make(map[negtypes.NegSyncerKey]negtypes.StateCountMap)
		sm.logger.V(3).Info("Syncer Metrics failed to initialize correctly, reinitializing syncerEPSStateMap")
	}
	sm.syncerEPSStateMap[key] = endpointStat.EndpointSliceStateCount
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
