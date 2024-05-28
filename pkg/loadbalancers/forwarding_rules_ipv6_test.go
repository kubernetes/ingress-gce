/*
Copyright 2022 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package loadbalancers

import (
	"testing"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"k8s.io/ingress-gce/pkg/composite"
)

func TestIPv6ForwardingRulesEqual(t *testing.T) {
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
			got, err := EqualIPv6ForwardingRules(tc.oldFwdRule, tc.newFwdRule)
			if err != nil {
				t.Errorf("EqualIPv6ForwardingRules(_, _) returned error %v, want nil", err)
			}
			if got != tc.expectEqual {
				t.Errorf("EqualIPv6ForwardingRules(_, _) = %t, want %t", got, tc.expectEqual)
			}
		})
	}
}
