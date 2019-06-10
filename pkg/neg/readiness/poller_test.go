/*
Copyright 2019 The Kubernetes Authors.

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

package readiness

import (
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"net"
	"testing"
)

func newFakePoller() *poller {
	reflector := newTestReadinessReflector(fakeContext())
	return reflector.poller
}

func TestPollerEndpointRegistrationAndScanForWork(t *testing.T) {
	poller := newFakePoller()
	podLister := poller.podLister
	fakeLookup := poller.lookup.(*fakeLookUp)
	namespace := "ns"
	name := "svc"
	port := int32(80)
	targetPort1 := "8080"
	targetPort2 := "namedport"
	syncerKey1 := negtypes.NegSyncerKey{
		Namespace:  namespace,
		Name:       name,
		Port:       port,
		TargetPort: targetPort1,
	}
	syncerKey2 := negtypes.NegSyncerKey{
		Namespace:  namespace,
		Name:       name,
		Port:       port,
		TargetPort: targetPort2,
	}
	zone1 := "zone1"
	zone2 := "zone2"
	name1 := "neg1"
	name2 := "neg2"
	neg1 := negMeta{
		SyncerKey: syncerKey1,
		Zone:      zone1,
		Name:      name1,
	}
	neg2 := negMeta{
		SyncerKey: syncerKey1,
		Zone:      zone2,
		Name:      name1,
	}
	neg3 := negMeta{
		SyncerKey: syncerKey2,
		Zone:      zone1,
		Name:      name2,
	}
	neg4 := negMeta{
		SyncerKey: syncerKey2,
		Zone:      zone2,
		Name:      name2,
	}

	for _, tc := range []struct {
		desc                string
		key                 negMeta
		inputMap            negtypes.EndpointPodMap
		mutateState         func()
		expectWork          map[negMeta]bool
		expectEndpointCount int
	}{
		{
			desc:        "empty input",
			key:         negMeta{},
			inputMap:    negtypes.EndpointPodMap{},
			mutateState: func() {},
			expectWork:  map[negMeta]bool{},
		},
		{
			desc:     "empty input 1",
			key:      neg2,
			inputMap: negtypes.EndpointPodMap{},
			mutateState: func() {
				fakeLookup.readinessGateEnabled = true
			},
			expectWork: map[negMeta]bool{},
		},
		{
			desc:     "empty input 2",
			key:      neg3,
			inputMap: generateEndpointMap(net.ParseIP("1.1.1.1"), 10, instance1, "8080"),
			mutateState: func() {
				fakeLookup.readinessGateEnabled = false
				endpointMap := generateEndpointMap(net.ParseIP("1.1.1.1"), 10, instance1, "8080")
				for _, v := range endpointMap {
					podLister.Add(generatePod(testNamespace, v.Name, true, false, false))
				}
			},
			expectWork: map[negMeta]bool{},
		},
		{
			desc:     "empty input 3",
			key:      neg4,
			inputMap: negtypes.EndpointPodMap{},
			mutateState: func() {
				fakeLookup.readinessGateEnabled = true
			},
			expectWork: map[negMeta]bool{},
		},
		{
			desc:     "add endpoint for neg1",
			key:      neg1,
			inputMap: generateEndpointMap(net.ParseIP("1.1.1.1"), 10, instance1, "8080"),
			mutateState: func() {
				fakeLookup.readinessGateEnabled = true
				endpointMap := generateEndpointMap(net.ParseIP("1.1.1.1"), 10, instance1, "8080")
				for _, v := range endpointMap {
					podLister.Add(generatePod(testNamespace, v.Name, true, false, false))
				}
			},
			expectWork: map[negMeta]bool{
				neg1: true,
			},
			expectEndpointCount: 10,
		},
		{
			desc:     "add endpoint for neg2",
			key:      neg2,
			inputMap: generateEndpointMap(net.ParseIP("1.1.2.1"), 10, instance2, "8080"),
			mutateState: func() {
				fakeLookup.readinessGateEnabled = true
				endpointMap := generateEndpointMap(net.ParseIP("1.1.2.1"), 10, instance2, "8080")
				for _, v := range endpointMap {
					podLister.Add(generatePod(testNamespace, v.Name, true, false, false))
				}
			},
			expectWork: map[negMeta]bool{
				neg1: true,
				neg2: true,
			},
			expectEndpointCount: 10,
		},
		{
			desc:     "add endpoint for neg3, half of endpoint is already ready",
			key:      neg3,
			inputMap: generateEndpointMap(net.ParseIP("1.1.3.1"), 10, instance3, "8080"),
			mutateState: func() {
				fakeLookup.readinessGateEnabled = true
				endpointMap := generateEndpointMap(net.ParseIP("1.1.3.1"), 10, instance3, "8080")
				count := 0
				for _, v := range endpointMap {
					count++
					var negConditionTrue bool
					if count > 5 {
						negConditionTrue = true
					}
					podLister.Add(generatePod(testNamespace, v.Name, true, negConditionTrue, negConditionTrue))
				}
			},
			expectWork: map[negMeta]bool{
				neg1: true,
				neg2: true,
				neg3: true,
			},
			expectEndpointCount: 5,
		},
		{
			desc:     "add endpoint for neg4, half of endpoint does not have neg readiness gate",
			key:      neg4,
			inputMap: generateEndpointMap(net.ParseIP("1.1.4.1"), 10, instance4, "8080"),
			mutateState: func() {
				fakeLookup.readinessGateEnabled = true
				endpointMap := generateEndpointMap(net.ParseIP("1.1.4.1"), 10, instance4, "8080")
				count := 0
				for _, v := range endpointMap {
					count++
					var negReadinessGate bool
					if count > 5 {
						negReadinessGate = true
					}
					podLister.Add(generatePod(testNamespace, v.Name, negReadinessGate, false, false))
				}
			},
			expectWork: map[negMeta]bool{
				neg1: true,
				neg2: true,
				neg3: true,
				neg4: true,
			},
			expectEndpointCount: 5,
		},
		{
			desc:     "change endpoints for neg1",
			key:      neg1,
			inputMap: generateEndpointMap(net.ParseIP("1.1.2.1"), 5, instance2, "8080"),
			mutateState: func() {
				fakeLookup.readinessGateEnabled = true
				endpointMap := generateEndpointMap(net.ParseIP("1.1.2.1"), 5, instance2, "8080")
				for _, v := range endpointMap {
					podLister.Add(generatePod(testNamespace, v.Name, true, false, false))
				}
			},
			expectWork: map[negMeta]bool{
				neg1: true,
				neg2: true,
				neg3: true,
				neg4: true,
			},
			expectEndpointCount: 5,
		},
		{
			desc:     "mark neg3, neg4 for polling",
			key:      negMeta{},
			inputMap: negtypes.EndpointPodMap{},
			mutateState: func() {
				poller.markPolling(neg3)
				poller.markPolling(neg4)
			},
			expectWork: map[negMeta]bool{
				neg1: true,
				neg2: true,
			},
		},
		{
			desc:     "mark neg1, neg2 for polling",
			key:      negMeta{},
			inputMap: negtypes.EndpointPodMap{},
			mutateState: func() {
				poller.markPolling(neg1)
				poller.markPolling(neg2)
			},
			expectWork: map[negMeta]bool{},
		},
		{
			desc:     "unmark neg1 polling",
			key:      negMeta{},
			inputMap: negtypes.EndpointPodMap{},
			mutateState: func() {
				poller.unMarkPolling(neg1)

			},
			expectWork: map[negMeta]bool{
				neg1: true,
			},
		},
		{
			desc:     "no longer need readiness gate for neg1",
			key:      neg1,
			inputMap: generateEndpointMap(net.ParseIP("1.1.1.1"), 10, instance1, "8080"),
			mutateState: func() {
				fakeLookup.readinessGateEnabled = false
				endpointMap := generateEndpointMap(net.ParseIP("1.1.1.1"), 10, instance1, "8080")
				for _, v := range endpointMap {
					podLister.Add(generatePod(testNamespace, v.Name, true, false, false))
				}
			},
			expectWork:          map[negMeta]bool{},
			expectEndpointCount: 0,
		},
	} {
		tc.mutateState()
		poller.RegisterNegEndpoints(tc.key, tc.inputMap)
		ret := poller.ScanForWork()
		if len(ret) != len(tc.expectWork) {
			t.Errorf("For test case %q, expect %v, got: %v", tc.desc, tc.expectWork, ret)
		}
		for _, key := range ret {
			if _, ok := tc.expectWork[key]; !ok {
				t.Errorf("For test case %q, expect work %v to not exists, but it exists", tc.desc, key)
			}
		}

		target, ok := poller.pollMap[tc.key]
		if tc.expectEndpointCount == 0 && ok {
			t.Errorf("For test case %q, expect key %v to not exists in pollMap, got: %v", tc.desc, tc.key, target)
		}
		if tc.expectEndpointCount > 0 {
			if !ok {
				t.Errorf("For test case %q, expect key %v to exists in pollMap, got nil", tc.desc, tc.key)
			}
			if ok && tc.expectEndpointCount != len(target.endpointMap) {
				t.Errorf("For test case %q, expect endpoint count %v, but got: %v", tc.desc, tc.expectEndpointCount, len(target.endpointMap))
			}
		}
	}
}
