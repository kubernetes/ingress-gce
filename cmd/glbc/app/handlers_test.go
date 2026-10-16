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

package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/GoogleCloudPlatform/gke-enterprise-mt/pkg/mtmetrics"
	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/klog/v2"
)

type nonGathererRegistry struct{}

func (nonGathererRegistry) Register(string, prometheus.Gatherer) error { return nil }
func (nonGathererRegistry) Unregister(string)                          {}

func TestLoopbackOnly(t *testing.T) {
	okHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("multitenancy_metrics_ok"))
	})
	wrapped := loopbackOnly(okHandler)

	testCases := []struct {
		name           string
		remoteAddr     string
		wantStatusCode int
		wantBodySubstr string
	}{
		{
			name:           "IPv4 loopback 127.0.0.1 is allowed",
			remoteAddr:     "127.0.0.1:54321",
			wantStatusCode: http.StatusOK,
			wantBodySubstr: "multitenancy_metrics_ok",
		},
		{
			name:           "IPv4 loopback 127.0.0.2 is allowed",
			remoteAddr:     "127.0.0.2:8087",
			wantStatusCode: http.StatusOK,
			wantBodySubstr: "multitenancy_metrics_ok",
		},
		{
			name:           "IPv6 loopback ::1 is allowed",
			remoteAddr:     "[::1]:54321",
			wantStatusCode: http.StatusOK,
			wantBodySubstr: "multitenancy_metrics_ok",
		},
		{
			name:           "External IPv4 is forbidden",
			remoteAddr:     "10.128.0.5:54321",
			wantStatusCode: http.StatusForbidden,
			wantBodySubstr: "Forbidden: accessed from outside localhost",
		},
		{
			name:           "External IPv6 is forbidden",
			remoteAddr:     "[2001:db8::1]:54321",
			wantStatusCode: http.StatusForbidden,
			wantBodySubstr: "Forbidden: accessed from outside localhost",
		},
		{
			name:           "Missing port in RemoteAddr (net.SplitHostPort error) is forbidden",
			remoteAddr:     "127.0.0.1",
			wantStatusCode: http.StatusForbidden,
			wantBodySubstr: "Forbidden: accessed from outside localhost",
		},
		{
			name:           "Empty RemoteAddr (net.SplitHostPort error) is forbidden",
			remoteAddr:     "",
			wantStatusCode: http.StatusForbidden,
			wantBodySubstr: "Forbidden: accessed from outside localhost",
		},
		{
			name:           "Malformed host:port with multiple colons (net.SplitHostPort error) is forbidden",
			remoteAddr:     "127.0.0.1:8080:8087",
			wantStatusCode: http.StatusForbidden,
			wantBodySubstr: "Forbidden: accessed from outside localhost",
		},
		{
			name:           "Malformed IPv6 host:port missing closing bracket (net.SplitHostPort error) is forbidden",
			remoteAddr:     "[::1:8087",
			wantStatusCode: http.StatusForbidden,
			wantBodySubstr: "Forbidden: accessed from outside localhost",
		},
		{
			name:           "Invalid IP in RemoteAddr (net.ParseIP nil) is forbidden",
			remoteAddr:     "not-an-ip:8087",
			wantStatusCode: http.StatusForbidden,
			wantBodySubstr: "Forbidden: accessed from outside localhost",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/metrics/multitenancy", nil)
			req.RemoteAddr = tc.remoteAddr
			rec := httptest.NewRecorder()

			wrapped.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatusCode {
				t.Errorf("loopbackOnly(%q) status = %d, want %d", tc.remoteAddr, rec.Code, tc.wantStatusCode)
			}
			if body := rec.Body.String(); !strings.Contains(body, tc.wantBodySubstr) {
				t.Errorf("loopbackOnly(%q) body = %q, want substring %q", tc.remoteAddr, body, tc.wantBodySubstr)
			}
		})
	}
}

func TestMultitenancyMetricsEndpoint(t *testing.T) {
	origMultiProject := flags.F.EnableMultiProjectMode
	origGatherer := mtmetrics.DefaultMultiGatherer
	defer func() {
		flags.F.EnableMultiProjectMode = origMultiProject
		mtmetrics.DefaultMultiGatherer = origGatherer
	}()

	testCases := []struct {
		name                   string
		enableMultiProjectMode bool
		gatherer               mtmetrics.TenantGathererRegistry
		remoteAddr             string
		wantStatusCode         int
	}{
		{
			name:                   "/metrics/multitenancy rejects external non-loopback requests when multi-project mode enabled",
			enableMultiProjectMode: true,
			gatherer:               origGatherer,
			remoteAddr:             "10.128.0.5:54321",
			wantStatusCode:         http.StatusForbidden,
		},
		{
			name:                   "/metrics/multitenancy allows loopback requests when multi-project mode enabled",
			enableMultiProjectMode: true,
			gatherer:               origGatherer,
			remoteAddr:             "127.0.0.1:54321",
			wantStatusCode:         http.StatusOK,
		},
		{
			name:                   "/metrics/multitenancy is not registered when multi-project mode disabled",
			enableMultiProjectMode: false,
			gatherer:               origGatherer,
			remoteAddr:             "127.0.0.1:54321",
			wantStatusCode:         http.StatusNotFound,
		},
		{
			name:                   "/metrics/multitenancy is not registered and does not crash when DefaultMultiGatherer does not implement prometheus.Gatherer",
			enableMultiProjectMode: true,
			gatherer:               nonGathererRegistry{},
			remoteAddr:             "127.0.0.1:54321",
			wantStatusCode:         http.StatusNotFound,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			flags.F.EnableMultiProjectMode = tc.enableMultiProjectMode
			mtmetrics.DefaultMultiGatherer = tc.gatherer
			mux := http.NewServeMux()
			if flags.F.EnableMultiProjectMode {
				registerMultitenancyMetricsHandler(mux, klog.TODO())
			}

			req := httptest.NewRequest(http.MethodGet, "/metrics/multitenancy", nil)
			req.RemoteAddr = tc.remoteAddr
			rec := httptest.NewRecorder()

			mux.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatusCode {
				t.Errorf("GET /metrics/multitenancy (EnableMultiProjectMode=%v) from %s returned status %d, want %d",
					tc.enableMultiProjectMode, tc.remoteAddr, rec.Code, tc.wantStatusCode)
			}
		})
	}
}
