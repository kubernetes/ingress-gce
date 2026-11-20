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

package resources

import (
	"context"
	"testing"
	"time"

	networkv1 "github.com/GoogleCloudPlatform/gke-networking-api/apis/network/v1"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/api/compute/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/ingress-gce/pkg/l4/annotations"
	"k8s.io/ingress-gce/pkg/l4/healthchecks"
	"k8s.io/ingress-gce/pkg/network"
	"k8s.io/ingress-gce/pkg/test"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
)

const (
	defaultNetworkURL      = "https://www.googleapis.com/compute/v1/projects/test-project/global/networks/default-vpc"
	defaultSubnetworkURL   = "https://www.googleapis.com/compute/v1/projects/test-project/regions/us-central1/subnetworks/default-subnet"
	secondaryNetworkName   = "secondary-net"
	secondaryNetworkURL    = "https://www.googleapis.com/compute/v1/projects/test-project/global/networks/secondary-vpc"
	secondarySubnetworkURL = "https://www.googleapis.com/compute/v1/projects/test-project/regions/us-central1/subnetworks/secondary-subnet"
)

type gceResourceNetworks struct {
	BackendServices map[string]string
	ForwardingRules map[string]forwardingRuleNetwork
	Firewalls       map[string]string
}

type forwardingRuleNetwork struct {
	Network    string
	Subnetwork string
}

func TestMultinetUsesNetworkInfo_NetLB(t *testing.T) {
	t.Parallel()

	const (
		ns   = "default"
		name = "multinet-netlb"
		uid  = types.UID("multinetnetlb")
	)
	l4Namer := namer.NewL4Namer(kubeSystemUID, nil)

	tcpPorts := []corev1.ServicePort{
		{Name: "tcp", Port: 8080, Protocol: corev1.ProtocolTCP},
	}
	mixedPorts := []corev1.ServicePort{
		{Name: "tcp", Port: 8080, Protocol: corev1.ProtocolTCP},
		{Name: "udp", Port: 53, Protocol: corev1.ProtocolUDP},
	}

	// External passthrough NetLB BackendServices and IPv4 ForwardingRules must
	// leave Network/Subnetwork empty rather than using the default VPC/subnet:
	// - BackendService.network is only valid for INTERNAL (or EXTERNAL with fastIpMove):
	//   https://cloud.google.com/compute/docs/reference/rest/v1/regionBackendServices#BackendService.FIELDS.network
	// - ForwardingRule.network is not used for external load balancing, and
	//   ForwardingRule.subnetwork is only used for internal LBs and external IPv6 NetLBs:
	//   https://cloud.google.com/compute/docs/reference/rest/v1/forwardingRules
	bsNetworks := map[string]string{
		l4Namer.L4Backend(ns, name): "",
	}
	ipv4Firewalls := map[string]string{
		l4Namer.L4Firewall(ns, name):                   secondaryNetworkURL,
		l4Namer.L4FirewallDeny(ns, name):               secondaryNetworkURL,
		l4Namer.L4HealthCheckFirewall(ns, name, false): secondaryNetworkURL,
	}
	ipv6Firewalls := map[string]string{
		l4Namer.L4IPv6Firewall(ns, name):                   secondaryNetworkURL,
		l4Namer.L4IPv6FirewallDeny(ns, name):               secondaryNetworkURL,
		l4Namer.L4IPv6HealthCheckFirewall(ns, name, false): secondaryNetworkURL,
	}
	dualStackFirewalls := map[string]string{
		l4Namer.L4Firewall(ns, name):                       secondaryNetworkURL,
		l4Namer.L4FirewallDeny(ns, name):                   secondaryNetworkURL,
		l4Namer.L4HealthCheckFirewall(ns, name, false):     secondaryNetworkURL,
		l4Namer.L4IPv6Firewall(ns, name):                   secondaryNetworkURL,
		l4Namer.L4IPv6FirewallDeny(ns, name):               secondaryNetworkURL,
		l4Namer.L4IPv6HealthCheckFirewall(ns, name, false): secondaryNetworkURL,
	}

	tests := []struct {
		name       string
		ipFamilies []corev1.IPFamily
		ports      []corev1.ServicePort
		useL3      bool
		want       gceResourceNetworks
	}{
		{
			name:       "IPv4",
			ipFamilies: []corev1.IPFamily{corev1.IPv4Protocol},
			ports:      tcpPorts,
			want: gceResourceNetworks{
				BackendServices: bsNetworks,
				ForwardingRules: map[string]forwardingRuleNetwork{
					"amultinetnetlb": {},
				},
				Firewalls: ipv4Firewalls,
			},
		},
		{
			name:       "IPv4_IPv6",
			ipFamilies: []corev1.IPFamily{corev1.IPv4Protocol, corev1.IPv6Protocol},
			ports:      tcpPorts,
			want: gceResourceNetworks{
				BackendServices: bsNetworks,
				ForwardingRules: map[string]forwardingRuleNetwork{
					"amultinetnetlb":      {},
					"amultinetnetlb-ipv6": {Subnetwork: secondarySubnetworkURL},
				},
				Firewalls: dualStackFirewalls,
			},
		},
		{
			name:       "IPv6",
			ipFamilies: []corev1.IPFamily{corev1.IPv6Protocol},
			ports:      tcpPorts,
			want: gceResourceNetworks{
				BackendServices: bsNetworks,
				ForwardingRules: map[string]forwardingRuleNetwork{
					"amultinetnetlb-ipv6": {Subnetwork: secondarySubnetworkURL},
				},
				Firewalls: ipv6Firewalls,
			},
		},
		{
			name:       "IPv4_MixedProtocol",
			ipFamilies: []corev1.IPFamily{corev1.IPv4Protocol},
			ports:      mixedPorts,
			useL3:      true,
			want: gceResourceNetworks{
				BackendServices: bsNetworks,
				ForwardingRules: map[string]forwardingRuleNetwork{
					l4Namer.L4ForwardingRule(ns, name, "l3"): {},
				},
				Firewalls: ipv4Firewalls,
			},
		},
		{
			name:       "IPv4_IPv6_MixedProtocol",
			ipFamilies: []corev1.IPFamily{corev1.IPv4Protocol, corev1.IPv6Protocol},
			ports:      mixedPorts,
			useL3:      true,
			want: gceResourceNetworks{
				BackendServices: bsNetworks,
				ForwardingRules: map[string]forwardingRuleNetwork{
					l4Namer.L4ForwardingRule(ns, name, "l3"):     {},
					l4Namer.L4IPv6ForwardingRule(ns, name, "l3"): {Subnetwork: secondarySubnetworkURL},
				},
				Firewalls: dualStackFirewalls,
			},
		},
		{
			name:       "IPv6_MixedProtocol",
			ipFamilies: []corev1.IPFamily{corev1.IPv6Protocol},
			ports:      mixedPorts,
			useL3:      true,
			want: gceResourceNetworks{
				BackendServices: bsNetworks,
				ForwardingRules: map[string]forwardingRuleNetwork{
					l4Namer.L4IPv6ForwardingRule(ns, name, "l3"): {Subnetwork: secondarySubnetworkURL},
				},
				Firewalls: ipv6Firewalls,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fakeGCE, nodeNames := mustSetupMultinetCloud(t, subnetExternalIPv6AccessType)
			svc := newMultinetService(ns, name, uid, tc.ipFamilies, tc.ports)
			svc.Spec.LoadBalancerClass = ptr.To(annotations.RegionalExternalLoadBalancerClass)

			recorder := record.NewFakeRecorder(100)
			l4NetLB := NewL4NetLB(&L4NetLBParams{
				Service:                      svc,
				Cloud:                        fakeGCE,
				Namer:                        l4Namer,
				Recorder:                     recorder,
				DualStackEnabled:             true,
				EnableMixedProtocol:          true,
				UseL3DefaultForMixedProtocol: true,
				UseDenyFirewalls:             true,
				UseNEGs:                      true,
				NetworkResolver:              network.NewFakeResolver(secondaryNetworkInfo()),
			}, klog.TODO())
			l4NetLB.healthChecks = healthchecks.Fake(fakeGCE, recorder)

			res := l4NetLB.EnsureFrontend(nodeNames, svc, time.Now())
			if res.Error != nil {
				t.Fatalf("EnsureFrontend(%v, %v) returned unexpected error: %v", tc.ipFamilies, tc.ports, res.Error)
			}

			got := resourceNetworks(fakeGCE)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("EnsureFrontend(%v, %v) resource networks mismatch (-want +got):\n%s", tc.ipFamilies, tc.ports, diff)
			}
		})
	}
}

func TestMultinetUsesNetworkInfo_ILB(t *testing.T) {
	t.Parallel()

	const (
		ns   = "default"
		name = "multinet-ilb"
		uid  = types.UID("multinetilb")
	)
	l4Namer := namer.NewL4Namer(kubeSystemUID, nil)

	tcpPorts := []corev1.ServicePort{
		{Name: "tcp", Port: 8080, Protocol: corev1.ProtocolTCP},
	}
	mixedPorts := []corev1.ServicePort{
		{Name: "tcp", Port: 8080, Protocol: corev1.ProtocolTCP},
		{Name: "udp", Port: 53, Protocol: corev1.ProtocolUDP},
	}

	bsNetworks := map[string]string{
		l4Namer.L4Backend(ns, name): secondaryNetworkURL,
	}
	secondaryFRNetwork := forwardingRuleNetwork{
		Network:    secondaryNetworkURL,
		Subnetwork: secondarySubnetworkURL,
	}
	ipv4Firewalls := map[string]string{
		l4Namer.L4Firewall(ns, name):                   secondaryNetworkURL,
		l4Namer.L4HealthCheckFirewall(ns, name, false): secondaryNetworkURL,
	}
	ipv6Firewalls := map[string]string{
		l4Namer.L4IPv6Firewall(ns, name):                   secondaryNetworkURL,
		l4Namer.L4IPv6HealthCheckFirewall(ns, name, false): secondaryNetworkURL,
	}
	dualStackFirewalls := map[string]string{
		l4Namer.L4Firewall(ns, name):                       secondaryNetworkURL,
		l4Namer.L4HealthCheckFirewall(ns, name, false):     secondaryNetworkURL,
		l4Namer.L4IPv6Firewall(ns, name):                   secondaryNetworkURL,
		l4Namer.L4IPv6HealthCheckFirewall(ns, name, false): secondaryNetworkURL,
	}

	tests := []struct {
		name       string
		ipFamilies []corev1.IPFamily
		ports      []corev1.ServicePort
		want       gceResourceNetworks
	}{
		{
			name:       "IPv4",
			ipFamilies: []corev1.IPFamily{corev1.IPv4Protocol},
			ports:      tcpPorts,
			want: gceResourceNetworks{
				BackendServices: bsNetworks,
				ForwardingRules: map[string]forwardingRuleNetwork{
					l4Namer.L4ForwardingRule(ns, name, "tcp"): secondaryFRNetwork,
				},
				Firewalls: ipv4Firewalls,
			},
		},
		{
			name:       "IPv4_IPv6",
			ipFamilies: []corev1.IPFamily{corev1.IPv4Protocol, corev1.IPv6Protocol},
			ports:      tcpPorts,
			want: gceResourceNetworks{
				BackendServices: bsNetworks,
				ForwardingRules: map[string]forwardingRuleNetwork{
					l4Namer.L4ForwardingRule(ns, name, "tcp"):     secondaryFRNetwork,
					l4Namer.L4IPv6ForwardingRule(ns, name, "tcp"): secondaryFRNetwork,
				},
				Firewalls: dualStackFirewalls,
			},
		},
		{
			name:       "IPv6",
			ipFamilies: []corev1.IPFamily{corev1.IPv6Protocol},
			ports:      tcpPorts,
			want: gceResourceNetworks{
				BackendServices: bsNetworks,
				ForwardingRules: map[string]forwardingRuleNetwork{
					l4Namer.L4IPv6ForwardingRule(ns, name, "tcp"): secondaryFRNetwork,
				},
				Firewalls: ipv6Firewalls,
			},
		},
		{
			name:       "IPv4_MixedProtocol",
			ipFamilies: []corev1.IPFamily{corev1.IPv4Protocol},
			ports:      mixedPorts,
			want: gceResourceNetworks{
				BackendServices: bsNetworks,
				ForwardingRules: map[string]forwardingRuleNetwork{
					l4Namer.L4ForwardingRule(ns, name, "l3"): secondaryFRNetwork,
				},
				Firewalls: ipv4Firewalls,
			},
		},
		{
			name:       "IPv4_IPv6_MixedProtocol",
			ipFamilies: []corev1.IPFamily{corev1.IPv4Protocol, corev1.IPv6Protocol},
			ports:      mixedPorts,
			want: gceResourceNetworks{
				BackendServices: bsNetworks,
				ForwardingRules: map[string]forwardingRuleNetwork{
					l4Namer.L4ForwardingRule(ns, name, "l3"):     secondaryFRNetwork,
					l4Namer.L4IPv6ForwardingRule(ns, name, "l3"): secondaryFRNetwork,
				},
				Firewalls: dualStackFirewalls,
			},
		},
		{
			name:       "IPv6_MixedProtocol",
			ipFamilies: []corev1.IPFamily{corev1.IPv6Protocol},
			ports:      mixedPorts,
			want: gceResourceNetworks{
				BackendServices: bsNetworks,
				ForwardingRules: map[string]forwardingRuleNetwork{
					l4Namer.L4IPv6ForwardingRule(ns, name, "l3"): secondaryFRNetwork,
				},
				Firewalls: ipv6Firewalls,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fakeGCE, nodeNames := mustSetupMultinetCloud(t, subnetInternalIPv6AccessType)
			svc := newMultinetService(ns, name, uid, tc.ipFamilies, tc.ports)
			svc.Annotations = map[string]string{gce.ServiceAnnotationLoadBalancerType: string(gce.LBTypeInternal)}

			recorder := record.NewFakeRecorder(100)
			l4 := NewL4Handler(&L4ILBParams{
				Service:             svc,
				Cloud:               fakeGCE,
				Namer:               l4Namer,
				Recorder:            recorder,
				DualStackEnabled:    true,
				EnableMixedProtocol: true,
				NetworkResolver:     network.NewFakeResolver(secondaryNetworkInfo()),
			}, klog.TODO())
			l4.healthChecks = healthchecks.Fake(fakeGCE, recorder)

			res := l4.EnsureInternalLoadBalancer(nodeNames, svc)
			if res.Error != nil {
				t.Fatalf("EnsureInternalLoadBalancer(%v, %v) returned unexpected error: %v", tc.ipFamilies, tc.ports, res.Error)
			}

			got := resourceNetworks(fakeGCE)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("EnsureInternalLoadBalancer(%v, %v) resource networks mismatch (-want +got):\n%s", tc.ipFamilies, tc.ports, diff)
			}
		})
	}
}

// mustSetupMultinetCloud creates a fake GCE environment whose default subnet is
// IPv4-only, while the secondary subnet supports dual-stack IPv6 with the given
// access type. Any fallback to the default subnet for IPv6 checks will fail.
func mustSetupMultinetCloud(t *testing.T, secondaryIPv6AccessType string) (*gce.Cloud, []string) {
	t.Helper()

	vals := gce.DefaultTestClusterValues()
	vals.NetworkURL = defaultNetworkURL
	vals.SubnetworkURL = defaultSubnetworkURL
	fakeGCE := getFakeGCECloud(vals)

	mockGCE := fakeGCE.Compute().(*cloud.MockGCE)
	ctx := context.Background()

	defaultSubnet := &compute.Subnetwork{
		Name:      "default-subnet",
		Network:   defaultNetworkURL,
		SelfLink:  defaultSubnetworkURL,
		StackType: "IPV4_ONLY",
	}
	if err := mockGCE.Subnetworks().Insert(ctx, meta.RegionalKey("default-subnet", fakeGCE.Region()), defaultSubnet); err != nil {
		t.Fatalf("Setup: failed to insert default subnet: %v", err)
	}

	secondarySubnet := &compute.Subnetwork{
		Name:           "secondary-subnet",
		Network:        secondaryNetworkURL,
		SelfLink:       secondarySubnetworkURL,
		StackType:      utils.DualStackSubnetStackType,
		Ipv6AccessType: secondaryIPv6AccessType,
	}
	if err := mockGCE.Subnetworks().Insert(ctx, meta.RegionalKey("secondary-subnet", fakeGCE.Region()), secondarySubnet); err != nil {
		t.Fatalf("Setup: failed to insert secondary subnet: %v", err)
	}

	nodeNames := []string{"test-node-1"}
	if _, err := test.CreateAndInsertNodes(fakeGCE, nodeNames, vals.ZoneName); err != nil {
		t.Fatalf("Setup: failed to insert nodes: %v", err)
	}

	return fakeGCE, nodeNames
}

func secondaryNetworkInfo() *network.NetworkInfo {
	return &network.NetworkInfo{
		IsDefault:     false,
		K8sNetwork:    secondaryNetworkName,
		NetworkURL:    secondaryNetworkURL,
		SubnetworkURL: secondarySubnetworkURL,
	}
}

func newMultinetService(ns, name string, uid types.UID, ipFamilies []corev1.IPFamily, ports []corev1.ServicePort) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			UID:       uid,
		},
		Spec: corev1.ServiceSpec{
			Type:                  corev1.ServiceTypeLoadBalancer,
			ExternalTrafficPolicy: corev1.ServiceExternalTrafficPolicyTypeCluster,
			IPFamilies:            ipFamilies,
			Ports:                 ports,
			Selector: map[string]string{
				networkv1.NetworkAnnotationKey: secondaryNetworkName,
			},
		},
	}
}

func resourceNetworks(gceCloud *gce.Cloud) gceResourceNetworks {
	mockGCE := gceCloud.Compute().(*cloud.MockGCE)
	got := gceResourceNetworks{
		BackendServices: make(map[string]string),
		ForwardingRules: make(map[string]forwardingRuleNetwork),
		Firewalls:       make(map[string]string),
	}
	for _, obj := range mockGCE.MockRegionBackendServices.Objects {
		bs := obj.ToGA()
		got.BackendServices[bs.Name] = bs.Network
	}
	for _, obj := range mockGCE.MockForwardingRules.Objects {
		fr := obj.ToGA()
		got.ForwardingRules[fr.Name] = forwardingRuleNetwork{
			Network:    fr.Network,
			Subnetwork: fr.Subnetwork,
		}
	}
	for _, obj := range mockGCE.MockFirewalls.Objects {
		fw := obj.ToGA()
		got.Firewalls[fw.Name] = fw.Network
	}
	return got
}
