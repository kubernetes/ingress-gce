package metrics

import (
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func labels(ipFamilies, ipFamilyPolicy string, status L4ServiceStatus) L4ServiceState {
	return L4ServiceState{L4DualStackServiceLabels: L4DualStackServiceLabels{IPFamilies: ipFamilies, IPFamilyPolicy: ipFamilyPolicy}, Status: status}
}

func TestComputeL4ILBDualStackMetrics(t *testing.T) {
	t.Parallel()

	currTime := time.Now()
	before10min := currTime.Add(-10 * time.Minute)
	before20min := currTime.Add(-20 * time.Minute)

	for _, tc := range []struct {
		desc                      string
		serviceStates             []L4ServiceState
		expectL4ILBDualStackCount map[L4ServiceState]int
	}{
		{
			desc:                      "empty input",
			serviceStates:             []L4ServiceState{},
			expectL4ILBDualStackCount: map[L4ServiceState]int{},
		},
		{
			desc: "one l4 ilb dual-stack service",
			serviceStates: []L4ServiceState{
				newL4ServiceState("IPv4", "SingleStack", StatusSuccess, nil),
			},
			expectL4ILBDualStackCount: map[L4ServiceState]int{
				labels("IPv4", "SingleStack", StatusSuccess): 1,
			},
		},
		{
			desc: "l4 ilb dual-stack service in error state",
			serviceStates: []L4ServiceState{
				newL4ServiceState("IPv4", "SingleStack", StatusError, nil),
			},
			expectL4ILBDualStackCount: map[L4ServiceState]int{
				labels("IPv4", "SingleStack", StatusError): 1,
			},
		},
		{
			desc: "l4 ilb dual-stack service in error state, for 10 minutes",
			serviceStates: []L4ServiceState{
				newL4ServiceState("IPv4", "SingleStack", StatusError, &before10min),
			},
			expectL4ILBDualStackCount: map[L4ServiceState]int{
				labels(
					"IPv4",
					"SingleStack",
					StatusError,
				): 1,
			},
		},
		{
			desc: "l4 ilb dual-stack service in error state, for 20 minutes",
			serviceStates: []L4ServiceState{
				newL4ServiceState("IPv4", "SingleStack", StatusError, &before20min),
			},
			expectL4ILBDualStackCount: map[L4ServiceState]int{
				labels(
					"IPv4",
					"SingleStack",
					StatusPersistentError,
				): 1,
			},
		},
		{
			desc: "L4 ILB dual-stack service with IPv4,IPv6 Families",
			serviceStates: []L4ServiceState{
				newL4ServiceState("IPv4,IPv6", "RequireDualStack", StatusSuccess, nil),
			},
			expectL4ILBDualStackCount: map[L4ServiceState]int{
				labels("IPv4,IPv6", "RequireDualStack", StatusSuccess): 1},
		},
		{
			desc: "many l4 ilb dual-stack services",
			serviceStates: []L4ServiceState{
				newL4ServiceState("IPv4,IPv6", "RequireDualStack", StatusSuccess, nil),
				newL4ServiceState("IPv4,IPv6", "RequireDualStack", StatusSuccess, nil),
				newL4ServiceState("IPv4", "SingleStack", StatusError, nil),
				newL4ServiceState("IPv6", "SingleStack", StatusSuccess, nil),
				newL4ServiceState("IPv6", "SingleStack", StatusSuccess, nil),
				newL4ServiceState("IPv4", "SingleStack", StatusError, &before10min),
				newL4ServiceState("IPv4", "SingleStack", StatusError, &before20min),
			},
			expectL4ILBDualStackCount: map[L4ServiceState]int{
				labels("IPv4,IPv6", "RequireDualStack", StatusSuccess): 2,
				labels("IPv4", "SingleStack", StatusError):             2,
				labels("IPv6", "SingleStack", StatusSuccess):           2,
				labels("IPv4", "SingleStack", StatusPersistentError):   1,
			},
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			newMetrics := NewFakeCollector()
			for i, serviceState := range tc.serviceStates {
				newMetrics.SetL4ILBService(fmt.Sprint(i), serviceState)
			}
			newMetrics.exportL4ILBDualStackMetrics()

			for labels, exp := range tc.expectL4ILBDualStackCount {
				countFloat := testutil.ToFloat64(l4ILBDualStackCount.WithLabelValues(labels.IPFamilies, labels.IPFamilyPolicy, string(labels.Status)))

				if countFloat != float64(exp) {
					t.Fatalf("incorrect values for labels: %v, want=%d, got=%f", labels, exp, countFloat)
				}
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
		serviceStates               []L4ServiceState
		expectL4NetLBDualStackCount map[L4ServiceState]int
	}{
		{
			desc:                        "empty input",
			serviceStates:               []L4ServiceState{},
			expectL4NetLBDualStackCount: map[L4ServiceState]int{},
		},
		{
			desc: "one l4 NetLB dual-stack service",
			serviceStates: []L4ServiceState{
				newL4ServiceState("IPv4", "SingleStack", StatusSuccess, nil),
			},
			expectL4NetLBDualStackCount: map[L4ServiceState]int{
				labels("IPv4", "SingleStack", StatusSuccess): 1,
			},
		},
		{
			desc: "l4 NetLB dual-stack service in error state",
			serviceStates: []L4ServiceState{
				newL4ServiceState("IPv4", "SingleStack", StatusError, nil),
			},
			expectL4NetLBDualStackCount: map[L4ServiceState]int{
				labels("IPv4", "SingleStack", StatusError): 1,
			},
		},
		{
			desc: "l4 NetLB dual-stack service in error state, for 10 minutes",
			serviceStates: []L4ServiceState{
				newL4ServiceState("IPv4", "SingleStack", StatusError, &before10min),
			},
			expectL4NetLBDualStackCount: map[L4ServiceState]int{
				labels("IPv4", "SingleStack", StatusError): 1,
			},
		},
		{
			desc: "l4 NetLB dual-stack service in error state, for 20 minutes",
			serviceStates: []L4ServiceState{
				newL4ServiceState("IPv4", "SingleStack", StatusError, &before20min),
			},
			expectL4NetLBDualStackCount: map[L4ServiceState]int{
				labels("IPv4", "SingleStack", StatusPersistentError): 1,
			},
		},
		{
			desc: "L4 NetLB dual-stack service with IPv4,IPv6 Families",
			serviceStates: []L4ServiceState{
				newL4ServiceState("IPv4,IPv6", "RequireDualStack", StatusSuccess, nil),
			},
			expectL4NetLBDualStackCount: map[L4ServiceState]int{
				labels("IPv4,IPv6", "RequireDualStack", StatusSuccess): 1,
			},
		},
		{
			desc: "many l4 NetLB dual-stack services",
			serviceStates: []L4ServiceState{
				newL4ServiceState("IPv4,IPv6", "RequireDualStack", StatusSuccess, nil),
				newL4ServiceState("IPv4,IPv6", "RequireDualStack", StatusSuccess, nil),
				newL4ServiceState("IPv4", "SingleStack", StatusError, nil),
				newL4ServiceState("IPv6", "SingleStack", StatusSuccess, nil),
				newL4ServiceState("IPv6", "SingleStack", StatusSuccess, nil),
				newL4ServiceState("IPv4", "SingleStack", StatusError, &before10min),
				newL4ServiceState("IPv4", "SingleStack", StatusError, &before20min),
			},
			expectL4NetLBDualStackCount: map[L4ServiceState]int{
				labels("IPv4,IPv6", "RequireDualStack", StatusSuccess): 2,
				labels("IPv4", "SingleStack", StatusError):             2,
				labels("IPv6", "SingleStack", StatusSuccess):           2,
				labels("IPv4", "SingleStack", StatusPersistentError):   1,
			},
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			newMetrics := NewFakeCollector()
			for i, serviceState := range tc.serviceStates {
				newMetrics.SetL4NetLBService(fmt.Sprint(i), serviceState)
			}
			newMetrics.exportL4NetLBDualStackMetrics()

			for labels, exp := range tc.expectL4NetLBDualStackCount {
				countFloat := testutil.ToFloat64(l4NetLBDualStackCount.WithLabelValues(labels.IPFamilies, labels.IPFamilyPolicy, string(labels.Status)))

				if countFloat != float64(exp) {
					t.Fatalf("incorrect values for labels: %v, want=%d, got=%f", labels, exp, countFloat)
				}
			}
		})
	}
}

func TestRetryPeriodForL4ILBDualStackServices(t *testing.T) {
	t.Parallel()
	currTime := time.Now()
	before5min := currTime.Add(-5 * time.Minute)

	svcName1 := "svc1"
	metrics := NewFakeCollector()

	errorState := newL4ServiceState("IPv4", "SingleStack", StatusError, &currTime)
	metrics.SetL4ILBService(svcName1, errorState)

	// change FirstSyncErrorTime and verify it will not change metrics state
	errorState.FirstSyncErrorTime = &before5min
	metrics.SetL4ILBService(svcName1, errorState)
	state, ok := metrics.l4ILBServiceMap[svcName1]
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
	metrics := NewFakeCollector()

	errorState := newL4ServiceState("IPv4", "SingleStack", StatusError, &currTime)
	metrics.SetL4NetLBService(svcName1, errorState)

	// change FirstSyncErrorTime and verify it will not change metrics state
	errorState.FirstSyncErrorTime = &before5min
	metrics.SetL4NetLBService(svcName1, errorState)
	state, ok := metrics.l4NetLBServiceMap[svcName1]
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

func newL4ServiceState(ipFamilies string, ipFamilyPolicy string, status L4ServiceStatus, firstSyncErrorTime *time.Time) L4ServiceState {
	return L4ServiceState{
		L4DualStackServiceLabels: L4DualStackServiceLabels{
			IPFamilies:     ipFamilies,
			IPFamilyPolicy: ipFamilyPolicy,
		},
		FirstSyncErrorTime: firstSyncErrorTime,
		Status:             status,
	}
}
