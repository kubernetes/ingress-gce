package namer

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// TestL4Namer verifies that all L4 resource names are of the expected length and format.
func TestL4Namer(t *testing.T) {
	longstring1 := "012345678901234567890123456789012345678901234567890123456789abc"
	longstring2 := "012345678901234567890123456789012345678901234567890123456789pqr"
	type names struct {
		FRName            string
		NetLbFRName       string
		IPv6FRName        string
		NetLbIPv6FRName   string
		NEGName           string
		NonDefaultNEGName string
		FWName            string
		IPv6FWName        string
		HcFwName          string
		IPv6HcFName       string
		HcName            string
	}

	testCases := []struct {
		desc       string
		namespace  string
		name       string
		subnetName string
		proto      string
		sharedHC   bool
		want       names
	}{
		{
			desc:       "simple case",
			namespace:  "namespace",
			name:       "name",
			subnetName: "subnet",
			proto:      "TCP",
			sharedHC:   false,
			want: names{
				FRName:            "k8s2-tcp-7kpbhpki-namespace-name-956p2p7x",
				NetLbFRName:       "k8s2-tcp-7kpbhpki-namespace-name-956p2p7x-a",
				IPv6FRName:        "k8s2-tcp-7kpbhpki-namespace-name-956p2p7x-ipv6",
				NetLbIPv6FRName:   "k8s2-tcp-7kpbhpki-namespace-name-956p2p7x-a-ipv6",
				NEGName:           "k8s2-7kpbhpki-namespace-name-956p2p7x",
				NonDefaultNEGName: "k8s2-7kpbhpki-namespace-name-185075-956p2p7x",
				FWName:            "k8s2-7kpbhpki-namespace-name-956p2p7x",
				IPv6FWName:        "k8s2-7kpbhpki-namespace-name-956p2p7x-ipv6",
				HcFwName:          "k8s2-7kpbhpki-namespace-name-956p2p7x-fw",
				IPv6HcFName:       "k8s2-7kpbhpki-namespace-name-956p2p7x-fw-ipv6",
				HcName:            "k8s2-7kpbhpki-namespace-name-956p2p7x",
			},
		},
		{
			desc:       "simple case, shared healthcheck",
			namespace:  "namespace",
			name:       "name",
			subnetName: "subnet",
			proto:      "TCP",
			sharedHC:   true,
			want: names{
				FRName:            "k8s2-tcp-7kpbhpki-namespace-name-956p2p7x",
				NetLbFRName:       "k8s2-tcp-7kpbhpki-namespace-name-956p2p7x-a",
				IPv6FRName:        "k8s2-tcp-7kpbhpki-namespace-name-956p2p7x-ipv6",
				NetLbIPv6FRName:   "k8s2-tcp-7kpbhpki-namespace-name-956p2p7x-a-ipv6",
				NEGName:           "k8s2-7kpbhpki-namespace-name-956p2p7x",
				NonDefaultNEGName: "k8s2-7kpbhpki-namespace-name-185075-956p2p7x",
				FWName:            "k8s2-7kpbhpki-namespace-name-956p2p7x",
				IPv6FWName:        "k8s2-7kpbhpki-namespace-name-956p2p7x-ipv6",
				HcFwName:          "k8s2-7kpbhpki-l4-shared-hc-fw",
				IPv6HcFName:       "k8s2-7kpbhpki-l4-shared-hc-fw-ipv6",
				HcName:            "k8s2-7kpbhpki-l4-shared-hc",
			},
		},
		{
			desc:       "long svc and namespace name",
			namespace:  longstring1,
			name:       longstring2,
			subnetName: "subnet",
			proto:      "UDP",
			sharedHC:   false,
			want: names{
				FRName:            "k8s2-udp-7kpbhpki-012345678901234567-01234567890123456-hwm400mg",
				NetLbFRName:       "k8s2-udp-7kpbhpki-012345678901234567-01234567890123456-hwm400-a",
				IPv6FRName:        "k8s2-udp-7kpbhpki-012345678901234567-01234567890123456-hwm-ipv6",
				NetLbIPv6FRName:   "k8s2-udp-7kpbhpki-012345678901234567-01234567890123456-h-a-ipv6",
				NEGName:           "k8s2-7kpbhpki-01234567890123456789-0123456789012345678-hwm400mg",
				NonDefaultNEGName: "k8s2-7kpbhpki-0123456789012345-0123456789012345-185075-hwm400mg",
				FWName:            "k8s2-7kpbhpki-01234567890123456789-0123456789012345678-hwm400mg",
				IPv6FWName:        "k8s2-7kpbhpki-01234567890123456789-0123456789012345678-hwm-ipv6",
				HcFwName:          "k8s2-7kpbhpki-01234567890123456789-0123456789012345678-hwm40-fw",
				IPv6HcFName:       "k8s2-7kpbhpki-01234567890123456789-0123456789012345678--fw-ipv6",
				HcName:            "k8s2-7kpbhpki-01234567890123456789-0123456789012345678-hwm400mg",
			},
		},
		{
			desc:       "long svc and namespace name, shared healthcheck",
			namespace:  longstring1,
			name:       longstring2,
			subnetName: "subnet",
			proto:      "UDP",
			sharedHC:   true,
			want: names{
				FRName:            "k8s2-udp-7kpbhpki-012345678901234567-01234567890123456-hwm400mg",
				NetLbFRName:       "k8s2-udp-7kpbhpki-012345678901234567-01234567890123456-hwm400-a",
				IPv6FRName:        "k8s2-udp-7kpbhpki-012345678901234567-01234567890123456-hwm-ipv6",
				NetLbIPv6FRName:   "k8s2-udp-7kpbhpki-012345678901234567-01234567890123456-h-a-ipv6",
				NEGName:           "k8s2-7kpbhpki-01234567890123456789-0123456789012345678-hwm400mg",
				NonDefaultNEGName: "k8s2-7kpbhpki-0123456789012345-0123456789012345-185075-hwm400mg",
				FWName:            "k8s2-7kpbhpki-01234567890123456789-0123456789012345678-hwm400mg",
				IPv6FWName:        "k8s2-7kpbhpki-01234567890123456789-0123456789012345678-hwm-ipv6",
				HcFwName:          "k8s2-7kpbhpki-l4-shared-hc-fw",
				IPv6HcFName:       "k8s2-7kpbhpki-l4-shared-hc-fw-ipv6",
				HcName:            "k8s2-7kpbhpki-l4-shared-hc",
			},
		},
		{
			desc:       "long subnet name",
			namespace:  "namespace",
			name:       "name",
			subnetName: longstring1,
			proto:      "TCP",
			sharedHC:   false,
			want: names{
				FRName:            "k8s2-tcp-7kpbhpki-namespace-name-956p2p7x",
				NetLbFRName:       "k8s2-tcp-7kpbhpki-namespace-name-956p2p7x-a",
				IPv6FRName:        "k8s2-tcp-7kpbhpki-namespace-name-956p2p7x-ipv6",
				NetLbIPv6FRName:   "k8s2-tcp-7kpbhpki-namespace-name-956p2p7x-a-ipv6",
				NEGName:           "k8s2-7kpbhpki-namespace-name-956p2p7x",
				NonDefaultNEGName: "k8s2-7kpbhpki-namespace-name-1fd834-956p2p7x",
				FWName:            "k8s2-7kpbhpki-namespace-name-956p2p7x",
				IPv6FWName:        "k8s2-7kpbhpki-namespace-name-956p2p7x-ipv6",
				HcFwName:          "k8s2-7kpbhpki-namespace-name-956p2p7x-fw",
				IPv6HcFName:       "k8s2-7kpbhpki-namespace-name-956p2p7x-fw-ipv6",
				HcName:            "k8s2-7kpbhpki-namespace-name-956p2p7x",
			},
		},
		{
			desc:       "l3 protocol",
			namespace:  "namespace",
			name:       "name",
			subnetName: longstring1,
			proto:      "L3_DEFAULT",
			sharedHC:   false,
			want: names{
				FRName:            "k8s2-l3-7kpbhpki-namespace-name-956p2p7x",
				NetLbFRName:       "k8s2-l3-7kpbhpki-namespace-name-956p2p7x-a",
				IPv6FRName:        "k8s2-l3-7kpbhpki-namespace-name-956p2p7x-ipv6",
				NetLbIPv6FRName:   "k8s2-l3-7kpbhpki-namespace-name-956p2p7x-a-ipv6",
				NEGName:           "k8s2-7kpbhpki-namespace-name-956p2p7x",
				NonDefaultNEGName: "k8s2-7kpbhpki-namespace-name-1fd834-956p2p7x",
				FWName:            "k8s2-7kpbhpki-namespace-name-956p2p7x",
				IPv6FWName:        "k8s2-7kpbhpki-namespace-name-956p2p7x-ipv6",
				HcFwName:          "k8s2-7kpbhpki-namespace-name-956p2p7x-fw",
				IPv6HcFName:       "k8s2-7kpbhpki-namespace-name-956p2p7x-fw-ipv6",
				HcName:            "k8s2-7kpbhpki-namespace-name-956p2p7x",
			},
		},
		{
			desc:       "l3 protocol with long svc and namespace name",
			namespace:  longstring1,
			name:       longstring2,
			subnetName: "subnet",
			proto:      "L3_DEFAULT",
			sharedHC:   true,
			want: names{
				FRName:            "k8s2-l3-7kpbhpki-012345678901234567-012345678901234567-hwm400mg",
				NetLbFRName:       "k8s2-l3-7kpbhpki-012345678901234567-012345678901234567-hwm400-a",
				IPv6FRName:        "k8s2-l3-7kpbhpki-012345678901234567-012345678901234567-hwm-ipv6",
				NetLbIPv6FRName:   "k8s2-l3-7kpbhpki-012345678901234567-012345678901234567-h-a-ipv6",
				NEGName:           "k8s2-7kpbhpki-01234567890123456789-0123456789012345678-hwm400mg",
				NonDefaultNEGName: "k8s2-7kpbhpki-0123456789012345-0123456789012345-185075-hwm400mg",
				FWName:            "k8s2-7kpbhpki-01234567890123456789-0123456789012345678-hwm400mg",
				IPv6FWName:        "k8s2-7kpbhpki-01234567890123456789-0123456789012345678-hwm-ipv6",
				HcFwName:          "k8s2-7kpbhpki-l4-shared-hc-fw",
				IPv6HcFName:       "k8s2-7kpbhpki-l4-shared-hc-fw-ipv6",
				HcName:            "k8s2-7kpbhpki-l4-shared-hc",
			},
		},
	}

	namer := NewL4Namer(kubeSystemUID, nil)
	for _, tc := range testCases {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			const frNumber = uint(10)

			// Act
			got := names{
				FRName:            namer.L4ForwardingRule(tc.namespace, tc.name, strings.ToLower(tc.proto)),
				NetLbFRName:       namer.L4NetLBForwardingRule(tc.namespace, tc.name, strings.ToLower(tc.proto), frNumber),
				IPv6FRName:        namer.L4IPv6ForwardingRule(tc.namespace, tc.name, strings.ToLower(tc.proto)),
				NetLbIPv6FRName:   namer.L4NetLBIPv6ForwardingRule(tc.namespace, tc.name, strings.ToLower(tc.proto), frNumber),
				NEGName:           namer.L4Backend(tc.namespace, tc.name),
				NonDefaultNEGName: namer.NonDefaultSubnetNEG(tc.namespace, tc.name, tc.subnetName, 0), // Port is not used for L4 NEG
				FWName:            namer.L4Firewall(tc.namespace, tc.name),
				IPv6FWName:        namer.L4IPv6Firewall(tc.namespace, tc.name),
				HcName:            namer.L4HealthCheck(tc.namespace, tc.name, tc.sharedHC),
				HcFwName:          namer.L4HealthCheckFirewall(tc.namespace, tc.name, tc.sharedHC),
				IPv6HcFName:       namer.L4IPv6HealthCheckFirewall(tc.namespace, tc.name, tc.sharedHC),
			}

			// Assert
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("got != want, (-want, +got):\n%s", diff)
			}

			// Extra check for naming length
			v := reflect.ValueOf(got)
			for i := 0; i < v.NumField(); i++ {
				field := v.Field(i)
				if field.Kind() == reflect.String {
					fieldName := v.Type().Field(i).Name
					if len(field.String()) > maxResourceNameLength {
						t.Errorf("%s: got len(%s) == %v, want <= %d", tc.desc, fieldName, len(field.String()), maxResourceNameLength)
					}
				}
			}
		})
	}
}

// TestL4NamerForwardingRulesNumbers verifies the conversion of the FR number for some edge cases
// Other behavior is tested in TestL4Namer
func TestL4NamerForwardingRulesNumbers(t *testing.T) {
	const namespace, name, protocol = "default", "netlb-service", "TCP"
	type netLbNames struct {
		IPv4 string
		IPv6 string
	}

	testCases := []struct {
		frNumber uint
		want     netLbNames
	}{
		{
			frNumber: 0,
			want: netLbNames{
				IPv4: "k8s2-tcp-7kpbhpki-default-netlb-service-y6efeqo7-0",
				IPv6: "k8s2-tcp-7kpbhpki-default-netlb-service-y6efeqo7-0-ipv6",
			},
		},
		{
			frNumber: 9,
			want: netLbNames{
				IPv4: "k8s2-tcp-7kpbhpki-default-netlb-service-y6efeqo7-9",
				IPv6: "k8s2-tcp-7kpbhpki-default-netlb-service-y6efeqo7-9-ipv6",
			},
		},
		{
			frNumber: 10,
			want: netLbNames{
				IPv4: "k8s2-tcp-7kpbhpki-default-netlb-service-y6efeqo7-a",
				IPv6: "k8s2-tcp-7kpbhpki-default-netlb-service-y6efeqo7-a-ipv6",
			},
		},
		{
			frNumber: 35,
			want: netLbNames{
				IPv4: "k8s2-tcp-7kpbhpki-default-netlb-service-y6efeqo7-z",
				IPv6: "k8s2-tcp-7kpbhpki-default-netlb-service-y6efeqo7-z-ipv6",
			},
		},
		{
			frNumber: 36,
			want: netLbNames{
				IPv4: "k8s2-tcp-7kpbhpki-default-netlb-service-y6efeqo7-10",
				IPv6: "k8s2-tcp-7kpbhpki-default-netlb-service-y6efeqo7-10-ipv6",
			},
		},
		{
			frNumber: 1000,
			want: netLbNames{
				IPv4: "k8s2-tcp-7kpbhpki-default-netlb-service-y6efeqo7-rs",
				IPv6: "k8s2-tcp-7kpbhpki-default-netlb-service-y6efeqo7-rs-ipv6",
			},
		},
		{
			frNumber: 13108,
			want: netLbNames{
				IPv4: "k8s2-tcp-7kpbhpki-default-netlb-service-y6efeqo7-a44",
				IPv6: "k8s2-tcp-7kpbhpki-default-netlb-service-y6efeqo7-a44-ipv6",
			},
		},
	}

	namer := NewL4Namer(kubeSystemUID, nil)
	for _, tC := range testCases {
		tC := tC
		t.Run(fmt.Sprint(tC.frNumber), func(t *testing.T) {
			t.Parallel()

			// Act
			got := netLbNames{
				IPv4: namer.L4NetLBForwardingRule(namespace, name, protocol, tC.frNumber),
				IPv6: namer.L4NetLBIPv6ForwardingRule(namespace, name, protocol, tC.frNumber),
			}

			// Assert
			if diff := cmp.Diff(tC.want, got); diff != "" {
				t.Errorf("want != got, (-want, +got):\n%s", diff)
			}
		})
	}
}
