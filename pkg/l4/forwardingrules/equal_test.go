package forwardingrules_test

import (
	"testing"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"

	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/l4/forwardingrules"
)

func TestEqualIPv4(t *testing.T) {
	t.Parallel()

	fwdRuleTCP := &composite.ForwardingRule{
		Name:                "tcp-fwd-rule",
		IPAddress:           "10.0.0.0",
		Ports:               []string{"123"},
		IPProtocol:          "TCP",
		LoadBalancingScheme: string(cloud.SchemeInternal),
		BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
	}
	fwdRuleUDP := &composite.ForwardingRule{
		Name:                "udp-fwd-rule",
		IPAddress:           "10.0.0.0",
		Ports:               []string{"123"},
		IPProtocol:          "UDP",
		LoadBalancingScheme: string(cloud.SchemeInternal),
		BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
	}

	for _, tc := range []struct {
		desc                   string
		oldFwdRule             *composite.ForwardingRule
		newFwdRule             *composite.ForwardingRule
		discretePortForwarding bool
		expectEqual            bool
	}{
		{
			desc: "empty ip address does not match valid ip",
			oldFwdRule: &composite.ForwardingRule{
				Name:                "empty-ip-address-fwd-rule",
				IPAddress:           "",
				Ports:               []string{"123"},
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeInternal),
				BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
			},
			newFwdRule:  fwdRuleTCP,
			expectEqual: false,
		},
		{
			desc:       "global access enabled",
			oldFwdRule: fwdRuleTCP,
			newFwdRule: &composite.ForwardingRule{
				Name:                "global-access-fwd-rule",
				IPAddress:           "10.0.0.0",
				Ports:               []string{"123"},
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeInternal),
				AllowGlobalAccess:   true,
				BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
			},
			expectEqual: false,
		},
		{
			desc:        "IP protocol changed",
			oldFwdRule:  fwdRuleTCP,
			newFwdRule:  fwdRuleUDP,
			expectEqual: false,
		},
		{
			desc:        "same forwarding rule",
			oldFwdRule:  fwdRuleTCP,
			newFwdRule:  fwdRuleTCP,
			expectEqual: true,
		},
		{
			desc: "same forwarding rule, different basepath",
			oldFwdRule: &composite.ForwardingRule{
				Name:                "fwd-rule-bs-link1",
				IPAddress:           "10.0.0.0",
				Ports:               []string{"123"},
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeInternal),
				BackendService:      "http://compute.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
			},
			newFwdRule: &composite.ForwardingRule{
				Name:                "fwd-rule-bs-link2",
				IPAddress:           "10.0.0.0",
				Ports:               []string{"123"},
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeInternal),
				BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
			},
			expectEqual: true,
		},
		{
			desc:       "same forwarding rule, one uses ALL keyword for ports",
			oldFwdRule: fwdRuleUDP,
			newFwdRule: &composite.ForwardingRule{
				Name:                "udp-fwd-rule-all-ports",
				IPAddress:           "10.0.0.0",
				Ports:               []string{"123"},
				AllPorts:            true,
				IPProtocol:          "UDP",
				LoadBalancingScheme: string(cloud.SchemeInternal),
				BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
				NetworkTier:         cloud.NetworkTierPremium.ToGCEValue(),
			},
			expectEqual: false,
		},
		{
			desc: "network tier mismatch",
			oldFwdRule: &composite.ForwardingRule{
				Name:                "fwd-rule-bs-link2-standard-ntier",
				IPAddress:           "10.0.0.0",
				Ports:               []string{"123"},
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeInternal),
				BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
				NetworkTier:         string(cloud.NetworkTierStandard),
			},
			newFwdRule: &composite.ForwardingRule{
				Name:                "fwd-rule-bs-link2-premium-ntier",
				IPAddress:           "10.0.0.0",
				Ports:               []string{"123"},
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeInternal),
				BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
				NetworkTier:         cloud.NetworkTierPremium.ToGCEValue(),
			},
			expectEqual: false,
		},
		{
			desc: "same forwarding rule, port range mismatch",
			oldFwdRule: &composite.ForwardingRule{
				Name:                "tcp-fwd-rule",
				IPAddress:           "10.0.0.0",
				PortRange:           "1-2",
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeInternal),
				BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
			},
			newFwdRule: &composite.ForwardingRule{
				Name:                "tcp-fwd-rule",
				IPAddress:           "10.0.0.0",
				PortRange:           "2-3",
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeInternal),
				BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
			},
			expectEqual: false,
		},
		{
			desc: "same forwarding rule, ports mismatch",
			oldFwdRule: &composite.ForwardingRule{
				Name:                "tcp-fwd-rule",
				IPAddress:           "10.0.0.0",
				Ports:               []string{"1", "2"},
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeInternal),
				BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
			},
			newFwdRule: &composite.ForwardingRule{
				Name:                "tcp-fwd-rule",
				IPAddress:           "10.0.0.0",
				Ports:               []string{"2", "3"},
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeInternal),
				BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
			},
			expectEqual: false,
		},
		{
			desc: "port range, discrete ports disabled",
			oldFwdRule: &composite.ForwardingRule{
				Name:                "tcp-fwd-rule",
				IPAddress:           "10.0.0.0",
				PortRange:           "1-3",
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeInternal),
				BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
			},
			newFwdRule: &composite.ForwardingRule{
				Name:                "tcp-fwd-rule",
				IPAddress:           "10.0.0.0",
				Ports:               []string{"1", "3"},
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeInternal),
				BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
			},
			expectEqual: false,
		},
		{
			desc: "port range, discrete ports enabled, from range to ports",
			oldFwdRule: &composite.ForwardingRule{
				Name:                "tcp-fwd-rule",
				IPAddress:           "10.0.0.0",
				PortRange:           "1-3",
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeInternal),
				BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
			},
			newFwdRule: &composite.ForwardingRule{
				Name:                "tcp-fwd-rule",
				IPAddress:           "10.0.0.0",
				Ports:               []string{"1", "2", "3"},
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeInternal),
				BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
			},
			discretePortForwarding: true,
			expectEqual:            true,
		},
		{
			desc: "port range, discrete ports enabled, from range to ports, port outside of range",
			oldFwdRule: &composite.ForwardingRule{
				Name:                "tcp-fwd-rule",
				IPAddress:           "10.0.0.0",
				PortRange:           "1-3",
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeInternal),
				BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
			},
			newFwdRule: &composite.ForwardingRule{
				Name:                "tcp-fwd-rule",
				IPAddress:           "10.0.0.0",
				Ports:               []string{"1", "3", "5"},
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeInternal),
				BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
			},
			discretePortForwarding: true,
			expectEqual:            false,
		},
		{
			desc: "port range, discrete ports enabled, from ports to range",
			oldFwdRule: &composite.ForwardingRule{
				Name:                "tcp-fwd-rule",
				IPAddress:           "10.0.0.0",
				Ports:               []string{"1", "2", "3"},
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeInternal),
				BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
			},
			newFwdRule: &composite.ForwardingRule{
				Name:                "tcp-fwd-rule",
				IPAddress:           "10.0.0.0",
				PortRange:           "1-3",
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeInternal),
				BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
			},
			discretePortForwarding: true,
			expectEqual:            false,
		},
		{
			desc: "same forwarding rule, ports vs port ranges mismatch",
			oldFwdRule: &composite.ForwardingRule{
				Name:                "tcp-fwd-rule",
				IPAddress:           "10.0.0.0",
				PortRange:           "1-3",
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeInternal),
				BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
			},
			newFwdRule: &composite.ForwardingRule{
				Name:                "tcp-fwd-rule",
				IPAddress:           "10.0.0.0",
				Ports:               []string{"1", "5"},
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeInternal),
				BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
			},
			expectEqual: false,
		},
		{
			desc: "network mismatch",
			oldFwdRule: &composite.ForwardingRule{
				Name:                "tcp-fwd-rule",
				IPAddress:           "10.0.0.0",
				Ports:               []string{"123"},
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeInternal),
				BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
				Network:             "https://www.googleapis.com/compute/v1/projects/test-poject/global/networks/test-vpc",
			},
			newFwdRule: &composite.ForwardingRule{
				Name:                "tcp-fwd-rule",
				IPAddress:           "10.0.0.0",
				Ports:               []string{"123"},
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeInternal),
				BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
				Network:             "https://www.googleapis.com/compute/v1/projects/test-poject/global/networks/test-other-vpc",
			},
			expectEqual: false,
		},
		{
			desc: "subnetwork mismatch",
			oldFwdRule: &composite.ForwardingRule{
				Name:                "tcp-fwd-rule",
				IPAddress:           "10.0.0.0",
				Ports:               []string{"123"},
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeInternal),
				BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
				Network:             "https://www.googleapis.com/compute/v1/projects/test-poject/global/networks/test-vpc",
				Subnetwork:          "https://www.googleapis.com/compute/v1/projects/test-poject/regions/us-central1/subnetworks/default-subnet",
			},
			newFwdRule: &composite.ForwardingRule{
				Name:                "tcp-fwd-rule",
				IPAddress:           "10.0.0.0",
				Ports:               []string{"123"},
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeInternal),
				BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
				Network:             "https://www.googleapis.com/compute/v1/projects/test-poject/global/networks/test-vpc",
				Subnetwork:          "https://www.googleapis.com/compute/v1/projects/test-poject/regions/us-central1/subnetworks/other-subnet",
			},
			expectEqual: false,
		},
		{
			desc: "equal network data",
			oldFwdRule: &composite.ForwardingRule{
				Name:                "tcp-fwd-rule",
				IPAddress:           "10.0.0.0",
				Ports:               []string{"123"},
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeInternal),
				BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
				Network:             "https://www.googleapis.com/compute/v1/projects/test-poject/global/networks/test-vpc",
				Subnetwork:          "https://www.googleapis.com/compute/v1/projects/test-poject/regions/us-central1/subnetworks/default-subnet",
			},
			newFwdRule: &composite.ForwardingRule{
				Name:                "tcp-fwd-rule",
				IPAddress:           "10.0.0.0",
				Ports:               []string{"123"},
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeInternal),
				BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
				Network:             "projects/test-poject/global/networks/test-vpc",
				Subnetwork:          "projects/test-poject/regions/us-central1/subnetworks/default-subnet",
			},
			expectEqual: true,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			flags.F.EnableDiscretePortForwarding = tc.discretePortForwarding
			got, err := forwardingrules.EqualIPv4(tc.oldFwdRule, tc.newFwdRule)
			if err != nil {
				t.Errorf("EqualIPv4(_, _) = %v, want nil error", err)
			}
			if got != tc.expectEqual {
				t.Errorf("EqualIPv4(_, _) = %t, want %t", got, tc.expectEqual)
			}
		})
	}
}

func TestEqualIPv6(t *testing.T) {
	t.Parallel()

	emptyAddressFwdRule := &composite.ForwardingRule{
		Name:                "empty-ip-address-fwd-rule",
		IPAddress:           "",
		Ports:               []string{"123"},
		IPProtocol:          "TCP",
		LoadBalancingScheme: string(cloud.SchemeInternal),
		BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
	}
	tcpFwdRule := &composite.ForwardingRule{
		Name:                "tcp-fwd-rule",
		IPAddress:           "0::1/32",
		Ports:               []string{"123"},
		IPProtocol:          "TCP",
		LoadBalancingScheme: string(cloud.SchemeInternal),
		BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
	}
	tcpFwdRuleIP2 := &composite.ForwardingRule{
		Name:                "tcp-fwd-rule-ipv2",
		IPAddress:           "0::2/32",
		Ports:               []string{"123"},
		IPProtocol:          "TCP",
		LoadBalancingScheme: string(cloud.SchemeInternal),
		BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
	}
	udpFwdRule := &composite.ForwardingRule{
		Name:                "udp-fwd-rule",
		IPAddress:           "0::1/32",
		Ports:               []string{"123"},
		IPProtocol:          "UDP",
		LoadBalancingScheme: string(cloud.SchemeInternal),
		BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
	}
	globalAccessFwdRule := &composite.ForwardingRule{
		Name:                "global-access-fwd-rule",
		IPAddress:           "0::1/32",
		Ports:               []string{"123"},
		IPProtocol:          "TCP",
		LoadBalancingScheme: string(cloud.SchemeInternal),
		AllowGlobalAccess:   true,
		BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
	}
	bsLink1FwdRule := &composite.ForwardingRule{
		Name:                "fwd-rule-bs-link1",
		IPAddress:           "0::1/32",
		Ports:               []string{"123"},
		IPProtocol:          "TCP",
		LoadBalancingScheme: string(cloud.SchemeInternal),
		BackendService:      "http://compute.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
	}
	bsLink2FwdRule := &composite.ForwardingRule{
		Name:                "fwd-rule-bs-link2",
		IPAddress:           "0::1/32",
		Ports:               []string{"123"},
		IPProtocol:          "TCP",
		LoadBalancingScheme: string(cloud.SchemeInternal),
		BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
	}
	udpAllPortsFwdRule := &composite.ForwardingRule{
		Name:                "udp-fwd-rule-all-ports",
		IPAddress:           "0::1/32",
		Ports:               []string{"123"},
		AllPorts:            true,
		IPProtocol:          "UDP",
		LoadBalancingScheme: string(cloud.SchemeInternal),
		BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
		NetworkTier:         cloud.NetworkTierPremium.ToGCEValue(),
	}
	bsLink2StandardNetworkTierFwdRule := &composite.ForwardingRule{
		Name:                "fwd-rule-bs-link2-standard-network-tier",
		IPAddress:           "0::1/32",
		Ports:               []string{"123"},
		IPProtocol:          "TCP",
		LoadBalancingScheme: string(cloud.SchemeInternal),
		BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
		NetworkTier:         string(cloud.NetworkTierStandard),
	}
	bsLink2PremiumNetworkTierFwdRule := &composite.ForwardingRule{
		Name:                "fwd-rule-bs-link2-premium-network-tier",
		IPAddress:           "0::1/32",
		Ports:               []string{"123"},
		IPProtocol:          "TCP",
		LoadBalancingScheme: string(cloud.SchemeInternal),
		BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
		NetworkTier:         cloud.NetworkTierPremium.ToGCEValue(),
	}

	testCases := []struct {
		desc        string
		oldFwdRule  *composite.ForwardingRule
		newFwdRule  *composite.ForwardingRule
		expectEqual bool
	}{
		{
			desc:        "empty and non empty ip should be equal",
			oldFwdRule:  emptyAddressFwdRule,
			newFwdRule:  tcpFwdRule,
			expectEqual: true,
		},
		{
			desc:        "forwarding rules different only in ips should be equal",
			oldFwdRule:  tcpFwdRule,
			newFwdRule:  tcpFwdRuleIP2,
			expectEqual: true,
		},
		{
			desc:        "global access enabled",
			oldFwdRule:  tcpFwdRule,
			newFwdRule:  globalAccessFwdRule,
			expectEqual: false,
		},
		{
			desc:        "IP protocol changed",
			oldFwdRule:  tcpFwdRule,
			newFwdRule:  udpFwdRule,
			expectEqual: false,
		},
		{
			desc:        "same forwarding rule",
			oldFwdRule:  udpFwdRule,
			newFwdRule:  udpFwdRule,
			expectEqual: true,
		},
		{
			desc:        "same forwarding rule, different basepath",
			oldFwdRule:  bsLink1FwdRule,
			newFwdRule:  bsLink2FwdRule,
			expectEqual: true,
		},
		{
			desc:        "same forwarding rule, one uses ALL keyword for ports",
			oldFwdRule:  udpFwdRule,
			newFwdRule:  udpAllPortsFwdRule,
			expectEqual: false,
		},
		{
			desc:        "network tier mismatch",
			oldFwdRule:  bsLink2PremiumNetworkTierFwdRule,
			newFwdRule:  bsLink2StandardNetworkTierFwdRule,
			expectEqual: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			got, err := forwardingrules.EqualIPv6(tc.oldFwdRule, tc.newFwdRule)
			if err != nil {
				t.Errorf("EqualIPv6(_, _) returned error %v, want nil", err)
			}
			if got != tc.expectEqual {
				t.Errorf("EqualIPv6(_, _) = %t, want %t", got, tc.expectEqual)
			}
		})
	}
}
