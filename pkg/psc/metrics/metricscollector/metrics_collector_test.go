package metricscollector

import (
	"strconv"
	"testing"

	"github.com/google/go-cmp/cmp"
	pscmetrics "k8s.io/ingress-gce/pkg/psc/metrics"
)

func TestComputePSCMetrics(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		desc     string
		saStates []pscmetrics.PSCState
		// service attachments to delete
		deleteStates  []string
		expectSACount map[feature]int
	}{
		{
			desc:     "empty input",
			saStates: []pscmetrics.PSCState{},
			expectSACount: map[feature]int{
				sa:          0,
				saInSuccess: 0,
				saInError:   0,
				saUnsync:    0,
			},
		},
		{
			desc: "one service attachment",
			saStates: []pscmetrics.PSCState{
				newPSCState(true),
			},
			expectSACount: map[feature]int{
				sa:          1,
				saInSuccess: 1,
				saInError:   0,
				saUnsync:    0,
			},
		},
		{
			desc: "one service attachment in error",
			saStates: []pscmetrics.PSCState{
				newPSCState(false),
			},
			expectSACount: map[feature]int{
				sa:          1,
				saInSuccess: 0,
				saInError:   1,
				saUnsync:    0,
			},
		},
		{
			desc: "many service attachments, some in error",
			saStates: []pscmetrics.PSCState{
				newPSCState(true),
				newPSCState(true),
				newPSCState(true),
				newPSCState(false),
				newPSCState(false),
			},
			expectSACount: map[feature]int{
				sa:          5,
				saInSuccess: 3,
				saInError:   2,
				saUnsync:    0,
			},
		},
		{
			desc: "some additions, and some deletions",
			saStates: []pscmetrics.PSCState{
				newPSCState(true),
				newPSCState(true),
				newPSCState(true),
				newPSCState(false),
				newPSCState(false),
			},
			deleteStates: []string{"0", "3"},
			expectSACount: map[feature]int{
				sa:          3,
				saInSuccess: 2,
				saInError:   1,
				saUnsync:    0,
			},
		},
		{
			desc: "some additions, and some unsynced",
			saStates: []pscmetrics.PSCState{
				newPSCState(true),
				newPSCState(true),
				newPSCState(true),
				newPSCState(false),
				{InSuccess: true, IsUnsync: true},
			},
			expectSACount: map[feature]int{
				sa:          5,
				saInSuccess: 4,
				saInError:   1,
				saUnsync:    1,
			},
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			newMetrics := FakePSCMetrics()
			for i, serviceState := range tc.saStates {
				newMetrics.SetServiceAttachment(strconv.Itoa(i), serviceState)
			}

			for _, key := range tc.deleteStates {
				newMetrics.DeleteServiceAttachment(key)
			}
			got := newMetrics.computePSCMetrics()
			if diff := cmp.Diff(tc.expectSACount, got); diff != "" {
				t.Fatalf("Got diff for service attachment counts (-want +got):\n%s", diff)
			}
		})
	}
}

func newPSCState(inSuccess bool) pscmetrics.PSCState {
	return pscmetrics.PSCState{
		InSuccess: inSuccess,
	}
}

func TestComputeServiceMetrics(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		desc           string
		services       []string
		deleteServices []string
		expectSACount  map[feature]int
	}{
		{
			desc: "empty input",
			expectSACount: map[feature]int{
				services: 0,
			},
		},
		{
			desc:     "one service",
			services: []string{"service-1"},
			expectSACount: map[feature]int{
				services: 1,
			},
		},
		{
			desc:     "many services",
			services: []string{"service-1", "service-2", "service-3", "service-4", "service-5", "service-6"},
			expectSACount: map[feature]int{
				services: 6,
			},
		},
		{
			desc:           "some additions, and some deletions",
			services:       []string{"service-1", "service-2", "service-3", "service-4", "service-5", "service-6"},
			deleteServices: []string{"service-2", "service-5"},
			expectSACount: map[feature]int{
				services: 4,
			},
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			newMetrics := FakePSCMetrics()
			for _, service := range tc.services {
				newMetrics.SetService(service)
			}

			for _, service := range tc.deleteServices {
				newMetrics.DeleteService(service)
			}

			got := newMetrics.computeServiceMetrics()
			if diff := cmp.Diff(tc.expectSACount, got); diff != "" {
				t.Fatalf("Got diff for service counts (-want +got):\n%s", diff)
			}
		})
	}
}
