package firewalls

import (
	"testing"

	"google.golang.org/api/compute/v1"
)

func TestEqual(t *testing.T) {
	testCases := []struct {
		desc            string
		a               *compute.Firewall
		b               *compute.Firewall
		skipDescription bool
		want            bool
		wantErr         bool
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
		{
			desc: "incorrect ports, one extra dash",
			a: &compute.Firewall{
				Allowed: []*compute.FirewallAllowed{
					{
						IPProtocol: "TCP",
						Ports:      []string{"1-1-1"},
					},
				},
			},
			b: &compute.Firewall{
				Allowed: []*compute.FirewallAllowed{
					{
						IPProtocol: "TCP",
						Ports:      []string{"1-1-1"},
					},
				},
			},
			wantErr: true,
		},
		{
			desc: "incorrect ports, empty",
			a: &compute.Firewall{
				Allowed: []*compute.FirewallAllowed{
					{
						IPProtocol: "TCP",
						Ports:      []string{""},
					},
				},
			},
			b: &compute.Firewall{
				Allowed: []*compute.FirewallAllowed{
					{
						IPProtocol: "TCP",
						Ports:      []string{""},
					},
				},
			},
			wantErr: true,
		},
		{
			desc: "incorrect ports, not a int",
			a: &compute.Firewall{
				Allowed: []*compute.FirewallAllowed{
					{
						IPProtocol: "TCP",
						Ports:      []string{"abc"},
					},
				},
			},
			b: &compute.Firewall{
				Allowed: []*compute.FirewallAllowed{
					{
						IPProtocol: "TCP",
						Ports:      []string{"abc"},
					},
				},
			},
			wantErr: true,
		},
		{
			desc: "incorrect ports, not a int in range",
			a: &compute.Firewall{
				Allowed: []*compute.FirewallAllowed{
					{
						IPProtocol: "TCP",
						Ports:      []string{"0-abc"},
					},
				},
			},
			b: &compute.Firewall{
				Allowed: []*compute.FirewallAllowed{
					{
						IPProtocol: "TCP",
						Ports:      []string{"abc-0"},
					},
				},
			},
			wantErr: true,
		},
		{
			desc: "same ipv6 source ranges with shortcut",
			a: &compute.Firewall{
				SourceRanges: []string{
					"2600:2900:3070:be4:6000:0:0:0/128",
				},
			},
			b: &compute.Firewall{
				SourceRanges: []string{
					"2600:2900:3070:be4:6000::/128",
				},
			},
			want: true,
		},
		{
			desc: "different priority",
			a: &compute.Firewall{
				Priority: 1000,
			},
			b: &compute.Firewall{
				Priority: 999,
			},
			want: false,
		},
		{
			desc: "same priority",
			a: &compute.Firewall{
				Priority: 1000,
			},
			b: &compute.Firewall{
				Priority: 1000,
			},
			want: true,
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			got, gotErr := Equal(tC.a, tC.b, tC.skipDescription)
			if got != tC.want || (gotErr != nil) != tC.wantErr {
				wantErr := "and no error"
				if tC.wantErr {
					wantErr = "and error"
				}
				t.Errorf("firewalls.Equal(%v, %v) = %v %v, want %v %s", tC.desc, tC.skipDescription, got, gotErr, tC.want, wantErr)
			}
		})
	}
}

func TestEqualIPRangeSet(t *testing.T) {
	tcs := []struct {
		desc      string
		a         []string
		b         []string
		wantEqual bool
	}{
		{
			desc:      "same IPv4 ranges",
			a:         []string{"1.2.3.4/24"},
			b:         []string{"1.2.3.4/24"},
			wantEqual: true,
		},
		{
			desc:      "same IPv4 ranges different ordering",
			a:         []string{"1.2.3.4/24", "5.6.7.8/24"},
			b:         []string{"5.6.7.8/24", "1.2.3.4/24"},
			wantEqual: true,
		},
		{
			desc:      "different IPv4 ranges",
			a:         []string{"1.2.3.4/24", "5.6.7.8/24"},
			b:         []string{"1.2.3.4/24", "5.5.5.5/24"},
			wantEqual: false,
		},
		{
			desc:      "same IPv6 ranges",
			a:         []string{"2600:2900:3070:be4:6000:0:0:0/128"},
			b:         []string{"2600:2900:3070:be4:6000:0:0:0/128"},
			wantEqual: true,
		},
		{
			desc:      "same IPv6 range sets",
			a:         []string{"2600:2900:3070:be4:6000:0:0:1/128", "2600:2900:3070:be4:6000:0:0:2/128"},
			b:         []string{"2600:2900:3070:be4:6000:0:0:2/128", "2600:2900:3070:be4:6000:0:0:1/128"},
			wantEqual: true,
		},
		{
			desc:      "same IPv6 addresses",
			a:         []string{"2600:2900:3070:be4:6000:0:0:0"},
			b:         []string{"2600:2900:3070:be4:6000:0:0:0"},
			wantEqual: true,
		},
		{
			desc:      "same IPv6 addresses and range sets",
			a:         []string{"2600:2900:3070:be4:6000:0:0:1/96", "2600:2900:3070:be4:6000:0:0:0"},
			b:         []string{"2600:2900:3070:be4:6000:0:0:0", "2600:2900:3070:be4:6000:0:0:1/96"},
			wantEqual: true,
		},
		{
			desc:      "same IPv6 addresses with shortcuts",
			a:         []string{"2600:2900:3070:be4:6000:0:0:0"},
			b:         []string{"2600:2900:3070:be4:6000::"},
			wantEqual: true,
		},
		{
			desc:      "same IPv6 ranges with shortcuts",
			a:         []string{"2600:2900:3070:be4:6000::/128"},
			b:         []string{"2600:2900:3070:be4:6000:0:0:0/128"},
			wantEqual: true,
		},
		{
			desc:      "different IPv6 ranges with shortcuts",
			a:         []string{"2600:2900:3070:be4:6000::/96"},
			b:         []string{"2600:2900:3070:be4:6000:0:0:0/128"},
			wantEqual: false,
		},
		{
			desc:      "different IPv6 ranges with shortcuts",
			a:         []string{"2600:2900:3070:be4:6000:0:0:0/96"},
			b:         []string{"2600:2900:3070:be4:6000:0:0:0/128"},
			wantEqual: false,
		},
		{
			desc:      "different IPv6 addresses",
			a:         []string{"2600:2900:3070:be4:6000:0:0:1"},
			b:         []string{"2600:2900:3070:be4:6000:0:0:2"},
			wantEqual: false,
		},
		{
			desc:      "different IPv6 addresses with shortcuts",
			a:         []string{"2600:2900:3070:be4:6000:0:0:1"},
			b:         []string{"2600:2900:3070:be4:6000::2"},
			wantEqual: false,
		},
		{
			desc:      "same IPv6 addresses with uppercase letters",
			a:         []string{"2600:2900:3070:be4:6000:0:0:0"},
			b:         []string{"2600:2900:3070:BE4:6000:0:0:0"},
			wantEqual: true,
		},
		{
			desc:      "same IPv6 ranges with shortcuts and uppercase letters",
			a:         []string{"2600:2900:3070:be4:6000::/128"},
			b:         []string{"2600:2900:3070:BE4:6000:0:0:0/128"},
			wantEqual: true,
		},
		{
			// fallback in case we get some non address value from the API, we should treat these as strings then
			desc:      "same unparseable addresses",
			a:         []string{"invalid/128"},
			b:         []string{"invalid/128"},
			wantEqual: true,
		},
		{
			// fallback in case we get some non address value from the API, we should treat these as strings then
			desc:      "different unparseable addresses",
			a:         []string{"invalid1/128"},
			b:         []string{"invalid2/128"},
			wantEqual: false,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.desc, func(t *testing.T) {
			got := equalIPRangeSet(tc.a, tc.b)
			if got != tc.wantEqual {
				t.Errorf("equalIPRangeSet(%v, %v), want %v got %v", tc.a, tc.b, tc.wantEqual, got)
			}
		})
	}
}
