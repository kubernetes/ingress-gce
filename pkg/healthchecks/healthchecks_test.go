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

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/utils"
)

var (
	namer = utils.NewNamer("uid1", "fw1")

	defaultBackendSvc = types.NamespacedName{Namespace: "system", Name: "default"}
)

func TestHealthCheckAdd(t *testing.T) {
	hcp := NewFakeHealthCheckProvider()
	healthChecks := NewHealthChecker(hcp, "/", "/healthz", namer, defaultBackendSvc)

	sp := utils.ServicePort{NodePort: 80, Protocol: annotations.ProtocolHTTP, NEGEnabled: false}
	hc := healthChecks.New(sp)
	_, err := healthChecks.Sync(hc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Verify the health check exists
	_, err = hcp.GetHealthCheck(namer.IGBackend(80))
	if err != nil {
		t.Fatalf("expected the health check to exist, err: %v", err)
	}

	sp = utils.ServicePort{NodePort: 443, Protocol: annotations.ProtocolHTTPS, NEGEnabled: false}
	hc = healthChecks.New(sp)
	_, err = healthChecks.Sync(hc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Verify the health check exists
	_, err = hcp.GetHealthCheck(namer.IGBackend(443))
	if err != nil {
		t.Fatalf("expected the health check to exist, err: %v", err)
	}

	sp = utils.ServicePort{NodePort: 3000, Protocol: annotations.ProtocolHTTP2, NEGEnabled: false}
	hc = healthChecks.New(sp)
	_, err = healthChecks.Sync(hc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Verify the health check exists
	_, err = hcp.GetHealthCheck(namer.IGBackend(3000))
	if err != nil {
		t.Fatalf("expected the health check to exist, err: %v", err)
	}
}

func TestHealthCheckAddExisting(t *testing.T) {
	hcp := NewFakeHealthCheckProvider()
	healthChecks := NewHealthChecker(hcp, "/", "/healthz", namer, defaultBackendSvc)

	// HTTP
	// Manually insert a health check
	httpHC := DefaultHealthCheck(3000, annotations.ProtocolHTTP)
	httpHC.Name = namer.IGBackend(3000)
	httpHC.RequestPath = "/my-probes-health"
	v1hc, err := httpHC.ToComputeHealthCheck()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	hcp.CreateHealthCheck(v1hc)

	sp := utils.ServicePort{NodePort: 3000, Protocol: annotations.ProtocolHTTP, NEGEnabled: false}
	// Should not fail adding the same type of health check
	hc := healthChecks.New(sp)
	_, err = healthChecks.Sync(hc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Verify the health check exists
	_, err = hcp.GetHealthCheck(httpHC.Name)
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
	hcp.CreateHealthCheck(v1hc)

	sp = utils.ServicePort{NodePort: 4000, Protocol: annotations.ProtocolHTTPS, NEGEnabled: false}
	hc = healthChecks.New(sp)
	_, err = healthChecks.Sync(hc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Verify the health check exists
	_, err = hcp.GetHealthCheck(httpsHC.Name)
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
	hcp.CreateHealthCheck(v1hc)

	sp = utils.ServicePort{NodePort: 5000, Protocol: annotations.ProtocolHTTPS, NEGEnabled: false}
	hc = healthChecks.New(sp)
	_, err = healthChecks.Sync(hc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Verify the health check exists
	_, err = hcp.GetHealthCheck(http2.Name)
	if err != nil {
		t.Fatalf("expected the health check to continue existing, err: %v", err)
	}
}

func TestHealthCheckDelete(t *testing.T) {
	hcp := NewFakeHealthCheckProvider()
	healthChecks := NewHealthChecker(hcp, "/", "/healthz", namer, defaultBackendSvc)

	// Create HTTP HC for 1234
	hc := DefaultHealthCheck(1234, annotations.ProtocolHTTP)
	hc.Name = namer.IGBackend(1234)
	v1hc, err := hc.ToComputeHealthCheck()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	hcp.CreateHealthCheck(v1hc)

	// Create HTTPS HC for 1234)
	v1hc.Type = string(annotations.ProtocolHTTPS)
	hcp.CreateHealthCheck(v1hc)

	// Delete only HTTP 1234
	err = healthChecks.Delete(namer.IGBackend(1234))
	if err != nil {
		t.Errorf("unexpected error when deleting health check, err: %v", err)
	}

	// Validate port is deleted
	_, err = hcp.GetHealthCheck(hc.Name)
	if !utils.IsHTTPErrorCode(err, http.StatusNotFound) {
		t.Errorf("expected not-found error, actual: %v", err)
	}

	// Delete only HTTP 1234
	err = healthChecks.Delete(namer.IGBackend(1234))
	if err == nil {
		t.Errorf("expected not-found error when deleting health check, err: %v", err)
	}
}

func TestHTTP2HealthCheckDelete(t *testing.T) {
	hcp := NewFakeHealthCheckProvider()
	healthChecks := NewHealthChecker(hcp, "/", "/healthz", namer, defaultBackendSvc)

	// Create HTTP2 HC for 1234
	hc := DefaultHealthCheck(1234, annotations.ProtocolHTTP2)
	hc.Name = namer.IGBackend(1234)

	alphahc := hc.ToAlphaComputeHealthCheck()
	hcp.CreateAlphaHealthCheck(alphahc)

	// Delete only HTTP2 1234
	err := healthChecks.Delete(namer.IGBackend(1234))
	if err != nil {
		t.Errorf("unexpected error when deleting health check, err: %v", err)
	}

	// Validate port is deleted
	_, err = hcp.GetAlphaHealthCheck(hc.Name)
	if !utils.IsHTTPErrorCode(err, http.StatusNotFound) {
		t.Errorf("expected not-found error, actual: %v", err)
	}
}

func TestHealthCheckUpdate(t *testing.T) {
	hcp := NewFakeHealthCheckProvider()
	healthChecks := NewHealthChecker(hcp, "/", "/healthz", namer, defaultBackendSvc)

	// HTTP
	// Manually insert a health check
	hc := DefaultHealthCheck(3000, annotations.ProtocolHTTP)
	hc.Name = namer.IGBackend(3000)
	hc.RequestPath = "/my-probes-health"
	v1hc, err := hc.ToComputeHealthCheck()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	hcp.CreateHealthCheck(v1hc)

	// Verify the health check exists
	_, err = healthChecks.Get(hc.Name, meta.VersionGA)
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
	_, err = healthChecks.Get(hc.Name, meta.VersionGA)
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
	_, err = healthChecks.Get(hc.Name, meta.VersionAlpha)
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
	hc, err = healthChecks.Get(hc.Name, meta.VersionAlpha)
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
	hc, err = healthChecks.Get(hc.Name, meta.VersionAlpha)
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
	hcp := NewFakeHealthCheckProvider()
	healthChecks := NewHealthChecker(hcp, "/", "/healthz", namer, defaultBackendSvc)
	sp := utils.ServicePort{NodePort: 8000, Protocol: annotations.ProtocolHTTPS, NEGEnabled: true}
	hc := healthChecks.New(sp)
	_, err := healthChecks.Sync(hc)
	if err != nil {
		t.Fatalf("got %v, want nil", err)
	}

	ret, err := healthChecks.Get(hc.Name, meta.VersionAlpha)
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
