/*
Copyright 2026 The Kubernetes Authors.

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

package flags

import (
	"testing"
	"time"
)

func TestJitteredResyncPeriod(t *testing.T) {
	const (
		period  = 10 * time.Minute
		samples = 1000
	)
	testCases := []struct {
		desc         string
		period       time.Duration
		jitterFactor float64
		// Every sampled period must be within [wantMin, wantMax].
		wantMin time.Duration
		wantMax time.Duration
		// wantJitter requires the samples to actually be randomized: both
		// halves of [wantMin, wantMax] must be hit. With 1000 uniformly
		// distributed samples the probability of a spurious failure is
		// 2 * 0.5^1000.
		wantJitter bool
	}{
		{
			desc:         "jitter disabled",
			period:       period,
			jitterFactor: 0,
			wantMin:      period,
			wantMax:      period,
		},
		{
			desc:         "negative factor is treated as disabled",
			period:       period,
			jitterFactor: -0.5,
			wantMin:      period,
			wantMax:      period,
		},
		{
			desc:         "20 percent jitter",
			period:       period,
			jitterFactor: 0.2,
			wantMin:      period,
			wantMax:      12 * time.Minute,
			wantJitter:   true,
		},
		{
			desc:         "100 percent jitter",
			period:       period,
			jitterFactor: 1,
			wantMin:      period,
			wantMax:      20 * time.Minute,
			wantJitter:   true,
		},
		{
			desc:         "zero period stays zero",
			period:       0,
			jitterFactor: 0.2,
			wantMin:      0,
			wantMax:      0,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			mid := tc.wantMin + (tc.wantMax-tc.wantMin)/2
			var belowMid, fromMid int
			for i := 0; i < samples; i++ {
				got := JitteredResyncPeriod(tc.period, tc.jitterFactor)
				if got < tc.wantMin || got > tc.wantMax {
					t.Fatalf("JitteredResyncPeriod(%v, %v) = %v, want in [%v, %v]", tc.period, tc.jitterFactor, got, tc.wantMin, tc.wantMax)
				}
				if got < mid {
					belowMid++
				} else {
					fromMid++
				}
			}
			if tc.wantJitter && (belowMid == 0 || fromMid == 0) {
				t.Errorf("JitteredResyncPeriod(%v, %v): %d of %d samples below %v and %d from %v on, want both halves of [%v, %v] to be hit", tc.period, tc.jitterFactor, belowMid, samples, mid, fromMid, mid, tc.wantMin, tc.wantMax)
			}
		})
	}
}
