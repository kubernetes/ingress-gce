/*
Copyright 2023 The Kubernetes Authors.

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

package network

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"k8s.io/klog/v2"

	networkv1 "github.com/GoogleCloudPlatform/gke-networking-api/apis/network/v1"
	netfake "github.com/GoogleCloudPlatform/gke-networking-api/client/network/clientset/versioned/fake"
	informergkenetworkparamset "github.com/GoogleCloudPlatform/gke-networking-api/client/network/informers/externalversions/network/v1"
	informernetwork "github.com/GoogleCloudPlatform/gke-networking-api/client/network/informers/externalversions/network/v1"
	compute "google.golang.org/api/compute/v1"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/ingress-gce/pkg/utils"
)

func TestServiceNetwork(t *testing.T) {

	namespace := "test"

	serviceWithSecondaryNet := &apiv1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "testService"},
		Spec: apiv1.ServiceSpec{
			Selector: map[string]string{
				networkSelector: "secondary-network",
			},
			ExternalTrafficPolicy: apiv1.ServiceExternalTrafficPolicyLocal,
		},
	}
	cloud := fakeCloud{}

	cases := []struct {
		desc               string
		network            *networkv1.Network
		gkeNetworkParamSet *networkv1.GKENetworkParamSet
		service            *apiv1.Service
		want               *NetworkInfo
		wantErr            string
	}{
		{
			desc:               "valid setup",
			network:            testNetwork("secondary-network", "secondary-network-params"),
			gkeNetworkParamSet: testGKENetworkParamSet("secondary-network-params", "secondary-vpc", "secondary-subnet"),
			service:            serviceWithSecondaryNet,
			want: &NetworkInfo{
				IsDefault:     false,
				K8sNetwork:    "secondary-network",
				NetworkURL:    "https://www.googleapis.com/compute/v1/projects/test-project/global/networks/secondary-vpc",
				SubnetworkURL: "https://www.googleapis.com/compute/v1/projects/test-project/regions/test-region/subnetworks/secondary-subnet",
			},
		},
		{
			desc: "service without network selector",
			service: &apiv1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: "testService"},
				Spec: apiv1.ServiceSpec{
					Selector: map[string]string{
						"app": "someapp",
					},
				},
			},
			want: DefaultNetwork(cloud),
		},
		{
			desc: "service with empty network selector",
			service: &apiv1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: "testService"},
				Spec: apiv1.ServiceSpec{
					Selector: map[string]string{
						networkSelector: "",
					},
				},
			},
			want: DefaultNetwork(cloud),
		},
		{
			desc: "service with network selector for the default network",
			service: &apiv1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: "testService"},
				Spec: apiv1.ServiceSpec{
					Selector: map[string]string{
						networkSelector: networkv1.DefaultPodNetworkName,
					},
				},
			},
			want: DefaultNetwork(cloud),
		},
		{
			desc:    "network not defined",
			service: serviceWithSecondaryNet,
			wantErr: "network secondary-network does not exist",
		},
		{
			desc: "network paramsRef for non GKENetworkParamSet",
			network: &networkv1.Network{
				ObjectMeta: metav1.ObjectMeta{
					Name: "secondary-network",
				},
				Spec: networkv1.NetworkSpec{
					Type: networkv1.L3NetworkType,
					ParametersRef: &networkv1.NetworkParametersReference{
						Group: networkingGKEGroup,
						Kind:  "UnsupportedNetworkParams",
						Name:  "secondary-network-params",
					},
				},
			},
			gkeNetworkParamSet: testGKENetworkParamSet("secondary-network-params", "secondary-vpc", "secondary-subnet"),
			service:            serviceWithSecondaryNet,
			wantErr:            "network.Spec.ParametersRef does not refer a GKENetworkParamSet resource",
		},
		{
			desc: "network paramsRef for GKENetworkParamSet with namespace",
			network: &networkv1.Network{
				ObjectMeta: metav1.ObjectMeta{
					Name: "secondary-network",
				},
				Spec: networkv1.NetworkSpec{
					Type: networkv1.L3NetworkType,
					ParametersRef: &networkv1.NetworkParametersReference{
						Group:     networkingGKEGroup,
						Kind:      "GKENetworkParamSet",
						Name:      "secondary-network-params",
						Namespace: &namespace,
					},
				},
			},
			gkeNetworkParamSet: testGKENetworkParamSet("secondary-network-params", "secondary-vpc", "secondary-subnet"),
			service:            serviceWithSecondaryNet,
			wantErr:            "network.Spec.ParametersRef.namespace must not be set for GKENetworkParamSet reference as it is a cluster scope resource",
		},
		{
			desc:               "missing GKENetworkParamSet",
			network:            testNetwork("secondary-network", "secondary-network-params"),
			gkeNetworkParamSet: nil,
			service:            serviceWithSecondaryNet,
			wantErr:            "GKENetworkParamSet secondary-network-params was not found",
		},
		{
			desc: "invalid network type L2",
			network: &networkv1.Network{
				ObjectMeta: metav1.ObjectMeta{
					Name: "secondary-network",
				},
				Spec: networkv1.NetworkSpec{
					Type: networkv1.L2NetworkType,
					ParametersRef: &networkv1.NetworkParametersReference{
						Group: networkingGKEGroup,
						Kind:  "GKENetworkParamSet",
						Name:  "secondary-network-params",
					},
				},
			},
			gkeNetworkParamSet: testGKENetworkParamSet("secondary-network-params", "secondary-vpc", "secondary-subnet"),
			service:            serviceWithSecondaryNet,
			wantErr:            "network.Spec.Type=L2 is not supported for multinetwork LoadBalancer services, the only supported network type is L3",
		},
		{
			desc: "invalid network type Device",
			network: &networkv1.Network{
				ObjectMeta: metav1.ObjectMeta{
					Name: "secondary-network",
				},
				Spec: networkv1.NetworkSpec{
					Type: networkv1.DeviceNetworkType,
					ParametersRef: &networkv1.NetworkParametersReference{
						Group: networkingGKEGroup,
						Kind:  "GKENetworkParamSet",
						Name:  "secondary-network-params",
					},
				},
			},
			gkeNetworkParamSet: testGKENetworkParamSet("secondary-network-params", "secondary-vpc", "secondary-subnet"),
			service:            serviceWithSecondaryNet,
			wantErr:            "network.Spec.Type=Device is not supported for multinetwork LoadBalancer services, the only supported network type is L3",
		},
	}

	for _, tc := range cases {

		t.Run(tc.desc, func(t *testing.T) {
			networkClient := netfake.NewSimpleClientset()

			networkInformer := informernetwork.NewNetworkInformer(networkClient, time.Minute*10, utils.NewNamespaceIndexer())
			gkeNetworkParamSetInformer := informergkenetworkparamset.NewGKENetworkParamSetInformer(networkClient, time.Minute*10, utils.NewNamespaceIndexer())

			networkIndexer := networkInformer.GetIndexer()
			gkeNetworkParamSetIndexer := gkeNetworkParamSetInformer.GetIndexer()

			if tc.network != nil {
				networkIndexer.Add(tc.network)
			}
			if tc.gkeNetworkParamSet != nil {
				gkeNetworkParamSetIndexer.Add(tc.gkeNetworkParamSet)
			}
			networksResolver := NewNetworksResolver(networkIndexer, gkeNetworkParamSetIndexer, fakeCloud{}, true, klog.Background())
			network, err := networksResolver.ServiceNetwork(tc.service)
			if err != nil {
				if tc.wantErr == "" {
					t.Fatalf("determining network info returned an error, err=%v", err)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("expected error containing message %q but got error %v", tc.wantErr, err)
				}
			}
			if err == nil && tc.wantErr != "" {
				t.Fatalf("expected error containing message %q but got no error", tc.wantErr)
			}
			diff := cmp.Diff(tc.want, network)
			if diff != "" {
				t.Errorf("Expected NetworkInfo ranges: %v, got ranges %v, diff: %s", tc.want, network, diff)
			}
		})
	}
}

type fakeCloud struct {
}

func (f fakeCloud) NetworkProjectID() string {
	return "test-project"
}

func (f fakeCloud) Region() string {
	return "test-region"
}

func (f fakeCloud) NetworkURL() string {
	return "https://www.googleapis.com/compute/v1/projects/test-project/global/networks/default"
}

func (f fakeCloud) SubnetworkURL() string {
	return "https://www.googleapis.com/compute/v1/projects/test-region/regions/test-region/subnetworks/secondary-subnet"
}

func (f fakeCloud) Get() string {
	return "https://www.googleapis.com/compute/v1/projects/test-project/global/networks/default"
}

func (f fakeCloud) GetNetwork(networkName string) (*compute.Network, error) {
	if networkName == "default" {
		network := compute.Network{
			Name:     "default",
			Id:       12345678910,
			SelfLink: "https://www.googleapis.com/compute/v1/projects/test-project/global/networks/default",
		}
		return &network, nil
	}
	return nil, fmt.Errorf("Invalid network name")
}

type fakeCloudWithNetworkId struct {
}

func (f fakeCloudWithNetworkId) NetworkProjectID() string {
	return "test-project"
}

func (f fakeCloudWithNetworkId) Region() string {
	return "test-region"
}

func (f fakeCloudWithNetworkId) NetworkURL() string {
	return "https://www.googleapis.com/compute/v1/projects/test-project/global/networks/12345678910"
}

func (f fakeCloudWithNetworkId) SubnetworkURL() string {
	return "https://www.googleapis.com/compute/v1/projects/test-region/regions/test-region/subnetworks/secondary-subnet"
}

func (f fakeCloudWithNetworkId) GetNetwork(networkName string) (*compute.Network, error) {
	if networkName == "12345678910" || networkName == "default" {
		network := compute.Network{
			Name:     "default",
			Id:       12345678910,
			SelfLink: "https://www.googleapis.com/compute/v1/projects/test-project/global/networks/default",
		}
		return &network, nil
	}
	return nil, fmt.Errorf("Invalid network name")
}

func TestServiceNetworkWithNetworkId(t *testing.T) {

	cases := []struct {
		desc                 string
		service              *apiv1.Service
		network              *networkv1.Network
		gkeNetworkParamSet   *networkv1.GKENetworkParamSet
		cloudProviderNerwork CloudNetworkProvider
		want                 *NetworkInfo
		wantErr              string
	}{
		{
			desc: "cluster with network name",
			service: &apiv1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: "testService"},
				Spec:       apiv1.ServiceSpec{},
			},
			cloudProviderNerwork: fakeCloud{},
			want:                 DefaultNetwork(fakeCloud{}),
		},
		{
			desc: "cluster with network Id",
			service: &apiv1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: "testService"},
				Spec:       apiv1.ServiceSpec{},
			},
			cloudProviderNerwork: fakeCloudWithNetworkId{},
			want:                 DefaultNetwork(fakeCloud{}),
		},
	}

	for _, tc := range cases {

		t.Run(tc.desc, func(t *testing.T) {

			adapter, err := NewAdapterNetworkSelfLink(fakeCloudWithNetworkId{})

			networkClient := netfake.NewSimpleClientset()

			networkInformer := informernetwork.NewNetworkInformer(networkClient, time.Minute*10, utils.NewNamespaceIndexer())
			gkeNetworkParamSetInformer := informergkenetworkparamset.NewGKENetworkParamSetInformer(networkClient, time.Minute*10, utils.NewNamespaceIndexer())

			networkIndexer := networkInformer.GetIndexer()
			gkeNetworkParamSetIndexer := gkeNetworkParamSetInformer.GetIndexer()

			if tc.network != nil {
				networkIndexer.Add(tc.network)
			}
			if tc.gkeNetworkParamSet != nil {
				gkeNetworkParamSetIndexer.Add(tc.gkeNetworkParamSet)
			}
			networksResolver := NewNetworksResolver(networkIndexer, gkeNetworkParamSetIndexer, adapter, true, klog.Background())
			network, err := networksResolver.ServiceNetwork(tc.service)
			if err != nil {
				if tc.wantErr == "" {
					t.Fatalf("determining network info returned an error, err=%v", err)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("expected error containing message %q but got error %v", tc.wantErr, err)
				}
			}
			if err == nil && tc.wantErr != "" {
				t.Fatalf("expected error containing message %q but got no error", tc.wantErr)
			}
			diff := cmp.Diff(tc.want, network)
			if diff != "" {
				t.Errorf("Expected NetworkInfo ranges: %v, got ranges %v, diff: %s", tc.want, network, diff)
			}
		})
	}
}

func testNetwork(name, gkeNetworkParamSetName string) *networkv1.Network {
	return &networkv1.Network{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: networkv1.NetworkSpec{
			Type: networkv1.L3NetworkType,
			ParametersRef: &networkv1.NetworkParametersReference{
				Group: networkingGKEGroup,
				Kind:  "GKENetworkParamSet",
				Name:  gkeNetworkParamSetName,
			},
		},
	}
}

func TestRefersGKENetworkParamSet(t *testing.T) {
	cases := []struct {
		desc string
		ref  *networkv1.NetworkParametersReference
		want bool
	}{
		{
			desc: "valid",
			ref: &networkv1.NetworkParametersReference{
				Group: networkingGKEGroup,
				Kind:  "GKENetworkParamSet",
				Name:  "test-params",
			},
			want: true,
		},
		{
			desc: "valid case insensitive kind",
			ref: &networkv1.NetworkParametersReference{
				Group: networkingGKEGroup,
				Kind:  "gKeNeTwOrkParamSet",
				Name:  "test-params",
			},
			want: true,
		},
		{
			desc: "nil ref",
			ref:  nil,
			want: false,
		},
		{
			desc: "invalid group",
			ref: &networkv1.NetworkParametersReference{
				Group: "somethingelse.k8s.io",
				Kind:  "GKENetworkParamSet",
				Name:  "test-params",
			},
			want: false,
		},
		{
			desc: "invalid kind",
			ref: &networkv1.NetworkParametersReference{
				Group: networkingGKEGroup,
				Kind:  "OtherParamSet",
				Name:  "test-params",
			},
			want: false,
		},
		{
			desc: "empty name",
			ref: &networkv1.NetworkParametersReference{
				Group: networkingGKEGroup,
				Kind:  "GKENetworkParamSet",
				Name:  "",
			},
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			got := refersGKENetworkParamSet(tc.ref)
			if tc.want != got {
				t.Errorf("refersGKENetworkParamSet(%+v) wanted %v but got %v", tc.ref, tc.want, got)
			}
		})
	}
}

func TestNodeIPForNetwork(t *testing.T) {
	cases := []struct {
		desc    string
		node    *apiv1.Node
		network string
		want    string
	}{
		{
			desc:    "no annotation",
			network: "test-network",
			node: &apiv1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "test-node", Annotations: map[string]string{}},
			},
			want: "",
		},
		{
			desc:    "annotation that has the network",
			network: "test-network",
			node: &apiv1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "test-node", Annotations: map[string]string{
					networkv1.NorthInterfacesAnnotationKey: northInterfacesAnnotation(t, networkv1.NorthInterfacesAnnotation{
						{
							Network:   "another-network",
							IpAddress: "10.0.0.1",
						},
						{
							Network:   "test-network",
							IpAddress: "192.168.0.1",
						},
					}),
				}},
			},
			want: "192.168.0.1",
		},
		{
			desc:    "annotation that does not have the network",
			network: "test-network",
			node: &apiv1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "test-node", Annotations: map[string]string{
					networkv1.NorthInterfacesAnnotationKey: northInterfacesAnnotation(t, networkv1.NorthInterfacesAnnotation{
						{
							Network:   "another-network",
							IpAddress: "10.0.0.1",
						},
						{
							Network:   "other-network",
							IpAddress: "192.168.0.1",
						},
					}),
				}},
			},
			want: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			got := GetNodeIPForNetwork(tc.node, tc.network)
			if tc.want != got {
				t.Errorf("GetNodeIPForNetwork(%+v, %q) wanted %v but got %v", tc.node, tc.network, tc.want, got)
			}
		})
	}
}

func TestIsConnectedToNetwork(t *testing.T) {
	cases := []struct {
		desc        string
		node        *apiv1.Node
		networkInfo NetworkInfo
		want        bool
	}{
		{
			desc: "no annotation",
			networkInfo: NetworkInfo{
				IsDefault:  false,
				K8sNetwork: "test-network",
			},
			node: &apiv1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "test-node", Annotations: map[string]string{}},
			},
			want: false,
		},
		{
			desc: "always connected to the default network",
			networkInfo: NetworkInfo{
				IsDefault:  true,
				K8sNetwork: "default",
			},
			node: &apiv1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "test-node", Annotations: map[string]string{}},
			},
			want: true,
		},
		{
			desc: "annotation that has the network",
			networkInfo: NetworkInfo{
				IsDefault:  false,
				K8sNetwork: "test-network",
			},
			node: &apiv1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "test-node", Annotations: map[string]string{
					networkv1.NorthInterfacesAnnotationKey: northInterfacesAnnotation(t, networkv1.NorthInterfacesAnnotation{
						{
							Network:   "default",
							IpAddress: "10.0.0.1",
						},
						{
							Network:   "test-network",
							IpAddress: "192.168.0.1",
						},
					}),
				}},
			},
			want: true,
		},
		{
			desc: "annotation that does not have the network",
			networkInfo: NetworkInfo{
				IsDefault:  false,
				K8sNetwork: "test-network",
			},
			node: &apiv1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "test-node", Annotations: map[string]string{
					networkv1.NorthInterfacesAnnotationKey: northInterfacesAnnotation(t, networkv1.NorthInterfacesAnnotation{
						{
							Network:   "default",
							IpAddress: "10.0.0.1",
						},
						{
							Network:   "other-network",
							IpAddress: "192.168.0.1",
						},
					}),
				}},
			},
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			got := tc.networkInfo.IsNodeConnected(tc.node)
			if tc.want != got {
				t.Errorf("IsConnectedToNetwork(%+v, %q) wanted %v but got %v", tc.node, tc.networkInfo.K8sNetwork, tc.want, got)
			}
		})
	}
}

func northInterfacesAnnotation(t *testing.T, annotation networkv1.NorthInterfacesAnnotation) string {
	annotationString, err := networkv1.MarshalNorthInterfacesAnnotation(annotation)
	if err != nil {
		t.Errorf("failed to marshal north interfaces annotation")
	}
	return annotationString
}

func testGKENetworkParamSet(name, vpc, subnet string) *networkv1.GKENetworkParamSet {
	return &networkv1.GKENetworkParamSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: networkv1.GKENetworkParamSetSpec{
			VPC:       vpc,
			VPCSubnet: subnet,
		},
	}
}
