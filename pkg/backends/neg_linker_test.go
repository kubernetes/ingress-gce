/*
Copyright 2018 The Kubernetes Authors.
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

package backends

import (
	"fmt"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/ingress-gce/pkg/apis/svcneg/v1beta1"
	"k8s.io/klog/v2"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/mock"
	"github.com/google/go-cmp/cmp"
	"github.com/kr/pretty"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"

	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/ingress-gce/pkg/annotations"
	befeatures "k8s.io/ingress-gce/pkg/backends/features"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/flags"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/ingress-gce/pkg/utils"
)

const (
	testZone1 = "zone1"
	testZone2 = "zone2"
)

func newTestNEGLinker(fakeNEG negtypes.NetworkEndpointGroupCloud, fakeGCE *gce.Cloud) *negLinker {
	fakeBackendPool := NewPool(fakeGCE, defaultNamer)
	ctx := negtypes.NewTestContext()

	// Add standard hooks for mocking update calls. Each test can set a update different hook if it chooses to.
	(fakeGCE.Compute().(*cloud.MockGCE)).MockAlphaBackendServices.UpdateHook = mock.UpdateAlphaBackendServiceHook
	(fakeGCE.Compute().(*cloud.MockGCE)).MockBetaBackendServices.UpdateHook = mock.UpdateBetaBackendServiceHook
	(fakeGCE.Compute().(*cloud.MockGCE)).MockBackendServices.UpdateHook = mock.UpdateBackendServiceHook
	(fakeGCE.Compute().(*cloud.MockGCE)).MockAlphaRegionBackendServices.UpdateHook = mock.UpdateAlphaRegionBackendServiceHook
	(fakeGCE.Compute().(*cloud.MockGCE)).MockBetaRegionBackendServices.UpdateHook = mock.UpdateBetaRegionBackendServiceHook
	(fakeGCE.Compute().(*cloud.MockGCE)).MockRegionBackendServices.UpdateHook = mock.UpdateRegionBackendServiceHook
	return &negLinker{fakeBackendPool, fakeNEG, fakeGCE, ctx.SvcNegInformer.GetIndexer(), false, klog.TODO()}
}

func TestLinkWithDifferentSvcPorts(t *testing.T) {
	t.Parallel()

	zones := []GroupKey{{Zone: "zone1"}, {Zone: "zone2"}}
	namespace, svcName, port := "ns", "name", "port"
	svc := types.NamespacedName{Namespace: namespace, Name: svcName}

	// validate different service port for both L4 ILB and L7 LBs
	testCases := []struct {
		desc    string
		svcPort utils.ServicePort
	}{
		{
			desc: "Link L4 NEGs",
			svcPort: utils.ServicePort{
				ID:             utils.ServicePortID{Service: svc},
				BackendNamer:   defaultL4Namer,
				VMIPNEGEnabled: true,
			},
		},
		{
			desc: "Link Ingress NEGs",
			svcPort: utils.ServicePort{
				ID:           utils.ServicePortID{Service: svc},
				Port:         80,
				NodePort:     30001,
				Protocol:     annotations.ProtocolHTTP,
				TargetPort:   intstr.FromString(port),
				NEGEnabled:   true,
				BackendNamer: defaultNamer,
			},
		},
		{
			desc: "Link RXLB Ingress",
			svcPort: utils.ServicePort{
				ID:                   utils.ServicePortID{Service: svc},
				Port:                 80,
				NodePort:             30001,
				Protocol:             annotations.ProtocolHTTP,
				TargetPort:           intstr.FromString(port),
				NEGEnabled:           true,
				L7XLBRegionalEnabled: true,
				BackendNamer:         defaultNamer,
			},
		},
	}

	for _, tc := range testCases {
		for _, populateSvcNeg := range []bool{true, false} {
			t.Run(tc.desc, func(t *testing.T) {
				fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
				fakeNEG := negtypes.NewFakeNetworkEndpointGroupCloud("test-subnetwork", "test-network")
				linker := newTestNEGLinker(fakeNEG, fakeGCE)

				// Mimic how the syncer would create the backend.
				if _, err := linker.backendPool.Create(tc.svcPort, "fake-healthcheck-link", klog.TODO()); err != nil {
					t.Fatalf("Failed to create backend service to NEG for svcPort %v: %v", tc.svcPort, err)
				}

				version := befeatures.VersionFromServicePort(&tc.svcPort)

				if populateSvcNeg {
					svcNeg := &v1beta1.ServiceNetworkEndpointGroup{
						TypeMeta: metav1.TypeMeta{
							Kind:       "ServiceNetworkEndpointGroup",
							APIVersion: "networking.gke.io/v1beta1",
						},
						ObjectMeta: metav1.ObjectMeta{
							Name:      tc.svcPort.NEGName(),
							Namespace: namespace,
						},
						Status: v1beta1.ServiceNetworkEndpointGroupStatus{
							NetworkEndpointGroups: []v1beta1.NegObjectReference{
								{SelfLink: fmt.Sprintf("https://www.googleapis.com/compute/alpha/projects/mock-project/zones/zone1/networkEndpointGroups/%s", tc.svcPort.NEGName())},
								{SelfLink: fmt.Sprintf("https://www.googleapis.com/compute/alpha/projects/mock-project/zones/zone2/networkEndpointGroups/%s", tc.svcPort.NEGName())},
							},
						},
					}
					if err := linker.svcNegLister.Add(svcNeg); err != nil {
						t.Fatalf("Failed to add svcneg: %v", err)
					}
				}

				for _, key := range zones {
					neg := &composite.NetworkEndpointGroup{
						Name:    tc.svcPort.NEGName(),
						Version: version,
					}
					if tc.svcPort.VMIPNEGEnabled {
						neg.NetworkEndpointType = string(negtypes.VmIpEndpointType)
					}
					err := fakeNEG.CreateNetworkEndpointGroup(neg, key.Zone, klog.TODO())
					if err != nil {
						t.Fatalf("unexpected error creating NEG for svcPort %v: %v", tc.svcPort, err)
					}
				}

				if err := linker.Link(tc.svcPort, zones); err != nil {
					t.Fatalf("Failed to link backend service to NEG for svcPort %v when populateSvcNeg = %v: %v", tc.svcPort, populateSvcNeg, err)
				}

				// validate function validates if the state is expected
				validate := func() {
					beName := tc.svcPort.BackendName()
					scope := befeatures.ScopeFromServicePort(&tc.svcPort)
					key, err := composite.CreateKey(fakeGCE, beName, scope)
					if err != nil {
						t.Fatalf("Failed to create composite key - %v", err)
					}
					bs, err := composite.GetBackendService(fakeGCE, key, version, klog.TODO())
					if err != nil {
						t.Fatalf("Failed to retrieve backend service using key %+v for svcPort %v: %v", key, tc.svcPort, err)
					}
					if len(bs.Backends) != len(zones) {
						t.Errorf("Expect %v backends in backend service %s, but got %v.key %+v %+v", len(zones), beName, len(bs.Backends), key, bs)
					}

					for _, be := range bs.Backends {
						neg := "networkEndpointGroups"
						if !strings.Contains(be.Group, neg) {
							t.Errorf("Got backend link %q, want containing %q", be.Group, neg)
						}
						if tc.svcPort.VMIPNEGEnabled {
							// Balancing mode should be connection, rate should be unset
							if be.BalancingMode != string(Connections) || be.MaxRatePerEndpoint != 0 {
								t.Errorf("Only 'Connection' balancing mode is supported with VM_IP NEGs, Got %q with max rate %v", be.BalancingMode, be.MaxRatePerEndpoint)
							}
						}
					}
				}

				validate()

				// mimic cluster node shrinks to one of the zone, so we only have nodes in zone1
				shrinkZone := []GroupKey{zones[0]}
				if populateSvcNeg {
					svcNegAfterShrink := &v1beta1.ServiceNetworkEndpointGroup{
						TypeMeta: metav1.TypeMeta{
							Kind:       "ServiceNetworkEndpointGroup",
							APIVersion: "networking.gke.io/v1beta1",
						},
						ObjectMeta: metav1.ObjectMeta{
							Name:      tc.svcPort.NEGName(),
							Namespace: namespace,
						},
						Status: v1beta1.ServiceNetworkEndpointGroupStatus{
							NetworkEndpointGroups: []v1beta1.NegObjectReference{
								{
									SelfLink: fmt.Sprintf("https://www.googleapis.com/compute/alpha/projects/mock-project/zones/zone1/networkEndpointGroups/%s", tc.svcPort.NEGName()),
									State:    v1beta1.ActiveState,
								},
								{
									SelfLink: fmt.Sprintf("https://www.googleapis.com/compute/alpha/projects/mock-project/zones/zone2/networkEndpointGroups/%s", tc.svcPort.NEGName()),
									State:    v1beta1.InactiveState, // NEG is marked as Inactive when there is no node in this zone.
								},
							},
						},
					}
					linker.svcNegLister.Update(svcNegAfterShrink)
				}

				if err := linker.Link(tc.svcPort, shrinkZone); err != nil {
					t.Fatalf("Failed to link backend service to NEG for svcPort %v when populateSvcNeg = %v: %v", tc.svcPort, populateSvcNeg, err)
				}

				validate()

			})
		}
	}
}

func TestLinkWithNEGUpdates(t *testing.T) {
	t.Parallel()

	namespace, svcName, port := "ns", "name", "port"
	svc := types.NamespacedName{Namespace: namespace, Name: svcName}
	svcPort := utils.ServicePort{
		ID:           utils.ServicePortID{Service: svc},
		Port:         80,
		Protocol:     annotations.ProtocolHTTP,
		TargetPort:   intstr.FromString(port),
		NEGEnabled:   true,
		BackendNamer: defaultNamer,
	}
	scope := befeatures.ScopeFromServicePort(&svcPort)
	version := befeatures.VersionFromServicePort(&svcPort)

	negName := svcPort.NEGName()
	beName := svcPort.BackendName()

	negUrl1 := fmt.Sprintf("https://www.googleapis.com/compute/alpha/projects/mock-project/zones/%s/networkEndpointGroups/%s", testZone1, negName)
	negUrl2 := fmt.Sprintf("https://www.googleapis.com/compute/alpha/projects/mock-project/zones/%s/networkEndpointGroups/%s", testZone2, negName)

	testCases := []struct {
		desc             string
		prevGroups       []GroupKey
		prevBackends     []*composite.Backend
		currGroups       []GroupKey
		currentNegObjRef []v1beta1.NegObjectReference
		expectedBackends []*composite.Backend
	}{
		{
			desc:         "Add a new zone",
			prevGroups:   []GroupKey{{Zone: testZone1}},
			prevBackends: []*composite.Backend{{Group: negUrl1}},
			currGroups:   []GroupKey{{Zone: testZone1}, {Zone: testZone2}},
			currentNegObjRef: []v1beta1.NegObjectReference{
				createNegRef(testZone1, negName, ""),
				createNegRef(testZone2, negName, ""),
			},
			expectedBackends: []*composite.Backend{{Group: negUrl1}, {Group: negUrl2}},
		},
		{
			desc:         "Remove a zone, the Backends should stay the same",
			prevGroups:   []GroupKey{{Zone: testZone1}, {Zone: testZone2}},
			prevBackends: []*composite.Backend{{Group: negUrl1}, {Group: negUrl2}},
			currGroups:   []GroupKey{{Zone: testZone1}},
			currentNegObjRef: []v1beta1.NegObjectReference{
				createNegRef(testZone1, negName, ""),
			},
			expectedBackends: []*composite.Backend{{Group: negUrl1}, {Group: negUrl2}},
		},
	}

	for _, tc := range testCases {
		for _, enableMultiSubnetClusterPhase1 := range []bool{true, false} {
			for _, populateSvcNeg := range []bool{true, false} {
				testName := fmt.Sprintf("%s, populateSvcNeg=%v", tc.desc, populateSvcNeg)
				t.Run(testName, func(t *testing.T) {
					fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
					fakeNEG := negtypes.NewFakeNetworkEndpointGroupCloud("test-subnetwork", "test-network")
					linker := newTestNEGLinker(fakeNEG, fakeGCE)
					linker.enableMultiSubnetClusterPhase1 = enableMultiSubnetClusterPhase1

					for _, zone := range []string{testZone1, testZone2} {
						neg := &composite.NetworkEndpointGroup{
							Name:    negName,
							Scope:   scope,
							Version: version,
						}
						if err := fakeNEG.CreateNetworkEndpointGroup(neg, zone, klog.TODO()); err != nil {
							t.Fatalf("unexpected error creating NEG for svcPort %v: %v", svcPort, err)
						}
					}

					prevBe := &composite.BackendService{
						Version:  version,
						Scope:    scope,
						Backends: tc.prevBackends,
					}
					key, err := composite.CreateKey(fakeGCE, beName, scope)
					if err != nil {
						t.Fatalf("Failed to create Backend Service key: %v", err)
					}
					if err := composite.CreateBackendService(fakeGCE, key, prevBe, klog.TODO()); err != nil {
						t.Fatalf("Failed to create Backend Service: %v", err)
					}

					if populateSvcNeg {
						svcNeg := &v1beta1.ServiceNetworkEndpointGroup{
							TypeMeta: metav1.TypeMeta{
								Kind:       "ServiceNetworkEndpointGroup",
								APIVersion: "networking.gke.io/v1beta1",
							},
							ObjectMeta: metav1.ObjectMeta{
								Name:      svcPort.NEGName(),
								Namespace: namespace,
							},
							Status: v1beta1.ServiceNetworkEndpointGroupStatus{
								NetworkEndpointGroups: tc.currentNegObjRef,
							},
						}
						if err := linker.svcNegLister.Add(svcNeg); err != nil {
							t.Fatalf("Failed to add svcneg: %v", err)
						}
					}

					if err := linker.Link(svcPort, tc.currGroups); err != nil {
						t.Fatalf("Failed to link Backend Service to NEG: %v", err)
					}

					updatedBe, err := composite.GetBackendService(fakeGCE, key, version, klog.TODO())
					if err != nil {
						t.Fatalf("Failed to get Backend Service: %v", err)
					}

					if diff := diffBackends(updatedBe.Backends, tc.expectedBackends, klog.TODO()); !diff.isEqual() {
						t.Fatalf("Got backends %v after Link(), expected %v", updatedBe.Backends, tc.expectedBackends)
					}
				})
			}
		}
	}
}

func TestGetNegSelfLinks(t *testing.T) {
	t.Parallel()

	groupKeys := []GroupKey{{Zone: testZone1}, {Zone: testZone2}}
	namespace, svcName, port := "ns", "name", "port"
	svc := types.NamespacedName{Namespace: namespace, Name: svcName}
	svcPort := utils.ServicePort{
		ID:           utils.ServicePortID{Service: svc},
		Port:         80,
		Protocol:     annotations.ProtocolHTTP,
		TargetPort:   intstr.FromString(port),
		NEGEnabled:   true,
		BackendNamer: defaultNamer,
	}
	negName := svcPort.NEGName()

	testCases := []struct {
		desc             string
		populateSvcNeg   bool
		testNegRef       []v1beta1.NegObjectReference
		expectedNegCount int
	}{
		{
			desc:           "Get NEGs from SvcNeg based on node zones",
			populateSvcNeg: true,
			// NEG state is empty for NEG created with MultiSubnetClusterPhase1=false
			testNegRef: []v1beta1.NegObjectReference{
				createNegRef(testZone1, negName, ""),
				createNegRef(testZone2, negName, ""),
			},
			expectedNegCount: 2,
		},
		{
			desc:             "Get NEGs from GCE based on node zones",
			populateSvcNeg:   false,
			expectedNegCount: 2,
		},
	}

	for _, tc := range testCases {
		for _, enableMultiSubnetPhase1 := range []bool{true, false} {
			// When SvcNeg is populated:
			// If EnableMultiSubnetClusterPhase1=true, getNegSelfLinks gets
			// NEGs based on all NEG ObjectReference in NEG CR.
			// Otherwise, it looks for NEGs located in the zones in GroupKeys
			// from NEG CR.
			testName := fmt.Sprintf("%s, enableMultiSubnetPhase1=%v", tc.desc, enableMultiSubnetPhase1)

			t.Run(testName, func(t *testing.T) {
				fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
				fakeNEG := negtypes.NewFakeNetworkEndpointGroupCloud("test-subnetwork", "test-network")
				linker := newTestNEGLinker(fakeNEG, fakeGCE)
				linker.enableMultiSubnetClusterPhase1 = enableMultiSubnetPhase1

				if tc.populateSvcNeg {
					svcNeg := &v1beta1.ServiceNetworkEndpointGroup{
						TypeMeta: metav1.TypeMeta{
							Kind:       "ServiceNetworkEndpointGroup",
							APIVersion: "networking.gke.io/v1beta1",
						},
						ObjectMeta: metav1.ObjectMeta{
							Name:      svcPort.NEGName(),
							Namespace: namespace,
						},
						Status: v1beta1.ServiceNetworkEndpointGroupStatus{
							NetworkEndpointGroups: tc.testNegRef,
						},
					}
					if err := linker.svcNegLister.Add(svcNeg); err != nil {
						t.Fatalf("Failed to add svcneg: %v", err)
					}
				}

				for _, groupKey := range groupKeys {
					neg := &composite.NetworkEndpointGroup{
						Name:    svcPort.NEGName(),
						Version: befeatures.VersionFromServicePort(&svcPort),
					}
					err := fakeNEG.CreateNetworkEndpointGroup(neg, groupKey.Zone, klog.TODO())
					if err != nil {
						t.Fatalf("unexpected error creating NEG for svcPort %v: %v", svcPort, err)
					}
				}

				negLinks, err := linker.getNegSelfLinks(svcPort, groupKeys)
				if err != nil {
					t.Fatalf("Failed to link backend service to NEG for svcPort %v: %v", svcPort, err)
				}
				if len(negLinks) != tc.expectedNegCount {
					t.Errorf("Got %d neg links, expected %d", len(negLinks), tc.expectedNegCount)
				}
			})
		}
	}
}

// TestGetNegSelfLinksWithMultiSubnetCluster checks if getNegSelfLinks() returns
// the correct set of NEGs when EnableMultiSubnetClusterPhase1 is enabled.
// It should return NEGs from non-default subnets, which will have a different name
// from the SvcPort.NEGName().
func TestGetNegSelfLinksWithMultiSubnetCluster(t *testing.T) {
	t.Parallel()

	groupKeys := []GroupKey{{Zone: testZone1}, {Zone: testZone2}}
	namespace, svcName, port := "ns", "name", "port"
	svc := types.NamespacedName{Namespace: namespace, Name: svcName}
	svcPort := utils.ServicePort{
		ID:           utils.ServicePortID{Service: svc},
		Port:         80,
		Protocol:     annotations.ProtocolHTTP,
		TargetPort:   intstr.FromString(port),
		NEGEnabled:   true,
		BackendNamer: defaultNamer,
	}
	defaultSubnetNegName := svcPort.NEGName()
	// TODO(sawsa307): Update NEG name once naming schema for non-default subnet is finalized.
	nonDefaultSubnetNegName := "non-default-neg"

	testCases := []struct {
		desc             string
		populateSvcNeg   bool
		testNegRef       []v1beta1.NegObjectReference
		expectedNegCount int
	}{
		{
			desc:           "Get NEGs from SvcNeg, all NEGs are in active state, and NEGs are from default subnet",
			populateSvcNeg: true,
			testNegRef: []v1beta1.NegObjectReference{
				createNegRef(testZone1, defaultSubnetNegName, v1beta1.ActiveState),
				createNegRef(testZone2, defaultSubnetNegName, v1beta1.ActiveState),
			},
			expectedNegCount: 2,
		},
		{
			desc:           "Get NEGs from SvcNeg, all NEGs are in active state, and containing NEGs from non-default subnet",
			populateSvcNeg: true,
			testNegRef: []v1beta1.NegObjectReference{
				createNegRef(testZone1, defaultSubnetNegName, v1beta1.ActiveState),
				createNegRef(testZone1, nonDefaultSubnetNegName, v1beta1.ActiveState),
				createNegRef(testZone2, defaultSubnetNegName, v1beta1.ActiveState),
				createNegRef(testZone2, nonDefaultSubnetNegName, v1beta1.ActiveState),
			},
			expectedNegCount: 4,
		},
		{
			desc:           "Get NEGs from SvcNeg, containing one NEG from default subnet in inactive state",
			populateSvcNeg: true,
			testNegRef: []v1beta1.NegObjectReference{
				createNegRef(testZone1, defaultSubnetNegName, v1beta1.ActiveState),
				createNegRef(testZone2, defaultSubnetNegName, v1beta1.InactiveState),
			},
			expectedNegCount: 2,
		},
		{
			desc:           "Get NEGs from SvcNeg, containing NEGs from non-default subnet in inactive state",
			populateSvcNeg: true,
			testNegRef: []v1beta1.NegObjectReference{
				createNegRef(testZone1, defaultSubnetNegName, v1beta1.ActiveState),
				createNegRef(testZone1, nonDefaultSubnetNegName, v1beta1.ActiveState),
				createNegRef(testZone2, defaultSubnetNegName, v1beta1.InactiveState), // zone2 is inactive
				createNegRef(testZone2, nonDefaultSubnetNegName, v1beta1.InactiveState),
			},
			expectedNegCount: 4,
		},
		{
			desc:             "Get NEGs from GCE",
			populateSvcNeg:   false,
			expectedNegCount: 2,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
			fakeNEG := negtypes.NewFakeNetworkEndpointGroupCloud("test-subnetwork", "test-network")
			linker := newTestNEGLinker(fakeNEG, fakeGCE)
			linker.enableMultiSubnetClusterPhase1 = true

			if tc.populateSvcNeg {
				svcNeg := &v1beta1.ServiceNetworkEndpointGroup{
					TypeMeta: metav1.TypeMeta{
						Kind:       "ServiceNetworkEndpointGroup",
						APIVersion: "networking.gke.io/v1beta1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      svcPort.NEGName(),
						Namespace: namespace,
					},
					Status: v1beta1.ServiceNetworkEndpointGroupStatus{
						NetworkEndpointGroups: tc.testNegRef,
					},
				}
				if err := linker.svcNegLister.Add(svcNeg); err != nil {
					t.Fatalf("Failed to add svcneg: %v", err)
				}
			}

			for _, groupKey := range groupKeys {
				neg := &composite.NetworkEndpointGroup{
					Name:    svcPort.NEGName(),
					Version: befeatures.VersionFromServicePort(&svcPort),
				}
				err := fakeNEG.CreateNetworkEndpointGroup(neg, groupKey.Zone, klog.TODO())
				if err != nil {
					t.Fatalf("unexpected error creating NEG for svcPort %v: %v", svcPort, err)
				}
			}

			negLinks, err := linker.getNegSelfLinks(svcPort, groupKeys)
			if err != nil {
				t.Fatalf("Failed to link backend service to NEG for svcPort %v: %v", svcPort, err)
			}
			if len(negLinks) != tc.expectedNegCount {
				t.Errorf("Got %d neg links, expected %d", len(negLinks), tc.expectedNegCount)
			}
		})
	}
}

func TestMergeBackends(t *testing.T) {
	t.Parallel()

	negUrl11 := "https://www.googleapis.com/compute/v1/projects/test-project/zones/us-central1-c/networkEndpointGroups/k8s1-325ba033-kube-system-default-http-backend-80-4520b6d9"
	negUrl12 := "https://www.googleapis.com/compute/beta/projects/test-project/zones/us-central1-c/networkEndpointGroups/k8s1-325ba033-kube-system-default-http-backend-80-4520b6d9"
	negUrl2 := "https://www.googleapis.com/compute/v1/projects/test-project/zones/us-central1-c/networkEndpointGroups/neg2"
	negUrl3 := "https://www.googleapis.com/compute/v1/projects/test-project/zones/us-central1-c/networkEndpointGroups/neg3"
	negUrl4 := "https://www.googleapis.com/compute/v1/projects/test-project/zones/us-central1-c/networkEndpointGroups/neg4"

	for _, tc := range []struct {
		name        string
		old         []*composite.Backend
		new         []*composite.Backend
		expect      []*composite.Backend
		expectError bool
	}{
		{
			name:   "empty",
			expect: []*composite.Backend{},
		},
		{
			name:        "mal formed NEG url in old",
			old:         []*composite.Backend{{Group: "malformed"}},
			expectError: true,
		},
		{
			name:        "mal formed NEG url in new",
			old:         []*composite.Backend{{Group: negUrl11}},
			new:         []*composite.Backend{{Group: "malformed"}},
			expectError: true,
		},
		{
			name:   "same",
			old:    []*composite.Backend{{Group: negUrl12}},
			new:    []*composite.Backend{{Group: negUrl12}},
			expect: []*composite.Backend{{Group: negUrl12}},
		},
		{
			name:   "same (multiple)",
			old:    []*composite.Backend{{Group: negUrl2}, {Group: negUrl3}},
			new:    []*composite.Backend{{Group: negUrl2}, {Group: negUrl3}},
			expect: []*composite.Backend{{Group: negUrl2}, {Group: negUrl3}},
		},
		{
			name:   "new has more backend than old",
			old:    []*composite.Backend{{Group: negUrl2}},
			new:    []*composite.Backend{{Group: negUrl3}, {Group: negUrl2}},
			expect: []*composite.Backend{{Group: negUrl2}, {Group: negUrl3}},
		},
		{
			name:   "old has more backend than new",
			old:    []*composite.Backend{{Group: negUrl2}, {Group: negUrl3}},
			new:    []*composite.Backend{{Group: negUrl2}},
			expect: []*composite.Backend{{Group: negUrl2}, {Group: negUrl3}},
		},
		{
			name:   "diff between old and new",
			old:    []*composite.Backend{{Group: negUrl12}, {Group: negUrl2}, {Group: negUrl3}},
			new:    []*composite.Backend{{Group: negUrl2}, {Group: negUrl3}, {Group: negUrl4}},
			expect: []*composite.Backend{{Group: negUrl12}, {Group: negUrl2}, {Group: negUrl3}, {Group: negUrl4}},
		},
		{
			name:   "update rate",
			old:    []*composite.Backend{{Group: negUrl2, MaxRatePerEndpoint: 1}},
			new:    []*composite.Backend{{Group: negUrl2, MaxRatePerEndpoint: 3}},
			expect: []*composite.Backend{{Group: negUrl2, MaxRatePerEndpoint: 3}},
		},
		{
			name:   "beta url and v1 url ",
			old:    []*composite.Backend{{Group: negUrl11, MaxRatePerEndpoint: 1}},
			new:    []*composite.Backend{{Group: negUrl12, MaxRatePerEndpoint: 1}},
			expect: []*composite.Backend{{Group: negUrl12, MaxRatePerEndpoint: 1}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ret, err := mergeBackends(tc.old, tc.new)
			if tc.expectError && err == nil {
				t.Errorf("Expect err != nil, however got err == nil")
			} else if !tc.expectError && err != nil {
				t.Errorf("Expect err == nil, however got %v", err)
			}

			if !tc.expectError {
				diffBackend := diffBackends(tc.expect, ret, klog.TODO())
				if !diffBackend.isEqual() {
					t.Errorf("Expect tc.expect == ret, however got, tc.expect = %v, ret = %v", tc.expect, ret)
				}
			}
		})
	}
}

func TestDiffBackends(t *testing.T) {
	// No t.Parallel().
	oldFlag := flags.F.EnableTrafficScaling
	flags.F.EnableTrafficScaling = true
	defer func() { flags.F.EnableTrafficScaling = oldFlag }()

	for _, tc := range []struct {
		name string
		old  []*composite.Backend
		new  []*composite.Backend

		isEqual  bool
		toRemove sets.String
		toAdd    sets.String
		changed  sets.String
	}{
		{
			name:    "empty",
			isEqual: true,
		},
		{
			name:    "same",
			old:     []*composite.Backend{{Group: "a"}},
			new:     []*composite.Backend{{Group: "a"}},
			isEqual: true,
		},
		{
			name:    "same (multiple)",
			old:     []*composite.Backend{{Group: "a"}, {Group: "b"}},
			new:     []*composite.Backend{{Group: "b"}, {Group: "a"}},
			isEqual: true,
		},
		{
			name: "same but different api version",
			old: []*composite.Backend{
				{Group: "https://www.googleapis.com/compute/beta/projects/project-name/zones/us-central1-c/networkEndpointGroups/k8s1-neg-name-1"},
				{Group: "https://www.googleapis.com/compute/v1/projects/project-name/zones/us-central1-c/networkEndpointGroups/k8s1-neg-name-2"},
			},
			new: []*composite.Backend{
				{Group: "https://www.googleapis.com/compute/v1/projects/project-name/zones/us-central1-c/networkEndpointGroups/k8s1-neg-name-1"},
				{Group: "https://www.googleapis.com/compute/beta/projects/project-name/zones/us-central1-c/networkEndpointGroups/k8s1-neg-name-2"},
			},
			isEqual: true,
		},
		{
			name:  "add backend",
			old:   []*composite.Backend{{Group: "a"}},
			new:   []*composite.Backend{{Group: "b"}, {Group: "a"}},
			toAdd: sets.NewString("b"),
		},
		{
			name:     "remove backend",
			old:      []*composite.Backend{{Group: "a"}, {Group: "b"}},
			new:      []*composite.Backend{{Group: "b"}},
			toRemove: sets.NewString("a"),
		},
		{
			name:     "add and remove",
			old:      []*composite.Backend{{Group: "a"}, {Group: "b"}, {Group: "c"}},
			new:      []*composite.Backend{{Group: "b"}, {Group: "a"}, {Group: "d"}},
			toAdd:    sets.NewString("d"),
			toRemove: sets.NewString("c"),
		},
		{
			name:    "update rate",
			old:     []*composite.Backend{{Group: "a", MaxRatePerEndpoint: 1}},
			new:     []*composite.Backend{{Group: "a", MaxRatePerEndpoint: 3}},
			changed: sets.NewString("a"),
		},
		{
			name:    "update capacity scaler",
			old:     []*composite.Backend{{Group: "a", CapacityScaler: 1.0}},
			new:     []*composite.Backend{{Group: "a", CapacityScaler: 0.5}},
			changed: sets.NewString("a"),
		},
		{
			name:    "no change",
			old:     []*composite.Backend{{Group: "a", CapacityScaler: 1.0}},
			new:     []*composite.Backend{{Group: "a", CapacityScaler: 1.0}},
			isEqual: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			diff := diffBackends(tc.old, tc.new, klog.TODO())
			if got := diff.isEqual(); got != tc.isEqual {
				t.Errorf("diff := diffBackends(%s, %s); diff.isEqual() = %t, want %t", pretty.Sprint(tc.old), pretty.Sprint(tc.new), got, tc.isEqual)
			}
			if got := diff.toRemove(); !got.Equal(tc.toRemove) {
				t.Errorf("diff := diffBackends(%s, %s); diff.toRemove() = %s, want %s", pretty.Sprint(tc.old), pretty.Sprint(tc.new), got, tc.toRemove)
			}
			if got := diff.toAdd(); !got.Equal(tc.toAdd) {
				t.Errorf("diff := diffBackends(%s, %s); diff.toAdd() = %s, want %s", pretty.Sprint(tc.old), pretty.Sprint(tc.new), got, tc.toAdd)
			}
			if got := diff.changed; !got.Equal(tc.changed) {
				t.Errorf("diff := diffBackends(%s, %s); diff.changed = %s, want %s", pretty.Sprint(tc.old), pretty.Sprint(tc.new), got, tc.changed)
			}
		})
	}
}

func TestBackendsForNEG(t *testing.T) {
	// No t.Parallel().
	oldFlag := flags.F.EnableTrafficScaling
	flags.F.EnableTrafficScaling = true
	defer func() { flags.F.EnableTrafficScaling = oldFlag }()

	f64 := func(x float64) *float64 { return &x }

	for _, tc := range []struct {
		name string
		negs []*composite.NetworkEndpointGroup
		sp   *utils.ServicePort
		want []*composite.Backend
	}{
		{
			name: "vm ip endpoint uses connections balancing mode",
			negs: []*composite.NetworkEndpointGroup{
				{
					NetworkEndpointType: string(negtypes.VmIpEndpointType),
					SelfLink:            "/neg1",
				},
			},
			sp: &utils.ServicePort{
				VMIPNEGEnabled: true,
			},
			want: []*composite.Backend{
				{
					BalancingMode: "CONNECTION",
					Group:         "/neg1",
				},
			},
		},
		{
			name: "vm ip endpoint (multiple)",
			negs: []*composite.NetworkEndpointGroup{
				{
					NetworkEndpointType: string(negtypes.VmIpEndpointType),
					SelfLink:            "/neg1",
				},
				{
					NetworkEndpointType: string(negtypes.VmIpEndpointType),
					SelfLink:            "/neg2",
				},
			},
			sp: &utils.ServicePort{
				VMIPNEGEnabled: true,
			},
			want: []*composite.Backend{
				{
					BalancingMode: "CONNECTION",
					Group:         "/neg1",
				},
				{
					BalancingMode: "CONNECTION",
					Group:         "/neg2",
				},
			},
		},
		{
			name: "neg endpoint defaults",
			negs: []*composite.NetworkEndpointGroup{
				{
					NetworkEndpointType: string(negtypes.VmIpPortEndpointType),
					SelfLink:            "/neg1",
				},
			},
			sp: &utils.ServicePort{},
			want: []*composite.Backend{
				{
					BalancingMode:      "RATE",
					MaxRatePerEndpoint: maxRPS,
					CapacityScaler:     1.0,
					Group:              "/neg1",
				},
			},
		},
		{
			name: "neg endpoint (traffic policy rate)",
			negs: []*composite.NetworkEndpointGroup{
				{
					NetworkEndpointType: string(negtypes.VmIpPortEndpointType),
					SelfLink:            "/neg1",
				},
			},
			sp: &utils.ServicePort{
				MaxRatePerEndpoint: f64(1234),
			},
			want: []*composite.Backend{
				{
					BalancingMode:      "RATE",
					MaxRatePerEndpoint: 1234,
					CapacityScaler:     1.0,
					Group:              "/neg1",
				},
			},
		},
		{
			name: "neg endpoint (traffic policy capacity scaler)",
			negs: []*composite.NetworkEndpointGroup{
				{
					NetworkEndpointType: string(negtypes.VmIpPortEndpointType),
					SelfLink:            "/neg1",
				},
			},
			sp: &utils.ServicePort{
				CapacityScaler: f64(0.5),
			},
			want: []*composite.Backend{
				{
					BalancingMode:      "RATE",
					MaxRatePerEndpoint: maxRPS,
					CapacityScaler:     0.5,
					Group:              "/neg1",
				},
			},
		},
		{
			name: "neg endpoint (traffic policy)",
			negs: []*composite.NetworkEndpointGroup{
				{
					NetworkEndpointType: string(negtypes.VmIpPortEndpointType),
					SelfLink:            "/neg1",
				},
			},
			sp: &utils.ServicePort{
				MaxRatePerEndpoint: f64(1234),
				CapacityScaler:     f64(0.5),
			},
			want: []*composite.Backend{
				{
					BalancingMode:      "RATE",
					MaxRatePerEndpoint: 1234,
					CapacityScaler:     0.5,
					Group:              "/neg1",
				},
			},
		},
		{
			name: "neg endpoint (multiple, traffic policy)",
			negs: []*composite.NetworkEndpointGroup{
				{
					NetworkEndpointType: string(negtypes.VmIpPortEndpointType),
					SelfLink:            "/neg1",
				},
				{
					NetworkEndpointType: string(negtypes.VmIpPortEndpointType),
					SelfLink:            "/neg2",
				},
			},
			sp: &utils.ServicePort{
				MaxRatePerEndpoint: f64(1234),
				CapacityScaler:     f64(0.5),
			},
			want: []*composite.Backend{
				{
					BalancingMode:      "RATE",
					MaxRatePerEndpoint: 1234,
					CapacityScaler:     0.5,
					Group:              "/neg1",
				},
				{
					BalancingMode:      "RATE",
					MaxRatePerEndpoint: 1234,
					CapacityScaler:     0.5,
					Group:              "/neg2",
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			negUrls := []string{}
			for _, neg := range tc.negs {
				negUrls = append(negUrls, neg.SelfLink)
			}
			got := backendsForNEGs(negUrls, tc.sp)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("backendForNEGs(_), diff(-tc.want +got) = %s", diff)
			}
		})
	}
}

func createNegRef(zone, negName string, state v1beta1.NegState) v1beta1.NegObjectReference {
	return v1beta1.NegObjectReference{
		SelfLink: fmt.Sprintf("https://www.googleapis.com/compute/alpha/projects/mock-project/zones/%s/networkEndpointGroups/%s", zone, negName),
		State:    state,
	}
}
