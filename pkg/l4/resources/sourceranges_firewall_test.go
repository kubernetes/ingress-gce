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
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"github.com/google/go-cmp/cmp"
	ga "google.golang.org/api/compute/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/ingress-gce/pkg/l4/annotations"
	"k8s.io/ingress-gce/pkg/test"
)

const (
	testIPv4SourceRange = "10.0.0.0/8"
	testIPv6SourceRange = "2001:db8::/32"

	// allowAllIPv6SourceRange is the default range used when a Service does
	// not set loadBalancerSourceRanges.
	allowAllIPv6SourceRange = "0::0/0"
)

func netLBNodesFirewallName(l4netlb *L4NetLB, ipFamily v1.IPFamily) string {
	if ipFamily == v1.IPv6Protocol {
		return l4netlb.namer.L4IPv6Firewall(l4netlb.Service.Namespace, l4netlb.Service.Name)
	}
	return l4netlb.namer.L4Firewall(l4netlb.Service.Namespace, l4netlb.Service.Name)
}

func ilbNodesFirewallName(l4 *L4, ipFamily v1.IPFamily) string {
	if ipFamily == v1.IPv6Protocol {
		return l4.namer.L4IPv6Firewall(l4.Service.Namespace, l4.Service.Name)
	}
	return l4.namer.L4Firewall(l4.Service.Namespace, l4.Service.Name)
}

func nodesFirewallAnnotationKey(ipFamily v1.IPFamily) string {
	if ipFamily == v1.IPv6Protocol {
		return annotations.FirewallRuleIPv6Key
	}
	return annotations.FirewallRuleKey
}

// verifyFirewallSourceRanges checks that the firewall rule exists and allows
// exactly the expected source ranges.
func verifyFirewallSourceRanges(t *testing.T, cloud *gce.Cloud, fwName string, wantSourceRanges []string) {
	t.Helper()

	firewall, err := cloud.GetFirewall(fwName)
	if err != nil {
		t.Fatalf("cloud.GetFirewall(%q) returned error %v, want nil", fwName, err)
	}
	if diff := cmp.Diff(wantSourceRanges, firewall.SourceRanges); diff != "" {
		t.Errorf("firewall %q source ranges mismatch (-want +got):\n%s", fwName, diff)
	}
}

// drainEvents returns every event recorded so far and empties the recorder.
func drainEvents(t *testing.T, recorder record.EventRecorder) []string {
	t.Helper()

	fakeRecorder, ok := recorder.(*record.FakeRecorder)
	if !ok {
		t.Fatalf("recorder is of type %T, want *record.FakeRecorder", recorder)
	}

	var events []string
	for {
		select {
		case event := <-fakeRecorder.Events:
			events = append(events, event)
		default:
			return events
		}
	}
}

func containsEventWithReason(events []string, reason string) bool {
	for _, event := range events {
		if strings.Contains(event, reason) {
			return true
		}
	}
	return false
}

// singleIPFamilySourceRangesTestCases covers dual-stack Services where
// loadBalancerSourceRanges contains CIDRs for only one IP family.
var singleIPFamilySourceRangesTestCases = []struct {
	desc string
	// sourceRanges is what the Service sets in loadBalancerSourceRanges. Every
	// CIDR belongs to configuredIPFamily.
	sourceRanges       []string
	configuredIPFamily v1.IPFamily
	// excludedIPFamily has no matching CIDR in sourceRanges.
	excludedIPFamily v1.IPFamily
}{
	{
		desc:               "only IPv4 source ranges",
		sourceRanges:       []string{testIPv4SourceRange},
		configuredIPFamily: v1.IPv4Protocol,
		excludedIPFamily:   v1.IPv6Protocol,
	},
	{
		desc:               "only IPv6 source ranges",
		sourceRanges:       []string{testIPv6SourceRange},
		configuredIPFamily: v1.IPv6Protocol,
		excludedIPFamily:   v1.IPv4Protocol,
	},
}

// TestNetLBSourceRangesSingleIPFamily verifies that a dual-stack NetLB Service
// with loadBalancerSourceRanges containing CIDRs of only one IP family does not
// create an allow firewall rule for the other family.
func TestNetLBSourceRangesSingleIPFamily(t *testing.T) {
	t.Parallel()

	for _, tc := range singleIPFamilySourceRangesTestCases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			nodeNames := []string{"test-node-1"}
			svc := test.NewL4NetLBRBSService(8080)
			svc.Spec.IPFamilies = []v1.IPFamily{v1.IPv4Protocol, v1.IPv6Protocol}
			svc.Spec.LoadBalancerSourceRanges = slices.Clone(tc.sourceRanges)
			l4netlb := mustSetupNetLBTestHandler(t, svc, nodeNames)

			result := l4netlb.EnsureFrontend(nodeNames, svc, time.Now())
			if result.Error != nil {
				t.Fatalf("EnsureFrontend() returned error %v, want nil", result.Error)
			}

			// The configured family gets an allow rule with the configured ranges.
			configuredFirewallName := netLBNodesFirewallName(l4netlb, tc.configuredIPFamily)
			verifyFirewallSourceRanges(t, l4netlb.cloud, configuredFirewallName, tc.sourceRanges)

			// The family with no matching CIDRs gets no allow rule.
			excludedFirewallName := netLBNodesFirewallName(l4netlb, tc.excludedIPFamily)
			if err := verifyFirewallNotExists(l4netlb.cloud, excludedFirewallName); err != nil {
				t.Errorf("%v, want no %s allow firewall when no %s CIDR is specified", err, tc.excludedIPFamily, tc.excludedIPFamily)
			}

			excludedAnnotationKey := nodesFirewallAnnotationKey(tc.excludedIPFamily)
			if got, ok := result.Annotations[excludedAnnotationKey]; ok {
				t.Errorf("result.Annotations[%q] = %q, want no annotation since no firewall was created", excludedAnnotationKey, got)
			}

			// A warning event is emitted for the family with no matching CIDRs.
			events := drainEvents(t, l4netlb.recorder)
			if !containsEventWithReason(events, noSourceRangesForIPFamilyReason) {
				t.Errorf("recorded events %v, want one with reason %q for %s", events, noSourceRangesForIPFamilyReason, tc.excludedIPFamily)
			}
		})
	}
}

// TestILBSourceRangesSingleIPFamily verifies the same behavior as
// TestNetLBSourceRangesSingleIPFamily for internal load balancers.
func TestILBSourceRangesSingleIPFamily(t *testing.T) {
	t.Parallel()

	for _, tc := range singleIPFamilySourceRangesTestCases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			nodeNames := []string{"test-node-1"}
			svc := test.NewL4ILBService(false, 8080)
			svc.Spec.IPFamilies = []v1.IPFamily{v1.IPv4Protocol, v1.IPv6Protocol}
			svc.Spec.LoadBalancerSourceRanges = slices.Clone(tc.sourceRanges)
			l4 := mustSetupILBTestHandler(t, svc, nodeNames)

			result := l4.EnsureInternalLoadBalancer(nodeNames, svc)
			if result.Error != nil {
				t.Fatalf("EnsureInternalLoadBalancer() returned error %v, want nil", result.Error)
			}

			configuredFirewallName := ilbNodesFirewallName(l4, tc.configuredIPFamily)
			verifyFirewallSourceRanges(t, l4.cloud, configuredFirewallName, tc.sourceRanges)

			excludedFirewallName := ilbNodesFirewallName(l4, tc.excludedIPFamily)
			if err := verifyFirewallNotExists(l4.cloud, excludedFirewallName); err != nil {
				t.Errorf("%v, want no %s allow firewall when no %s CIDR is specified", err, tc.excludedIPFamily, tc.excludedIPFamily)
			}

			excludedAnnotationKey := nodesFirewallAnnotationKey(tc.excludedIPFamily)
			if got, ok := result.Annotations[excludedAnnotationKey]; ok {
				t.Errorf("result.Annotations[%q] = %q, want no annotation since no firewall was created", excludedAnnotationKey, got)
			}

			events := drainEvents(t, l4.recorder)
			if !containsEventWithReason(events, noSourceRangesForIPFamilyReason) {
				t.Errorf("recorded events %v, want one with reason %q for %s", events, noSourceRangesForIPFamilyReason, tc.excludedIPFamily)
			}
		})
	}
}

// TestNetLBSourceRangesDeleteStaleIPv6AllowFirewall verifies that updating
// loadBalancerSourceRanges to contain only IPv4 CIDRs deletes an existing IPv6
// allow firewall rule from a previous sync.
func TestNetLBSourceRangesDeleteStaleIPv6AllowFirewall(t *testing.T) {
	t.Parallel()

	nodeNames := []string{"test-node-1"}
	svc := test.NewL4NetLBRBSService(8080)
	svc.Spec.IPFamilies = []v1.IPFamily{v1.IPv4Protocol, v1.IPv6Protocol}
	l4netlb := mustSetupNetLBTestHandler(t, svc, nodeNames)

	// Sync first with no loadBalancerSourceRanges so default allow rules are created.
	result := l4netlb.EnsureFrontend(nodeNames, svc, time.Now())
	if result.Error != nil {
		t.Fatalf("EnsureFrontend() returned error %v, want nil", result.Error)
	}
	svc.Annotations = result.Annotations

	ipv6FirewallName := netLBNodesFirewallName(l4netlb, v1.IPv6Protocol)
	verifyFirewallSourceRanges(t, l4netlb.cloud, ipv6FirewallName, []string{allowAllIPv6SourceRange})

	// Update loadBalancerSourceRanges to IPv4 CIDRs only.
	svc.Spec.LoadBalancerSourceRanges = []string{testIPv4SourceRange}
	result = l4netlb.EnsureFrontend(nodeNames, svc, time.Now())
	if result.Error != nil {
		t.Fatalf("EnsureFrontend() after updating source ranges returned error %v, want nil", result.Error)
	}

	if err := verifyFirewallNotExists(l4netlb.cloud, ipv6FirewallName); err != nil {
		t.Errorf("%v, want the stale IPv6 allow firewall to be deleted", err)
	}
	if got, ok := result.Annotations[annotations.FirewallRuleIPv6Key]; ok {
		t.Errorf("result.Annotations[%q] = %q, want the annotation to be dropped with the firewall", annotations.FirewallRuleIPv6Key, got)
	}
}

// TestILBSourceRangesDeleteStaleIPv6AllowFirewall verifies the same convergence
// behavior as TestNetLBSourceRangesDeleteStaleIPv6AllowFirewall for internal
// load balancers.
func TestILBSourceRangesDeleteStaleIPv6AllowFirewall(t *testing.T) {
	t.Parallel()

	nodeNames := []string{"test-node-1"}
	svc := test.NewL4ILBService(false, 8080)
	svc.Spec.IPFamilies = []v1.IPFamily{v1.IPv4Protocol, v1.IPv6Protocol}
	l4 := mustSetupILBTestHandler(t, svc, nodeNames)

	result := l4.EnsureInternalLoadBalancer(nodeNames, svc)
	if result.Error != nil {
		t.Fatalf("EnsureInternalLoadBalancer() returned error %v, want nil", result.Error)
	}
	svc.Annotations = result.Annotations

	ipv6FirewallName := ilbNodesFirewallName(l4, v1.IPv6Protocol)
	verifyFirewallSourceRanges(t, l4.cloud, ipv6FirewallName, []string{allowAllIPv6SourceRange})

	svc.Spec.LoadBalancerSourceRanges = []string{testIPv4SourceRange}
	result = l4.EnsureInternalLoadBalancer(nodeNames, svc)
	if result.Error != nil {
		t.Fatalf("EnsureInternalLoadBalancer() after updating source ranges returned error %v, want nil", result.Error)
	}

	if err := verifyFirewallNotExists(l4.cloud, ipv6FirewallName); err != nil {
		t.Errorf("%v, want the stale IPv6 allow firewall to be deleted", err)
	}
	if got, ok := result.Annotations[annotations.FirewallRuleIPv6Key]; ok {
		t.Errorf("result.Annotations[%q] = %q, want the annotation to be dropped with the firewall", annotations.FirewallRuleIPv6Key, got)
	}
}

// TestNetLBIPv6LongFormSourceRangesNoUpdateOnResync verifies that when a user
// specifies full (long-form) IPv6 CIDRs in loadBalancerSourceRanges and GCE
// stores the canonical shortened form (e.g. "fe80::/64"), subsequent resyncs
// treat them as equal and do not patch the firewall rule.
func TestNetLBIPv6LongFormSourceRangesNoUpdateOnResync(t *testing.T) {
	t.Parallel()

	nodeNames := []string{"test-node-1"}
	svc := test.NewL4NetLBRBSService(8080)
	svc.Spec.IPFamilies = []v1.IPFamily{v1.IPv4Protocol, v1.IPv6Protocol}
	longFormRanges := []string{
		testIPv4SourceRange,
		"fe80:0000:0000:0000:0000:0000:0000:0000/64",
		"2001:0db8:0000:0000:0000:0000:0000:0000/32",
	}
	svc.Spec.LoadBalancerSourceRanges = slices.Clone(longFormRanges)
	l4netlb := mustSetupNetLBTestHandler(t, svc, nodeNames)

	result := l4netlb.EnsureFrontend(nodeNames, svc, time.Now())
	if result.Error != nil {
		t.Fatalf("EnsureFrontend() returned error %v, want nil", result.Error)
	}
	svc.Annotations = result.Annotations

	ipv6FirewallName := netLBNodesFirewallName(l4netlb, v1.IPv6Protocol)
	verifyFirewallSourceRanges(t, l4netlb.cloud, ipv6FirewallName, []string{"fe80::/64", "2001:db8::/32"})

	// Fail if PatchFirewall is called during resync when GCE holds shortened IPv6 CIDRs
	// and the Service spec still holds long-form IPv6 CIDRs.
	mockGCE := l4netlb.cloud.Compute().(*cloud.MockGCE)
	mockGCE.MockFirewalls.PatchHook = func(ctx context.Context, key *meta.Key, obj *ga.Firewall, m *cloud.MockFirewalls, options ...cloud.Option) error {
		t.Errorf("Unexpected PatchFirewall call on resync for firewall %q with SourceRanges %v", key.Name, obj.SourceRanges)
		return nil
	}

	svc.Spec.LoadBalancerSourceRanges = slices.Clone(longFormRanges)
	resyncResult := l4netlb.EnsureFrontend(nodeNames, svc, time.Now())
	if resyncResult.Error != nil {
		t.Fatalf("EnsureFrontend() on resync returned error %v, want nil", resyncResult.Error)
	}
	if resyncResult.GCEResourceUpdate.WereAnyResourcesModified() {
		t.Errorf("GCEResourceUpdate.WereAnyResourcesModified() = true, want false on resync (%+v)", resyncResult.GCEResourceUpdate)
	}
}

// TestILBIPv6LongFormSourceRangesNoUpdateOnResync verifies the same canonical
// vs. long-form IPv6 source ranges resync behavior for internal load balancers.
func TestILBIPv6LongFormSourceRangesNoUpdateOnResync(t *testing.T) {
	t.Parallel()

	nodeNames := []string{"test-node-1"}
	svc := test.NewL4ILBService(false, 8080)
	svc.Spec.IPFamilies = []v1.IPFamily{v1.IPv4Protocol, v1.IPv6Protocol}
	longFormRanges := []string{
		testIPv4SourceRange,
		"fe80:0000:0000:0000:0000:0000:0000:0000/64",
		"2001:0db8:0000:0000:0000:0000:0000:0000/32",
	}
	svc.Spec.LoadBalancerSourceRanges = slices.Clone(longFormRanges)
	l4 := mustSetupILBTestHandler(t, svc, nodeNames)

	result := l4.EnsureInternalLoadBalancer(nodeNames, svc)
	if result.Error != nil {
		t.Fatalf("EnsureInternalLoadBalancer() returned error %v, want nil", result.Error)
	}
	svc.Annotations = result.Annotations

	ipv6FirewallName := ilbNodesFirewallName(l4, v1.IPv6Protocol)
	verifyFirewallSourceRanges(t, l4.cloud, ipv6FirewallName, []string{"fe80::/64", "2001:db8::/32"})

	mockGCE := l4.cloud.Compute().(*cloud.MockGCE)
	mockGCE.MockFirewalls.PatchHook = func(ctx context.Context, key *meta.Key, obj *ga.Firewall, m *cloud.MockFirewalls, options ...cloud.Option) error {
		t.Errorf("Unexpected PatchFirewall call on resync for firewall %q with SourceRanges %v", key.Name, obj.SourceRanges)
		return nil
	}

	svc.Spec.LoadBalancerSourceRanges = slices.Clone(longFormRanges)
	resyncResult := l4.EnsureInternalLoadBalancer(nodeNames, svc)
	if resyncResult.Error != nil {
		t.Fatalf("EnsureInternalLoadBalancer() on resync returned error %v, want nil", resyncResult.Error)
	}
	if resyncResult.ResourceUpdates.WereAnyResourcesModified() {
		t.Errorf("ResourceUpdates.WereAnyResourcesModified() = true, want false on resync (%+v)", resyncResult.ResourceUpdates)
	}
}
