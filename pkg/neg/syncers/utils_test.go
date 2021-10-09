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
	"fmt"
	"net"
	"reflect"
	"strconv"
	"testing"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	v1 "k8s.io/api/core/v1"
	discovery "k8s.io/api/discovery/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	negv1beta1 "k8s.io/ingress-gce/pkg/apis/svcneg/v1beta1"
	"k8s.io/ingress-gce/pkg/composite"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/legacy-cloud-providers/gce"
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

func TestToZoneNetworkEndpointMapUtil(t *testing.T) {
	t.Parallel()
	_, transactionSyncer := newTestTransactionSyncer(negtypes.NewAdapter(gce.NewFakeGCECloud(gce.DefaultTestClusterValues())), negtypes.VmIpPortEndpointType, false, false)
	podLister := transactionSyncer.podLister

	// add all pods in default endpoint into podLister
	for i := 1; i <= 12; i++ {
		podLister.Add(&v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testServiceNamespace,
				Name:      fmt.Sprintf("pod%v", i),
			},
		})
	}
	// pod6 is deleted
	podLister.Update(&v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:         testServiceNamespace,
			Name:              "pod6",
			DeletionTimestamp: &metav1.Time{},
		},
	})

	podLister.Update(&v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:         testServiceNamespace,
			Name:              "pod12",
			DeletionTimestamp: &metav1.Time{},
		},
	})

	zoneGetter := negtypes.NewFakeZoneGetter()
	testCases := []struct {
		desc                string
		portName            string
		endpointSets        map[string]negtypes.NetworkEndpointSet
		expectMap           negtypes.EndpointPodMap
		networkEndpointType negtypes.NetworkEndpointType
	}{
		{
			desc:                "non exist target port",
			portName:            "non-exists",
			endpointSets:        map[string]negtypes.NetworkEndpointSet{},
			expectMap:           negtypes.EndpointPodMap{},
			networkEndpointType: negtypes.VmIpPortEndpointType,
		},
		{
			desc:     "target port number",
			portName: "",
			endpointSets: map[string]negtypes.NetworkEndpointSet{
				negtypes.TestZone1: negtypes.NewNetworkEndpointSet(
					networkEndpointFromEncodedEndpoint("10.100.1.1||instance1||80"),
					networkEndpointFromEncodedEndpoint("10.100.1.2||instance1||80"),
					networkEndpointFromEncodedEndpoint("10.100.2.1||instance2||80"),
					networkEndpointFromEncodedEndpoint("10.100.1.3||instance1||80")),
				negtypes.TestZone2: negtypes.NewNetworkEndpointSet(
					networkEndpointFromEncodedEndpoint("10.100.3.1||instance3||80")),
			},
			expectMap: negtypes.EndpointPodMap{
				networkEndpointFromEncodedEndpoint("10.100.1.1||instance1||80"): types.NamespacedName{Namespace: testServiceNamespace, Name: "pod1"},
				networkEndpointFromEncodedEndpoint("10.100.1.2||instance1||80"): types.NamespacedName{Namespace: testServiceNamespace, Name: "pod2"},
				networkEndpointFromEncodedEndpoint("10.100.2.1||instance2||80"): types.NamespacedName{Namespace: testServiceNamespace, Name: "pod3"},
				networkEndpointFromEncodedEndpoint("10.100.3.1||instance3||80"): types.NamespacedName{Namespace: testServiceNamespace, Name: "pod4"},
				networkEndpointFromEncodedEndpoint("10.100.1.3||instance1||80"): types.NamespacedName{Namespace: testServiceNamespace, Name: "pod5"},
			},
			networkEndpointType: negtypes.VmIpPortEndpointType,
		},
		{
			desc:     "named target port",
			portName: testNamedPort,
			endpointSets: map[string]negtypes.NetworkEndpointSet{
				negtypes.TestZone1: negtypes.NewNetworkEndpointSet(
					networkEndpointFromEncodedEndpoint("10.100.2.2||instance2||81")),
				negtypes.TestZone2: negtypes.NewNetworkEndpointSet(
					networkEndpointFromEncodedEndpoint("10.100.4.1||instance4||81"),
					networkEndpointFromEncodedEndpoint("10.100.3.2||instance3||8081"),
					networkEndpointFromEncodedEndpoint("10.100.4.2||instance4||8081"),
					networkEndpointFromEncodedEndpoint("10.100.4.3||instance4||81")),
			},
			expectMap: negtypes.EndpointPodMap{
				networkEndpointFromEncodedEndpoint("10.100.2.2||instance2||81"):   types.NamespacedName{Namespace: testServiceNamespace, Name: "pod7"},
				networkEndpointFromEncodedEndpoint("10.100.4.1||instance4||81"):   types.NamespacedName{Namespace: testServiceNamespace, Name: "pod8"},
				networkEndpointFromEncodedEndpoint("10.100.4.3||instance4||81"):   types.NamespacedName{Namespace: testServiceNamespace, Name: "pod9"},
				networkEndpointFromEncodedEndpoint("10.100.3.2||instance3||8081"): types.NamespacedName{Namespace: testServiceNamespace, Name: "pod10"},
				networkEndpointFromEncodedEndpoint("10.100.4.2||instance4||8081"): types.NamespacedName{Namespace: testServiceNamespace, Name: "pod11"},
			},
			networkEndpointType: negtypes.VmIpPortEndpointType,
		},
		{
			desc:     "Non-GCP network endpoints",
			portName: "",
			endpointSets: map[string]negtypes.NetworkEndpointSet{
				negtypes.TestZone1: negtypes.NewNetworkEndpointSet(
					networkEndpointFromEncodedEndpoint("10.100.1.1||||80"),
					networkEndpointFromEncodedEndpoint("10.100.1.2||||80"),
					networkEndpointFromEncodedEndpoint("10.100.2.1||||80"),
					networkEndpointFromEncodedEndpoint("10.100.1.3||||80")),
				negtypes.TestZone2: negtypes.NewNetworkEndpointSet(
					networkEndpointFromEncodedEndpoint("10.100.3.1||||80")),
			},
			expectMap: negtypes.EndpointPodMap{
				networkEndpointFromEncodedEndpoint("10.100.1.1||||80"): types.NamespacedName{Namespace: testServiceNamespace, Name: "pod1"},
				networkEndpointFromEncodedEndpoint("10.100.1.2||||80"): types.NamespacedName{Namespace: testServiceNamespace, Name: "pod2"},
				networkEndpointFromEncodedEndpoint("10.100.2.1||||80"): types.NamespacedName{Namespace: testServiceNamespace, Name: "pod3"},
				networkEndpointFromEncodedEndpoint("10.100.3.1||||80"): types.NamespacedName{Namespace: testServiceNamespace, Name: "pod4"},
				networkEndpointFromEncodedEndpoint("10.100.1.3||||80"): types.NamespacedName{Namespace: testServiceNamespace, Name: "pod5"},
			},
			networkEndpointType: negtypes.NonGCPPrivateEndpointType,
		},
	}

	for _, tc := range testCases {
		retSet, retMap, err := toZoneNetworkEndpointMap(negtypes.EndpointsDataFromEndpoints(getDefaultEndpoint()), zoneGetter, tc.portName, podLister, "", tc.networkEndpointType)
		if err != nil {
			t.Errorf("For case %q, expect nil error, but got %v.", tc.desc, err)
		}

		if !reflect.DeepEqual(retSet, tc.endpointSets) {
			t.Errorf("For case %q, expecting endpoint set %v, but got %v.", tc.desc, tc.endpointSets, retSet)
		}

		if !reflect.DeepEqual(retMap, tc.expectMap) {
			t.Errorf("For case %q, expecting endpoint map %v, but got %v.", tc.desc, tc.expectMap, retMap)
		}
	}
}

func TestRetrieveExistingZoneNetworkEndpointMap(t *testing.T) {
	zoneGetter := negtypes.NewFakeZoneGetter()
	negCloud := negtypes.NewFakeNetworkEndpointGroupCloud("test-subnetwork", "test-newtork")
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

	testCases := []struct {
		desc      string
		mutate    func(cloud negtypes.NetworkEndpointGroupCloud)
		mode      negtypes.EndpointsCalculatorMode
		expect    map[string]negtypes.NetworkEndpointSet
		expectErr bool
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
			expectErr: false,
		},
		{
			desc: "one empty and two non-empty negs",
			mutate: func(cloud negtypes.NetworkEndpointGroupCloud) {
				cloud.AttachNetworkEndpoints(testNegName, negtypes.TestZone1, []*composite.NetworkEndpoint{
					{
						Instance:  negtypes.TestInstance1,
						IpAddress: testIP1,
						Port:      testPort,
					},
				}, meta.VersionGA)
			},
			expect: map[string]negtypes.NetworkEndpointSet{
				negtypes.TestZone1: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: testIP1, Node: negtypes.TestInstance1, Port: strconv.Itoa(int(testPort))}),
				negtypes.TestZone2: negtypes.NewNetworkEndpointSet(),
				negtypes.TestZone4: negtypes.NewNetworkEndpointSet(),
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
					},
				}, meta.VersionGA)
			},
			expect: map[string]negtypes.NetworkEndpointSet{
				negtypes.TestZone1: negtypes.NewNetworkEndpointSet(
					negtypes.NetworkEndpoint{IP: testIP1, Node: negtypes.TestInstance1, Port: strconv.Itoa(int(testPort))},
					negtypes.NetworkEndpoint{IP: testIP2, Node: negtypes.TestInstance2, Port: strconv.Itoa(int(testPort))},
				),
				negtypes.TestZone2: negtypes.NewNetworkEndpointSet(),
				negtypes.TestZone4: negtypes.NewNetworkEndpointSet(),
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
					},
					{
						Instance:  negtypes.TestInstance4,
						IpAddress: testIP4,
						Port:      testPort,
					},
				}, meta.VersionGA)
			},
			expect: map[string]negtypes.NetworkEndpointSet{
				negtypes.TestZone1: negtypes.NewNetworkEndpointSet(
					negtypes.NetworkEndpoint{IP: testIP1, Node: negtypes.TestInstance1, Port: strconv.Itoa(int(testPort))},
					negtypes.NetworkEndpoint{IP: testIP2, Node: negtypes.TestInstance2, Port: strconv.Itoa(int(testPort))},
				),
				negtypes.TestZone2: negtypes.NewNetworkEndpointSet(
					negtypes.NetworkEndpoint{IP: testIP3, Node: negtypes.TestInstance3, Port: strconv.Itoa(int(testPort))},
					negtypes.NetworkEndpoint{IP: testIP4, Node: negtypes.TestInstance4, Port: strconv.Itoa(int(testPort))},
				),
				negtypes.TestZone4: negtypes.NewNetworkEndpointSet(),
			},
			expectErr: false,
		},
		{
			desc: "all 3 negs with multiple endpoints",
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
					negtypes.NetworkEndpoint{IP: testIP1, Node: negtypes.TestInstance1, Port: strconv.Itoa(int(testPort))},
					negtypes.NetworkEndpoint{IP: testIP2, Node: negtypes.TestInstance2, Port: strconv.Itoa(int(testPort))},
				),
				negtypes.TestZone2: negtypes.NewNetworkEndpointSet(
					negtypes.NetworkEndpoint{IP: testIP3, Node: negtypes.TestInstance3, Port: strconv.Itoa(int(testPort))},
					negtypes.NetworkEndpoint{IP: testIP4, Node: negtypes.TestInstance4, Port: strconv.Itoa(int(testPort))},
				),
				negtypes.TestZone4: negtypes.NewNetworkEndpointSet(
					negtypes.NetworkEndpoint{IP: testIP6, Node: negtypes.TestUpgradeInstance1, Port: strconv.Itoa(int(testPort))},
					negtypes.NetworkEndpoint{IP: testIP7, Node: negtypes.TestUpgradeInstance2, Port: strconv.Itoa(int(testPort))},
				),
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
					negtypes.NetworkEndpoint{IP: testIP1, Node: negtypes.TestInstance1, Port: strconv.Itoa(int(testPort))},
					negtypes.NetworkEndpoint{IP: testIP2, Node: negtypes.TestInstance2, Port: strconv.Itoa(int(testPort))},
				),
				negtypes.TestZone2: negtypes.NewNetworkEndpointSet(
					negtypes.NetworkEndpoint{IP: testIP3, Node: negtypes.TestInstance3, Port: strconv.Itoa(int(testPort))},
					negtypes.NetworkEndpoint{IP: testIP4, Node: negtypes.TestInstance4, Port: strconv.Itoa(int(testPort))},
				),
				negtypes.TestZone4: negtypes.NewNetworkEndpointSet(
					negtypes.NetworkEndpoint{IP: testIP6, Node: negtypes.TestUpgradeInstance1, Port: strconv.Itoa(int(testPort))},
					negtypes.NetworkEndpoint{IP: testIP7, Node: negtypes.TestUpgradeInstance2, Port: strconv.Itoa(int(testPort))},
				),
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
					negtypes.NetworkEndpoint{IP: testIP1, Node: negtypes.TestInstance1, Port: strconv.Itoa(int(testPort))},
					negtypes.NetworkEndpoint{IP: testIP2, Node: negtypes.TestInstance2, Port: strconv.Itoa(int(testPort))},
				),
				negtypes.TestZone2: negtypes.NewNetworkEndpointSet(
					negtypes.NetworkEndpoint{IP: testIP3, Node: negtypes.TestInstance3, Port: strconv.Itoa(int(testPort))},
					negtypes.NetworkEndpoint{IP: testIP4, Node: negtypes.TestInstance4, Port: strconv.Itoa(int(testPort))},
				),
				negtypes.TestZone3: negtypes.NewNetworkEndpointSet(
					negtypes.NetworkEndpoint{IP: testIP5, Node: negtypes.TestUnreadyInstance1, Port: strconv.Itoa(int(testPort))},
				),
				negtypes.TestZone4: negtypes.NewNetworkEndpointSet(
					negtypes.NetworkEndpoint{IP: testIP6, Node: negtypes.TestUpgradeInstance1, Port: strconv.Itoa(int(testPort))},
					negtypes.NetworkEndpoint{IP: testIP7, Node: negtypes.TestUpgradeInstance2, Port: strconv.Itoa(int(testPort))},
				),
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
		out, err := retrieveExistingZoneNetworkEndpointMap(negName, zoneGetter, negCloud, meta.VersionGA, tc.mode)

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
			if !reflect.DeepEqual(out, tc.expect) {
				t.Errorf("For test case %q, endpointSets output = %+v, but want %+v", tc.desc, tc.expect, out)
			}
		}
	}
}

func TestMakeEndpointBatch(t *testing.T) {
	testCases := []struct {
		desc        string
		endpointNum int
		leftOverNum int
	}{
		{
			"input with zero endpoints",
			0,
			0,
		},
		{
			"input with 1 endpoints",
			1,
			0,
		},
		{
			"input with 500 endpoints",
			500,
			0,
		},
		{
			"input with 501 endpoints",
			501,
			1,
		},
		{
			"input with 1000 endpoints",
			1000,
			500,
		},
	}
	for _, negType := range []negtypes.NetworkEndpointType{negtypes.VmIpPortEndpointType, negtypes.VmIpEndpointType} {
		for _, tc := range testCases {
			endpointSet, endpointMap := genTestEndpoints(tc.endpointNum, negType)
			out, err := makeEndpointBatch(endpointSet, negType)

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

func TestShouldPodBeInNeg(t *testing.T) {
	t.Parallel()

	_, transactionSyncer := newTestTransactionSyncer(negtypes.NewAdapter(gce.NewFakeGCECloud(gce.DefaultTestClusterValues())), negtypes.VmIpPortEndpointType, false, false)

	podLister := transactionSyncer.podLister

	namespace1 := "ns1"
	namespace2 := "ns2"
	name1 := "n1"
	name2 := "n2"

	podLister.Add(&v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace1,
			Name:      name1,
		},
	})

	// deleted pod
	podLister.Add(&v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:         namespace1,
			Name:              name2,
			DeletionTimestamp: &metav1.Time{},
		},
	})

	podLister.Add(&v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace2,
			Name:      name2,
		},
	})

	for _, tc := range []struct {
		desc      string
		namespace string
		name      string
		expect    bool
	}{
		{
			desc: "empty input",
		},
		{
			desc:      "non exists pod",
			namespace: "non exists",
			name:      "non exists",
			expect:    false,
		},
		{
			desc:      "pod exists and not deleted",
			namespace: namespace1,
			name:      name1,
			expect:    true,
		},
		{
			desc:      "pod exists and deleted",
			namespace: namespace1,
			name:      name2,
			expect:    false,
		},
		{
			desc:      "pod exists and not deleted 2",
			namespace: namespace2,
			name:      name2,
			expect:    true,
		},
	} {
		ret := shouldPodBeInNeg(podLister, tc.namespace, tc.name)
		if ret != tc.expect {
			t.Errorf("For test case %q, endpointSets output = %+v, but got %+v", tc.desc, tc.expect, ret)
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

func genTestEndpoints(num int, epType negtypes.NetworkEndpointType) (negtypes.NetworkEndpointSet, map[negtypes.NetworkEndpoint]*composite.NetworkEndpoint) {
	endpointSet := negtypes.NewNetworkEndpointSet()
	endpointMap := map[negtypes.NetworkEndpoint]*composite.NetworkEndpoint{}
	ip := "1.2.3.4"
	instance := "instance"
	port := 0
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
			endpointMap[key] = &composite.NetworkEndpoint{
				IpAddress: ip,
				Instance:  instance,
				Port:      int64(port),
			}
		}
	}
	return endpointSet, endpointMap
}

func networkEndpointFromEncodedEndpoint(encodedEndpoint string) negtypes.NetworkEndpoint {
	ip, node, port := decodeEndpoint(encodedEndpoint)
	return negtypes.NetworkEndpoint{IP: ip, Node: node, Port: port}
}

func getDefaultEndpointSlices() []*discovery.EndpointSlice {
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
				Name:      testServiceName + "-1",
				Namespace: testServiceNamespace,
			},
			AddressType: "IPv4",
			Endpoints: []discovery.Endpoint{
				{
					Addresses: []string{"10.100.1.1"},
					NodeName:  &instance1,
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
				{
					Addresses: []string{"10.100.2.1"},
					NodeName:  &instance2,
					TargetRef: &v1.ObjectReference{
						Namespace: testServiceNamespace,
						Name:      "pod3",
					},
				},
				{
					Addresses: []string{"10.100.3.1"},
					NodeName:  &instance3,
					TargetRef: &v1.ObjectReference{
						Namespace: testServiceNamespace,
						Name:      "pod4",
					},
				},
				{
					Addresses: []string{"10.100.1.3"},
					NodeName:  &instance1,
					TargetRef: &v1.ObjectReference{
						Namespace: testServiceNamespace,
						Name:      "pod5",
					},
					Conditions: discovery.EndpointConditions{Ready: &notReady},
				},
				{
					Addresses: []string{"10.100.1.4"},
					NodeName:  &instance1,
					TargetRef: &v1.ObjectReference{
						Namespace: testServiceNamespace,
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
				Name:      testServiceName + "-2",
				Namespace: testServiceNamespace,
			},
			AddressType: "IPv4",
			Endpoints: []discovery.Endpoint{
				{
					Addresses: []string{"10.100.2.2"},
					NodeName:  &instance2,
					TargetRef: &v1.ObjectReference{
						Namespace: testServiceNamespace,
						Name:      "pod7",
					},
				},
				{
					Addresses: []string{"10.100.4.1"},
					NodeName:  &instance4,
					TargetRef: &v1.ObjectReference{
						Namespace: testServiceNamespace,
						Name:      "pod8",
					},
				},
				{
					Addresses: []string{"10.100.4.3"},
					NodeName:  &instance4,
					TargetRef: &v1.ObjectReference{
						Namespace: testServiceNamespace,
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
				Name:      testServiceName + "-3",
				Namespace: testServiceNamespace,
			},
			AddressType: "IPv4",
			Endpoints: []discovery.Endpoint{
				{
					Addresses: []string{"10.100.3.2"},
					NodeName:  &instance3,
					TargetRef: &v1.ObjectReference{
						Namespace: testServiceNamespace,
						Name:      "pod10",
					},
				},
				{
					Addresses: []string{"10.100.4.2"},
					NodeName:  &instance4,
					TargetRef: &v1.ObjectReference{
						Namespace: testServiceNamespace,
						Name:      "pod11",
					},
				},
				{
					Addresses: []string{"10.100.4.4"},
					NodeName:  &instance4,
					TargetRef: &v1.ObjectReference{
						Namespace: testServiceNamespace,
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
	}
}

func getDefaultEndpoint() *v1.Endpoints {
	instance1 := negtypes.TestInstance1
	instance2 := negtypes.TestInstance2
	instance3 := negtypes.TestInstance3
	instance4 := negtypes.TestInstance4
	return &v1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testServiceName,
			Namespace: testServiceNamespace,
		},
		Subsets: []v1.EndpointSubset{
			{
				Addresses: []v1.EndpointAddress{
					{
						IP:       "10.100.1.1",
						NodeName: &instance1,
						TargetRef: &v1.ObjectReference{
							Namespace: testServiceNamespace,
							Name:      "pod1",
						},
					},
					{
						IP:       "10.100.1.2",
						NodeName: &instance1,
						TargetRef: &v1.ObjectReference{
							Namespace: testServiceNamespace,
							Name:      "pod2",
						},
					},
					{
						IP:       "10.100.2.1",
						NodeName: &instance2,
						TargetRef: &v1.ObjectReference{
							Namespace: testServiceNamespace,
							Name:      "pod3",
						},
					},
					{
						IP:       "10.100.3.1",
						NodeName: &instance3,
						TargetRef: &v1.ObjectReference{
							Namespace: testServiceNamespace,
							Name:      "pod4",
						},
					},
				},
				NotReadyAddresses: []v1.EndpointAddress{
					{
						IP:       "10.100.1.3",
						NodeName: &instance1,
						TargetRef: &v1.ObjectReference{
							Namespace: testServiceNamespace,
							Name:      "pod5",
						},
					},
					{
						IP:       "10.100.1.4",
						NodeName: &instance1,
						TargetRef: &v1.ObjectReference{
							Namespace: testServiceNamespace,
							Name:      "pod6",
						},
					},
				},
				Ports: []v1.EndpointPort{
					{
						Name:     "",
						Port:     int32(80),
						Protocol: v1.ProtocolTCP,
					},
				},
			},
			{
				Addresses: []v1.EndpointAddress{
					{
						IP:       "10.100.2.2",
						NodeName: &instance2,
						TargetRef: &v1.ObjectReference{
							Namespace: testServiceNamespace,
							Name:      "pod7",
						},
					},
					{
						IP:       "10.100.4.1",
						NodeName: &instance4,
						TargetRef: &v1.ObjectReference{
							Namespace: testServiceNamespace,
							Name:      "pod8",
						},
					},
				},
				NotReadyAddresses: []v1.EndpointAddress{
					{
						IP:       "10.100.4.3",
						NodeName: &instance4,
						TargetRef: &v1.ObjectReference{
							Namespace: testServiceNamespace,
							Name:      "pod9",
						},
					},
				},
				Ports: []v1.EndpointPort{
					{
						Name:     testNamedPort,
						Port:     int32(81),
						Protocol: v1.ProtocolTCP,
					},
				},
			},
			{
				Addresses: []v1.EndpointAddress{
					{
						IP:       "10.100.3.2",
						NodeName: &instance3,
						TargetRef: &v1.ObjectReference{
							Namespace: testServiceNamespace,
							Name:      "pod10",
						},
					},
					{
						IP:       "10.100.4.2",
						NodeName: &instance4,
						TargetRef: &v1.ObjectReference{
							Namespace: testServiceNamespace,
							Name:      "pod11",
						},
					},
				},
				NotReadyAddresses: []v1.EndpointAddress{
					{
						IP:       "10.100.4.4",
						NodeName: &instance4,
						TargetRef: &v1.ObjectReference{
							Namespace: testServiceNamespace,
							Name:      "pod12",
						},
					},
				},
				Ports: []v1.EndpointPort{
					{
						Name:     testNamedPort,
						Port:     int32(8081),
						Protocol: v1.ProtocolTCP,
					},
				},
			},
		},
	}
}
