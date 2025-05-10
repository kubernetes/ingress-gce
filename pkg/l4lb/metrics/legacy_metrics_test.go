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
		serviceStates    []L4ILBServiceLegacyState
		expectL4ILBCount map[feature]int
	}{
		{
			desc:          "empty input",
			serviceStates: []L4ILBServiceLegacyState{},
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
			serviceStates: []L4ILBServiceLegacyState{
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
			serviceStates: []L4ILBServiceLegacyState{
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
			serviceStates: []L4ILBServiceLegacyState{
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
			serviceStates: []L4ILBServiceLegacyState{
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
			serviceStates: []L4ILBServiceLegacyState{
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
			serviceStates: []L4ILBServiceLegacyState{
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
			serviceStates: []L4ILBServiceLegacyState{
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
			serviceStates: []L4ILBServiceLegacyState{
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
			newMetrics := NewFakeCollector()
			for i, serviceState := range tc.serviceStates {
				newMetrics.SetL4ILBServiceForLegacyMetric(fmt.Sprint(i), serviceState)
			}
			got := newMetrics.computeL4ILBLegacyMetrics()
			if diff := cmp.Diff(tc.expectL4ILBCount, got); diff != "" {
				t.Fatalf("Got diff for L4 ILB service counts (-want +got):\n%s", diff)
			}
		})
	}
}

func newL4ILBServiceState(globalAccess bool, customSubnet bool, inSuccess bool, isUserError bool) L4ILBServiceLegacyState {
	return L4ILBServiceLegacyState{
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
		serviceStates      []L4NetLBServiceLegacyState
		expectL4NetLBCount netLBFeatureCount
	}{
		{
			desc:          "empty input",
			serviceStates: []L4NetLBServiceLegacyState{},
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
			serviceStates: []L4NetLBServiceLegacyState{
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
			serviceStates: []L4NetLBServiceLegacyState{
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
			serviceStates: []L4NetLBServiceLegacyState{
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
			serviceStates: []L4NetLBServiceLegacyState{
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
			serviceStates: []L4NetLBServiceLegacyState{
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
			serviceStates: []L4NetLBServiceLegacyState{
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
			serviceStates: []L4NetLBServiceLegacyState{
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
			serviceStates: []L4NetLBServiceLegacyState{
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
			serviceStates: []L4NetLBServiceLegacyState{
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
			newMetrics := NewFakeCollector()
			for i, serviceState := range tc.serviceStates {
				newMetrics.SetL4NetLBServiceForLegacyMetric(fmt.Sprint(i), serviceState)
			}
			got := newMetrics.computeL4NetLBLegacyMetrics()
			if diff := cmp.Diff(tc.expectL4NetLBCount, got, cmp.AllowUnexported(netLBFeatureCount{})); diff != "" {
				t.Fatalf("Got diff for L4 NetLB service counts (-want +got):\n%s", diff)
			}
		})
	}
}

func newL4NetLBServiceState(inSuccess, managed, premium, userError bool, errorTimestamp *time.Time) L4NetLBServiceLegacyState {
	return L4NetLBServiceLegacyState{
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
	newMetrics := NewFakeCollector()
	errorState := newL4NetLBServiceState(isError, managed, premium, noUserError, &currTime)
	newMetrics.SetL4NetLBServiceForLegacyMetric(svcName1, errorState)

	if err := checkMetricsComputation(newMetrics /*expErrorCount =*/, 0 /*expSvcCount =*/, 1); err != nil {
		t.Fatalf("Check metrics computation failed err: %v", err)
	}
	errorState.FirstSyncErrorTime = &before5min
	newMetrics.SetL4NetLBServiceForLegacyMetric(svcName1, errorState)
	state, ok := newMetrics.l4NetLBServiceLegacyMap[svcName1]
	if !ok {
		t.Fatalf("state should be set")
	}
	if *state.FirstSyncErrorTime == before5min {
		t.Fatal("Time Should Not be rewrite")
	}
	errorState.FirstSyncErrorTime = &before25min
	newMetrics.SetL4NetLBServiceForLegacyMetric(svcName2, errorState)
	if err := checkMetricsComputation(newMetrics /*expErrorCount =*/, 1 /*expSvcCount =*/, 2); err != nil {
		t.Fatalf("Check metrics computation failed err: %v", err)
	}
}

func checkMetricsComputation(newMetrics *Collector, expErrorCount, expSvcCount int) error {
	got := newMetrics.computeL4NetLBLegacyMetrics()
	if got.inError != expErrorCount {
		return fmt.Errorf("Error count mismatch expected: %v got: %v", expErrorCount, got.inError)
	}
	if got.service != expSvcCount {
		return fmt.Errorf("Service count mismatch expected: %v got: %v", expSvcCount, got.service)
	}
	return nil
}
