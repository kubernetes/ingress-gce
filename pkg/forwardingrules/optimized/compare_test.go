package optimized_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/forwardingrules"
	"k8s.io/ingress-gce/pkg/forwardingrules/optimized"
)

// TestCompare verifies Compare logic against a set of Forwarding Rules.
// Specific tests for equality and patchability are covered in corresponding unit tests.
func TestCompare(t *testing.T) {
	testCases := []struct {
		desc      string
		haveFRs   []*composite.ForwardingRule
		wantFRs   []*composite.ForwardingRule
		equal     optimized.Equal
		patchable optimized.Patchable

		wantCalls *optimized.APIOperations
	}{
		{
			desc: "nothing changed",
			haveFRs: []*composite.ForwardingRule{
				{
					Name:           "fr-1",
					BackendService: "https://compute.googleapis.com/projects/test-project/regions/us-central1/backendServices/bs-1",
				},
			},
			wantFRs: []*composite.ForwardingRule{
				{
					Name:           "fr-1",
					BackendService: "https://compute.googleapis.com/projects/test-project/regions/us-central1/backendServices/bs-1",
				},
			},
			equal:     forwardingrules.EqualIPv4,
			patchable: forwardingrules.PatchableIPv4,
			wantCalls: &optimized.APIOperations{},
		},
		{
			desc:    "simple create",
			haveFRs: nil,
			wantFRs: []*composite.ForwardingRule{
				{
					Name:           "fr-1",
					BackendService: "https://compute.googleapis.com/projects/test-project/regions/us-central1/backendServices/bs-1",
				},
			},
			equal:     forwardingrules.EqualIPv4,
			patchable: forwardingrules.PatchableIPv4,
			wantCalls: &optimized.APIOperations{
				Create: []*composite.ForwardingRule{
					{
						Name:           "fr-1",
						BackendService: "https://compute.googleapis.com/projects/test-project/regions/us-central1/backendServices/bs-1",
					},
				},
			},
		},
		{
			desc: "simple delete",
			haveFRs: []*composite.ForwardingRule{
				{
					Name:           "fr-1",
					BackendService: "https://compute.googleapis.com/projects/test-project/regions/us-central1/backendServices/bs-1",
				},
			},
			wantFRs:   nil,
			equal:     forwardingrules.EqualIPv4,
			patchable: forwardingrules.PatchableIPv4,
			wantCalls: &optimized.APIOperations{
				Delete: []string{"fr-1"},
			},
		},
		{
			desc: "simple patch",
			haveFRs: []*composite.ForwardingRule{
				{
					Name:           "fr-1",
					BackendService: "https://compute.googleapis.com/projects/test-project/regions/us-central1/backendServices/bs-1",
				},
			},
			wantFRs: []*composite.ForwardingRule{
				{
					Name:              "fr-1",
					AllowGlobalAccess: true,
					BackendService:    "https://compute.googleapis.com/projects/test-project/regions/us-central1/backendServices/bs-1",
				},
			},
			equal:     forwardingrules.EqualIPv4,
			patchable: forwardingrules.PatchableIPv4,
			wantCalls: &optimized.APIOperations{
				Update: []*composite.ForwardingRule{
					{
						Name:              "fr-1",
						AllowGlobalAccess: true,
					},
				},
			},
		},
		{
			desc: "recreate",
			haveFRs: []*composite.ForwardingRule{
				{
					Name:           "fr-1",
					IPProtocol:     "TCP",
					BackendService: "https://compute.googleapis.com/projects/test-project/regions/us-central1/backendServices/bs-1",
				},
			},
			wantFRs: []*composite.ForwardingRule{
				{
					Name:           "fr-1",
					IPProtocol:     "UDP",
					BackendService: "https://compute.googleapis.com/projects/test-project/regions/us-central1/backendServices/bs-1",
				},
			},
			equal:     forwardingrules.EqualIPv4,
			patchable: forwardingrules.PatchableIPv4,
			wantCalls: &optimized.APIOperations{
				Delete: []string{"fr-1"},
				Create: []*composite.ForwardingRule{
					{
						Name:           "fr-1",
						IPProtocol:     "UDP",
						BackendService: "https://compute.googleapis.com/projects/test-project/regions/us-central1/backendServices/bs-1",
					},
				},
			},
		},
		{
			desc: "multiple operations",
			haveFRs: []*composite.ForwardingRule{
				{
					Name:           "fr-delete",
					BackendService: "https://compute.googleapis.com/projects/test-project/regions/us-central1/backendServices/bs-1",
				},
				{
					Name:           "fr-update",
					BackendService: "https://compute.googleapis.com/projects/test-project/regions/us-central1/backendServices/bs-1",
				},
				{
					Name:           "fr-recreate",
					BackendService: "https://compute.googleapis.com/projects/test-project/regions/us-central1/backendServices/bs-1",
				},
			},
			wantFRs: []*composite.ForwardingRule{
				{
					Name:           "fr-create",
					BackendService: "https://compute.googleapis.com/projects/test-project/regions/us-central1/backendServices/bs-1",
				},
				{
					Name:              "fr-update",
					AllowGlobalAccess: true,
					BackendService:    "https://compute.googleapis.com/projects/test-project/regions/us-central1/backendServices/bs-1",
				},
				{
					Name:           "fr-recreate",
					IPProtocol:     "UDP",
					BackendService: "https://compute.googleapis.com/projects/test-project/regions/us-central1/backendServices/bs-1",
				},
			},
			equal:     forwardingrules.EqualIPv4,
			patchable: forwardingrules.PatchableIPv4,
			wantCalls: &optimized.APIOperations{
				Create: []*composite.ForwardingRule{
					{
						Name:           "fr-recreate",
						IPProtocol:     "UDP",
						BackendService: "https://compute.googleapis.com/projects/test-project/regions/us-central1/backendServices/bs-1",
					},
					{
						Name:           "fr-create",
						BackendService: "https://compute.googleapis.com/projects/test-project/regions/us-central1/backendServices/bs-1",
					},
				},
				Update: []*composite.ForwardingRule{
					{
						Name:              "fr-update",
						AllowGlobalAccess: true,
					},
				},
				Delete: []string{"fr-recreate", "fr-delete"},
			},
		},
	}
	for _, tC := range testCases {
		tC := tC
		t.Run(tC.desc, func(t *testing.T) {
			t.Parallel()

			// Act
			ops, err := optimized.Compare(tC.haveFRs, tC.wantFRs, tC.equal, tC.patchable)

			// Assert
			if err != nil {
				t.Fatalf("optimized.Compare() error = %v", err)
			}

			if diff := cmp.Diff(ops, tC.wantCalls); diff != "" {
				t.Errorf("want != got, (-want, +got):\n%s", diff)
			}
		})
	}
}
