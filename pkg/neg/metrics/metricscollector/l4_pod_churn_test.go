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

package metricscollector

import (
	"testing"

	"github.com/GoogleCloudPlatform/gke-enterprise-mt/pkg/mtmetrics"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
)

func TestPodChurnDiff(t *testing.T) {
	cases := []struct {
		desc                string
		prev, curr          map[string]int
		wantChanged         int
		wantZeroTransitions int
	}{
		{
			desc: "no change",
			prev: map[string]int{"a": 4},
			curr: map[string]int{"a": 4},
		},
		{
			desc: "both empty",
			prev: map[string]int{},
			curr: map[string]int{},
		},
		{
			desc:        "weight-only change on an existing node",
			prev:        map[string]int{"a": 3},
			curr:        map[string]int{"a": 4},
			wantChanged: 1,
		},
		{
			desc:                "node gets its first ready pod",
			prev:                map[string]int{"a": 3},
			curr:                map[string]int{"a": 3, "b": 1},
			wantChanged:         1,
			wantZeroTransitions: 1,
		},
		{
			desc:                "node loses its last ready pod",
			prev:                map[string]int{"a": 3, "b": 1},
			curr:                map[string]int{"a": 3},
			wantChanged:         1,
			wantZeroTransitions: 1,
		},
		{
			desc:                "mixed: one weight change, one attach, one detach",
			prev:                map[string]int{"a": 3, "b": 1},
			curr:                map[string]int{"a": 4, "c": 2},
			wantChanged:         3,
			wantZeroTransitions: 2,
		},
		{
			desc:                "scale to zero",
			prev:                map[string]int{"a": 2, "b": 5},
			curr:                map[string]int{},
			wantChanged:         2,
			wantZeroTransitions: 2,
		},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			changed, zero := podChurnDiff(tc.prev, tc.curr)
			if changed != tc.wantChanged || zero != tc.wantZeroTransitions {
				t.Errorf("podChurnDiff() = (%d, %d), want (%d, %d)", changed, zero, tc.wantChanged, tc.wantZeroTransitions)
			}
		})
	}
}

// histogramState returns the sample count and sum of one histogram series.
func histogramState(t *testing.T, vec mtmetrics.ObserverVec, lvs ...string) (uint64, float64) {
	t.Helper()
	m := &dto.Metric{}
	if err := vec.WithLabelValues(lvs...).(prometheus.Metric).Write(m); err != nil {
		t.Fatalf("Write() failed: %v", err)
	}
	return m.GetHistogram().GetSampleCount(), m.GetHistogram().GetSampleSum()
}

func TestUpdateL4BackendPods(t *testing.T) {
	sm := FakeSyncerMetrics()
	key := negtypes.NegSyncerKey{
		Namespace:        "ns",
		Name:             "svc",
		NegType:          negtypes.VmIpEndpointType,
		EpCalculatorMode: negtypes.L4LocalMode,
		L4LBType:         negtypes.L4InternalLB,
	}
	labels := []string{"internal", "local"}

	steps := []struct {
		desc     string
		pods     map[string]int
		wantN    uint64  // cumulative churn events
		wantSum  float64 // cumulative sum of observed node counts
		wantZero uint64  // cumulative zero-transition events
	}{
		{
			desc: "first sync is a baseline only",
			pods: map[string]int{"a": 3, "b": 1},
		},
		{
			desc:    "a: 3->4 is a churn event observed at 2 nodes",
			pods:    map[string]int{"a": 4, "b": 1},
			wantN:   1,
			wantSum: 2,
		},
		{
			desc:     "c: 0->1 is churn and a zero transition, observed at max(2, 3) = 3 nodes",
			pods:     map[string]int{"a": 4, "b": 1, "c": 1},
			wantN:    2,
			wantSum:  5,
			wantZero: 1,
		},
		{
			desc:     "no change emits nothing",
			pods:     map[string]int{"a": 4, "b": 1, "c": 1},
			wantN:    2,
			wantSum:  5,
			wantZero: 1,
		},
		{
			desc:     "scale to zero: 3 nodes go to 0, each observed at max(3, 0) = 3 nodes",
			pods:     map[string]int{},
			wantN:    5,
			wantSum:  14,
			wantZero: 4,
		},
	}
	for _, step := range steps {
		sm.UpdateL4BackendPods(key, step.pods)

		n, sum := histogramState(t, sm.l4PodChurnEvents, labels...)
		zero, _ := histogramState(t, sm.l4PodChurnZeroTransitionEvents, labels...)
		if n != step.wantN || sum != step.wantSum {
			t.Errorf("%s: l4_pod_churn_events count=%d sum=%v, want count=%d sum=%v", step.desc, n, sum, step.wantN, step.wantSum)
		}
		if zero != step.wantZero {
			t.Errorf("%s: l4_pod_churn_zero_transition_events count=%d, want %d", step.desc, zero, step.wantZero)
		}
	}
}

func TestUpdateL4BackendPodsBaselineAfterDelete(t *testing.T) {
	sm := FakeSyncerMetrics()
	key := negtypes.NegSyncerKey{
		Namespace:        "ns",
		Name:             "svc",
		NegType:          negtypes.VmIpEndpointType,
		EpCalculatorMode: negtypes.L4ClusterMode,
		L4LBType:         negtypes.L4ExternalLB,
	}

	sm.UpdateL4BackendPods(key, map[string]int{"a": 1})
	sm.DeleteSyncer(key)
	// A recreated syncer starts from a fresh baseline instead of diffing
	// against the state of the deleted one.
	sm.UpdateL4BackendPods(key, map[string]int{"a": 5, "b": 5})

	n, _ := histogramState(t, sm.l4PodChurnEvents, "external", "cluster")
	if n != 0 {
		t.Errorf("l4_pod_churn_events count=%d after recreating the syncer, want 0", n)
	}
}
