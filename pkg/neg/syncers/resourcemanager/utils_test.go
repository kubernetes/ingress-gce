/*
Copyright 2026 The Kubernetes Authors.

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

package resourcemanager

import (
	"reflect"
	"testing"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"k8s.io/ingress-gce/pkg/neg/metrics"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/ingress-gce/pkg/network"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog/v2"

	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/ingress-gce/pkg/composite"
)

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
		metrics.NewNegMetrics(),
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
		metrics.NewNegMetrics())

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
			metrics.NewNegMetrics())
		if err != nil {
			t.Errorf("Errored while ensuring network endpoint groups: %s", err)
		}
		negRef := (&SvcNegResourceManager{}).negToNegRef(negObj)

		neg, err := fakeCloud.GetNetworkEndpointGroup(negName, testZone, apiVersion, klog.TODO())
		if err != nil {
			t.Errorf("Failed to retrieve NEG %q: %v", negName, err)
		}

		if neg == nil {
			t.Errorf("Failed to find neg")
		}

		expectedNegObj := (&SvcNegResourceManager{}).negToNegRef(neg)
		if negRef != expectedNegObj {
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
			metrics.NewNegMetrics(),
		)

		if err != nil {
			t.Errorf("Unexpected error when ensuring NEG: %s", err)
		}

		negRef = (&SvcNegResourceManager{}).negToNegRef(negObj)
		if negRef != expectedNegObj {
			t.Errorf("Expected neg object %+v, but received %+v", expectedNegObj, negObj)
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
				metrics.NewNegMetrics(),
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
				Namespace:   testServiceNameSpace,
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
				metrics.NewNegMetrics(),
			)

			if err != nil {
				t.Errorf("Unexpected error when called with duplicated NEG: %v", err)
			}
		})
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
		Namespace:   testServiceNameSpace,
		ServiceName: testServiceName,
		Port:        testPort,
	}.String()

	anotherNegDesc := utils.NegDescription{
		ClusterUID:  "another-cluster",
		Namespace:   testServiceNameSpace,
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
		fakeCloud := negtypes.NewAdapterWithNetwork(fakeGCE, testNetwork, testSubnetwork, metrics.NewNegMetrics())
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
			metrics.NewNegMetrics(),
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
