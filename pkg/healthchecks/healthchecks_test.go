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
	"context"
	"fmt"
	"net/http"
	"reflect"
	"testing"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/mock"
	"github.com/kr/pretty"
	computealpha "google.golang.org/api/compute/v0.alpha"
	computebeta "google.golang.org/api/compute/v0.beta"
	"google.golang.org/api/compute/v1"
	api_v1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/ingress-gce/pkg/annotations"
	backendconfigv1 "k8s.io/ingress-gce/pkg/apis/backendconfig/v1"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/loadbalancers/features"
	"k8s.io/ingress-gce/pkg/translator"
	"k8s.io/ingress-gce/pkg/utils"
	namer_util "k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/klog"
	"k8s.io/legacy-cloud-providers/gce"
)

var (
	testNamer         = namer_util.NewNamer("uid1", "fw1")
	testSPs           = map[string]*utils.ServicePort{}
	defaultBackendSvc = types.NamespacedName{Namespace: "system", Name: "default"}
)

func init() {
	// Init klog flags so we can see the V logs.
	klog.InitFlags(nil)
	// var logLevel string
	// flag.StringVar(&logLevel, "logLevel", "4", "test")
	// flag.Lookup("v").Value.Set(logLevel)

	// Generate many types of ServicePorts.
	// Example: sps["HTTP-8000-neg-nil"] is a ServicePort for HTTP with NEG-enabled.
	testSPs = map[string]*utils.ServicePort{}
	for _, p := range []annotations.AppProtocol{
		annotations.ProtocolHTTP,
		annotations.ProtocolHTTPS,
		annotations.ProtocolHTTP2,
	} {
		path := "/foo"
		num := int64(1234)

		for npk, np := range map[string]int64{"80": 80, "8000": 8000} {
			for _, mode := range []string{"reg", "neg", "ilb"} {
				for bck, bc := range map[string]*backendconfigv1.HealthCheckConfig{
					"nil": nil,
					"bc":  &backendconfigv1.HealthCheckConfig{RequestPath: &path},
					"bcall": &backendconfigv1.HealthCheckConfig{
						RequestPath:        &path,
						CheckIntervalSec:   &num,
						TimeoutSec:         &num,
						HealthyThreshold:   &num,
						UnhealthyThreshold: &num,
						Port:               &num,
					},
				} {
					sp := &utils.ServicePort{
						NodePort:     np,
						Protocol:     p,
						BackendNamer: testNamer,
					}
					switch mode {
					case "reg":
					case "neg":
						sp.NEGEnabled = true
					case "ilb":
						sp.NEGEnabled = true
						sp.L7ILBEnabled = true
					}
					if bc != nil {
						sp.BackendConfig = &backendconfigv1.BackendConfig{
							Spec: backendconfigv1.BackendConfigSpec{HealthCheck: bc},
						}
					}
					testSPs[fmt.Sprintf("%s-%s-%s-%s", p, npk, mode, bck)] = sp
				}
			}
		}
	}
}

func TestHealthCheckAdd(t *testing.T) {
	fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
	healthChecks := NewHealthChecker(fakeGCE, "/", defaultBackendSvc)

	sp := &utils.ServicePort{NodePort: 80, Protocol: annotations.ProtocolHTTP, NEGEnabled: false, BackendNamer: testNamer}
	_, err := healthChecks.SyncServicePort(sp, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Verify the health check exists
	_, err = fakeGCE.GetHealthCheck(testNamer.IGBackend(80))
	if err != nil {
		t.Fatalf("expected the health check to exist, err: %v", err)
	}

	sp = &utils.ServicePort{NodePort: 443, Protocol: annotations.ProtocolHTTPS, NEGEnabled: false, BackendNamer: testNamer}
	_, err = healthChecks.SyncServicePort(sp, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Verify the health check exists
	_, err = fakeGCE.GetHealthCheck(testNamer.IGBackend(443))
	if err != nil {
		t.Fatalf("expected the health check to exist, err: %v", err)
	}

	sp = &utils.ServicePort{NodePort: 3000, Protocol: annotations.ProtocolHTTP2, NEGEnabled: false, BackendNamer: testNamer}
	_, err = healthChecks.SyncServicePort(sp, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Verify the health check exists
	_, err = fakeGCE.GetHealthCheck(testNamer.IGBackend(3000))
	if err != nil {
		t.Fatalf("expected the health check to exist, err: %v", err)
	}
}

func TestHealthCheckAddExisting(t *testing.T) {
	fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
	healthChecks := NewHealthChecker(fakeGCE, "/", defaultBackendSvc)

	// HTTP
	// Manually insert a health check
	httpHC := translator.DefaultHealthCheck(3000, annotations.ProtocolHTTP)
	httpHC.Name = testNamer.IGBackend(3000)
	httpHC.RequestPath = "/my-probes-health"
	v1hc, err := httpHC.ToComputeHealthCheck()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	fakeGCE.CreateHealthCheck(v1hc)

	sp := &utils.ServicePort{NodePort: 3000, Protocol: annotations.ProtocolHTTP, NEGEnabled: false, BackendNamer: testNamer}
	// Should not fail adding the same type of health check
	_, err = healthChecks.SyncServicePort(sp, nil)
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
	httpsHC := translator.DefaultHealthCheck(4000, annotations.ProtocolHTTPS)
	httpsHC.Name = testNamer.IGBackend(4000)
	httpsHC.RequestPath = "/my-probes-health"
	v1hc, err = httpHC.ToComputeHealthCheck()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	fakeGCE.CreateHealthCheck(v1hc)

	sp = &utils.ServicePort{NodePort: 4000, Protocol: annotations.ProtocolHTTPS, NEGEnabled: false, BackendNamer: testNamer}
	_, err = healthChecks.SyncServicePort(sp, nil)
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
	http2 := translator.DefaultHealthCheck(5000, annotations.ProtocolHTTP2)
	http2.Name = testNamer.IGBackend(5000)
	http2.RequestPath = "/my-probes-health"
	v1hc, err = httpHC.ToComputeHealthCheck()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	fakeGCE.CreateHealthCheck(v1hc)

	sp = &utils.ServicePort{NodePort: 5000, Protocol: annotations.ProtocolHTTPS, NEGEnabled: false, BackendNamer: testNamer}
	_, err = healthChecks.SyncServicePort(sp, nil)
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
	hc := translator.DefaultHealthCheck(1234, annotations.ProtocolHTTP)
	hc.Name = testNamer.IGBackend(1234)
	v1hc, err := hc.ToComputeHealthCheck()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	fakeGCE.CreateHealthCheck(v1hc)

	// Create HTTPS HC for 1234)
	v1hc.Type = string(annotations.ProtocolHTTPS)
	fakeGCE.CreateHealthCheck(v1hc)

	// Delete only HTTP 1234
	err = healthChecks.Delete(testNamer.IGBackend(1234), meta.Global)
	if err != nil {
		t.Errorf("unexpected error when deleting health check, err: %v", err)
	}

	// Validate port is deleted
	_, err = fakeGCE.GetHealthCheck(hc.Name)
	if !utils.IsHTTPErrorCode(err, http.StatusNotFound) {
		t.Errorf("expected not-found error, actual: %v", err)
	}
}

func TestHTTP2HealthCheckDelete(t *testing.T) {
	fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
	healthChecks := NewHealthChecker(fakeGCE, "/", defaultBackendSvc)

	// Create HTTP2 HC for 1234
	hc := translator.DefaultHealthCheck(1234, annotations.ProtocolHTTP2)
	hc.Name = testNamer.IGBackend(1234)

	alphahc, err := hc.ToAlphaComputeHealthCheck()
	if err != nil {
		t.Error(err)
	}
	fakeGCE.CreateAlphaHealthCheck(alphahc)

	// Delete only HTTP2 1234
	err = healthChecks.Delete(testNamer.IGBackend(1234), meta.Global)
	if err != nil {
		t.Errorf("unexpected error when deleting health check, err: %v", err)
	}

	// Validate port is deleted
	_, err = fakeGCE.GetAlphaHealthCheck(hc.Name)
	if !utils.IsHTTPErrorCode(err, http.StatusNotFound) {
		t.Errorf("expected not-found error, actual: %v", err)
	}
}

func TestRegionalHealthCheckDelete(t *testing.T) {
	fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
	healthChecks := NewHealthChecker(fakeGCE, "/", defaultBackendSvc)

	hc := healthChecks.new(
		utils.ServicePort{
			ID: utils.ServicePortID{
				Service: types.NamespacedName{
					Namespace: "ns2",
					Name:      "svc2",
				},
			},
			Port:         80,
			Protocol:     annotations.ProtocolHTTP,
			NEGEnabled:   true,
			L7ILBEnabled: true,
			BackendNamer: testNamer,
		},
	)
	hcName := testNamer.NEG("ns2", "svc2", 80)

	if err := healthChecks.create(hc, nil); err != nil {
		t.Fatalf("healthchecks.Create(%q) = %v, want nil", hc.Name, err)
	}

	key, err := composite.CreateKey(fakeGCE, hc.Name, features.L7ILBScope())
	if err != nil {
		t.Fatalf("Unexpected error creating composite key: %v", err)
	}

	// Verify that Health-check exists.
	if existingHC, err := composite.GetHealthCheck(fakeGCE, key, features.L7ILBVersions().HealthCheck); err != nil || existingHC == nil {
		t.Fatalf("GetHealthCheck(%q) = %v, %v, want nil", hc.Name, existingHC, err)
	}

	// Delete HTTP health-check.
	if err = healthChecks.Delete(hcName, meta.Regional); err != nil {
		t.Errorf("healthchecks.Delete(%q, %q) = %v, want nil", hcName, meta.Regional, err)
	}

	// Validate health-check is deleted.
	if _, err = composite.GetHealthCheck(fakeGCE, key, features.L7ILBVersions().HealthCheck); !utils.IsHTTPErrorCode(err, http.StatusNotFound) {
		t.Errorf("Expected not-found error, actual: %v", err)
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
	hc := translator.DefaultHealthCheck(3000, annotations.ProtocolHTTP)
	hc.Name = testNamer.IGBackend(3000)
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
	_, err = healthChecks.sync(hc, nil)
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
	_, err = healthChecks.sync(hc, nil)
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
	hc.PortSpecification = "USE_SERVING_PORT"
	_, err = healthChecks.sync(hc, nil)

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

	_, err = healthChecks.sync(hc, nil)
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

func TestVersion(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		desc    string
		hc      *translator.HealthCheck
		version meta.Version
	}{
		{
			desc: "Basic Http Health Check",
			hc: &translator.HealthCheck{
				HealthCheck: computealpha.HealthCheck{
					Type: string(annotations.ProtocolHTTP),
				},
			},
			version: meta.VersionGA,
		},
		{
			desc: "Basic Http2 Health Check",
			hc: &translator.HealthCheck{
				HealthCheck: computealpha.HealthCheck{
					Type: string(annotations.ProtocolHTTP2),
				},
			},
			version: meta.VersionBeta,
		},
		{
			desc: "Http Health Check with NEG",
			hc: &translator.HealthCheck{
				HealthCheck: computealpha.HealthCheck{
					Type: string(annotations.ProtocolHTTP),
				},
				ForNEG: true,
			},
			version: meta.VersionBeta,
		},
		{
			desc: "Http Health Check with ILB and NEG",
			hc: &translator.HealthCheck{
				HealthCheck: computealpha.HealthCheck{
					Type: string(annotations.ProtocolHTTP),
				},
				ForILB: true,
				ForNEG: true,
			},
			version: meta.VersionGA,
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
	type ws struct {
		host             string
		path             string
		timeoutSec       int64
		checkIntervalSec int64
		port             int64
	}
	for _, tc := range []struct {
		desc  string
		neg   bool
		probe *api_v1.Probe
		want  ws
	}{
		{
			desc:  "no override",
			probe: &api_v1.Probe{},
			want:  ws{timeoutSec: 60, checkIntervalSec: 60, port: 8080},
		},
		{
			desc: "override path",
			probe: &api_v1.Probe{
				TimeoutSeconds: 1234,
				Handler: api_v1.Handler{
					HTTPGet: &api_v1.HTTPGetAction{Path: "/override"},
				},
			},
			want: ws{path: "/override", timeoutSec: 1234, checkIntervalSec: 60, port: 8080},
		},
		{
			desc: "override host",
			probe: &api_v1.Probe{
				TimeoutSeconds: 1234,
				Handler: api_v1.Handler{
					HTTPGet: &api_v1.HTTPGetAction{Host: "foo.com"},
				},
			},
			want: ws{host: "foo.com", path: "/", timeoutSec: 1234, checkIntervalSec: 60, port: 8080},
		},
		{
			desc: "override host (via header)",
			probe: &api_v1.Probe{
				TimeoutSeconds: 1234,
				Handler: api_v1.Handler{
					HTTPGet: &api_v1.HTTPGetAction{
						HTTPHeaders: []api_v1.HTTPHeader{{Name: "Host", Value: "foo.com"}},
					},
				},
			},
			want: ws{host: "foo.com", path: "/", timeoutSec: 1234, checkIntervalSec: 60, port: 8080},
		},
		{
			// TODO(bowei): this is somewhat subtle behavior.
			desc: "ignore port",
			probe: &api_v1.Probe{
				TimeoutSeconds: 1234,
				Handler: api_v1.Handler{
					HTTPGet: &api_v1.HTTPGetAction{Port: intstr.FromInt(3000)},
				},
			},
			want: ws{path: "/", timeoutSec: 1234, checkIntervalSec: 60, port: 8080},
		},
		{
			desc: "override timeouts",
			probe: &api_v1.Probe{
				TimeoutSeconds: 50,
				PeriodSeconds:  100,
				Handler: api_v1.Handler{
					HTTPGet: &api_v1.HTTPGetAction{Port: intstr.FromInt(3000)},
				},
			},
			want: ws{path: "/", timeoutSec: 50, checkIntervalSec: 160, port: 8080},
		},
		{
			desc: "override timeouts (neg)",
			neg:  true,
			probe: &api_v1.Probe{
				TimeoutSeconds: 50,
				PeriodSeconds:  100,
				Handler: api_v1.Handler{
					HTTPGet: &api_v1.HTTPGetAction{Port: intstr.FromInt(3000)},
				},
			},
			want: ws{path: "/", timeoutSec: 50, checkIntervalSec: 100, port: 0},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			var got *translator.HealthCheck
			if tc.neg {
				got = translator.DefaultNEGHealthCheck(annotations.ProtocolHTTP)
			} else {
				got = translator.DefaultHealthCheck(8080, annotations.ProtocolHTTP)
			}
			translator.ApplyProbeSettingsToHC(tc.probe, got)

			if got.Host != tc.want.host {
				t.Errorf("got.Host = %q, want %q", got.Host, tc.want.host)
			}
			if got.RequestPath != tc.want.path {
				t.Errorf("got.RequestPath = %q, want %q", got.RequestPath, tc.want.path)
			}
			if got.TimeoutSec != tc.want.timeoutSec {
				t.Errorf("got.TimeoutSec = %d, want %d", got.TimeoutSec, tc.want.timeoutSec)
			}
			if got.CheckIntervalSec != tc.want.checkIntervalSec {
				t.Errorf("got.CheckIntervalSec = %d, want %d", got.CheckIntervalSec, tc.want.checkIntervalSec)
			}
			if got.Port != tc.want.port {
				t.Errorf("got.Port = %d, want %d", got.Port, tc.want.port)
			}
		})
	}
}

func TestCalculateDiff(t *testing.T) {
	t.Parallel()

	type tc struct {
		desc     string
		old, new *translator.HealthCheck
		c        *backendconfigv1.HealthCheckConfig
		hasDiff  bool
	}
	cases := []tc{
		{
			desc: "HTTP, no change",
			old:  translator.DefaultHealthCheck(8080, annotations.ProtocolHTTP),
			new:  translator.DefaultHealthCheck(8080, annotations.ProtocolHTTP),
		},
		{
			desc:    "HTTP => HTTPS",
			old:     translator.DefaultHealthCheck(8080, annotations.ProtocolHTTP),
			new:     translator.DefaultHealthCheck(8080, annotations.ProtocolHTTPS),
			hasDiff: true,
		},
		{
			// TODO(bowei) -- this seems wrong.
			desc: "Port change",
			old:  translator.DefaultHealthCheck(8080, annotations.ProtocolHTTP),
			new:  translator.DefaultHealthCheck(443, annotations.ProtocolHTTP),
			// hasDiff: true
		},
		{
			desc:    "PortSpecification",
			old:     translator.DefaultHealthCheck(8080, annotations.ProtocolHTTP),
			new:     translator.DefaultNEGHealthCheck(annotations.ProtocolHTTP),
			hasDiff: true,
		},
	}

	// TODO(bowei) -- this seems wrong, we should be checking for Port changes.
	newHC := translator.DefaultHealthCheck(8080, annotations.ProtocolHTTP)
	newHC.Port = 80
	cases = append(cases, tc{
		desc: "Port",
		old:  translator.DefaultHealthCheck(8080, annotations.ProtocolHTTP),
		new:  newHC,
		// hasDiff: true
	})

	newHC = translator.DefaultHealthCheck(8080, annotations.ProtocolHTTP)
	newHC.CheckIntervalSec = 1000
	cases = append(cases, tc{
		desc: "ignore changes without backend config",
		old:  translator.DefaultHealthCheck(8080, annotations.ProtocolHTTP),
		new:  newHC,
	})

	i64 := func(i int64) *int64 { return &i }
	s := func(s string) *string { return &s }

	newHC = translator.DefaultHealthCheck(8080, annotations.ProtocolHTTP)
	newHC.CheckIntervalSec = 1000
	cases = append(cases, tc{
		desc:    "CheckIntervalSec",
		old:     translator.DefaultHealthCheck(8080, annotations.ProtocolHTTP),
		new:     newHC,
		c:       &backendconfigv1.HealthCheckConfig{CheckIntervalSec: i64(1000)},
		hasDiff: true,
	})

	newHC = translator.DefaultHealthCheck(8080, annotations.ProtocolHTTP)
	newHC.TimeoutSec = 1000
	cases = append(cases, tc{
		desc:    "TimeoutSec",
		old:     translator.DefaultHealthCheck(8080, annotations.ProtocolHTTP),
		new:     newHC,
		c:       &backendconfigv1.HealthCheckConfig{TimeoutSec: i64(1000)},
		hasDiff: true,
	})

	newHC = translator.DefaultHealthCheck(8080, annotations.ProtocolHTTP)
	newHC.HealthyThreshold = 1000
	cases = append(cases, tc{
		desc:    "HealthyThreshold",
		old:     translator.DefaultHealthCheck(8080, annotations.ProtocolHTTP),
		new:     newHC,
		c:       &backendconfigv1.HealthCheckConfig{HealthyThreshold: i64(1000)},
		hasDiff: true,
	})

	newHC = translator.DefaultHealthCheck(8080, annotations.ProtocolHTTP)
	newHC.UnhealthyThreshold = 1000
	cases = append(cases, tc{
		desc:    "UnhealthyThreshold",
		old:     translator.DefaultHealthCheck(8080, annotations.ProtocolHTTP),
		new:     newHC,
		c:       &backendconfigv1.HealthCheckConfig{UnhealthyThreshold: i64(1000)},
		hasDiff: true,
	})

	newHC = translator.DefaultHealthCheck(8080, annotations.ProtocolHTTP)
	newHC.RequestPath = "/foo"
	cases = append(cases, tc{
		desc:    "RequestPath",
		old:     translator.DefaultHealthCheck(8080, annotations.ProtocolHTTP),
		new:     newHC,
		c:       &backendconfigv1.HealthCheckConfig{RequestPath: s("/foo")},
		hasDiff: true,
	})

	newHC = translator.DefaultHealthCheck(8080, annotations.ProtocolHTTP)
	newHC.RequestPath = "/foo"
	cases = append(cases, tc{
		desc: "non-backendconfig field is not a diff",
		old:  translator.DefaultHealthCheck(8080, annotations.ProtocolHTTP),
		new:  newHC,
		c:    &backendconfigv1.HealthCheckConfig{},
	})

	newHC = translator.DefaultHealthCheck(8080, annotations.ProtocolHTTP)
	newHC.TimeoutSec = 1000
	newHC.RequestPath = "/foo"
	cases = append(cases, tc{
		desc:    "multiple changes",
		old:     translator.DefaultHealthCheck(8080, annotations.ProtocolHTTP),
		new:     newHC,
		c:       &backendconfigv1.HealthCheckConfig{TimeoutSec: i64(1000), RequestPath: s("/foo")},
		hasDiff: true,
	})

	newHC = translator.DefaultHealthCheck(500, annotations.ProtocolHTTP)
	newHC.Port = 500
	cases = append(cases, tc{
		desc:    "Backendconfig Port",
		old:     translator.DefaultHealthCheck(8080, annotations.ProtocolHTTP),
		new:     newHC,
		c:       &backendconfigv1.HealthCheckConfig{Port: i64(500)},
		hasDiff: true,
	})

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			diffs := calculateDiff(tc.old, tc.new, tc.c)
			t.Logf("\nold=%+v\nnew=%+v\ndiffs = %s", tc.old, tc.new, diffs)
			if diffs.hasDiff() != tc.hasDiff {
				t.Errorf("calculateDiff(%+v, %+v, %+v) = %s; hasDiff = %t, want %t", tc.old, tc.new, tc.c, diffs, diffs.hasDiff(), tc.hasDiff)
			}
		})
	}
}

const (
	regSelfLink = "https://www.googleapis.com/compute/v1/projects/test-project/global/healthChecks/k8s-be-80--uid1"
	negSelfLink = "https://www.googleapis.com/compute/beta/projects/test-project/global/healthChecks/k8s1-uid1---0-56ff9a48"
	ilbSelfLink = "https://www.googleapis.com/compute/beta/projects/test-project/regions/us-central1/healthChecks/k8s1-uid1---0-56ff9a48"
)

type syncSPFixture struct{}

func (*syncSPFixture) hc() *compute.HealthCheck {
	return &compute.HealthCheck{
		Name:               "k8s-be-80--uid1",
		CheckIntervalSec:   60,
		HealthyThreshold:   1,
		TimeoutSec:         60,
		Type:               "HTTP",
		UnhealthyThreshold: 10,
		SelfLink:           regSelfLink,
		HttpHealthCheck: &compute.HTTPHealthCheck{
			Port:        80,
			RequestPath: "/",
		},
	}
}

func (f *syncSPFixture) hcs() *compute.HealthCheck  { return f.toS(f.hc()) }
func (f *syncSPFixture) hc2() *compute.HealthCheck  { return f.to2(f.hc()) }
func (f *syncSPFixture) negs() *compute.HealthCheck { return f.toS(f.neg()) }
func (f *syncSPFixture) neg2() *compute.HealthCheck { return f.to2(f.neg()) }
func (f *syncSPFixture) ilbs() *compute.HealthCheck { return f.toS(f.ilb()) }
func (f *syncSPFixture) ilb2() *compute.HealthCheck { return f.to2(f.ilb()) }

func (f *syncSPFixture) toS(h *compute.HealthCheck) *compute.HealthCheck {
	h.Type = "HTTPS"
	c := (compute.HTTPSHealthCheck)(*h.HttpHealthCheck)
	h.HttpsHealthCheck = &c
	h.HttpHealthCheck = nil
	return h
}

func (f *syncSPFixture) to2(h *compute.HealthCheck) *compute.HealthCheck {
	h.Type = "HTTP2"
	c := (compute.HTTP2HealthCheck)(*h.HttpHealthCheck)
	h.Http2HealthCheck = &c
	h.HttpHealthCheck = nil
	return h
}

func (*syncSPFixture) neg() *compute.HealthCheck {
	return &compute.HealthCheck{
		Name:               "k8s1-uid1---0-56ff9a48",
		CheckIntervalSec:   15,
		HealthyThreshold:   1,
		TimeoutSec:         15,
		Type:               "HTTP",
		UnhealthyThreshold: 2,
		SelfLink:           negSelfLink,
		HttpHealthCheck: &compute.HTTPHealthCheck{
			PortSpecification: "USE_SERVING_PORT",
			RequestPath:       "/",
		},
	}
}

func (f *syncSPFixture) ilb() *compute.HealthCheck {
	h := f.neg()
	h.Region = "us-central1"
	h.SelfLink = ilbSelfLink
	return h
}

func (f *syncSPFixture) setupExistingHCFunc(hc *compute.HealthCheck) func(m *cloud.MockGCE) {
	return func(m *cloud.MockGCE) {
		key := meta.GlobalKey(hc.Name)
		m.HealthChecks().Insert(context.Background(), key, hc)
	}
}

func (f *syncSPFixture) setupExistingRegionalHCFunc(region string, hc *compute.HealthCheck) func(m *cloud.MockGCE) {
	return func(m *cloud.MockGCE) {
		key := meta.RegionalKey(hc.Name, region)
		m.RegionHealthChecks().Insert(context.Background(), key, hc)
	}
}

func setupMockUpdate(mock *cloud.MockGCE) {
	// Mock does not know how to autogenerate update() methods.
	mock.MockHealthChecks.UpdateHook = func(ctx context.Context, k *meta.Key, hc *compute.HealthCheck, m *cloud.MockHealthChecks) error {
		m.Objects[*k].Obj = hc
		return nil
	}
	mock.MockAlphaHealthChecks.UpdateHook = func(ctx context.Context, k *meta.Key, hc *computealpha.HealthCheck, m *cloud.MockAlphaHealthChecks) error {
		m.Objects[*k].Obj = hc
		return nil
	}
	mock.MockBetaHealthChecks.UpdateHook = func(ctx context.Context, k *meta.Key, hc *computebeta.HealthCheck, m *cloud.MockBetaHealthChecks) error {
		m.Objects[*k].Obj = hc
		return nil
	}
	mock.MockRegionHealthChecks.UpdateHook = func(ctx context.Context, k *meta.Key, hc *compute.HealthCheck, m *cloud.MockRegionHealthChecks) error {
		m.Objects[*k].Obj = hc
		return nil
	}
	mock.MockAlphaRegionHealthChecks.UpdateHook = func(ctx context.Context, k *meta.Key, hc *computealpha.HealthCheck, m *cloud.MockAlphaRegionHealthChecks) error {
		m.Objects[*k].Obj = hc
		return nil
	}
	mock.MockBetaRegionHealthChecks.UpdateHook = func(ctx context.Context, k *meta.Key, hc *computebeta.HealthCheck, m *cloud.MockBetaRegionHealthChecks) error {
		m.Objects[*k].Obj = hc
		return nil
	}
}

func TestSyncServicePort(t *testing.T) {
	// No parallel().
	oldEnable := flags.F.EnableBackendConfigHealthCheck
	flags.F.EnableBackendConfigHealthCheck = true
	defer func() { flags.F.EnableBackendConfigHealthCheck = oldEnable }()

	type tc struct {
		desc     string
		setup    func(*cloud.MockGCE)
		sp       *utils.ServicePort
		probe    *v1.Probe
		regional bool

		wantSelfLink  string
		wantErr       bool
		wantComputeHC *compute.HealthCheck
	}
	fixture := syncSPFixture{}

	var cases []*tc

	cases = append(cases, &tc{desc: "create http", sp: testSPs["HTTP-80-reg-nil"], wantComputeHC: fixture.hc()})
	cases = append(cases, &tc{desc: "create https", sp: testSPs["HTTPS-80-reg-nil"], wantComputeHC: fixture.hcs()})
	cases = append(cases, &tc{desc: "create http2", sp: testSPs["HTTP2-80-reg-nil"], wantComputeHC: fixture.hc2()})
	cases = append(cases, &tc{desc: "create neg", sp: testSPs["HTTP-80-neg-nil"], wantComputeHC: fixture.neg()})
	cases = append(cases, &tc{desc: "create ilb", sp: testSPs["HTTP-80-ilb-nil"], regional: true, wantComputeHC: fixture.ilb()})

	// Probe override
	chc := fixture.hc()
	chc.HttpHealthCheck.RequestPath = "/foo"
	chc.HttpHealthCheck.Host = "foo.com"
	chc.CheckIntervalSec = 61
	chc.TimeoutSec = 1234
	cases = append(cases, &tc{
		desc: "create probe",
		sp:   testSPs["HTTP-80-reg-nil"],
		probe: &v1.Probe{
			Handler: v1.Handler{
				HTTPGet: &v1.HTTPGetAction{Path: "/foo", Host: "foo.com"},
			},
			PeriodSeconds:  1,
			TimeoutSeconds: 1234,
		},
		wantComputeHC: chc,
	})

	// Probe override (NEG)
	chc = fixture.neg()
	chc.HttpHealthCheck.RequestPath = "/foo"
	chc.HttpHealthCheck.Host = "foo.com"
	chc.CheckIntervalSec = 1234
	chc.TimeoutSec = 5678
	cases = append(cases, &tc{
		desc: "create probe neg",
		sp:   testSPs["HTTP-80-neg-nil"],
		probe: &v1.Probe{
			Handler: v1.Handler{
				HTTPGet: &v1.HTTPGetAction{Path: "/foo", Host: "foo.com"},
			},
			PeriodSeconds:  1234,
			TimeoutSeconds: 5678,
		},
		wantComputeHC: chc,
	})

	// Probe override (ILB)
	chc = fixture.ilb()
	chc.HttpHealthCheck.RequestPath = "/foo"
	chc.HttpHealthCheck.Host = "foo.com"
	chc.CheckIntervalSec = 1234
	chc.TimeoutSec = 5678
	cases = append(cases, &tc{
		desc:     "create probe ilb",
		sp:       testSPs["HTTP-80-ilb-nil"],
		regional: true,
		probe: &v1.Probe{
			Handler: v1.Handler{
				HTTPGet: &v1.HTTPGetAction{Path: "/foo", Host: "foo.com"},
			},
			PeriodSeconds:  1234,
			TimeoutSeconds: 5678,
		},
		wantComputeHC: chc,
	})

	// BackendConfig
	chc = fixture.hc()
	chc.HttpHealthCheck.RequestPath = "/foo"
	cases = append(cases, &tc{desc: "create backendconfig", sp: testSPs["HTTP-80-reg-bc"], wantComputeHC: chc})

	// BackendConfig all
	chc = fixture.hc()
	chc.HttpHealthCheck.RequestPath = "/foo"
	chc.CheckIntervalSec = 1234
	chc.HealthyThreshold = 1234
	chc.UnhealthyThreshold = 1234
	chc.TimeoutSec = 1234
	chc.HttpHealthCheck.Port = 1234
	// PortSpecification is set by the controller
	chc.HttpHealthCheck.PortSpecification = "USE_FIXED_PORT"
	cases = append(cases, &tc{desc: "create backendconfig all", sp: testSPs["HTTP-80-reg-bcall"], wantComputeHC: chc})

	i64 := func(i int64) *int64 { return &i }

	// BackendConfig port
	chc = fixture.hc()
	chc.HttpHealthCheck.Port = 1234
	// PortSpecification is set by the controller
	chc.HttpHealthCheck.PortSpecification = "USE_FIXED_PORT"
	sp := utils.ServicePort{
		NodePort:      80,
		Protocol:      annotations.ProtocolHTTP,
		BackendNamer:  testNamer,
		BackendConfig: &backendconfigv1.BackendConfig{Spec: backendconfigv1.BackendConfigSpec{HealthCheck: &backendconfigv1.HealthCheckConfig{Port: i64(1234)}}},
	}
	cases = append(cases, &tc{desc: "create backendconfig port", sp: &sp, wantComputeHC: chc})

	// BackendConfig neg
	chc = fixture.neg()
	chc.HttpHealthCheck.RequestPath = "/foo"
	cases = append(cases, &tc{
		desc:          "create backendconfig neg",
		sp:            testSPs["HTTP-80-neg-bc"],
		wantComputeHC: chc,
	})

	// BackendConfig ilb
	chc = fixture.ilb()
	chc.HttpHealthCheck.RequestPath = "/foo"
	cases = append(cases, &tc{
		desc:          "create backendconfig ilb",
		sp:            testSPs["HTTP-80-ilb-bc"],
		regional:      true,
		wantComputeHC: chc,
	})

	// Probe and BackendConfig override
	chc = fixture.hc()
	chc.HttpHealthCheck.RequestPath = "/foo"
	chc.HttpHealthCheck.Host = "foo.com"
	chc.CheckIntervalSec = 61
	chc.TimeoutSec = 1234
	cases = append(cases, &tc{
		desc: "create probe and backendconfig",
		sp:   testSPs["HTTP-80-reg-bc"],
		probe: &v1.Probe{
			Handler: v1.Handler{
				HTTPGet: &v1.HTTPGetAction{Path: "/bar", Host: "foo.com"},
			},
			PeriodSeconds:  1,
			TimeoutSeconds: 1234,
		},
		wantComputeHC: chc,
	})

	// Update tests
	cases = append(cases, &tc{
		desc:          "update http no change",
		setup:         fixture.setupExistingHCFunc(fixture.hc()),
		sp:            testSPs["HTTP-80-reg-nil"],
		wantComputeHC: fixture.hc(),
	})
	cases = append(cases, &tc{
		desc:          "update https no change",
		setup:         fixture.setupExistingHCFunc(fixture.hcs()),
		sp:            testSPs["HTTPS-80-reg-nil"],
		wantComputeHC: fixture.hcs(),
	})
	cases = append(cases, &tc{
		desc:          "update http2 no change",
		setup:         fixture.setupExistingHCFunc(fixture.hc2()),
		sp:            testSPs["HTTP2-80-reg-nil"],
		wantComputeHC: fixture.hc2(),
	})
	cases = append(cases, &tc{
		desc:          "update neg no change",
		setup:         fixture.setupExistingHCFunc(fixture.neg()),
		sp:            testSPs["HTTP-80-neg-nil"],
		wantComputeHC: fixture.neg(),
	})
	cases = append(cases, &tc{
		desc:          "update ilb no change",
		setup:         fixture.setupExistingHCFunc(fixture.ilb()),
		sp:            testSPs["HTTP-80-ilb-nil"],
		regional:      true,
		wantComputeHC: fixture.ilb(),
	})

	// Update protocol
	cases = append(cases, &tc{
		desc:          "update http to https",
		setup:         fixture.setupExistingHCFunc(fixture.hc()),
		sp:            testSPs["HTTPS-80-reg-nil"],
		wantComputeHC: fixture.hcs(),
	})
	cases = append(cases, &tc{
		desc:          "update http to http2",
		setup:         fixture.setupExistingHCFunc(fixture.hc()),
		sp:            testSPs["HTTP2-80-reg-nil"],
		wantComputeHC: fixture.hc2(),
	})
	cases = append(cases, &tc{
		desc:          "update https to http",
		setup:         fixture.setupExistingHCFunc(fixture.hcs()),
		sp:            testSPs["HTTP-80-reg-nil"],
		wantComputeHC: fixture.hc(),
	})
	cases = append(cases, &tc{
		desc:          "update https to http2",
		setup:         fixture.setupExistingHCFunc(fixture.hcs()),
		sp:            testSPs["HTTP2-80-reg-nil"],
		wantComputeHC: fixture.hc2(),
	})
	cases = append(cases, &tc{
		desc:          "update neg protocol",
		setup:         fixture.setupExistingHCFunc(fixture.neg()),
		sp:            testSPs["HTTPS-80-neg-nil"],
		wantComputeHC: fixture.negs(),
	})
	cases = append(cases, &tc{
		desc:          "update ilb protocol",
		setup:         fixture.setupExistingRegionalHCFunc("us-central1", fixture.ilb()),
		sp:            testSPs["HTTPS-80-ilb-nil"],
		regional:      true,
		wantComputeHC: fixture.ilbs(),
	})

	// Preserve user settings.
	chc = fixture.hc()
	chc.HttpHealthCheck.RequestPath = "/user-path"
	chc.CheckIntervalSec = 1234
	cases = append(cases, &tc{
		desc:          "update preserve",
		setup:         fixture.setupExistingHCFunc(chc),
		sp:            testSPs["HTTP-80-reg-nil"],
		wantComputeHC: chc,
	})

	// Preserve user settings while changing the protocol
	chc = fixture.hc()
	chc.HttpHealthCheck.RequestPath = "/user-path"
	chc.CheckIntervalSec = 1234
	wantCHC := fixture.hcs()
	wantCHC.HttpsHealthCheck.RequestPath = "/user-path"
	wantCHC.CheckIntervalSec = 1234
	cases = append(cases, &tc{
		desc:          "update preserve across protocol change",
		setup:         fixture.setupExistingHCFunc(chc),
		sp:            testSPs["HTTPS-80-reg-nil"],
		wantComputeHC: wantCHC,
	})

	// Preserve user settings while changing the protocol (neg)
	chc = fixture.neg()
	chc.HttpHealthCheck.RequestPath = "/user-path"
	chc.CheckIntervalSec = 1234
	wantCHC = fixture.negs()
	wantCHC.HttpsHealthCheck.RequestPath = "/user-path"
	wantCHC.CheckIntervalSec = 1234
	cases = append(cases, &tc{
		desc:          "update preserve across protocol change neg",
		setup:         fixture.setupExistingHCFunc(chc),
		sp:            testSPs["HTTPS-80-neg-nil"],
		wantComputeHC: wantCHC,
	})

	// Preserve user settings while changing the protocol (ilb)
	chc = fixture.ilb()
	chc.HttpHealthCheck.RequestPath = "/user-path"
	chc.CheckIntervalSec = 1234
	wantCHC = fixture.ilbs()
	wantCHC.HttpsHealthCheck.RequestPath = "/user-path"
	wantCHC.CheckIntervalSec = 1234
	cases = append(cases, &tc{
		desc:          "update preserve across protocol change ilb",
		setup:         fixture.setupExistingRegionalHCFunc("us-central1", chc),
		sp:            testSPs["HTTPS-80-ilb-nil"],
		regional:      true,
		wantComputeHC: wantCHC,
	})

	// Preserve some settings, but override some in the backend config
	chc = fixture.hc()
	chc.HttpHealthCheck.RequestPath = "/user-path"
	chc.CheckIntervalSec = 1234
	wantCHC = fixture.hc()
	wantCHC.HttpHealthCheck.RequestPath = "/foo" // from bc
	wantCHC.CheckIntervalSec = 1234              // same
	cases = append(cases, &tc{
		desc:          "update preserve and backendconfig (path)",
		sp:            testSPs["HTTP-80-reg-bc"],
		setup:         fixture.setupExistingHCFunc(chc),
		wantComputeHC: wantCHC,
	})

	// Override all settings from backendconfig.
	chc = fixture.hc()
	chc.HttpHealthCheck.RequestPath = "/user-path"
	chc.CheckIntervalSec = 1234
	wantCHC = fixture.hc()
	wantCHC.HttpHealthCheck.RequestPath = "/foo" // from bc
	wantCHC.CheckIntervalSec = 1234
	wantCHC.HealthyThreshold = 1234
	wantCHC.UnhealthyThreshold = 1234
	wantCHC.TimeoutSec = 1234
	wantCHC.HttpHealthCheck.Port = 1234
	wantCHC.HttpHealthCheck.PortSpecification = "USE_FIXED_PORT"
	cases = append(cases, &tc{
		desc:          "update preserve backendconfig all",
		setup:         fixture.setupExistingHCFunc(chc),
		sp:            testSPs["HTTP-80-reg-bcall"],
		wantComputeHC: wantCHC,
	})

	// BUG: changing probe settings has not effect on the healthcheck
	// update.
	// TODO(bowei): document this.
	chc = fixture.hc()
	chc.HttpHealthCheck.RequestPath = "/user-path"
	cases = append(cases, &tc{
		desc:  "update probe has no effect (bug)",
		setup: fixture.setupExistingHCFunc(chc),
		sp:    testSPs["HTTP-80-reg-nil"],
		probe: &v1.Probe{
			Handler: v1.Handler{
				HTTPGet: &v1.HTTPGetAction{Path: "/foo", Host: "foo.com"},
			},
			PeriodSeconds:  1,
			TimeoutSeconds: 1234,
		},
		wantComputeHC: chc,
	})

	// BUG: Enable NEG, leaks old healthcheck, does not preserve old
	// settings.

	// BUG: Switch to/from ILB, leaks old healthcheck, does not preserve old
	// settings.

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())

			mock := fakeGCE.Compute().(*cloud.MockGCE)
			setupMockUpdate(mock)

			if tc.setup != nil {
				tc.setup(mock)
			}

			hcs := NewHealthChecker(fakeGCE, "/", defaultBackendSvc)

			gotSelfLink, err := hcs.SyncServicePort(tc.sp, tc.probe)
			if gotErr := err != nil; gotErr != tc.wantErr {
				t.Errorf("hcs.SyncServicePort(tc.sp, tc.probe) = _, %v; gotErr = %t, want %t\nsp = %s\nprobe = %s", err, gotErr, tc.wantErr, pretty.Sprint(tc.sp), pretty.Sprint(tc.probe))
			}
			if tc.wantSelfLink != "" && gotSelfLink != tc.wantSelfLink {
				t.Errorf("hcs.SyncServicePort(tc.sp, tc.probe) = %q, _; want = %q", gotSelfLink, tc.wantSelfLink)
			}

			verify := func() {
				t.Helper()
				var computeHCs []*compute.HealthCheck
				if tc.regional {
					computeHCs, _ = fakeGCE.Compute().RegionHealthChecks().List(context.Background(), fakeGCE.Region(), nil)
				} else {
					computeHCs, _ = fakeGCE.Compute().HealthChecks().List(context.Background(), nil)
				}
				if len(computeHCs) != 1 {
					t.Fatalf("Got %d healthchecks, want 1\n%s", len(computeHCs), pretty.Sprint(computeHCs))
				}

				gotHC := computeHCs[0]
				// Filter out fields that are hard to deal with in the mock and
				// test cases.
				filter := func(hc *compute.HealthCheck) {
					hc.Description = ""
					hc.SelfLink = ""
				}
				filter(gotHC)
				filter(tc.wantComputeHC)

				if !reflect.DeepEqual(gotHC, tc.wantComputeHC) {
					t.Fatalf("Compute healthcheck is:\n%s, want:\n%s", pretty.Sprint(gotHC), pretty.Sprint(tc.wantComputeHC))
				}
			}

			verify()

			// Check that resync should not have an effect and does not issue
			// an update to GCE. Hook Update() to fail.
			mock.MockHealthChecks.UpdateHook = func(context.Context, *meta.Key, *compute.HealthCheck, *cloud.MockHealthChecks) error {
				t.Fatalf("resync should not result in an update")
				return nil
			}

			gotSelfLink, err = hcs.SyncServicePort(tc.sp, tc.probe)
			if gotErr := err != nil; gotErr != tc.wantErr {
				t.Errorf("hcs.SyncServicePort(tc.sp, tc.probe) = %v; gotErr = %t, want %t\nsp = %s\nprobe = %s", err, gotErr, tc.wantErr, pretty.Sprint(tc.sp), pretty.Sprint(tc.probe))
			}
			if tc.wantSelfLink != "" && gotSelfLink != tc.wantSelfLink {
				t.Errorf("hcs.SyncServicePort(tc.sp, tc.probe) = %q, _; want = %q", gotSelfLink, tc.wantSelfLink)
			}
			verify()
		})
	}
}

// TODO(bowei): test regional delete
// TODO(bowei): test errors from GCE
