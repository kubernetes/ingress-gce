/*
Copyright 2022 The Kubernetes Authors.

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
	"strings"
	"testing"
	"time"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/api/compute/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/l4/address"
	"k8s.io/ingress-gce/pkg/l4/forwardingrules"
	"k8s.io/ingress-gce/pkg/network"
	"k8s.io/ingress-gce/pkg/test"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/klog/v2"

	"k8s.io/cloud-provider-gcp/providers/gce"
	l4utils "k8s.io/ingress-gce/pkg/l4/utils"
)

func TestL4EnsureIPv6ForwardingRuleUpdate(t *testing.T) {
	serviceNamespace := "testNs"
	serviceName := "testSvc"
	l4namer := namer.NewL4Namer("test", namer.NewNamer("testCluster", "testFirewall", klog.TODO()))

	bsLink := "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1"
	networkURL := "https://www.googleapis.com/compute/v1/projects/test-poject/global/networks/test-vpc"
	subnetworkURL := "https://www.googleapis.com/compute/v1/projects/test-poject/regions/us-central1/subnetworks/default-subnet"

	testCases := []struct {
		desc         string
		svc          *corev1.Service
		namedAddress *compute.Address
		existingRule *composite.ForwardingRule
		wantRule     *composite.ForwardingRule
		wantUpdate   l4utils.ResourceSyncStatus
		wantErrMsg   string
	}{
		{
			desc: "create",
			svc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: serviceName, Namespace: serviceNamespace, UID: types.UID("1")},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Port:     8080,
							Protocol: corev1.ProtocolTCP,
						},
					},
					Type: "LoadBalancer",
				},
			},
			existingRule: nil,
			wantRule: &composite.ForwardingRule{
				Ports:               []string{"8080"},
				IPProtocol:          "TCP",
				IpVersion:           IPVersionIPv6,
				LoadBalancingScheme: string(cloud.SchemeInternal),
				NetworkTier:         cloud.NetworkTierDefault.ToGCEValue(),
				Version:             meta.VersionGA,
				BackendService:      bsLink,
				Description:         ipV6ForwardingRuleDescription(t, serviceNamespace, serviceName),
			},
			wantUpdate: l4utils.ResourceUpdate,
		},
		{
			desc: "no update",
			svc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: serviceName, Namespace: serviceNamespace, UID: types.UID("1")},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Port:     8080,
							Protocol: corev1.ProtocolTCP,
						},
					},
					Type: "LoadBalancer",
				},
			},
			existingRule: &composite.ForwardingRule{
				Ports:               []string{"8080"},
				IPProtocol:          "TCP",
				IpVersion:           IPVersionIPv6,
				LoadBalancingScheme: string(cloud.SchemeInternal),
				NetworkTier:         cloud.NetworkTierDefault.ToGCEValue(),
				Version:             meta.VersionGA,
				BackendService:      bsLink,
				Description:         ipV6ForwardingRuleDescription(t, serviceNamespace, serviceName),
			},
			wantRule: &composite.ForwardingRule{
				Ports:               []string{"8080"},
				IPProtocol:          "TCP",
				IpVersion:           IPVersionIPv6,
				LoadBalancingScheme: string(cloud.SchemeInternal),
				NetworkTier:         cloud.NetworkTierDefault.ToGCEValue(),
				Version:             meta.VersionGA,
				BackendService:      bsLink,
				Description:         ipV6ForwardingRuleDescription(t, serviceNamespace, serviceName),
			},
			wantUpdate: l4utils.ResourceResync,
		},
		{
			desc: "update ports",
			svc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: serviceName, Namespace: serviceNamespace, UID: types.UID("1")},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Port:     8080,
							Protocol: corev1.ProtocolTCP,
						},
						{
							Port:     8082,
							Protocol: corev1.ProtocolTCP,
						},
					},
					Type: "LoadBalancer",
				},
			},
			existingRule: &composite.ForwardingRule{
				Ports:               []string{"8080"},
				IPProtocol:          "TCP",
				IpVersion:           IPVersionIPv6,
				LoadBalancingScheme: string(cloud.SchemeInternal),
				NetworkTier:         cloud.NetworkTierDefault.ToGCEValue(),
				Version:             meta.VersionGA,
				BackendService:      bsLink,
				Description:         ipV6ForwardingRuleDescription(t, serviceNamespace, serviceName),
			},
			wantRule: &composite.ForwardingRule{
				Ports:               []string{"8080", "8082"},
				IPProtocol:          "TCP",
				IpVersion:           IPVersionIPv6,
				LoadBalancingScheme: string(cloud.SchemeInternal),
				NetworkTier:         cloud.NetworkTierDefault.ToGCEValue(),
				Version:             meta.VersionGA,
				BackendService:      bsLink,
				Description:         ipV6ForwardingRuleDescription(t, serviceNamespace, serviceName),
			},
			wantUpdate: l4utils.ResourceUpdate,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
			l4 := &L4{
				cloud:           fakeGCE,
				forwardingRules: forwardingrules.New(fakeGCE, meta.VersionGA, meta.Regional, klog.TODO()),
				namer:           l4namer,
				Service:         tc.svc,
				network: network.NetworkInfo{
					IsDefault:     true,
					NetworkURL:    networkURL,
					SubnetworkURL: subnetworkURL,
				},
				recorder: &record.FakeRecorder{},
			}
			tc.wantRule.Name = l4.getIPv6FRName()
			if tc.existingRule != nil {
				tc.existingRule.Name = l4.getIPv6FRName()
			}
			if tc.namedAddress != nil {
				fakeGCE.ReserveRegionAddress(tc.namedAddress, fakeGCE.Region())
			}
			fr, updated, err := l4.ensureIPv6ForwardingRule(bsLink, gce.ILBOptions{}, tc.existingRule, address.IPToUse{Reserved: true})

			if err != nil && tc.wantErrMsg == "" {
				t.Errorf("ensureIPv4ForwardingRule() err=%v", err)
			}
			if tc.wantErrMsg != "" {
				if err == nil {
					t.Errorf("ensureIPv4ForwardingRule() wanted error with msg=%q but got none", tc.wantErrMsg)
				} else if !strings.Contains(err.Error(), tc.wantErrMsg) {
					t.Errorf("ensureIPv4ForwardingRule() wanted error with msg=%q but got err=%v", tc.wantErrMsg, err)
				}
				return
			}
			if updated != tc.wantUpdate {
				t.Errorf("ensureIPv4ForwardingRule() wanted updated=%v but got=%v", tc.wantUpdate, updated)
			}

			if diff := cmp.Diff(tc.wantRule, fr, cmpopts.IgnoreFields(composite.ForwardingRule{}, "SelfLink", "Region", "Scope")); diff != "" {
				t.Errorf("ensureIPv4ForwardingRule() diff -want +got\n%v\n", diff)
			}
		})
	}
}

func ipV6ForwardingRuleDescription(t *testing.T, namespace, name string) string {
	t.Helper()
	description, err := (&utils.L4LBResourceDescription{ServiceName: utils.ServiceKeyFunc(namespace, name)}).Marshal()
	if err != nil {
		t.Errorf("failed to create forwarding rule description for service %s/%s", namespace, name)
	}
	return description
}

// TestL4NetLBIsIPv6ForwardingRuleUpToDate pins the conditions under which the NetLB
// IPv6 address reservation is skipped, in particular the ones that are not a plain
// difference between the existing and the wanted forwarding rule.
func TestL4NetLBIsIPv6ForwardingRuleUpToDate(t *testing.T) {
	t.Parallel()
	nodeNames := []string{"test-node-1"}

	svc := test.NewL4NetLBRBSDualStackService(v1.ProtocolTCP, []v1.IPFamily{v1.IPv6Protocol}, v1.ServiceExternalTrafficPolicyTypeCluster)
	l4NetLB := mustSetupNetLBTestHandler(t, svc, nodeNames)
	if result := l4NetLB.EnsureFrontend(nodeNames, svc, time.Now()); result.Error != nil {
		t.Fatalf("EnsureFrontend() returned error %v, want nil", result.Error)
	}

	existingFR, err := l4NetLB.forwardingRules.Get(l4NetLB.ipv6FRName())
	if err != nil || existingFR == nil {
		t.Fatalf("forwardingRules.Get(%q) = %v, %v, want the IPv6 forwarding rule", l4NetLB.ipv6FRName(), existingFR, err)
	}
	subnetworkURL, err := l4NetLB.ipv6SubnetURL()
	if err != nil {
		t.Fatalf("ipv6SubnetURL() returned error %v, want nil", err)
	}
	ipCollection, err := l4NetLB.ipv6IPCollection()
	if err != nil {
		t.Fatalf("ipv6IPCollection() returned error %v, want nil", err)
	}
	// l4NetLB.Service is svc itself, keep the original ports to restore them
	// after the cases that mutate them.
	originalPorts := svc.Spec.Ports

	// inputs of the predicate, as computed by ensureIPv6ForwardingRule on a resync.
	type inputs struct {
		existingFR, toDeleteFR *composite.ForwardingRule
		bsLink                 string
		ipv6                   string
		netTier                cloud.NetworkTier
		ports                  []v1.ServicePort
	}
	resyncInputs := func() inputs {
		return inputs{
			existingFR: existingFR,
			bsLink:     existingFR.BackendService,
			ipv6:       strings.Split(existingFR.IPAddress, "/")[0],
			netTier:    cloud.NetworkTierPremium,
			ports:      originalPorts,
		}
	}

	testCases := []struct {
		desc   string
		mutate func(in *inputs)
		want   bool
	}{
		{
			desc:   "ports change",
			mutate: func(in *inputs) { in.ports = []v1.ServicePort{{Port: 8081, Protocol: v1.ProtocolTCP}} },
			want:   false,
		},
		{
			// Runs after "ports change" to verify that the cases are isolated from each other.
			desc:   "unchanged service",
			mutate: func(in *inputs) {},
			want:   true,
		},
		{
			desc:   "no existing forwarding rule",
			mutate: func(in *inputs) { in.existingFR = nil },
			want:   false,
		},
		{
			desc:   "forwarding rule with the alternate single/mixed protocol name to delete",
			mutate: func(in *inputs) { in.toDeleteFR = existingFR },
			want:   false,
		},
		{
			desc:   "IP is reset",
			mutate: func(in *inputs) { in.ipv6 = "" },
			want:   false,
		},
		{
			desc:   "different IPv6 address requested",
			mutate: func(in *inputs) { in.ipv6 = "2001:db8::1" },
			want:   false,
		},
		{
			desc:   "network tier changes",
			mutate: func(in *inputs) { in.netTier = cloud.NetworkTierStandard },
			want:   false,
		},
		{
			desc:   "backend service changes",
			mutate: func(in *inputs) { in.bsLink = existingFR.BackendService + "-other" },
			want:   false,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			in := resyncInputs()
			tc.mutate(&in)
			l4NetLB.Service.Spec.Ports = in.ports
			defer func() { l4NetLB.Service.Spec.Ports = originalPorts }()

			if got := l4NetLB.isIPv6ForwardingRuleUpToDate(in.existingFR, in.toDeleteFR, in.bsLink, in.ipv6, subnetworkURL, ipCollection, in.netTier, l4NetLB.svcLogger); got != tc.want {
				t.Errorf("isIPv6ForwardingRuleUpToDate() = %v, want %v", got, tc.want)
			}
		})
	}
}
