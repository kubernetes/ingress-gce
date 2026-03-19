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

// TestAllowedForConsecutivePorts verifies that old behavior of compressing consecutive ports into port ranges is preserved,
// which allows users to define a single firewall rule for extreme number of ports as long as there are no more then 100 consecutive objects (ports or port ranges).
func TestAllowedForConsecutivePorts(t *testing.T) {
	testCases := []struct {
		desc        string
		svcPortFunc func() []api_v1.ServicePort
		want        []*compute.FirewallAllowed
	}{
		{
			desc: "empty",
			svcPortFunc: func() []api_v1.ServicePort {
				return nil
			},
			want: nil,
		},
		{
			desc: "1_1000",
			svcPortFunc: func() []api_v1.ServicePort {
				var svcPorts []api_v1.ServicePort
				for i := 1; i <= 1000; i++ {
					svcPorts = append(svcPorts, api_v1.ServicePort{
						Protocol: api_v1.ProtocolUDP,
						Port:     int32(i),
					})
				}
				return svcPorts
			},
			want: []*compute.FirewallAllowed{
				{
					IPProtocol: "udp",
					Ports:      []string{"1-1000"},
				},
			},
		},
		{
			desc: "1_2_3__5_6_7_9",
			svcPortFunc: func() []api_v1.ServicePort {
				return []api_v1.ServicePort{
					{Port: 1},
					{Port: 2},
					{Port: 3},
					{Port: 5},
					{Port: 6},
					{Port: 7},
					{Port: 9},
				}
			},
			want: []*compute.FirewallAllowed{
				{
					IPProtocol: "tcp",
					Ports:      []string{"1-3", "5-7", "9"},
				},
			},
		},
		{
			desc: "1_2_3__5_6_7_9_mixed",
			svcPortFunc: func() []api_v1.ServicePort {
				return []api_v1.ServicePort{
					{Port: 1},
					{Port: 2},
					{Port: 3},
					{Port: 5},
					{Port: 6},
					{Port: 7},
					{Port: 9},
					{Protocol: api_v1.ProtocolUDP, Port: 1},
					{Protocol: api_v1.ProtocolUDP, Port: 2},
					{Protocol: api_v1.ProtocolUDP, Port: 3},
					{Protocol: api_v1.ProtocolUDP, Port: 9},
				}
			},
			want: []*compute.FirewallAllowed{
				{
					IPProtocol: "udp",
					Ports:      []string{"1-3", "9"},
				},
				{
					IPProtocol: "tcp",
					Ports:      []string{"1-3", "5-7", "9"},
				},
			},
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			got := AllowedForService(tC.svcPortFunc())
			if diff := cmp.Diff(tC.want, got); diff != "" {
				t.Errorf("AllowedForService() returned diff (-want +got):\n%s", diff)
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
