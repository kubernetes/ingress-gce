/*
Copyright 2025 The Kubernetes Authors.

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

package validation

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestParseHealthCheckSourceCIDRs(t *testing.T) {
	testCases := []struct {
		name      string
		input     string
		want      []string
		wantError bool
	}{
		{
			name:  "empty string",
			input: "",
			want:  nil,
		},
		{
			name:  "single valid CIDR",
			input: "192.168.1.0/24",
			want:  []string{"192.168.1.0/24"},
		},
		{
			name:  "multiple valid CIDRs",
			input: "130.211.0.0/22,35.191.0.0/16",
			want:  []string{"130.211.0.0/22", "35.191.0.0/16"},
		},
		{
			name:  "multiple valid CIDRs with spaces",
			input: "130.211.0.0/22, 35.191.0.0/16",
			want:  []string{"130.211.0.0/22", "35.191.0.0/16"},
		},
		{
			name:  "valid IPv6 CIDR",
			input: "2001:db8::/32",
			want:  []string{"2001:db8::/32"},
		},
		{
			name:  "mixed IPv4 and IPv6 CIDRs",
			input: "192.168.1.0/24,2001:db8::/32",
			want:  []string{"192.168.1.0/24", "2001:db8::/32"},
		},
		{
			name:  "multiple IPv6 CIDRs",
			input: "2001:db8::/32,fe80::/64",
			want:  []string{"2001:db8::/32", "fe80::/64"},
		},
		{
			name:  "IPv6 CIDRs with spaces",
			input: "2001:db8::/32, fe80::/64",
			want:  []string{"2001:db8::/32", "fe80::/64"},
		},
		{
			name:  "IPv6 with different prefix lengths",
			input: "2001:db8::/48,::1/128",
			want:  []string{"2001:db8::/48", "::1/128"},
		},
		{
			name:  "IPv6 loopback CIDR",
			input: "::1/128",
			want:  []string{"::1/128"},
		},
		{
			name:  "IPv6 link-local CIDR",
			input: "fe80::/64",
			want:  []string{"fe80::/64"},
		},
		{
			name:  "IPv6 multicast CIDR",
			input: "ff00::/8",
			want:  []string{"ff00::/8"},
		},
		{
			name:  "mixed IPv4, IPv6 with spaces",
			input: "192.168.1.0/24, 2001:db8::/32, fe80::/64",
			want:  []string{"192.168.1.0/24", "2001:db8::/32", "fe80::/64"},
		},
		{
			name:      "empty CIDR entries cause error",
			input:     "192.168.1.0/24,,35.191.0.0/16",
			wantError: true,
		},
		{
			name:      "trailing comma causes error",
			input:     "192.168.1.0/24,",
			wantError: true,
		},
		{
			name:      "leading comma causes error",
			input:     ",192.168.1.0/24",
			wantError: true,
		},
		{
			name:      "multiple spaces and commas cause error",
			input:     " 192.168.1.0/24 , , 35.191.0.0/16 ",
			wantError: true,
		},
		// Error cases
		{
			name:      "single IP without netmask",
			input:     "192.168.1.1",
			wantError: true,
		},
		{
			name:      "invalid IP address",
			input:     "300.300.300.300/24",
			wantError: true,
		},
		{
			name:      "invalid netmask",
			input:     "192.168.1.0/33",
			wantError: true,
		},
		{
			name:      "invalid CIDR format",
			input:     "not-an-ip/24",
			wantError: true,
		},
		{
			name:      "valid and invalid CIDRs mixed",
			input:     "192.168.1.0/24,invalid-cidr",
			wantError: true,
		},
		{
			name:      "IPv6 single IP without netmask",
			input:     "2001:db8::1",
			wantError: true,
		},
		{
			name:      "invalid IPv6 address",
			input:     "gggg::/32",
			wantError: true,
		},
		{
			name:      "invalid IPv6 netmask",
			input:     "2001:db8::/129",
			wantError: true,
		},
		{
			name:      "invalid IPv6 format",
			input:     "2001:db8:::/32",
			wantError: true,
		},
		{
			name:      "valid IPv4 and invalid IPv6 mixed",
			input:     "192.168.1.0/24,2001:db8:::/32",
			wantError: true,
		},
		{
			name:      "valid IPv6 and invalid IPv4 mixed",
			input:     "2001:db8::/32,300.300.300.300/24",
			wantError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseHealthCheckSourceCIDRs(tc.input)
			if (err != nil) != tc.wantError {
				t.Errorf("ParseHealthCheckSourceCIDRs(%q) expected error=%v, got error=%v", tc.input, tc.wantError, err != nil)
			}
			if err == nil {
				if diff := cmp.Diff(tc.want, got); diff != "" {
					t.Errorf("ParseHealthCheckSourceCIDRs(%q) returned diff (-want +got):\n%s", tc.input, diff)
				}
			}

			validateErr := ValidateHealthCheckSourceCIDRs(tc.input)
			if (validateErr != nil) != tc.wantError {
				t.Errorf("ValidateHealthCheckSourceCIDRs(%q) expected error=%v, got error=%v", tc.input, tc.wantError, validateErr != nil)
			}
		})
	}
}

func TestValidateCIDR(t *testing.T) {
	testCases := []struct {
		name      string
		input     string
		wantError bool
	}{
		{
			name:  "valid IPv4 CIDR",
			input: "192.168.1.0/24",
		},
		{
			name:  "valid IPv6 CIDR",
			input: "2001:db8::/32",
		},
		{
			name:      "invalid IPv4 CIDR",
			input:     "192.168.1.0/33",
			wantError: true,
		},
		{
			name:      "invalid IPv6 CIDR",
			input:     "2001:db8::/129",
			wantError: true,
		},
		{
			name:      "IP without netmask",
			input:     "192.168.1.1",
			wantError: true,
		},
		{
			name:      "invalid format",
			input:     "not-an-ip/24",
			wantError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateCIDR(tc.input)
			if (err != nil) != tc.wantError {
				t.Errorf("ValidateCIDR(%q) expected error=%v, got error=%v", tc.input, tc.wantError, err != nil)
			}
		})
	}
}
