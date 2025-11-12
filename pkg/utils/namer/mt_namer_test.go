package namer

import (
	"fmt"
	"testing"

	"k8s.io/klog/v2"
)

func TestMTNamer(t *testing.T) {
	testCases := []struct {
		desc            string
		tenantUID       string
		clusterFirewall string
		namespace    string
		name         string
		port         int32
		expectedNeg  string
		expectedPrefix string
	}{
		{
			desc:              "UID with dashes",
			tenantUID:         "tenant-a-123",
			clusterFirewall:   "",
			namespace:         "ns-a",
			name:              "service-a",
			port:              80,
			expectedNegPrefix: "gk3-mt1-tenant-a-neg-ns-a-service-a-80-",
			expectedPrefix:    "gk3-mt1-tenant-a-",
		},
		{
			desc:              "UID without dashes",
			tenantUID:         "tenanta123",
			clusterFirewall:   "",
			namespace:         "ns-a",
			name:              "service-a",
			port:              80,
			expectedNegPrefix: "gk3-mt1-tenanta1-neg-ns-a-service-a-80-",
			expectedPrefix:    "gk3-mt1-tenanta1-",
		},
		{
			desc:              "long tenant uid",
			tenantUID:         "long-tenant-uid-that-is-longer-than-8-chars",
			clusterFirewall:   "",
			namespace:         "ns-b",
			name:              "service-b",
			port:              8080,
			expectedNegPrefix: "gk3-mt1-long-ten-neg-ns-b-service-b-8080-",
			expectedPrefix:    "gk3-mt1-long-ten-",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			namer := NewMTNamer(tc.tenantUID, tc.clusterFirewall, klog.TODO())

			// 1. Verify the Prefix 
			if !strings.HasPrefix(namer.prefix, tc.expectedPrefix) {
				t.Errorf("NewMTNamer() prefix = %q, want start with %q", namer.prefix, tc.expectedPrefix)
			}

			// 2. Verify the Principal Entity UID (Tenant ID)
			if namer.UID() != tc.tenantUID {
				t.Errorf("namer.UID() = %q, want %q", namer.UID(), tc.tenantUID)
			}

			// 3. Verify NEG generation
			negName := namer.NEG(tc.namespace, tc.name, tc.port)
			
			// Check the structure (Prefix + Content)
			if !strings.HasPrefix(negName, tc.expectedNegPrefix) {
				t.Errorf("namer.NEG() start mismatch.\nGot:  %q\nWant: %q...", negName, tc.expectedNegPrefix)
			}

			// 4. Verify Ownership Association
			if !namer.NameBelongsToEntity(negName) {
				t.Errorf("namer.NameBelongsToEntity(%q) returned false, want true", negName)
			}
		})
	}
}
