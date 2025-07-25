/*
Copyright 2015 The Kubernetes Authors.

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

package firewalls

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	gcpfirewallv1 "github.com/GoogleCloudPlatform/gke-networking-api/apis/gcpfirewall/v1"
	"k8s.io/klog/v2"

	firewallclient "github.com/GoogleCloudPlatform/gke-networking-api/client/gcpfirewall/clientset/versioned/fake"
	"google.golang.org/api/compute/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/cloud-provider-gcp/providers/gce"
	test "k8s.io/ingress-gce/pkg/test"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/ingress-gce/pkg/utils/slice"
)

var defaultNamer = namer.NewNamer("ABC", "XYZ", klog.TODO())
var ruleName = defaultNamer.FirewallRule()
var srcRanges = gce.L7LoadBalancerSrcRanges()

func portRanges() []string {
	return []string{"20000-23000"}
}

func TestFirewallPoolSync(t *testing.T) {
	fwp := NewFakeFirewallsProvider(false, false)
	fp := NewFirewallPool(fwp, defaultNamer, srcRanges, portRanges(), klog.TODO())
	nodes := []string{"node-a", "node-b", "node-c"}
	if err := fp.Sync(nodes, nil, nil, true); err != nil {
		t.Fatal(err)
	}
	verifyFirewallRule(fwp, ruleName, nodes, srcRanges, portRanges(), t)

	fwClient := firewallclient.NewSimpleClientset()
	fcrp := NewFirewallCRPool(fwClient, fwp, defaultNamer, srcRanges, portRanges(), true, klog.TODO())
	if err := fcrp.Sync(nodes, nil, nil, true); err != nil {
		t.Fatal(err)
	}
	verifyFirewallCR(fwClient, ruleName, srcRanges, portRanges(), true, true, t)

}

func TestFirewallPoolSyncNodes(t *testing.T) {
	fwp := NewFakeFirewallsProvider(false, false)
	fwClient := firewallclient.NewSimpleClientset()
	fp := NewFirewallPool(fwp, defaultNamer, srcRanges, portRanges(), klog.TODO())
	fcrp := NewFirewallCRPool(fwClient, fwp, defaultNamer, srcRanges, portRanges(), true, klog.TODO())
	nodes := []string{"node-a", "node-b", "node-c"}

	if err := fp.Sync(nodes, nil, nil, true); err != nil {
		t.Fatal(err)
	}
	verifyFirewallRule(fwp, ruleName, nodes, srcRanges, portRanges(), t)
	if err := fcrp.Sync(nodes, nil, nil, true); err != nil {
		t.Fatal(err)
	}
	verifyFirewallCR(fwClient, ruleName, srcRanges, portRanges(), true, true, t)

	// Add nodes
	nodes = append(nodes, "node-d", "node-e")
	if err := fp.Sync(nodes, nil, nil, true); err != nil {
		t.Errorf("unexpected err when syncing firewall, err: %v", err)
	}
	verifyFirewallRule(fwp, ruleName, nodes, srcRanges, portRanges(), t)
	verifyFirewallCR(fwClient, ruleName, srcRanges, portRanges(), true, true, t)

	// Remove nodes
	nodes = []string{"node-a", "node-c"}
	if err := fp.Sync(nodes, nil, nil, true); err != nil {
		t.Errorf("unexpected err when syncing firewall, err: %v", err)
	}
	verifyFirewallRule(fwp, ruleName, nodes, srcRanges, portRanges(), t)
	verifyFirewallCR(fwClient, ruleName, srcRanges, portRanges(), true, true, t)
}

func TestFirewallPoolSyncSrcRanges(t *testing.T) {
	fwp := NewFakeFirewallsProvider(false, false)
	fwClient := firewallclient.NewSimpleClientset()
	fp := NewFirewallPool(fwp, defaultNamer, srcRanges, portRanges(), klog.TODO())
	fcrp := NewFirewallCRPool(fwClient, fwp, defaultNamer, srcRanges, portRanges(), true, klog.TODO())
	nodes := []string{"node-a", "node-b", "node-c"}

	if err := fp.Sync(nodes, nil, nil, true); err != nil {
		t.Fatal(err)
	}

	verifyFirewallRule(fwp, ruleName, nodes, srcRanges, portRanges(), t)

	if err := fcrp.Sync(nodes, nil, nil, true); err != nil {
		t.Fatal(err)
	}

	verifyFirewallCR(fwClient, ruleName, srcRanges, portRanges(), true, true, t)

	// Manually modify source ranges to bad values.
	f, _ := fwp.GetFirewall(ruleName)
	f.SourceRanges = []string{"A", "B", "C"}
	if err := fwp.UpdateFirewall(f); err != nil {
		t.Fatal(err)
	}

	if err := fp.Sync(nodes, nil, nil, true); err != nil {
		t.Errorf("unexpected err when syncing firewall, err: %v", err)
	}
	verifyFirewallRule(fwp, ruleName, nodes, srcRanges, portRanges(), t)
	verifyFirewallCR(fwClient, ruleName, srcRanges, portRanges(), true, true, t)
}

func TestFirewallPoolSyncPorts(t *testing.T) {
	fwp := NewFakeFirewallsProvider(false, false)
	fwClient := firewallclient.NewSimpleClientset()
	nodes := []string{"node-a", "node-b", "node-c"}
	emptyPortRanges := make([]string, 0)

	// Verify empty ports' list
	fp := NewFirewallPool(fwp, defaultNamer, srcRanges, emptyPortRanges, klog.TODO())
	fcrp := NewFirewallCRPool(fwClient, fwp, defaultNamer, srcRanges, emptyPortRanges, true, klog.TODO())

	if err := fp.Sync(nodes, nil, nil, true); err != nil {
		t.Fatal(err)
	}
	verifyFirewallRule(fwp, ruleName, nodes, srcRanges, emptyPortRanges, t)

	if err := fcrp.Sync(nodes, nil, nil, true); err != nil {
		t.Fatal(err)
	}
	verifyFirewallCR(fwClient, ruleName, srcRanges, emptyPortRanges, true, true, t)

	// Verify a preset ports' list
	fp = NewFirewallPool(fwp, defaultNamer, srcRanges, portRanges(), klog.TODO())
	fcrp = NewFirewallCRPool(fwClient, fwp, defaultNamer, srcRanges, portRanges(), true, klog.TODO())

	if err := fp.Sync(nodes, nil, nil, true); err != nil {
		t.Errorf("unexpected err when syncing firewall, err: %v", err)
	}
	verifyFirewallRule(fwp, ruleName, nodes, srcRanges, portRanges(), t)

	if err := fcrp.Sync(nodes, nil, nil, true); err != nil {
		t.Fatal(err)
	}
	verifyFirewallCR(fwClient, ruleName, srcRanges, portRanges(), true, true, t)

	// Manually modify port list to bad values.
	f, _ := fwp.GetFirewall(ruleName)
	f.Allowed[0].Ports[0] = "578"
	if err := fwp.UpdateFirewall(f); err != nil {
		t.Fatal(err)
	}

	// Expect firewall to be synced back to normal
	if err := fp.Sync(nodes, nil, nil, true); err != nil {
		t.Errorf("unexpected err when syncing firewall, err: %v", err)
	}
	verifyFirewallRule(fwp, ruleName, nodes, srcRanges, portRanges(), t)

	if err := fcrp.Sync(nodes, nil, nil, true); err != nil {
		t.Fatal(err)
	}
	verifyFirewallCR(fwClient, ruleName, srcRanges, portRanges(), true, true, t)

	// Verify additional ports are included
	negTargetports := []string{"80", "443", "8080"}
	if err := fp.Sync(nodes, negTargetports, nil, true); err != nil {
		t.Errorf("unexpected err when syncing firewall, err: %v", err)
	}
	verifyFirewallRule(fwp, ruleName, nodes, srcRanges, append(portRanges(), negTargetports...), t)

	if err := fcrp.Sync(nodes, negTargetports, nil, true); err != nil {
		t.Fatal(err)
	}
	verifyFirewallCR(fwClient, ruleName, srcRanges, append(portRanges(), negTargetports...), true, true, t)

	if err := fp.Sync(nodes, negTargetports, nil, false); err != nil {
		t.Errorf("unexpected err when syncing firewall, err: %v", err)
	}
	verifyFirewallRule(fwp, ruleName, nodes, srcRanges, negTargetports, t)

	if err := fcrp.Sync(nodes, negTargetports, nil, false); err != nil {
		t.Fatal(err)
	}
	verifyFirewallCR(fwClient, ruleName, srcRanges, negTargetports, true, true, t)
}

func TestFirewallCRSyncDryRun(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		desc      string
		dryRun    bool
		wantError bool
	}{
		{
			desc:      "dry run",
			dryRun:    true,
			wantError: false,
		},
		{
			desc:      "full mode",
			dryRun:    false,
			wantError: true,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			fwp := NewFakeFirewallsProvider(false, false)
			nodes := []string{"node-a", "node-b", "node-c"}

			fwClient := firewallclient.NewSimpleClientset()
			fcrp := NewFirewallCRPool(fwClient, fwp, defaultNamer, srcRanges, portRanges(), tc.dryRun, klog.TODO())
			// Create CR
			if err := fcrp.Sync(nodes, nil, nil, true); err != nil {
				t.Fatal(err)
			}
			verifyFirewallCR(fwClient, ruleName, srcRanges, portRanges(), true, tc.dryRun, t)

			markCRWithReconciliationError(fwClient, t)

			err := fcrp.Sync(nodes, nil, nil, true)
			if (err != nil) != tc.wantError {
				t.Errorf("Sync() = %v, want error %v", err, tc.wantError)
			}
			verifyFirewallCR(fwClient, ruleName, srcRanges, portRanges(), true, tc.dryRun, t)
		})
	}

}

func TestFirewallPoolSyncGetServerError(t *testing.T) {
	fwp := NewFakeFirewallsProvider(false, false)
	fp := NewFirewallPool(fwp, defaultNamer, srcRanges, portRanges(), klog.TODO())
	nodes := []string{"node-a", "node-b", "node-c"}
	// Sync to create the firewall.
	if err := fp.Sync(nodes, nil, nil, true); err != nil {
		t.Fatalf("expect Sync to return nil, but got err %v", err)
	}

	serverError := test.FakeGoogleAPIRequestServerError()
	fwp.getFirewallHook = func(name string) (*compute.Firewall, error) {
		return nil, serverError
	}
	// GetFirewall returns server errors, and Sync should not trigger CreateFirewall.
	// InternalServerError from GetFirewall should be received, instead of
	// StatusConflict error from CreateFirewall.
	err := fp.Sync(nodes, nil, nil, true)
	if !errors.Is(err, serverError) {
		t.Fatalf("expect Sync to return %v, but got %v", serverError, err)
	}
}

func TestFirewallPoolSyncRanges(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		desc             string
		additionalRanges []string
	}{
		{
			desc:             "Empty list",
			additionalRanges: []string{},
		},
		{
			desc:             "One additional Range",
			additionalRanges: []string{"10.128.0.0/24"},
		},
		{
			desc:             "Multiple ranges",
			additionalRanges: []string{"10.128.0.0/24", "10.132.0.0/24", "10.134.0.0/24"},
		},
		{
			desc:             "Duplicate ranges",
			additionalRanges: []string{"10.128.0.0/24", "10.132.0.0/24", "10.134.0.0/24", "10.132.0.0/24", "10.134.0.0/24"},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			fwp := NewFakeFirewallsProvider(false, false)
			fwClient := firewallclient.NewSimpleClientset()
			fp := NewFirewallPool(fwp, defaultNamer, srcRanges, portRanges(), klog.TODO())
			fcrp := NewFirewallCRPool(fwClient, fwp, defaultNamer, srcRanges, portRanges(), true, klog.TODO())
			nodes := []string{"node-a", "node-b", "node-c"}

			if err := fp.Sync(nodes, nil, tc.additionalRanges, true); err != nil {
				t.Fatalf("fp.Sync(%v, nil, %v) = %v; want nil", nodes, tc.additionalRanges, err)
			}

			resultRanges := append(srcRanges, tc.additionalRanges...)
			verifyFirewallRule(fwp, ruleName, nodes, resultRanges, portRanges(), t)

			if err := fcrp.Sync(nodes, nil, tc.additionalRanges, true); err != nil {
				t.Fatal(err)
			}
			verifyFirewallCR(fwClient, ruleName, resultRanges, portRanges(), true, true, t)
		})
	}
}

func TestFirewallPoolGC(t *testing.T) {
	fwp := NewFakeFirewallsProvider(false, false)
	fwClient := firewallclient.NewSimpleClientset()
	fp := NewFirewallPool(fwp, defaultNamer, srcRanges, portRanges(), klog.TODO())
	fcrp := NewFirewallCRPool(fwClient, fwp, defaultNamer, srcRanges, portRanges(), true, klog.TODO())
	nodes := []string{"node-a", "node-b", "node-c"}

	if err := fp.Sync(nodes, nil, nil, true); err != nil {
		t.Fatal(err)
	}
	verifyFirewallRule(fwp, ruleName, nodes, srcRanges, portRanges(), t)
	if err := fcrp.Sync(nodes, nil, nil, true); err != nil {
		t.Fatal(err)
	}
	verifyFirewallCR(fwClient, ruleName, srcRanges, portRanges(), true, true, t)

	if err := fp.GC(); err != nil {
		t.Fatal(err)
	}
	if err := fcrp.GC(); err != nil {
		t.Fatal(err)
	}

	f, err := fwp.GetFirewall(ruleName)
	if err == nil || f != nil {
		t.Fatalf("GetFirewall() = %v, %v, expected nil, (error)", f, err)
	}

	fw := fwClient.NetworkingV1().GCPFirewalls()
	_, err = fw.Get(context.TODO(), ruleName, metav1.GetOptions{})
	if !k8serrors.IsNotFound(err) {
		t.Fatalf("Expected error to be 'NotFound', but got: %v", err)
	}
}

// TestSyncOnXPNWithPermission tests that firewall sync continues to work when OnXPN=true
func TestSyncOnXPNWithPermission(t *testing.T) {
	// Fake XPN cluster with permission
	fwp := NewFakeFirewallsProvider(true, false)
	fp := NewFirewallPool(fwp, defaultNamer, srcRanges, portRanges(), klog.TODO())
	nodes := []string{"node-a", "node-b", "node-c"}

	if err := fp.Sync(nodes, nil, nil, true); err != nil {
		t.Errorf("unexpected err when syncing firewall, err: %v", err)
	}
	verifyFirewallRule(fwp, ruleName, nodes, srcRanges, portRanges(), t)
}

// TestSyncOnXPNReadOnly tests that controller behavior is accurate when the controller
// does not have permission to create/update/delete firewall rules.
// Specific errors should be returned.
func TestSyncXPNReadOnly(t *testing.T) {
	fwp := NewFakeFirewallsProvider(true, true)
	fp := NewFirewallPool(fwp, defaultNamer, srcRanges, portRanges(), klog.TODO())
	nodes := []string{"node-a", "node-b", "node-c"}

	err := fp.Sync(nodes, nil, nil, true)
	validateXPNError(err, "create", t)

	// Manually create the firewall
	expectedFirewall := &compute.Firewall{
		Name:         ruleName,
		SourceRanges: srcRanges,
		Network:      fwp.NetworkURL(),
		Allowed: []*compute.FirewallAllowed{
			{
				IPProtocol: "tcp",
				Ports:      portRanges(),
			},
		},
		TargetTags: nodes,
	}
	if err = fwp.doCreateFirewall(expectedFirewall); err != nil {
		t.Errorf("unexpected err when creating firewall, err: %v", err)
	}

	// Run sync again with same state - expect no event
	if err = fp.Sync(nodes, nil, nil, true); err != nil {
		t.Errorf("unexpected err when syncing firewall, err: %v", err)
	}

	nodes = append(nodes, "node-d")
	err = fp.Sync(nodes, nil, nil, true)
	validateXPNError(err, "update", t)

	err = fp.GC()
	validateXPNError(err, "delete", t)
}

func validateXPNError(err error, op string, t *testing.T) {
	fwErr, ok := err.(*FirewallXPNError)
	if !ok || !strings.Contains(fwErr.Message, op) {
		t.Errorf("Expected firewall sync error with a user message. Received err: %v", err)
		return
	}
	// Ensure that the error message and the source ranges are correct
	errString := fwErr.Error()
	if !strings.Contains(errString, "Firewall change required by security admin") {
		t.Errorf("XPN error does not contain the expected string, got '%s'", errString)
	}
	if op == "delete" {
		return
	}
	// Check source ranges for update/create operations.
	expectedSourceRanges := gce.L7LoadBalancerSrcRanges()
	incorrectSourceRanges := gce.L4LoadBalancerSrcRanges()
	// incorrectSourceRanges are those included for L4 LB but not L7 LB. These should not be present in the error
	// message emitted for L7 LB.
	for _, val := range expectedSourceRanges {
		incorrectSourceRanges = slice.RemoveString(incorrectSourceRanges, val, nil)
	}
	for _, val := range expectedSourceRanges {
		if !strings.Contains(errString, val) {
			t.Errorf("Expected source ranges '%s' in XPN error, Got '%s'", expectedSourceRanges, errString)
		}
	}
	for _, val := range incorrectSourceRanges {
		if strings.Contains(errString, val) {
			t.Errorf("Expected source ranges '%s' in XPN error, Got '%s'", expectedSourceRanges, errString)
		}
	}
}

func verifyFirewallCR(firewallclient *firewallclient.Clientset, ruleName string, sourceRanges, expectedPorts []string, crEnabled bool, dryRun bool, t *testing.T) {
	if !crEnabled {
		fw := firewallclient.NetworkingV1().GCPFirewalls()
		actualFW, _ := fw.Get(context.TODO(), ruleName, metav1.GetOptions{})
		if actualFW != nil {
			t.Errorf("firewallCR is disabled, should not generate firewall CR")
		}
		return
	}

	fw := firewallclient.NetworkingV1().GCPFirewalls()
	actualFW, err := fw.Get(context.TODO(), ruleName, metav1.GetOptions{})
	if err != nil {
		t.Errorf("could not get firewall CR, err %v", err)
	}

	if actualFW.Spec.Action != "ALLOW" {
		t.Errorf("Action isn't ALLOW")
	}

	if actualFW.Spec.Disabled != dryRun {
		t.Errorf("Spec.Disabled should equal {%v}", dryRun)
	}

	ports := sets.NewString(expectedPorts...)
	srcranges := sets.NewString(sourceRanges...)

	// Empty ports' list would mean that all protocols are permitted
	// (not only TCP)
	if len(actualFW.Spec.Ports) == 0 {
		t.Errorf("Empty list of allowed protocols is not permited")
	}

	actualPorts := sets.NewString()
	for _, protocolports := range actualFW.Spec.Ports {
		if protocolports.Protocol != "TCP" {
			t.Errorf("Protocol isn't TCP")
		}
		if protocolports.EndPort != nil {
			actualPorts.Insert(fmt.Sprintf("%d-%d", *protocolports.StartPort, *protocolports.EndPort))
		} else if protocolports.StartPort != nil {
			actualPorts.Insert(fmt.Sprintf("%d", *protocolports.StartPort))
		}

	}
	if !actualPorts.Equal(ports) {
		t.Errorf("actual Ports(%v) does not equal to expected Ports(%v)", actualPorts, ports)
	}

	actualSrcRanges := sets.NewString()
	for _, ipblock := range actualFW.Spec.Ingress.Source.IPBlocks {
		actualSrcRanges.Insert(string(ipblock))
	}

	if !actualSrcRanges.Equal(srcranges) {
		t.Errorf("actual SrcRanges(%v) does not equal to expected SrcRanges(%v)", actualSrcRanges, srcranges)
	}

}

func verifyFirewallRule(fwp *fakeFirewallsProvider, ruleName string, expectedNodes, sourceRanges, expectedPorts []string, t *testing.T) {
	// Verify firewall rule was created
	f, err := fwp.GetFirewall(ruleName)
	if err != nil {
		t.Errorf("could not retrieve firewall via cloud api, err %v", err)
	}

	if len(f.Allowed) != 1 || f.Allowed[0].IPProtocol != "tcp" {
		t.Errorf("allowed doesn't exist or isn't 'tcp'")
	}

	if !sets.NewString(f.Allowed[0].Ports...).Equal(sets.NewString(expectedPorts...)) {
		t.Errorf("allowed ports doesn't equal expected ports, Actual: %+v, Expected: %+v", f.Allowed[0].Ports, expectedPorts)
	}

	if !sets.NewString(f.TargetTags...).Equal(sets.NewString(expectedNodes...)) {
		t.Errorf("target tags doesn't equal expected target tags. Actual: %v, Expected: %v", f.TargetTags, expectedNodes)
	}

	if !sets.NewString(f.SourceRanges...).Equal(sets.NewString(sourceRanges...)) {
		t.Errorf("source CIDRs doesn't equal expected CIDRs. Actual: %v, Expected: %v", f.SourceRanges, sourceRanges)
	}
}

func markCRWithReconciliationError(firewallclient *firewallclient.Clientset, t *testing.T) {
	fw, _ := firewallclient.NetworkingV1().GCPFirewalls().Get(context.TODO(), ruleName, metav1.GetOptions{})
	if fw == nil {
		t.Errorf("firewallCR not found")
	}
	fw.Status.Conditions = []metav1.Condition{{Reason: string(gcpfirewallv1.FirewallRuleReasonSyncError)}}
	_, err := firewallclient.NetworkingV1().GCPFirewalls().Update(context.TODO(), fw, metav1.UpdateOptions{})
	if err != nil {
		t.Errorf("could not update firewall CR, err %v", err)
	}
}
