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

	compute "google.golang.org/api/compute/v1"

	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/utils"
)

var namer = utils.NewNamer("uid1", "fw1")

func TestHealthCheckAdd(t *testing.T) {
	hcp := NewFakeHealthCheckProvider()
	healthChecks := NewHealthChecker(hcp, "/", namer)

	hc := healthChecks.New(80, annotations.ProtocolHTTP, false)
	_, err := healthChecks.Sync(hc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Verify the health check exists
	_, err = hcp.GetHealthCheck(namer.Backend(80))
	if err != nil {
		t.Fatalf("expected the health check to exist, err: %v", err)
	}

	hc = healthChecks.New(443, annotations.ProtocolHTTPS, false)
	_, err = healthChecks.Sync(hc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Verify the health check exists
	_, err = hcp.GetHealthCheck(namer.Backend(443))
	if err != nil {
		t.Fatalf("expected the health check to exist, err: %v", err)
	}

	hc = healthChecks.New(3000, annotations.ProtocolHTTP2, false)
	_, err = healthChecks.Sync(hc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Verify the health check exists
	_, err = hcp.GetHealthCheck(namer.Backend(3000))
	if err != nil {
		t.Fatalf("expected the health check to exist, err: %v", err)
	}
}

func TestHealthCheckAddExisting(t *testing.T) {
	hcp := NewFakeHealthCheckProvider()
	healthChecks := NewHealthChecker(hcp, "/", namer)

	// HTTP
	// Manually insert a health check
	httpHC := DefaultHealthCheck(3000, annotations.ProtocolHTTP)
	httpHC.Name = namer.Backend(3000)
	httpHC.RequestPath = "/my-probes-health"
	v1hc, err := httpHC.ToComputeHealthCheck()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	hcp.CreateHealthCheck(v1hc)

	// Should not fail adding the same type of health check
	hc := healthChecks.New(3000, annotations.ProtocolHTTP, false)
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
	httpsHC.Name = namer.Backend(4000)
	httpsHC.RequestPath = "/my-probes-health"
	v1hc, err = httpHC.ToComputeHealthCheck()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	hcp.CreateHealthCheck(v1hc)

	hc = healthChecks.New(4000, annotations.ProtocolHTTPS, false)
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
	http2.Name = namer.Backend(5000)
	http2.RequestPath = "/my-probes-health"
	v1hc, err = httpHC.ToComputeHealthCheck()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	hcp.CreateHealthCheck(v1hc)

	hc = healthChecks.New(5000, annotations.ProtocolHTTPS, false)
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
	healthChecks := NewHealthChecker(hcp, "/", namer)

	// Create HTTP HC for 1234
	hc := DefaultHealthCheck(1234, annotations.ProtocolHTTP)
	hc.Name = namer.Backend(1234)
	v1hc, err := hc.ToComputeHealthCheck()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	hcp.CreateHealthCheck(v1hc)

	// Create HTTPS HC for 1234)
	v1hc.Type = string(annotations.ProtocolHTTPS)
	hcp.CreateHealthCheck(v1hc)

	// Delete only HTTP 1234
	err = healthChecks.Delete(1234)
	if err != nil {
		t.Errorf("unexpected error when deleting health check, err: %v", err)
	}

	// Validate port is deleted
	_, err = hcp.GetHealthCheck(hc.Name)
	if !utils.IsHTTPErrorCode(err, http.StatusNotFound) {
		t.Errorf("expected not-found error, actual: %v", err)
	}

	// Delete only HTTP 1234
	err = healthChecks.Delete(1234)
	if err == nil {
		t.Errorf("expected not-found error when deleting health check, err: %v", err)
	}
}

func TestHTTP2HealthCheckDelete(t *testing.T) {
	hcp := NewFakeHealthCheckProvider()
	healthChecks := NewHealthChecker(hcp, "/", namer)

	// Create HTTP2 HC for 1234
	hc := DefaultHealthCheck(1234, annotations.ProtocolHTTP2)
	hc.Name = namer.Backend(1234)

	alphahc := hc.ToAlphaComputeHealthCheck()
	hcp.CreateAlphaHealthCheck(alphahc)

	// Delete only HTTP2 1234
	err := healthChecks.Delete(1234)
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
	healthChecks := NewHealthChecker(hcp, "/", namer)

	// HTTP
	// Manually insert a health check
	hc := DefaultHealthCheck(3000, annotations.ProtocolHTTP)
	hc.Name = namer.Backend(3000)
	hc.RequestPath = "/my-probes-health"
	v1hc, err := hc.ToComputeHealthCheck()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	hcp.CreateHealthCheck(v1hc)

	// Verify the health check exists
	_, err = healthChecks.Get(3000, false)
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
	_, err = healthChecks.Get(3000, false)
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
	_, err = healthChecks.Get(3000, true)
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
	hc, err = healthChecks.Get(3000, true)
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
	hc, err = healthChecks.Get(3000, true)
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

func TestHealthCheckDeleteLegacy(t *testing.T) {
	hcp := NewFakeHealthCheckProvider()
	healthChecks := NewHealthChecker(hcp, "/", namer)

	err := hcp.CreateHttpHealthCheck(&compute.HttpHealthCheck{
		Name: namer.Backend(80),
	})
	if err != nil {
		t.Fatalf("expected health check to be created, err: %v", err)
	}

	err = healthChecks.DeleteLegacy(80)
	if err != nil {
		t.Fatalf("expected health check to be deleted, err: %v", err)
	}

}

func TestAlphaHealthCheck(t *testing.T) {
	hcp := NewFakeHealthCheckProvider()
	healthChecks := NewHealthChecker(hcp, "/", namer)
	hc := healthChecks.New(8000, annotations.ProtocolHTTP, true)
	_, err := healthChecks.Sync(hc)
	if err != nil {
		t.Fatalf("got %v, want nil", err)
	}

	ret, err := healthChecks.Get(8000, true)
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
