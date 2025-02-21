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
	"context"
	"fmt"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/filter"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/api/compute/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/ingress-gce/pkg/composite"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	namer_util "k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/klog/v2"
	clocktesting "k8s.io/utils/clock/testing"
)

type testPatcher struct {
	count      int
	lastPod    string
	lastNegKey *meta.Key
	lastBsKey  *meta.Key
}

func (p *testPatcher) syncPod(pod string, negKey, bsKey *meta.Key) error {
	p.count++
	p.lastPod = pod
	p.lastNegKey = negKey
	p.lastBsKey = bsKey
	return nil
}

func (p *testPatcher) Eval(t *testing.T, pod string, negKey, bsKey *meta.Key) {
	if p.lastPod != pod {
		t.Errorf("got pod=%q; want=%q", p.lastPod, pod)
	}

	if diff := cmp.Diff(negKey, p.lastNegKey); diff != "" {
		t.Errorf("diff found in expected NEG; (-want +got):\n%s", diff)
	}

	if diff := cmp.Diff(bsKey, p.lastBsKey); diff != "" {
		t.Errorf("diff found in expected BackendService; (-want +got):\n%s", diff)
	}
}

func newFakePoller() (*poller, error) {
	reflector, err := newTestReadinessReflector(negtypes.NewTestContext(), false)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize reflector: %s", err)
	}
	poller := reflector.poller
	poller.patcher = &testPatcher{}
	return poller, nil
}

func TestPollerEndpointRegistrationAndScanForWork(t *testing.T) {
	t.Parallel()

	poller, err := newFakePoller()
	if err != nil {
		t.Fatalf("failed to create fake poller")
	}
	podLister := poller.podLister
	fakeLookup := poller.lookup.(*fakeLookUp)
	namespace := "ns"
	name := "svc"
	port := int32(80)
	targetPort1 := "8080"
	targetPort2 := "namedport"
	syncerKey1 := negtypes.NegSyncerKey{
		Namespace: namespace,
		Name:      name,
		PortTuple: negtypes.SvcPortTuple{
			Port:       port,
			TargetPort: targetPort1,
		},
	}
	syncerKey2 := negtypes.NegSyncerKey{
		Namespace: namespace,
		Name:      name,
		PortTuple: negtypes.SvcPortTuple{
			Port:       port,
			TargetPort: targetPort2,
		},
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

func TestPoll(t *testing.T) {
	t.Parallel()

	poller, err := newFakePoller()
	if err != nil {
		t.Fatalf("failed to create fake poller")
	}
	fakeClock := clocktesting.NewFakeClock(time.Now())
	poller.clock = fakeClock
	patcherTester := poller.patcher.(*testPatcher)
	negCloud := poller.negCloud
	namer := namer_util.NewNamer("clusteruid", "", klog.TODO())

	ns := "ns"
	podName := "pod1"
	negName := namer.NEG(ns, "svc", int32(80))
	zone := "us-central1-b"
	key := negMeta{
		SyncerKey: negtypes.NegSyncerKey{},
		Name:      negName,
		Zone:      zone,
	}
	ip := "10.1.2.3"
	port := int64(80)
	instance := "k8s-node-xxxxxx"
	irrelevantEntry := negtypes.NetworkEndpointEntry{
		NetworkEndpoint: &composite.NetworkEndpoint{
			IpAddress: ip,
			Port:      port,
			Instance:  "foo-instance",
		},
		Healths: []*composite.HealthStatusForNetworkEndpoint{
			{
				BackendService: &composite.BackendServiceReference{
					BackendService: negName,
				},
				HealthState: "HEALTHY",
			},
		},
	}

	pollAndValidate := func(desc string, expectErr bool, expectRetry bool, expectPatchCount int, stepClock bool, healthStatusDelay bool) {
		if stepClock {
			go func() {
				time.Sleep(2 * time.Second)
				delay := retryDelay
				if healthStatusDelay {
					delay = hcRetryDelay
				}
				fakeClock.Step(delay)
			}()
		}
		retry, err := poller.Poll(key)
		if expectErr && err == nil {
			t.Errorf("For case %q, expect err, but got %v", desc, err)
		} else if !expectErr && err != nil {
			t.Errorf("For case %q, does not expect err, but got %v", desc, err)
		}
		if retry != expectRetry {
			t.Errorf("For case %q, expect retry = %v, but got %v", desc, expectRetry, retry)
		}
		if patcherTester.count != expectPatchCount {
			t.Errorf("For case %q, expect patcherTester.count = %v, but got %v", desc, expectPatchCount, patcherTester.count)
		}
	}

	step := "mark polling to true"
	poller.pollMap[key] = &pollTarget{
		endpointMap: negtypes.EndpointPodMap{
			negtypes.NetworkEndpoint{IP: ip, Port: strconv.FormatInt(port, 10), Node: instance}: types.NamespacedName{Namespace: ns, Name: podName},
		},
		polling: true,
	}

	pollAndValidate(step, false, true, 0, false, false)
	pollAndValidate(step, false, true, 0, false, false)

	step = "unmark polling"
	poller.pollMap[key].polling = false
	pollAndValidate(step, true, true, 0, true, false)
	pollAndValidate(step, true, true, 0, true, false)

	step = "NEG exists, but with no endpoint"
	// create NEG, but with no endpoint
	negCloud.CreateNetworkEndpointGroup(&composite.NetworkEndpointGroup{Name: negName, Zone: zone, Version: meta.VersionGA}, zone, klog.TODO())
	pollAndValidate(step, false, true, 0, true, true)
	pollAndValidate(step, false, true, 0, true, true)

	step = "NE added to the NEG, but NE health status is empty"
	ne := &composite.NetworkEndpoint{
		IpAddress: ip,
		Port:      port,
		Instance:  instance,
	}

	negCloud.AttachNetworkEndpoints(negName, zone, []*composite.NetworkEndpoint{ne}, meta.VersionGA, klog.TODO())
	// add NE with empty healthy status
	negtypes.GetNetworkEndpointStore(negCloud).AddNetworkEndpointHealthStatus(*meta.ZonalKey(negName, zone), []negtypes.NetworkEndpointEntry{
		{
			NetworkEndpoint: ne,
			Healths:         []*composite.HealthStatusForNetworkEndpoint{},
		},
	})

	pollAndValidate(step, false, false, 1, false, false)
	pollAndValidate(step, false, false, 2, false, false)
	patcherTester.Eval(t, fmt.Sprintf("%v/%v", ns, podName), meta.ZonalKey(negName, zone), nil)

	step = "NE health status is empty and there are other endpoint with health status in NEG"
	negtypes.GetNetworkEndpointStore(negCloud).AddNetworkEndpointHealthStatus(*meta.ZonalKey(negName, zone), []negtypes.NetworkEndpointEntry{
		irrelevantEntry,
		{
			NetworkEndpoint: ne,
			Healths:         []*composite.HealthStatusForNetworkEndpoint{},
		},
	})
	pollAndValidate(step, false, true, 2, true, true)
	pollAndValidate(step, false, true, 2, true, true)

	step = "NE has nonhealthy status"
	negtypes.GetNetworkEndpointStore(negCloud).AddNetworkEndpointHealthStatus(*meta.ZonalKey(negName, zone), []negtypes.NetworkEndpointEntry{
		{
			NetworkEndpoint: ne,
			Healths: []*composite.HealthStatusForNetworkEndpoint{
				{
					BackendService: &composite.BackendServiceReference{
						BackendService: negName,
					},
					HealthState: "UNKNOWN",
				},
			},
		},
	})
	pollAndValidate(step, false, true, 2, true, true)
	pollAndValidate(step, false, true, 2, true, true)

	step = "NE has nonhealthy status with irrelevant entry"
	negtypes.GetNetworkEndpointStore(negCloud).AddNetworkEndpointHealthStatus(*meta.ZonalKey(negName, zone), []negtypes.NetworkEndpointEntry{
		irrelevantEntry,
		{
			NetworkEndpoint: ne,
			Healths: []*composite.HealthStatusForNetworkEndpoint{
				{
					BackendService: &composite.BackendServiceReference{
						BackendService: negName,
					},
					HealthState: "UNKNOWN",
				},
			},
		},
	})
	pollAndValidate(step, false, true, 2, true, true)
	pollAndValidate(step, false, true, 2, true, true)

	step = "NE has unsupported health"
	negtypes.GetNetworkEndpointStore(negCloud).AddNetworkEndpointHealthStatus(*meta.ZonalKey(negName, zone), []negtypes.NetworkEndpointEntry{
		{
			NetworkEndpoint: ne,
			Healths: []*composite.HealthStatusForNetworkEndpoint{
				{
					HealthCheckService: &composite.HealthCheckServiceReference{
						HealthCheckService: negName,
					},
					HealthState: "HEALTHY",
				},
			},
		},
	})
	pollAndValidate(step, false, false, 3, false, false)
	pollAndValidate(step, false, false, 4, false, false)

	step = "NE has healthy status"
	bsName := "bar"
	backendServiceUrl := fmt.Sprintf("https://www.googleapis.com/compute/v1/projects/foo/global/backendServices/%v", bsName)
	negtypes.GetNetworkEndpointStore(negCloud).AddNetworkEndpointHealthStatus(*meta.ZonalKey(negName, zone), []negtypes.NetworkEndpointEntry{
		{
			NetworkEndpoint: ne,
			Healths: []*composite.HealthStatusForNetworkEndpoint{
				{
					BackendService: &composite.BackendServiceReference{
						BackendService: backendServiceUrl,
					},
					HealthState: healthyState,
				},
			},
		},
		irrelevantEntry,
	})
	pollAndValidate(step, false, false, 5, false, false)
	patcherTester.Eval(t, fmt.Sprintf("%v/%v", ns, podName), meta.ZonalKey(negName, zone), meta.GlobalKey(bsName))
	pollAndValidate(step, false, false, 6, false, false)
	patcherTester.Eval(t, fmt.Sprintf("%v/%v", ns, podName), meta.ZonalKey(negName, zone), meta.GlobalKey(bsName))

	step = "ListNetworkEndpoint return error response"
	fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
	m := (fakeGCE.Compute().(*cloud.MockGCE))
	m.MockNetworkEndpointGroups.ListNetworkEndpointsHook = func(ctx context.Context, key *meta.Key, obj *compute.NetworkEndpointGroupsListEndpointsRequest, filter *filter.F, m *cloud.MockNetworkEndpointGroups, options ...cloud.Option) ([]*compute.NetworkEndpointWithHealthStatus, error) {
		return nil, fmt.Errorf("random error from GCE")
	}
	poller.negCloud = negtypes.NewAdapter(fakeGCE)
	pollAndValidate(step, true, true, 6, true, false)
	pollAndValidate(step, true, true, 6, true, false)
}

func TestProcessHealthStatus_shouldNotCrashWhenMissingKeyFromPollMap(t *testing.T) {
	t.Parallel()
	poller, err := newFakePoller()
	if err != nil {
		t.Fatalf("failed to create fake poller")
	}

	// key was not in pollMap
	key := negMeta{
		SyncerKey: negtypes.NegSyncerKey{},
		Name:      "foo",
		Zone:      "zone",
	}
	res := []*composite.NetworkEndpointWithHealthStatus{}

	// processHealthStatus should not crash when pollMap does not have
	// corresponding key.
	retry, err := poller.processHealthStatus(key, res)
	if retry != false {
		t.Errorf("expect retry == false, but got %v", retry)
	}
	if err != nil {
		t.Errorf("expect err == nil, but got %v", err)
	}
}

func TestProcessHealthStatus_dualStackNEGs(t *testing.T) {
	bsName := "bsName1"
	backendServiceURL := fmt.Sprintf("https://www.googleapis.com/compute/v1/projects/foo/global/backendServices/%v", bsName)
	namespace := "ns1"
	podName := "podName1"

	networkEndpoint := func(ipv4Address, ipv6Address string) negtypes.NetworkEndpoint {
		return negtypes.NetworkEndpoint{IP: ipv4Address, IPv6: ipv6Address, Port: "0"}
	}

	testCases := []struct {
		desc                      string
		healthStatus              *composite.NetworkEndpointWithHealthStatus
		endpointPodMap            negtypes.EndpointPodMap
		shouldUpdateReadinessGate bool
		enableDualStackNEG        bool
	}{
		{
			desc: "IPv6 endpoint should be ignored when enableDualStackNEG is false",
			healthStatus: &composite.NetworkEndpointWithHealthStatus{
				NetworkEndpoint: &composite.NetworkEndpoint{IpAddress: "10.0.0.1", Ipv6Address: "a::b"},
				Healths: []*composite.HealthStatusForNetworkEndpoint{{
					BackendService:  &composite.BackendServiceReference{BackendService: backendServiceURL},
					HealthState:     healthyState,
					Ipv6HealthState: healthyState,
				}},
			},
			endpointPodMap: negtypes.EndpointPodMap{
				// This endpoint isn't an exact match of the endpoint in health status
				// but should still get updated since IPv6 endpoint should get ignored.
				networkEndpoint("10.0.0.1", ""): {Namespace: namespace, Name: podName},
			},
			shouldUpdateReadinessGate: true,
			enableDualStackNEG:        false,
		},
		{
			desc: "only IPv6 endpoint healthy, but enableDualStackNEG is false so readiness gate should not update",
			healthStatus: &composite.NetworkEndpointWithHealthStatus{
				NetworkEndpoint: &composite.NetworkEndpoint{IpAddress: "10.0.0.1"},
				Healths: []*composite.HealthStatusForNetworkEndpoint{{
					Ipv6HealthState: healthyState, // Only IPv6 healthy.
				}},
			},
			endpointPodMap: negtypes.EndpointPodMap{
				networkEndpoint("10.0.0.1", ""): {Namespace: namespace, Name: podName},
			},
			shouldUpdateReadinessGate: false,
			enableDualStackNEG:        false,
		},
		{
			desc: "no readiness gate updated since no pod found matching endpoint",
			healthStatus: &composite.NetworkEndpointWithHealthStatus{
				NetworkEndpoint: &composite.NetworkEndpoint{IpAddress: "10.0.0.1", Ipv6Address: "a::b"},
				Healths: []*composite.HealthStatusForNetworkEndpoint{{
					HealthState:     healthyState,
					Ipv6HealthState: healthyState,
				}},
			},
			endpointPodMap: negtypes.EndpointPodMap{
				// This endpoint isn't an exact match of the endpoint in health status
				// and hence there should be no update.
				networkEndpoint("10.0.0.1", ""): {Namespace: namespace, Name: podName},
			},
			shouldUpdateReadinessGate: false,
			enableDualStackNEG:        true,
		},
		{
			desc: "both IPv4 and IPv6 healthy, readiness gate should get updated",
			healthStatus: &composite.NetworkEndpointWithHealthStatus{
				NetworkEndpoint: &composite.NetworkEndpoint{IpAddress: "10.0.0.1", Ipv6Address: "a::b"},
				Healths: []*composite.HealthStatusForNetworkEndpoint{{
					// Both IPv4 and IPv6 healthy.
					HealthState:     healthyState,
					Ipv6HealthState: healthyState,
				}},
			},
			endpointPodMap: negtypes.EndpointPodMap{
				networkEndpoint("10.0.0.1", "a::b"): {Namespace: namespace, Name: podName},
			},
			shouldUpdateReadinessGate: true,
			enableDualStackNEG:        true,
		},
		{
			desc: "only IPv4 healthy, readiness gate should get updated",
			healthStatus: &composite.NetworkEndpointWithHealthStatus{
				NetworkEndpoint: &composite.NetworkEndpoint{IpAddress: "10.0.0.1", Ipv6Address: "a::b"},
				Healths: []*composite.HealthStatusForNetworkEndpoint{{
					HealthState: healthyState, // Only IPv4 healthy.
				}},
			},
			endpointPodMap: negtypes.EndpointPodMap{
				networkEndpoint("10.0.0.1", "a::b"): {Namespace: namespace, Name: podName},
			},
			shouldUpdateReadinessGate: true,
			enableDualStackNEG:        true,
		},
		{
			desc: "only IPv6 healthy, readiness gate should get updated",
			healthStatus: &composite.NetworkEndpointWithHealthStatus{
				NetworkEndpoint: &composite.NetworkEndpoint{IpAddress: "10.0.0.1", Ipv6Address: "a::b"},
				Healths: []*composite.HealthStatusForNetworkEndpoint{{
					Ipv6HealthState: healthyState, // Only IPv6 healthy.
				}},
			},
			endpointPodMap: negtypes.EndpointPodMap{
				networkEndpoint("10.0.0.1", "a::b"): {Namespace: namespace, Name: podName},
			},
			shouldUpdateReadinessGate: true,
			enableDualStackNEG:        true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			neg := negMeta{SyncerKey: negtypes.NegSyncerKey{}, Name: "negName", Zone: "zone1"}

			// Set common fields shared by all input values in testCases.
			tc.healthStatus.Healths[0].BackendService = &composite.BackendServiceReference{BackendService: backendServiceURL}

			poller, err := newFakePoller()
			if err != nil {
				t.Fatalf("failed to create fake poller")
			}
			poller.pollMap[neg] = &pollTarget{
				endpointMap: tc.endpointPodMap,
				polling:     true,
			}
			poller.enableDualStackNEG = tc.enableDualStackNEG

			poller.processHealthStatus(neg, []*composite.NetworkEndpointWithHealthStatus{tc.healthStatus})

			patcher := poller.patcher.(*testPatcher)
			if !tc.shouldUpdateReadinessGate && patcher.count > 0 {
				t.Errorf("Readiness gates updated for %v; want no readiness gate updated", patcher.lastPod)
			}
			if tc.shouldUpdateReadinessGate && patcher.count == 0 {
				t.Errorf("No readiness gate updated; want readiness gates for %v to get updated", keyFunc(namespace, podName))
			}
			if tc.shouldUpdateReadinessGate && patcher.count > 0 {
				patcher.Eval(t, keyFunc(namespace, podName), meta.ZonalKey(neg.Name, neg.Zone), meta.GlobalKey(bsName))
			}
		})
	}
}
