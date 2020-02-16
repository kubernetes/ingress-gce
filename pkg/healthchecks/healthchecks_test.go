/*
Copyright 2017 The Kubernetes Authors.

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

package healthchecks

import (
	"net/http"
	"testing"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/mock"
	computealpha "google.golang.org/api/compute/v0.alpha"
	api_v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/utils"
	namer_util "k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/legacy-cloud-providers/gce"
)

var (
	namer = namer_util.NewNamer("uid1", "fw1")

	defaultBackendSvc = types.NamespacedName{Namespace: "system", Name: "default"}
)

func TestHealthCheckAdd(t *testing.T) {
	fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
	healthChecks := NewHealthChecker(fakeGCE, "/", defaultBackendSvc)

	sp := utils.ServicePort{NodePort: 80, Protocol: annotations.ProtocolHTTP, NEGEnabled: false, BackendNamer: namer}
	hc := healthChecks.New(sp)
	_, err := healthChecks.Sync(hc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Verify the health check exists
	_, err = fakeGCE.GetHealthCheck(namer.IGBackend(80))
	if err != nil {
		t.Fatalf("expected the health check to exist, err: %v", err)
	}

	sp = utils.ServicePort{NodePort: 443, Protocol: annotations.ProtocolHTTPS, NEGEnabled: false, BackendNamer: namer}
	hc = healthChecks.New(sp)
	_, err = healthChecks.Sync(hc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Verify the health check exists
	_, err = fakeGCE.GetHealthCheck(namer.IGBackend(443))
	if err != nil {
		t.Fatalf("expected the health check to exist, err: %v", err)
	}

	sp = utils.ServicePort{NodePort: 3000, Protocol: annotations.ProtocolHTTP2, NEGEnabled: false, BackendNamer: namer}
	hc = healthChecks.New(sp)
	_, err = healthChecks.Sync(hc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Verify the health check exists
	_, err = fakeGCE.GetHealthCheck(namer.IGBackend(3000))
	if err != nil {
		t.Fatalf("expected the health check to exist, err: %v", err)
	}
}

func TestHealthCheckAddExisting(t *testing.T) {
	fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
	healthChecks := NewHealthChecker(fakeGCE, "/", defaultBackendSvc)

	// HTTP
	// Manually insert a health check
	httpHC := DefaultHealthCheck(3000, annotations.ProtocolHTTP)
	httpHC.Name = namer.IGBackend(3000)
	httpHC.RequestPath = "/my-probes-health"
	v1hc, err := httpHC.ToComputeHealthCheck()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	fakeGCE.CreateHealthCheck(v1hc)

	sp := utils.ServicePort{NodePort: 3000, Protocol: annotations.ProtocolHTTP, NEGEnabled: false, BackendNamer: namer}
	// Should not fail adding the same type of health check
	hc := healthChecks.New(sp)
	_, err = healthChecks.Sync(hc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Verify the health check exists
	_, err = fakeGCE.GetHealthCheck(httpHC.Name)
	if err != nil {
		t.Fatalf("expected the health check to continue existing, err: %v", err)
	}

	// HTTPS
	// Manually insert a health check
	httpsHC := DefaultHealthCheck(4000, annotations.ProtocolHTTPS)
	httpsHC.Name = namer.IGBackend(4000)
	httpsHC.RequestPath = "/my-probes-health"
	v1hc, err = httpHC.ToComputeHealthCheck()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	fakeGCE.CreateHealthCheck(v1hc)

	sp = utils.ServicePort{NodePort: 4000, Protocol: annotations.ProtocolHTTPS, NEGEnabled: false, BackendNamer: namer}
	hc = healthChecks.New(sp)
	_, err = healthChecks.Sync(hc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Verify the health check exists
	_, err = fakeGCE.GetHealthCheck(httpsHC.Name)
	if err != nil {
		t.Fatalf("expected the health check to continue existing, err: %v", err)
	}

	// HTTP2
	// Manually insert a health check
	http2 := DefaultHealthCheck(5000, annotations.ProtocolHTTP2)
	http2.Name = namer.IGBackend(5000)
	http2.RequestPath = "/my-probes-health"
	v1hc, err = httpHC.ToComputeHealthCheck()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	fakeGCE.CreateHealthCheck(v1hc)

	sp = utils.ServicePort{NodePort: 5000, Protocol: annotations.ProtocolHTTPS, NEGEnabled: false, BackendNamer: namer}
	hc = healthChecks.New(sp)
	_, err = healthChecks.Sync(hc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Verify the health check exists
	_, err = fakeGCE.GetHealthCheck(http2.Name)
	if err != nil {
		t.Fatalf("expected the health check to continue existing, err: %v", err)
	}
}

func TestHealthCheckDelete(t *testing.T) {
	fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
	healthChecks := NewHealthChecker(fakeGCE, "/", defaultBackendSvc)

	// Create HTTP HC for 1234
	hc := DefaultHealthCheck(1234, annotations.ProtocolHTTP)
	hc.Name = namer.IGBackend(1234)
	v1hc, err := hc.ToComputeHealthCheck()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	fakeGCE.CreateHealthCheck(v1hc)

	// Create HTTPS HC for 1234)
	v1hc.Type = string(annotations.ProtocolHTTPS)
	fakeGCE.CreateHealthCheck(v1hc)

	// Delete only HTTP 1234
	err = healthChecks.Delete(namer.IGBackend(1234), meta.Global)
	if err != nil {
		t.Errorf("unexpected error when deleting health check, err: %v", err)
	}

	// Validate port is deleted
	_, err = fakeGCE.GetHealthCheck(hc.Name)
	if !utils.IsHTTPErrorCode(err, http.StatusNotFound) {
		t.Errorf("expected not-found error, actual: %v", err)
	}

	// Delete only HTTP 1234
	err = healthChecks.Delete(namer.IGBackend(1234), meta.Global)
	if err == nil {
		t.Errorf("expected not-found error when deleting health check, err: %v", err)
	}
}

func TestHTTP2HealthCheckDelete(t *testing.T) {
	fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
	healthChecks := NewHealthChecker(fakeGCE, "/", defaultBackendSvc)

	// Create HTTP2 HC for 1234
	hc := DefaultHealthCheck(1234, annotations.ProtocolHTTP2)
	hc.Name = namer.IGBackend(1234)

	alphahc := hc.ToAlphaComputeHealthCheck()
	fakeGCE.CreateAlphaHealthCheck(alphahc)

	// Delete only HTTP2 1234
	err := healthChecks.Delete(namer.IGBackend(1234), meta.Global)
	if err != nil {
		t.Errorf("unexpected error when deleting health check, err: %v", err)
	}

	// Validate port is deleted
	_, err = fakeGCE.GetAlphaHealthCheck(hc.Name)
	if !utils.IsHTTPErrorCode(err, http.StatusNotFound) {
		t.Errorf("expected not-found error, actual: %v", err)
	}
}

func TestHealthCheckUpdate(t *testing.T) {
	fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())

	// Add Hooks
	(fakeGCE.Compute().(*cloud.MockGCE)).MockHealthChecks.UpdateHook = mock.UpdateHealthCheckHook
	(fakeGCE.Compute().(*cloud.MockGCE)).MockAlphaHealthChecks.UpdateHook = mock.UpdateAlphaHealthCheckHook
	(fakeGCE.Compute().(*cloud.MockGCE)).MockBetaHealthChecks.UpdateHook = mock.UpdateBetaHealthCheckHook

	healthChecks := NewHealthChecker(fakeGCE, "/", defaultBackendSvc)

	// HTTP
	// Manually insert a health check
	hc := DefaultHealthCheck(3000, annotations.ProtocolHTTP)
	hc.Name = namer.IGBackend(3000)
	hc.RequestPath = "/my-probes-health"
	v1hc, err := hc.ToComputeHealthCheck()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	fakeGCE.CreateHealthCheck(v1hc)

	// Verify the health check exists
	_, err = healthChecks.Get(hc.Name, meta.VersionGA, meta.Global)
	if err != nil {
		t.Fatalf("expected the health check to exist, err: %v", err)
	}

	// Change to HTTPS
	hc.Type = string(annotations.ProtocolHTTPS)
	_, err = healthChecks.Sync(hc)
	if err != nil {
		t.Fatalf("unexpected err while syncing healthcheck, err %v", err)
	}

	// Verify the health check exists
	_, err = healthChecks.Get(hc.Name, meta.VersionGA, meta.Global)
	if err != nil {
		t.Fatalf("expected the health check to exist, err: %v", err)
	}

	// Verify the check is now HTTPS
	if hc.Protocol() != annotations.ProtocolHTTPS {
		t.Fatalf("expected check to be of type HTTPS")
	}

	// Change to HTTP2
	hc.Type = string(annotations.ProtocolHTTP2)
	_, err = healthChecks.Sync(hc)
	if err != nil {
		t.Fatalf("unexpected err while syncing healthcheck, err %v", err)
	}

	// Verify the health check exists. HTTP2 is alpha-only.
	_, err = healthChecks.Get(hc.Name, meta.VersionAlpha, meta.Global)
	if err != nil {
		t.Fatalf("expected the health check to exist, err: %v", err)
	}

	// Verify the check is now HTTP2
	if hc.Protocol() != annotations.ProtocolHTTP2 {
		t.Fatalf("expected check to be of type HTTP2")
	}

	// Change to NEG Health Check
	hc.ForNEG = true
	hc.PortSpecification = UseServingPortSpecification
	_, err = healthChecks.Sync(hc)

	if err != nil {
		t.Fatalf("unexpected err while syncing healthcheck, err %v", err)
	}

	// Verify the health check exists.
	hc, err = healthChecks.Get(hc.Name, meta.VersionAlpha, meta.Global)
	if err != nil {
		t.Fatalf("expected the health check to exist, err: %v", err)
	}

	if hc.Port != 0 {
		t.Fatalf("expected health check with PortSpecification to have no Port, got %v", hc.Port)
	}

	// Change back to regular Health Check
	hc.ForNEG = false
	hc.Port = 3000
	hc.PortSpecification = ""

	_, err = healthChecks.Sync(hc)
	if err != nil {
		t.Fatalf("unexpected err while syncing healthcheck, err %v", err)
	}

	// Verify the health check exists.
	hc, err = healthChecks.Get(hc.Name, meta.VersionAlpha, meta.Global)
	if err != nil {
		t.Fatalf("expected the health check to exist, err: %v", err)
	}

	if len(hc.PortSpecification) != 0 {
		t.Fatalf("expected health check with Port to have no PortSpecification, got %+v", hc)
	}

	if hc.Port == 0 {
		t.Fatalf("expected health check with PortSpecification to have Port")
	}
}

func TestAlphaHealthCheck(t *testing.T) {
	fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
	healthChecks := NewHealthChecker(fakeGCE, "/", defaultBackendSvc)
	sp := utils.ServicePort{NodePort: 8000, Protocol: annotations.ProtocolHTTPS, NEGEnabled: true, BackendNamer: namer}
	hc := healthChecks.New(sp)
	_, err := healthChecks.Sync(hc)
	if err != nil {
		t.Fatalf("got %v, want nil", err)
	}

	ret, err := healthChecks.Get(hc.Name, meta.VersionAlpha, meta.Global)
	if err != nil {
		t.Fatalf("got %v, want nil", err)
	}
	if ret.Port != 0 {
		t.Errorf("got ret.Port == %d, want 0", ret.Port)
	}
	if ret.PortSpecification != UseServingPortSpecification {
		t.Errorf("got ret.PortSpecification = %q, want %q", UseServingPortSpecification, ret.PortSpecification)
	}
}

func TestVersion(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		desc    string
		hc      *HealthCheck
		version meta.Version
	}{
		{
			desc: "Basic Http Health Check",
			hc: &HealthCheck{
				HealthCheck: computealpha.HealthCheck{
					Type: string(annotations.ProtocolHTTP),
				},
			},
			version: meta.VersionGA,
		},
		{
			desc: "Basic Http2 Health Check",
			hc: &HealthCheck{
				HealthCheck: computealpha.HealthCheck{
					Type: string(annotations.ProtocolHTTP2),
				},
			},
			version: meta.VersionBeta,
		},
		{
			desc: "Http Health Check with NEG",
			hc: &HealthCheck{
				HealthCheck: computealpha.HealthCheck{
					Type: string(annotations.ProtocolHTTP),
				},
				ForNEG: true,
			},
			version: meta.VersionBeta,
		},
		{
			desc: "Http2 Health Check with ILB and NEG",
			hc: &HealthCheck{
				HealthCheck: computealpha.HealthCheck{
					Type: string(annotations.ProtocolHTTP),
				},
				forILB: true,
				ForNEG: true,
			},
			version: meta.VersionBeta,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			result := tc.hc.Version()

			if result != tc.version {
				t.Fatalf("hc.Version() = %s, want %s", result, tc.version)
			}
		})
	}
}

func TestApplyProbeSettingsToHC(t *testing.T) {
	p := "healthz"
	hc := DefaultHealthCheck(8080, annotations.ProtocolHTTPS)
	probe := &api_v1.Probe{
		Handler: api_v1.Handler{
			HTTPGet: &api_v1.HTTPGetAction{
				Scheme: api_v1.URISchemeHTTP,
				Path:   p,
				Port: intstr.IntOrString{
					Type:   intstr.Int,
					IntVal: 80,
				},
			},
		},
	}

	ApplyProbeSettingsToHC(probe, hc)

	if hc.Protocol() != annotations.ProtocolHTTPS || hc.Port != 8080 {
		t.Errorf("Basic HC settings changed")
	}
	if hc.RequestPath != "/"+p {
		t.Errorf("Failed to apply probe's requestpath")
	}
}
