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
	"reflect"
	"testing"

	"github.com/kr/pretty"
	computealpha "google.golang.org/api/compute/v0.alpha"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/utils/healthcheck"
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

func TestOverwriteWithTHC(t *testing.T) {
	wantHC := &HealthCheck{
		ForNEG: true,
		HealthCheck: computealpha.HealthCheck{
			CheckIntervalSec:   5,
			TimeoutSec:         5,
			UnhealthyThreshold: 10,
			HealthyThreshold:   1,
			Type:               "HTTP",
			Description:        (&healthcheck.HealthcheckInfo{HealthcheckConfig: healthcheck.TransparentHC}).GenerateHealthcheckDescription(),
		},
		HTTPHealthCheck: computealpha.HTTPHealthCheck{
			Port:              7877,
			PortSpecification: "USE_FIXED_PORT",
			RequestPath:       "/api/podhealth",
		},
		healthcheckInfo: healthcheck.HealthcheckInfo{
			HealthcheckConfig: healthcheck.TransparentHC,
		},
	}

	hc := &HealthCheck{
		ForNEG: true,
	}
	OverwriteWithTHC(hc, 7877)
	if !reflect.DeepEqual(hc, wantHC) {
		t.Fatalf("Translate healthcheck is:\n%s, want:\n%s", pretty.Sprint(hc), pretty.Sprint(wantHC))
	}
}
