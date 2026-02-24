package forwardingrules_test

import (
	"testing"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/google/go-cmp/cmp"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/l4/forwardingrules"
)

func TestPatchableIPv4(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		desc string

		existing *composite.ForwardingRule
		new      *composite.ForwardingRule

		filtered  *composite.ForwardingRule
		patchable bool
	}{
		{
			desc: "AllowGlobalAccess true -> false",
			existing: &composite.ForwardingRule{
				Id:                  123,
				Name:                "tcp-fwd-rule",
				AllowGlobalAccess:   true,
				IPAddress:           "10.0.0.0",
				Ports:               []string{"123"},
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeInternal),
				BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
				Network:             "https://www.googleapis.com/compute/v1/projects/test-poject/global/networks/test-vpc",
				Subnetwork:          "https://www.googleapis.com/compute/v1/projects/test-poject/regions/us-central1/subnetworks/default-subnet",
			},
			new: &composite.ForwardingRule{
				Name:                "tcp-fwd-rule",
				IPAddress:           "10.0.0.0",
				Ports:               []string{"123"},
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeInternal),
				BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
				Network:             "https://www.googleapis.com/compute/v1/projects/test-poject/global/networks/test-vpc",
				Subnetwork:          "https://www.googleapis.com/compute/v1/projects/test-poject/regions/us-central1/subnetworks/default-subnet",
			},
			filtered: &composite.ForwardingRule{
				Id:                123,
				Name:              "tcp-fwd-rule",
				AllowGlobalAccess: false,
			},
			patchable: true,
		},
		{
			desc: "AllowGlobalAccess false -> true",
			existing: &composite.ForwardingRule{
				Id:   123,
				Name: "tcp-fwd-rule",

				IPAddress:           "10.0.0.0",
				Ports:               []string{"123"},
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeInternal),
				BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
				Network:             "https://www.googleapis.com/compute/v1/projects/test-poject/global/networks/test-vpc",
				Subnetwork:          "https://www.googleapis.com/compute/v1/projects/test-poject/regions/us-central1/subnetworks/default-subnet",
			},
			new: &composite.ForwardingRule{
				Name:              "tcp-fwd-rule",
				AllowGlobalAccess: true,

				IPAddress:           "10.0.0.0",
				Ports:               []string{"123"},
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeInternal),
				BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
				Network:             "https://www.googleapis.com/compute/v1/projects/test-poject/global/networks/test-vpc",
				Subnetwork:          "https://www.googleapis.com/compute/v1/projects/test-poject/regions/us-central1/subnetworks/default-subnet",
			},
			filtered: &composite.ForwardingRule{
				Id:                123,
				Name:              "tcp-fwd-rule",
				AllowGlobalAccess: true,
			},
			patchable: true,
		},
		{
			desc: "NetworkTier STANDARD -> PREMIUM",
			existing: &composite.ForwardingRule{
				Id:                123,
				Name:              "tcp-fwd-rule",
				AllowGlobalAccess: true,
				NetworkTier:       cloud.NetworkTierStandard.ToGCEValue(),

				IPAddress:           "10.0.0.0",
				Ports:               []string{"123"},
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeInternal),
				BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
				Network:             "https://www.googleapis.com/compute/v1/projects/test-poject/global/networks/test-vpc",
				Subnetwork:          "https://www.googleapis.com/compute/v1/projects/test-poject/regions/us-central1/subnetworks/default-subnet",
			},
			new: &composite.ForwardingRule{
				Name:              "tcp-fwd-rule",
				AllowGlobalAccess: true,
				NetworkTier:       cloud.NetworkTierPremium.ToGCEValue(),

				IPAddress:           "10.0.0.0",
				Ports:               []string{"123"},
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeInternal),
				BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
				Network:             "https://www.googleapis.com/compute/v1/projects/test-poject/global/networks/test-vpc",
				Subnetwork:          "https://www.googleapis.com/compute/v1/projects/test-poject/regions/us-central1/subnetworks/default-subnet",
			},
			filtered: &composite.ForwardingRule{
				Id:                123,
				Name:              "tcp-fwd-rule",
				AllowGlobalAccess: true,
				NetworkTier:       cloud.NetworkTierPremium.ToGCEValue(),
			},
			patchable: true,
		},
		{
			desc: "NetworkTier PREMIUM -> STANDARD",
			existing: &composite.ForwardingRule{
				Id:          123,
				Name:        "tcp-fwd-rule",
				NetworkTier: cloud.NetworkTierPremium.ToGCEValue(),

				IPAddress:           "10.0.0.0",
				Ports:               []string{"123"},
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeInternal),
				BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
				Network:             "https://www.googleapis.com/compute/v1/projects/test-poject/global/networks/test-vpc",
				Subnetwork:          "https://www.googleapis.com/compute/v1/projects/test-poject/regions/us-central1/subnetworks/default-subnet",
			},
			new: &composite.ForwardingRule{
				Name:        "tcp-fwd-rule",
				NetworkTier: cloud.NetworkTierStandard.ToGCEValue(),

				IPAddress:           "10.0.0.0",
				Ports:               []string{"123"},
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeInternal),
				BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
				Network:             "https://www.googleapis.com/compute/v1/projects/test-poject/global/networks/test-vpc",
				Subnetwork:          "https://www.googleapis.com/compute/v1/projects/test-poject/regions/us-central1/subnetworks/default-subnet",
			},
			filtered: &composite.ForwardingRule{
				Id:          123,
				Name:        "tcp-fwd-rule",
				NetworkTier: cloud.NetworkTierStandard.ToGCEValue(),
			},
			patchable: true,
		},
		{
			desc: "Address change",
			existing: &composite.ForwardingRule{
				Id:        123,
				Name:      "tcp-fwd-rule",
				IPAddress: "10.0.0.0",

				Ports:               []string{"123"},
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeInternal),
				BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
				Network:             "https://www.googleapis.com/compute/v1/projects/test-poject/global/networks/test-vpc",
				Subnetwork:          "https://www.googleapis.com/compute/v1/projects/test-poject/regions/us-central1/subnetworks/default-subnet",
			},
			new: &composite.ForwardingRule{
				Name:      "tcp-fwd-rule",
				IPAddress: "12.0.0.0",

				Ports:               []string{"123"},
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeInternal),
				BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
				Network:             "https://www.googleapis.com/compute/v1/projects/test-poject/global/networks/test-vpc",
				Subnetwork:          "https://www.googleapis.com/compute/v1/projects/test-poject/regions/us-central1/subnetworks/default-subnet",
			},
			filtered:  nil,
			patchable: false,
		},
	}

	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			gotPatchable, gotFiltered := forwardingrules.PatchableIPv4(tC.existing, tC.new)
			if gotPatchable != tC.patchable {
				t.Errorf("PatchableIPv4(_, _) = %t, want %t", gotPatchable, tC.patchable)
			}

			diff := cmp.Diff(gotFiltered, tC.filtered)
			if diff != "" {
				t.Errorf("PatchableIPv4(_, _) mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
