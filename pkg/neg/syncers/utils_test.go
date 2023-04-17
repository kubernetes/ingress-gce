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

package syncers

import (
	"errors"
	"fmt"
	"net"
	"reflect"
	"strconv"
	"testing"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	discovery "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/cache"
	"k8s.io/cloud-provider-gcp/providers/gce"
	negv1beta1 "k8s.io/ingress-gce/pkg/apis/svcneg/v1beta1"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/neg/syncers/labels"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/ingress-gce/pkg/utils"
)

func TestEncodeDecodeEndpoint(t *testing.T) {
	ip := "10.0.0.10"
	instance := "somehost"
	port := "8080"

	retIp, retInstance, retPort := decodeEndpoint(encodeEndpoint(ip, instance, port))

	if ip != retIp || instance != retInstance || retPort != port {
		t.Fatalf("Encode and decode endpoint failed. Expect %q, %q, %q but got %q, %q, %q.", ip, instance, port, retIp, retInstance, retPort)
	}
}

func TestCalculateDifference(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		targetSet  map[string]sets.String
		currentSet map[string]sets.String
		addSet     map[string]sets.String
		removeSet  map[string]sets.String
	}{
		// unchanged
		{
			targetSet: map[string]sets.String{
				negtypes.TestZone1: sets.NewString("a", "b", "c"),
			},
			currentSet: map[string]sets.String{
				negtypes.TestZone1: sets.NewString("a", "b", "c"),
			},
			addSet:    map[string]sets.String{},
			removeSet: map[string]sets.String{},
		},
		// unchanged
		{
			targetSet:  map[string]sets.String{},
			currentSet: map[string]sets.String{},
			addSet:     map[string]sets.String{},
			removeSet:  map[string]sets.String{},
		},
		// add in one zone
		{
			targetSet: map[string]sets.String{
				negtypes.TestZone1: sets.NewString("a", "b", "c"),
			},
			currentSet: map[string]sets.String{},
			addSet: map[string]sets.String{
				negtypes.TestZone1: sets.NewString("a", "b", "c"),
			},
			removeSet: map[string]sets.String{},
		},
		// add in 2 zones
		{
			targetSet: map[string]sets.String{
				negtypes.TestZone1: sets.NewString("a", "b", "c"),
				negtypes.TestZone2: sets.NewString("e", "f", "g"),
			},
			currentSet: map[string]sets.String{},
			addSet: map[string]sets.String{
				negtypes.TestZone1: sets.NewString("a", "b", "c"),
				negtypes.TestZone2: sets.NewString("e", "f", "g"),
			},
			removeSet: map[string]sets.String{},
		},
		// remove in one zone
		{
			targetSet: map[string]sets.String{},
			currentSet: map[string]sets.String{
				negtypes.TestZone1: sets.NewString("a", "b", "c"),
			},
			addSet: map[string]sets.String{},
			removeSet: map[string]sets.String{
				negtypes.TestZone1: sets.NewString("a", "b", "c"),
			},
		},
		// remove in 2 zones
		{
			targetSet: map[string]sets.String{},
			currentSet: map[string]sets.String{
				negtypes.TestZone1: sets.NewString("a", "b", "c"),
				negtypes.TestZone2: sets.NewString("e", "f", "g"),
			},
			addSet: map[string]sets.String{},
			removeSet: map[string]sets.String{
				negtypes.TestZone1: sets.NewString("a", "b", "c"),
				negtypes.TestZone2: sets.NewString("e", "f", "g"),
			},
		},
		// add and delete in one zone
		{
			targetSet: map[string]sets.String{
				negtypes.TestZone1: sets.NewString("a", "b", "c"),
			},
			currentSet: map[string]sets.String{
				negtypes.TestZone1: sets.NewString("b", "c", "d"),
			},
			addSet: map[string]sets.String{
				negtypes.TestZone1: sets.NewString("a"),
			},
			removeSet: map[string]sets.String{
				negtypes.TestZone1: sets.NewString("d"),
			},
		},
		// add and delete in 2 zones
		{
			targetSet: map[string]sets.String{
				negtypes.TestZone1: sets.NewString("a", "b", "c"),
				negtypes.TestZone2: sets.NewString("a", "b", "c"),
			},
			currentSet: map[string]sets.String{
				negtypes.TestZone1: sets.NewString("b", "c", "d"),
				negtypes.TestZone2: sets.NewString("b", "c", "d"),
			},
			addSet: map[string]sets.String{
				negtypes.TestZone1: sets.NewString("a"),
				negtypes.TestZone2: sets.NewString("a"),
			},
			removeSet: map[string]sets.String{
				negtypes.TestZone1: sets.NewString("d"),
				negtypes.TestZone2: sets.NewString("d"),
			},
		},
	}

	for _, tc := range testCases {
		addSet, removeSet := calculateDifference(tc.targetSet, tc.currentSet)

		if !reflect.DeepEqual(addSet, tc.addSet) {
			t.Errorf("Failed to calculate difference for add, expecting %v, but got %v", tc.addSet, addSet)
		}

		if !reflect.DeepEqual(removeSet, tc.removeSet) {
			t.Errorf("Failed to calculate difference for remove, expecting %v, but got %v", tc.removeSet, removeSet)
		}
	}
}

func TestNetworkEndpointCalculateDifference(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		targetSet  map[string]negtypes.NetworkEndpointSet
		currentSet map[string]negtypes.NetworkEndpointSet
		addSet     map[string]negtypes.NetworkEndpointSet
		removeSet  map[string]negtypes.NetworkEndpointSet
	}{
		// unchanged
		{
			targetSet: map[string]negtypes.NetworkEndpointSet{
				negtypes.TestZone1: negtypes.NewNetworkEndpointSet(genNetworkEndpoint("a"), genNetworkEndpoint("b"), genNetworkEndpoint("c")),
			},
			currentSet: map[string]negtypes.NetworkEndpointSet{
				negtypes.TestZone1: negtypes.NewNetworkEndpointSet(genNetworkEndpoint("a"), genNetworkEndpoint("b"), genNetworkEndpoint("c")),
			},
			addSet:    map[string]negtypes.NetworkEndpointSet{},
			removeSet: map[string]negtypes.NetworkEndpointSet{},
		},
		// unchanged
		{
			targetSet:  map[string]negtypes.NetworkEndpointSet{},
			currentSet: map[string]negtypes.NetworkEndpointSet{},
			addSet:     map[string]negtypes.NetworkEndpointSet{},
			removeSet:  map[string]negtypes.NetworkEndpointSet{},
		},
		// add in one zone
		{
			targetSet: map[string]negtypes.NetworkEndpointSet{
				negtypes.TestZone1: negtypes.NewNetworkEndpointSet(genNetworkEndpoint("a"), genNetworkEndpoint("b"), genNetworkEndpoint("c")),
			},
			currentSet: map[string]negtypes.NetworkEndpointSet{},
			addSet: map[string]negtypes.NetworkEndpointSet{
				negtypes.TestZone1: negtypes.NewNetworkEndpointSet(genNetworkEndpoint("a"), genNetworkEndpoint("b"), genNetworkEndpoint("c")),
			},
			removeSet: map[string]negtypes.NetworkEndpointSet{},
		},
		// add in 2 zones
		{
			targetSet: map[string]negtypes.NetworkEndpointSet{
				negtypes.TestZone1: negtypes.NewNetworkEndpointSet(genNetworkEndpoint("a"), genNetworkEndpoint("b"), genNetworkEndpoint("c")),
				negtypes.TestZone2: negtypes.NewNetworkEndpointSet(genNetworkEndpoint("e"), genNetworkEndpoint("f"), genNetworkEndpoint("g")),
			},
			currentSet: map[string]negtypes.NetworkEndpointSet{},
			addSet: map[string]negtypes.NetworkEndpointSet{
				negtypes.TestZone1: negtypes.NewNetworkEndpointSet(genNetworkEndpoint("a"), genNetworkEndpoint("b"), genNetworkEndpoint("c")),
				negtypes.TestZone2: negtypes.NewNetworkEndpointSet(genNetworkEndpoint("e"), genNetworkEndpoint("f"), genNetworkEndpoint("g")),
			},
			removeSet: map[string]negtypes.NetworkEndpointSet{},
		},
		// remove in one zone
		{
			targetSet: map[string]negtypes.NetworkEndpointSet{},
			currentSet: map[string]negtypes.NetworkEndpointSet{
				negtypes.TestZone1: negtypes.NewNetworkEndpointSet(genNetworkEndpoint("a"), genNetworkEndpoint("b"), genNetworkEndpoint("c")),
			},
			addSet: map[string]negtypes.NetworkEndpointSet{},
			removeSet: map[string]negtypes.NetworkEndpointSet{
				negtypes.TestZone1: negtypes.NewNetworkEndpointSet(genNetworkEndpoint("a"), genNetworkEndpoint("b"), genNetworkEndpoint("c")),
			},
		},
		// remove in 2 zones
		{
			targetSet: map[string]negtypes.NetworkEndpointSet{},
			currentSet: map[string]negtypes.NetworkEndpointSet{
				negtypes.TestZone1: negtypes.NewNetworkEndpointSet(genNetworkEndpoint("a"), genNetworkEndpoint("b"), genNetworkEndpoint("c")),
				negtypes.TestZone2: negtypes.NewNetworkEndpointSet(genNetworkEndpoint("e"), genNetworkEndpoint("f"), genNetworkEndpoint("g")),
			},
			addSet: map[string]negtypes.NetworkEndpointSet{},
			removeSet: map[string]negtypes.NetworkEndpointSet{
				negtypes.TestZone1: negtypes.NewNetworkEndpointSet(genNetworkEndpoint("a"), genNetworkEndpoint("b"), genNetworkEndpoint("c")),
				negtypes.TestZone2: negtypes.NewNetworkEndpointSet(genNetworkEndpoint("e"), genNetworkEndpoint("f"), genNetworkEndpoint("g")),
			},
		},
		// add and delete in one zone
		{
			targetSet: map[string]negtypes.NetworkEndpointSet{
				negtypes.TestZone1: negtypes.NewNetworkEndpointSet(genNetworkEndpoint("a"), genNetworkEndpoint("b"), genNetworkEndpoint("c")),
			},
			currentSet: map[string]negtypes.NetworkEndpointSet{
				negtypes.TestZone1: negtypes.NewNetworkEndpointSet(genNetworkEndpoint("b"), genNetworkEndpoint("c"), genNetworkEndpoint("d")),
			},
			addSet: map[string]negtypes.NetworkEndpointSet{
				negtypes.TestZone1: negtypes.NewNetworkEndpointSet(genNetworkEndpoint("a")),
			},
			removeSet: map[string]negtypes.NetworkEndpointSet{
				negtypes.TestZone1: negtypes.NewNetworkEndpointSet(genNetworkEndpoint("d")),
			},
		},
		// add and delete in 2 zones
		{
			targetSet: map[string]negtypes.NetworkEndpointSet{
				negtypes.TestZone1: negtypes.NewNetworkEndpointSet(genNetworkEndpoint("a"), genNetworkEndpoint("b"), genNetworkEndpoint("c")),
				negtypes.TestZone2: negtypes.NewNetworkEndpointSet(genNetworkEndpoint("a"), genNetworkEndpoint("b"), genNetworkEndpoint("c")),
			},
			currentSet: map[string]negtypes.NetworkEndpointSet{
				negtypes.TestZone1: negtypes.NewNetworkEndpointSet(genNetworkEndpoint("b"), genNetworkEndpoint("c"), genNetworkEndpoint("d")),
				negtypes.TestZone2: negtypes.NewNetworkEndpointSet(genNetworkEndpoint("b"), genNetworkEndpoint("c"), genNetworkEndpoint("d")),
			},
			addSet: map[string]negtypes.NetworkEndpointSet{
				negtypes.TestZone1: negtypes.NewNetworkEndpointSet(genNetworkEndpoint("a")),
				negtypes.TestZone2: negtypes.NewNetworkEndpointSet(genNetworkEndpoint("a")),
			},
			removeSet: map[string]negtypes.NetworkEndpointSet{
				negtypes.TestZone1: negtypes.NewNetworkEndpointSet(genNetworkEndpoint("d")),
				negtypes.TestZone2: negtypes.NewNetworkEndpointSet(genNetworkEndpoint("d")),
			},
		},
	}

	for _, tc := range testCases {
		addSet, removeSet := calculateNetworkEndpointDifference(tc.targetSet, tc.currentSet)

		if !reflect.DeepEqual(addSet, tc.addSet) {
			t.Errorf("Failed to calculate difference for add, expecting %v, but got %v", tc.addSet, addSet)
		}

		if !reflect.DeepEqual(removeSet, tc.removeSet) {
			t.Errorf("Failed to calculate difference for remove, expecting %v, but got %v", tc.removeSet, removeSet)
		}
	}
}

func TestFindAndFilterMigrationEndpoints(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name                              string
		addEndpoints                      map[string]negtypes.NetworkEndpointSet
		removeEndpoints                   map[string]negtypes.NetworkEndpointSet
		wantMigrationEndpointsInAddSet    map[string]negtypes.NetworkEndpointSet
		wantMigrationEndpointsInRemoveSet map[string]negtypes.NetworkEndpointSet
	}{
		{
			name: "detect multiple migrating endpoints",
			addEndpoints: map[string]negtypes.NetworkEndpointSet{
				"zone1": negtypes.NewNetworkEndpointSet([]negtypes.NetworkEndpoint{
					{IP: "a", IPv6: "A"}, // migrating
					{IP: "b"},
					{IP: "c", IPv6: "C"},
					{IP: "d"}, // migrating
				}...),
				"zone2": negtypes.NewNetworkEndpointSet([]negtypes.NetworkEndpoint{
					{IP: "e", IPv6: "E"}, // migrating
				}...),
			},
			removeEndpoints: map[string]negtypes.NetworkEndpointSet{
				"zone1": negtypes.NewNetworkEndpointSet([]negtypes.NetworkEndpoint{
					{IP: "a"}, // migrating
					{IP: "f", IPv6: "F"},
					{IP: "d", IPv6: "D"}, // migrating
				}...),
				"zone2": negtypes.NewNetworkEndpointSet([]negtypes.NetworkEndpoint{
					{IPv6: "E"}, // migrating
				}...),
			},
			wantMigrationEndpointsInAddSet: map[string]negtypes.NetworkEndpointSet{
				"zone1": negtypes.NewNetworkEndpointSet([]negtypes.NetworkEndpoint{
					{IP: "a", IPv6: "A"},
					{IP: "d"},
				}...),
				"zone2": negtypes.NewNetworkEndpointSet([]negtypes.NetworkEndpoint{
					{IP: "e", IPv6: "E"},
				}...),
			},
			wantMigrationEndpointsInRemoveSet: map[string]negtypes.NetworkEndpointSet{
				"zone1": negtypes.NewNetworkEndpointSet([]negtypes.NetworkEndpoint{
					{IP: "a"},
					{IP: "d", IPv6: "D"},
				}...),
				"zone2": negtypes.NewNetworkEndpointSet([]negtypes.NetworkEndpoint{
					{IPv6: "E"},
				}...),
			},
		},
		{
			name: "partial IP change without stack change is not considered migrating",
			addEndpoints: map[string]negtypes.NetworkEndpointSet{
				"zone1": negtypes.NewNetworkEndpointSet([]negtypes.NetworkEndpoint{
					{IP: "a", IPv6: "A"},
				}...),
			},
			removeEndpoints: map[string]negtypes.NetworkEndpointSet{
				"zone1": negtypes.NewNetworkEndpointSet([]negtypes.NetworkEndpoint{
					{IP: "a", Port: "B"},
				}...),
			},
			wantMigrationEndpointsInAddSet:    map[string]negtypes.NetworkEndpointSet{},
			wantMigrationEndpointsInRemoveSet: map[string]negtypes.NetworkEndpointSet{},
		},
		{
			name: "difference in port or node is not considered migrating",
			addEndpoints: map[string]negtypes.NetworkEndpointSet{
				"zone1": negtypes.NewNetworkEndpointSet([]negtypes.NetworkEndpoint{
					{IP: "a", IPv6: "A", Port: "80"},
					{IP: "b", Node: "node2"},
				}...),
			},
			removeEndpoints: map[string]negtypes.NetworkEndpointSet{
				"zone1": negtypes.NewNetworkEndpointSet([]negtypes.NetworkEndpoint{
					{IP: "a", Port: "81"},
					{IP: "b", IPv6: "B", Node: "node1"},
				}...),
			},
			wantMigrationEndpointsInAddSet:    map[string]negtypes.NetworkEndpointSet{},
			wantMigrationEndpointsInRemoveSet: map[string]negtypes.NetworkEndpointSet{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gotMigrationEndpointsInAddSet, gotMigrationEndpointsInRemoveSet := findAndFilterMigrationEndpoints(tc.addEndpoints, tc.removeEndpoints)

			if diff := cmp.Diff(tc.wantMigrationEndpointsInAddSet, gotMigrationEndpointsInAddSet); diff != "" {
				t.Errorf("findAndFilterMigrationEndpoints(tc.addEndpoints, tc.removeEndpoints) returned unexpected diff for migrationEndpointsInAddSet (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.wantMigrationEndpointsInRemoveSet, gotMigrationEndpointsInRemoveSet); diff != "" {
				t.Errorf("findAndFilterMigrationEndpoints(tc.addEndpoints, tc.removeEndpoints) returned unexpected diff for migrationEndpointsInRemoveSet (-want +got):\n%s", diff)
			}
		})
	}
}

func TestMoveEndpoint(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		endpoint    negtypes.NetworkEndpoint
		inputSource map[string]negtypes.NetworkEndpointSet
		inputDest   map[string]negtypes.NetworkEndpointSet
		wantSource  map[string]negtypes.NetworkEndpointSet
		wantDest    map[string]negtypes.NetworkEndpointSet
		zone        string
		wantSuccess bool
	}{
		{
			name:     "completely valid input, shoud successfully move",
			endpoint: negtypes.NetworkEndpoint{IP: "a", IPv6: "A"},
			inputSource: map[string]negtypes.NetworkEndpointSet{
				"zone1": negtypes.NewNetworkEndpointSet([]negtypes.NetworkEndpoint{
					{IP: "a", IPv6: "A"},
					{IP: "b", IPv6: "B"},
				}...),
			},
			inputDest: map[string]negtypes.NetworkEndpointSet{
				"zone1": negtypes.NewNetworkEndpointSet([]negtypes.NetworkEndpoint{
					{IP: "c", IPv6: "C"},
				}...),
			},
			wantSource: map[string]negtypes.NetworkEndpointSet{
				"zone1": negtypes.NewNetworkEndpointSet([]negtypes.NetworkEndpoint{
					{IP: "b", IPv6: "B"},
				}...),
			},
			wantDest: map[string]negtypes.NetworkEndpointSet{
				"zone1": negtypes.NewNetworkEndpointSet([]negtypes.NetworkEndpoint{
					{IP: "a", IPv6: "A"},
					{IP: "c", IPv6: "C"},
				}...),
			},
			zone:        "zone1",
			wantSuccess: true,
		},
		{
			name:     "zone does not exist in source",
			endpoint: negtypes.NetworkEndpoint{IP: "a", IPv6: "A"},
			inputSource: map[string]negtypes.NetworkEndpointSet{
				"zone1": negtypes.NewNetworkEndpointSet([]negtypes.NetworkEndpoint{
					{IP: "a", IPv6: "A"},
				}...),
			},
			inputDest: map[string]negtypes.NetworkEndpointSet{
				"zone3": negtypes.NewNetworkEndpointSet([]negtypes.NetworkEndpoint{
					{IP: "c", IPv6: "C"},
				}...),
			},
			wantSource: map[string]negtypes.NetworkEndpointSet{
				"zone1": negtypes.NewNetworkEndpointSet([]negtypes.NetworkEndpoint{
					{IP: "a", IPv6: "A"},
				}...),
			},
			wantDest: map[string]negtypes.NetworkEndpointSet{
				"zone3": negtypes.NewNetworkEndpointSet([]negtypes.NetworkEndpoint{
					{IP: "c", IPv6: "C"},
				}...),
			},
			zone: "zone3",
		},
		{
			name:     "zone does not exist in destination, shoud successfully move",
			endpoint: negtypes.NetworkEndpoint{IP: "a", IPv6: "A"},
			inputSource: map[string]negtypes.NetworkEndpointSet{
				"zone1": negtypes.NewNetworkEndpointSet([]negtypes.NetworkEndpoint{
					{IP: "a", IPv6: "A"},
					{IP: "b", IPv6: "B"},
				}...),
			},
			inputDest: map[string]negtypes.NetworkEndpointSet{
				"zone2": negtypes.NewNetworkEndpointSet([]negtypes.NetworkEndpoint{
					{IP: "c", IPv6: "C"},
				}...),
			},
			wantSource: map[string]negtypes.NetworkEndpointSet{
				"zone1": negtypes.NewNetworkEndpointSet([]negtypes.NetworkEndpoint{
					{IP: "b", IPv6: "B"},
				}...),
			},
			wantDest: map[string]negtypes.NetworkEndpointSet{
				"zone1": negtypes.NewNetworkEndpointSet([]negtypes.NetworkEndpoint{
					{IP: "a", IPv6: "A"},
				}...),
				"zone2": negtypes.NewNetworkEndpointSet([]negtypes.NetworkEndpoint{
					{IP: "c", IPv6: "C"},
				}...),
			},
			zone:        "zone1",
			wantSuccess: true,
		},
		{
			name:     "source is nil",
			endpoint: negtypes.NetworkEndpoint{IP: "a", IPv6: "A"},
			inputDest: map[string]negtypes.NetworkEndpointSet{
				"zone3": negtypes.NewNetworkEndpointSet([]negtypes.NetworkEndpoint{
					{IP: "c", IPv6: "C"},
				}...),
			},
			wantDest: map[string]negtypes.NetworkEndpointSet{
				"zone3": negtypes.NewNetworkEndpointSet([]negtypes.NetworkEndpoint{
					{IP: "c", IPv6: "C"},
				}...),
			},
			zone: "zone3",
		},
		{
			name:     "destination is nil",
			endpoint: negtypes.NetworkEndpoint{IP: "a", IPv6: "A"},
			inputSource: map[string]negtypes.NetworkEndpointSet{
				"zone3": negtypes.NewNetworkEndpointSet([]negtypes.NetworkEndpoint{
					{IP: "c", IPv6: "C"},
				}...),
			},
			wantSource: map[string]negtypes.NetworkEndpointSet{
				"zone3": negtypes.NewNetworkEndpointSet([]negtypes.NetworkEndpoint{
					{IP: "c", IPv6: "C"},
				}...),
			},
			zone: "zone3",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gotSuccess := moveEndpoint(tc.endpoint, tc.inputSource, tc.inputDest, tc.zone)

			if gotSuccess != tc.wantSuccess {
				t.Errorf("moveEndpoint(%v, ...) = %v, want = %v", tc.endpoint, gotSuccess, tc.wantSuccess)
			}
			if diff := cmp.Diff(tc.wantSource, tc.inputSource); diff != "" {
				t.Errorf("moveEndpoint(%v, ...) returned unexpected diff for source (-want +got):\n%s", tc.endpoint, diff)
			}
			if diff := cmp.Diff(tc.wantDest, tc.inputDest); diff != "" {
				t.Errorf("moveEndpoint(%v, ...) returned unexpected diff for destination (-want +got):\n%s", tc.endpoint, diff)
			}
		})
	}
}

func TestEnsureNetworkEndpointGroup(t *testing.T) {
	var (
		testZone             = "test-zone"
		testNamedPort        = "named-port"
		testServiceName      = "test-svc"
		testServiceNameSpace = "test-ns"
		testNetwork          = cloud.ResourcePath("network", &meta.Key{Zone: testZone, Name: "test-network"})
		testSubnetwork       = cloud.ResourcePath("subnetwork", &meta.Key{Zone: testZone, Name: "test-subnetwork"})
		testKubesystemUID    = "kube-system-uid"
		testPort             = "80"
	)

	testCases := []struct {
		description         string
		negName             string
		enableNonGCPMode    bool
		networkEndpointType negtypes.NetworkEndpointType
		expectedSubnetwork  string
		apiVersion          meta.Version
	}{
		{
			description:         "Create NEG of type GCE_VM_IP_PORT",
			negName:             "gcp-neg",
			enableNonGCPMode:    false,
			networkEndpointType: negtypes.VmIpPortEndpointType,
			expectedSubnetwork:  testSubnetwork,
			apiVersion:          meta.VersionGA,
		},
		{
			description:         "Create NEG of type NON_GCP_PRIVATE_IP_PORT",
			negName:             "non-gcp-neg",
			enableNonGCPMode:    true,
			networkEndpointType: negtypes.NonGCPPrivateEndpointType,
			expectedSubnetwork:  "",
			apiVersion:          meta.VersionGA,
		},
		{
			description:         "Create NEG of type GCE_VM_IP",
			negName:             "gcp-ip-neg",
			enableNonGCPMode:    false,
			networkEndpointType: negtypes.VmIpEndpointType,
			expectedSubnetwork:  testSubnetwork,
			apiVersion:          meta.VersionAlpha,
		},
		{
			description:         "Create NEG of type GCE_VM_IP_PORT with Neg CRD",
			negName:             "gcp-neg",
			enableNonGCPMode:    false,
			networkEndpointType: negtypes.VmIpPortEndpointType,
			expectedSubnetwork:  testSubnetwork,
			apiVersion:          meta.VersionGA,
		},
		{
			description:         "Create NEG of type NON_GCP_PRIVATE_IP_PORT with Neg CRD",
			negName:             "non-gcp-neg",
			enableNonGCPMode:    true,
			networkEndpointType: negtypes.NonGCPPrivateEndpointType,
			expectedSubnetwork:  "",
			apiVersion:          meta.VersionGA,
		},
		{
			description:         "Create NEG of type GCE_VM_IP with Neg CRD",
			negName:             "gcp-ip-neg",
			enableNonGCPMode:    false,
			networkEndpointType: negtypes.VmIpEndpointType,
			expectedSubnetwork:  testSubnetwork,
			apiVersion:          meta.VersionAlpha,
		},
	}
	for _, tc := range testCases {
		fakeCloud := negtypes.NewFakeNetworkEndpointGroupCloud(testSubnetwork, testNetwork)
		_, err := ensureNetworkEndpointGroup(
			testServiceNameSpace,
			testServiceName,
			tc.negName,
			testZone,
			testNamedPort,
			testKubesystemUID,
			testPort,
			tc.networkEndpointType,
			fakeCloud,
			nil,
			nil,
			tc.apiVersion,
			false,
		)
		if err != nil {
			t.Errorf("unexpected error: %s", err)
		}

		neg, err := fakeCloud.GetNetworkEndpointGroup(tc.negName, testZone, tc.apiVersion)
		if err != nil {
			t.Errorf("Failed to retrieve NEG %q: %v", tc.negName, err)
		}

		if neg.NetworkEndpointType != string(tc.networkEndpointType) {
			t.Errorf("Unexpected NetworkEndpointType, expecting %q but got %q", tc.networkEndpointType, neg.NetworkEndpointType)
		}

		if neg.Subnetwork != tc.expectedSubnetwork {
			t.Errorf("Unexpected Subnetwork, expecting %q but got %q", tc.expectedSubnetwork, neg.Subnetwork)
		}

		expectedNegDesc := utils.NegDescription{
			ClusterUID:  testKubesystemUID,
			Namespace:   testServiceNamespace,
			ServiceName: testServiceName,
			Port:        testPort,
		}

		actualNegDesc, err := utils.NegDescriptionFromString(neg.Description)
		if err != nil {
			t.Errorf("Invalid neg description: %s", err)
		}

		if !reflect.DeepEqual(*actualNegDesc, expectedNegDesc) {
			t.Errorf("Unexpected neg description: %s, expected %s", neg.Description, expectedNegDesc.String())
		}

		// Call ensureNetworkEndpointGroup with the same NEG.
		_, err = ensureNetworkEndpointGroup(
			testServiceNameSpace,
			testServiceName,
			tc.negName,
			testZone,
			testNamedPort,
			testKubesystemUID,
			testPort,
			tc.networkEndpointType,
			fakeCloud,
			nil,
			nil,
			tc.apiVersion,
			false,
		)

		if err != nil {
			t.Errorf("Unexpected error when called with duplicated NEG: %v", err)
		}
	}
}

func TestToZoneNetworkEndpointMap(t *testing.T) {
	t.Parallel()
	zoneGetter := negtypes.NewFakeZoneGetter()
	podLister := negtypes.NewTestContext().PodInformer.GetIndexer()
	addPodsToLister(podLister)
	testCases := []struct {
		desc                       string
		portName                   string
		wantZoneNetworkEndpointMap map[string]negtypes.NetworkEndpointSet
		wantNetworkEndpointPodMap  negtypes.EndpointPodMap
		networkEndpointType        negtypes.NetworkEndpointType
		enableDualStackNEG         bool
	}{
		{
			desc:                       "target port does not exist",
			portName:                   "non-exists",
			wantZoneNetworkEndpointMap: map[string]negtypes.NetworkEndpointSet{},
			wantNetworkEndpointPodMap:  negtypes.EndpointPodMap{},
			networkEndpointType:        negtypes.VmIpPortEndpointType,
		},
		{
			desc:     "default service port name",
			portName: "",
			wantZoneNetworkEndpointMap: map[string]negtypes.NetworkEndpointSet{
				negtypes.TestZone1: negtypes.NewNetworkEndpointSet([]negtypes.NetworkEndpoint{
					{IP: "10.100.1.1", Node: "instance1", Port: "80"},
					{IP: "10.100.1.2", Node: "instance1", Port: "80"},
					{IP: "10.100.1.3", Node: "instance1", Port: "80"},
					{IP: "10.100.1.4", Node: "instance1", Port: "80"},
					{IP: "10.100.2.1", Node: "instance2", Port: "80"},
				}...),
				negtypes.TestZone2: negtypes.NewNetworkEndpointSet([]negtypes.NetworkEndpoint{
					{IP: "10.100.3.1", Node: "instance3", Port: "80"},
				}...),
			},
			wantNetworkEndpointPodMap: negtypes.EndpointPodMap{
				{IP: "10.100.1.1", Node: "instance1", Port: "80"}: {Namespace: testServiceNamespace, Name: "pod1"},
				{IP: "10.100.1.2", Node: "instance1", Port: "80"}: {Namespace: testServiceNamespace, Name: "pod2"},
				{IP: "10.100.2.1", Node: "instance2", Port: "80"}: {Namespace: testServiceNamespace, Name: "pod3"},
				{IP: "10.100.3.1", Node: "instance3", Port: "80"}: {Namespace: testServiceNamespace, Name: "pod4"},
				{IP: "10.100.1.3", Node: "instance1", Port: "80"}: {Namespace: testServiceNamespace, Name: "pod5"},
				{IP: "10.100.1.4", Node: "instance1", Port: "80"}: {Namespace: testServiceNamespace, Name: "pod6"},
			},
			networkEndpointType: negtypes.VmIpPortEndpointType,
		},
		{
			desc:     "explicitly named service port",
			portName: testNamedPort,
			wantZoneNetworkEndpointMap: map[string]negtypes.NetworkEndpointSet{
				negtypes.TestZone1: negtypes.NewNetworkEndpointSet([]negtypes.NetworkEndpoint{
					{IP: "10.100.2.2", Node: "instance2", Port: "81"},
				}...),
				negtypes.TestZone2: negtypes.NewNetworkEndpointSet([]negtypes.NetworkEndpoint{
					{IP: "10.100.4.1", Node: "instance4", Port: "81"},
					{IP: "10.100.3.2", Node: "instance3", Port: "8081"},
					{IP: "10.100.4.2", Node: "instance4", Port: "8081"},
					{IP: "10.100.4.3", Node: "instance4", Port: "81"},
					{IP: "10.100.4.4", Node: "instance4", Port: "8081"},
				}...),
			},
			wantNetworkEndpointPodMap: negtypes.EndpointPodMap{
				{IP: "10.100.2.2", Node: "instance2", Port: "81"}:   {Namespace: testServiceNamespace, Name: "pod7"},
				{IP: "10.100.4.1", Node: "instance4", Port: "81"}:   {Namespace: testServiceNamespace, Name: "pod8"},
				{IP: "10.100.4.3", Node: "instance4", Port: "81"}:   {Namespace: testServiceNamespace, Name: "pod9"},
				{IP: "10.100.3.2", Node: "instance3", Port: "8081"}: {Namespace: testServiceNamespace, Name: "pod10"},
				{IP: "10.100.4.2", Node: "instance4", Port: "8081"}: {Namespace: testServiceNamespace, Name: "pod11"},
				{IP: "10.100.4.4", Node: "instance4", Port: "8081"}: {Namespace: testServiceNamespace, Name: "pod12"},
			},
			networkEndpointType: negtypes.VmIpPortEndpointType,
		},
		{
			desc:     "dual stack enabled with explicitly named service ports",
			portName: testNamedPort,
			wantZoneNetworkEndpointMap: map[string]negtypes.NetworkEndpointSet{
				negtypes.TestZone1: negtypes.NewNetworkEndpointSet([]negtypes.NetworkEndpoint{
					{IP: "10.100.2.2", Node: "instance2", Port: "81"},
				}...),
				negtypes.TestZone2: negtypes.NewNetworkEndpointSet([]negtypes.NetworkEndpoint{
					{IP: "10.100.4.1", Node: "instance4", Port: "81"},
					{IP: "10.100.3.2", IPv6: "a:b::1", Node: "instance3", Port: "8081"},
					{IP: "10.100.4.2", IPv6: "a:b::2", Node: "instance4", Port: "8081"},
					{IP: "10.100.4.3", Node: "instance4", Port: "81"},
					{IP: "10.100.4.4", IPv6: "a:b::3", Node: "instance4", Port: "8081"},
				}...),
			},
			wantNetworkEndpointPodMap: negtypes.EndpointPodMap{
				{IP: "10.100.2.2", Node: "instance2", Port: "81"}:                   {Namespace: testServiceNamespace, Name: "pod7"},
				{IP: "10.100.4.1", Node: "instance4", Port: "81"}:                   {Namespace: testServiceNamespace, Name: "pod8"},
				{IP: "10.100.4.3", Node: "instance4", Port: "81"}:                   {Namespace: testServiceNamespace, Name: "pod9"},
				{IP: "10.100.3.2", IPv6: "a:b::1", Node: "instance3", Port: "8081"}: {Namespace: testServiceNamespace, Name: "pod10"},
				{IP: "10.100.4.2", IPv6: "a:b::2", Node: "instance4", Port: "8081"}: {Namespace: testServiceNamespace, Name: "pod11"},
				{IP: "10.100.4.4", IPv6: "a:b::3", Node: "instance4", Port: "8081"}: {Namespace: testServiceNamespace, Name: "pod12"},
			},
			networkEndpointType: negtypes.VmIpPortEndpointType,
			enableDualStackNEG:  true,
		},
		{
			desc:     "non GCP network endpoints",
			portName: "",
			wantZoneNetworkEndpointMap: map[string]negtypes.NetworkEndpointSet{
				negtypes.TestZone1: negtypes.NewNetworkEndpointSet([]negtypes.NetworkEndpoint{
					{IP: "10.100.1.1", Port: "80"},
					{IP: "10.100.1.2", Port: "80"},
					{IP: "10.100.1.3", Port: "80"},
					{IP: "10.100.1.4", Port: "80"},
					{IP: "10.100.2.1", Port: "80"},
				}...),
				negtypes.TestZone2: negtypes.NewNetworkEndpointSet([]negtypes.NetworkEndpoint{
					{IP: "10.100.3.1", Port: "80"},
				}...),
			},
			wantNetworkEndpointPodMap: negtypes.EndpointPodMap{
				{IP: "10.100.1.1", Port: "80"}: {Namespace: testServiceNamespace, Name: "pod1"},
				{IP: "10.100.1.2", Port: "80"}: {Namespace: testServiceNamespace, Name: "pod2"},
				{IP: "10.100.2.1", Port: "80"}: {Namespace: testServiceNamespace, Name: "pod3"},
				{IP: "10.100.3.1", Port: "80"}: {Namespace: testServiceNamespace, Name: "pod4"},
				{IP: "10.100.1.3", Port: "80"}: {Namespace: testServiceNamespace, Name: "pod5"},
				{IP: "10.100.1.4", Port: "80"}: {Namespace: testServiceNamespace, Name: "pod6"},
			},
			networkEndpointType: negtypes.NonGCPPrivateEndpointType,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			gotResult, err := toZoneNetworkEndpointMap(negtypes.EndpointsDataFromEndpointSlices(getDefaultEndpointSlices()), zoneGetter, podLister, tc.portName, tc.networkEndpointType, tc.enableDualStackNEG)
			if err != nil {
				t.Errorf("toZoneNetworkEndpointMap() = err %v, want no error", err)
			}

			zoneNetworkEndpointMap, networkEndpointPodMap := gotResult.NetworkEndpointSet, gotResult.EndpointPodMap
			if diff := cmp.Diff(tc.wantZoneNetworkEndpointMap, zoneNetworkEndpointMap); diff != "" {
				t.Errorf("toZoneNetworkEndpointMap() returned unexpected diff for zoneNetworkEndpointMap (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.wantNetworkEndpointPodMap, networkEndpointPodMap); diff != "" {
				t.Errorf("toZoneNetworkEndpointMap() returned unexpected diff for networkEndpointPodMap (-want +got):\n%s", diff)
			}
		})
	}
}

func TestIpsForPod(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		desc  string
		input []negtypes.EndpointsData
		want  map[types.NamespacedName]negtypes.NetworkEndpoint
	}{
		{
			desc: "normal",
			input: negtypes.EndpointsDataFromEndpointSlices([]*discovery.EndpointSlice{
				{
					AddressType: discovery.AddressTypeIPv4,
					Endpoints: []discovery.Endpoint{
						{
							Addresses: []string{"10.0.0.1"},
							TargetRef: &v1.ObjectReference{Namespace: "ns", Name: "pod1"},
						},
					},
				},
				{
					AddressType: discovery.AddressTypeIPv4,
					Endpoints: []discovery.Endpoint{
						{
							Addresses: []string{"10.0.0.2"},
							TargetRef: &v1.ObjectReference{Namespace: "ns", Name: "pod2"},
						},
					},
				},
				{
					AddressType: discovery.AddressTypeIPv6,
					Endpoints: []discovery.Endpoint{
						{
							Addresses: []string{"a:b::1"},
							TargetRef: &v1.ObjectReference{Namespace: "ns", Name: "pod1"},
						},
						{
							Addresses: []string{"a:b::2"},
							TargetRef: &v1.ObjectReference{Namespace: "ns", Name: "pod2"},
						},
					},
				},
			}),
			want: map[types.NamespacedName]negtypes.NetworkEndpoint{
				{Namespace: "ns", Name: "pod1"}: {IP: "10.0.0.1", IPv6: "a:b::1"},
				{Namespace: "ns", Name: "pod2"}: {IP: "10.0.0.2", IPv6: "a:b::2"},
			},
		},
		{
			desc: "should skip endpoints without any address or target",
			input: negtypes.EndpointsDataFromEndpointSlices([]*discovery.EndpointSlice{
				{
					AddressType: discovery.AddressTypeIPv4,
					Endpoints: []discovery.Endpoint{
						{
							Addresses: []string{"10.0.0.1"},
							TargetRef: &v1.ObjectReference{Namespace: "ns", Name: "pod1"},
						},
						{
							// Endpoint without any address.
							TargetRef: &v1.ObjectReference{Namespace: "ns", Name: "pod2"},
						},
						{
							// Endpoint without any target.
							Addresses: []string{"10.0.0.2"},
						},
					},
				},
			}),
			want: map[types.NamespacedName]negtypes.NetworkEndpoint{
				{Namespace: "ns", Name: "pod1"}: {IP: "10.0.0.1"},
			},
		},
		{
			desc: "should ignore additional addresses in the same endpoint",
			input: negtypes.EndpointsDataFromEndpointSlices([]*discovery.EndpointSlice{
				{
					AddressType: discovery.AddressTypeIPv4,
					Endpoints: []discovery.Endpoint{
						{
							Addresses: []string{"10.0.0.1", "10.0.0.2"}, // More than 1 address.
							TargetRef: &v1.ObjectReference{Namespace: "ns", Name: "pod1"},
						},
					},
				},
				{
					AddressType: discovery.AddressTypeIPv6,
					Endpoints: []discovery.Endpoint{
						{
							Addresses: []string{"a:b::1", "a:b::2", "a:b::3"}, // More than 1 address.
							TargetRef: &v1.ObjectReference{Namespace: "ns", Name: "pod1"},
						},
					},
				},
			}),
			want: map[types.NamespacedName]negtypes.NetworkEndpoint{
				{Namespace: "ns", Name: "pod1"}: {IP: "10.0.0.1", IPv6: "a:b::1"},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			got := ipsForPod(tc.input)

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("ipsForPods(tc.input) returned unexpected diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestRetrieveExistingZoneNetworkEndpointMap(t *testing.T) {
	zoneGetter := negtypes.NewFakeZoneGetter()
	negCloud := negtypes.NewFakeNetworkEndpointGroupCloud("test-subnetwork", "test-network")
	negName := "test-neg-name"
	irrelevantNegName := "irrelevant"
	testIP1 := "1.2.3.4"
	testIP2 := "1.2.3.5"
	testIP3 := "1.2.3.6"
	testIP4 := "1.2.3.7"
	testIP5 := "1.2.3.8"
	testIP6 := "1.2.3.9"
	testIP7 := "1.2.3.10"
	testPort := int64(80)

	endpoint1 := negtypes.NetworkEndpoint{IP: testIP1, Node: negtypes.TestInstance1, Port: strconv.Itoa(int(testPort))}
	endpoint2 := negtypes.NetworkEndpoint{IP: testIP2, Node: negtypes.TestInstance2, Port: strconv.Itoa(int(testPort))}
	endpoint3 := negtypes.NetworkEndpoint{IP: testIP3, Node: negtypes.TestInstance3, Port: strconv.Itoa(int(testPort))}
	endpoint4 := negtypes.NetworkEndpoint{IP: testIP4, Node: negtypes.TestInstance4, Port: strconv.Itoa(int(testPort))}
	endpoint5 := negtypes.NetworkEndpoint{IP: testIP5, Node: negtypes.TestUnreadyInstance1, Port: strconv.Itoa(int(testPort))}
	endpoint6 := negtypes.NetworkEndpoint{IP: testIP6, Node: negtypes.TestUpgradeInstance1, Port: strconv.Itoa(int(testPort))}
	endpoint7 := negtypes.NetworkEndpoint{IP: testIP7, Node: negtypes.TestUpgradeInstance2, Port: strconv.Itoa(int(testPort))}

	testCases := []struct {
		desc                string
		mutate              func(cloud negtypes.NetworkEndpointGroupCloud)
		mode                negtypes.EndpointsCalculatorMode
		expect              map[string]negtypes.NetworkEndpointSet
		expectAnnotationMap labels.EndpointPodLabelMap
		expectErr           bool
	}{
		{
			desc:      "neg does not exist",
			mutate:    func(cloud negtypes.NetworkEndpointGroupCloud) {},
			expectErr: true,
		},
		{
			desc: "neg only exists in one of the zone",
			mutate: func(cloud negtypes.NetworkEndpointGroupCloud) {
				cloud.CreateNetworkEndpointGroup(&composite.NetworkEndpointGroup{Name: testNegName, Version: meta.VersionGA}, negtypes.TestZone1)
			},
			expectErr: true,
		},
		{
			desc: "neg only exists in one of the zone plus irrelevant negs",
			mutate: func(cloud negtypes.NetworkEndpointGroupCloud) {
				cloud.CreateNetworkEndpointGroup(&composite.NetworkEndpointGroup{Name: irrelevantNegName, Version: meta.VersionGA}, negtypes.TestZone2)
			},
			expectErr: true,
		},
		{
			desc: "empty negs exists in all 3 zones",
			mutate: func(cloud negtypes.NetworkEndpointGroupCloud) {
				cloud.CreateNetworkEndpointGroup(&composite.NetworkEndpointGroup{Name: testNegName, Version: meta.VersionGA}, negtypes.TestZone2)
				cloud.CreateNetworkEndpointGroup(&composite.NetworkEndpointGroup{Name: testNegName, Version: meta.VersionGA}, negtypes.TestZone4)
			},
			expect: map[string]negtypes.NetworkEndpointSet{
				negtypes.TestZone1: negtypes.NewNetworkEndpointSet(),
				negtypes.TestZone2: negtypes.NewNetworkEndpointSet(),
				negtypes.TestZone4: negtypes.NewNetworkEndpointSet(),
			},
			expectAnnotationMap: labels.EndpointPodLabelMap{},
			expectErr:           false,
		},
		{
			desc: "one empty and two non-empty negs",
			mutate: func(cloud negtypes.NetworkEndpointGroupCloud) {
				cloud.AttachNetworkEndpoints(testNegName, negtypes.TestZone1, []*composite.NetworkEndpoint{
					{
						Instance:  negtypes.TestInstance1,
						IpAddress: testIP1,
						Port:      testPort,
						Annotations: map[string]string{
							"foo": "bar",
						},
					},
				}, meta.VersionGA)
			},
			expect: map[string]negtypes.NetworkEndpointSet{
				negtypes.TestZone1: negtypes.NewNetworkEndpointSet(endpoint1),
				negtypes.TestZone2: negtypes.NewNetworkEndpointSet(),
				negtypes.TestZone4: negtypes.NewNetworkEndpointSet(),
			},
			expectAnnotationMap: labels.EndpointPodLabelMap{
				endpoint1: labels.PodLabelMap{
					"foo": "bar",
				},
			},
			expectErr: false,
		},
		{
			desc: "one neg with multiple endpoints",
			mutate: func(cloud negtypes.NetworkEndpointGroupCloud) {
				cloud.AttachNetworkEndpoints(testNegName, negtypes.TestZone1, []*composite.NetworkEndpoint{
					{
						Instance:  negtypes.TestInstance2,
						IpAddress: testIP2,
						Port:      testPort,
						Annotations: map[string]string{
							"foo": "bar",
						},
					},
				}, meta.VersionGA)
			},
			expect: map[string]negtypes.NetworkEndpointSet{
				negtypes.TestZone1: negtypes.NewNetworkEndpointSet(
					endpoint1,
					endpoint2,
				),
				negtypes.TestZone2: negtypes.NewNetworkEndpointSet(),
				negtypes.TestZone4: negtypes.NewNetworkEndpointSet(),
			},
			expectAnnotationMap: labels.EndpointPodLabelMap{
				endpoint1: labels.PodLabelMap{
					"foo": "bar",
				},
				endpoint2: labels.PodLabelMap{
					"foo": "bar",
				},
			},
			expectErr: false,
		},
		{
			desc: "2 negs with multiple endpoints",
			mutate: func(cloud negtypes.NetworkEndpointGroupCloud) {
				cloud.AttachNetworkEndpoints(testNegName, negtypes.TestZone2, []*composite.NetworkEndpoint{
					{
						Instance:  negtypes.TestInstance3,
						IpAddress: testIP3,
						Port:      testPort,
						Annotations: map[string]string{
							"foo": "bar",
						},
					},
					{
						Instance:  negtypes.TestInstance4,
						IpAddress: testIP4,
						Port:      testPort,
						Annotations: map[string]string{
							"foo": "bar",
						},
					},
				}, meta.VersionGA)
			},
			expect: map[string]negtypes.NetworkEndpointSet{
				negtypes.TestZone1: negtypes.NewNetworkEndpointSet(
					endpoint1,
					endpoint2,
				),
				negtypes.TestZone2: negtypes.NewNetworkEndpointSet(
					endpoint3,
					endpoint4,
				),
				negtypes.TestZone4: negtypes.NewNetworkEndpointSet(),
			},
			expectAnnotationMap: labels.EndpointPodLabelMap{
				endpoint1: labels.PodLabelMap{
					"foo": "bar",
				},
				endpoint2: labels.PodLabelMap{
					"foo": "bar",
				},
				endpoint3: labels.PodLabelMap{
					"foo": "bar",
				},
				endpoint4: labels.PodLabelMap{
					"foo": "bar",
				},
			},
			expectErr: false,
		},
		{
			desc: "all 3 negs with multiple endpoints, endpoint6 and endpoint7 with no pod label",
			mutate: func(cloud negtypes.NetworkEndpointGroupCloud) {
				cloud.AttachNetworkEndpoints(testNegName, negtypes.TestZone4, []*composite.NetworkEndpoint{
					{
						Instance:  negtypes.TestUpgradeInstance1,
						IpAddress: testIP6,
						Port:      testPort,
					},
					{
						Instance:  negtypes.TestUpgradeInstance2,
						IpAddress: testIP7,
						Port:      testPort,
					},
				}, meta.VersionGA)
			},
			expect: map[string]negtypes.NetworkEndpointSet{
				negtypes.TestZone1: negtypes.NewNetworkEndpointSet(
					endpoint1,
					endpoint2,
				),
				negtypes.TestZone2: negtypes.NewNetworkEndpointSet(
					endpoint3,
					endpoint4,
				),
				negtypes.TestZone4: negtypes.NewNetworkEndpointSet(
					endpoint6,
					endpoint7,
				),
			},
			expectAnnotationMap: labels.EndpointPodLabelMap{
				endpoint1: labels.PodLabelMap{
					"foo": "bar",
				},
				endpoint2: labels.PodLabelMap{
					"foo": "bar",
				},
				endpoint3: labels.PodLabelMap{
					"foo": "bar",
				},
				endpoint4: labels.PodLabelMap{
					"foo": "bar",
				},
				endpoint6: nil,
				endpoint7: nil,
			},
			expectErr: false,
		},
		{
			desc: "irrelevant neg",
			mutate: func(cloud negtypes.NetworkEndpointGroupCloud) {
				cloud.AttachNetworkEndpoints(irrelevantNegName, negtypes.TestZone2, []*composite.NetworkEndpoint{
					{
						Instance:  negtypes.TestInstance3,
						IpAddress: testIP4,
						Port:      testPort,
					},
				}, meta.VersionGA)
			},
			expect: map[string]negtypes.NetworkEndpointSet{
				negtypes.TestZone1: negtypes.NewNetworkEndpointSet(
					endpoint1,
					endpoint2,
				),
				negtypes.TestZone2: negtypes.NewNetworkEndpointSet(
					endpoint3,
					endpoint4,
				),
				negtypes.TestZone4: negtypes.NewNetworkEndpointSet(
					endpoint6,
					endpoint7,
				),
			},
			expectAnnotationMap: labels.EndpointPodLabelMap{
				endpoint1: labels.PodLabelMap{
					"foo": "bar",
				},
				endpoint2: labels.PodLabelMap{
					"foo": "bar",
				},
				endpoint3: labels.PodLabelMap{
					"foo": "bar",
				},
				endpoint4: labels.PodLabelMap{
					"foo": "bar",
				},
				endpoint6: nil,
				endpoint7: nil,
			},
			expectErr: false,
		},
		{
			desc: "non-empty negs in 4 zones, zone3 has no ready nodes, zone4 has upgrading nodes, but all NEGs are returned",
			mutate: func(cloud negtypes.NetworkEndpointGroupCloud) {
				// attach also creates the NEG in the fake implementation.
				cloud.AttachNetworkEndpoints(testNegName, negtypes.TestZone3, []*composite.NetworkEndpoint{
					{
						Instance:  negtypes.TestUnreadyInstance1,
						IpAddress: testIP5,
						Port:      testPort,
					},
				}, meta.VersionGA)
			},
			// set mode to L4 since this scenario applies more to VM_IP NEGs.
			mode: negtypes.L4LocalMode,
			expect: map[string]negtypes.NetworkEndpointSet{
				// NEGs in zone1, zone2 and zone4 are created from previous test case.
				negtypes.TestZone1: negtypes.NewNetworkEndpointSet(
					endpoint1,
					endpoint2,
				),
				negtypes.TestZone2: negtypes.NewNetworkEndpointSet(
					endpoint3,
					endpoint4,
				),
				negtypes.TestZone3: negtypes.NewNetworkEndpointSet(
					endpoint5,
				),
				negtypes.TestZone4: negtypes.NewNetworkEndpointSet(
					endpoint6,
					endpoint7,
				),
			},
			expectAnnotationMap: labels.EndpointPodLabelMap{
				endpoint1: labels.PodLabelMap{
					"foo": "bar",
				},
				endpoint2: labels.PodLabelMap{
					"foo": "bar",
				},
				endpoint3: labels.PodLabelMap{
					"foo": "bar",
				},
				endpoint4: labels.PodLabelMap{
					"foo": "bar",
				},
				endpoint5: nil,
				endpoint6: nil,
				endpoint7: nil,
			},
			expectErr: false,
		},
		{
			desc: "NEG does not exist in a zone where endpoints exist(mimics user deleting NEG manually)",
			mutate: func(cloud negtypes.NetworkEndpointGroupCloud) {
				cloud.DeleteNetworkEndpointGroup(testNegName, negtypes.TestZone2, meta.VersionGA)
			},
			expectErr: true,
		},
	}

	for _, tc := range testCases {
		tc.mutate(negCloud)
		// tc.mode of "" will result in the default node predicate being selected, which is ok for this test.
		endpointSets, annotationMap, err := retrieveExistingZoneNetworkEndpointMap(negName, zoneGetter, negCloud, meta.VersionGA, tc.mode)

		if tc.expectErr {
			if err == nil {
				t.Errorf("For test case %q, expecting error, but got nil", tc.desc)
			}
		} else {
			if err != nil {
				t.Errorf("For test case %q, endpointSets err = nil, but got %v", tc.desc, err)
			}
		}

		if !tc.expectErr {
			if diff := cmp.Diff(endpointSets, tc.expect); diff != "" {
				t.Errorf("For test case %q, (-want +got):\n%s", tc.desc, diff)
			}
			if diff := cmp.Diff(annotationMap, tc.expectAnnotationMap); diff != "" {
				t.Errorf("For test case %q, (-want +got):\n%s", tc.desc, diff)
			}
		}
	}
}

func TestMakeEndpointBatch(t *testing.T) {
	oldFlag := flags.F.EnableNEGLabelPropagation
	defer func() { flags.F.EnableNEGLabelPropagation = oldFlag }()

	testCases := []struct {
		desc                 string
		labelPropagationFlag bool
		endpointNum          int
		leftOverNum          int
	}{
		{
			"input with zero endpoints",
			false,
			0,
			0,
		},
		{
			"input with 1 endpoints",
			false,
			1,
			0,
		},
		{
			"input with 500 endpoints",
			false,
			500,
			0,
		},
		{
			"input with 501 endpoints",
			false,
			501,
			1,
		},
		{
			"input with 1000 endpoints",
			false,
			1000,
			500,
		},
		{
			"input with zero endpoints with label propagation",
			true,
			0,
			0,
		},
		{
			"input with 1 endpoints with label propagation",
			true,
			1,
			0,
		},
		{
			"input with 500 endpoints with label propagation",
			true,
			500,
			0,
		},
		{
			"input with 501 endpoints with label propagation",
			true,
			501,
			1,
		},
		{
			"input with 1000 endpoints with label propagation",
			true,
			1000,
			500,
		},
	}
	for _, negType := range []negtypes.NetworkEndpointType{negtypes.VmIpPortEndpointType, negtypes.VmIpEndpointType} {
		for _, tc := range testCases {
			flags.F.EnableNEGLabelPropagation = tc.labelPropagationFlag

			endpointSet, endpointMap, endpointPodLabelMap := genTestEndpoints(tc.endpointNum, negType, flags.F.EnableNEGLabelPropagation)

			out, err := makeEndpointBatch(endpointSet, negType, endpointPodLabelMap)

			if err != nil {
				t.Errorf("Expect err = nil, but got %v", err)
			}

			if endpointSet.Len() != tc.leftOverNum {
				t.Errorf("Expect endpoint set has %d endpoints left, but got %d", tc.leftOverNum, endpointSet.Len())
			}

			expectOutputEndpoints := tc.endpointNum
			if tc.endpointNum > MAX_NETWORK_ENDPOINTS_PER_BATCH {
				expectOutputEndpoints = MAX_NETWORK_ENDPOINTS_PER_BATCH
			}

			if expectOutputEndpoints != len(out) {
				t.Errorf("Expect %d endpoint(s) in output, but got %d", expectOutputEndpoints, len(out))
			}

			for key, endpoint := range out {
				if endpointSet.Has(key) {
					t.Errorf("Expect %q endpoint to exist in output endpoint map, but not", key)
				}
				expectEndpoint, ok := endpointMap[key]
				if !ok {
					t.Errorf("Expect %q endpoint to exist in expected endpoint map, but not", key)
				} else {
					if !reflect.DeepEqual(expectEndpoint, endpoint) {
						t.Errorf("Expect endpoint object %+v, but got %+v", expectEndpoint, endpoint)
					}
				}
			}
		}
	}
}

func TestNameUniqueness(t *testing.T) {
	var (
		testZone             = "test-zone"
		testNamedPort        = "named-port"
		testServiceName      = "test-svc"
		testServiceNameSpace = "test-ns"
		testNetwork          = cloud.ResourcePath("network", &meta.Key{Zone: testZone, Name: "test-network"})
		testSubnetwork       = cloud.ResourcePath("subnetwork", &meta.Key{Zone: testZone, Name: "test-subnetwork"})
		testKubesystemUID    = "cluster-uid"
		testPort             = "80"
		testServiceName2     = "test-svc-2"
		negName              = "test-neg"
		apiVersion           = meta.VersionGA
		networkEndpointType  = negtypes.VmIpPortEndpointType
	)
	fakeCloud := negtypes.NewFakeNetworkEndpointGroupCloud(testSubnetwork, testNetwork)
	_, err := ensureNetworkEndpointGroup(
		testServiceNameSpace,
		testServiceName,
		negName,
		testZone,
		testNamedPort,
		testKubesystemUID,
		testPort,
		networkEndpointType,
		fakeCloud,
		nil,
		nil,
		apiVersion,
		false,
	)
	if err != nil {
		t.Errorf("Errored while ensuring network endpoint groups: %s", err)
	}

	neg, err := fakeCloud.GetNetworkEndpointGroup(negName, testZone, apiVersion)
	if err != nil {
		t.Errorf("Failed to retrieve NEG %q: %v", negName, err)
	}

	if neg == nil {
		t.Errorf("Failed to find neg")
	}

	// Call ensureNetworkEndpointGroup with the same NEG name and different service name
	_, err = ensureNetworkEndpointGroup(
		testServiceNameSpace,
		testServiceName2,
		negName,
		testZone,
		testNamedPort,
		testKubesystemUID,
		testPort,
		networkEndpointType,
		fakeCloud,
		nil,
		nil,
		apiVersion,
		false,
	)

	if err == nil {
		t.Errorf("Expected error when called with duplicate NEG name")
	}
}

func TestNegObjectCrd(t *testing.T) {

	var (
		testZone             = "test-zone"
		testNamedPort        = "named-port"
		testServiceName      = "test-svc"
		testServiceNameSpace = "test-ns"
		testNetwork          = cloud.ResourcePath("network", &meta.Key{Zone: testZone, Name: "test-network"})
		testSubnetwork       = cloud.ResourcePath("subnetwork", &meta.Key{Zone: testZone, Name: "test-subnetwork"})
		testKubesystemUID    = "cluster-uid"
		testPort             = "80"
		negName              = "test-neg"
		apiVersion           = meta.VersionGA
	)

	for _, networkEndpointType := range []negtypes.NetworkEndpointType{
		negtypes.VmIpPortEndpointType,
		negtypes.VmIpEndpointType,
		negtypes.NonGCPPrivateEndpointType,
	} {
		fakeCloud := negtypes.NewFakeNetworkEndpointGroupCloud(testSubnetwork, testNetwork)
		negObj, err := ensureNetworkEndpointGroup(
			testServiceNameSpace,
			testServiceName,
			negName,
			testZone,
			testNamedPort,
			testKubesystemUID,
			testPort,
			networkEndpointType,
			fakeCloud,
			nil,
			nil,
			apiVersion,
			false,
		)
		if err != nil {
			t.Errorf("Errored while ensuring network endpoint groups: %s", err)
		}

		neg, err := fakeCloud.GetNetworkEndpointGroup(negName, testZone, apiVersion)
		if err != nil {
			t.Errorf("Failed to retrieve NEG %q: %v", negName, err)
		}

		if neg == nil {
			t.Errorf("Failed to find neg")
		}

		var expectedNegObj negv1beta1.NegObjectReference
		expectedNegObj = negv1beta1.NegObjectReference{
			Id:                  fmt.Sprint(neg.Id),
			SelfLink:            neg.SelfLink,
			NetworkEndpointType: negv1beta1.NetworkEndpointType(networkEndpointType),
		}

		if negObj != expectedNegObj {
			t.Errorf("Expected neg object %+v, but received %+v", expectedNegObj, negObj)
		}

		// Call ensureNetworkEndpointGroup with the same NEG name and different service name
		negObj, err = ensureNetworkEndpointGroup(
			testServiceNameSpace,
			testServiceName,
			negName,
			testZone,
			testNamedPort,
			testKubesystemUID,
			testPort,
			networkEndpointType,
			fakeCloud,
			nil,
			nil,
			apiVersion,
			false,
		)

		if err != nil {
			t.Errorf("Unexpected error when ensuring NEG: %s", err)
		}

		if negObj != expectedNegObj {
			t.Errorf("Expected neg object %+v, but received %+v", expectedNegObj, negObj)
		}
	}
}

func TestNEGRecreate(t *testing.T) {

	var (
		testZone             = "test-zone"
		testNamedPort        = "named-port"
		testServiceName      = "test-svc"
		testServiceNameSpace = "test-ns"
		testNetwork          = cloud.ResourcePath("network", &meta.Key{Zone: testZone, Name: "test-network"})
		testSubnetwork       = cloud.ResourcePath("subnetwork", &meta.Key{Zone: testZone, Name: "test-subnetwork"})
		diffNetwork          = "another-network"
		diffSubnetwork       = "another-subnetwork"
		testKubesystemUID    = "cluster-uid"
		testPort             = "80"
		negName              = "test-neg"
		apiVersion           = meta.VersionGA
	)

	matchingNegDesc := utils.NegDescription{
		ClusterUID:  testKubesystemUID,
		Namespace:   testServiceNamespace,
		ServiceName: testServiceName,
		Port:        testPort,
	}.String()

	anotherNegDesc := utils.NegDescription{
		ClusterUID:  "another-cluster",
		Namespace:   testServiceNamespace,
		ServiceName: testServiceName,
		Port:        testPort,
	}.String()

	testCases := []struct {
		desc           string
		network        string
		subnetwork     string
		negType        negtypes.NetworkEndpointType
		negDescription string
		expectRecreate bool
		expectError    bool
		customName     bool
	}{
		{
			desc:           "incorrect network, empty neg description, GCP endpoint type",
			network:        diffNetwork,
			subnetwork:     diffSubnetwork,
			negType:        negtypes.VmIpPortEndpointType,
			negDescription: "",
			expectRecreate: true,
			expectError:    false,
		},
		{
			desc:           "correct network, incorrect subnetwork, empty neg description, GCP endpoint type",
			network:        testNetwork,
			subnetwork:     diffSubnetwork,
			negType:        negtypes.VmIpPortEndpointType,
			negDescription: "",
			expectRecreate: true,
			expectError:    false,
		},
		{
			desc:           "correct network, correct subnetwork, customName, empty neg description, GCP endpoint type",
			network:        testNetwork,
			subnetwork:     testSubnetwork,
			negType:        negtypes.VmIpPortEndpointType,
			negDescription: "",
			expectRecreate: false,
			expectError:    true,
			customName:     true,
		},
		{
			desc:           "incorrect network, matching neg description, GCP endpoint type",
			network:        diffNetwork,
			subnetwork:     diffSubnetwork,
			negType:        negtypes.VmIpPortEndpointType,
			negDescription: matchingNegDesc,
			expectRecreate: true,
			expectError:    false,
		},
		{
			desc:           "correct network, incorrect subnetwork, matching neg description, GCP endpoint type",
			network:        testNetwork,
			subnetwork:     diffSubnetwork,
			negType:        negtypes.VmIpPortEndpointType,
			negDescription: matchingNegDesc,
			expectRecreate: true,
			expectError:    false,
		},
		{
			desc:           "incorrect network, different neg description, GCP endpoint type",
			network:        diffNetwork,
			subnetwork:     diffSubnetwork,
			negType:        negtypes.VmIpPortEndpointType,
			negDescription: anotherNegDesc,
			expectRecreate: false,
			expectError:    true,
		},
		{
			desc:           "correct network, incorrect subnetwork, different neg description, GCP endpoint type",
			network:        testNetwork,
			subnetwork:     diffSubnetwork,
			negType:        negtypes.VmIpPortEndpointType,
			negDescription: anotherNegDesc,
			expectRecreate: false,
			expectError:    true,
		},
		{
			desc:           "incorrect network, Non GCP endpoint type",
			network:        diffNetwork,
			subnetwork:     diffSubnetwork,
			negType:        negtypes.NonGCPPrivateEndpointType,
			negDescription: "",
			expectRecreate: false,
			expectError:    false,
		},
		{
			desc:           "correct network, incorrect subnetwork, Non GCP endpoint type",
			network:        testNetwork,
			subnetwork:     diffSubnetwork,
			negType:        negtypes.NonGCPPrivateEndpointType,
			negDescription: "",
			expectRecreate: false,
			expectError:    false,
		},
	}

	for _, tc := range testCases {
		fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
		negtypes.MockNetworkEndpointAPIs(fakeGCE)
		fakeCloud := negtypes.NewAdapterWithNetwork(fakeGCE, testNetwork, testSubnetwork)
		fakeCloud.CreateNetworkEndpointGroup(&composite.NetworkEndpointGroup{
			Version:             apiVersion,
			Name:                negName,
			NetworkEndpointType: string(tc.negType),
			Network:             tc.network,
			Subnetwork:          tc.subnetwork,
			Description:         tc.negDescription,
		}, testZone)

		// Ensure with the correct network and subnet
		_, err := ensureNetworkEndpointGroup(
			testServiceNameSpace,
			testServiceName,
			negName,
			testZone,
			testNamedPort,
			testKubesystemUID,
			testPort,
			tc.negType,
			fakeCloud,
			nil,
			nil,
			apiVersion,
			tc.customName,
		)
		if !tc.expectError && err != nil {
			t.Errorf("TestCase: %s, Errored while ensuring network endpoint groups: %s", tc.desc, err)
		} else if tc.expectError && err == nil {
			t.Errorf("TestCase: %s, Expected error when ensure network endpoint groups", tc.desc)
		}

		neg, err := fakeCloud.GetNetworkEndpointGroup(negName, testZone, apiVersion)
		if err != nil {
			t.Errorf("TestCase: %s, Failed to retrieve NEG %q: %v", tc.desc, negName, err)
		}

		if neg == nil {
			t.Errorf("TestCase: %s, Failed to find neg", tc.desc)
		}

		if tc.expectRecreate && (neg.Subnetwork != testSubnetwork || neg.Network != testNetwork) {
			t.Errorf("TestCase: %s\n Neg should have been recreated. Expected subnetwork %s, and found %s. Expected network %s, and found %s", tc.desc, testSubnetwork, neg.Subnetwork, testNetwork, testNetwork)
		} else if !tc.expectRecreate && (neg.Subnetwork != tc.subnetwork || neg.Network != tc.network) {
			t.Errorf("TestCase: %s\n Neg should not have been recreated. Expected subnetwork %s, and found %s. Expected network %s, and found %s", tc.desc, tc.subnetwork, neg.Subnetwork, tc.network, neg.Network)
		}
	}
}

func TestToZoneNetworkEndpointMapDegradedMode(t *testing.T) {
	t.Parallel()

	fakeZoneGetter := negtypes.NewFakeZoneGetter()
	testContext := negtypes.NewTestContext()
	podLister := testContext.PodInformer.GetIndexer()
	addPodsToLister(podLister)

	nodeLister := testContext.NodeInformer.GetIndexer()
	for i := 1; i <= 4; i++ {
		nodeLister.Add(&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf("instance%v", i),
			},
		})
	}

	testNonExistPort := "non-exists"
	testEmptyNamedPort := ""
	testNamedPort := "named-Port"
	testCases := []struct {
		desc                string
		testEndpointSlices  []*discovery.EndpointSlice
		portName            string
		expectedEndpointMap map[string]negtypes.NetworkEndpointSet
		expectedPodMap      negtypes.EndpointPodMap
		networkEndpointType negtypes.NetworkEndpointType
	}{
		{
			desc:                "non exist target port",
			testEndpointSlices:  getDefaultEndpointSlices(),
			portName:            testNonExistPort,
			expectedEndpointMap: map[string]negtypes.NetworkEndpointSet{},
			expectedPodMap:      negtypes.EndpointPodMap{},
			networkEndpointType: negtypes.VmIpPortEndpointType,
		},
		{
			desc:               "empty named port",
			testEndpointSlices: getDefaultEndpointSlices(),
			portName:           testEmptyNamedPort,
			expectedEndpointMap: map[string]negtypes.NetworkEndpointSet{
				negtypes.TestZone1: negtypes.NewNetworkEndpointSet(
					networkEndpointFromEncodedEndpoint("10.100.1.1||instance1||80"),
					networkEndpointFromEncodedEndpoint("10.100.1.2||instance1||80"),
					networkEndpointFromEncodedEndpoint("10.100.2.1||instance2||80"),
					networkEndpointFromEncodedEndpoint("10.100.1.3||instance1||80"),
					networkEndpointFromEncodedEndpoint("10.100.1.4||instance1||80")),
				negtypes.TestZone2: negtypes.NewNetworkEndpointSet(
					networkEndpointFromEncodedEndpoint("10.100.3.1||instance3||80")),
			},
			expectedPodMap: negtypes.EndpointPodMap{
				networkEndpointFromEncodedEndpoint("10.100.1.1||instance1||80"): types.NamespacedName{Namespace: testServiceNamespace, Name: "pod1"},
				networkEndpointFromEncodedEndpoint("10.100.1.2||instance1||80"): types.NamespacedName{Namespace: testServiceNamespace, Name: "pod2"},
				networkEndpointFromEncodedEndpoint("10.100.2.1||instance2||80"): types.NamespacedName{Namespace: testServiceNamespace, Name: "pod3"},
				networkEndpointFromEncodedEndpoint("10.100.3.1||instance3||80"): types.NamespacedName{Namespace: testServiceNamespace, Name: "pod4"},
				networkEndpointFromEncodedEndpoint("10.100.1.3||instance1||80"): types.NamespacedName{Namespace: testServiceNamespace, Name: "pod5"},
				networkEndpointFromEncodedEndpoint("10.100.1.4||instance1||80"): types.NamespacedName{Namespace: testServiceNamespace, Name: "pod6"},
			},
			networkEndpointType: negtypes.VmIpPortEndpointType,
		},
		{
			desc:               "named target port",
			testEndpointSlices: getDefaultEndpointSlices(),
			portName:           testNamedPort,
			expectedEndpointMap: map[string]negtypes.NetworkEndpointSet{
				negtypes.TestZone1: negtypes.NewNetworkEndpointSet(
					networkEndpointFromEncodedEndpoint("10.100.2.2||instance2||81")),
				negtypes.TestZone2: negtypes.NewNetworkEndpointSet(
					networkEndpointFromEncodedEndpoint("10.100.4.1||instance4||81"),
					networkEndpointFromEncodedEndpoint("10.100.3.2||instance3||8081"),
					networkEndpointFromEncodedEndpoint("10.100.4.2||instance4||8081"),
					networkEndpointFromEncodedEndpoint("10.100.4.3||instance4||81"),
					networkEndpointFromEncodedEndpoint("10.100.4.4||instance4||8081")),
			},
			expectedPodMap: negtypes.EndpointPodMap{
				networkEndpointFromEncodedEndpoint("10.100.2.2||instance2||81"):   types.NamespacedName{Namespace: testServiceNamespace, Name: "pod7"},
				networkEndpointFromEncodedEndpoint("10.100.4.1||instance4||81"):   types.NamespacedName{Namespace: testServiceNamespace, Name: "pod8"},
				networkEndpointFromEncodedEndpoint("10.100.4.3||instance4||81"):   types.NamespacedName{Namespace: testServiceNamespace, Name: "pod9"},
				networkEndpointFromEncodedEndpoint("10.100.3.2||instance3||8081"): types.NamespacedName{Namespace: testServiceNamespace, Name: "pod10"},
				networkEndpointFromEncodedEndpoint("10.100.4.2||instance4||8081"): types.NamespacedName{Namespace: testServiceNamespace, Name: "pod11"},
				networkEndpointFromEncodedEndpoint("10.100.4.4||instance4||8081"): types.NamespacedName{Namespace: testServiceNamespace, Name: "pod12"},
			},
			networkEndpointType: negtypes.VmIpPortEndpointType,
		},
		{
			desc:               "Non-GCP network endpoints",
			testEndpointSlices: getDefaultEndpointSlices(),
			portName:           testEmptyNamedPort,
			expectedEndpointMap: map[string]negtypes.NetworkEndpointSet{
				negtypes.TestZone1: negtypes.NewNetworkEndpointSet(
					networkEndpointFromEncodedEndpoint("10.100.1.1||||80"),
					networkEndpointFromEncodedEndpoint("10.100.1.2||||80"),
					networkEndpointFromEncodedEndpoint("10.100.2.1||||80"),
					networkEndpointFromEncodedEndpoint("10.100.1.3||||80"),
					networkEndpointFromEncodedEndpoint("10.100.1.4||||80")),
				negtypes.TestZone2: negtypes.NewNetworkEndpointSet(
					networkEndpointFromEncodedEndpoint("10.100.3.1||||80")),
			},
			expectedPodMap: negtypes.EndpointPodMap{
				networkEndpointFromEncodedEndpoint("10.100.1.1||||80"): types.NamespacedName{Namespace: testServiceNamespace, Name: "pod1"},
				networkEndpointFromEncodedEndpoint("10.100.1.2||||80"): types.NamespacedName{Namespace: testServiceNamespace, Name: "pod2"},
				networkEndpointFromEncodedEndpoint("10.100.2.1||||80"): types.NamespacedName{Namespace: testServiceNamespace, Name: "pod3"},
				networkEndpointFromEncodedEndpoint("10.100.3.1||||80"): types.NamespacedName{Namespace: testServiceNamespace, Name: "pod4"},
				networkEndpointFromEncodedEndpoint("10.100.1.3||||80"): types.NamespacedName{Namespace: testServiceNamespace, Name: "pod5"},
				networkEndpointFromEncodedEndpoint("10.100.1.4||||80"): types.NamespacedName{Namespace: testServiceNamespace, Name: "pod6"},
			},
			networkEndpointType: negtypes.NonGCPPrivateEndpointType,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			result := toZoneNetworkEndpointMapDegradedMode(negtypes.EndpointsDataFromEndpointSlices(tc.testEndpointSlices), fakeZoneGetter, podLister, nodeLister, tc.portName, tc.networkEndpointType)
			if !reflect.DeepEqual(result.NetworkEndpointSet, tc.expectedEndpointMap) {
				t.Errorf("degraded mode endpoint set is not calculated correctly:\ngot %+v,\n expected %+v", result.NetworkEndpointSet, tc.expectedEndpointMap)
			}
			if !reflect.DeepEqual(result.EndpointPodMap, tc.expectedPodMap) {
				t.Errorf("degraded mode endpoint map is not calculated correctly:\ngot %+v,\n expected %+v", result.EndpointPodMap, tc.expectedPodMap)
			}
		})
	}
}

func TestDegradedModeValidateEndpointInfo(t *testing.T) {
	t.Parallel()
	emptyNamedPort := ""
	emptyNodeName := ""
	port80 := int32(80)
	protocolTCP := v1.ProtocolTCP
	instance1 := negtypes.TestInstance1
	fakeZoneGetter := negtypes.NewFakeZoneGetter()
	testContext := negtypes.NewTestContext()
	podLister := testContext.PodInformer.GetIndexer()
	addPodsToLister(podLister)

	nodeLister := testContext.NodeInformer.GetIndexer()
	nodeLister.Add(&corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: instance1,
		},
	})

	endpointMap := map[string]negtypes.NetworkEndpointSet{
		negtypes.TestZone1: negtypes.NewNetworkEndpointSet(
			networkEndpointFromEncodedEndpoint("10.100.1.1||instance1||80"),
			networkEndpointFromEncodedEndpoint("10.100.1.2||instance1||80"),
		),
	}
	podMap := negtypes.EndpointPodMap{
		networkEndpointFromEncodedEndpoint("10.100.1.1||instance1||80"): types.NamespacedName{Namespace: testServiceNamespace, Name: "pod1"},
		networkEndpointFromEncodedEndpoint("10.100.1.2||instance1||80"): types.NamespacedName{Namespace: testServiceNamespace, Name: "pod2"},
	}

	testCases := []struct {
		desc                string
		testEndpointSlices  []*discovery.EndpointSlice
		endpointType        negtypes.NetworkEndpointType
		expectedEndpointMap map[string]negtypes.NetworkEndpointSet
		expectedPodMap      negtypes.EndpointPodMap
	}{
		{
			desc: "endpoint without nodeName, nodeName should be filled",
			testEndpointSlices: []*discovery.EndpointSlice{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testServiceName + "-1",
						Namespace: testServiceNamespace,
						Labels: map[string]string{
							discovery.LabelServiceName: testServiceName,
						},
					},
					AddressType: "IPv4",
					Endpoints: []discovery.Endpoint{
						{
							Addresses: []string{"10.100.1.1"},
							NodeName:  nil,
							TargetRef: &v1.ObjectReference{
								Namespace: testServiceNamespace,
								Name:      "pod1",
							},
						},
						{
							Addresses: []string{"10.100.1.2"},
							NodeName:  &instance1,
							TargetRef: &v1.ObjectReference{
								Namespace: testServiceNamespace,
								Name:      "pod2",
							},
						},
					},
					Ports: []discovery.EndpointPort{
						{
							Name:     &emptyNamedPort,
							Port:     &port80,
							Protocol: &protocolTCP,
						},
					},
				},
			},
			endpointType:        negtypes.VmIpPortEndpointType,
			expectedEndpointMap: endpointMap,
			expectedPodMap:      podMap,
		},
		{
			desc: "endpoint with empty nodeName, nodeName should be filled",
			testEndpointSlices: []*discovery.EndpointSlice{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testServiceName + "-1",
						Namespace: testServiceNamespace,
						Labels: map[string]string{
							discovery.LabelServiceName: testServiceName,
						},
					},
					AddressType: "IPv4",
					Endpoints: []discovery.Endpoint{
						{
							Addresses: []string{"10.100.1.1"},
							NodeName:  &emptyNodeName,
							TargetRef: &v1.ObjectReference{
								Namespace: testServiceNamespace,
								Name:      "pod1",
							},
						},
						{
							Addresses: []string{"10.100.1.2"},
							NodeName:  &instance1,
							TargetRef: &v1.ObjectReference{
								Namespace: testServiceNamespace,
								Name:      "pod2",
							},
						},
					},
					Ports: []discovery.EndpointPort{
						{
							Name:     &emptyNamedPort,
							Port:     &port80,
							Protocol: &protocolTCP,
						},
					},
				},
			},
			endpointType:        negtypes.VmIpPortEndpointType,
			expectedEndpointMap: endpointMap,
			expectedPodMap:      podMap,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			result := toZoneNetworkEndpointMapDegradedMode(negtypes.EndpointsDataFromEndpointSlices(tc.testEndpointSlices), fakeZoneGetter, podLister, nodeLister, emptyNamedPort, tc.endpointType)
			if !reflect.DeepEqual(result.NetworkEndpointSet, tc.expectedEndpointMap) {
				t.Errorf("degraded mode endpoint set is not calculated correctly:\ngot %+v,\n expected %+v", result.NetworkEndpointSet, tc.expectedEndpointMap)
			}
			if !reflect.DeepEqual(result.EndpointPodMap, tc.expectedPodMap) {
				t.Errorf("degraded mode endpoint map is not calculated correctly:\ngot %+v,\n expected %+v", result.EndpointPodMap, tc.expectedPodMap)
			}
		})
	}
}

func TestValidatePod(t *testing.T) {
	t.Parallel()

	instance1 := negtypes.TestInstance1
	testNodeNonExistent := "node-non-existent"
	testContext := negtypes.NewTestContext()
	nodeLister := testContext.NodeInformer.GetIndexer()
	nodeLister.Add(&corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: instance1,
		},
	})
	testCases := []struct {
		desc      string
		pod       *v1.Pod
		expectErr error
	}{
		{
			desc: "a valid pod with phase running",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testServiceNamespace,
					Name:      "pod1",
				},
				Status: v1.PodStatus{
					Phase: v1.PodRunning,
				},
				Spec: corev1.PodSpec{
					NodeName: instance1,
				},
			},
			expectErr: nil,
		},
		{
			desc: "a terminal pod with phase failed",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testServiceNamespace,
					Name:      "pod2",
				},
				Status: v1.PodStatus{
					Phase: v1.PodFailed,
				},
				Spec: corev1.PodSpec{
					NodeName: instance1,
				},
			},
			expectErr: negtypes.ErrEPPodTerminal,
		},
		{
			desc: "a terminal pod with phase succeeded",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testServiceNamespace,
					Name:      "pod3",
				},
				Status: v1.PodStatus{
					Phase: v1.PodSucceeded,
				},
				Spec: corev1.PodSpec{
					NodeName: instance1,
				},
			},
			expectErr: negtypes.ErrEPPodTerminal,
		},
		{
			desc: "a pod from non-existent node",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testServiceNamespace,
					Name:      "pod4",
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
				},
				Spec: corev1.PodSpec{
					NodeName: testNodeNonExistent,
				},
			},
			expectErr: negtypes.ErrEPNodeNotFound,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			if got := validatePod(tc.pod, nodeLister); !errors.Is(got, tc.expectErr) {
				t.Errorf("validatePod() = %t, expected %t\n", got, tc.expectErr)
			}
		})
	}
}

func TestParseIPAddress(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		desc  string
		input string
		want  string
	}{
		{
			desc:  "normal IPv4",
			input: "10.0.0.1",
			want:  "10.0.0.1",
		},
		{
			desc:  "normal IPv6",
			input: "a::b",
			want:  "a::b",
		},
		{
			desc:  "empty address",
			input: "",
			want:  "",
		},
		{
			desc:  "invalid IP address",
			input: "random string",
			want:  "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			got := parseIPAddress(tc.input)
			if got != tc.want {
				t.Errorf("parseIPAddress(%v)=%q; want=%q", tc.input, got, tc.want)
			}
		})
	}
}

func addPodsToLister(podLister cache.Indexer) {
	// add all pods in default endpoint into podLister
	for i := 1; i <= 6; i++ {
		podLister.Add(&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testServiceNamespace,
				Name:      fmt.Sprintf("pod%v", i),
			},
			Status: corev1.PodStatus{
				Phase: v1.PodRunning,
			},
			Spec: corev1.PodSpec{
				NodeName: testInstance1,
			},
		})
	}
	for i := 7; i <= 12; i++ {
		podLister.Add(&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testServiceNamespace,
				Name:      fmt.Sprintf("pod%v", i),
			},
			Status: corev1.PodStatus{
				Phase: v1.PodRunning,
			},
			Spec: corev1.PodSpec{
				NodeName: testInstance4,
			},
		})
	}

	podLister.Update(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testServiceNamespace,
			Name:      "pod3",
		},
		Status: corev1.PodStatus{
			Phase: v1.PodRunning,
		},
		Spec: corev1.PodSpec{
			NodeName: testInstance2,
		},
	})
	podLister.Update(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testServiceNamespace,
			Name:      "pod4",
		},
		Status: corev1.PodStatus{
			Phase: v1.PodRunning,
		},
		Spec: corev1.PodSpec{
			NodeName: testInstance3,
		},
	})
	podLister.Update(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testServiceNamespace,
			Name:      "pod7",
		},
		Status: corev1.PodStatus{
			Phase: v1.PodRunning,
		},
		Spec: corev1.PodSpec{
			NodeName: testInstance2,
		},
	})
	podLister.Update(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testServiceNamespace,
			Name:      "pod10",
		},
		Status: corev1.PodStatus{
			Phase: v1.PodRunning,
		},
		Spec: corev1.PodSpec{
			NodeName: testInstance3,
		},
	})
}

// numToIP converts the given number to an IP address.
// assumes that the input is smaller than 2^32.
func numToIP(input int) string {
	ip := []byte{0, 0, 0, 0}
	div := 256
	ip[3] = byte(input % div)
	for i := 1; i < 4; i++ {
		ip[3-i] = byte(input / div)
		div = div * 256
	}
	return net.IP(ip).String()
}

func genTestEndpoints(num int, epType negtypes.NetworkEndpointType, lpFlag bool) (negtypes.NetworkEndpointSet, map[negtypes.NetworkEndpoint]*composite.NetworkEndpoint, labels.EndpointPodLabelMap) {
	endpointSet := negtypes.NewNetworkEndpointSet()
	endpointMap := map[negtypes.NetworkEndpoint]*composite.NetworkEndpoint{}
	endpointPodLabelMap := labels.EndpointPodLabelMap{}
	ip := "1.2.3.4"
	instance := "instance"
	port := 0
	annotations := map[string]string{
		"label1": "value1",
		"label2": "value2",
	}
	for count := 0; count < num; count++ {
		switch epType {
		case negtypes.VmIpEndpointType:
			ip = numToIP(count)
			key := negtypes.NetworkEndpoint{IP: ip, Node: instance}
			endpointSet.Insert(key)
			endpointMap[key] = &composite.NetworkEndpoint{
				IpAddress: ip,
				Instance:  instance,
			}
		case negtypes.VmIpPortEndpointType:
			port++
			key := negtypes.NetworkEndpoint{IP: ip, Node: instance, Port: strconv.Itoa(port)}
			endpointSet.Insert(key)
			endpointPodLabelMap[key] = annotations
			networkEndpoint := &composite.NetworkEndpoint{
				IpAddress: ip,
				Instance:  instance,
				Port:      int64(port),
			}
			if lpFlag {
				networkEndpoint.Annotations = annotations
			}
			endpointMap[key] = networkEndpoint
		}
	}
	return endpointSet, endpointMap, endpointPodLabelMap
}

func networkEndpointFromEncodedEndpoint(encodedEndpoint string) negtypes.NetworkEndpoint {
	ip, node, port := decodeEndpoint(encodedEndpoint)
	return negtypes.NetworkEndpoint{IP: ip, Node: node, Port: port}
}

func getDefaultEndpointSlices() []*discovery.EndpointSlice {
	return getTestEndpointSlices(testServiceName, testServiceNamespace)
}

func getTestEndpointSlices(name, namespace string) []*discovery.EndpointSlice {
	instance1 := negtypes.TestInstance1
	instance2 := negtypes.TestInstance2
	instance3 := negtypes.TestInstance3
	instance4 := negtypes.TestInstance4
	notReady := false
	emptyNamedPort := ""
	testNamedPort := testNamedPort
	port80 := int32(80)
	port81 := int32(81)
	port8081 := int32(8081)
	protocolTCP := v1.ProtocolTCP
	return []*discovery.EndpointSlice{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name + "-1",
				Namespace: namespace,
				Labels: map[string]string{
					discovery.LabelServiceName: name,
				},
			},
			AddressType: "IPv4",
			Endpoints: []discovery.Endpoint{
				{
					Addresses: []string{"10.100.1.1"},
					NodeName:  &instance1,
					TargetRef: &v1.ObjectReference{
						Namespace: namespace,
						Name:      "pod1",
					},
				},
				{
					Addresses: []string{"10.100.1.2"},
					NodeName:  &instance1,
					TargetRef: &v1.ObjectReference{
						Namespace: namespace,
						Name:      "pod2",
					},
				},
				{
					Addresses: []string{"10.100.2.1"},
					NodeName:  &instance2,
					TargetRef: &v1.ObjectReference{
						Namespace: namespace,
						Name:      "pod3",
					},
				},
				{
					Addresses: []string{"10.100.3.1"},
					NodeName:  &instance3,
					TargetRef: &v1.ObjectReference{
						Namespace: namespace,
						Name:      "pod4",
					},
				},
				{
					Addresses: []string{"10.100.1.3"},
					NodeName:  &instance1,
					TargetRef: &v1.ObjectReference{
						Namespace: namespace,
						Name:      "pod5",
					},
					Conditions: discovery.EndpointConditions{Ready: &notReady},
				},
				{
					Addresses: []string{"10.100.1.4"},
					NodeName:  &instance1,
					TargetRef: &v1.ObjectReference{
						Namespace: namespace,
						Name:      "pod6",
					},
					Conditions: discovery.EndpointConditions{Ready: &notReady},
				},
			},
			Ports: []discovery.EndpointPort{
				{
					Name:     &emptyNamedPort,
					Port:     &port80,
					Protocol: &protocolTCP,
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name + "-2",
				Namespace: namespace,
				Labels: map[string]string{
					discovery.LabelServiceName: name,
				},
			},
			AddressType: "IPv4",
			Endpoints: []discovery.Endpoint{
				{
					Addresses: []string{"10.100.2.2"},
					NodeName:  &instance2,
					TargetRef: &v1.ObjectReference{
						Namespace: namespace,
						Name:      "pod7",
					},
				},
				{
					Addresses: []string{"10.100.4.1"},
					NodeName:  &instance4,
					TargetRef: &v1.ObjectReference{
						Namespace: namespace,
						Name:      "pod8",
					},
				},
				{
					Addresses: []string{"10.100.4.3"},
					NodeName:  &instance4,
					TargetRef: &v1.ObjectReference{
						Namespace: namespace,
						Name:      "pod9",
					},
					Conditions: discovery.EndpointConditions{Ready: &notReady},
				},
			},
			Ports: []discovery.EndpointPort{
				{
					Name:     &testNamedPort,
					Port:     &port81,
					Protocol: &protocolTCP,
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name + "-3",
				Namespace: namespace,
				Labels: map[string]string{
					discovery.LabelServiceName: name,
				},
			},
			AddressType: "IPv4",
			Endpoints: []discovery.Endpoint{
				{
					Addresses: []string{"10.100.3.2"},
					NodeName:  &instance3,
					TargetRef: &v1.ObjectReference{
						Namespace: namespace,
						Name:      "pod10",
					},
				},
				{
					Addresses: []string{"10.100.4.2"},
					NodeName:  &instance4,
					TargetRef: &v1.ObjectReference{
						Namespace: namespace,
						Name:      "pod11",
					},
				},
				{
					Addresses: []string{"10.100.4.4"},
					NodeName:  &instance4,
					TargetRef: &v1.ObjectReference{
						Namespace: namespace,
						Name:      "pod12",
					},
					Conditions: discovery.EndpointConditions{Ready: &notReady},
				},
			},
			Ports: []discovery.EndpointPort{
				{
					Name:     &testNamedPort,
					Port:     &port8081,
					Protocol: &protocolTCP,
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name + "-4",
				Namespace: namespace,
				Labels: map[string]string{
					discovery.LabelServiceName: name,
				},
			},
			AddressType: discovery.AddressTypeIPv6,
			Endpoints: []discovery.Endpoint{
				{
					Addresses: []string{"a:b::1"},
					NodeName:  &instance3,
					TargetRef: &v1.ObjectReference{
						Namespace: namespace,
						Name:      "pod10",
					},
				},
				{
					Addresses: []string{"a:b::2"},
					NodeName:  &instance4,
					TargetRef: &v1.ObjectReference{
						Namespace: namespace,
						Name:      "pod11",
					},
				},
				{
					Addresses: []string{"a:b::3"},
					NodeName:  &instance4,
					TargetRef: &v1.ObjectReference{
						Namespace: namespace,
						Name:      "pod12",
					},
					Conditions: discovery.EndpointConditions{Ready: &notReady},
				},
			},
			Ports: []discovery.EndpointPort{
				{
					Name:     &emptyNamedPort,
					Port:     &port80,
					Protocol: &protocolTCP,
				},
			},
		},
	}
}
