/*
Copyright 2021 The Kubernetes Authors.

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

package translator

import (
	"testing"

	computealpha "google.golang.org/api/compute/v0.alpha"
	"k8s.io/ingress-gce/pkg/annotations"
)

func TestMerge(t *testing.T) {
	testCases := []struct {
		desc    string
		hc      *HealthCheck
		wantErr bool
	}{
		{
			desc: "HTTP",
			hc:   &HealthCheck{HealthCheck: computealpha.HealthCheck{Type: "HTTP"}},
		},
		{
			desc: "HTTP2",
			hc:   &HealthCheck{HealthCheck: computealpha.HealthCheck{Type: "HTTP2"}},
		},
		{
			desc: "HTTPS",
			hc:   &HealthCheck{HealthCheck: computealpha.HealthCheck{Type: "HTTPS"}},
		},
		{
			desc:    "Malformed Protocol",
			hc:      &HealthCheck{HealthCheck: computealpha.HealthCheck{Type: "http"}},
			wantErr: true,
		},
		{
			desc: "PortSpecification is set",
			hc:   &HealthCheck{HealthCheck: computealpha.HealthCheck{Type: "HTTP"}, HTTPHealthCheck: computealpha.HTTPHealthCheck{PortSpecification: "FOO"}},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			err := tc.hc.merge()
			if (err != nil) != tc.wantErr {
				t.Errorf("hc.merge() = %v, wantErr = %t", err, tc.wantErr)
			}

			// Verify correct struct was set
			switch tc.hc.Protocol() {
			case annotations.ProtocolHTTP:
				if tc.hc.HttpHealthCheck == nil || tc.hc.HttpsHealthCheck != nil || tc.hc.Http2HealthCheck != nil {
					t.Errorf("Invalid HC %v for protocol %q", tc.hc, annotations.ProtocolHTTP)
				}
			case annotations.ProtocolHTTPS:
				if tc.hc.HttpHealthCheck != nil || tc.hc.HttpsHealthCheck == nil || tc.hc.Http2HealthCheck != nil {
					t.Errorf("Invalid HC %v for protocol %q", tc.hc, annotations.ProtocolHTTPS)
				}
			case annotations.ProtocolHTTP2:
				if tc.hc.HttpHealthCheck != nil || tc.hc.HttpsHealthCheck != nil || tc.hc.Http2HealthCheck == nil {
					t.Errorf("Invalid HC %v for protocol %q", tc.hc, annotations.ProtocolHTTP2)
				}
			}

			// Verify port spec
			if tc.hc.PortSpecification != "" && tc.hc.PortSpecification != "USE_FIXED_PORT" {
				if tc.hc.Port != 0 {
					t.Errorf("Error Port must be 0 if PortSpecification is specified: %v", tc.hc)
				}
			}
		})
	}
}
