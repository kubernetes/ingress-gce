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

package loadbalancers

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/api/compute/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/healthchecksl4"
	"k8s.io/ingress-gce/pkg/network"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/klog/v2"
)

const (
	nodeTag   = "gke-node-name"
	ipv4Range = "1.2.3.4"
	ipv6Range = "1:2:3:4:5:6::/96"
)

var nodeTags = []string{nodeTag}

func newL4NetLB(fakeGCE *gce.Cloud, svc *v1.Service, deny bool) *L4NetLB {
	namer := namer.NewL4Namer(kubeSystemUID, namer.NewNamer("uid1", "fw1", klog.TODO()))
	recorder := record.NewFakeRecorder(100)
	networkResolver := network.NewFakeResolver(network.DefaultNetwork(fakeGCE))

	l4netlbParams := &L4NetLBParams{
		Service:             svc,
		Cloud:               fakeGCE,
		Namer:               namer,
		Recorder:            recorder,
		NetworkResolver:     networkResolver,
		DualStackEnabled:    true,
		EnableMixedProtocol: true,
		UseNEGs:             true,
		UseDenyFirewalls:    deny,
	}

	l4 := NewL4NetLB(l4netlbParams, klog.TODO())
	l4.healthChecks = healthchecksl4.Fake(fakeGCE, l4netlbParams.Recorder)

	return l4
}

func initCloud() (*gce.Cloud, error) {
	fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())

	if err := fakeGCE.InsertInstance(fakeGCE.ProjectID(), fakeGCE.LocalZone(), &compute.Instance{
		Name: nodeTag,
		Tags: &compute.Tags{Items: []string{nodeTag}},
	}); err != nil {
		return nil, fmt.Errorf("fakeGCE.InsertInstance() returned error %w", err)
	}

	dualStackSubnetwork := &compute.Subnetwork{
		StackType:      "IPV4_IPV6",
		Ipv6AccessType: "EXTERNAL",
	}
	if err := fakeGCE.Compute().Subnetworks().Insert(context.TODO(), meta.RegionalKey("test-subnet", fakeGCE.Region()), dualStackSubnetwork); err != nil {
		return nil, fmt.Errorf("fakeGCE.Compute().Subnetworks().Insert() returned error %w", err)
	}

	// GCP assigns IP Addresses when creating Forwarding Rules
	mockGCE := fakeGCE.Compute().(*cloud.MockGCE)
	mockGCE.MockForwardingRules.InsertHook = func(ctx context.Context, key *meta.Key, obj *compute.ForwardingRule, m *cloud.MockForwardingRules, options ...cloud.Option) (bool, error) {
		m.Lock.Lock()
		defer m.Lock.Unlock()

		if obj.IPAddress != "" {
			return false, nil
		}

		obj.IPAddress = ipv4Range
		if obj.IpVersion == "IPV6" {
			obj.IPAddress = ipv6Range
		}

		return false, nil
	}

	return fakeGCE, nil
}

func assertDenyFirewallRules(cloud *gce.Cloud, annotations map[string]string, annotationKey string, want *compute.Firewall) error {
	fwName := want.Name
	got, err := cloud.GetFirewall(fwName)
	if err != nil {
		return fmt.Errorf("cloud.GetFirewall(%q) = %v, want nil", fwName, err)
	}
	cmpIgnore := cmpopts.IgnoreFields(compute.Firewall{}, "CreationTimestamp", "SelfLink")
	if diff := cmp.Diff(want, got, cmpIgnore); diff != "" {
		return fmt.Errorf("firewall %q diff (-want +got):\n%s", fwName, diff)
	}

	annotation, ok := annotations[annotationKey]
	if !ok {
		return fmt.Errorf("Service annotations = %v, want annotation %q", annotations, annotationKey)
	}
	if annotation != fwName {
		return fmt.Errorf("Service annotation %q = %q, want %q", annotationKey, annotation, fwName)
	}
	return nil
}

// TestCreateDeleteNetLB verifies that only required Deny Firewall Rules are present for each IP stack type.
// It also verifies that resources are cleaned up afterwards.
func TestCreateDeleteNetLB(t *testing.T) {
	// Arrange
	oldFlag := flags.F.EnablePinhole
	defer func() {
		flags.F.EnablePinhole = oldFlag
	}()
	flags.F.EnablePinhole = true

	type wants struct {
		firewall      *compute.Firewall
		annotationKey string
	}
	ipv4DenyFirewall := wants{
		annotationKey: "service.kubernetes.io/firewall-rule-deny",
		firewall:      testDenyIPv4(),
	}
	ipv6DenyFirewall := wants{
		annotationKey: "service.kubernetes.io/firewall-rule-deny-ipv6",
		firewall:      testDenyIPv6(),
	}

	testCases := []struct {
		desc              string
		svc               *v1.Service
		want              []wants
		dontWantFirewalls []string
	}{
		{
			desc:              "IPv4",
			svc:               testService(v1.IPv4Protocol),
			want:              []wants{ipv4DenyFirewall},
			dontWantFirewalls: []string{testDenyIPv6().Name},
		},
		{
			desc:              "IPv6",
			svc:               testService(v1.IPv6Protocol),
			want:              []wants{ipv6DenyFirewall},
			dontWantFirewalls: []string{testDenyIPv4().Name},
		},
		{
			desc: "dualstack",
			svc:  testService(v1.IPv4Protocol, v1.IPv6Protocol),
			want: []wants{ipv4DenyFirewall, ipv6DenyFirewall},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			// Arrange
			fakeGCE, err := initCloud()
			if err != nil {
				t.Fatalf("initCloud() returned error %v", err)
			}

			l4netlb := newL4NetLB(fakeGCE, tc.svc, true)

			// Act
			result := l4netlb.EnsureFrontend(nodeTags, tc.svc)

			// Assert
			if result.Error != nil {
				t.Errorf("l4netlb.EnsureFrontend() = %v, want nil", result.Error)
			}
			for _, want := range tc.want {
				if err := assertDenyFirewallRules(fakeGCE, result.Annotations, want.annotationKey, want.firewall); err != nil {
					t.Errorf("assertDenyFirewallRule(%q) = %v, want nil", want.firewall.Name, err)
				}
			}
			for _, fwName := range tc.dontWantFirewalls {
				if got, err := fakeGCE.GetFirewall(fwName); !utils.IsNotFoundError(err) || got != nil {
					t.Errorf("fakeGCE.GetFirewall(%q) = %v, %v, want nil, nil", fwName, got, err)
				}
			}

			// Act
			result = l4netlb.EnsureLoadBalancerDeleted(tc.svc)

			// Assert
			if result.Error != nil {
				t.Errorf("l4netlb.EnsureLoadBalancerDeleted() = %v, want nil", result.Error)
			}
			for _, want := range tc.want {
				if got, err := fakeGCE.GetFirewall(want.firewall.Name); !utils.IsNotFoundError(err) || got != nil {
					t.Errorf("fakeGCE.GetFirewall(%q) = %v, %v, want nil, nil", want.firewall.Name, got, err)
				}
			}
		})
	}
}

func TestUpdateFromMigrationNetLB(t *testing.T) {
	oldFlag := flags.F.EnablePinhole
	defer func() {
		flags.F.EnablePinhole = oldFlag
	}()
	flags.F.EnablePinhole = true

	// Arrange, create a service without the flag enabled
	fakeGCE, err := initCloud()
	if err != nil {
		t.Fatalf("initCloud() returned error %v", err)
	}
	svc := testService(v1.IPv4Protocol, v1.IPv6Protocol)

	denyFlag := false
	l4netlb := newL4NetLB(fakeGCE, svc, denyFlag)

	// Act
	result := l4netlb.EnsureFrontend(nodeTags, svc)
	if result.Error != nil {
		t.Fatalf("l4netlb.EnsureFrontend() = %v, want nil", result.Error)
	}

	// There shouldn't be any Deny rules without the flag enabled
	for _, fwName := range []string{testDenyIPv4().Name, testDenyIPv6().Name} {
		if got, err := fakeGCE.GetFirewall(fwName); !utils.IsNotFoundError(err) || got != nil {
			t.Errorf("fakeGCE.GetFirewall(%q) = %v, %v, want nil, nil", fwName, got, err)
		}
	}

	// Re-arrange with denyFlag enabled
	denyFlag = true
	l4netlb = newL4NetLB(fakeGCE, svc, denyFlag)

	// Act on existing resources
	result = l4netlb.EnsureFrontend(nodeTags, svc)

	// Assert
	if result.Error != nil {
		t.Errorf("l4netlb.EnsureFrontend() = %v, want nil", result.Error)
	}
	if err := assertDenyFirewallRules(fakeGCE, result.Annotations, "service.kubernetes.io/firewall-rule-deny", testDenyIPv4()); err != nil {
		t.Errorf("assertDenyFirewallRule(ipv4) = %v, want nil", err)
	}
	if err := assertDenyFirewallRules(fakeGCE, result.Annotations, "service.kubernetes.io/firewall-rule-deny-ipv6", testDenyIPv6()); err != nil {
		t.Errorf("assertDenyFirewallRule(ipv6) = %v, want nil", err)
	}
}

func TestDoNotCreateDenyIfAllowErrorsOut(t *testing.T) {
	oldFlag := flags.F.EnablePinhole
	defer func() {
		flags.F.EnablePinhole = oldFlag
	}()
	flags.F.EnablePinhole = true

	// Arrange
	fakeGCE, err := initCloud()
	if err != nil {
		t.Fatalf("initCloud() returned error %v", err)
	}

	svc := testService(v1.IPv4Protocol, v1.IPv6Protocol)
	l4netlb := newL4NetLB(fakeGCE, svc, true)

	mockGCE := fakeGCE.Compute().(*cloud.MockGCE)
	mockGCE.MockFirewalls.InsertError = map[meta.Key]error{
		*meta.RegionalKey("k8s2-axyqjz2d-default-external-vgyw2apb", ""):      fmt.Errorf("firewall 🔥 insert error ipv4"),
		*meta.RegionalKey("k8s2-axyqjz2d-default-external-vgyw2apb-ipv6", ""): fmt.Errorf("firewall 🔥 insert error ipv6"),
	}

	// Act
	result := l4netlb.EnsureFrontend(nodeTags, svc)

	// Assert
	if result.Error == nil {
		t.Fatal("l4netlb.EnsureFrontend().Error was unexpectedly nil, want Firewall error")
	}

	if !strings.Contains(result.Error.Error(), "firewall 🔥 insert error") {
		t.Fatalf("error doesn't include errors from the mock, something else might have failed: %v", result.Error)
	}

	if result.GCEResourceInError != annotations.FirewallRuleIPv6Resource { // IPv6 is provisioned second so it overwrites the IPv4 errors
		t.Errorf("result.GCEResourceInError = %q, want %q", result.GCEResourceInError, annotations.FirewallRuleResource)
	}

	// Assert that the Deny Firewall Rules weren't created
	for _, fwName := range []string{testDenyIPv4().Name, testDenyIPv6().Name} {
		if got, err := fakeGCE.GetFirewall(fwName); !utils.IsNotFoundError(err) || got != nil {
			t.Errorf("fakeGCE.GetFirewall(%q) = %v, %v, want nil, nil", fwName, got, err)
		}
	}
}

func TestDeleteAfterRollbackNetLB(t *testing.T) {
	oldFlag := flags.F.EnablePinhole
	defer func() {
		flags.F.EnablePinhole = oldFlag
	}()
	flags.F.EnablePinhole = true

	// Arrange
	fakeGCE, err := initCloud()
	if err != nil {
		t.Fatalf("initCloud() returned error %v", err)
	}

	svc := testService(v1.IPv4Protocol, v1.IPv6Protocol)

	// Arrange by provisioning resources with the Deny flag
	l4netlb := newL4NetLB(fakeGCE, svc, true)
	result := l4netlb.EnsureFrontend(nodeTags, svc)
	if result.Error != nil {
		t.Errorf("l4netlb.EnsureFrontend() = %v, want nil", result.Error)
	}

	if err := assertDenyFirewallRules(fakeGCE, result.Annotations, "service.kubernetes.io/firewall-rule-deny", testDenyIPv4()); err != nil {
		t.Errorf("assertDenyFirewallRule(ipv4) = %v, want nil", err)
	}
	if err := assertDenyFirewallRules(fakeGCE, result.Annotations, "service.kubernetes.io/firewall-rule-deny-ipv6", testDenyIPv6()); err != nil {
		t.Errorf("assertDenyFirewallRule(ipv6) = %v, want nil", err)
	}

	// Act: Disable the flag and reconcile
	l4netlb = newL4NetLB(fakeGCE, svc, false)
	result = l4netlb.EnsureFrontend(nodeTags, svc)
	if result.Error != nil {
		t.Errorf("l4netlb.EnsureFrontend() = %v, want nil", result.Error)
	}

	// Assert that Deny Forwarding Rules were cleaned up properly with flag disabled
	for _, fwName := range []string{testDenyIPv4().Name, testDenyIPv6().Name} {
		if got, err := fakeGCE.GetFirewall(fwName); !utils.IsNotFoundError(err) || got != nil {
			t.Errorf("fakeGCE.GetFirewall(%q) = %v, %v, want nil, nil", fwName, got, err)
		}
	}

	// Act: Remove the LB with flag disabled
	result = l4netlb.EnsureLoadBalancerDeleted(svc)
	if result.Error != nil {
		t.Errorf("l4netlb.EnsureLoadBalancerDeleted() = %v, want nil", result.Error)
	}

	// Assert that Deny Forwarding Rules were cleaned up properly with flag disabled
	for _, fwName := range []string{testDenyIPv4().Name, testDenyIPv6().Name} {
		if got, err := fakeGCE.GetFirewall(fwName); !utils.IsNotFoundError(err) || got != nil {
			t.Errorf("fakeGCE.GetFirewall(%q) = %v, %v, want nil, nil", fwName, got, err)
		}
	}
}

// testService returns a generic Multi Protocol service with specified IP Families
func testService(families ...v1.IPFamily) *v1.Service {
	return &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "external",
			Namespace: "default",
			Annotations: map[string]string{
				"networking.gke.io/load-balancer-subnet": "test-subnet",
			},
		},
		Spec: v1.ServiceSpec{
			Type: v1.ServiceTypeLoadBalancer,
			Ports: []v1.ServicePort{
				{
					Port:     int32(80),
					Protocol: v1.ProtocolTCP,
				},
				{
					Port:     int32(443),
					Protocol: v1.ProtocolTCP,
				},
				{
					Port:     int32(12345),
					Protocol: v1.ProtocolUDP,
				},
			},
			IPFamilies: families,
		},
	}
}

func testDenyIPv4() *compute.Firewall {
	return &compute.Firewall{
		Name: "k8s2-axyqjz2d-default-external-vgyw2apb-deny",
		Denied: []*compute.FirewallDenied{
			{
				IPProtocol: "all",
			},
		},
		Description:       `{"networking.gke.io/service-name":"default/external","networking.gke.io/api-version":"ga"}`,
		Priority:          1000,
		DestinationRanges: []string{ipv4Range},
		TargetTags:        []string{nodeTag},
	}
}

func testDenyIPv6() *compute.Firewall {
	return &compute.Firewall{
		Name: "k8s2-axyqjz2d-default-external-vgyw2apb-deny-ipv6",
		Denied: []*compute.FirewallDenied{
			{
				IPProtocol: "all",
			},
		},
		Description:       `{"networking.gke.io/service-name":"default/external","networking.gke.io/api-version":"ga"}`,
		Priority:          1000,
		DestinationRanges: []string{ipv6Range},
		TargetTags:        []string{nodeTag},
	}
}
