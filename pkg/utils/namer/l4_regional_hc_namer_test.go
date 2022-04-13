package namer

import (
	"testing"
)

// TestRegionalL4HealthCheckNamer verifies that all L4 health check names are of the expected length and format.
func TestRegionalL4HealthCheckNamer(t *testing.T) {
	longstring1 := "012345678901234567890123456789012345678901234567890123456789abc"
	longstring2 := "012345678901234567890123456789012345678901234567890123456789pqr"
	testCases := []struct {
		desc           string
		namespace      string
		name           string
		sharedHC       bool
		expectHcFwName string
		expectHcName   string
	}{
		{
			desc:           "simple case",
			namespace:      "namespace",
			name:           "name",
			sharedHC:       false,
			expectHcFwName: "k8s2-7kpbhpki-namespace-name-956p2p7x-fw",
			expectHcName:   "k8s2-7kpbhpki-namespace-name-956p2p7x",
		},
		{
			desc:           "simple case, shared healthcheck",
			namespace:      "namespace",
			name:           "name",
			sharedHC:       true,
			expectHcFwName: "k8s2-7kpbhpki-l4-shared-hc-fw-regional",
			expectHcName:   "k8s2-7kpbhpki-l4-shared-hc",
		},
		{
			desc:           "long svc and namespace name",
			namespace:      longstring1,
			name:           longstring2,
			sharedHC:       false,
			expectHcFwName: "k8s2-7kpbhpki-01234567890123456789-0123456789012345678-hwm40-fw",
			expectHcName:   "k8s2-7kpbhpki-01234567890123456789-0123456789012345678-hwm400mg",
		},
	}

	l4namer := NewL4Namer(kubeSystemUID, nil)
	newNamer := NewL4RegionalHealthCheckNamer(l4namer)
	for _, tc := range testCases {
		hcName, hcFwName := newNamer.L4HealthCheck(tc.namespace, tc.name, tc.sharedHC)
		if hcFwName != tc.expectHcFwName {
			t.Errorf("%s FirewallName For Healthcheck: got %q, want %q", tc.desc, hcFwName, tc.expectHcFwName)
		}
		if hcName != tc.expectHcName {
			t.Errorf("%s HealthCheckName: got %q, want %q", tc.desc, hcName, tc.expectHcName)
		}
	}
}
