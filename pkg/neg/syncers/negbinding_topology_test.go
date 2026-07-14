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

package syncers

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	nodetopologyv1 "github.com/GoogleCloudPlatform/gke-networking-api/apis/nodetopology/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	negbindingv1beta1 "k8s.io/ingress-gce/pkg/apis/negbinding/v1beta1"
	"k8s.io/ingress-gce/pkg/neg/types/shared"
	fakenegbinding "k8s.io/ingress-gce/pkg/negbinding/client/clientset/versioned/fake"
	informernegbinding "k8s.io/ingress-gce/pkg/negbinding/client/informers/externalversions/negbinding/v1beta1"
	"k8s.io/ingress-gce/pkg/network"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/zonegetter"
	"k8s.io/klog/v2"
)

func TestNEGBindingTopologyProvider(t *testing.T) {
	namespace := "test-namespace"
	name := "test-binding"
	defaultSubnetURL := "https://www.googleapis.com/compute/v1/projects/test-project/regions/us-central1/subnetworks/default-subnet"

	testCases := []struct {
		desc            string
		initialBinding  *negbindingv1beta1.NetworkEndpointGroupBinding
		expectedSubnets []nodetopologyv1.SubnetConfig
		expectedZones   shared.ZonesPerSubnetMap
		updatedBinding  *negbindingv1beta1.NetworkEndpointGroupBinding // optional runtime update
		updatedSubnets  []nodetopologyv1.SubnetConfig                  // expected after update
		updatedZones    shared.ZonesPerSubnetMap                       // expected after update
	}{
		{
			desc: "Empty NEG list",
			initialBinding: &negbindingv1beta1.NetworkEndpointGroupBinding{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      name,
				},
				Spec: negbindingv1beta1.NetworkEndpointGroupBindingSpec{
					NetworkEndpointGroups: []negbindingv1beta1.SpecNegRef{},
				},
			},
			expectedSubnets: []nodetopologyv1.SubnetConfig{},
			expectedZones:   shared.ZonesPerSubnetMap{},
		},
		{
			desc: "Single subnet with primary default mapping",
			initialBinding: &negbindingv1beta1.NetworkEndpointGroupBinding{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      name,
				},
				Spec: negbindingv1beta1.NetworkEndpointGroupBindingSpec{
					NetworkEndpointGroups: []negbindingv1beta1.SpecNegRef{
						{
							Name:   "neg-default",
							Subnet: "default-subnet",
							Zones:  []string{"us-central1-a", "us-central1-b"},
						},
					},
				},
			},
			expectedSubnets: []nodetopologyv1.SubnetConfig{
				{
					Name:       "default-subnet",
					SubnetPath: defaultSubnetURL,
				},
			},
			expectedZones: shared.ZonesPerSubnetMap{
				"default-subnet": sets.New("us-central1-a", "us-central1-b"),
			},
		},
		{
			desc: "Multiple subnets and dynamic update verification",
			initialBinding: &negbindingv1beta1.NetworkEndpointGroupBinding{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      name,
				},
				Spec: negbindingv1beta1.NetworkEndpointGroupBindingSpec{
					NetworkEndpointGroups: []negbindingv1beta1.SpecNegRef{
						{
							Name:   "neg-a",
							Subnet: "subnet-a",
							Zones:  []string{"us-central1-a"},
						},
					},
				},
			},
			expectedSubnets: []nodetopologyv1.SubnetConfig{
				{
					Name:       "subnet-a",
					SubnetPath: "https://www.googleapis.com/compute/v1/projects/test-project/regions/us-central1/subnetworks/subnet-a",
				},
			},
			expectedZones: shared.ZonesPerSubnetMap{
				"subnet-a": sets.New("us-central1-a"),
			},
			updatedBinding: &negbindingv1beta1.NetworkEndpointGroupBinding{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      name,
				},
				Spec: negbindingv1beta1.NetworkEndpointGroupBindingSpec{
					NetworkEndpointGroups: []negbindingv1beta1.SpecNegRef{
						{
							Name:   "neg-a",
							Subnet: "subnet-a",
							Zones:  []string{"us-central1-a", "us-central1-b"},
						},
						{
							Name:   "neg-b",
							Subnet: "subnet-b",
							Zones:  []string{"us-central1-c"},
						},
					},
				},
			},
			updatedSubnets: []nodetopologyv1.SubnetConfig{
				{
					Name:       "subnet-a",
					SubnetPath: "https://www.googleapis.com/compute/v1/projects/test-project/regions/us-central1/subnetworks/subnet-a",
				},
				{
					Name:       "subnet-b",
					SubnetPath: "https://www.googleapis.com/compute/v1/projects/test-project/regions/us-central1/subnetworks/subnet-b",
				},
			},
			updatedZones: shared.ZonesPerSubnetMap{
				"subnet-a": sets.New("us-central1-a", "us-central1-b"),
				"subnet-b": sets.New("us-central1-c"),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			fakeClient := fakenegbinding.NewSimpleClientset()
			informer := informernegbinding.NewNetworkEndpointGroupBindingInformer(fakeClient, "", time.Second, utils.NewNamespaceIndexer())
			negBindingLister := informer.GetIndexer()

			p, err := NewNEGBindingTopologyProvider(namespace, name, negBindingLister, defaultSubnetURL)
			if err != nil {
				t.Fatalf("NewNegBindingTopologyProvider() failed unexpectedly: %v", err)
			}

			negBindingLister.Add(tc.initialBinding)

			subnets := p.ListSubnetsInDefaultNetwork(klog.TODO())
			if !reflect.DeepEqual(subnets, tc.expectedSubnets) {
				t.Errorf("ListSubnetsInDefaultNetwork() returned %+v, expected %+v", subnets, tc.expectedSubnets)
			}

			zonesPerSubnet, err := p.ListZonesPerSubnet(zonegetter.AllNodesFilter, network.NetworkInfo{IsDefault: true}, klog.TODO())
			if err != nil {
				t.Errorf("ListZonesPerSubnet() returned unexpected error: %v", err)
			}
			if !reflect.DeepEqual(zonesPerSubnet, tc.expectedZones) {
				t.Errorf("ListZonesPerSubnet() returned %+v, expected %+v", zonesPerSubnet, tc.expectedZones)
			}

			if _, ok := zonesPerSubnet["unknown-subnet"]; ok {
				t.Errorf("ListZonesPerSubnet() returned zones for unknown-subnet, expected it to be absent")
			}

			if tc.updatedBinding != nil {
				negBindingLister.Update(tc.updatedBinding)

				subnets = p.ListSubnetsInDefaultNetwork(klog.TODO())
				if !reflect.DeepEqual(subnets, tc.updatedSubnets) {
					t.Errorf("ListSubnetsInDefaultNetwork() after update returned %+v, expected %+v", subnets, tc.updatedSubnets)
				}

				zonesPerSubnet, err = p.ListZonesPerSubnet(zonegetter.AllNodesFilter, network.NetworkInfo{IsDefault: true}, klog.TODO())
				if err != nil {
					t.Errorf("ListZonesPerSubnet() after update returned unexpected error: %v", err)
				}
				if !reflect.DeepEqual(zonesPerSubnet, tc.updatedZones) {
					t.Errorf("ListZonesPerSubnet() after update returned %+v, expected %+v", zonesPerSubnet, tc.updatedZones)
				}
			}
		})
	}

}

func TestNewNEGBindingTopologyProviderInvalidDefaultSubnetURL(t *testing.T) {
	namespace := "test-namespace"
	name := "test-binding"

	fakeClient := fakenegbinding.NewSimpleClientset()
	informer := informernegbinding.NewNetworkEndpointGroupBindingInformer(fakeClient, "", time.Second, utils.NewNamespaceIndexer())
	negBindingLister := informer.GetIndexer()

	_, err := NewNEGBindingTopologyProvider(namespace, name, negBindingLister, "invalid-url-with-no-slashes")
	if err == nil {
		t.Error("NewNEGBindingTopologyProvider() with invalid defaultSubnetURL returned no error")
	} else if expected := `failed to parse default subnetwork URL "invalid-url-with-no-slashes": "invalid-url-with-no-slashes" is not a valid resource URL`; err.Error() != expected {
		t.Errorf("NewNEGBindingTopologyProvider() returned error %q, expected %q", err.Error(), expected)
	}
}

func TestNEGBindingTopologyProviderInvalidTypeInCache(t *testing.T) {
	namespace := "test-namespace"
	name := "test-binding"
	defaultSubnetURL := "https://www.googleapis.com/compute/v1/projects/test-project/regions/us-central1/subnetworks/default-subnet"

	fakeClient := fakenegbinding.NewSimpleClientset()
	informer := informernegbinding.NewNetworkEndpointGroupBindingInformer(fakeClient, "", time.Second, utils.NewNamespaceIndexer())
	negBindingLister := informer.GetIndexer()

	p, err := NewNEGBindingTopologyProvider(namespace, name, negBindingLister, defaultSubnetURL)
	if err != nil {
		t.Fatalf("NewNegBindingTopologyProvider() failed unexpectedly: %v", err)
	}

	invalidObj := &metav1.PartialObjectMetadata{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
	}
	negBindingLister.Add(invalidObj)

	subnets := p.ListSubnetsInDefaultNetwork(klog.TODO())
	if subnets != nil {
		t.Errorf("ListSubnetsInDefaultNetwork() returned %v, expected nil when cache has invalid type", subnets)
	}

	_, err = p.ListZonesPerSubnet(zonegetter.AllNodesFilter, network.NetworkInfo{IsDefault: true}, klog.TODO())
	if err == nil {
		t.Errorf("ListZonesPerSubnet() returned no error, expected error when cache has invalid type")
	} else {
		expectedErr := fmt.Sprintf(`failed to get NegBinding from store: cached object "%s/%s" is of type *v1.PartialObjectMetadata, expected *NetworkEndpointGroupBinding`, namespace, name)
		if err.Error() != expectedErr {
			t.Errorf("ListZonesPerSubnet() returned error %q, expected %q", err.Error(), expectedErr)
		}
	}
}

func TestNEGBindingTopologyProviderNEGBindingNotInStore(t *testing.T) {
	namespace := "test-namespace"
	name := "test-binding"
	defaultSubnetURL := "https://www.googleapis.com/compute/v1/projects/test-project/regions/us-central1/subnetworks/default-subnet"

	fakeClient := fakenegbinding.NewSimpleClientset()
	informer := informernegbinding.NewNetworkEndpointGroupBindingInformer(fakeClient, "", time.Second, utils.NewNamespaceIndexer())
	negBindingLister := informer.GetIndexer()

	p, err := NewNEGBindingTopologyProvider(namespace, name, negBindingLister, defaultSubnetURL)
	if err != nil {
		t.Fatalf("NewNegBindingTopologyProvider() failed unexpectedly: %v", err)
	}

	subnets := p.ListSubnetsInDefaultNetwork(klog.TODO())
	if subnets != nil {
		t.Errorf("ListSubnetsInDefaultNetwork() returned %v, expected nil when object is not in store", subnets)
	}

	_, err = p.ListZonesPerSubnet(zonegetter.AllNodesFilter, network.NetworkInfo{IsDefault: true}, klog.TODO())
	if err == nil {
		t.Errorf("ListZonesPerSubnet() returned no error, expected error when object is not in store")
	} else {
		expectedErr := fmt.Sprintf("failed to get NegBinding from store: negbinding %s/%s is not in store", namespace, name)
		if err.Error() != expectedErr {
			t.Errorf("ListZonesPerSubnet() returned error %q, expected %q", err.Error(), expectedErr)
		}
	}
}

func TestNEGBindingTopologyProviderMultinetError(t *testing.T) {
	namespace := "test-namespace"
	name := "test-binding"
	defaultSubnetURL := "https://www.googleapis.com/compute/v1/projects/test-project/regions/us-central1/subnetworks/default-subnet"

	fakeClient := fakenegbinding.NewSimpleClientset()
	informer := informernegbinding.NewNetworkEndpointGroupBindingInformer(fakeClient, "", time.Second, utils.NewNamespaceIndexer())
	negBindingLister := informer.GetIndexer()

	p, err := NewNEGBindingTopologyProvider(namespace, name, negBindingLister, defaultSubnetURL)
	if err != nil {
		t.Fatalf("NewNegBindingTopologyProvider() failed unexpectedly: %v", err)
	}

	multinetInfo := network.NetworkInfo{IsDefault: false}
	_, err = p.ListZonesPerSubnet(zonegetter.AllNodesFilter, multinetInfo, klog.TODO())
	if err == nil {
		t.Errorf("ListZonesPerSubnet() expected error for multi-network mode, got nil")
	} else if expected := "NEGBinding does not support multi-network mode"; err.Error() != expected {
		t.Errorf("ListZonesPerSubnet() returned error %q, expected %q", err.Error(), expected)
	}
}
