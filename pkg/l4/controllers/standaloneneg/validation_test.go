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
	"strings"
	"testing"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"github.com/google/go-cmp/cmp"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/l4/resources"
	l4utils "k8s.io/ingress-gce/pkg/l4/utils"
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

func TestBackendServiceKey(t *testing.T) {
	testCases := []struct {
		desc      string
		bsURL     string
		wantKey   *meta.Key
		wantError bool
	}{
		{
			desc:    "regional backend service",
			bsURL:   "https://www.googleapis.com/compute/v1/projects/p/regions/us-central1/backendServices/bs",
			wantKey: meta.RegionalKey("bs", "us-central1"),
		},
		{
			desc:    "global backend service",
			bsURL:   "https://www.googleapis.com/compute/v1/projects/p/global/backendServices/bs",
			wantKey: meta.GlobalKey("bs"),
		},
		{
			desc:      "missing backend service",
			bsURL:     "",
			wantError: true,
		},
		{
			desc:      "invalid backend service URL",
			bsURL:     "not/a/url",
			wantError: true,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			key, err := backendServiceKey(&composite.ForwardingRule{BackendService: tc.bsURL})
			if tc.wantError {
				if err == nil || !resources.IsUserError(err) {
					t.Errorf("backendServiceKey() error = %v, want a user error", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("backendServiceKey() error = %v", err)
			}
			if *key != *tc.wantKey {
				t.Errorf("backendServiceKey() = %v, want %v", key, tc.wantKey)
			}
		})
	}
}

func TestValidateBackendServiceAttachment(t *testing.T) {
	activeNEG := negSelfLink(snegTestProject, snegTestZone, snegTestNEG)
	drainingNEG := negSelfLink(snegTestProject, snegTestZone, "old-neg")
	otherNEG := negSelfLink(snegTestProject, snegTestZone, "other-neg")
	negs := serviceNEGs{active: negKeys(t, activeNEG), draining: negKeys(t, drainingNEG)}
	noNEGs := serviceNEGs{active: negKeys(t), draining: negKeys(t)}

	testCases := []struct {
		desc     string
		backends []*composite.Backend
		negs     serviceNEGs
		// wantWarning is a substring of the expected DrainingNEGsAttached warning message, if any.
		wantWarning string
		// wantError is a substring of the expected backend not attached user error, if any.
		wantError string
	}{
		{
			desc:     "active NEG attached",
			backends: []*composite.Backend{{Group: otherNEG}, {Group: activeNEG}},
			negs:     negs,
		},
		{
			desc:     "active and draining NEGs attached",
			backends: []*composite.Backend{{Group: drainingNEG}, {Group: activeNEG}},
			negs:     negs,
		},
		{
			desc:        "only draining NEG attached",
			backends:    []*composite.Backend{{Group: otherNEG}, {Group: drainingNEG}},
			negs:        negs,
			wantWarning: "Backend service bs only has NEGs attached that are no longer synced, of a previous NEG name or of a removed subnet: us-central1-a/old-neg.",
		},
		{
			desc:      "service NEG not attached",
			backends:  []*composite.Backend{{Group: otherNEG}},
			negs:      negs,
			wantError: "the service NEGs are not attached",
		},
		{
			desc:      "invalid backends are skipped",
			backends:  []*composite.Backend{nil, {Group: ""}, {Group: "invalid"}},
			negs:      negs,
			wantError: "the service NEGs are not attached",
		},
		{
			desc:      "no service NEGs",
			backends:  []*composite.Backend{{Group: otherNEG}},
			negs:      noNEGs,
			wantError: "no NEGs of the service found in its ServiceNetworkEndpointGroups",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			warnings, err := validateBackendServiceAttachment(&composite.BackendService{Name: "bs", Backends: tc.backends}, tc.negs)
			if tc.wantError == "" && err != nil {
				t.Errorf("validateBackendServiceAttachment() error = %v, want nil", err)
			}
			if tc.wantError != "" && (!l4utils.IsBackendNotAttachedError(err) || !resources.IsUserError(err) || !strings.Contains(err.Error(), tc.wantError)) {
				t.Errorf("validateBackendServiceAttachment() error = %v, want a backend not attached user error containing %q", err, tc.wantError)
			}
			switch {
			case tc.wantWarning == "" && len(warnings) != 0:
				t.Errorf("validateBackendServiceAttachment() warnings = %v, want none", warnings)
			case tc.wantWarning != "" && (len(warnings) != 1 || warnings[0].reason != "DrainingNEGsAttached" || !strings.Contains(warnings[0].message, tc.wantWarning)):
				t.Errorf("validateBackendServiceAttachment() warnings = %v, want one DrainingNEGsAttached warning containing %q", warnings, tc.wantWarning)
			}
		})
	}
}

func TestForwardingRuleVIPs(t *testing.T) {
	testCases := []struct {
		desc string
		fr   *composite.ForwardingRule
		want []string
	}{
		{
			desc: "regional forwarding rule",
			fr:   &composite.ForwardingRule{IPAddress: "10.0.0.1"},
			want: []string{"10.0.0.1"},
		},
		{
			desc: "global forwarding rule",
			fr:   &composite.ForwardingRule{IPAddresses: []string{"10.0.0.1", "2001:db8::1/96"}},
			want: []string{"10.0.0.1", "2001:db8::1"},
		},
		{
			desc: "IPv6 address is made canonical",
			fr:   &composite.ForwardingRule{IPAddress: "2001:0db8:0000:0000:0000:0000:0000:0001/96"},
			want: []string{"2001:db8::1"},
		},
		{
			desc: "empty addresses are skipped",
			fr:   &composite.ForwardingRule{IPAddresses: []string{""}},
			want: nil,
		},
		{
			desc: "unparsable address is kept",
			fr:   &composite.ForwardingRule{IPAddress: "not-an-ip"},
			want: []string{"not-an-ip"},
		},
		{
			desc: "no address",
			fr:   &composite.ForwardingRule{},
			want: nil,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			if diff := cmp.Diff(tc.want, forwardingRuleVIPs(tc.fr)); diff != "" {
				t.Errorf("forwardingRuleVIPs() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestHealthCheckKey(t *testing.T) {
	testCases := []struct {
		desc      string
		hcURL     string
		wantKey   *meta.Key
		wantError bool
	}{
		{
			desc:    "regional health check",
			hcURL:   "https://www.googleapis.com/compute/v1/projects/p/regions/us-central1/healthChecks/hc",
			wantKey: meta.RegionalKey("hc", "us-central1"),
		},
		{
			desc:    "global health check",
			hcURL:   "https://www.googleapis.com/compute/v1/projects/p/global/healthChecks/hc",
			wantKey: meta.GlobalKey("hc"),
		},
		{
			desc:      "not a health check",
			hcURL:     "https://www.googleapis.com/compute/v1/projects/p/global/backendServices/bs",
			wantError: true,
		},
		{
			desc:      "invalid URL",
			hcURL:     "invalid",
			wantError: true,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			key, err := healthCheckKey(&composite.BackendService{Name: "bs", HealthChecks: []string{tc.hcURL}})
			if tc.wantError {
				if err == nil {
					t.Errorf("healthCheckKey() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("healthCheckKey() error = %v", err)
			}
			if *key != *tc.wantKey {
				t.Errorf("healthCheckKey() = %v, want %v", key, tc.wantKey)
			}
		})
	}
}

func TestHTTPHealthCheckPort(t *testing.T) {
	testCases := []struct {
		desc          string
		hc            *composite.HealthCheck
		wantPort      int64
		wantNotUsable bool
	}{
		{
			desc:     "HTTP health check with a fixed port",
			hc:       httpHC("hc", 12345),
			wantPort: 12345,
		},
		{
			desc:          "HTTP health check using the serving port",
			hc:            httpHCWithServingPort("hc"),
			wantNotUsable: true,
		},
		{
			desc:          "TCP health check",
			hc:            tcpHC("hc", 12345),
			wantNotUsable: true,
		},
		{
			desc:          "HTTP health check without HTTP settings",
			hc:            &composite.HealthCheck{Name: "hc", Type: "HTTP"},
			wantNotUsable: true,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			port, notUsableReason := httpHealthCheckPort(tc.hc, "hc-url")
			if port != tc.wantPort {
				t.Errorf("httpHealthCheckPort() port = %d, want %d", port, tc.wantPort)
			}
			if gotNotUsable := notUsableReason != ""; gotNotUsable != tc.wantNotUsable {
				t.Errorf("httpHealthCheckPort() notUsableReason = %q, want not usable = %v", notUsableReason, tc.wantNotUsable)
			}
		})
	}
}
