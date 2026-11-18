/*
Copyright 2026 The Kubernetes Authors.

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

package standaloneneg

import (
	"testing"

	"k8s.io/ingress-gce/pkg/composite"
)

func TestValidateForwardingRule(t *testing.T) {
	testCases := []struct {
		desc           string
		fr             *composite.ForwardingRule
		frName         string
		expectError    bool
		expectErrorMsg string
	}{
		{
			desc: "Valid TCP rule",
			fr: &composite.ForwardingRule{
				LoadBalancingScheme: "EXTERNAL",
				IPProtocol:          "TCP",
			},
			frName:      "valid-tcp",
			expectError: false,
		},
		{
			desc: "Valid UDP rule",
			fr: &composite.ForwardingRule{
				LoadBalancingScheme: "EXTERNAL",
				IPProtocol:          "UDP",
			},
			frName:      "valid-udp",
			expectError: false,
		},
		{
			desc: "Valid two IPv4 addresses",
			fr: &composite.ForwardingRule{
				LoadBalancingScheme: "EXTERNAL_PASSTHROUGH",
				IPProtocol:          "TCP",
				IPAddresses:         []string{"10.0.0.100", "10.0.1.101"},
			},
			frName:      "valid-two-ipv4",
			expectError: false,
		},
		{
			desc: "Valid two IPv6 addresses",
			fr: &composite.ForwardingRule{
				LoadBalancingScheme: "EXTERNAL_PASSTHROUGH",
				IPProtocol:          "TCP",
				IPAddresses:         []string{"2600:1234::1234/96", "2600:1235::123/96"},
			},
			frName:      "valid-two-ipv6",
			expectError: false,
		},
		{
			desc: "Valid L3_DEFAULT rule",
			fr: &composite.ForwardingRule{
				LoadBalancingScheme: "EXTERNAL",
				IPProtocol:          "L3_DEFAULT",
			},
			frName:      "valid-l3-default",
			expectError: false,
		},
		{
			desc: "Internal scheme not supported",
			fr: &composite.ForwardingRule{
				LoadBalancingScheme: "INTERNAL",
				IPProtocol:          "TCP",
			},
			frName:         "internal-scheme",
			expectError:    true,
			expectErrorMsg: "forwarding rule internal-scheme has unsupported load balancing scheme: INTERNAL, supported schemes are: EXTERNAL, EXTERNAL_PASSTHROUGH",
		},
		{
			desc: "Valid EXTERNAL_PASSTHROUGH rule",
			fr: &composite.ForwardingRule{
				LoadBalancingScheme: "EXTERNAL_PASSTHROUGH",
				IPProtocol:          "TCP",
			},
			frName:      "valid-ext-passthrough",
			expectError: false,
		},
		{
			desc: "Unsupported scheme",
			fr: &composite.ForwardingRule{
				LoadBalancingScheme: "INTERNAL_SELF_MANAGED",
				IPProtocol:          "TCP",
			},
			frName:         "invalid-scheme",
			expectError:    true,
			expectErrorMsg: "forwarding rule invalid-scheme has unsupported load balancing scheme: INTERNAL_SELF_MANAGED, supported schemes are: EXTERNAL, EXTERNAL_PASSTHROUGH",
		},
		{
			desc: "Unsupported protocol",
			fr: &composite.ForwardingRule{
				LoadBalancingScheme: "EXTERNAL",
				IPProtocol:          "ESP",
			},
			frName:         "invalid-protocol",
			expectError:    true,
			expectErrorMsg: "forwarding rule invalid-protocol has unsupported protocol: ESP, supported protocols are: TCP, UDP, L3_DEFAULT",
		},
		{
			desc: "More than two IP addresses in IPAddresses array",
			fr: &composite.ForwardingRule{
				LoadBalancingScheme: "EXTERNAL_PASSTHROUGH",
				IPProtocol:          "TCP",
				IPAddresses:         []string{"10.0.0.100", "10.0.0.101", "10.0.0.102"},
			},
			frName:         "too-many-ips",
			expectError:    true,
			expectErrorMsg: "forwarding rule too-many-ips has more than 2 IP addresses",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			err := validateForwardingRule(tc.fr, tc.frName)
			if (err != nil) != tc.expectError {
				t.Errorf("validateForwardingRule() error = %v, expectError = %v", err, tc.expectError)
			}
			if err != nil && tc.expectErrorMsg != "" && err.Error() != tc.expectErrorMsg {
				t.Errorf("validateForwardingRule() error message = %q, expected = %q", err.Error(), tc.expectErrorMsg)
			}
		})
	}
}
