/*
Copyright 2022 The Kubernetes Authors.

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
	"fmt"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

const (
	premium             = true
	standard            = false
	managed             = true
	unmanaged           = false
	isSuccess           = true
	isError             = false
	isUserError         = true
	noUserError         = false
	enableGlobalAccess  = true
	disableGlobalAccess = false
	enableCustomSubnet  = true
	disableCustomSubnet = false
)

func TestComputeL4ILBMetrics(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		desc             string
		serviceStates    []L4ILBServiceState
		expectL4ILBCount map[feature]int
	}{
		{
			desc:          "empty input",
			serviceStates: []L4ILBServiceState{},
			expectL4ILBCount: map[feature]int{
				l4ILBService:      0,
				l4ILBGlobalAccess: 0,
				l4ILBCustomSubnet: 0,
				l4ILBInSuccess:    0,
				l4ILBInError:      0,
				l4ILBInUserError:  0,
			},
		},
		{
			desc: "one l4 ilb service",
			serviceStates: []L4ILBServiceState{
				newL4ILBServiceState(disableGlobalAccess, disableCustomSubnet, isSuccess, noUserError),
			},
			expectL4ILBCount: map[feature]int{
				l4ILBService:      1,
				l4ILBGlobalAccess: 0,
				l4ILBCustomSubnet: 0,
				l4ILBInSuccess:    1,
				l4ILBInError:      0,
				l4ILBInUserError:  0,
			},
		},
		{
			desc: "l4 ilb service in error state",
			serviceStates: []L4ILBServiceState{
				newL4ILBServiceState(disableGlobalAccess, enableCustomSubnet, isError, noUserError),
			},
			expectL4ILBCount: map[feature]int{
				l4ILBService:      1,
				l4ILBGlobalAccess: 0,
				l4ILBCustomSubnet: 0,
				l4ILBInSuccess:    0,
				l4ILBInError:      1,
				l4ILBInUserError:  0,
			},
		},
		{
			desc: "l4 ilb service in user error state",
			serviceStates: []L4ILBServiceState{
				newL4ILBServiceState(disableGlobalAccess, enableCustomSubnet, isError, isUserError),
			},
			expectL4ILBCount: map[feature]int{
				l4ILBService:      1,
				l4ILBGlobalAccess: 0,
				l4ILBCustomSubnet: 0,
				l4ILBInSuccess:    0,
				l4ILBInError:      0,
				l4ILBInUserError:  1,
			},
		},
		{
			desc: "global access for l4 ilb service enabled",
			serviceStates: []L4ILBServiceState{
				newL4ILBServiceState(enableGlobalAccess, disableCustomSubnet, isSuccess, noUserError),
			},
			expectL4ILBCount: map[feature]int{
				l4ILBService:      1,
				l4ILBGlobalAccess: 1,
				l4ILBCustomSubnet: 0,
				l4ILBInSuccess:    1,
				l4ILBInError:      0,
				l4ILBInUserError:  0,
			},
		},
		{
			desc: "custom subnet for l4 ilb service enabled",
			serviceStates: []L4ILBServiceState{
				newL4ILBServiceState(disableGlobalAccess, enableCustomSubnet, isSuccess, noUserError),
			},
			expectL4ILBCount: map[feature]int{
				l4ILBService:      1,
				l4ILBGlobalAccess: 0,
				l4ILBCustomSubnet: 1,
				l4ILBInSuccess:    1,
				l4ILBInError:      0,
				l4ILBInUserError:  0,
			},
		},
		{
			desc: "both global access and custom subnet for l4 ilb service enabled",
			serviceStates: []L4ILBServiceState{
				newL4ILBServiceState(enableGlobalAccess, enableCustomSubnet, isSuccess, noUserError),
			},
			expectL4ILBCount: map[feature]int{
				l4ILBService:      1,
				l4ILBGlobalAccess: 1,
				l4ILBCustomSubnet: 1,
				l4ILBInSuccess:    1,
				l4ILBInError:      0,
				l4ILBInUserError:  0,
			},
		},
		{
			desc: "many l4 ilb services",
			serviceStates: []L4ILBServiceState{
				newL4ILBServiceState(disableGlobalAccess, disableCustomSubnet, isSuccess, noUserError),
				newL4ILBServiceState(disableGlobalAccess, enableCustomSubnet, isSuccess, noUserError),
				newL4ILBServiceState(enableGlobalAccess, disableCustomSubnet, isSuccess, noUserError),
				newL4ILBServiceState(enableGlobalAccess, enableCustomSubnet, isSuccess, noUserError),
			},
			expectL4ILBCount: map[feature]int{
				l4ILBService:      4,
				l4ILBGlobalAccess: 2,
				l4ILBCustomSubnet: 2,
				l4ILBInSuccess:    4,
				l4ILBInError:      0,
				l4ILBInUserError:  0,
			},
		},
		{
			desc: "many l4 ilb services with some in error state",
			serviceStates: []L4ILBServiceState{
				newL4ILBServiceState(disableGlobalAccess, disableCustomSubnet, isSuccess, noUserError),
				newL4ILBServiceState(disableGlobalAccess, enableCustomSubnet, isError, noUserError),
				newL4ILBServiceState(disableGlobalAccess, enableCustomSubnet, isSuccess, noUserError),
				newL4ILBServiceState(enableGlobalAccess, disableCustomSubnet, isSuccess, noUserError),
				newL4ILBServiceState(enableGlobalAccess, disableCustomSubnet, isError, noUserError),
				newL4ILBServiceState(enableGlobalAccess, enableCustomSubnet, isSuccess, noUserError),
				newL4ILBServiceState(enableGlobalAccess, enableCustomSubnet, isError, isUserError),
			},
			expectL4ILBCount: map[feature]int{
				l4ILBService:      7,
				l4ILBGlobalAccess: 2,
				l4ILBCustomSubnet: 2,
				l4ILBInSuccess:    4,
				l4ILBInError:      2,
				l4ILBInUserError:  1,
			},
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			newMetrics := FakeControllerMetrics()
			for i, serviceState := range tc.serviceStates {
				newMetrics.SetL4ILBService(fmt.Sprint(i), serviceState)
			}
			got := newMetrics.computeL4ILBMetrics()
			if diff := cmp.Diff(tc.expectL4ILBCount, got); diff != "" {
				t.Fatalf("Got diff for L4 ILB service counts (-want +got):\n%s", diff)
			}
		})
	}
}

func newL4ILBServiceState(globalAccess bool, customSubnet bool, inSuccess bool, isUserError bool) L4ILBServiceState {
	return L4ILBServiceState{
		EnabledGlobalAccess: globalAccess,
		EnabledCustomSubnet: customSubnet,
		InSuccess:           inSuccess,
		IsUserError:         isUserError,
	}
}

func TestComputeL4NetLBMetrics(t *testing.T) {
	t.Parallel()

	currTime := time.Now()
	before5min := currTime.Add(-5 * time.Minute)
	before25min := currTime.Add(-25 * time.Minute)

	for _, tc := range []struct {
		desc               string
		serviceStates      []L4NetLBServiceState
		expectL4NetLBCount netLBFeatureCount
	}{
		{
			desc:          "empty input",
			serviceStates: []L4NetLBServiceState{},
			expectL4NetLBCount: netLBFeatureCount{
				service:            0,
				managedStaticIP:    0,
				premiumNetworkTier: 0,
				success:            0,
				inUserError:        0,
				inError:            0,
			},
		},
		{
			desc: "one l4 Netlb service",
			serviceStates: []L4NetLBServiceState{
				newL4NetLBServiceState(isSuccess, unmanaged, noUserError, standard /*resyncTime = */, nil),
			},
			expectL4NetLBCount: netLBFeatureCount{
				service:            1,
				managedStaticIP:    0,
				premiumNetworkTier: 0,
				success:            1,
				inUserError:        0,
				inError:            0,
			},
		},
		{
			desc: "one l4 Netlb service in premium network tier",
			serviceStates: []L4NetLBServiceState{
				newL4NetLBServiceState(isSuccess, unmanaged, premium, noUserError /*resyncTime = */, nil),
			},
			expectL4NetLBCount: netLBFeatureCount{
				service:            1,
				managedStaticIP:    0,
				premiumNetworkTier: 1,
				success:            1,
				inUserError:        0,
				inError:            0,
			},
		},
		{
			desc: "one l4 Netlb service in premium network tier and managed static ip",
			serviceStates: []L4NetLBServiceState{
				newL4NetLBServiceState(isSuccess, managed, premium, noUserError /*resyncTime = */, nil),
			},
			expectL4NetLBCount: netLBFeatureCount{
				service:            1,
				managedStaticIP:    1,
				premiumNetworkTier: 1,
				success:            1,
				inUserError:        0,
				inError:            0,
			},
		},
		{
			desc: "l4 Netlb service in error state with timestamp greater than resync period",
			serviceStates: []L4NetLBServiceState{
				newL4NetLBServiceState(isError, unmanaged, standard, noUserError, &before25min),
			},
			expectL4NetLBCount: netLBFeatureCount{
				service:            1,
				managedStaticIP:    0,
				premiumNetworkTier: 0,
				success:            0,
				inUserError:        0,
				inError:            1,
			},
		},
		{
			desc: "l4 Netlb service in error state should not count static ip and network tier",
			serviceStates: []L4NetLBServiceState{
				newL4NetLBServiceState(isError, unmanaged, standard, noUserError, &before25min),
				newL4NetLBServiceState(isError, managed, premium, noUserError, &before25min),
			},
			expectL4NetLBCount: netLBFeatureCount{
				service:            2,
				managedStaticIP:    0,
				premiumNetworkTier: 0,
				success:            0,
				inUserError:        0,
				inError:            2,
			},
		},
		{
			desc: "two l4 Netlb services with different network tier",
			serviceStates: []L4NetLBServiceState{
				newL4NetLBServiceState(isSuccess, unmanaged, standard, noUserError /*resyncTime = */, nil),
				newL4NetLBServiceState(isSuccess, unmanaged, premium, noUserError, nil),
			},
			expectL4NetLBCount: netLBFeatureCount{
				service:            2,
				managedStaticIP:    0,
				premiumNetworkTier: 1,
				success:            2,
				inUserError:        0,
			},
		},
		{
			desc: "two l4 Netlb services with user error should not count others features",
			serviceStates: []L4NetLBServiceState{
				newL4NetLBServiceState(isError, unmanaged, standard, isUserError /*resyncTime = */, nil),
				newL4NetLBServiceState(isError, unmanaged, premium, isUserError, nil),
			},
			expectL4NetLBCount: netLBFeatureCount{
				service:            2,
				managedStaticIP:    0,
				premiumNetworkTier: 0,
				success:            0,
				inUserError:        2,
				inError:            0,
			},
		},
		{
			desc: "Error should be measure only after retry time period",
			serviceStates: []L4NetLBServiceState{
				newL4NetLBServiceState(isError, unmanaged, premium, noUserError, &before5min),
			},
			expectL4NetLBCount: netLBFeatureCount{
				service:            1,
				managedStaticIP:    0,
				premiumNetworkTier: 0,
				success:            0,
				inUserError:        0,
				inError:            0,
			},
		},
		{
			desc: "multi l4 Netlb services",
			serviceStates: []L4NetLBServiceState{
				newL4NetLBServiceState(isSuccess, unmanaged, standard, noUserError /*resyncTime = */, nil),
				newL4NetLBServiceState(isSuccess, unmanaged, premium, noUserError, nil),
				newL4NetLBServiceState(isSuccess, unmanaged, premium, noUserError, nil),
				newL4NetLBServiceState(isSuccess, managed, premium, noUserError, nil),
				newL4NetLBServiceState(isSuccess, managed, premium, noUserError, nil),
				newL4NetLBServiceState(isSuccess, managed, standard, noUserError, nil),
				newL4NetLBServiceState(isError, managed, premium, noUserError, &before25min),
				newL4NetLBServiceState(isError, managed, standard, noUserError, &before25min),
			},
			expectL4NetLBCount: netLBFeatureCount{
				service:            8,
				managedStaticIP:    3,
				premiumNetworkTier: 4,
				success:            6,
				inUserError:        0,
				inError:            2,
			},
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			newMetrics := FakeControllerMetrics()
			for i, serviceState := range tc.serviceStates {
				newMetrics.SetL4NetLBService(fmt.Sprint(i), serviceState)
			}
			got := newMetrics.computeL4NetLBMetrics()
			if diff := cmp.Diff(tc.expectL4NetLBCount, got, cmp.AllowUnexported(netLBFeatureCount{})); diff != "" {
				t.Fatalf("Got diff for L4 NetLB service counts (-want +got):\n%s", diff)
			}
		})
	}
}

func TestComputeL4NetLBDualStackMetrics(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		desc                        string
		serviceStates               []L4DualStackServiceState
		expectL4NetLBDualStackCount map[L4DualStackServiceState]int
	}{
		{
			desc:                        "empty input",
			serviceStates:               []L4DualStackServiceState{},
			expectL4NetLBDualStackCount: map[L4DualStackServiceState]int{},
		},
		{
			desc: "one l4 NetLB dual-stack service",
			serviceStates: []L4DualStackServiceState{
				newL4DualStackServiceState("IPv4", "SingleStack", StatusSuccess),
			},
			expectL4NetLBDualStackCount: map[L4DualStackServiceState]int{
				L4DualStackServiceState{"IPv4", "SingleStack", StatusSuccess}: 1,
			},
		},
		{
			desc: "l4 NetLB dual-stack service in error state",
			serviceStates: []L4DualStackServiceState{
				newL4DualStackServiceState("IPv4", "SingleStack", StatusError),
			},
			expectL4NetLBDualStackCount: map[L4DualStackServiceState]int{
				L4DualStackServiceState{"IPv4", "SingleStack", StatusError}: 1,
			},
		},
		{
			desc: "L4 NetLB dual-stack service with IPv4,IPv6 Families",
			serviceStates: []L4DualStackServiceState{
				newL4DualStackServiceState("IPv4,IPv6", "RequireDualStack", StatusSuccess),
			},
			expectL4NetLBDualStackCount: map[L4DualStackServiceState]int{
				L4DualStackServiceState{"IPv4,IPv6", "RequireDualStack", StatusSuccess}: 1,
			},
		},
		{
			desc: "many l4 NetLB dual-stack services",
			serviceStates: []L4DualStackServiceState{
				newL4DualStackServiceState("IPv4,IPv6", "RequireDualStack", StatusSuccess),
				newL4DualStackServiceState("IPv4,IPv6", "RequireDualStack", StatusSuccess),
				newL4DualStackServiceState("IPv4", "SingleStack", StatusError),
				newL4DualStackServiceState("IPv6", "SingleStack", StatusSuccess),
				newL4DualStackServiceState("IPv6", "SingleStack", StatusSuccess),
			},
			expectL4NetLBDualStackCount: map[L4DualStackServiceState]int{
				L4DualStackServiceState{"IPv4,IPv6", "RequireDualStack", StatusSuccess}: 2,
				L4DualStackServiceState{"IPv4", "SingleStack", StatusError}:             1,
				L4DualStackServiceState{"IPv6", "SingleStack", StatusSuccess}:           2,
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

func newL4NetLBServiceState(inSuccess, managed, premium, userError bool, errorTimestamp *time.Time) L4NetLBServiceState {
	return L4NetLBServiceState{
		IsPremiumTier:      premium,
		IsManagedIP:        managed,
		InSuccess:          inSuccess,
		IsUserError:        userError,
		FirstSyncErrorTime: errorTimestamp,
	}
}

func TestRetryPeriodForL4NetLBServices(t *testing.T) {
	t.Parallel()
	currTime := time.Now()
	before5min := currTime.Add(-5 * time.Minute)
	before25min := currTime.Add(-25 * time.Minute)

	svcName1 := "svc1"
	svcName2 := "svc2"
	newMetrics := FakeControllerMetrics()
	errorState := newL4NetLBServiceState(isError, managed, premium, noUserError, &currTime)
	newMetrics.SetL4NetLBService(svcName1, errorState)

	if err := checkMetricsComputation(newMetrics /*expErrorCount =*/, 0 /*expSvcCount =*/, 1); err != nil {
		t.Fatalf("Check metrics computation failed err: %v", err)
	}
	errorState.FirstSyncErrorTime = &before5min
	newMetrics.SetL4NetLBService(svcName1, errorState)
	state, ok := newMetrics.l4NetLBServiceMap[svcName1]
	if !ok {
		t.Fatalf("state should be set")
	}
	if *state.FirstSyncErrorTime == before5min {
		t.Fatal("Time Should Not be rewrite")
	}
	errorState.FirstSyncErrorTime = &before25min
	newMetrics.SetL4NetLBService(svcName2, errorState)
	if err := checkMetricsComputation(newMetrics /*expErrorCount =*/, 1 /*expSvcCount =*/, 2); err != nil {
		t.Fatalf("Check metrics computation failed err: %v", err)
	}
}

func checkMetricsComputation(newMetrics *ControllerMetrics, expErrorCount, expSvcCount int) error {
	got := newMetrics.computeL4NetLBMetrics()
	if got.inError != expErrorCount {
		return fmt.Errorf("Error count mismatch expected: %v got: %v", expErrorCount, got.inError)
	}
	if got.service != expSvcCount {
		return fmt.Errorf("Service count mismatch expected: %v got: %v", expSvcCount, got.service)
	}
	return nil
}

func TestComputeL4ILBDualStackMetrics(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		desc                      string
		serviceStates             []L4DualStackServiceState
		expectL4ILBDualStackCount map[L4DualStackServiceState]int
	}{
		{
			desc:                      "empty input",
			serviceStates:             []L4DualStackServiceState{},
			expectL4ILBDualStackCount: map[L4DualStackServiceState]int{},
		},
		{
			desc: "one l4 ilb dual-stack service",
			serviceStates: []L4DualStackServiceState{
				newL4DualStackServiceState("IPv4", "SingleStack", StatusSuccess),
			},
			expectL4ILBDualStackCount: map[L4DualStackServiceState]int{
				L4DualStackServiceState{"IPv4", "SingleStack", StatusSuccess}: 1,
			},
		},
		{
			desc: "l4 ilb dual-stack service in error state",
			serviceStates: []L4DualStackServiceState{
				newL4DualStackServiceState("IPv4", "SingleStack", StatusError),
			},
			expectL4ILBDualStackCount: map[L4DualStackServiceState]int{
				L4DualStackServiceState{"IPv4", "SingleStack", StatusError}: 1,
			},
		},
		{
			desc: "L4 ILB dual-stack service with IPv4,IPv6 Families",
			serviceStates: []L4DualStackServiceState{
				newL4DualStackServiceState("IPv4,IPv6", "RequireDualStack", StatusSuccess),
			},
			expectL4ILBDualStackCount: map[L4DualStackServiceState]int{
				L4DualStackServiceState{"IPv4,IPv6", "RequireDualStack", StatusSuccess}: 1,
			},
		},
		{
			desc: "many l4 ilb dual-stack services",
			serviceStates: []L4DualStackServiceState{
				newL4DualStackServiceState("IPv4,IPv6", "RequireDualStack", StatusSuccess),
				newL4DualStackServiceState("IPv4,IPv6", "RequireDualStack", StatusSuccess),
				newL4DualStackServiceState("IPv4", "SingleStack", StatusError),
				newL4DualStackServiceState("IPv6", "SingleStack", StatusSuccess),
				newL4DualStackServiceState("IPv6", "SingleStack", StatusSuccess),
			},
			expectL4ILBDualStackCount: map[L4DualStackServiceState]int{
				L4DualStackServiceState{"IPv4,IPv6", "RequireDualStack", StatusSuccess}: 2,
				L4DualStackServiceState{"IPv4", "SingleStack", StatusError}:             1,
				L4DualStackServiceState{"IPv6", "SingleStack", StatusSuccess}:           2,
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

func newL4DualStackServiceState(ipFamilies string, ipFamilyPolicy string, status L4DualStackServiceStatus) L4DualStackServiceState {
	return L4DualStackServiceState{
		IPFamilies:     ipFamilies,
		IPFamilyPolicy: ipFamilyPolicy,
		Status:         status,
	}
}
