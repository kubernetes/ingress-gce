package optimized_test

import (
	"testing"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/forwardingrules"
	"k8s.io/ingress-gce/pkg/forwardingrules/optimized"
	"k8s.io/klog/v2"
)

func TestFetch(t *testing.T) {
	testCases := []struct {
		desc string
		link string
		have []*composite.ForwardingRule

		want []*composite.ForwardingRule
	}{
		{
			desc: "single Backend Service",
			link: "https://compute.googleapis.com/projects/test-project/regions/us-central1/backendServices/test-be",
			have: []*composite.ForwardingRule{
				{Name: "test-fr-1", BackendService: "https://compute.googleapis.com/projects/test-project/regions/us-central1/backendServices/test-be"},
				{Name: "test-fr-2", BackendService: "https://compute.googleapis.com/projects/test-project/regions/us-central1/backendServices/test-be"},
			},
			want: []*composite.ForwardingRule{
				{Name: "test-fr-1", BackendService: "https://compute.googleapis.com/projects/test-project/regions/us-central1/backendServices/test-be"},
				{Name: "test-fr-2", BackendService: "https://compute.googleapis.com/projects/test-project/regions/us-central1/backendServices/test-be"},
			},
		},
		{
			desc: "multiple Backend Services",
			link: "https://compute.googleapis.com/projects/test-project/regions/us-central1/backendServices/test-be-1",
			have: []*composite.ForwardingRule{
				{Name: "test-fr-1", BackendService: "https://compute.googleapis.com/projects/test-project/regions/us-central1/backendServices/test-be-1"},
				{Name: "test-fr-2", BackendService: "https://compute.googleapis.com/projects/test-project/regions/us-central1/backendServices/test-be-2"},
			},
			want: []*composite.ForwardingRule{
				{Name: "test-fr-1", BackendService: "https://compute.googleapis.com/projects/test-project/regions/us-central1/backendServices/test-be-1"},
			},
		},
		{
			desc: "no matching forwarding rules",
			link: "https://compute.googleapis.com/projects/test-project/regions/us-central1/backendServices/test-be-1",
			have: []*composite.ForwardingRule{
				{Name: "test-fr-2", BackendService: "https://compute.googleapis.com/projects/test-project/regions/us-central1/backendServices/test-be-2"},
			},
			want: nil,
		},
		{
			desc: "name substring match",
			link: "https://compute.googleapis.com/projects/test-project/regions/us-central1/backendServices/test-be-1",
			have: []*composite.ForwardingRule{
				{Name: "test-fr-1", BackendService: "https://compute.googleapis.com/projects/test-project/regions/us-central1/backendServices/test-be-1-suffix"},
				{Name: "test-fr-2", BackendService: "https://compute.googleapis.com/projects/test-project/regions/us-central1/backendServices/test-be-1"},
			},
			want: []*composite.ForwardingRule{
				{Name: "test-fr-2", BackendService: "https://compute.googleapis.com/projects/test-project/regions/us-central1/backendServices/test-be-1"},
			},
		},
	}
	for _, tC := range testCases {
		tC := tC
		t.Run(tC.desc, func(t *testing.T) {
			t.Parallel()

			// Arrange
			vals := gce.DefaultTestClusterValues()
			fakeGCE := gce.NewFakeGCECloud(vals)

			repo := forwardingrules.New(fakeGCE, meta.VersionGA, meta.Regional, klog.TODO())
			for _, fr := range tC.have {
				if err := repo.Create(fr); err != nil {
					t.Fatalf("Create(%+v) returned error: %v, want nil", fr, err)
				}
			}

			// Act
			got, err := optimized.Fetch(repo, tC.link)

			// Assert
			if err != nil {
				t.Fatalf("Fetch(%q) returned error: %v, want nil", tC.link, err)
			}

			ignoreOpts := cmpopts.IgnoreFields(composite.ForwardingRule{}, "Version", "SelfLink", "Region")
			if diff := cmp.Diff(tC.want, got, ignoreOpts); diff != "" {
				t.Errorf("Fetch(%q) returned diff (-want +got):\n%s", tC.link, diff)
			}
		})
	}
}
