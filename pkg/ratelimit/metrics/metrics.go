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
)

var register sync.Once

const (
	rateLimitSubsystem  = "rate_limit"
	rateLimitLatencyKey = "rate_limit_delay_seconds"
)

var (
	rateLimitLatencyMetricsLabels = []string{
		"key", // rate limiter key
	}

	RateLimitLatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Subsystem: rateLimitSubsystem,
			Name:      rateLimitLatencyKey,
			Help:      "Latency of the RateLimiter Accept Operation",
			// custom buckets = [0.5s, 1s, 2s, 4s, 8s, 16s, 32s, 64s, 128s, 256s(~4min), 512s(~8min), 1024s(~17min), 2048s(~34min), 4096s(~68min), +Inf]
			Buckets: prometheus.ExponentialBuckets(0.5, 2, 14),
		},
		rateLimitLatencyMetricsLabels,
	)
)

func RegisterMetrics() {
	register.Do(func() {
		prometheus.MustRegister(RateLimitLatency)
	})
}

func PublishRateLimiterMetrics(key string, start time.Time) {
	RateLimitLatency.WithLabelValues(key).Observe(time.Since(start).Seconds())
}
