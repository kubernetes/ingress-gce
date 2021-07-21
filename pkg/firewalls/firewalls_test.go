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
	"strings"
	"testing"

	"google.golang.org/api/compute/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/ingress-gce/pkg/utils/slice"
	"k8s.io/legacy-cloud-providers/gce"
)

var defaultNamer = namer.NewNamer("ABC", "XYZ")
var ruleName = defaultNamer.FirewallRule()

//var srcRanges = []string{"1.1.1.1/11", "2.2.2.2/22"}
var srcRanges = gce.L7LoadBalancerSrcRanges()

func portRanges() []string {
	return []string{"20000-23000"}
}

func TestFirewallPoolSync(t *testing.T) {
	fwp := NewFakeFirewallsProvider(false, false)
	fp := NewFirewallPool(fwp, defaultNamer, srcRanges, portRanges())
	nodes := []string{"node-a", "node-b", "node-c"}

	if err := fp.Sync(nodes, nil, nil, true); err != nil {
		t.Fatal(err)
	}
	verifyFirewallRule(fwp, ruleName, nodes, srcRanges, portRanges(), t)
}

func TestFirewallPoolSyncNodes(t *testing.T) {
	fwp := NewFakeFirewallsProvider(false, false)
	fp := NewFirewallPool(fwp, defaultNamer, srcRanges, portRanges())
	nodes := []string{"node-a", "node-b", "node-c"}

	if err := fp.Sync(nodes, nil, nil, true); err != nil {
		t.Fatal(err)
	}
	verifyFirewallRule(fwp, ruleName, nodes, srcRanges, portRanges(), t)

	// Add nodes
	nodes = append(nodes, "node-d", "node-e")
	if err := fp.Sync(nodes, nil, nil, true); err != nil {
		t.Errorf("unexpected err when syncing firewall, err: %v", err)
	}
	verifyFirewallRule(fwp, ruleName, nodes, srcRanges, portRanges(), t)

	// Remove nodes
	nodes = []string{"node-a", "node-c"}
	if err := fp.Sync(nodes, nil, nil, true); err != nil {
		t.Errorf("unexpected err when syncing firewall, err: %v", err)
	}
	verifyFirewallRule(fwp, ruleName, nodes, srcRanges, portRanges(), t)
}

func TestFirewallPoolSyncSrcRanges(t *testing.T) {
	fwp := NewFakeFirewallsProvider(false, false)
	fp := NewFirewallPool(fwp, defaultNamer, srcRanges, portRanges())
	nodes := []string{"node-a", "node-b", "node-c"}

	if err := fp.Sync(nodes, nil, nil, true); err != nil {
		t.Fatal(err)
	}
	verifyFirewallRule(fwp, ruleName, nodes, srcRanges, portRanges(), t)

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
}

func TestFirewallPoolSyncPorts(t *testing.T) {
	fwp := NewFakeFirewallsProvider(false, false)
	fp := NewFirewallPool(fwp, defaultNamer, srcRanges, portRanges())
	nodes := []string{"node-a", "node-b", "node-c"}

	if err := fp.Sync(nodes, nil, nil, true); err != nil {
		t.Fatal(err)
	}
	verifyFirewallRule(fwp, ruleName, nodes, srcRanges, portRanges(), t)

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

	// Verify additional ports are included
	negTargetports := []string{"80", "443", "8080"}
	if err := fp.Sync(nodes, negTargetports, nil, true); err != nil {
		t.Errorf("unexpected err when syncing firewall, err: %v", err)
	}
	verifyFirewallRule(fwp, ruleName, nodes, srcRanges, append(portRanges(), negTargetports...), t)

	if err := fp.Sync(nodes, negTargetports, nil, false); err != nil {
		t.Errorf("unexpected err when syncing firewall, err: %v", err)
	}
	verifyFirewallRule(fwp, ruleName, nodes, srcRanges, negTargetports, t)
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
			fp := NewFirewallPool(fwp, defaultNamer, srcRanges, portRanges())
			nodes := []string{"node-a", "node-b", "node-c"}

			if err := fp.Sync(nodes, nil, tc.additionalRanges, true); err != nil {
				t.Fatalf("fp.Sync(%v, nil, %v) = %v; want nil", nodes, tc.additionalRanges, err)
			}

			resultRanges := append(srcRanges, tc.additionalRanges...)
			verifyFirewallRule(fwp, ruleName, nodes, resultRanges, portRanges(), t)
		})
	}
}

func TestFirewallPoolGC(t *testing.T) {
	fwp := NewFakeFirewallsProvider(false, false)
	fp := NewFirewallPool(fwp, defaultNamer, srcRanges, portRanges())
	nodes := []string{"node-a", "node-b", "node-c"}

	if err := fp.Sync(nodes, nil, nil, true); err != nil {
		t.Fatal(err)
	}
	verifyFirewallRule(fwp, ruleName, nodes, srcRanges, portRanges(), t)

	if err := fp.GC(); err != nil {
		t.Fatal(err)
	}

	f, err := fwp.GetFirewall(ruleName)
	if err == nil || f != nil {
		t.Fatalf("GetFirewall() = %v, %v, expected nil, (error)", f, err)
	}
}

// TestSyncOnXPNWithPermission tests that firewall sync continues to work when OnXPN=true
func TestSyncOnXPNWithPermission(t *testing.T) {
	// Fake XPN cluster with permission
	fwp := NewFakeFirewallsProvider(true, false)
	fp := NewFirewallPool(fwp, defaultNamer, srcRanges, portRanges())
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
	fp := NewFirewallPool(fwp, defaultNamer, srcRanges, portRanges())
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

func verifyFirewallRule(fwp *fakeFirewallsProvider, ruleName string, expectedNodes, expectedCIDRs, expectedPorts []string, t *testing.T) {
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
		t.Errorf("target tags doesn't equal expected taget tags. Actual: %v, Expected: %v", f.TargetTags, expectedNodes)
	}

	if !sets.NewString(f.SourceRanges...).Equal(sets.NewString(expectedCIDRs...)) {
		t.Errorf("source CIDRs doesn't equal expected CIDRs. Actual: %v, Expected: %v", f.SourceRanges, expectedCIDRs)
	}
}
