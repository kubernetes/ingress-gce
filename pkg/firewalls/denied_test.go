package firewalls_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	compute "google.golang.org/api/compute/v1"
	"k8s.io/ingress-gce/pkg/firewalls"
)

func TestDeniedAll(t *testing.T) {
	got := firewalls.DeniedAll()
	want := []*compute.FirewallDenied{
		{
			IPProtocol: "all",
		},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("DeniedAll() returned diff (-want +got):\n%s", diff)
	}
}
