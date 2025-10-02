/*
Copyright 2025 The Kubernetes Authors.

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
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/apimachinery/pkg/util/wait"
	pscmetrics "k8s.io/ingress-gce/pkg/psc/metrics"
	"k8s.io/klog/v2"
	"k8s.io/utils/clock"
)

var register sync.Once

// RegisterSyncerMetrics registers syncer related metrics
func RegisterMetrics() {
	register.Do(func() {
		prometheus.MustRegister(serviceAttachmentCount)
		prometheus.MustRegister(serviceCount)
	})
}

type PSCMetricsCollector struct {
	clock clock.Clock
	// duration between metrics exports
	metricsInterval time.Duration

	mu sync.Mutex

	// pscMap stores information about the ServiceAttachments managed by the PSC Controller
	pscMap map[string]pscmetrics.PSCState

	serviceMap map[string]struct{}

	// logger logs message related to NegMetricsCollector
	logger klog.Logger
}

func NewPSCMetricsCollector(exportInterval time.Duration, logger klog.Logger) *PSCMetricsCollector {
	return &PSCMetricsCollector{
		pscMap:          make(map[string]pscmetrics.PSCState),
		serviceMap:      make(map[string]struct{}),
		clock:           clock.RealClock{},
		metricsInterval: exportInterval,
		logger:          logger.WithName("PSCMetricsCollectorCollector"),
	}
}

// FakeSyncerMetrics creates new NegMetricsCollector with fixed 5 second metricsInterval, to be used in tests
func FakePSCMetrics() *PSCMetricsCollector {
	return NewPSCMetricsCollector(5*time.Second, klog.TODO())
}

func (m *PSCMetricsCollector) Run(stopCh <-chan struct{}) {
	m.logger.V(3).Info("PSC Metrics initialized.", "exportInterval", m.metricsInterval)
	// Compute and export metrics periodically.
	go func() {
		time.Sleep(m.metricsInterval)
		wait.Until(m.export, m.metricsInterval, stopCh)
	}()
	<-stopCh
}

// SetServiceAttachment adds sa state to the map to be counted during metrics computation.
// SetServiceAttachment implements PSCMetricsCollectorCollector.
func (m *PSCMetricsCollector) SetServiceAttachment(saKey string, state pscmetrics.PSCState) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.pscMap == nil {
		klog.Fatalf("PSC Metrics failed to initialize correctly.")
	}
	m.pscMap[saKey] = state
}

// DeleteServiceAttachment removes sa state to the map to be counted during metrics computation.
// DeleteServiceAttachment implements PSCMetricsCollectorCollector.
func (m *PSCMetricsCollector) DeleteServiceAttachment(saKey string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.pscMap, saKey)
}

// SetService adds the service to the map to be counted during metrics computation.
// SetService implements PSCMetricsCollectorCollector.
func (m *PSCMetricsCollector) SetService(serviceKey string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.serviceMap == nil {
		klog.Fatalf("PSC Metrics failed to initialize correctly.")
	}
	m.serviceMap[serviceKey] = struct{}{}
}

// DeleteService removes the service from the map to be counted during metrics computation.
// DeleteService implements PSCMetricsCollectorCollector.
func (m *PSCMetricsCollector) DeleteService(serviceKey string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.serviceMap, serviceKey)
}

func (m *PSCMetricsCollector) computePSCMetrics() map[feature]int {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logger.V(4).Info("Compute PSC Usage metrics from psc state map", "pscStateMap", m.pscMap)

	counts := map[feature]int{
		sa:          0,
		saInSuccess: 0,
		saInError:   0,
	}

	for _, state := range m.pscMap {
		counts[sa]++
		if state.InSuccess {
			counts[saInSuccess]++
		} else {
			counts[saInError]++
		}
	}
	return counts
}

func (m *PSCMetricsCollector) computeServiceMetrics() map[feature]int {
	return map[feature]int{
		services: len(m.serviceMap),
	}
}

// export exports syncer metrics.
func (m *PSCMetricsCollector) export() {
	defer func() {
		if r := recover(); r != nil {
			m.logger.Error(nil, "failed to export PC metrics", "recoverMessage", r)
		}
	}()

	saCount := m.computePSCMetrics()
	m.logger.V(3).Info("Exporting PSC Usage Metrics", "serviceAttachmentsCount", saCount)
	for feature, count := range saCount {
		serviceAttachmentCount.With(prometheus.Labels{"feature": feature.String()}).Set(float64(count))
	}
	m.logger.V(3).Info("Exported PSC Usage Metrics", "serviceAttachmentsCount", saCount)

	services := m.computeServiceMetrics()
	m.logger.V(3).Info("Exporting Service Metrics", "serviceCount", serviceCount)
	for feature, count := range services {
		serviceCount.With(prometheus.Labels{"feature": feature.String()}).Set(float64(count))
	}
	m.logger.V(3).Info("Exported Service Metrics", "serviceCount", serviceCount)
}
