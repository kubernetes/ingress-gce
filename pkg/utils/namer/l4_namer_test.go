package namer

import (
	"strings"
	"testing"
)

// TestL4Namer verifies that all L4 resource names are of the expected length and format.
func TestL4Namer(t *testing.T) {
	longstring1 := "012345678901234567890123456789012345678901234567890123456789abc"
	longstring2 := "012345678901234567890123456789012345678901234567890123456789pqr"
	testCases := []struct {
		desc           string
		namespace      string
		name           string
		proto          string
		sharedHC       bool
		expectFRName   string
		expectNEGName  string
		expectFWName   string
		expectHcFwName string
		expectHcName   string
	}{
		{
			"simple case",
			"namespace",
			"name",
			"TCP",
			false,
			"k8s2-tcp-7kpbhpki-namespace-name-956p2p7x",
			"k8s2-7kpbhpki-namespace-name-956p2p7x",
			"k8s2-7kpbhpki-namespace-name-956p2p7x",
			"k8s2-7kpbhpki-namespace-name-956p2p7x-fw",
			"k8s2-7kpbhpki-namespace-name-956p2p7x",
		},
		{
			"simple case, shared healthcheck",
			"namespace",
			"name",
			"TCP",
			true,
			"k8s2-tcp-7kpbhpki-namespace-name-956p2p7x",
			"k8s2-7kpbhpki-namespace-name-956p2p7x",
			"k8s2-7kpbhpki-namespace-name-956p2p7x",
			"k8s2-7kpbhpki-l4-shared-hc-fw",
			"k8s2-7kpbhpki-l4-shared-hc",
		},
		{
			"long svc and namespace name",
			longstring1,
			longstring2,
			"UDP",
			false,
			"k8s2-udp-7kpbhpki-012345678901234567-01234567890123456-hwm400mg",
			"k8s2-7kpbhpki-01234567890123456789-0123456789012345678-hwm400mg",
			"k8s2-7kpbhpki-01234567890123456789-0123456789012345678-hwm400mg",
			"k8s2-7kpbhpki-01234567890123456789-0123456789012345678-hwm40-fw",
			"k8s2-7kpbhpki-01234567890123456789-0123456789012345678-hwm400mg",
		},
		{
			"long svc and namespace name, shared healthcheck",
			longstring1,
			longstring2,
			"UDP",
			true,
			"k8s2-udp-7kpbhpki-012345678901234567-01234567890123456-hwm400mg",
			"k8s2-7kpbhpki-01234567890123456789-0123456789012345678-hwm400mg",
			"k8s2-7kpbhpki-01234567890123456789-0123456789012345678-hwm400mg",
			"k8s2-7kpbhpki-l4-shared-hc-fw",
			"k8s2-7kpbhpki-l4-shared-hc",
		},
	}

	newNamer := NewL4Namer(kubeSystemUID, nil)
	for _, tc := range testCases {
		frName := newNamer.L4ForwardingRule(tc.namespace, tc.name, strings.ToLower(tc.proto))
		negName := newNamer.L4Backend(tc.namespace, tc.name)
		fwName := newNamer.L4Firewall(tc.namespace, tc.name)
		hcName := newNamer.L4HealthCheck(tc.namespace, tc.name, tc.sharedHC)
		hcFwName := newNamer.L4HealthCheckFirewall(tc.namespace, tc.name, tc.sharedHC)
		if len(frName) > maxResourceNameLength || len(negName) > maxResourceNameLength || len(fwName) > maxResourceNameLength || len(hcName) > maxResourceNameLength || len(hcFwName) > maxResourceNameLength {
			t.Errorf("%s: got len(frName) == %v, len(negName) == %v, len(fwName) == %v, len(hcName) == %v, len(hcFwName) == %v want <= 63", tc.desc, len(frName), len(negName), len(fwName), len(hcName), len(hcFwName))
		}
		if frName != tc.expectFRName {
			t.Errorf("%s ForwardingRuleName: got %q, want %q", tc.desc, frName, tc.expectFRName)
		}
		if negName != tc.expectNEGName {
			t.Errorf("%s VMIPNEGName: got %q, want %q", tc.desc, negName, tc.expectFRName)
		}
		if fwName != tc.expectFWName {
			t.Errorf("%s FirewallName: got %q, want %q", tc.desc, fwName, tc.expectFWName)
		}
		if hcFwName != tc.expectHcFwName {
			t.Errorf("%s FirewallName For Healthcheck: got %q, want %q", tc.desc, hcFwName, tc.expectHcFwName)
		}
		if hcName != tc.expectHcName {
			t.Errorf("%s HealthCheckName: got %q, want %q", tc.desc, hcName, tc.expectHcName)
		}
	}
}
