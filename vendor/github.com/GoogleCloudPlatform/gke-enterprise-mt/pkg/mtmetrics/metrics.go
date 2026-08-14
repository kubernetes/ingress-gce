/*
 * Copyright 2026 Google LLC
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     https://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
/*
Package mtmetrics provides a multi-tenant metrics framework that supports
dual-emission of metrics to both a tenant-local registry and a shared global
registry, with stateful tracking and aggregation of gauge values across tenants.
*/
package mtmetrics

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
)

// CounterVec defines the interface for a counter vector, which partitions a
// counter metric into individual counters distinguished by label values.
type CounterVec interface {
	// WithLabelValues returns the Counter for the given slice of label values
	// (in the same order as the variable label names in the CounterOpts).
	// In multi-tenant mode, this may automatically append tenant-specific labels.
	WithLabelValues(lvs ...string) prometheus.Counter
	// Reset deletes all metrics in this vector.
	Reset()
}

// GaugeVec defines the interface for a gauge vector, which partitions a gauge
// metric into individual gauges distinguished by label values.
type GaugeVec interface {
	// WithLabelValues returns the Gauge for the given slice of label values.
	// In multi-tenant mode, this may automatically append tenant-specific labels.
	WithLabelValues(lvs ...string) prometheus.Gauge
	// DeleteLabelValues deletes the metric for the given slice of label values.
	// Returns true if the metric was deleted.
	DeleteLabelValues(lvs ...string) bool
	// Reset deletes all metrics in this vector.
	Reset()
}

// ObserverVec defines the interface for an observer vector (typically used by
// Histograms), which partitions an observer metric into individual observers
// distinguished by label values.
type ObserverVec interface {
	// WithLabelValues returns the Observer for the given slice of label values.
	// In multi-tenant mode, this may automatically append tenant-specific labels.
	WithLabelValues(lvs ...string) prometheus.Observer
	// Reset deletes all metrics in this vector.
	Reset()
}

// MetricFactory defines the interface for creating metrics. It abstracts the
// underlying metrics registry, allowing for standard single-tenant metrics
// or multi-tenant dual-emitting metrics.
type MetricFactory interface {
	// NewCounterVec creates and registers a CounterVec.
	NewCounterVec(opts prometheus.CounterOpts, labelNames []string) (CounterVec, error)
	// NewGaugeVec creates and registers a GaugeVec.
	NewGaugeVec(opts prometheus.GaugeOpts, labelNames []string) (GaugeVec, error)
	// NewHistogramVec creates and registers an ObserverVec.
	NewHistogramVec(opts prometheus.HistogramOpts, labelNames []string) (ObserverVec, error)

	// NewCounter creates and registers a single Counter.
	NewCounter(opts prometheus.CounterOpts) (prometheus.Counter, error)
	// NewGauge creates and registers a single Gauge.
	NewGauge(opts prometheus.GaugeOpts) (prometheus.Gauge, error)
	// NewHistogram creates and registers a single Histogram.
	NewHistogram(opts prometheus.HistogramOpts) (prometheus.Histogram, error)

	// Cleanup cleans up resources associated with the factory (e.g., removing
	// tenant-specific metrics from shared aggregators).
	Cleanup()
}

// stdMetricFactory is a MetricFactory implementation that creates standard
// Prometheus metrics and registers them with a provided Prometheus Registerer.
// It handles duplicate registrations by returning the already registered collector
// if the types match.
type StdMetricFactory struct {
	reg prometheus.Registerer
}

// NewStdMetricFactory creates a new StdMetricFactory that registers metrics to the
// provided registerer. This is typically used in single-tenant mode or for
// registering global shared metrics.
func NewStdMetricFactory(r prometheus.Registerer) *StdMetricFactory {
	return &StdMetricFactory{reg: r}
}

// Cleanup is a no-op for StdMetricFactory. In standard single-tenant clusters,
// metrics are registered once at startup and live for the lifetime of the process.
// Dynamic cleanup is only performed in MT mode (mtMetricFactory) during tenant churn.
func (f *StdMetricFactory) Cleanup() {}

// NewCounterVec creates a new CounterVec and registers it to the standard registry.
// If the metric is already registered, it returns the existing CounterVec.
func (f *StdMetricFactory) NewCounterVec(opts prometheus.CounterOpts, labelNames []string) (CounterVec, error) {
	vec := prometheus.NewCounterVec(opts, labelNames)
	if err := f.reg.Register(vec); err != nil {
		if are, ok := err.(prometheus.AlreadyRegisteredError); ok {
			var okAssert bool
			vec, okAssert = are.ExistingCollector.(*prometheus.CounterVec)
			if !okAssert {
				return nil, fmt.Errorf("metric %q already registered with different type", opts.Name)
			}
		} else {
			return nil, err
		}
	}
	return vec, nil
}

// NewGaugeVec creates a new GaugeVec and registers it to the standard registry.
// If the metric is already registered, it returns the existing GaugeVec.
func (f *StdMetricFactory) NewGaugeVec(opts prometheus.GaugeOpts, labelNames []string) (GaugeVec, error) {
	vec := prometheus.NewGaugeVec(opts, labelNames)
	if err := f.reg.Register(vec); err != nil {
		if are, ok := err.(prometheus.AlreadyRegisteredError); ok {
			var okAssert bool
			vec, okAssert = are.ExistingCollector.(*prometheus.GaugeVec)
			if !okAssert {
				return nil, fmt.Errorf("metric %q already registered with different type", opts.Name)
			}
		} else {
			return nil, err
		}
	}
	return vec, nil
}

// NewHistogramVec creates a new HistogramVec (returned as ObserverVec) and
// registers it to the standard registry. If the metric is already registered,
// it returns the existing HistogramVec.
func (f *StdMetricFactory) NewHistogramVec(opts prometheus.HistogramOpts, labelNames []string) (ObserverVec, error) {
	vec := prometheus.NewHistogramVec(opts, labelNames)
	if err := f.reg.Register(vec); err != nil {
		if are, ok := err.(prometheus.AlreadyRegisteredError); ok {
			var okAssert bool
			vec, okAssert = are.ExistingCollector.(*prometheus.HistogramVec)
			if !okAssert {
				return nil, fmt.Errorf("metric %q already registered with different type", opts.Name)
			}
		} else {
			return nil, err
		}
	}
	return vec, nil
}

// NewCounter creates a new Counter and registers it to the standard registry.
// If the metric is already registered, it returns the existing Counter.
func (f *StdMetricFactory) NewCounter(opts prometheus.CounterOpts) (prometheus.Counter, error) {
	c := prometheus.NewCounter(opts)
	if err := f.reg.Register(c); err != nil {
		if are, ok := err.(prometheus.AlreadyRegisteredError); ok {
			var okAssert bool
			c, okAssert = are.ExistingCollector.(prometheus.Counter)
			if !okAssert {
				return nil, fmt.Errorf("metric %q already registered with different type", opts.Name)
			}
		} else {
			return nil, err
		}
	}
	return c, nil
}

// NewGauge creates a new Gauge and registers it to the standard registry.
// If the metric is already registered, it returns the existing Gauge.
func (f *StdMetricFactory) NewGauge(opts prometheus.GaugeOpts) (prometheus.Gauge, error) {
	g := prometheus.NewGauge(opts)
	if err := f.reg.Register(g); err != nil {
		if are, ok := err.(prometheus.AlreadyRegisteredError); ok {
			var okAssert bool
			g, okAssert = are.ExistingCollector.(prometheus.Gauge)
			if !okAssert {
				return nil, fmt.Errorf("metric %q already registered with different type", opts.Name)
			}
		} else {
			return nil, err
		}
	}
	return g, nil
}

// NewHistogram creates a new Histogram and registers it to the standard registry.
// If the metric is already registered, it returns the existing Histogram.
func (f *StdMetricFactory) NewHistogram(opts prometheus.HistogramOpts) (prometheus.Histogram, error) {
	h := prometheus.NewHistogram(opts)
	if err := f.reg.Register(h); err != nil {
		if are, ok := err.(prometheus.AlreadyRegisteredError); ok {
			var okAssert bool
			h, okAssert = are.ExistingCollector.(prometheus.Histogram)
			if !okAssert {
				return nil, fmt.Errorf("metric %q already registered with different type", opts.Name)
			}
		} else {
			return nil, err
		}
	}
	return h, nil
}
