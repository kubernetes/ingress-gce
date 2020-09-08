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
		desc          string
		namespace     string
		name          string
		proto         string
		expectFRName  string
		expectNEGName string
	}{
		{
			"simple case",
			"namespace",
			"name",
			"TCP",
			"k8s2-tcp-7kpbhpki-namespace-name-956p2p7x",
			"k8s2-7kpbhpki-namespace-name-956p2p7x",
		},
		{
			"long svc and namespace name",
			longstring1,
			longstring2,
			"UDP",
			"k8s2-udp-7kpbhpki-012345678901234567-01234567890123456-hwm400mg",
			"k8s2-7kpbhpki-01234567890123456789-0123456789012345678-hwm400mg",
		},
	}

	newNamer := NewL4Namer(kubeSystemUID, nil)
	for _, tc := range testCases {
		frName := newNamer.L4ForwardingRule(tc.namespace, tc.name, strings.ToLower(tc.proto))
		negName, ok := newNamer.VMIPNEG(tc.namespace, tc.name)
		if !ok {
			t.Errorf("Namer does not support VMIPNEG")
		}
		if len(frName) > 63 || len(negName) > 63 {
			t.Errorf("%s: got len(frName) == %v, len(negName) == %v, want <= 63", tc.desc, len(frName), len(negName))
		}
		if frName != tc.expectFRName {
			t.Errorf("%s ForwardingRuleName: got %q, want %q", tc.desc, frName, tc.expectFRName)
		}
		if negName != tc.expectNEGName {
			t.Errorf("%s VMIPNEGName: got %q, want %q", tc.desc, negName, tc.expectFRName)
		}
	}
}
