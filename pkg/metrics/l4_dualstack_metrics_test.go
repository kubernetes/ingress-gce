package metrics

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func TestComputeL4ILBDualStackMetrics(t *testing.T) {
	t.Parallel()

	currTime := time.Now()
	before10min := currTime.Add(-10 * time.Minute)
	before20min := currTime.Add(-20 * time.Minute)

	for _, tc := range []struct {
		desc                      string
		serviceStates             []L4DualStackServiceState
		expectL4ILBDualStackCount map[L4DualStackServiceLabels]int
	}{
		{
			desc:                      "empty input",
			serviceStates:             []L4DualStackServiceState{},
			expectL4ILBDualStackCount: map[L4DualStackServiceLabels]int{},
		},
		{
			desc: "one l4 ilb dual-stack service",
			serviceStates: []L4DualStackServiceState{
				newL4DualStackServiceState("IPv4", "SingleStack", StatusSuccess, nil),
			},
			expectL4ILBDualStackCount: map[L4DualStackServiceLabels]int{
				L4DualStackServiceLabels{"IPv4", "SingleStack", StatusSuccess}: 1,
			},
		},
		{
			desc: "l4 ilb dual-stack service in error state",
			serviceStates: []L4DualStackServiceState{
				newL4DualStackServiceState("IPv4", "SingleStack", StatusError, nil),
			},
			expectL4ILBDualStackCount: map[L4DualStackServiceLabels]int{
				L4DualStackServiceLabels{"IPv4", "SingleStack", StatusError}: 1,
			},
		},
		{
			desc: "l4 ilb dual-stack service in error state, for 10 minutes",
			serviceStates: []L4DualStackServiceState{
				newL4DualStackServiceState("IPv4", "SingleStack", StatusError, &before10min),
			},
			expectL4ILBDualStackCount: map[L4DualStackServiceLabels]int{
				L4DualStackServiceLabels{
					"IPv4",
					"SingleStack",
					StatusError,
				}: 1,
			},
		},
		{
			desc: "l4 ilb dual-stack service in error state, for 20 minutes",
			serviceStates: []L4DualStackServiceState{
				newL4DualStackServiceState("IPv4", "SingleStack", StatusError, &before20min),
			},
			expectL4ILBDualStackCount: map[L4DualStackServiceLabels]int{
				L4DualStackServiceLabels{
					"IPv4",
					"SingleStack",
					StatusPersistentError,
				}: 1,
			},
		},
		{
			desc: "L4 ILB dual-stack service with IPv4,IPv6 Families",
			serviceStates: []L4DualStackServiceState{
				newL4DualStackServiceState("IPv4,IPv6", "RequireDualStack", StatusSuccess, nil),
			},
			expectL4ILBDualStackCount: map[L4DualStackServiceLabels]int{
				L4DualStackServiceLabels{"IPv4,IPv6", "RequireDualStack", StatusSuccess}: 1},
		},
		{
			desc: "many l4 ilb dual-stack services",
			serviceStates: []L4DualStackServiceState{
				newL4DualStackServiceState("IPv4,IPv6", "RequireDualStack", StatusSuccess, nil),
				newL4DualStackServiceState("IPv4,IPv6", "RequireDualStack", StatusSuccess, nil),
				newL4DualStackServiceState("IPv4", "SingleStack", StatusError, nil),
				newL4DualStackServiceState("IPv6", "SingleStack", StatusSuccess, nil),
				newL4DualStackServiceState("IPv6", "SingleStack", StatusSuccess, nil),
				newL4DualStackServiceState("IPv4", "SingleStack", StatusError, &before10min),
				newL4DualStackServiceState("IPv4", "SingleStack", StatusError, &before20min),
			},
			expectL4ILBDualStackCount: map[L4DualStackServiceLabels]int{
				L4DualStackServiceLabels{"IPv4,IPv6", "RequireDualStack", StatusSuccess}: 2,
				L4DualStackServiceLabels{"IPv4", "SingleStack", StatusError}:             2,
				L4DualStackServiceLabels{"IPv6", "SingleStack", StatusSuccess}:           2,
				L4DualStackServiceLabels{"IPv4", "SingleStack", StatusPersistentError}:   1,
			},
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			newMetrics := FakeControllerMetrics()
			for i, serviceState := range tc.serviceStates {
				newMetrics.SetL4ILBDualStackService(fmt.Sprint(i), serviceState)
			}
			got := newMetrics.computeL4ILBDualStackMetrics()
			if diff := cmp.Diff(tc.expectL4ILBDualStackCount, got); diff != "" {
				t.Fatalf("Got diff for L4 ILB Dual-Stack service counts (-want +got):\n%s", diff)
			}
		})
	}
}

func TestComputeL4NetLBDualStackMetrics(t *testing.T) {
	t.Parallel()

	currTime := time.Now()
	before10min := currTime.Add(-10 * time.Minute)
	before20min := currTime.Add(-20 * time.Minute)

	for _, tc := range []struct {
		desc                        string
		serviceStates               []L4DualStackServiceState
		expectL4NetLBDualStackCount map[L4DualStackServiceLabels]int
	}{
		{
			desc:                        "empty input",
			serviceStates:               []L4DualStackServiceState{},
			expectL4NetLBDualStackCount: map[L4DualStackServiceLabels]int{},
		},
		{
			desc: "one l4 NetLB dual-stack service",
			serviceStates: []L4DualStackServiceState{
				newL4DualStackServiceState("IPv4", "SingleStack", StatusSuccess, nil),
			},
			expectL4NetLBDualStackCount: map[L4DualStackServiceLabels]int{
				L4DualStackServiceLabels{"IPv4", "SingleStack", StatusSuccess}: 1,
			},
		},
		{
			desc: "l4 NetLB dual-stack service in error state",
			serviceStates: []L4DualStackServiceState{
				newL4DualStackServiceState("IPv4", "SingleStack", StatusError, nil),
			},
			expectL4NetLBDualStackCount: map[L4DualStackServiceLabels]int{
				L4DualStackServiceLabels{"IPv4", "SingleStack", StatusError}: 1,
			},
		},
		{
			desc: "l4 NetLB dual-stack service in error state, for 10 minutes",
			serviceStates: []L4DualStackServiceState{
				newL4DualStackServiceState("IPv4", "SingleStack", StatusError, &before10min),
			},
			expectL4NetLBDualStackCount: map[L4DualStackServiceLabels]int{
				L4DualStackServiceLabels{"IPv4", "SingleStack", StatusError}: 1,
			},
		},
		{
			desc: "l4 NetLB dual-stack service in error state, for 20 minutes",
			serviceStates: []L4DualStackServiceState{
				newL4DualStackServiceState("IPv4", "SingleStack", StatusError, &before20min),
			},
			expectL4NetLBDualStackCount: map[L4DualStackServiceLabels]int{
				L4DualStackServiceLabels{"IPv4", "SingleStack", StatusPersistentError}: 1,
			},
		},
		{
			desc: "L4 NetLB dual-stack service with IPv4,IPv6 Families",
			serviceStates: []L4DualStackServiceState{
				newL4DualStackServiceState("IPv4,IPv6", "RequireDualStack", StatusSuccess, nil),
			},
			expectL4NetLBDualStackCount: map[L4DualStackServiceLabels]int{
				L4DualStackServiceLabels{"IPv4,IPv6", "RequireDualStack", StatusSuccess}: 1,
			},
		},
		{
			desc: "many l4 NetLB dual-stack services",
			serviceStates: []L4DualStackServiceState{
				newL4DualStackServiceState("IPv4,IPv6", "RequireDualStack", StatusSuccess, nil),
				newL4DualStackServiceState("IPv4,IPv6", "RequireDualStack", StatusSuccess, nil),
				newL4DualStackServiceState("IPv4", "SingleStack", StatusError, nil),
				newL4DualStackServiceState("IPv6", "SingleStack", StatusSuccess, nil),
				newL4DualStackServiceState("IPv6", "SingleStack", StatusSuccess, nil),
				newL4DualStackServiceState("IPv4", "SingleStack", StatusError, &before10min),
				newL4DualStackServiceState("IPv4", "SingleStack", StatusError, &before20min),
			},
			expectL4NetLBDualStackCount: map[L4DualStackServiceLabels]int{
				L4DualStackServiceLabels{"IPv4,IPv6", "RequireDualStack", StatusSuccess}: 2,
				L4DualStackServiceLabels{"IPv4", "SingleStack", StatusError}:             2,
				L4DualStackServiceLabels{"IPv6", "SingleStack", StatusSuccess}:           2,
				L4DualStackServiceLabels{"IPv4", "SingleStack", StatusPersistentError}:   1,
			},
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			newMetrics := FakeControllerMetrics()
			for i, serviceState := range tc.serviceStates {
				newMetrics.SetL4NetLBDualStackService(fmt.Sprint(i), serviceState)
			}
			got := newMetrics.computeL4NetLBDualStackMetrics()
			if diff := cmp.Diff(tc.expectL4NetLBDualStackCount, got); diff != "" {
				t.Fatalf("Got diff for L4 NetLB Dual-Stack service counts (-want +got):\n%s", diff)
			}
		})
	}
}

func TestRetryPeriodForL4ILBDualStackServices(t *testing.T) {
	t.Parallel()
	currTime := time.Now()
	before5min := currTime.Add(-5 * time.Minute)

	svcName1 := "svc1"
	metrics := FakeControllerMetrics()

	errorState := newL4DualStackServiceState("IPv4", "SingleStack", StatusError, &currTime)
	metrics.SetL4ILBDualStackService(svcName1, errorState)

	// change FirstSyncErrorTime and verify it will not change metrics state
	errorState.FirstSyncErrorTime = &before5min
	metrics.SetL4ILBDualStackService(svcName1, errorState)
	state, ok := metrics.l4ILBDualStackServiceMap[svcName1]
	if !ok {
		t.Fatalf("state should be set")
	}
	if *state.FirstSyncErrorTime != currTime {
		t.Errorf("FirstSyncErrorTime should not change, expected %v, got %v", currTime, *state.FirstSyncErrorTime)
	}
	if state.Status != StatusError {
		t.Errorf("Expected status %s, got %s", StatusError, state.Status)
	}
}

func TestRetryPeriodForL4NetLBDualStackServices(t *testing.T) {
	t.Parallel()
	currTime := time.Now()
	before5min := currTime.Add(-5 * time.Minute)

	svcName1 := "svc1"
	metrics := FakeControllerMetrics()

	errorState := newL4DualStackServiceState("IPv4", "SingleStack", StatusError, &currTime)
	metrics.SetL4NetLBDualStackService(svcName1, errorState)

	// change FirstSyncErrorTime and verify it will not change metrics state
	errorState.FirstSyncErrorTime = &before5min
	metrics.SetL4NetLBDualStackService(svcName1, errorState)
	state, ok := metrics.l4NetLBDualStackServiceMap[svcName1]
	if !ok {
		t.Fatalf("state should be set")
	}
	if *state.FirstSyncErrorTime != currTime {
		t.Errorf("FirstSyncErrorTime should not change, expected %v, got %v", currTime, *state.FirstSyncErrorTime)
	}
	if state.Status != StatusError {
		t.Errorf("Expected status %s, got %s", StatusError, state.Status)
	}
}

func newL4DualStackServiceState(ipFamilies string, ipFamilyPolicy string, status L4ServiceStatus, firstSyncErrorTime *time.Time) L4DualStackServiceState {
	return L4DualStackServiceState{
		L4DualStackServiceLabels: L4DualStackServiceLabels{
			IPFamilies:     ipFamilies,
			IPFamilyPolicy: ipFamilyPolicy,
			Status:         status,
		},
		FirstSyncErrorTime: firstSyncErrorTime,
	}
}
