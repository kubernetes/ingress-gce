/*
Copyright 2021 The Kubernetes Authors.

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

const (
	pscControllerSubsystem = "psc_controller"
	pscProcessLatency      = "psc_process_duration_seconds"
	lastProcessTimestamp   = "psc_process_timestamp"

	resultSuccess = "success"
	resultError   = "error"

	GCProcess   = "GC"
	SyncProcess = "Sync"
)

var (
	pscProcessMetricsLabels = []string{
		"process", // type of process loop
		"result",  // result of the process
	}

	pscProcessTSMetricsLabels = []string{
		"process", // type of process loop
	}

	PSCProcessLatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Subsystem: pscControllerSubsystem,
			Name:      pscProcessLatency,
			Help:      "Latency of a PSC Process",
			// custom buckets - [1s, 2s, 4s, 8s, 16s, 32s, 64s, 128s, 256s(~4min), 512s(~8min), 1024s(~17min), 2048 (~34min), 4096(~68min), +Inf]
			Buckets: prometheus.ExponentialBuckets(1, 2, 13),
		},
		pscProcessMetricsLabels,
	)

	LastProcessTimestamp = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: pscControllerSubsystem,
			Name:      lastProcessTimestamp,
			Help:      "The timestamp of the last execution of a PSC process loop.",
		},
		pscProcessTSMetricsLabels,
	)
)

var register sync.Once

func RegisterMetrics() {
	register.Do(func() {
		prometheus.MustRegister(PSCProcessLatency)
		prometheus.MustRegister(LastProcessTimestamp)
	})
}

// PublishLastProcessTimestampMetrics publishes the last timestamp of the provided process
func PublishLastProcessTimestampMetrics(process string) {
	LastProcessTimestamp.WithLabelValues(process).Set(float64(time.Now().UTC().UnixNano()))
}

// PublishLastProcessTimestampMetrics calculates and publishes the process latency
func PublishPSCProcessMetrics(process string, err error, start time.Time) {
	result := resultSuccess
	if err != nil {
		result = resultError
	}
	PSCProcessLatency.WithLabelValues(process, result).Observe(time.Since(start).Seconds())
}
