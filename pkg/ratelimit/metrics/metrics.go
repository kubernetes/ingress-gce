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
	"k8s.io/ingress-gce/pkg/utils"
)

var register sync.Once

const rateLimiterSubsystem = "rate_limiter"

var (
	metricsLabels = []string{
		"api_key", // rate limiter key (e.g. ga.Addresses.Get)
	}

	RateLimitLatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Subsystem: rateLimiterSubsystem,
			Name:      "rate_limit_delay_seconds",
			Help:      "Latency of the RateLimiter Accept Operation",
			// custom buckets = [0.5s, 1s, 2s, 4s, 8s, 16s, 32s, 64s, 128s, 256s(~4min), 512s(~8min), 1024s(~17min), 2048s(~34min), 4096s(~68min), +Inf]
			Buckets: prometheus.ExponentialBuckets(0.5, 2, 14),
		},
		metricsLabels,
	)

	StrategyLatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Subsystem: rateLimiterSubsystem,
			Name:      "strategy_delay_seconds",
			Help:      "Latency of the strategyRateLimiter Accept Operation",
			// custom buckets = [0.5s, 1s, 2s, 4s, 8s, 16s, 32s, 64s, 128s, 256s(~4min), 512s(~8min), 1024s(~17min), 2048s(~34min), 4096s(~68min), +Inf]
			Buckets: prometheus.ExponentialBuckets(0.5, 2, 14),
		},
		metricsLabels,
	)

	StrategyUsedDelay = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Subsystem: rateLimiterSubsystem,
			Name:      "strategy_used_delay_seconds",
			Help:      "Delay in seconds used by the throttling strategy",
			// custom buckets = [0.1, 0.2s, 04.s, 0.8s, 1.6s, 3.2s, 6.4s, 12.8s, 25.6s, 51.2s, 102.4s, 204.8s(~3min), 409.6s(~7min), 819.2s(~14min), +Inf]
			Buckets: prometheus.ExponentialBuckets(0.1, 2, 14),
		},
		metricsLabels,
	)

	StrategyLockLatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Subsystem: rateLimiterSubsystem,
			Name:      "strategy_lock_delay_seconds",
			Help:      "Latency of the lock acquisition for the StrategyRateLimiter",
			// custom buckets = [1e-9s, 1e-8s, 1e-7s, 1e-6s, 1e-5s, 1e-4s, 0.001s, 0.01s, 0.1s, 0.2s, 04.s, 0.8s, 1.6s, 3.2s, 6.4s, 12.8s, 25.6s, 51.2s, 102.4s, 204.8s(~3min), 409.6s(~7min), 819.2s(~14min), +Inf]
			Buckets: append(prometheus.ExponentialBuckets(1e-9, 10, 8), prometheus.ExponentialBuckets(0.1, 12, 14)...),
		},
		metricsLabels,
	)

	ErrorsCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: rateLimiterSubsystem,
			Name:      "errors_counter",
			Help:      "Number of different errors in the rate limiter",
		},
		append(metricsLabels, "error_type"),
	)
)

func RegisterMetrics() {
	register.Do(func() {
		prometheus.MustRegister(RateLimitLatency)
		prometheus.MustRegister(StrategyLatency)
		prometheus.MustRegister(StrategyUsedDelay)
		prometheus.MustRegister(StrategyLockLatency)
		prometheus.MustRegister(ErrorsCounter)
	})
}

func PublishRateLimiterMetrics(key string, start time.Time) {
	RateLimitLatency.WithLabelValues(key).Observe(time.Since(start).Seconds())
}

func PublishStrategyRateLimiterLatencyMetrics(key string, start time.Time) {
	StrategyLatency.WithLabelValues(key).Observe(time.Since(start).Seconds())
}

func PublishStrategyMetrics(key string, usedDelay, lockLatency time.Duration) {
	StrategyUsedDelay.WithLabelValues(key).Observe(usedDelay.Seconds())
	StrategyLockLatency.WithLabelValues(key).Observe(lockLatency.Seconds())
}

func PublishErrorRateLimiterMetrics(key string, err error) {
	errorType := "none"
	if utils.IsQuotaExceededError(err) {
		errorType = "quota_exceeded_error"
	} else if err != nil {
		errorType = "non_quota_error"
	}
	ErrorsCounter.WithLabelValues(key, errorType).Inc()
}
