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
		desc                    string
		namespace               string
		name                    string
		subnetName              string
		proto                   string
		sharedHC                bool
		expectFRName            string
		expectIPv6FRName        string
		expectNEGName           string
		expectNonDefaultNEGName string
		expectFWName            string
		expectIPv6FWName        string
		expectHcFwName          string
		expectIPv6HcFName       string
		expectHcName            string
	}{
		{
			desc:                    "simple case",
			namespace:               "namespace",
			name:                    "name",
			subnetName:              "subnet",
			proto:                   "TCP",
			sharedHC:                false,
			expectFRName:            "k8s2-tcp-7kpbhpki-namespace-name-956p2p7x",
			expectIPv6FRName:        "k8s2-tcp-7kpbhpki-namespace-name-956p2p7x-ipv6",
			expectNEGName:           "k8s2-7kpbhpki-namespace-name-956p2p7x",
			expectNonDefaultNEGName: "k8s2-7kpbhpki-namespace-name-185075-956p2p7x",
			expectFWName:            "k8s2-7kpbhpki-namespace-name-956p2p7x",
			expectIPv6FWName:        "k8s2-7kpbhpki-namespace-name-956p2p7x-ipv6",
			expectHcFwName:          "k8s2-7kpbhpki-namespace-name-956p2p7x-fw",
			expectIPv6HcFName:       "k8s2-7kpbhpki-namespace-name-956p2p7x-fw-ipv6",
			expectHcName:            "k8s2-7kpbhpki-namespace-name-956p2p7x",
		},
		{
			desc:                    "simple case, shared healthcheck",
			namespace:               "namespace",
			name:                    "name",
			subnetName:              "subnet",
			proto:                   "TCP",
			sharedHC:                true,
			expectFRName:            "k8s2-tcp-7kpbhpki-namespace-name-956p2p7x",
			expectIPv6FRName:        "k8s2-tcp-7kpbhpki-namespace-name-956p2p7x-ipv6",
			expectNEGName:           "k8s2-7kpbhpki-namespace-name-956p2p7x",
			expectNonDefaultNEGName: "k8s2-7kpbhpki-namespace-name-185075-956p2p7x",
			expectFWName:            "k8s2-7kpbhpki-namespace-name-956p2p7x",
			expectIPv6FWName:        "k8s2-7kpbhpki-namespace-name-956p2p7x-ipv6",
			expectHcFwName:          "k8s2-7kpbhpki-l4-shared-hc-fw",
			expectIPv6HcFName:       "k8s2-7kpbhpki-l4-shared-hc-fw-ipv6",
			expectHcName:            "k8s2-7kpbhpki-l4-shared-hc",
		},
		{
			desc:                    "long svc and namespace name",
			namespace:               longstring1,
			name:                    longstring2,
			subnetName:              "subnet",
			proto:                   "UDP",
			sharedHC:                false,
			expectFRName:            "k8s2-udp-7kpbhpki-012345678901234567-01234567890123456-hwm400mg",
			expectIPv6FRName:        "k8s2-udp-7kpbhpki-012345678901234567-01234567890123456-hwm-ipv6",
			expectNEGName:           "k8s2-7kpbhpki-01234567890123456789-0123456789012345678-hwm400mg",
			expectNonDefaultNEGName: "k8s2-7kpbhpki-0123456789012345-0123456789012345-185075-hwm400mg",
			expectFWName:            "k8s2-7kpbhpki-01234567890123456789-0123456789012345678-hwm400mg",
			expectIPv6FWName:        "k8s2-7kpbhpki-01234567890123456789-0123456789012345678-hwm-ipv6",
			expectHcFwName:          "k8s2-7kpbhpki-01234567890123456789-0123456789012345678-hwm40-fw",
			expectIPv6HcFName:       "k8s2-7kpbhpki-01234567890123456789-0123456789012345678--fw-ipv6",
			expectHcName:            "k8s2-7kpbhpki-01234567890123456789-0123456789012345678-hwm400mg",
		},
		{
			desc:                    "long svc and namespace name, shared healthcheck",
			namespace:               longstring1,
			name:                    longstring2,
			subnetName:              "subnet",
			proto:                   "UDP",
			sharedHC:                true,
			expectFRName:            "k8s2-udp-7kpbhpki-012345678901234567-01234567890123456-hwm400mg",
			expectIPv6FRName:        "k8s2-udp-7kpbhpki-012345678901234567-01234567890123456-hwm-ipv6",
			expectNEGName:           "k8s2-7kpbhpki-01234567890123456789-0123456789012345678-hwm400mg",
			expectNonDefaultNEGName: "k8s2-7kpbhpki-0123456789012345-0123456789012345-185075-hwm400mg",
			expectFWName:            "k8s2-7kpbhpki-01234567890123456789-0123456789012345678-hwm400mg",
			expectIPv6FWName:        "k8s2-7kpbhpki-01234567890123456789-0123456789012345678-hwm-ipv6",
			expectHcFwName:          "k8s2-7kpbhpki-l4-shared-hc-fw",
			expectIPv6HcFName:       "k8s2-7kpbhpki-l4-shared-hc-fw-ipv6",
			expectHcName:            "k8s2-7kpbhpki-l4-shared-hc",
		},
		{
			desc:                    "long subnet name",
			namespace:               "namespace",
			name:                    "name",
			subnetName:              longstring1,
			proto:                   "TCP",
			sharedHC:                false,
			expectFRName:            "k8s2-tcp-7kpbhpki-namespace-name-956p2p7x",
			expectIPv6FRName:        "k8s2-tcp-7kpbhpki-namespace-name-956p2p7x-ipv6",
			expectNEGName:           "k8s2-7kpbhpki-namespace-name-956p2p7x",
			expectNonDefaultNEGName: "k8s2-7kpbhpki-namespace-name-1fd834-956p2p7x",
			expectFWName:            "k8s2-7kpbhpki-namespace-name-956p2p7x",
			expectIPv6FWName:        "k8s2-7kpbhpki-namespace-name-956p2p7x-ipv6",
			expectHcFwName:          "k8s2-7kpbhpki-namespace-name-956p2p7x-fw",
			expectIPv6HcFName:       "k8s2-7kpbhpki-namespace-name-956p2p7x-fw-ipv6",
			expectHcName:            "k8s2-7kpbhpki-namespace-name-956p2p7x",
		},
	}

	newNamer := NewL4Namer(kubeSystemUID, nil)
	for _, tc := range testCases {
		frName := newNamer.L4ForwardingRule(tc.namespace, tc.name, strings.ToLower(tc.proto))
		ipv6FrName := newNamer.L4IPv6ForwardingRule(tc.namespace, tc.name, strings.ToLower(tc.proto))
		negName := newNamer.L4Backend(tc.namespace, tc.name)
		nonDefaultNegName := newNamer.NonDefaultSubnetNEG(tc.namespace, tc.name, tc.subnetName, 0) // Port is not used for L4 NEG
		fwName := newNamer.L4Firewall(tc.namespace, tc.name)
		ipv6FWName := newNamer.L4IPv6Firewall(tc.namespace, tc.name)
		hcName := newNamer.L4HealthCheck(tc.namespace, tc.name, tc.sharedHC)
		hcFwName := newNamer.L4HealthCheckFirewall(tc.namespace, tc.name, tc.sharedHC)
		ipv6hcFwName := newNamer.L4IPv6HealthCheckFirewall(tc.namespace, tc.name, tc.sharedHC)
		if len(frName) > maxResourceNameLength || len(ipv6FrName) > maxResourceNameLength || len(negName) > maxResourceNameLength || len(fwName) > maxResourceNameLength || len(ipv6FWName) > maxResourceNameLength || len(hcName) > maxResourceNameLength || len(hcFwName) > maxResourceNameLength || len(ipv6hcFwName) > maxResourceNameLength {
			t.Errorf("%s: got len(frName) == %v, got len(ipv6FrName) == %v, len(negName) == %v, len(fwName) == %v, len(ipv6FWName) == %v, len(hcName) == %v, len(hcFwName) == %v, len(ipv6hcFwName) == %v want <= %d", tc.desc, len(frName), len(ipv6FrName), len(negName), len(fwName), len(ipv6FWName), len(hcName), len(hcFwName), len(ipv6hcFwName), maxResourceNameLength)
		}
		if frName != tc.expectFRName {
			t.Errorf("%s ForwardingRuleName: got %q, want %q", tc.desc, frName, tc.expectFRName)
		}
		if ipv6FrName != tc.expectIPv6FRName {
			t.Errorf("%s IPv6 ForwardingRuleName: got %q, want %q", tc.desc, ipv6FrName, tc.expectIPv6FRName)
		}
		if negName != tc.expectNEGName {
			t.Errorf("%s VMIPNEGName: got %q, want %q", tc.desc, negName, tc.expectNEGName)
		}
		if nonDefaultNegName != tc.expectNonDefaultNEGName {
			t.Errorf("%s non default subnet VMIPNEGName: got %q, want %q", tc.desc, nonDefaultNegName, tc.expectNonDefaultNEGName)
		}
		if fwName != tc.expectFWName {
			t.Errorf("%s FirewallName: got %q, want %q", tc.desc, fwName, tc.expectFWName)
		}
		if ipv6FWName != tc.expectIPv6FWName {
			t.Errorf("%s IPv6 FirewallName: got %q, want %q", tc.desc, ipv6FWName, tc.expectIPv6FWName)
		}
		if hcFwName != tc.expectHcFwName {
			t.Errorf("%s FirewallName For Healthcheck: got %q, want %q", tc.desc, hcFwName, tc.expectHcFwName)
		}
		if ipv6hcFwName != tc.expectIPv6HcFName {
			t.Errorf("%s IPv6 FirewallName For Healthcheck: got %q, want %q", tc.desc, ipv6hcFwName, tc.expectIPv6HcFName)
		}
		if hcName != tc.expectHcName {
			t.Errorf("%s HealthCheckName: got %q, want %q", tc.desc, hcName, tc.expectHcName)
		}
	}
}
