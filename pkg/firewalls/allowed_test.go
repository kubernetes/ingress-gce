package firewalls

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	compute "google.golang.org/api/compute/v1"
	api_v1 "k8s.io/api/core/v1"
)

func TestAllowedForService(t *testing.T) {
	testCases := []struct {
		desc     string
		svcPorts []api_v1.ServicePort
		want     []*compute.FirewallAllowed
	}{
		{
			desc: "empty",
		},
		{
			desc: "udp only",
			svcPorts: []api_v1.ServicePort{
				{
					Protocol: api_v1.ProtocolUDP,
					Port:     1080,
				},
				{
					Protocol: api_v1.ProtocolUDP,
					Port:     8080,
				},
			},
			want: []*compute.FirewallAllowed{
				{
					IPProtocol: "udp",
					Ports:      []string{"1080", "8080"},
				},
			},
		},
		{
			desc: "tcp only",
			svcPorts: []api_v1.ServicePort{
				{
					Protocol: api_v1.ProtocolUDP,
					Port:     1080,
				},
				{
					Protocol: api_v1.ProtocolTCP,
					Port:     80,
				},
				{
					Protocol: api_v1.ProtocolTCP,
					Port:     443,
				},
				{
					Protocol: api_v1.ProtocolTCP,
					Port:     8080,
				},
				{
					Protocol: api_v1.ProtocolUDP,
					Port:     8080,
				},
			},
			want: []*compute.FirewallAllowed{
				{
					IPProtocol: "udp",
					Ports:      []string{"1080", "8080"},
				},
				{
					IPProtocol: "tcp",
					Ports:      []string{"80", "443", "8080"},
				},
			},
		},
		{
			desc: "mixed protocol",
			svcPorts: []api_v1.ServicePort{
				{
					Protocol: api_v1.ProtocolTCP,
					Port:     80,
				},
				{
					Protocol: api_v1.ProtocolTCP,
					Port:     443,
				},
				{
					Protocol: api_v1.ProtocolTCP,
					Port:     8080,
				},
			},
			want: []*compute.FirewallAllowed{
				{
					IPProtocol: "tcp",
					Ports:      []string{"80", "443", "8080"},
				},
			},
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			got := AllowedForService(tC.svcPorts)
			if eq, err := equalAllowRules(got, tC.want); !eq || err != nil {
				t.Errorf("AllowedForService(_) = %v, want %v", got, tC.want)
			}
		})
	}
}

func TestDeniedAll(t *testing.T) {
	got := DeniedAll()
	want := []*compute.FirewallDenied{
		{
			IPProtocol: "all",
		},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("DeniedAll() returned diff (-want +got):\n%s", diff)
	}
}
