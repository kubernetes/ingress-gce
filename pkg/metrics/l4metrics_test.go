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

	"github.com/google/go-cmp/cmp"
)

const (
	premium             = true
	standard            = false
	managed             = true
	unmanaged           = false
	isSuccess           = true
	isError             = false
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
			},
		},
		{
			desc: "one l4 ilb service",
			serviceStates: []L4ILBServiceState{
				newL4ILBServiceState(disableGlobalAccess, disableCustomSubnet, isSuccess),
			},
			expectL4ILBCount: map[feature]int{
				l4ILBService:      1,
				l4ILBGlobalAccess: 0,
				l4ILBCustomSubnet: 0,
				l4ILBInSuccess:    1,
				l4ILBInError:      0,
			},
		},
		{
			desc: "l4 ilb service in error state",
			serviceStates: []L4ILBServiceState{
				newL4ILBServiceState(disableGlobalAccess, enableCustomSubnet, isError),
			},
			expectL4ILBCount: map[feature]int{
				l4ILBService:      1,
				l4ILBGlobalAccess: 0,
				l4ILBCustomSubnet: 0,
				l4ILBInSuccess:    0,
				l4ILBInError:      1,
			},
		},
		{
			desc: "global access for l4 ilb service enabled",
			serviceStates: []L4ILBServiceState{
				newL4ILBServiceState(enableGlobalAccess, disableCustomSubnet, isSuccess),
			},
			expectL4ILBCount: map[feature]int{
				l4ILBService:      1,
				l4ILBGlobalAccess: 1,
				l4ILBCustomSubnet: 0,
				l4ILBInSuccess:    1,
				l4ILBInError:      0,
			},
		},
		{
			desc: "custom subnet for l4 ilb service enabled",
			serviceStates: []L4ILBServiceState{
				newL4ILBServiceState(disableGlobalAccess, enableCustomSubnet, isSuccess),
			},
			expectL4ILBCount: map[feature]int{
				l4ILBService:      1,
				l4ILBGlobalAccess: 0,
				l4ILBCustomSubnet: 1,
				l4ILBInSuccess:    1,
				l4ILBInError:      0,
			},
		},
		{
			desc: "both global access and custom subnet for l4 ilb service enabled",
			serviceStates: []L4ILBServiceState{
				newL4ILBServiceState(enableGlobalAccess, enableCustomSubnet, isSuccess),
			},
			expectL4ILBCount: map[feature]int{
				l4ILBService:      1,
				l4ILBGlobalAccess: 1,
				l4ILBCustomSubnet: 1,
				l4ILBInSuccess:    1,
				l4ILBInError:      0,
			},
		},
		{
			desc: "many l4 ilb services",
			serviceStates: []L4ILBServiceState{
				newL4ILBServiceState(disableGlobalAccess, disableCustomSubnet, isSuccess),
				newL4ILBServiceState(disableGlobalAccess, enableCustomSubnet, isSuccess),
				newL4ILBServiceState(enableGlobalAccess, disableCustomSubnet, isSuccess),
				newL4ILBServiceState(enableGlobalAccess, enableCustomSubnet, isSuccess),
			},
			expectL4ILBCount: map[feature]int{
				l4ILBService:      4,
				l4ILBGlobalAccess: 2,
				l4ILBCustomSubnet: 2,
				l4ILBInSuccess:    4,
				l4ILBInError:      0,
			},
		},
		{
			desc: "many l4 ilb services with some in error state",
			serviceStates: []L4ILBServiceState{
				newL4ILBServiceState(disableGlobalAccess, disableCustomSubnet, isSuccess),
				newL4ILBServiceState(disableGlobalAccess, enableCustomSubnet, isError),
				newL4ILBServiceState(disableGlobalAccess, enableCustomSubnet, isSuccess),
				newL4ILBServiceState(enableGlobalAccess, disableCustomSubnet, isSuccess),
				newL4ILBServiceState(enableGlobalAccess, disableCustomSubnet, isError),
				newL4ILBServiceState(enableGlobalAccess, enableCustomSubnet, isSuccess),
			},
			expectL4ILBCount: map[feature]int{
				l4ILBService:      6,
				l4ILBGlobalAccess: 2,
				l4ILBCustomSubnet: 2,
				l4ILBInSuccess:    4,
				l4ILBInError:      2,
			},
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			newMetrics := NewControllerMetrics()
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

func newL4ILBServiceState(globalAccess, customSubnet, inSuccess bool) L4ILBServiceState {
	return L4ILBServiceState{
		EnabledGlobalAccess: globalAccess,
		EnabledCustomSubnet: customSubnet,
		InSuccess:           inSuccess,
	}
}

func TestComputeL4NetLBMetrics(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		desc               string
		serviceStates      []L4NetLBServiceState
		expectL4NetLBCount map[feature]int
	}{
		{
			desc:          "empty input",
			serviceStates: []L4NetLBServiceState{},
			expectL4NetLBCount: map[feature]int{
				l4NetLBService:            0,
				l4NetLBManagedStaticIP:    0,
				l4NetLBStaticIP:           0,
				l4NetLBPremiumNetworkTier: 0,
				l4NetLBInSuccess:          0,
				l4NetLBInError:            0,
			},
		},
		{
			desc: "one l4 Netlb service",
			serviceStates: []L4NetLBServiceState{
				newL4NetLBServiceState(isSuccess, unmanaged, standard),
			},
			expectL4NetLBCount: map[feature]int{
				l4NetLBService:            1,
				l4NetLBManagedStaticIP:    0,
				l4NetLBStaticIP:           1,
				l4NetLBPremiumNetworkTier: 0,
				l4NetLBInSuccess:          1,
				l4NetLBInError:            0,
			},
		},
		{
			desc: "one l4 Netlb service in premium network tier",
			serviceStates: []L4NetLBServiceState{
				newL4NetLBServiceState(isSuccess, unmanaged, premium),
			},
			expectL4NetLBCount: map[feature]int{
				l4NetLBService:            1,
				l4NetLBManagedStaticIP:    0,
				l4NetLBStaticIP:           1,
				l4NetLBPremiumNetworkTier: 1,
				l4NetLBInSuccess:          1,
				l4NetLBInError:            0,
			},
		},
		{
			desc: "one l4 Netlb service in premium network tier and managed static ip",
			serviceStates: []L4NetLBServiceState{
				newL4NetLBServiceState(isSuccess, managed, premium),
			},
			expectL4NetLBCount: map[feature]int{
				l4NetLBService:            1,
				l4NetLBManagedStaticIP:    1,
				l4NetLBStaticIP:           1,
				l4NetLBPremiumNetworkTier: 1,
				l4NetLBInSuccess:          1,
				l4NetLBInError:            0,
			},
		},
		{
			desc: "l4 Netlb service in error state",
			serviceStates: []L4NetLBServiceState{
				newL4NetLBServiceState(isError, unmanaged, standard),
			},
			expectL4NetLBCount: map[feature]int{
				l4NetLBService:            1,
				l4NetLBManagedStaticIP:    0,
				l4NetLBStaticIP:           0,
				l4NetLBPremiumNetworkTier: 0,
				l4NetLBInSuccess:          0,
				l4NetLBInError:            1,
			},
		},
		{
			desc: "l4 Netlb service in error state should not count static ip and network tier",
			serviceStates: []L4NetLBServiceState{
				newL4NetLBServiceState(isError, unmanaged, standard),
				newL4NetLBServiceState(isError, managed, premium),
			},
			expectL4NetLBCount: map[feature]int{
				l4NetLBService:            2,
				l4NetLBManagedStaticIP:    0,
				l4NetLBStaticIP:           0,
				l4NetLBPremiumNetworkTier: 0,
				l4NetLBInSuccess:          0,
				l4NetLBInError:            2,
			},
		},
		{
			desc: "two l4 Netlb services with different network tier",
			serviceStates: []L4NetLBServiceState{
				newL4NetLBServiceState(isSuccess, unmanaged, standard),
				newL4NetLBServiceState(isSuccess, unmanaged, premium),
			},
			expectL4NetLBCount: map[feature]int{
				l4NetLBService:            2,
				l4NetLBManagedStaticIP:    0,
				l4NetLBStaticIP:           2,
				l4NetLBPremiumNetworkTier: 1,
				l4NetLBInSuccess:          2,
				l4NetLBInError:            0,
			},
		},
		{
			desc: "multi l4 Netlb services",
			serviceStates: []L4NetLBServiceState{
				newL4NetLBServiceState(isSuccess, unmanaged, standard),
				newL4NetLBServiceState(isSuccess, unmanaged, premium),
				newL4NetLBServiceState(isSuccess, unmanaged, premium),
				newL4NetLBServiceState(isSuccess, managed, premium),
				newL4NetLBServiceState(isSuccess, managed, premium),
				newL4NetLBServiceState(isSuccess, managed, standard),
				newL4NetLBServiceState(isError, managed, premium),
				newL4NetLBServiceState(isError, managed, standard),
			},
			expectL4NetLBCount: map[feature]int{
				l4NetLBService:            8,
				l4NetLBManagedStaticIP:    3,
				l4NetLBStaticIP:           6,
				l4NetLBPremiumNetworkTier: 4,
				l4NetLBInSuccess:          6,
				l4NetLBInError:            2,
			},
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			newMetrics := NewControllerMetrics()
			for i, serviceState := range tc.serviceStates {
				newMetrics.SetL4NetLBService(fmt.Sprint(i), serviceState)
			}
			got := newMetrics.computeL4NetLBMetrics()
			if diff := cmp.Diff(tc.expectL4NetLBCount, got); diff != "" {
				t.Fatalf("Got diff for L4 NetLB service counts (-want +got):\n%s", diff)
			}
		})
	}
}

func newL4NetLBServiceState(inSuccess, managed, premium bool) L4NetLBServiceState {
	return L4NetLBServiceState{
		IsPremiumTier: premium,
		IsManagedIP:   managed,
		InSuccess:     inSuccess,
	}
}
