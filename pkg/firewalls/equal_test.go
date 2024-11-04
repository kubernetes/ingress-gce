package firewalls_test

import (
	"testing"

	"google.golang.org/api/compute/v1"
	"k8s.io/ingress-gce/pkg/firewalls"
)

func TestEqual(t *testing.T) {
	testCases := []struct {
		desc            string
		a               *compute.Firewall
		b               *compute.Firewall
		skipDescription bool
		want            bool
	}{
		{
			desc: "same, complete example",
			a: &compute.Firewall{
				Description: "example fw rule",
				Allowed: []*compute.FirewallAllowed{
					{
						IPProtocol: "TCP",
						Ports:      []string{"123", "321"},
					},
				},
				SourceRanges: []string{
					"10.1.2.8/29",
					"10.1.3.8/29",
				},
				DestinationRanges: []string{
					"10.1.2.16/29",
					"10.1.3.16/29",
				},
				TargetTags: []string{"k8s-test"},
			},
			b: &compute.Firewall{
				Description: "example fw rule",
				Allowed: []*compute.FirewallAllowed{
					{
						IPProtocol: "TCP",
						Ports:      []string{"123", "321"},
					},
				},
				SourceRanges: []string{
					"10.1.2.8/29",
					"10.1.3.8/29",
				},
				DestinationRanges: []string{
					"10.1.2.16/29",
					"10.1.3.16/29",
				},
				TargetTags: []string{"k8s-test"},
			},
			want: true,
		},
		{
			desc: "nil, nil",
			a:    nil,
			b:    nil,
			want: true,
		},
		{
			desc: "b nil",
			a:    &compute.Firewall{},
			b:    nil,
			want: false,
		},
		{
			desc: "a nil",
			a:    nil,
			b:    &compute.Firewall{},
			want: false,
		},
		{
			desc: "same rules, multi protocol",
			a: &compute.Firewall{
				Allowed: []*compute.FirewallAllowed{
					{
						IPProtocol: "TCP",
						Ports:      []string{"123", "345"},
					},
					{
						IPProtocol: "UDP",
						Ports:      []string{"123", "321"},
					},
				},
			},
			b: &compute.Firewall{
				Allowed: []*compute.FirewallAllowed{
					{
						IPProtocol: "TCP",
						Ports:      []string{"123", "345"},
					},
					{
						IPProtocol: "UDP",
						Ports:      []string{"123", "321"},
					},
				},
			},
			want: true,
		},
		{
			desc: "same rules, tcp protocol",
			a: &compute.Firewall{
				Allowed: []*compute.FirewallAllowed{
					{
						IPProtocol: "TCP",
						Ports:      []string{"123", "321"},
					},
				},
			},
			b: &compute.Firewall{
				Allowed: []*compute.FirewallAllowed{
					{
						IPProtocol: "TCP",
						Ports:      []string{"123", "321"},
					},
				},
			},
			want: true,
		},
		{
			desc: "same rules, udp protocol",
			a: &compute.Firewall{
				Allowed: []*compute.FirewallAllowed{
					{
						IPProtocol: "UDP",
						Ports:      []string{"123", "321"},
					},
				},
			},
			b: &compute.Firewall{
				Allowed: []*compute.FirewallAllowed{
					{
						IPProtocol: "UDP",
						Ports:      []string{"123", "321"},
					},
				},
			},
			want: true,
		},
		{
			desc: "same rules, port ranges",
			a: &compute.Firewall{
				Allowed: []*compute.FirewallAllowed{
					{
						IPProtocol: "UDP",
						Ports:      []string{"123-125", "321"},
					},
				},
			},
			b: &compute.Firewall{
				Allowed: []*compute.FirewallAllowed{
					{
						IPProtocol: "UDP",
						Ports:      []string{"123", "124", "125", "321-321"},
					},
				},
			},
			want: true,
		},
		{
			desc: "mixed case protocol",
			a: &compute.Firewall{
				Allowed: []*compute.FirewallAllowed{
					{
						IPProtocol: "udp",
						Ports:      []string{"1234"},
					},
				},
			},
			b: &compute.Firewall{
				Allowed: []*compute.FirewallAllowed{
					{
						IPProtocol: "UDP",
						Ports:      []string{"1234"},
					},
				},
			},
			want: true,
		},
		{
			desc: "different source ranges",
			a: &compute.Firewall{
				SourceRanges: []string{
					"11.1.2.8/29",
				},
			},
			b: &compute.Firewall{
				SourceRanges: []string{
					"10.1.2.8/29",
				},
			},
			want: false,
		},
		{
			desc: "different source ranges, list",
			a: &compute.Firewall{
				SourceRanges: []string{
					"10.1.2.8/29",
					"10.1.2.9/29",
				},
			},
			b: &compute.Firewall{
				SourceRanges: []string{
					"10.1.2.8/29",
					"10.1.2.10/29",
				},
			},
			want: false,
		},
		{
			desc: "different destination ranges",
			a: &compute.Firewall{
				DestinationRanges: []string{
					"10.1.2.16/29",
				},
			},
			b: &compute.Firewall{
				DestinationRanges: []string{
					"11.1.2.16/29",
				},
			},
			want: false,
		},
		{
			desc: "different destination ranges, list",
			a: &compute.Firewall{
				DestinationRanges: []string{
					"10.1.2.17/29",
					"10.1.2.16/29",
				},
			},
			b: &compute.Firewall{
				DestinationRanges: []string{
					"11.1.2.17/29",
					"10.1.2.16/29",
				},
			},
			want: false,
		},
		{
			desc: "different descriptions, skipDescription=true",
			a: &compute.Firewall{
				Description: "fw rule v0",
			},
			b: &compute.Firewall{
				Description: "fw rule v1",
			},
			skipDescription: true,
			want:            true,
		},
		{
			desc: "different descriptions, skipDescriptions=false",
			a: &compute.Firewall{
				Description: "fw rule v0",
			},
			b: &compute.Firewall{
				Description: "fw rule v1",
			},
			skipDescription: false,
			want:            false,
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			got := firewalls.Equal(tC.a, tC.b, tC.skipDescription)
			if got != tC.want {
				t.Errorf("firewalls.Equal(%v, %v, %v) = %v, want %v", tC.a, tC.b, tC.skipDescription, got, tC.want)
			}
		})
	}
}
