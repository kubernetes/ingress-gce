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
	"strings"
	"testing"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"github.com/google/go-cmp/cmp"
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
	"k8s.io/ingress-gce/pkg/network"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/zonegetter"
	"k8s.io/klog/v2"
)

const defaultTestSubnetURL = "https://www.googleapis.com/compute/v1/projects/mock-project/regions/test-region/subnetworks/default"

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
		targetSet  map[string]sets.Set[string]
		currentSet map[string]sets.Set[string]
		addSet     map[string]sets.Set[string]
		removeSet  map[string]sets.Set[string]
	}{
		// unchanged
		{
			targetSet: map[string]sets.Set[string]{
				negtypes.TestZone1: sets.New("a", "b", "c"),
			},
			currentSet: map[string]sets.Set[string]{
				negtypes.TestZone1: sets.New("a", "b", "c"),
			},
			addSet:    map[string]sets.Set[string]{},
			removeSet: map[string]sets.Set[string]{},
		},
		// unchanged
		{
			targetSet:  map[string]sets.Set[string]{},
			currentSet: map[string]sets.Set[string]{},
			addSet:     map[string]sets.Set[string]{},
			removeSet:  map[string]sets.Set[string]{},
		},
		// add in one zone
		{
			targetSet: map[string]sets.Set[string]{
				negtypes.TestZone1: sets.New("a", "b", "c"),
			},
			currentSet: map[string]sets.Set[string]{},
			addSet: map[string]sets.Set[string]{
				negtypes.TestZone1: sets.New("a", "b", "c"),
			},
			removeSet: map[string]sets.Set[string]{},
		},
		// add in 2 zones
		{
			targetSet: map[string]sets.Set[string]{
				negtypes.TestZone1: sets.New("a", "b", "c"),
				negtypes.TestZone2: sets.New("e", "f", "g"),
			},
			currentSet: map[string]sets.Set[string]{},
			addSet: map[string]sets.Set[string]{
				negtypes.TestZone1: sets.New("a", "b", "c"),
				negtypes.TestZone2: sets.New("e", "f", "g"),
			},
			removeSet: map[string]sets.Set[string]{},
		},
		// remove in one zone
		{
			targetSet: map[string]sets.Set[string]{},
			currentSet: map[string]sets.Set[string]{
				negtypes.TestZone1: sets.New("a", "b", "c"),
			},
			addSet: map[string]sets.Set[string]{},
			removeSet: map[string]sets.Set[string]{
				negtypes.TestZone1: sets.New("a", "b", "c"),
			},
		},
		// remove in 2 zones
		{
			targetSet: map[string]sets.Set[string]{},
			currentSet: map[string]sets.Set[string]{
				negtypes.TestZone1: sets.New("a", "b", "c"),
				negtypes.TestZone2: sets.New("e", "f", "g"),
			},
			addSet: map[string]sets.Set[string]{},
			removeSet: map[string]sets.Set[string]{
				negtypes.TestZone1: sets.New("a", "b", "c"),
				negtypes.TestZone2: sets.New("e", "f", "g"),
			},
		},
		// add and delete in one zone
		{
			targetSet: map[string]sets.Set[string]{
				negtypes.TestZone1: sets.New("a", "b", "c"),
			},
			currentSet: map[string]sets.Set[string]{
				negtypes.TestZone1: sets.New("b", "c", "d"),
			},
			addSet: map[string]sets.Set[string]{
				negtypes.TestZone1: sets.New("a"),
			},
			removeSet: map[string]sets.Set[string]{
				negtypes.TestZone1: sets.New("d"),
			},
		},
		// add and delete in 2 zones
		{
			targetSet: map[string]sets.Set[string]{
				negtypes.TestZone1: sets.New("a", "b", "c"),
				negtypes.TestZone2: sets.New("a", "b", "c"),
			},
			currentSet: map[string]sets.Set[string]{
				negtypes.TestZone1: sets.New("b", "c", "d"),
				negtypes.TestZone2: sets.New("b", "c", "d"),
			},
			addSet: map[string]sets.Set[string]{
				negtypes.TestZone1: sets.New("a"),
				negtypes.TestZone2: sets.New("a"),
			},
			removeSet: map[string]sets.Set[string]{
				negtypes.TestZone1: sets.New("d"),
				negtypes.TestZone2: sets.New("d"),
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
		targetSet  map[negtypes.NEGLocation]negtypes.NetworkEndpointSet
		currentSet map[negtypes.NEGLocation]negtypes.NetworkEndpointSet
		addSet     map[negtypes.NEGLocation]negtypes.NetworkEndpointSet
		removeSet  map[negtypes.NEGLocation]negtypes.NetworkEndpointSet
	}{
		// unchanged
		{
			targetSet: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: negtypes.TestZone1}: negtypes.NewNetworkEndpointSet(genNetworkEndpoint("a"), genNetworkEndpoint("b"), genNetworkEndpoint("c")),
			},
			currentSet: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: negtypes.TestZone1}: negtypes.NewNetworkEndpointSet(genNetworkEndpoint("a"), genNetworkEndpoint("b"), genNetworkEndpoint("c")),
			},
			addSet:    map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{},
			removeSet: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{},
		},
		// unchanged
		{
			targetSet:  map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{},
			currentSet: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{},
			addSet:     map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{},
			removeSet:  map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{},
		},
		// add in one zone
		{
			targetSet: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: negtypes.TestZone1}: negtypes.NewNetworkEndpointSet(genNetworkEndpoint("a"), genNetworkEndpoint("b"), genNetworkEndpoint("c")),
			},
			currentSet: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{},
			addSet: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: negtypes.TestZone1}: negtypes.NewNetworkEndpointSet(genNetworkEndpoint("a"), genNetworkEndpoint("b"), genNetworkEndpoint("c")),
			},
			removeSet: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{},
		},
		// add in 2 zones
		{
			targetSet: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: negtypes.TestZone1}: negtypes.NewNetworkEndpointSet(genNetworkEndpoint("a"), genNetworkEndpoint("b"), genNetworkEndpoint("c")),
				{Zone: negtypes.TestZone2}: negtypes.NewNetworkEndpointSet(genNetworkEndpoint("e"), genNetworkEndpoint("f"), genNetworkEndpoint("g")),
			},
			currentSet: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{},
			addSet: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: negtypes.TestZone1}: negtypes.NewNetworkEndpointSet(genNetworkEndpoint("a"), genNetworkEndpoint("b"), genNetworkEndpoint("c")),
				{Zone: negtypes.TestZone2}: negtypes.NewNetworkEndpointSet(genNetworkEndpoint("e"), genNetworkEndpoint("f"), genNetworkEndpoint("g")),
			},
			removeSet: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{},
		},
		// remove in one zone
		{
			targetSet: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{},
			currentSet: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: negtypes.TestZone1}: negtypes.NewNetworkEndpointSet(genNetworkEndpoint("a"), genNetworkEndpoint("b"), genNetworkEndpoint("c")),
			},
			addSet: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{},
			removeSet: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: negtypes.TestZone1}: negtypes.NewNetworkEndpointSet(genNetworkEndpoint("a"), genNetworkEndpoint("b"), genNetworkEndpoint("c")),
			},
		},
		// remove in 2 zones
		{
			targetSet: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{},
			currentSet: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: negtypes.TestZone1}: negtypes.NewNetworkEndpointSet(genNetworkEndpoint("a"), genNetworkEndpoint("b"), genNetworkEndpoint("c")),
				{Zone: negtypes.TestZone2}: negtypes.NewNetworkEndpointSet(genNetworkEndpoint("e"), genNetworkEndpoint("f"), genNetworkEndpoint("g")),
			},
			addSet: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{},
			removeSet: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: negtypes.TestZone1}: negtypes.NewNetworkEndpointSet(genNetworkEndpoint("a"), genNetworkEndpoint("b"), genNetworkEndpoint("c")),
				{Zone: negtypes.TestZone2}: negtypes.NewNetworkEndpointSet(genNetworkEndpoint("e"), genNetworkEndpoint("f"), genNetworkEndpoint("g")),
			},
		},
		// add and delete in one zone
		{
			targetSet: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: negtypes.TestZone1}: negtypes.NewNetworkEndpointSet(genNetworkEndpoint("a"), genNetworkEndpoint("b"), genNetworkEndpoint("c")),
			},
			currentSet: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: negtypes.TestZone1}: negtypes.NewNetworkEndpointSet(genNetworkEndpoint("b"), genNetworkEndpoint("c"), genNetworkEndpoint("d")),
			},
			addSet: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: negtypes.TestZone1}: negtypes.NewNetworkEndpointSet(genNetworkEndpoint("a")),
			},
			removeSet: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: negtypes.TestZone1}: negtypes.NewNetworkEndpointSet(genNetworkEndpoint("d")),
			},
		},
		// add and delete in 2 zones
		{
			targetSet: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: negtypes.TestZone1}: negtypes.NewNetworkEndpointSet(genNetworkEndpoint("a"), genNetworkEndpoint("b"), genNetworkEndpoint("c")),
				{Zone: negtypes.TestZone2}: negtypes.NewNetworkEndpointSet(genNetworkEndpoint("a"), genNetworkEndpoint("b"), genNetworkEndpoint("c")),
			},
			currentSet: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: negtypes.TestZone1}: negtypes.NewNetworkEndpointSet(genNetworkEndpoint("b"), genNetworkEndpoint("c"), genNetworkEndpoint("d")),
				{Zone: negtypes.TestZone2}: negtypes.NewNetworkEndpointSet(genNetworkEndpoint("b"), genNetworkEndpoint("c"), genNetworkEndpoint("d")),
			},
			addSet: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: negtypes.TestZone1}: negtypes.NewNetworkEndpointSet(genNetworkEndpoint("a")),
				{Zone: negtypes.TestZone2}: negtypes.NewNetworkEndpointSet(genNetworkEndpoint("a")),
			},
			removeSet: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: negtypes.TestZone1}: negtypes.NewNetworkEndpointSet(genNetworkEndpoint("d")),
				{Zone: negtypes.TestZone2}: negtypes.NewNetworkEndpointSet(genNetworkEndpoint("d")),
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
		defaultNetwork       = network.NetworkInfo{IsDefault: true, K8sNetwork: "default"}
	)

	testCases := []struct {
		description         string
		negName             string
		enableNonGCPMode    bool
		networkEndpointType negtypes.NetworkEndpointType
		expectedSubnetwork  string
		expectedNetwork     string
		apiVersion          meta.Version
		networkInfo         network.NetworkInfo
	}{
		{
			description:         "Create NEG of type GCE_VM_IP_PORT",
			negName:             "gcp-neg",
			enableNonGCPMode:    false,
			networkEndpointType: negtypes.VmIpPortEndpointType,
			expectedSubnetwork:  testSubnetwork,
			apiVersion:          meta.VersionGA,
			networkInfo:         defaultNetwork,
		},
		{
			description:         "Create NEG of type NON_GCP_PRIVATE_IP_PORT",
			negName:             "non-gcp-neg",
			enableNonGCPMode:    true,
			networkEndpointType: negtypes.NonGCPPrivateEndpointType,
			expectedSubnetwork:  "",
			apiVersion:          meta.VersionGA,
			networkInfo:         defaultNetwork,
		},
		{
			description:         "Create NEG of type GCE_VM_IP",
			negName:             "gcp-ip-neg",
			enableNonGCPMode:    false,
			networkEndpointType: negtypes.VmIpEndpointType,
			expectedSubnetwork:  testSubnetwork,
			apiVersion:          meta.VersionAlpha,
			networkInfo:         defaultNetwork,
		},
		{
			description:         "Create NEG of type GCE_VM_IP_PORT with Neg CRD",
			negName:             "gcp-neg",
			enableNonGCPMode:    false,
			networkEndpointType: negtypes.VmIpPortEndpointType,
			expectedSubnetwork:  testSubnetwork,
			apiVersion:          meta.VersionGA,
			networkInfo:         defaultNetwork,
		},
		{
			description:         "Create NEG of type NON_GCP_PRIVATE_IP_PORT with Neg CRD",
			negName:             "non-gcp-neg",
			enableNonGCPMode:    true,
			networkEndpointType: negtypes.NonGCPPrivateEndpointType,
			expectedSubnetwork:  "",
			apiVersion:          meta.VersionGA,
			networkInfo:         defaultNetwork,
		},
		{
			description:         "Create NEG of type GCE_VM_IP with Neg CRD",
			negName:             "gcp-ip-neg",
			enableNonGCPMode:    false,
			networkEndpointType: negtypes.VmIpEndpointType,
			expectedSubnetwork:  testSubnetwork,
			apiVersion:          meta.VersionAlpha,
			networkInfo:         defaultNetwork,
		},
		{
			description:         "Create NEG of type GCE_VM_IP_PORT in alternate network",
			negName:             "gcp-neg",
			enableNonGCPMode:    false,
			networkEndpointType: negtypes.VmIpPortEndpointType,
			expectedNetwork:     cloud.ResourcePath("network", &meta.Key{Name: "other-network"}),
			expectedSubnetwork:  cloud.ResourcePath("subnetwork", &meta.Key{Zone: testZone, Name: "other-subnet"}),
			apiVersion:          meta.VersionGA,
			networkInfo: network.NetworkInfo{
				IsDefault:     false,
				K8sNetwork:    "other-network",
				NetworkURL:    cloud.ResourcePath("network", &meta.Key{Name: "other-network"}),
				SubnetworkURL: cloud.ResourcePath("subnetwork", &meta.Key{Zone: testZone, Name: "other-subnet"}),
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			fakeCloud := negtypes.NewFakeNetworkEndpointGroupCloud(testSubnetwork, testNetwork)
			if tc.networkInfo.IsDefault {
				tc.networkInfo.NetworkURL = fakeCloud.NetworkURL()
				tc.networkInfo.SubnetworkURL = fakeCloud.SubnetworkURL()
			}
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
				tc.networkInfo,
				klog.TODO(),
			)
			if err != nil {
				t.Errorf("unexpected error: %s", err)
			}

			neg, err := fakeCloud.GetNetworkEndpointGroup(tc.negName, testZone, tc.apiVersion, klog.TODO())
			if err != nil {
				t.Errorf("Failed to retrieve NEG %q: %v", tc.negName, err)
			}

			if neg.NetworkEndpointType != string(tc.networkEndpointType) {
				t.Errorf("Unexpected NetworkEndpointType, expecting %q but got %q", tc.networkEndpointType, neg.NetworkEndpointType)
			}

			if neg.Subnetwork != tc.expectedSubnetwork {
				t.Errorf("Unexpected Subnetwork, expecting %q but got %q", tc.expectedSubnetwork, neg.Subnetwork)
			}

			if tc.expectedNetwork == "" {
				tc.expectedNetwork = testNetwork
			}
			if neg.Network != tc.expectedNetwork {
				t.Errorf("Unexpected Network, expecting %q but got %q", tc.expectedNetwork, neg.Network)
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
				tc.networkInfo,
				klog.TODO(),
			)

			if err != nil {
				t.Errorf("Unexpected error when called with duplicated NEG: %v", err)
			}
		})
	}
}

func TestToZoneNetworkEndpointMap(t *testing.T) {
	t.Parallel()
	nodeInformer := zonegetter.FakeNodeInformer()
	zonegetter.PopulateFakeNodeInformer(nodeInformer, false)
	zoneGetter, err := zonegetter.NewFakeZoneGetter(nodeInformer, zonegetter.FakeNodeTopologyInformer(), defaultTestSubnetURL, false)
	if err != nil {
		t.Fatalf("failed to initialize zone getter: %v", err)
	}

	podLister := negtypes.NewTestContext().PodInformer.GetIndexer()
	testEndpointSlice := getDefaultEndpointSlices()
	addPodsToLister(podLister, testEndpointSlice)
	testCases := []struct {
		desc                       string
		portName                   string
		wantZoneNetworkEndpointMap map[negtypes.NEGLocation]negtypes.NetworkEndpointSet
		wantNetworkEndpointPodMap  negtypes.EndpointPodMap
		networkEndpointType        negtypes.NetworkEndpointType
		enableDualStackNEG         bool
	}{
		{
			desc:                       "target port does not exist",
			portName:                   "non-exists",
			wantZoneNetworkEndpointMap: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{},
			wantNetworkEndpointPodMap:  negtypes.EndpointPodMap{},
			networkEndpointType:        negtypes.VmIpPortEndpointType,
		},
		{
			desc:     "default service port name",
			portName: "",
			wantZoneNetworkEndpointMap: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet([]negtypes.NetworkEndpoint{
					{IP: "10.100.1.1", Node: "instance1", Port: "80"},
					{IP: "10.100.1.2", Node: "instance1", Port: "80"},
					{IP: "10.100.1.3", Node: "instance1", Port: "80"},
					{IP: "10.100.1.4", Node: "instance1", Port: "80"},
					{IP: "10.100.2.1", Node: "instance2", Port: "80"},
				}...),
				{Zone: negtypes.TestZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet([]negtypes.NetworkEndpoint{
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
			wantZoneNetworkEndpointMap: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet([]negtypes.NetworkEndpoint{
					{IP: "10.100.2.2", Node: "instance2", Port: "81"},
				}...),
				{Zone: negtypes.TestZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet([]negtypes.NetworkEndpoint{
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
			wantZoneNetworkEndpointMap: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet([]negtypes.NetworkEndpoint{
					{IP: "10.100.2.2", Node: "instance2", Port: "81"},
				}...),
				{Zone: negtypes.TestZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet([]negtypes.NetworkEndpoint{
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
			wantZoneNetworkEndpointMap: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet([]negtypes.NetworkEndpoint{
					{IP: "10.100.1.1", Port: "80"},
					{IP: "10.100.1.2", Port: "80"},
					{IP: "10.100.1.3", Port: "80"},
					{IP: "10.100.1.4", Port: "80"},
					{IP: "10.100.2.1", Port: "80"},
				}...),
				{Zone: negtypes.TestZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet([]negtypes.NetworkEndpoint{
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
			gotResult, err := toZoneNetworkEndpointMap(negtypes.EndpointsDataFromEndpointSlices(getDefaultEndpointSlices()), zoneGetter, podLister, tc.portName, tc.networkEndpointType, tc.enableDualStackNEG, false, klog.TODO())
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
	nodeInformer := zonegetter.FakeNodeInformer()
	zonegetter.PopulateFakeNodeInformer(nodeInformer, false)
	zoneGetter, err := zonegetter.NewFakeZoneGetter(nodeInformer, zonegetter.FakeNodeTopologyInformer(), defaultTestSubnetURL, false)
	if err != nil {
		t.Fatalf("failed to initialize zone getter: %v", err)
	}
	negCloud := negtypes.NewFakeNetworkEndpointGroupCloud("test-subnetwork", "test-network")
	defaultSubnetNegName := "test-neg-name"
	nonDefaultSubnetNegName := "non-default-neg-name"
	irrelevantNegName := "irrelevant"
	testIP1 := "1.2.3.4"
	testIP2 := "1.2.3.5"
	testIP3 := "1.2.3.6"
	testIP4 := "1.2.3.7"
	testIP5 := "1.2.3.8"
	testIP6 := "1.2.3.9"
	testIP7 := "1.2.3.10"
	testIP8 := "1.2.3.11"
	testIP9 := "1.2.3.12"
	testPort := int64(80)

	endpoint1 := negtypes.NetworkEndpoint{IP: testIP1, Node: negtypes.TestInstance1, Port: strconv.Itoa(int(testPort))}
	endpoint2 := negtypes.NetworkEndpoint{IP: testIP2, Node: negtypes.TestInstance2, Port: strconv.Itoa(int(testPort))}
	endpoint3 := negtypes.NetworkEndpoint{IP: testIP3, Node: negtypes.TestInstance3, Port: strconv.Itoa(int(testPort))}
	endpoint4 := negtypes.NetworkEndpoint{IP: testIP4, Node: negtypes.TestInstance4, Port: strconv.Itoa(int(testPort))}
	endpoint5 := negtypes.NetworkEndpoint{IP: testIP5, Node: negtypes.TestUnreadyInstance1, Port: strconv.Itoa(int(testPort))}
	endpoint6 := negtypes.NetworkEndpoint{IP: testIP6, Node: negtypes.TestUpgradeInstance1, Port: strconv.Itoa(int(testPort))}
	endpoint7 := negtypes.NetworkEndpoint{IP: testIP7, Node: negtypes.TestUpgradeInstance2, Port: strconv.Itoa(int(testPort))}
	endpoint8 := negtypes.NetworkEndpoint{IP: testIP8, Node: negtypes.TestInstance5, Port: strconv.Itoa(int(testPort))}
	endpoint9 := negtypes.NetworkEndpoint{IP: testIP9, Node: negtypes.TestInstance6, Port: strconv.Itoa(int(testPort))}

	mappingWithDefaultSubnetOnly := map[string]string{defaultTestSubnet: defaultSubnetNegName}
	mappingWithAdditionalSubnet := map[string]string{
		defaultTestSubnet:    defaultSubnetNegName,
		additionalTestSubnet: nonDefaultSubnetNegName,
	}
	testCases := []struct {
		desc                string
		mutate              func(cloud negtypes.NetworkEndpointGroupCloud)
		mode                negtypes.EndpointsCalculatorMode
		subnetToNegMapping  map[string]string
		expect              map[negtypes.NEGLocation]negtypes.NetworkEndpointSet
		expectAnnotationMap labels.EndpointPodLabelMap
		expectErr           bool
	}{
		{
			desc:               "neg does not exist",
			mutate:             func(cloud negtypes.NetworkEndpointGroupCloud) {},
			subnetToNegMapping: mappingWithDefaultSubnetOnly,
			expectErr:          true,
		},
		{
			desc: "neg only exists in one of the zone",
			mutate: func(cloud negtypes.NetworkEndpointGroupCloud) {
				cloud.CreateNetworkEndpointGroup(&composite.NetworkEndpointGroup{Name: testNegName, Version: meta.VersionGA}, negtypes.TestZone1, klog.TODO())
			},
			subnetToNegMapping: mappingWithDefaultSubnetOnly,
			expectErr:          true,
		},
		{
			desc: "neg only exists in one of the zone plus irrelevant negs",
			mutate: func(cloud negtypes.NetworkEndpointGroupCloud) {
				cloud.CreateNetworkEndpointGroup(&composite.NetworkEndpointGroup{Name: irrelevantNegName, Version: meta.VersionGA}, negtypes.TestZone2, klog.TODO())
			},
			subnetToNegMapping: mappingWithDefaultSubnetOnly,
			expectErr:          true,
		},
		{
			desc: "empty negs exists in all 3 zones",
			mutate: func(cloud negtypes.NetworkEndpointGroupCloud) {
				cloud.CreateNetworkEndpointGroup(&composite.NetworkEndpointGroup{Name: testNegName, Version: meta.VersionGA}, negtypes.TestZone2, klog.TODO())
				cloud.CreateNetworkEndpointGroup(&composite.NetworkEndpointGroup{Name: testNegName, Version: meta.VersionGA}, negtypes.TestZone4, klog.TODO())
			},
			subnetToNegMapping: mappingWithDefaultSubnetOnly,
			expect: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(),
				{Zone: negtypes.TestZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(),
				{Zone: negtypes.TestZone4, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(),
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
				}, meta.VersionGA, klog.TODO())
			},
			subnetToNegMapping: mappingWithDefaultSubnetOnly,
			expect: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(endpoint1),
				{Zone: negtypes.TestZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(),
				{Zone: negtypes.TestZone4, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(),
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
				}, meta.VersionGA, klog.TODO())
			},
			subnetToNegMapping: mappingWithDefaultSubnetOnly,
			expect: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(
					endpoint1,
					endpoint2,
				),
				{Zone: negtypes.TestZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(),
				{Zone: negtypes.TestZone4, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(),
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
				}, meta.VersionGA, klog.TODO())
			},
			subnetToNegMapping: mappingWithDefaultSubnetOnly,
			expect: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(
					endpoint1,
					endpoint2,
				),
				{Zone: negtypes.TestZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(
					endpoint3,
					endpoint4,
				),
				{Zone: negtypes.TestZone4, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(),
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
			desc: "no changes to negs in the default subnet, negs in non-default subnet are newly created without any endpoints",
			mutate: func(cloud negtypes.NetworkEndpointGroupCloud) {
				cloud.CreateNetworkEndpointGroup(&composite.NetworkEndpointGroup{Name: nonDefaultSubnetNegName, Version: meta.VersionGA}, negtypes.TestZone1, klog.TODO())
				cloud.CreateNetworkEndpointGroup(&composite.NetworkEndpointGroup{Name: nonDefaultSubnetNegName, Version: meta.VersionGA}, negtypes.TestZone2, klog.TODO())
				cloud.CreateNetworkEndpointGroup(&composite.NetworkEndpointGroup{Name: nonDefaultSubnetNegName, Version: meta.VersionGA}, negtypes.TestZone4, klog.TODO())
			},
			subnetToNegMapping: mappingWithAdditionalSubnet,
			expect: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(
					endpoint1,
					endpoint2,
				),
				{Zone: negtypes.TestZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(
					endpoint3,
					endpoint4,
				),
				{Zone: negtypes.TestZone4, Subnet: defaultTestSubnet}:    negtypes.NewNetworkEndpointSet(),
				{Zone: negtypes.TestZone1, Subnet: additionalTestSubnet}: negtypes.NewNetworkEndpointSet(),
				{Zone: negtypes.TestZone2, Subnet: additionalTestSubnet}: negtypes.NewNetworkEndpointSet(),
				{Zone: negtypes.TestZone4, Subnet: additionalTestSubnet}: negtypes.NewNetworkEndpointSet(),
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
			desc: "no changes to negs in the default subnet, negs in non-default subnet have newly added endpoints",
			mutate: func(cloud negtypes.NetworkEndpointGroupCloud) {
				cloud.AttachNetworkEndpoints(nonDefaultSubnetNegName, negtypes.TestZone1, []*composite.NetworkEndpoint{
					{
						Instance:  negtypes.TestInstance5,
						IpAddress: testIP8,
						Port:      testPort,
						Annotations: map[string]string{
							"foo": "bar",
						},
					},
				}, meta.VersionGA, klog.TODO())
				cloud.AttachNetworkEndpoints(nonDefaultSubnetNegName, negtypes.TestZone2, []*composite.NetworkEndpoint{
					{
						Instance:  negtypes.TestInstance6,
						IpAddress: testIP9,
						Port:      testPort,
						Annotations: map[string]string{
							"foo": "bar",
						},
					},
				}, meta.VersionGA, klog.TODO())
			},
			subnetToNegMapping: mappingWithAdditionalSubnet,
			expect: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(
					endpoint1,
					endpoint2,
				),
				{Zone: negtypes.TestZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(
					endpoint3,
					endpoint4,
				),
				{Zone: negtypes.TestZone4, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(),
				{Zone: negtypes.TestZone1, Subnet: additionalTestSubnet}: negtypes.NewNetworkEndpointSet(
					endpoint8,
				),
				{Zone: negtypes.TestZone2, Subnet: additionalTestSubnet}: negtypes.NewNetworkEndpointSet(
					endpoint9,
				),
				{Zone: negtypes.TestZone4, Subnet: additionalTestSubnet}: negtypes.NewNetworkEndpointSet(),
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
				endpoint8: labels.PodLabelMap{
					"foo": "bar",
				},
				endpoint9: labels.PodLabelMap{
					"foo": "bar",
				},
			},
			expectErr: false,
		},
		{
			desc: "negs in the addtional subnet are deleted, and 2 negs in the default subnet with multiple endpoints",
			mutate: func(cloud negtypes.NetworkEndpointGroupCloud) {
				cloud.DeleteNetworkEndpointGroup(nonDefaultSubnetNegName, negtypes.TestZone1, meta.VersionGA, klog.TODO())
				cloud.DeleteNetworkEndpointGroup(nonDefaultSubnetNegName, negtypes.TestZone2, meta.VersionGA, klog.TODO())
				cloud.DeleteNetworkEndpointGroup(nonDefaultSubnetNegName, negtypes.TestZone4, meta.VersionGA, klog.TODO())
			},
			subnetToNegMapping: mappingWithDefaultSubnetOnly,
			expect: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(
					endpoint1,
					endpoint2,
				),
				{Zone: negtypes.TestZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(
					endpoint3,
					endpoint4,
				),
				{Zone: negtypes.TestZone4, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(),
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
				}, meta.VersionGA, klog.TODO())
			},
			subnetToNegMapping: mappingWithDefaultSubnetOnly,
			expect: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(
					endpoint1,
					endpoint2,
				),
				{Zone: negtypes.TestZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(
					endpoint3,
					endpoint4,
				),
				{Zone: negtypes.TestZone4, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(
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
				}, meta.VersionGA, klog.TODO())
			},
			subnetToNegMapping: mappingWithDefaultSubnetOnly,
			expect: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(
					endpoint1,
					endpoint2,
				),
				{Zone: negtypes.TestZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(
					endpoint3,
					endpoint4,
				),
				{Zone: negtypes.TestZone4, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(
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
				}, meta.VersionGA, klog.TODO())
			},
			// set mode to L4 since this scenario applies more to VM_IP NEGs.
			mode:               negtypes.L4LocalMode,
			subnetToNegMapping: mappingWithDefaultSubnetOnly,
			expect: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				// NEGs in zone1, zone2 and zone4 are created from previous test case.
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(
					endpoint1,
					endpoint2,
				),
				{Zone: negtypes.TestZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(
					endpoint3,
					endpoint4,
				),
				{Zone: negtypes.TestZone3, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(
					endpoint5,
				),
				{Zone: negtypes.TestZone4, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(
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
				cloud.DeleteNetworkEndpointGroup(testNegName, negtypes.TestZone2, meta.VersionGA, klog.TODO())
			},
			subnetToNegMapping: mappingWithDefaultSubnetOnly,
			expectErr:          true,
		},
	}

	for _, tc := range testCases {
		tc.mutate(negCloud)
		// tc.mode of "" will result in the default node predicate being selected, which is ok for this test.
		endpointSets, annotationMap, err := retrieveExistingZoneNetworkEndpointMap(tc.subnetToNegMapping, zoneGetter, negCloud, meta.VersionGA, tc.mode, false, klog.TODO())

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

			out, err := makeEndpointBatch(endpointSet, negType, endpointPodLabelMap, klog.TODO())

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
		networkInfo          = network.NetworkInfo{
			NetworkURL:    testNetwork,
			SubnetworkURL: testSubnetwork,
		}
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
		networkInfo,
		klog.TODO(),
	)
	if err != nil {
		t.Errorf("Errored while ensuring network endpoint groups: %s", err)
	}

	neg, err := fakeCloud.GetNetworkEndpointGroup(negName, testZone, apiVersion, klog.TODO())
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
		networkInfo,
		klog.TODO(),
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
		networkInfo          = network.NetworkInfo{
			NetworkURL:    testNetwork,
			SubnetworkURL: testSubnetwork,
		}
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
			networkInfo,
			klog.TODO(),
		)
		if err != nil {
			t.Errorf("Errored while ensuring network endpoint groups: %s", err)
		}

		neg, err := fakeCloud.GetNetworkEndpointGroup(negName, testZone, apiVersion, klog.TODO())
		if err != nil {
			t.Errorf("Failed to retrieve NEG %q: %v", negName, err)
		}

		if neg == nil {
			t.Errorf("Failed to find neg")
		}

		var subnetURL string
		if networkEndpointType != negtypes.NonGCPPrivateEndpointType {
			subnetURL = testSubnetwork
		}
		expectedNegObj := negv1beta1.NegObjectReference{
			Id:                  fmt.Sprint(neg.Id),
			SelfLink:            neg.SelfLink,
			NetworkEndpointType: negv1beta1.NetworkEndpointType(networkEndpointType),
		}
		if flags.F.EnableMultiSubnetClusterPhase1 {
			expectedNegObj.State = negv1beta1.ActiveState
			expectedNegObj.SubnetURL = subnetURL
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
			networkInfo,
			klog.TODO(),
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
		networkInfo          = network.NetworkInfo{
			NetworkURL:    testNetwork,
			SubnetworkURL: testSubnetwork,
		}
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
		}, testZone, klog.TODO())

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
			networkInfo,
			klog.TODO(),
		)
		if !tc.expectError && err != nil {
			t.Errorf("TestCase: %s, Errored while ensuring network endpoint groups: %s", tc.desc, err)
		} else if tc.expectError && err == nil {
			t.Errorf("TestCase: %s, Expected error when ensure network endpoint groups", tc.desc)
		}

		neg, err := fakeCloud.GetNetworkEndpointGroup(negName, testZone, apiVersion, klog.TODO())
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

	nodeInformer := zonegetter.FakeNodeInformer()
	zonegetter.PopulateFakeNodeInformer(nodeInformer, false)
	fakeZoneGetter, err := zonegetter.NewFakeZoneGetter(nodeInformer, zonegetter.FakeNodeTopologyInformer(), defaultTestSubnetURL, false)
	if err != nil {
		t.Fatalf("failed to initialize zone getter: %v", err)
	}
	testContext := negtypes.NewTestContext()
	podLister := testContext.PodInformer.GetIndexer()
	addPodsToLister(podLister, getDefaultEndpointSlices())

	instance1 := negtypes.TestInstance1

	emptyNamedPort := ""
	port80 := int32(80)
	protocolTCP := v1.ProtocolTCP
	hostNetworkPodIP := "10.10.0.1"
	hostNetworkPodName := "test-pod-host-network"
	testLabels := map[string]string{
		"run": "foo",
	}
	podLister.Add(&v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testServiceNamespace,
			Name:      hostNetworkPodName,
			Labels:    testLabels,
		},
		Spec: v1.PodSpec{
			NodeName:    instance1,
			HostNetwork: true,
		},
		Status: v1.PodStatus{
			Phase:  v1.PodRunning,
			PodIP:  hostNetworkPodIP,
			PodIPs: []v1.PodIP{{IP: hostNetworkPodIP}},
		},
	})

	nodeLister := testContext.NodeInformer.GetIndexer()
	for i := 1; i <= 4; i++ {
		nodeLister.Add(&v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf("instance%v", i),
			},
			Spec: v1.NodeSpec{
				PodCIDR:  fmt.Sprintf("10.100.%v.0/24", i),
				PodCIDRs: []string{fmt.Sprintf("200%v:db8::/48", i), fmt.Sprintf("10.100.%v.0/24", i)},
			},
			Status: v1.NodeStatus{
				Addresses: []v1.NodeAddress{
					{Type: v1.NodeInternalIP, Address: fmt.Sprintf("10.10.0.%v", i)},
				},
			},
		})
	}

	serviceLister := testContext.ServiceInformer.GetIndexer()
	serviceLister.Add(&v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testServiceNamespace,
			Name:      testServiceName,
		},
		Spec: v1.ServiceSpec{
			Selector: testLabels,
		},
	})

	testNonExistPort := "non-exists"
	testEmptyNamedPort := ""
	testNamedPort := "named-Port"
	testCases := []struct {
		desc                string
		testEndpointSlices  []*discovery.EndpointSlice
		portName            string
		expectedEndpointMap map[negtypes.NEGLocation]negtypes.NetworkEndpointSet
		expectedPodMap      negtypes.EndpointPodMap
		networkEndpointType negtypes.NetworkEndpointType
	}{
		{
			desc:                "non exist target port",
			testEndpointSlices:  getDefaultEndpointSlices(),
			portName:            testNonExistPort,
			expectedEndpointMap: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{},
			expectedPodMap:      negtypes.EndpointPodMap{},
			networkEndpointType: negtypes.VmIpPortEndpointType,
		},
		{
			desc:               "empty named port",
			testEndpointSlices: getDefaultEndpointSlices(),
			portName:           testEmptyNamedPort,
			expectedEndpointMap: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(
					networkEndpointFromEncodedEndpoint("10.100.1.1||instance1||80"),
					networkEndpointFromEncodedEndpoint("10.100.1.2||instance1||80"),
					networkEndpointFromEncodedEndpoint("10.100.2.1||instance2||80"),
					networkEndpointFromEncodedEndpoint("10.100.1.3||instance1||80"),
					networkEndpointFromEncodedEndpoint("10.100.1.4||instance1||80")),
				{Zone: negtypes.TestZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(
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
			expectedEndpointMap: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(
					networkEndpointFromEncodedEndpoint("10.100.2.2||instance2||81")),
				{Zone: negtypes.TestZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(
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
			expectedEndpointMap: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(
					networkEndpointFromEncodedEndpoint("10.100.1.1||||80"),
					networkEndpointFromEncodedEndpoint("10.100.1.2||||80"),
					networkEndpointFromEncodedEndpoint("10.100.2.1||||80"),
					networkEndpointFromEncodedEndpoint("10.100.1.3||||80"),
					networkEndpointFromEncodedEndpoint("10.100.1.4||||80")),
				{Zone: negtypes.TestZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(
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
		{
			desc: "hostNetwork pods",
			testEndpointSlices: []*discovery.EndpointSlice{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testServiceName + "-1",
						Namespace: testServiceNamespace,
						Labels: map[string]string{
							discovery.LabelServiceName: testServiceName,
							discovery.LabelManagedBy:   managedByEPSControllerValue,
						},
					},
					AddressType: "IPv4",
					Endpoints: []discovery.Endpoint{
						{
							Addresses: []string{hostNetworkPodIP}, // Node IP for instance1 is 10.10.0.1, its PodCIDR range is 10.100.1.0/24
							NodeName:  &instance1,
							TargetRef: &v1.ObjectReference{
								Namespace: testServiceNamespace,
								Name:      hostNetworkPodName,
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
			portName: testEmptyNamedPort,
			expectedEndpointMap: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(
					networkEndpointFromEncodedEndpoint("10.10.0.1||instance1||80"),
				),
			},
			expectedPodMap: negtypes.EndpointPodMap{
				networkEndpointFromEncodedEndpoint("10.10.0.1||instance1||80"): types.NamespacedName{Namespace: testServiceNamespace, Name: hostNetworkPodName},
			},
			networkEndpointType: negtypes.VmIpPortEndpointType,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			result := toZoneNetworkEndpointMapDegradedMode(negtypes.EndpointsDataFromEndpointSlices(tc.testEndpointSlices), fakeZoneGetter, podLister, nodeLister, serviceLister, tc.portName, tc.networkEndpointType, false, false, klog.TODO())
			if !reflect.DeepEqual(result.NetworkEndpointSet, tc.expectedEndpointMap) {
				t.Errorf("degraded mode endpoint set is not calculated correctly:\ngot %+v,\n expected %+v", result.NetworkEndpointSet, tc.expectedEndpointMap)
			}
			if !reflect.DeepEqual(result.EndpointPodMap, tc.expectedPodMap) {
				t.Errorf("degraded mode endpoint map is not calculated correctly:\ngot %+v,\n expected %+v", result.EndpointPodMap, tc.expectedPodMap)
			}
		})
	}
}

// TestValidateEndpointFields validates if toZoneNetworkEndpointMap
// and toZoneNetworkEndpointMapDegradedMode return the correct endpoints and
// correct type of error with the supplied invalid endpoint information.
func TestValidateEndpointFields(t *testing.T) {
	emptyNamedPort := ""
	emptyNodeName := ""
	port80 := int32(80)
	protocolTCP := v1.ProtocolTCP
	instance1 := negtypes.TestInstance1
	instance2 := negtypes.TestInstance2
	instance3 := negtypes.TestInstance3
	instance4 := negtypes.TestInstance4
	emptyZoneInstance := negtypes.TestEmptyZoneInstance
	emptyZonePod := "empty-zone-pod" // This should map to emptyZoneInstance.
	notExistInstance := negtypes.TestNotExistInstance

	testContext := negtypes.NewTestContext()
	podLister := testContext.PodInformer.GetIndexer()
	addPodsToLister(podLister, getDefaultEndpointSlices())
	nodeLister := testContext.NodeInformer.GetIndexer()
	zonegetter.PopulateFakeNodeInformer(testContext.NodeInformer, false)
	fakeZoneGetter, err := zonegetter.NewFakeZoneGetter(testContext.NodeInformer, testContext.NodeTopologyInformer, defaultTestSubnetURL, false)
	if err != nil {
		t.Fatalf("failed to initialize zone getter: %v", err)
	}

	// Add the pod that corresponds to empty zone instance.
	podLister.Add(&v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testServiceNamespace,
			Name:      emptyZonePod,
			Labels: map[string]string{
				discovery.LabelServiceName: testServiceName,
				discovery.LabelManagedBy:   managedByEPSControllerValue,
			},
		},
		Spec: v1.PodSpec{
			NodeName: emptyZoneInstance,
		},
		Status: v1.PodStatus{
			Phase: v1.PodRunning,
			PodIP: "10.100.5.1",
			PodIPs: []v1.PodIP{
				{IP: "10.100.5.1"},
			},
		},
	})

	testLabels := map[string]string{
		"run": "foo",
	}
	serviceLister := testContext.ServiceInformer.GetIndexer()
	serviceLister.Add(&v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testServiceNamespace,
			Name:      testServiceName,
		},
		Spec: v1.ServiceSpec{
			Selector: testLabels,
		},
	})

	// endpointMap and podMap contain all correct endpoints.
	endpointMap := map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
		{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(
			negtypes.NetworkEndpoint{IP: "10.100.1.1", Node: instance1, Port: "80"},
			negtypes.NetworkEndpoint{IP: "10.100.1.2", Node: instance1, Port: "80"},
		),
	}
	podMap := negtypes.EndpointPodMap{
		negtypes.NetworkEndpoint{IP: "10.100.1.1", Node: instance1, Port: "80"}: types.NamespacedName{Namespace: testServiceNamespace, Name: "pod1"},
		negtypes.NetworkEndpoint{IP: "10.100.1.2", Node: instance1, Port: "80"}: types.NamespacedName{Namespace: testServiceNamespace, Name: "pod2"},
	}

	// endpointMapExcluded and podMapExcluded only includes valid endpoints.
	// In normal mode, we exclude the specific endpoint for cases where an endpoint's pod information is invalid, including:
	//	1. endpoint has an empty targetRef
	//  2. endpoint's corresponding pod does not exist
	//  3. endpoint corresponds to an object that fails pod type assertion
	//
	// In degraded mode, we should exclude the invalid endpoint for non-coverable cases(pod invalid or empty zone).
	// We always inject to first endpoint, so the result only contain the second endpoint.
	endpointMapExcluded := map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
		{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(
			negtypes.NetworkEndpoint{IP: "10.100.1.2", Node: instance1, Port: "80"},
		),
	}
	podMapExcluded := negtypes.EndpointPodMap{
		negtypes.NetworkEndpoint{IP: "10.100.1.2", Node: instance1, Port: "80"}: types.NamespacedName{Namespace: testServiceNamespace, Name: "pod2"},
	}

	testCases := []struct {
		desc                            string
		testEndpointSlices              []*discovery.EndpointSlice
		expectedEndpointMap             map[negtypes.NEGLocation]negtypes.NetworkEndpointSet
		expectedPodMap                  negtypes.EndpointPodMap
		expectErr                       error
		expectErrorState                bool
		expectedEndpointMapDegradedMode map[negtypes.NEGLocation]negtypes.NetworkEndpointSet
		expectedPodMapDegradedMode      negtypes.EndpointPodMap
	}{
		{
			desc: "endpoints with no errors, both calculations should have all endpoints",
			testEndpointSlices: []*discovery.EndpointSlice{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testServiceName,
						Namespace: testServiceNamespace,
						Labels: map[string]string{
							discovery.LabelServiceName: testServiceName,
						},
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
			expectedEndpointMap:             endpointMap,
			expectedPodMap:                  podMap,
			expectErr:                       nil,
			expectErrorState:                false,
			expectedEndpointMapDegradedMode: endpointMap,
			expectedPodMapDegradedMode:      podMap,
		},
		{
			desc: "include one endpoint that has missing nodeName, error will be raised in normal mode, nodeName should be filled in degraded mode",
			testEndpointSlices: []*discovery.EndpointSlice{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testServiceName,
						Namespace: testServiceNamespace,
						Labels: map[string]string{
							discovery.LabelServiceName: testServiceName,
							discovery.LabelManagedBy:   managedByEPSControllerValue,
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
			expectedEndpointMap:             nil,
			expectedPodMap:                  nil,
			expectErr:                       negtypes.ErrEPNodeMissing,
			expectErrorState:                true,
			expectedEndpointMapDegradedMode: endpointMap,
			expectedPodMapDegradedMode:      podMap,
		},
		{
			desc: "include one endpoint that has empty nodeName, error will be raised in normal mode, nodeName should be filled in degraded mode",
			testEndpointSlices: []*discovery.EndpointSlice{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testServiceName,
						Namespace: testServiceNamespace,
						Labels: map[string]string{
							discovery.LabelServiceName: testServiceName,
							discovery.LabelManagedBy:   managedByEPSControllerValue,
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
			expectedEndpointMap:             nil,
			expectedPodMap:                  nil,
			expectErr:                       negtypes.ErrEPNodeMissing,
			expectErrorState:                true,
			expectedEndpointMapDegradedMode: endpointMap,
			expectedPodMapDegradedMode:      podMap,
		},
		{
			desc: "include one endpoint that has missing pod, endpoint should be excluded in both calculations",
			testEndpointSlices: []*discovery.EndpointSlice{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testServiceName,
						Namespace: testServiceNamespace,
						Labels: map[string]string{
							discovery.LabelServiceName: testServiceName,
						},
					},
					AddressType: "IPv4",
					Endpoints: []discovery.Endpoint{
						{
							Addresses: []string{"10.100.1.1"},
							NodeName:  &instance1,
							TargetRef: nil,
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
			expectedEndpointMap:             endpointMapExcluded,
			expectedPodMap:                  podMapExcluded,
			expectErr:                       nil,
			expectErrorState:                false,
			expectedEndpointMapDegradedMode: endpointMapExcluded,
			expectedPodMapDegradedMode:      podMapExcluded,
		},
		{
			desc: "include one endpoint that does not correspond to an existing pod, endpoint should be excluded in both calculations",
			testEndpointSlices: []*discovery.EndpointSlice{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testServiceName,
						Namespace: testServiceNamespace,
						Labels: map[string]string{
							discovery.LabelServiceName: testServiceName,
						},
					},
					AddressType: "IPv4",
					Endpoints: []discovery.Endpoint{
						{
							Addresses: []string{"10.100.1.1"},
							NodeName:  &instance1,
							TargetRef: &v1.ObjectReference{
								Namespace: testServiceNamespace,
								Name:      "foo", // this is a non-existing pod
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
			expectedEndpointMap:             endpointMapExcluded,
			expectedPodMap:                  podMapExcluded,
			expectErr:                       nil,
			expectErrorState:                false,
			expectedEndpointMapDegradedMode: endpointMapExcluded,
			expectedPodMapDegradedMode:      podMapExcluded,
		},
		{
			desc: "include one endpoint that does not correspond to an existing node, error will be raised in normal mode, instance name will be deduced from pod and corrected in degraded mode",
			testEndpointSlices: []*discovery.EndpointSlice{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testServiceName,
						Namespace: testServiceNamespace,
						Labels: map[string]string{
							discovery.LabelServiceName: testServiceName,
						},
					},
					AddressType: "IPv4",
					Endpoints: []discovery.Endpoint{
						{
							Addresses: []string{"10.100.1.1"},
							NodeName:  &notExistInstance,
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
			expectedEndpointMap:             nil,
			expectedPodMap:                  nil,
			expectErr:                       negtypes.ErrEPNodeNotFound,
			expectErrorState:                true,
			expectedEndpointMapDegradedMode: endpointMap,
			expectedPodMapDegradedMode:      podMap,
		},
		{
			desc: "include one endpoint that corresponds to an empty zone, error will be raised in normal mode, endpoint should be excluded in degraded mode",
			testEndpointSlices: []*discovery.EndpointSlice{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testServiceName,
						Namespace: testServiceNamespace,
						Labels: map[string]string{
							discovery.LabelServiceName: testServiceName,
						},
					},
					AddressType: "IPv4",
					Endpoints: []discovery.Endpoint{
						{
							Addresses: []string{"10.100.1.1"},
							NodeName:  &emptyZoneInstance,
							TargetRef: &v1.ObjectReference{
								Namespace: testServiceNamespace,
								Name:      emptyZonePod,
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
			expectedEndpointMap:             nil,
			expectedPodMap:                  nil,
			expectErr:                       negtypes.ErrEPZoneMissing,
			expectErrorState:                true,
			expectedEndpointMapDegradedMode: endpointMapExcluded,
			expectedPodMapDegradedMode:      podMapExcluded,
		},
		{
			// Endpoint IP and pod IP check is only performed during degraded
			// mode, so in normal calculation, endpoints with not matching will
			// be included.
			desc: "single stack ipv4 endpoints, contains one endpoint with IPv4 address not matching to its pod, normal mode will include the invalid endpoint, endpoint should be removed in degraded mode",
			testEndpointSlices: []*discovery.EndpointSlice{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testServiceName,
						Namespace: testServiceNamespace,
						Labels: map[string]string{
							discovery.LabelServiceName: testServiceName,
							discovery.LabelManagedBy:   managedByEPSControllerValue,
						},
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
							Addresses: []string{"10.100.2.2"}, // the IPv4 address of this pod is 10.100.2.1
							NodeName:  &instance2,
							TargetRef: &v1.ObjectReference{
								Namespace: testServiceNamespace,
								Name:      "pod3",
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
			expectedEndpointMap: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(
					negtypes.NetworkEndpoint{IP: "10.100.1.1", Node: instance1, Port: "80"},
					negtypes.NetworkEndpoint{IP: "10.100.1.2", Node: instance1, Port: "80"},
					negtypes.NetworkEndpoint{IP: "10.100.2.2", Node: instance2, Port: "80"},
				),
			},
			expectedPodMap: negtypes.EndpointPodMap{
				negtypes.NetworkEndpoint{IP: "10.100.1.1", Node: instance1, Port: "80"}: types.NamespacedName{Namespace: testServiceNamespace, Name: "pod1"},
				negtypes.NetworkEndpoint{IP: "10.100.1.2", Node: instance1, Port: "80"}: types.NamespacedName{Namespace: testServiceNamespace, Name: "pod2"},
				negtypes.NetworkEndpoint{IP: "10.100.2.2", Node: instance2, Port: "80"}: types.NamespacedName{Namespace: testServiceNamespace, Name: "pod3"},
			},
			expectErr:                       nil,
			expectErrorState:                false,
			expectedEndpointMapDegradedMode: endpointMap,
			expectedPodMapDegradedMode:      podMap,
		},
		{
			// Endpoint IP and pod IP check is only performed during degraded
			// mode, so in normal calculation, endpoints with not matching will
			// be included.
			desc: "dual stack endpoints, contains one endpoint with IPv6 address not matching to its pod, normal mode will include the invalid endpoint, endpoint should be removed in degraded mode",
			testEndpointSlices: []*discovery.EndpointSlice{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testServiceName,
						Namespace: testServiceNamespace,
						Labels: map[string]string{
							discovery.LabelServiceName: testServiceName,
						},
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
						Name:      testServiceName,
						Namespace: testServiceNamespace,
						Labels: map[string]string{
							discovery.LabelServiceName: testServiceName,
						},
					},
					AddressType: "IPv6",
					Endpoints: []discovery.Endpoint{
						{
							Addresses: []string{"a:b::1"},
							NodeName:  &instance3,
							TargetRef: &v1.ObjectReference{
								Namespace: testServiceNamespace,
								Name:      "pod10",
							},
						},
						{
							Addresses: []string{"a:b::2"},
							NodeName:  &instance4,
							TargetRef: &v1.ObjectReference{
								Namespace: testServiceNamespace,
								Name:      "pod11",
							},
						},
						{
							Addresses: []string{"a:b::4"}, // the IPv6 address of this pod is a:b::3
							NodeName:  &instance4,
							TargetRef: &v1.ObjectReference{
								Namespace: testServiceNamespace,
								Name:      "pod12",
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
			expectedEndpointMap: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: negtypes.TestZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(
					negtypes.NetworkEndpoint{IP: "10.100.3.2", IPv6: "a:b::1", Node: instance3, Port: "80"},
					negtypes.NetworkEndpoint{IP: "10.100.4.2", IPv6: "a:b::2", Node: instance4, Port: "80"},
					negtypes.NetworkEndpoint{IP: "10.100.4.4", IPv6: "a:b::4", Node: instance4, Port: "80"},
				),
			},
			expectedPodMap: negtypes.EndpointPodMap{
				negtypes.NetworkEndpoint{IP: "10.100.3.2", IPv6: "a:b::1", Node: instance3, Port: "80"}: types.NamespacedName{Namespace: testServiceNamespace, Name: "pod10"},
				negtypes.NetworkEndpoint{IP: "10.100.4.2", IPv6: "a:b::2", Node: instance4, Port: "80"}: types.NamespacedName{Namespace: testServiceNamespace, Name: "pod11"},
				negtypes.NetworkEndpoint{IP: "10.100.4.4", IPv6: "a:b::4", Node: instance4, Port: "80"}: types.NamespacedName{Namespace: testServiceNamespace, Name: "pod12"},
			},
			expectErr:        nil,
			expectErrorState: false,
			expectedEndpointMapDegradedMode: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: negtypes.TestZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(
					negtypes.NetworkEndpoint{IP: "10.100.3.2", IPv6: "a:b::1", Node: instance3, Port: "80"},
					negtypes.NetworkEndpoint{IP: "10.100.4.2", IPv6: "a:b::2", Node: instance4, Port: "80"},
				),
			},
			expectedPodMapDegradedMode: negtypes.EndpointPodMap{
				negtypes.NetworkEndpoint{IP: "10.100.3.2", IPv6: "a:b::1", Node: instance3, Port: "80"}: types.NamespacedName{Namespace: testServiceNamespace, Name: "pod10"},
				negtypes.NetworkEndpoint{IP: "10.100.4.2", IPv6: "a:b::2", Node: instance4, Port: "80"}: types.NamespacedName{Namespace: testServiceNamespace, Name: "pod11"},
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			for _, enableMultiSubnetCluster := range []bool{true, false} {
				result, err := toZoneNetworkEndpointMap(negtypes.EndpointsDataFromEndpointSlices(tc.testEndpointSlices), fakeZoneGetter, podLister, emptyNamedPort, negtypes.VmIpPortEndpointType, true, enableMultiSubnetCluster, klog.TODO())
				if !errors.Is(err, tc.expectErr) {
					t.Errorf("normal mode with enableMultiSubnetCluster = %v, calculation got error %v, expected %v", err, enableMultiSubnetCluster, tc.expectErr)
				}
				if !reflect.DeepEqual(result.NetworkEndpointSet, tc.expectedEndpointMap) {
					t.Errorf("normal mode with enableMultiSubnetCluster = %v, endpoint set is not calculated correctly:\ngot %+v,\n expected %+v", enableMultiSubnetCluster, result.NetworkEndpointSet, tc.expectedEndpointMapDegradedMode)
				}
				if !reflect.DeepEqual(result.EndpointPodMap, tc.expectedPodMap) {
					t.Errorf("normal mode with enableMultiSubnetCluster = %v, endpoint map is not calculated correctly:\ngot %+v,\n expected %+v", enableMultiSubnetCluster, result.EndpointPodMap, tc.expectedPodMapDegradedMode)
				}
				if syncErr := negtypes.ClassifyError(err); syncErr.IsErrorState != tc.expectErrorState {
					t.Errorf("normal mode with enableMultiSubnetCluster = %v, got isErrorState=%v,\n expected %v", enableMultiSubnetCluster, syncErr.IsErrorState, tc.expectErrorState)
				}

				resultDegradedMode := toZoneNetworkEndpointMapDegradedMode(negtypes.EndpointsDataFromEndpointSlices(tc.testEndpointSlices), fakeZoneGetter, podLister, nodeLister, serviceLister, emptyNamedPort, negtypes.VmIpPortEndpointType, true, enableMultiSubnetCluster, klog.TODO())
				if !reflect.DeepEqual(resultDegradedMode.NetworkEndpointSet, tc.expectedEndpointMapDegradedMode) {
					t.Errorf("degraded mode with enableMultiSubnetCluster = %v, endpoint set is not calculated correctly:\ngot %+v,\n expected %+v", enableMultiSubnetCluster, resultDegradedMode.NetworkEndpointSet, tc.expectedEndpointMapDegradedMode)
				}
				if !reflect.DeepEqual(resultDegradedMode.EndpointPodMap, tc.expectedPodMapDegradedMode) {
					t.Errorf("degraded mode with enableMultiSubnetCluster = %v, endpoint map is not calculated correctly:\ngot %+v,\n expected %+v", enableMultiSubnetCluster, resultDegradedMode.EndpointPodMap, tc.expectedPodMapDegradedMode)
				}
			}
		})
	}
}

func TestValidateEndpointFieldsMultipleSubnets(t *testing.T) {
	t.Parallel()

	emptyNamedPort := ""
	port80 := int32(80)
	protocolTCP := v1.ProtocolTCP
	instance1 := negtypes.TestInstance1

	defaultSubnetLabelInstance := negtypes.TestDefaultSubnetLabelInstance
	defaultSubnetLabelPod := negtypes.TestDefaultSubnetLabelPod
	emptySubnetLabelInstance := negtypes.TestEmptySubnetLabelInstance
	emptySubnetLabelPod := negtypes.TestEmptySubnetLabelPod
	noPodCIDRInstance := negtypes.TestNoPodCIDRInstance
	noPodCIDRPod := negtypes.TestNoPodCIDRPod
	nonDefaultSubnetInstance := negtypes.TestNonDefaultSubnetInstance
	nonDefaultSubnetPod := negtypes.TestNonDefaultSubnetPod

	testContext := negtypes.NewTestContext()
	podLister := testContext.PodInformer.GetIndexer()
	addPodsToLister(podLister, getDefaultEndpointSlices())
	nodeLister := testContext.NodeInformer.GetIndexer()
	zonegetter.PopulateFakeNodeInformer(testContext.NodeInformer, true)
	fakeZoneGetter, err := zonegetter.NewFakeZoneGetter(testContext.NodeInformer, testContext.NodeTopologyInformer, defaultTestSubnetURL, true)
	if err != nil {
		t.Fatalf("failed to initialize zone getter: %v", err)
	}

	// Add defaultSubnetLabelPod that corresponds to defaultSubnetLabelInstance.
	podLister.Add(&v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testServiceNamespace,
			Name:      defaultSubnetLabelPod,
			Labels: map[string]string{
				discovery.LabelServiceName: testServiceName,
				discovery.LabelManagedBy:   managedByEPSControllerValue,
			},
		},
		Spec: v1.PodSpec{
			NodeName: defaultSubnetLabelInstance,
		},
		Status: v1.PodStatus{
			Phase: v1.PodRunning,
			PodIP: "10.101.1.1",
			PodIPs: []v1.PodIP{
				{IP: "10.101.1.1"},
			},
		},
	})

	// Add emptySubnetLabelPod that corresponds to emptySubnetLabelInstance.
	podLister.Add(&v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testServiceNamespace,
			Name:      emptySubnetLabelPod,
			Labels: map[string]string{
				discovery.LabelServiceName: testServiceName,
				discovery.LabelManagedBy:   managedByEPSControllerValue,
			},
		},
		Spec: v1.PodSpec{
			NodeName: emptySubnetLabelInstance,
		},
		Status: v1.PodStatus{
			Phase: v1.PodRunning,
			PodIP: "10.101.2.1",
			PodIPs: []v1.PodIP{
				{IP: "10.101.2.1"},
			},
		},
	})

	// Add noPodCIDRPod that corresponds to noPodCIDRInstance.
	podLister.Add(&v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testServiceNamespace,
			Name:      noPodCIDRPod,
			Labels: map[string]string{
				discovery.LabelServiceName: testServiceName,
				discovery.LabelManagedBy:   managedByEPSControllerValue,
			},
		},
		Spec: v1.PodSpec{
			NodeName: noPodCIDRInstance,
		},
		Status: v1.PodStatus{
			Phase: v1.PodRunning,
			PodIP: "10.101.3.1",
			PodIPs: []v1.PodIP{
				{IP: "10.101.3.1"},
			},
		},
	})

	// Add nonDefaultSubnetPod that corresponds to nonDefaultSubnetInstance.
	podLister.Add(&v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testServiceNamespace,
			Name:      nonDefaultSubnetPod,
			Labels: map[string]string{
				discovery.LabelServiceName: testServiceName,
				discovery.LabelManagedBy:   managedByEPSControllerValue,
			},
		},
		Spec: v1.PodSpec{
			NodeName: nonDefaultSubnetInstance,
		},
		Status: v1.PodStatus{
			Phase: v1.PodRunning,
			PodIP: "10.200.1.1",
			PodIPs: []v1.PodIP{
				{IP: "10.200.1.1"},
			},
		},
	})

	testLabels := map[string]string{
		"run": "foo",
	}
	serviceLister := testContext.ServiceInformer.GetIndexer()
	serviceLister.Add(&v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testServiceNamespace,
			Name:      testServiceName,
		},
		Spec: v1.ServiceSpec{
			Selector: testLabels,
		},
	})

	endpointMap := map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
		{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(
			negtypes.NetworkEndpoint{IP: "10.100.1.1", Node: instance1, Port: "80"},
			negtypes.NetworkEndpoint{IP: "10.100.1.2", Node: instance1, Port: "80"},
		),
	}
	podMap := negtypes.EndpointPodMap{
		negtypes.NetworkEndpoint{IP: "10.100.1.1", Node: instance1, Port: "80"}: types.NamespacedName{Namespace: testServiceNamespace, Name: "pod1"},
		negtypes.NetworkEndpoint{IP: "10.100.1.2", Node: instance1, Port: "80"}: types.NamespacedName{Namespace: testServiceNamespace, Name: "pod2"},
	}

	testCases := []struct {
		desc                            string
		testEndpointSlices              []*discovery.EndpointSlice
		expectedEndpointMap             map[negtypes.NEGLocation]negtypes.NetworkEndpointSet
		expectedPodMap                  negtypes.EndpointPodMap
		expectErr                       error
		expectErrorState                bool
		expectedEndpointMapDegradedMode map[negtypes.NEGLocation]negtypes.NetworkEndpointSet
		expectedPodMapDegradedMode      negtypes.EndpointPodMap
	}{
		{
			desc: "include one endpoint that corresponds to a node without subnet label, endpoint should be included in both calculations",
			testEndpointSlices: []*discovery.EndpointSlice{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testServiceName,
						Namespace: testServiceNamespace,
						Labels: map[string]string{
							discovery.LabelServiceName: testServiceName,
						},
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
							Addresses: []string{"10.101.1.1"},
							NodeName:  &defaultSubnetLabelInstance,
							TargetRef: &v1.ObjectReference{
								Namespace: testServiceNamespace,
								Name:      defaultSubnetLabelPod,
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
			expectedEndpointMap: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(
					negtypes.NetworkEndpoint{IP: "10.100.1.1", Node: instance1, Port: "80"},
					negtypes.NetworkEndpoint{IP: "10.100.1.2", Node: instance1, Port: "80"},
				),
				{Zone: negtypes.TestZone5, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(
					negtypes.NetworkEndpoint{IP: "10.101.1.1", Node: defaultSubnetLabelInstance, Port: "80"},
				),
			},
			expectedPodMap: negtypes.EndpointPodMap{
				negtypes.NetworkEndpoint{IP: "10.100.1.1", Node: instance1, Port: "80"}:                  types.NamespacedName{Namespace: testServiceNamespace, Name: "pod1"},
				negtypes.NetworkEndpoint{IP: "10.100.1.2", Node: instance1, Port: "80"}:                  types.NamespacedName{Namespace: testServiceNamespace, Name: "pod2"},
				negtypes.NetworkEndpoint{IP: "10.101.1.1", Node: defaultSubnetLabelInstance, Port: "80"}: types.NamespacedName{Namespace: testServiceNamespace, Name: defaultSubnetLabelPod},
			},
			expectErr:        nil,
			expectErrorState: false,
			expectedEndpointMapDegradedMode: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(
					negtypes.NetworkEndpoint{IP: "10.100.1.1", Node: instance1, Port: "80"},
					negtypes.NetworkEndpoint{IP: "10.100.1.2", Node: instance1, Port: "80"},
				),
				{Zone: negtypes.TestZone5, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(
					negtypes.NetworkEndpoint{IP: "10.101.1.1", Node: defaultSubnetLabelInstance, Port: "80"},
				),
			},
			expectedPodMapDegradedMode: negtypes.EndpointPodMap{
				negtypes.NetworkEndpoint{IP: "10.100.1.1", Node: instance1, Port: "80"}:                  types.NamespacedName{Namespace: testServiceNamespace, Name: "pod1"},
				negtypes.NetworkEndpoint{IP: "10.100.1.2", Node: instance1, Port: "80"}:                  types.NamespacedName{Namespace: testServiceNamespace, Name: "pod2"},
				negtypes.NetworkEndpoint{IP: "10.101.1.1", Node: defaultSubnetLabelInstance, Port: "80"}: types.NamespacedName{Namespace: testServiceNamespace, Name: defaultSubnetLabelPod},
			},
		},
		{
			desc: "include one endpoint that corresponds to a node with empty string as subnet label, endpoint should be included in both calculations",
			testEndpointSlices: []*discovery.EndpointSlice{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testServiceName,
						Namespace: testServiceNamespace,
						Labels: map[string]string{
							discovery.LabelServiceName: testServiceName,
						},
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
							Addresses: []string{"10.101.2.1"},
							NodeName:  &emptySubnetLabelInstance,
							TargetRef: &v1.ObjectReference{
								Namespace: testServiceNamespace,
								Name:      negtypes.TestEmptySubnetLabelPod,
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
			expectedEndpointMap: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(
					negtypes.NetworkEndpoint{IP: "10.100.1.1", Node: instance1, Port: "80"},
					negtypes.NetworkEndpoint{IP: "10.100.1.2", Node: instance1, Port: "80"},
				),
				{Zone: negtypes.TestZone6, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(
					negtypes.NetworkEndpoint{IP: "10.101.2.1", Node: emptySubnetLabelInstance, Port: "80"},
				),
			},
			expectedPodMap: negtypes.EndpointPodMap{
				negtypes.NetworkEndpoint{IP: "10.100.1.1", Node: instance1, Port: "80"}:                types.NamespacedName{Namespace: testServiceNamespace, Name: "pod1"},
				negtypes.NetworkEndpoint{IP: "10.100.1.2", Node: instance1, Port: "80"}:                types.NamespacedName{Namespace: testServiceNamespace, Name: "pod2"},
				negtypes.NetworkEndpoint{IP: "10.101.2.1", Node: emptySubnetLabelInstance, Port: "80"}: types.NamespacedName{Namespace: testServiceNamespace, Name: emptySubnetLabelPod},
			},
			expectErr:        nil,
			expectErrorState: false,
			expectedEndpointMapDegradedMode: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(
					negtypes.NetworkEndpoint{IP: "10.100.1.1", Node: instance1, Port: "80"},
					negtypes.NetworkEndpoint{IP: "10.100.1.2", Node: instance1, Port: "80"},
				),
				{Zone: negtypes.TestZone6, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(
					negtypes.NetworkEndpoint{IP: "10.101.2.1", Node: emptySubnetLabelInstance, Port: "80"},
				),
			},
			expectedPodMapDegradedMode: negtypes.EndpointPodMap{
				negtypes.NetworkEndpoint{IP: "10.100.1.1", Node: instance1, Port: "80"}:                types.NamespacedName{Namespace: testServiceNamespace, Name: "pod1"},
				negtypes.NetworkEndpoint{IP: "10.100.1.2", Node: instance1, Port: "80"}:                types.NamespacedName{Namespace: testServiceNamespace, Name: "pod2"},
				negtypes.NetworkEndpoint{IP: "10.101.2.1", Node: emptySubnetLabelInstance, Port: "80"}: types.NamespacedName{Namespace: testServiceNamespace, Name: emptySubnetLabelPod},
			},
		},
		{
			desc: "include one endpoint that corresponds to a node without PodCIDR, endpoint should be excluded in degraded mode",
			testEndpointSlices: []*discovery.EndpointSlice{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testServiceName,
						Namespace: testServiceNamespace,
						Labels: map[string]string{
							discovery.LabelServiceName: testServiceName,
						},
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
							Addresses: []string{"10.101.3.1"},
							NodeName:  &noPodCIDRInstance,
							TargetRef: &v1.ObjectReference{
								Namespace: testServiceNamespace,
								Name:      noPodCIDRPod,
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
			expectedEndpointMap:             nil,
			expectedPodMap:                  nil,
			expectErr:                       negtypes.ErrEPNodePodCIDRNotSet,
			expectErrorState:                true,
			expectedEndpointMapDegradedMode: endpointMap,
			expectedPodMapDegradedMode:      podMap,
		},
		{
			desc: "include one endpoint that corresponds to a non-default subnet node, endpoint should be excluded in both calculations",
			testEndpointSlices: []*discovery.EndpointSlice{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testServiceName,
						Namespace: testServiceNamespace,
						Labels: map[string]string{
							discovery.LabelServiceName: testServiceName,
						},
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
							Addresses: []string{"10.200.1.1"},
							NodeName:  &nonDefaultSubnetInstance,
							TargetRef: &v1.ObjectReference{
								Namespace: testServiceNamespace,
								Name:      nonDefaultSubnetPod,
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
			expectedEndpointMap:             endpointMap,
			expectedPodMap:                  podMap,
			expectErr:                       nil,
			expectErrorState:                false,
			expectedEndpointMapDegradedMode: endpointMap,
			expectedPodMapDegradedMode:      podMap,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			result, err := toZoneNetworkEndpointMap(negtypes.EndpointsDataFromEndpointSlices(tc.testEndpointSlices), fakeZoneGetter, podLister, emptyNamedPort, negtypes.VmIpPortEndpointType, true, true, klog.TODO())
			if !errors.Is(err, tc.expectErr) {
				t.Errorf("With multi-subnet cluster enabled, normal mode calculation got error %v, expected %v", err, tc.expectErr)
			}
			if !reflect.DeepEqual(result.NetworkEndpointSet, tc.expectedEndpointMap) {
				t.Errorf("With multi-subnet cluster enabled, normal mode endpoint set is not calculated correctly:\ngot %+v,\n expected %+v", result.NetworkEndpointSet, tc.expectedEndpointMapDegradedMode)
			}
			if !reflect.DeepEqual(result.EndpointPodMap, tc.expectedPodMap) {
				t.Errorf("With multi-subnet cluster enabled, normal mode endpoint map is not calculated correctly:\ngot %+v,\n expected %+v", result.EndpointPodMap, tc.expectedPodMapDegradedMode)
			}
			if syncErr := negtypes.ClassifyError(err); syncErr.IsErrorState != tc.expectErrorState {
				t.Errorf("With multi-subnet cluster enabled, normal mode got isErrorState=%v,\n expected %v", syncErr.IsErrorState, tc.expectErrorState)
			}

			resultDegradedMode := toZoneNetworkEndpointMapDegradedMode(negtypes.EndpointsDataFromEndpointSlices(tc.testEndpointSlices), fakeZoneGetter, podLister, nodeLister, serviceLister, emptyNamedPort, negtypes.VmIpPortEndpointType, true, true, klog.TODO())
			if !reflect.DeepEqual(resultDegradedMode.NetworkEndpointSet, tc.expectedEndpointMapDegradedMode) {
				t.Errorf("With multi-subnet cluste enabled, degraded mode endpoint set is not calculated correctly:\ngot %+v,\n expected %+v", resultDegradedMode.NetworkEndpointSet, tc.expectedEndpointMapDegradedMode)
			}
			if !reflect.DeepEqual(resultDegradedMode.EndpointPodMap, tc.expectedPodMapDegradedMode) {
				t.Errorf("With multi-subnet cluste enabled, degraded mode endpoint map is not calculated correctly:\ngot %+v,\n expected %+v", resultDegradedMode.EndpointPodMap, tc.expectedPodMapDegradedMode)
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
	nodeLister.Add(&v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: instance1,
		},
		Spec: v1.NodeSpec{
			PodCIDR:  "10.100.1.0/24",
			PodCIDRs: []string{"2001:db8::/48", "10.100.1.0/24"},
		},
	})
	testPodIPv4 := "10.100.1.1"
	testPodIPv4OutOfRange := "10.101.1.1"
	testPodIPv6 := "2001:db8::2:1"
	testPodIPv6OutOfRange := "2001:db9::2:1"
	testLabels1 := map[string]string{
		"run": "foo",
	}
	testLabels2 := map[string]string{
		"run": "bar",
	}
	serviceLister := testContext.ServiceInformer.GetIndexer()
	serviceLister.Add(&v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testServiceNamespace,
			Name:      testServiceName,
		},
		Spec: v1.ServiceSpec{
			Selector: testLabels1,
		},
	})
	testServiceNameNotFound := "foo"
	testCases := []struct {
		desc            string
		pod             *v1.Pod
		networkEndpoint negtypes.NetworkEndpoint
		serviceName     string
		isCustomEPS     bool
		expectErr       error
	}{
		{
			desc: "a valid pod with single stack IPv4 address and phase running",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testServiceNamespace,
					Name:      "pod1",
					Labels:    testLabels1,
				},
				Status: v1.PodStatus{
					Phase: v1.PodRunning,
				},
				Spec: v1.PodSpec{
					NodeName: instance1,
				},
			},
			networkEndpoint: negtypes.NetworkEndpoint{IP: testPodIPv4},
			serviceName:     testServiceName,
			isCustomEPS:     false,
			expectErr:       nil,
		},
		{
			desc: "a valid pod with single stack IPv6 address and phase running",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testServiceNamespace,
					Name:      "pod2",
					Labels:    testLabels1,
				},
				Status: v1.PodStatus{
					Phase: v1.PodRunning,
				},
				Spec: v1.PodSpec{
					NodeName: instance1,
				},
			},
			networkEndpoint: negtypes.NetworkEndpoint{IP: testPodIPv6},
			serviceName:     testServiceName,
			isCustomEPS:     false,
			expectErr:       nil,
		},
		{
			desc: "a terminal pod with phase failed",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testServiceNamespace,
					Name:      "pod3",
					Labels:    testLabels1,
				},
				Status: v1.PodStatus{
					Phase: v1.PodFailed,
				},
				Spec: v1.PodSpec{
					NodeName: instance1,
				},
			},
			networkEndpoint: negtypes.NetworkEndpoint{IP: testPodIPv4},
			serviceName:     testServiceName,
			isCustomEPS:     false,
			expectErr:       negtypes.ErrEPPodTerminal,
		},
		{
			desc: "a terminal pod with phase succeeded",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testServiceNamespace,
					Name:      "pod4",
					Labels:    testLabels1,
				},
				Status: v1.PodStatus{
					Phase: v1.PodSucceeded,
				},
				Spec: v1.PodSpec{
					NodeName: instance1,
				},
			},
			networkEndpoint: negtypes.NetworkEndpoint{IP: testPodIPv4},
			serviceName:     testServiceName,
			isCustomEPS:     false,
			expectErr:       negtypes.ErrEPPodTerminal,
		},
		{
			desc: "a pod from non-existent node",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testServiceNamespace,
					Name:      "pod5",
					Labels:    testLabels1,
				},
				Status: v1.PodStatus{
					Phase: v1.PodRunning,
				},
				Spec: v1.PodSpec{
					NodeName: testNodeNonExistent,
				},
			},
			networkEndpoint: negtypes.NetworkEndpoint{IP: testPodIPv4},
			serviceName:     testServiceName,
			isCustomEPS:     false,
			expectErr:       negtypes.ErrEPNodeNotFound,
		},
		{
			desc: "a pod with single stack IPv4 IP address outside of the node's allocated pod range",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testServiceNamespace,
					Name:      "pod6",
					Labels:    testLabels1,
				},
				Status: v1.PodStatus{
					Phase: v1.PodRunning,
				},
				Spec: v1.PodSpec{
					NodeName: instance1,
				},
			},
			networkEndpoint: negtypes.NetworkEndpoint{IP: testPodIPv4OutOfRange},
			serviceName:     testServiceName,
			isCustomEPS:     false,
			expectErr:       negtypes.ErrEPIPOutOfPodCIDR,
		},
		{
			desc: "a pod with single stack IPv6 IP address outside of the node's allocated pod range",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testServiceNamespace,
					Name:      "pod7",
					Labels:    testLabels1,
				},
				Status: v1.PodStatus{
					Phase: v1.PodRunning,
				},
				Spec: v1.PodSpec{
					NodeName: instance1,
				},
			},
			networkEndpoint: negtypes.NetworkEndpoint{IP: testPodIPv6OutOfRange},
			serviceName:     testServiceName,
			isCustomEPS:     false,
			expectErr:       negtypes.ErrEPIPOutOfPodCIDR,
		},
		{
			desc: "a pod with dual stack, and both of its ips are within the range",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testServiceNamespace,
					Name:      "pod8",
					Labels:    testLabels1,
				},
				Status: v1.PodStatus{
					Phase: v1.PodRunning,
				},
				Spec: v1.PodSpec{
					NodeName: instance1,
				},
			},
			networkEndpoint: negtypes.NetworkEndpoint{IP: testPodIPv4, IPv6: testPodIPv6},
			serviceName:     testServiceName,
			isCustomEPS:     false,
			expectErr:       nil,
		},
		{
			desc: "a pod with dual stack, and its IPv4 IP address is outside of the node's allocated pod range",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testServiceNamespace,
					Name:      "pod9",
					Labels:    testLabels1,
				},
				Status: v1.PodStatus{
					Phase: v1.PodRunning,
				},
				Spec: v1.PodSpec{
					NodeName: instance1,
				},
			},
			networkEndpoint: negtypes.NetworkEndpoint{IP: testPodIPv4OutOfRange, IPv6: testPodIPv6},
			serviceName:     testServiceName,
			isCustomEPS:     false,
			expectErr:       negtypes.ErrEPIPOutOfPodCIDR,
		},
		{
			desc: "a pod with dual stack, and its IPv6 IP address is outside of the node's allocated pod range",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testServiceNamespace,
					Name:      "pod10",
					Labels:    testLabels1,
				},
				Status: v1.PodStatus{
					Phase: v1.PodRunning,
				},
				Spec: v1.PodSpec{
					NodeName: instance1,
				},
			},
			networkEndpoint: negtypes.NetworkEndpoint{IP: testPodIPv4, IPv6: testPodIPv6OutOfRange},
			serviceName:     testServiceName,
			isCustomEPS:     false,
			expectErr:       negtypes.ErrEPIPOutOfPodCIDR,
		},
		{
			desc: "a pod with single stack IPv4, and its IPv6 IP address is empty",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testServiceNamespace,
					Name:      "pod11",
					Labels:    testLabels1,
				},
				Status: v1.PodStatus{
					Phase: v1.PodRunning,
				},
				Spec: v1.PodSpec{
					NodeName: instance1,
				},
			},
			networkEndpoint: negtypes.NetworkEndpoint{IP: testPodIPv4, IPv6: ""},
			serviceName:     testServiceName,
			isCustomEPS:     false,
			expectErr:       nil,
		},
		{
			desc: "a pod with non-existing service name",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testServiceNamespace,
					Name:      "pod12",
					Labels:    testLabels1,
				},
				Status: v1.PodStatus{
					Phase: v1.PodRunning,
				},
				Spec: v1.PodSpec{
					NodeName: instance1,
				},
			},
			networkEndpoint: negtypes.NetworkEndpoint{IP: testPodIPv4},
			serviceName:     testServiceNameNotFound,
			isCustomEPS:     false,
			expectErr:       negtypes.ErrEPServiceNotFound,
		},
		{
			desc: "a pod referenced by a non-custom endpoint slice, with labels not matching to service's label selector",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testServiceNamespace,
					Name:      "pod13",
					Labels:    testLabels2,
				},
				Status: v1.PodStatus{
					Phase: v1.PodRunning,
				},
				Spec: v1.PodSpec{
					NodeName: instance1,
				},
			},
			networkEndpoint: negtypes.NetworkEndpoint{IP: testPodIPv4},
			serviceName:     testServiceName,
			isCustomEPS:     false,
			expectErr:       negtypes.ErrEPPodLabelMismatch,
		},
		{
			desc: "a pod referenced by a custom endpoint slice, with labels not matching to service's label selector",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testServiceNamespace,
					Name:      "pod14",
					Labels:    testLabels2,
				},
				Status: v1.PodStatus{
					Phase: v1.PodRunning,
				},
				Spec: v1.PodSpec{
					NodeName: instance1,
				},
			},
			networkEndpoint: negtypes.NetworkEndpoint{IP: testPodIPv4},
			serviceName:     testServiceName,
			isCustomEPS:     true, // for custom endpoint slice, we won't check the pod's labels
			expectErr:       nil,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			if _, got := validatePod(tc.pod, nodeLister, serviceLister, tc.networkEndpoint, tc.serviceName, tc.isCustomEPS, klog.TODO()); !errors.Is(got, tc.expectErr) {
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

// addPodsToLister takes endpoints from endpointSlices
// and add pods to podLister based on endpoints' IPs and pod mapping.
func addPodsToLister(podLister cache.Indexer, endpointSlices []*discovery.EndpointSlice) {
	testLabels := map[string]string{
		"run": "foo",
	}
	// collect both ipv4 and ipv6 IP address for pods
	podToIPs := make(map[string][]v1.PodIP)
	podToNodeName := make(map[string]string)
	for _, eps := range endpointSlices {
		for _, ep := range eps.Endpoints {
			pod := fmt.Sprintf("%s/%s", ep.TargetRef.Namespace, ep.TargetRef.Name)
			podToNodeName[pod] = *ep.NodeName
			for _, addr := range ep.Addresses {
				podToIPs[pod] = append(podToIPs[pod], v1.PodIP{IP: addr})
			}
		}
	}
	for pod, IPs := range podToIPs {
		strs := strings.Split(pod, "/")
		podNamespace := strs[0]
		podName := strs[1]
		podLister.Add(&v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: podNamespace,
				Name:      podName,
				Labels:    testLabels,
			},
			Spec: v1.PodSpec{
				NodeName: podToNodeName[pod],
			},
			Status: v1.PodStatus{
				Phase:  v1.PodRunning,
				PodIP:  IPs[0].IP,
				PodIPs: IPs,
			},
		})
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
					discovery.LabelManagedBy:   managedByEPSControllerValue,
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
					discovery.LabelManagedBy:   managedByEPSControllerValue,
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
					discovery.LabelManagedBy:   managedByEPSControllerValue,
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
					discovery.LabelManagedBy:   managedByEPSControllerValue,
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
