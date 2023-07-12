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
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

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
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/ingress-gce/pkg/annotations"
	backendconfigv1 "k8s.io/ingress-gce/pkg/apis/backendconfig/v1"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/loadbalancers/features"
	"k8s.io/ingress-gce/pkg/translator"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/healthcheck"
	namer_util "k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/klog/v2"
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
	// Example: sps["HTTP-8000-neg-nil-nothc"] is a ServicePort for HTTP with NEG-enabled.
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
					for thck, thc := range map[string]bool{"thc": true, "nothc": false} {
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
						if thc {
							sp.THCConfiguration.THCOptInOnSvc = true
							if mode == "reg" { // No THC without NEG.
								continue
							}
						}
						testSPs[fmt.Sprintf("%s-%s-%s-%s-%s", p, npk, mode, bck, thck)] = sp
					}
				}
			}
		}
	}
}

func TestHealthCheckAdd(t *testing.T) {
	fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
	healthChecks := NewHealthChecker(fakeGCE, "/", defaultBackendSvc, NewFakeRecorderGetter(0), NewFakeServiceGetter(), HealthcheckFlags{})

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

	sp = &utils.ServicePort{NodePort: 8080, Protocol: annotations.ProtocolHTTP, NEGEnabled: false, BackendNamer: testNamer, THCConfiguration: utils.THCConfiguration{THCOptInOnSvc: true}}
	_, err = healthChecks.SyncServicePort(sp, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Verify the health check exists
	_, err = fakeGCE.GetHealthCheck(testNamer.IGBackend(8080))
	if err != nil {
		t.Fatalf("expected the health check to exist, err: %v", err)
	}
}

func TestHealthCheckAddExisting(t *testing.T) {
	fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
	healthChecks := NewHealthChecker(fakeGCE, "/", defaultBackendSvc, NewFakeRecorderGetter(0), NewFakeServiceGetter(), HealthcheckFlags{})

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

	// Enable Transparent Health Checks
	sp = &utils.ServicePort{NodePort: 3000, Protocol: annotations.ProtocolHTTP, NEGEnabled: false, BackendNamer: testNamer, THCConfiguration: utils.THCConfiguration{THCOptInOnSvc: true}}
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
	healthChecks := NewHealthChecker(fakeGCE, "/", defaultBackendSvc, NewFakeRecorderGetter(0), NewFakeServiceGetter(), HealthcheckFlags{})

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
	healthChecks := NewHealthChecker(fakeGCE, "/", defaultBackendSvc, NewFakeRecorderGetter(0), NewFakeServiceGetter(), HealthcheckFlags{})

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
	healthChecks := NewHealthChecker(fakeGCE, "/", defaultBackendSvc, NewFakeRecorderGetter(0), NewFakeServiceGetter(), HealthcheckFlags{})

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

	healthChecks := NewHealthChecker(fakeGCE, "/", defaultBackendSvc, NewFakeRecorderGetter(0), NewFakeServiceGetter(), HealthcheckFlags{})

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
	_, err = healthChecks.sync(hc, nil, utils.THCConfiguration{})
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
	_, err = healthChecks.sync(hc, nil, utils.THCConfiguration{})
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
	_, err = healthChecks.sync(hc, nil, utils.THCConfiguration{})

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

	_, err = healthChecks.sync(hc, nil, utils.THCConfiguration{})
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

func TestEnableTHC(t *testing.T) {
	fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())

	const thcPort int64 = 5678

	// Add Hooks
	(fakeGCE.Compute().(*cloud.MockGCE)).MockHealthChecks.UpdateHook = mock.UpdateHealthCheckHook
	(fakeGCE.Compute().(*cloud.MockGCE)).MockAlphaHealthChecks.UpdateHook = mock.UpdateAlphaHealthCheckHook
	(fakeGCE.Compute().(*cloud.MockGCE)).MockBetaHealthChecks.UpdateHook = mock.UpdateBetaHealthCheckHook

	healthChecks := NewHealthChecker(fakeGCE, "/", defaultBackendSvc, NewFakeRecorderGetter(0), NewFakeServiceGetter(), HealthcheckFlags{EnableTHC: true, THCPort: thcPort})

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
	initialObtainedHC, err := healthChecks.Get(hc.Name, meta.VersionGA, meta.Global)
	if err != nil {
		t.Fatalf("expected the health check to exist, err: %v", err)
	}

	wantHC := *initialObtainedHC // shallow copy
	wantHC.CheckIntervalSec = 5
	wantHC.TimeoutSec = 5
	wantHC.UnhealthyThreshold = 10
	wantHC.HealthyThreshold = 1
	wantHC.Type = "HTTP"
	wantHC.Description = (&healthcheck.HealthcheckInfo{HealthcheckConfig: healthcheck.TransparentHC}).GenerateHealthcheckDescription()
	wantHC.RequestPath = "/api/podhealth"
	wantHC.Port = thcPort
	wantHC.PortSpecification = "USE_FIXED_PORT"

	oldName := hc.Name
	hc = &translator.HealthCheck{}
	translator.OverwriteWithTHC(hc, thcPort)
	hc.Name = oldName
	// Enable Transparent Health Checks
	_, err = healthChecks.sync(hc, nil, utils.THCConfiguration{THCOptInOnSvc: true})
	if err != nil {
		t.Fatalf("unexpected err while syncing healthcheck, err %v", err)
	}

	// Verify the health check exists
	obtainedHC, err := healthChecks.Get(hc.Name, meta.VersionGA, meta.Global)
	if err != nil {
		t.Fatalf("expected the health check to exist, err: %v", err)
	}

	// Verify the parameters.
	if !reflect.DeepEqual(obtainedHC, &wantHC) {
		t.Fatalf("Translate healthcheck is:\n%s, want:\n%s", pretty.Sprint(obtainedHC), pretty.Sprint(wantHC))
	}
}

func getSingletonHealthcheck(t *testing.T, c *gce.Cloud) *compute.HealthCheck {
	t.Helper()
	// Verify the health check exists
	var computeHCs []*compute.HealthCheck
	computeHCs, _ = c.Compute().HealthChecks().List(context.Background(), nil)
	if len(computeHCs) != 1 {
		t.Fatalf("Got %d healthchecks, want 1\n%s", len(computeHCs), pretty.Sprint(computeHCs))
	}
	return utils.DeepCopyComputeHealthCheck(computeHCs[0]) // Make a copy to avoid reading an overwritten version later.
}

func TestEmitTHCEvents(t *testing.T) {
	t.Parallel()

	testClusterValues := gce.DefaultTestClusterValues()
	fakeGCE := gce.NewFakeGCECloud(testClusterValues)

	fakeSingletonRecorderGetter := NewFakeSingletonRecorderGetter(10)
	healthChecks := NewHealthChecker(fakeGCE, "/", defaultBackendSvc, fakeSingletonRecorderGetter, NewFakeServiceGetter(), HealthcheckFlags{})

	hc := translator.DefaultHealthCheck(3000, annotations.ProtocolHTTP)
	hc.Service = &v1.Service{}

	type tc = struct {
		wantTexts []string
		events    utils.THCEvents
	}

	testCases := []tc{
		{
			wantTexts: []string{"Normal THCConfigured Transparent Health Check successfully configured."},
			events:    utils.THCEvents{THCConfigured: true},
		},
		{
			wantTexts: []string{"Warning BackendConfigOverridesTHC Both THC and BackendConfig annotations present and the BackendConfig has spec.healthCheck. The THC annotation will be ignored."},
			events:    utils.THCEvents{BackendConfigOverridesTHC: true},
		},
		{
			wantTexts: []string{"Warning THCAnnotationWithoutFlag THC annotation present, but the Transparent Health Checks feature is not enabled."},
			events:    utils.THCEvents{THCAnnotationWithoutFlag: true},
		},
		{
			wantTexts: []string{"Warning THCAnnotationWithoutFlag THC annotation present, but the Transparent Health Checks feature is not enabled."},
			events:    utils.THCEvents{THCAnnotationWithoutFlag: true},
		},
		{
			wantTexts: []string{"Warning THCAnnotationWithoutNEG THC annotation present, but NEG is disabled. Will not enable Transparent Health Checks."},
			events:    utils.THCEvents{THCAnnotationWithoutNEG: true},
		},
	}

	fakeRecorder := fakeSingletonRecorderGetter.FakeRecorder()
	for _, tc := range testCases {
		healthChecks.emitTHCEvents(hc, tc.events)
		for _, wantText := range tc.wantTexts {
			select {
			case output := <-fakeRecorder.Events:
				if output != wantText {
					t.Errorf("Incorrect event emitted on healthcheck update: %s.", output)
				}
			case <-time.After(10 * time.Second):
				t.Fatalf("Timeout when expecting Event.")
			}
		}
		select {
		case output := <-fakeRecorder.Events:
			t.Fatalf("Unexpected event: %s", output)
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// Test changing the value of the flag EnableUpdateCustomHealthCheckDescription from false to true.
func TestRolloutUpdateCustomHCDescription(t *testing.T) {
	// No parallel() because we modify the value of the flags:
	// - EnableUpdateCustomHealthCheckDescription,
	// - GKEClusterName.

	testClusterValues := gce.DefaultTestClusterValues()

	oldUpdateDescription := flags.F.EnableUpdateCustomHealthCheckDescription
	oldGKEClusterName := flags.F.GKEClusterName
	// Start with EnableUpdateCustomHealthCheckDescription = false.
	flags.F.EnableUpdateCustomHealthCheckDescription = false
	flags.F.GKEClusterName = testClusterValues.ClusterName
	defer func() {
		flags.F.EnableUpdateCustomHealthCheckDescription = oldUpdateDescription
		flags.F.GKEClusterName = oldGKEClusterName
	}()

	testClusterValues.Regional = true
	fakeGCE := gce.NewFakeGCECloud(testClusterValues)

	var (
		defaultSP       *utils.ServicePort = testSPs["HTTP-80-reg-nil-nothc"]
		backendConfigSP *utils.ServicePort = testSPs["HTTP-80-reg-bc-nothc"]
	)

	// Add Hooks
	(fakeGCE.Compute().(*cloud.MockGCE)).MockHealthChecks.UpdateHook = mock.UpdateHealthCheckHook
	(fakeGCE.Compute().(*cloud.MockGCE)).MockAlphaHealthChecks.UpdateHook = mock.UpdateAlphaHealthCheckHook
	(fakeGCE.Compute().(*cloud.MockGCE)).MockBetaHealthChecks.UpdateHook = mock.UpdateBetaHealthCheckHook

	fakeSingletonRecorderGetter := NewFakeSingletonRecorderGetter(1)
	healthChecks := NewHealthChecker(fakeGCE, "/", defaultBackendSvc, fakeSingletonRecorderGetter, NewFakeServiceGetter(), HealthcheckFlags{})

	_, err := healthChecks.SyncServicePort(defaultSP, nil)
	if err != nil {
		t.Fatalf("unexpected err while syncing healthcheck, err %v", err)
	}

	outputDefaultHC := getSingletonHealthcheck(t, fakeGCE)

	if outputDefaultHC.Description != translator.DescriptionForDefaultHealthChecks {
		t.Fatalf("incorrect Description, is: \"%v\", want: \"%v\"",
			outputDefaultHC.Description, translator.DescriptionForDefaultHealthChecks)
	}

	_, err = healthChecks.SyncServicePort(backendConfigSP, nil)
	if err != nil {
		t.Fatalf("unexpected err while syncing healthcheck, err %v", err)
	}

	outputBCHC := getSingletonHealthcheck(t, fakeGCE)

	// Verify that BackendConfig overwrites only the specified values.
	// In particular, with the flag set to false, the Description does not get overwritten.
	outputDefaultHC.HttpHealthCheck.RequestPath = *backendConfigSP.BackendConfig.Spec.HealthCheck.RequestPath
	if !reflect.DeepEqual(outputBCHC, outputDefaultHC) {
		t.Fatalf("Compute healthcheck is:\n%s, want:\n%s", pretty.Sprint(outputBCHC), pretty.Sprint(outputDefaultHC))
	}

	// Modify the flag and see what happens.
	flags.F.EnableUpdateCustomHealthCheckDescription = true

	_, err = healthChecks.SyncServicePort(backendConfigSP, nil)
	if err != nil {
		t.Fatalf("unexpected err while syncing healthcheck, err %v", err)
	}

	outputBCHCWithFlag := getSingletonHealthcheck(t, fakeGCE)

	wantDesc := healthcheck.HealthcheckDesc{
		K8sCluster:  fmt.Sprintf("/locations/%s/clusters/%s", gce.DefaultTestClusterValues().Region, gce.DefaultTestClusterValues().ClusterName),
		K8sResource: fmt.Sprintf("/namespaces/%s/services/%s", defaultBackendSvc.Namespace, defaultBackendSvc.Name),
		Config:      "BackendConfig",
	}
	bytes, err := json.MarshalIndent(wantDesc, "", "    ")
	if err != nil {
		t.Fatalf("Error while generating the wantDesc JSON.")
	}
	wantDescription := string(bytes)

	if outputBCHCWithFlag.Description != wantDescription {
		t.Fatalf("incorrect Description, is: \"%v\", want: \"%v\"",
			outputBCHCWithFlag.Description, wantDescription)
	}

	// Verify that only the Description is modified after rollout.
	filter := func(hc *compute.HealthCheck) {
		hc.Description = ""
	}
	filter(outputBCHC)
	filter(outputBCHCWithFlag)
	if !reflect.DeepEqual(outputBCHC, outputBCHCWithFlag) {
		t.Fatalf("Compute healthcheck is:\n%s, want:\n%s", pretty.Sprint(outputBCHC), pretty.Sprint(outputBCHCWithFlag))
	}

	fakeRecorder := fakeSingletonRecorderGetter.FakeRecorder()
	select {
	case output := <-fakeRecorder.Events:
		if !strings.HasPrefix(output, "Normal HealthcheckDescriptionUpdate") {
			t.Fatalf("Incorrect event emitted on healthcheck update: %s.", output)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("Timeout when expecting Event.")
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
				ProbeHandler: api_v1.ProbeHandler{
					HTTPGet: &api_v1.HTTPGetAction{Path: "/override"},
				},
			},
			want: ws{path: "/override", timeoutSec: 1234, checkIntervalSec: 60, port: 8080},
		},
		{
			desc: "override host",
			probe: &api_v1.Probe{
				TimeoutSeconds: 1234,
				ProbeHandler: api_v1.ProbeHandler{
					HTTPGet: &api_v1.HTTPGetAction{Host: "foo.com"},
				},
			},
			want: ws{host: "foo.com", path: "/", timeoutSec: 1234, checkIntervalSec: 60, port: 8080},
		},
		{
			desc: "override host (via header)",
			probe: &api_v1.Probe{
				TimeoutSeconds: 1234,
				ProbeHandler: api_v1.ProbeHandler{
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
				ProbeHandler: api_v1.ProbeHandler{
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
				ProbeHandler: api_v1.ProbeHandler{
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
				ProbeHandler: api_v1.ProbeHandler{
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
		diffSize *int64
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
	newHC.Description = translator.DescriptionForHealthChecksFromBackendConfig
	cases = append(cases, tc{
		desc:     "hc with empty backendconfig and appropriate Description is a diff",
		old:      translator.DefaultHealthCheck(8080, annotations.ProtocolHTTP),
		new:      newHC,
		c:        &backendconfigv1.HealthCheckConfig{},
		hasDiff:  true,
		diffSize: i64(1),
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
			diffs := calculateDiff(tc.old, tc.new, tc.c, false)
			t.Logf("\nold=%+v\nnew=%+v\ndiffs = %s", tc.old, tc.new, diffs)
			if diffs.hasDiff() != tc.hasDiff {
				t.Errorf("calculateDiff(%+v, %+v, %+v) = %s; hasDiff = %t, want %t", tc.old, tc.new, tc.c, diffs, diffs.hasDiff(), tc.hasDiff)
			}
			if tc.diffSize != nil && diffs.size() != *tc.diffSize {
				t.Errorf("calculateDiff(%+v, %+v, %+v) = %s; size = %v, want %v", tc.old, tc.new, tc.c, diffs, diffs.size(), *tc.diffSize)
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
		Description: translator.DescriptionForDefaultHealthChecks,
	}
}

func (*syncSPFixture) thc() *compute.HealthCheck {
	return &compute.HealthCheck{
		Name:               "k8s1-uid1---0-56ff9a48",
		CheckIntervalSec:   5,
		HealthyThreshold:   1,
		TimeoutSec:         5,
		Type:               "HTTP",
		UnhealthyThreshold: 10,
		SelfLink:           negSelfLink,
		HttpHealthCheck: &compute.HTTPHealthCheck{
			PortSpecification: "USE_FIXED_PORT",
			Port:              7877,
			RequestPath:       "/api/podhealth",
		},
		Description: jsonDescription(healthcheck.TransparentHC, false),
	}
}

func (*syncSPFixture) bchc() *compute.HealthCheck {
	return &compute.HealthCheck{
		Name:               "k8s-be-80--uid1",
		CheckIntervalSec:   61,
		HealthyThreshold:   1,
		TimeoutSec:         1234,
		Type:               "HTTP",
		UnhealthyThreshold: 10,
		SelfLink:           regSelfLink,
		HttpHealthCheck: &compute.HTTPHealthCheck{
			Port:        80,
			RequestPath: "/foo",
			Host:        "foo.com",
		},
		Description: (&healthcheck.HealthcheckInfo{HealthcheckConfig: healthcheck.BackendConfigHC}).GenerateHealthcheckDescription(),
	}
}

func hcFromBC(bchcc *backendconfigv1.HealthCheckConfig, neg bool, json bool) *compute.HealthCheck {
	fixture := syncSPFixture{}
	hc := fixture.hc()
	if neg {
		hc = fixture.neg()
	}
	if bchcc == nil {
		return hc
	}
	if bchcc.CheckIntervalSec != nil {
		hc.CheckIntervalSec = *bchcc.CheckIntervalSec
	}
	if bchcc.HealthyThreshold != nil {
		hc.HealthyThreshold = *bchcc.HealthyThreshold
	}
	if bchcc.TimeoutSec != nil {
		hc.TimeoutSec = *bchcc.TimeoutSec
	}
	if bchcc.Type != nil {
		hc.Type = *bchcc.Type
	}
	if bchcc.UnhealthyThreshold != nil {
		hc.UnhealthyThreshold = *bchcc.UnhealthyThreshold
	}
	if bchcc.Port != nil {
		hc.HttpHealthCheck.Port = *bchcc.Port
		hc.HttpHealthCheck.PortSpecification = "USE_FIXED_PORT"
	}
	if bchcc.RequestPath != nil {
		hc.HttpHealthCheck.RequestPath = *bchcc.RequestPath
	}
	if json {
		hc.Description = jsonDescription(healthcheck.BackendConfigHC, false)
	}
	return hc
}

func (f *syncSPFixture) hcs() *compute.HealthCheck  { return f.toS(f.hc()) }
func (f *syncSPFixture) hc2() *compute.HealthCheck  { return f.to2(f.hc()) }
func (f *syncSPFixture) negs() *compute.HealthCheck { return f.toS(f.neg()) }
func (f *syncSPFixture) neg2() *compute.HealthCheck { return f.to2(f.neg()) }
func (f *syncSPFixture) ilbs() *compute.HealthCheck { return f.toS(f.ilb()) }
func (f *syncSPFixture) ilb2() *compute.HealthCheck { return f.to2(f.ilb()) }
func (f *syncSPFixture) thcs() *compute.HealthCheck { panic("no such thing exists") }
func (f *syncSPFixture) thc2() *compute.HealthCheck { panic("no such thing exists") }

func (f *syncSPFixture) toS(h *compute.HealthCheck) *compute.HealthCheck {
	h.Type = "HTTPS"
	c := (compute.HTTPSHealthCheck)(*h.HttpHealthCheck)
	h.HttpsHealthCheck = &c
	h.HttpHealthCheck = nil
	return h
}

func toIlb(h *compute.HealthCheck) {
	h.Region = "us-central1"
	h.SelfLink = ilbSelfLink
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
		Description: translator.DescriptionForDefaultNEGHealthChecks,
	}
}

func (f *syncSPFixture) ilb() *compute.HealthCheck {
	h := f.neg()
	h.Region = "us-central1"
	h.SelfLink = ilbSelfLink
	h.Description = translator.DescriptionForDefaultILBHealthChecks
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
	// No parallel() because we modify the value of the flags:
	// - EnableUpdateCustomHealthCheckDescription,
	// - GKEClusterName.
	oldUpdateDescription := flags.F.EnableUpdateCustomHealthCheckDescription
	oldGKEClusterName := flags.F.GKEClusterName
	flags.F.GKEClusterName = gce.DefaultTestClusterValues().ClusterName
	defer func() {
		flags.F.EnableUpdateCustomHealthCheckDescription = oldUpdateDescription
		flags.F.GKEClusterName = oldGKEClusterName
	}()

	type tc struct {
		desc            string
		setup           func(*cloud.MockGCE)
		sp              *utils.ServicePort
		probe           *v1.Probe
		regional        bool
		regionalCluster bool

		wantSelfLink        string
		wantErr             bool
		wantComputeHC       *compute.HealthCheck
		updateHCDescription bool
		enableTHC           bool
		recalc              bool
	}
	fixture := syncSPFixture{}

	var cases []*tc

	cases = append(cases, &tc{desc: "create http", sp: testSPs["HTTP-80-reg-nil-nothc"], wantComputeHC: fixture.hc()})
	cases = append(cases, &tc{desc: "create https", sp: testSPs["HTTPS-80-reg-nil-nothc"], wantComputeHC: fixture.hcs()})
	cases = append(cases, &tc{desc: "create http2", sp: testSPs["HTTP2-80-reg-nil-nothc"], wantComputeHC: fixture.hc2()})
	cases = append(cases, &tc{desc: "create neg", sp: testSPs["HTTP-80-neg-nil-nothc"], wantComputeHC: fixture.neg()})
	cases = append(cases, &tc{desc: "create ilb", sp: testSPs["HTTP-80-ilb-nil-nothc"], regional: true, wantComputeHC: fixture.ilb()})

	// Probe override
	chc := fixture.hc()
	chc.HttpHealthCheck.RequestPath = "/foo"
	chc.HttpHealthCheck.Host = "foo.com"
	chc.CheckIntervalSec = 61
	chc.TimeoutSec = 1234
	chc.Description = translator.DescriptionForHealthChecksFromReadinessProbe
	cases = append(cases, &tc{
		desc: "create probe",
		sp:   testSPs["HTTP-80-reg-nil-nothc"],
		probe: &v1.Probe{
			ProbeHandler: api_v1.ProbeHandler{
				HTTPGet: &v1.HTTPGetAction{Path: "/foo", Host: "foo.com"},
			},
			PeriodSeconds:  1,
			TimeoutSeconds: 1234,
		},
		wantComputeHC: chc,
	})

	// Transparent Health Check (NEG)
	chc = fixture.thc()
	cases = append(cases, &tc{
		desc:          "create thc neg",
		sp:            testSPs["HTTP-80-neg-nil-thc"],
		enableTHC:     true,
		wantComputeHC: chc,
	})

	// Transparent Health Check (ILB)
	chc = fixture.thc()
	toIlb(chc)
	cases = append(cases, &tc{
		desc:          "create thc ilb",
		sp:            testSPs["HTTP-80-ilb-nil-thc"],
		enableTHC:     true,
		regional:      true,
		wantComputeHC: chc,
	})

	// Transparent Health Check (ILB), regional cluster
	chc = fixture.thc()
	toIlb(chc)
	cases = append(cases, &tc{
		desc:            "create thc ilb regional cluster",
		sp:              testSPs["HTTP-80-ilb-nil-thc"],
		enableTHC:       true,
		regional:        true,
		regionalCluster: true,
		wantComputeHC:   chc,
	})

	// Probe ignored with THC
	chc = fixture.thc()
	cases = append(cases, &tc{
		desc: "create thc prob",
		sp:   testSPs["HTTP-80-neg-nil-thc"],
		probe: &v1.Probe{
			ProbeHandler: api_v1.ProbeHandler{
				HTTPGet: &v1.HTTPGetAction{Path: "/foo", Host: "foo.com"},
			},
			PeriodSeconds:  1234,
			TimeoutSeconds: 5678,
		},
		enableTHC:     true,
		wantComputeHC: chc,
	})

	// Probe override (NEG)
	chc = fixture.neg()
	chc.HttpHealthCheck.RequestPath = "/foo"
	chc.HttpHealthCheck.Host = "foo.com"
	chc.CheckIntervalSec = 1234
	chc.TimeoutSec = 5678
	chc.Description = translator.DescriptionForHealthChecksFromReadinessProbe
	cases = append(cases, &tc{
		desc: "create probe neg",
		sp:   testSPs["HTTP-80-neg-nil-nothc"],
		probe: &v1.Probe{
			ProbeHandler: api_v1.ProbeHandler{
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
	chc.Description = translator.DescriptionForHealthChecksFromReadinessProbe
	cases = append(cases, &tc{
		desc:     "create probe ilb",
		sp:       testSPs["HTTP-80-ilb-nil-nothc"],
		regional: true,
		probe: &v1.Probe{
			ProbeHandler: api_v1.ProbeHandler{
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
	chc.Description = translator.DescriptionForHealthChecksFromBackendConfig
	cases = append(cases, &tc{desc: "create backendconfig", sp: testSPs["HTTP-80-reg-bc-nothc"], wantComputeHC: chc})

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
	chc.Description = translator.DescriptionForHealthChecksFromBackendConfig
	cases = append(cases, &tc{desc: "create backendconfig all", sp: testSPs["HTTP-80-reg-bcall-nothc"], wantComputeHC: chc})

	i64 := func(i int64) *int64 { return &i }

	// BackendConfig port
	chc = fixture.hc()
	chc.HttpHealthCheck.Port = 1234
	// PortSpecification is set by the controller
	chc.HttpHealthCheck.PortSpecification = "USE_FIXED_PORT"
	chc.Description = translator.DescriptionForHealthChecksFromBackendConfig
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
	chc.Description = translator.DescriptionForHealthChecksFromBackendConfig
	cases = append(cases, &tc{
		desc:          "create backendconfig neg",
		sp:            testSPs["HTTP-80-neg-bc-nothc"],
		wantComputeHC: chc,
	})

	// BackendConfig ilb
	chc = fixture.ilb()
	chc.HttpHealthCheck.RequestPath = "/foo"
	chc.Description = translator.DescriptionForHealthChecksFromBackendConfig
	cases = append(cases, &tc{
		desc:          "create backendconfig ilb",
		sp:            testSPs["HTTP-80-ilb-bc-nothc"],
		regional:      true,
		wantComputeHC: chc,
	})

	// BackendConfig ilb, regional cluster
	chc = fixture.ilb()
	chc.HttpHealthCheck.RequestPath = "/foo"
	chc.Description = translator.DescriptionForHealthChecksFromBackendConfig
	cases = append(cases, &tc{
		desc:            "create backendconfig ilb regional cluster",
		sp:              testSPs["HTTP-80-ilb-bc-nothc"],
		regional:        true,
		regionalCluster: true,
		wantComputeHC:   chc,
	})

	// Probe and BackendConfig override
	chc = fixture.hc()
	chc.HttpHealthCheck.RequestPath = "/foo"
	chc.HttpHealthCheck.Host = "foo.com"
	chc.CheckIntervalSec = 61
	chc.TimeoutSec = 1234
	chc.Description = translator.DescriptionForHealthChecksFromBackendConfig
	cases = append(cases, &tc{
		desc: "create probe and backendconfig",
		sp:   testSPs["HTTP-80-reg-bc-nothc"],
		probe: &v1.Probe{
			ProbeHandler: api_v1.ProbeHandler{
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
		sp:            testSPs["HTTP-80-reg-nil-nothc"],
		wantComputeHC: fixture.hc(),
	})
	cases = append(cases, &tc{
		desc:          "update https no change",
		setup:         fixture.setupExistingHCFunc(fixture.hcs()),
		sp:            testSPs["HTTPS-80-reg-nil-nothc"],
		wantComputeHC: fixture.hcs(),
	})
	cases = append(cases, &tc{
		desc:          "update http2 no change",
		setup:         fixture.setupExistingHCFunc(fixture.hc2()),
		sp:            testSPs["HTTP2-80-reg-nil-nothc"],
		wantComputeHC: fixture.hc2(),
	})
	cases = append(cases, &tc{
		desc:          "update neg no change",
		setup:         fixture.setupExistingHCFunc(fixture.neg()),
		sp:            testSPs["HTTP-80-neg-nil-nothc"],
		wantComputeHC: fixture.neg(),
	})
	cases = append(cases, &tc{
		desc:          "update ilb no change",
		setup:         fixture.setupExistingHCFunc(fixture.ilb()),
		sp:            testSPs["HTTP-80-ilb-nil-nothc"],
		regional:      true,
		wantComputeHC: fixture.ilb(),
	})

	// Update protocol
	cases = append(cases, &tc{
		desc:          "update http to https",
		setup:         fixture.setupExistingHCFunc(fixture.hc()),
		sp:            testSPs["HTTPS-80-reg-nil-nothc"],
		wantComputeHC: fixture.hcs(),
	})
	cases = append(cases, &tc{
		desc:          "update http to http2",
		setup:         fixture.setupExistingHCFunc(fixture.hc()),
		sp:            testSPs["HTTP2-80-reg-nil-nothc"],
		wantComputeHC: fixture.hc2(),
	})
	cases = append(cases, &tc{
		desc:          "update https to http",
		setup:         fixture.setupExistingHCFunc(fixture.hcs()),
		sp:            testSPs["HTTP-80-reg-nil-nothc"],
		wantComputeHC: fixture.hc(),
	})
	cases = append(cases, &tc{
		desc:          "update https to http2",
		setup:         fixture.setupExistingHCFunc(fixture.hcs()),
		sp:            testSPs["HTTP2-80-reg-nil-nothc"],
		wantComputeHC: fixture.hc2(),
	})
	cases = append(cases, &tc{
		desc:          "update neg protocol",
		setup:         fixture.setupExistingHCFunc(fixture.neg()),
		sp:            testSPs["HTTPS-80-neg-nil-nothc"],
		wantComputeHC: fixture.negs(),
	})
	cases = append(cases, &tc{
		desc:          "update ilb protocol",
		setup:         fixture.setupExistingRegionalHCFunc("us-central1", fixture.ilb()),
		sp:            testSPs["HTTPS-80-ilb-nil-nothc"],
		regional:      true,
		wantComputeHC: fixture.ilbs(),
	})
	cases = append(cases, &tc{
		desc:          "update neg to thc",
		setup:         fixture.setupExistingHCFunc(fixture.neg()),
		sp:            testSPs["HTTP-80-neg-nil-thc"],
		wantComputeHC: fixture.thc(),
		enableTHC:     true,
	})
	w := fixture.thc()
	toIlb(w)
	cases = append(cases, &tc{
		desc:          "update ilb to thc",
		setup:         fixture.setupExistingHCFunc(fixture.ilb()),
		sp:            testSPs["HTTP-80-ilb-nil-thc"],
		regional:      true,
		wantComputeHC: w,
		enableTHC:     true,
	})
	cases = append(cases, &tc{
		desc:            "update ilb to thc regional cluster",
		setup:           fixture.setupExistingHCFunc(fixture.ilb()),
		sp:              testSPs["HTTP-80-ilb-nil-thc"],
		regional:        true,
		regionalCluster: true,
		wantComputeHC:   w,
		enableTHC:       true,
	})

	// Preserve user settings.
	chc = fixture.hc()
	chc.HttpHealthCheck.RequestPath = "/user-path"
	chc.CheckIntervalSec = 1234
	cases = append(cases, &tc{
		desc:          "update preserve",
		setup:         fixture.setupExistingHCFunc(chc),
		sp:            testSPs["HTTP-80-reg-nil-nothc"],
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
		sp:            testSPs["HTTPS-80-reg-nil-nothc"],
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
		sp:            testSPs["HTTPS-80-neg-nil-nothc"],
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
		sp:            testSPs["HTTPS-80-ilb-nil-nothc"],
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
	wantCHC.Description = translator.DescriptionForHealthChecksFromBackendConfig
	cases = append(cases, &tc{
		desc:          "update preserve and backendconfig (path)",
		sp:            testSPs["HTTP-80-reg-bc-nothc"],
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
	wantCHC.Description = translator.DescriptionForHealthChecksFromBackendConfig
	cases = append(cases, &tc{
		desc:          "update preserve backendconfig all",
		setup:         fixture.setupExistingHCFunc(chc),
		sp:            testSPs["HTTP-80-reg-bcall-nothc"],
		wantComputeHC: wantCHC,
	})

	// Override all settings from thc.
	chc = fixture.neg()
	chc.HttpHealthCheck.RequestPath = "/user-path"
	chc.CheckIntervalSec = 5678
	chc.CheckIntervalSec = 5678
	chc.HealthyThreshold = 5678
	chc.UnhealthyThreshold = 5678
	chc.TimeoutSec = 5678
	chc.HttpHealthCheck.Port = 5678
	chc.HttpHealthCheck.PortSpecification = "USE_FIXED_PORT"
	cases = append(cases, &tc{
		desc:          "update preserve thc",
		setup:         fixture.setupExistingHCFunc(chc),
		sp:            testSPs["HTTP-80-neg-nil-thc"],
		wantComputeHC: fixture.thc(),
		enableTHC:     true,
	})

	// BUG: changing probe settings has not effect on the healthcheck
	// update.
	// TODO(bowei): document this.
	chc = fixture.hc()
	chc.HttpHealthCheck.RequestPath = "/user-path"
	cases = append(cases, &tc{
		desc:  "update probe has no effect (bug)",
		setup: fixture.setupExistingHCFunc(chc),
		sp:    testSPs["HTTP-80-reg-nil-nothc"],
		probe: &v1.Probe{
			ProbeHandler: api_v1.ProbeHandler{
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
		tc := *tc
		tc.updateHCDescription = true
		tc.desc = tc.desc + " with updateHCDescription"
		copyOfWant := *tc.wantComputeHC
		if tc.sp.BackendConfig != nil || tc.sp.THCConfiguration.THCOptInOnSvc == true {
			config := healthcheck.TransparentHC
			if tc.sp.BackendConfig != nil {
				config = healthcheck.BackendConfigHC
			}
			copyOfWant.Description = jsonDescription(config, tc.regionalCluster)
		}
		tc.wantComputeHC = &copyOfWant
		cases = append(cases, &tc)
	}

	for _, tc := range cases {
		if tc.updateHCDescription {
			tc := *tc
			tc.recalc = true
			tc.desc = tc.desc + " and recalc"
			copyOfWant := *tc.wantComputeHC
			tc.wantComputeHC = &copyOfWant
			cases = append(cases, &tc)
		}
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			flags.F.EnableUpdateCustomHealthCheckDescription = tc.updateHCDescription
			testClusterValues := gce.DefaultTestClusterValues()
			testClusterValues.Regional = tc.regionalCluster
			fakeGCE := gce.NewFakeGCECloud(testClusterValues)

			mock := fakeGCE.Compute().(*cloud.MockGCE)
			setupMockUpdate(mock)

			if tc.setup != nil {
				tc.setup(mock)
			}

			hcs := NewHealthChecker(fakeGCE, "/", defaultBackendSvc, NewFakeRecorderGetter(0), NewFakeServiceGetter(), HealthcheckFlags{
				EnableTHC: tc.enableTHC,
				EnableRecalculationOnBackendConfigRemoval: tc.recalc,
				THCPort: 7877,
			})

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

				// Filter out SelfLink (because it is hard to deal with in the mock and
				// test cases) and Description if the flag EnableUpdateCustomHealthCheckDescription is disabled.
				filter := func(hc compute.HealthCheck) compute.HealthCheck {
					hc.SelfLink = ""
					if !tc.updateHCDescription {
						hc.Description = ""
					}
					return hc
				}
				gotHC := filter(*computeHCs[0])
				wantHC := filter(*tc.wantComputeHC)

				if !reflect.DeepEqual(gotHC, wantHC) {
					t.Fatalf("Compute healthcheck is:\n%s, want:\n%s", pretty.Sprint(gotHC), pretty.Sprint(wantHC))
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

func jsonDescription(config healthcheck.HealthcheckConfig, regionalCluster bool) string {
	var wantLocation string
	if regionalCluster {
		wantLocation = gce.DefaultTestClusterValues().Region
	} else {
		wantLocation = gce.DefaultTestClusterValues().ZoneName
	}
	wantDesc := healthcheck.HealthcheckDesc{
		K8sCluster:  fmt.Sprintf("/locations/%s/clusters/%s", wantLocation, gce.DefaultTestClusterValues().ClusterName),
		K8sResource: fmt.Sprintf("/namespaces/%s/services/%s", defaultBackendSvc.Namespace, defaultBackendSvc.Name),
		Config:      config,
	}
	bytes, err := json.MarshalIndent(wantDesc, "", "    ")
	if err != nil {
		klog.Fatal("Error while generating the wantDesc JSON.")
	}
	return string(bytes)
}

// TestCustomHealthcheckRemoval tests recalculation of health check parameters on removal of
// BackendConfig and TransparentHealthCheck (THC) annotations. Depending on the flags, Description
// update or full racalculation may happen on removal of BackendConfig. Full recalculation
// happens on THC removal.
func TestCustomHealthcheckRemoval(t *testing.T) {
	// No parallel() because we modify the value of the flags:
	// - EnableUpdateCustomHealthCheckDescription.
	// – GKEClusterName
	oldUpdateDescription := flags.F.EnableUpdateCustomHealthCheckDescription
	oldGKEClusterName := flags.F.GKEClusterName
	defer func() {
		flags.F.EnableUpdateCustomHealthCheckDescription = oldUpdateDescription
		flags.F.GKEClusterName = oldGKEClusterName
	}()
	flags.F.GKEClusterName = gce.DefaultTestClusterValues().ClusterName

	type tc struct {
		desc                string
		setup               func(*cloud.MockGCE)
		sp                  utils.ServicePort
		probe               *v1.Probe
		wantSelfLink        string
		wantErr             bool
		wantComputeHC       *compute.HealthCheck
		updateHCDescription bool
		enableTHC           bool
		recalc              bool
		expectedEvent       int
		wantEventPrefix     string
	}
	fixture := syncSPFixture{}

	var cases []*tc

	// Don't recalculate health check on BackendConfig removal (legacy behaviour, recalc == false), but update Description.
	chc := fixture.bchc()
	wantCHC := fixture.bchc()
	wantCHC.Description = translator.DescriptionForDefaultHealthChecks
	cases = append(cases, &tc{
		desc:                "not recalculate to default on bc removal",
		setup:               fixture.setupExistingHCFunc(chc),
		sp:                  *testSPs["HTTP-80-reg-nil-nothc"],
		wantComputeHC:       wantCHC,
		updateHCDescription: true,
		expectedEvent:       1,
		wantEventPrefix:     "Normal HealthcheckDescriptionUpdate Healthcheck will be updated and the only field updated is Description",
	})

	// Don't recalculate health check on BackendConfig removal (legacy behaviour, recalc == false), but update Description.
	chc = fixture.bchc()
	wantCHC = fixture.bchc()
	wantCHC.Description = translator.DescriptionForHealthChecksFromReadinessProbe
	cases = append(cases, &tc{
		desc:  "not recalculate to rp on bc removal",
		setup: fixture.setupExistingHCFunc(chc),
		sp:    *testSPs["HTTP-80-reg-nil-nothc"],
		probe: &v1.Probe{
			ProbeHandler: api_v1.ProbeHandler{
				HTTPGet: &v1.HTTPGetAction{Path: "/foo", Host: "foo.com"},
			},
			PeriodSeconds:  1,
			TimeoutSeconds: 1234,
		},
		wantComputeHC:       wantCHC,
		updateHCDescription: true,
		expectedEvent:       1,
		wantEventPrefix:     "Normal HealthcheckDescriptionUpdate Healthcheck will be updated and the only field updated is Description",
	})

	// Update hc from BackendConfig with only interval specified to Default.
	chc = fixture.hc()
	chc.CheckIntervalSec = 17
	chc.Description = (&healthcheck.HealthcheckInfo{HealthcheckConfig: healthcheck.BackendConfigHC}).GenerateHealthcheckDescription()
	wantCHC = fixture.hc()
	cases = append(cases, &tc{
		desc:                "update hc from BackendConfig to Default",
		setup:               fixture.setupExistingHCFunc(chc),
		sp:                  *testSPs["HTTP-80-reg-nil-nothc"],
		wantComputeHC:       wantCHC,
		updateHCDescription: true,
		recalc:              true,
	})

	// Update hc from BackendConfig to NEG.
	chc = fixture.neg()
	chc.CheckIntervalSec = 17
	chc.Description = (&healthcheck.HealthcheckInfo{HealthcheckConfig: healthcheck.BackendConfigHC}).GenerateHealthcheckDescription()
	wantCHC = fixture.neg()
	cases = append(cases, &tc{
		desc:                "update hc from BackendConfig to NEG",
		setup:               fixture.setupExistingHCFunc(chc),
		sp:                  *testSPs["HTTP-80-neg-nil-nothc"],
		wantComputeHC:       wantCHC,
		updateHCDescription: true,
		recalc:              true,
	})

	// Update hc from BackendConfig to ReadinessProbe.
	chc = fixture.neg()
	chc.Description = (&healthcheck.HealthcheckInfo{HealthcheckConfig: healthcheck.BackendConfigHC}).GenerateHealthcheckDescription()
	wantCHC = fixture.neg()
	wantCHC.HttpHealthCheck.RequestPath = "/foo"
	wantCHC.HttpHealthCheck.Host = "foo.com"
	wantCHC.CheckIntervalSec = 1234
	wantCHC.TimeoutSec = 5678
	wantCHC.Description = translator.DescriptionForHealthChecksFromReadinessProbe
	cases = append(cases, &tc{
		desc:  "update hc from BackendConfig to ReadinessProbe",
		setup: fixture.setupExistingHCFunc(chc),
		sp:    *testSPs["HTTP-80-neg-nil-nothc"],
		probe: &v1.Probe{
			ProbeHandler: api_v1.ProbeHandler{
				HTTPGet: &v1.HTTPGetAction{Path: "/foo", Host: "foo.com"},
			},
			PeriodSeconds:  1234,
			TimeoutSeconds: 5678,
		},
		wantComputeHC:       wantCHC,
		updateHCDescription: true,
		recalc:              true,
	})

	// Update hc from THC to NEG.
	chc = fixture.thc()
	wantCHC = fixture.neg()
	cases = append(cases, &tc{
		desc:          "update hc from THC to NEG",
		setup:         fixture.setupExistingHCFunc(chc),
		sp:            *testSPs["HTTP-80-neg-nil-nothc"],
		wantComputeHC: wantCHC,
		enableTHC:     true,
	})

	// Update hc from THC to NEG on THC flag disablement. In practise, this is covered by the previous case
	// "update hc from THC to NEG", because it should never happen that utils.ServicePort has
	// THCConfiguration.THCOptInOnSvc == true despite the flag enableTHC disabled.
	chc = fixture.thc()
	wantCHC = fixture.neg()
	cases = append(cases, &tc{
		desc:          "update hc from THC to NEG on THC flag disablement",
		setup:         fixture.setupExistingHCFunc(chc),
		sp:            *testSPs["HTTP-80-neg-nil-thc"],
		wantComputeHC: wantCHC,
		enableTHC:     false,
	})

	// Update hc from THC to ReadinessProbe.
	chc = fixture.thc()
	wantCHC = fixture.neg()
	wantCHC.HttpHealthCheck.RequestPath = "/foo"
	wantCHC.HttpHealthCheck.Host = "foo.com"
	wantCHC.CheckIntervalSec = 1234
	wantCHC.TimeoutSec = 5678
	wantCHC.Description = translator.DescriptionForHealthChecksFromReadinessProbe
	cases = append(cases, &tc{
		desc:  "update hc from THC to ReadinessProbe",
		setup: fixture.setupExistingHCFunc(chc),
		sp:    *testSPs["HTTP-80-neg-nil-nothc"],
		probe: &v1.Probe{
			ProbeHandler: api_v1.ProbeHandler{
				HTTPGet: &v1.HTTPGetAction{Path: "/foo", Host: "foo.com"},
			},
			PeriodSeconds:  1234,
			TimeoutSeconds: 5678,
		},
		wantComputeHC: wantCHC,
	})

	// Update hc from THC to BackendConfig, where JSON is not enabled for BC.
	chc = fixture.thc()
	json := false
	wantCHC = hcFromBC(testSPs["HTTP-80-neg-bcall-nothc"].BackendConfig.Spec.HealthCheck, true, json)
	cases = append(cases, &tc{
		desc:                "update hc from THC to BackendConfig without JSON",
		setup:               fixture.setupExistingHCFunc(chc),
		sp:                  *testSPs["HTTP-80-neg-bcall-nothc"],
		wantComputeHC:       wantCHC,
		updateHCDescription: json,
	})

	// Update hc from THC to BackendConfig.
	chc = fixture.thc()
	json = true
	wantCHC = hcFromBC(testSPs["HTTP-80-neg-bcall-nothc"].BackendConfig.Spec.HealthCheck, true, json)
	cases = append(cases, &tc{
		desc:                "update hc from THC to BackendConfig",
		setup:               fixture.setupExistingHCFunc(chc),
		sp:                  *testSPs["HTTP-80-neg-bcall-nothc"],
		wantComputeHC:       wantCHC,
		updateHCDescription: json,
	})

	// Update hc from BackendConfig (with JSON) to THC.
	json = true
	chc = hcFromBC(testSPs["HTTP-80-neg-bcall-nothc"].BackendConfig.Spec.HealthCheck, true, json)
	wantCHC = fixture.thc()
	cases = append(cases, &tc{
		desc:                "update hc from JSON BC to THC",
		setup:               fixture.setupExistingHCFunc(chc),
		sp:                  *testSPs["HTTP-80-neg-nil-thc"],
		wantComputeHC:       wantCHC,
		updateHCDescription: json,
		enableTHC:           true,
	})

	// Update hc from BackendConfig (without JSON!) to THC.
	json = false
	chc = hcFromBC(testSPs["HTTP-80-neg-bcall-nothc"].BackendConfig.Spec.HealthCheck, true, json)
	wantCHC = fixture.thc()
	cases = append(cases, &tc{
		desc:                "update hc from plain-text BC to THC",
		setup:               fixture.setupExistingHCFunc(chc),
		sp:                  *testSPs["HTTP-80-neg-nil-thc"],
		wantComputeHC:       wantCHC,
		updateHCDescription: json,
		enableTHC:           true,
	})

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			flags.F.EnableUpdateCustomHealthCheckDescription = tc.updateHCDescription
			testClusterValues := gce.DefaultTestClusterValues()
			fakeGCE := gce.NewFakeGCECloud(testClusterValues)

			mock := fakeGCE.Compute().(*cloud.MockGCE)
			setupMockUpdate(mock)

			tc.setup(mock)

			fakeSingletonRecorderGetter := NewFakeSingletonRecorderGetter(tc.expectedEvent)
			hcs := NewHealthChecker(fakeGCE, "/", defaultBackendSvc, fakeSingletonRecorderGetter, NewFakeServiceGetter(), HealthcheckFlags{
				EnableTHC: tc.enableTHC,
				EnableRecalculationOnBackendConfigRemoval: tc.recalc,
				THCPort: 7877,
			})

			gotSelfLink, err := hcs.SyncServicePort(&tc.sp, tc.probe)
			if gotErr := err != nil; gotErr != tc.wantErr {
				t.Errorf("hcs.SyncServicePort(tc.sp, tc.probe) = _, %v; gotErr = %t, want %t\nsp = %s\nprobe = %s", err, gotErr, tc.wantErr, pretty.Sprint(tc.sp), pretty.Sprint(tc.probe))
			}
			if tc.wantSelfLink != "" && gotSelfLink != tc.wantSelfLink {
				t.Errorf("hcs.SyncServicePort(tc.sp, tc.probe) = %q, _; want = %q", gotSelfLink, tc.wantSelfLink)
			}

			verify := func() {
				t.Helper()
				var computeHCs []*compute.HealthCheck
				computeHCs, _ = fakeGCE.Compute().HealthChecks().List(context.Background(), nil)
				if len(computeHCs) != 1 {
					t.Fatalf("Got %d healthchecks, want 1\n%s", len(computeHCs), pretty.Sprint(computeHCs))
				}

				// Filter out SelfLink because it is hard to deal with in the mock and
				// test cases.
				filter := func(hc compute.HealthCheck) compute.HealthCheck {
					hc.SelfLink = ""
					return hc
				}
				gotHC := filter(*computeHCs[0])
				wantHC := filter(*tc.wantComputeHC)

				if !reflect.DeepEqual(gotHC, wantHC) {
					t.Fatalf("Compute healthcheck is:\n%s, want:\n%s", pretty.Sprint(gotHC), pretty.Sprint(wantHC))
				}
			}

			verify()

			// Check that resync should not have an effect and does not issue
			// an update to GCE. Hook Update() to fail.
			mock.MockHealthChecks.UpdateHook = func(context.Context, *meta.Key, *compute.HealthCheck, *cloud.MockHealthChecks) error {
				t.Fatalf("resync should not result in an update")
				return nil
			}

			gotSelfLink, err = hcs.SyncServicePort(&tc.sp, tc.probe)
			if gotErr := err != nil; gotErr != tc.wantErr {
				t.Errorf("hcs.SyncServicePort(tc.sp, tc.probe) = %v; gotErr = %t, want %t\nsp = %s\nprobe = %s", err, gotErr, tc.wantErr, pretty.Sprint(tc.sp), pretty.Sprint(tc.probe))
			}
			if tc.wantSelfLink != "" && gotSelfLink != tc.wantSelfLink {
				t.Errorf("hcs.SyncServicePort(tc.sp, tc.probe) = %q, _; want = %q", gotSelfLink, tc.wantSelfLink)
			}
			verify()

			if tc.expectedEvent > 0 {
				fakeRecorder := fakeSingletonRecorderGetter.FakeRecorder()
				for i := 0; i < tc.expectedEvent; i++ {
					select {
					case output := <-fakeRecorder.Events:
						if !strings.HasPrefix(output, tc.wantEventPrefix) {
							t.Errorf("Incorrect event emitted on healthcheck update: \"%s\".", output)
						}
					case <-time.After(10 * time.Second):
						t.Fatalf("Timeout when expecting Event.")
					}
				}
				select {
				case output := <-fakeRecorder.Events:
					t.Fatalf("Unexpected event: %s", output)
				case <-time.After(100 * time.Millisecond):
				}
			}
		})
	}
}

func TestIsBackendConfigRemoved(t *testing.T) {
	t.Parallel()

	healthcheckDescBefore := map[string]*healthcheck.HealthcheckDesc{
		"non-JSON": nil,
		"BC":       {Config: healthcheck.BackendConfigHC},
		"THC":      {Config: healthcheck.TransparentHC},
	}

	bcAfter := map[string]*backendconfigv1.HealthCheckConfig{
		"no-BC": nil,
		"BC":    {},
	}

	type testCase struct {
		desc   string
		hcDesc *healthcheck.HealthcheckDesc
		bchcc  *backendconfigv1.HealthCheckConfig
		want   bool
	}

	testCases := []testCase{}

	for beforeKey, beforeValue := range healthcheckDescBefore {
		for afterKey, afterValue := range bcAfter {
			newCase := testCase{
				desc:   fmt.Sprintf("%s to %s", beforeKey, afterKey),
				hcDesc: beforeValue,
				bchcc:  afterValue,
				want:   beforeKey == "BC" && afterKey == "no-BC",
			}
			testCases = append(testCases, newCase)
		}
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			got := isBackendConfigRemoved(tc.hcDesc, tc.bchcc)
			if got != tc.want {
				t.Errorf("healthchecker.isBackendConfigRemoved(%v, %v): got=%v, want=%v", tc.hcDesc, tc.bchcc, got, tc.want)
			}
		})
	}
}

func TestIsTHCRemoved(t *testing.T) {
	t.Parallel()

	healthcheckDescBefore := map[string]*healthcheck.HealthcheckDesc{
		"non-JSON": nil,
		"BC":       {Config: healthcheck.BackendConfigHC},
		"THC":      {Config: healthcheck.TransparentHC},
	}

	thcAfter := map[string]bool{"THC": true, "NO-THC": false}

	type testCase struct {
		desc          string
		hcDesc        *healthcheck.HealthcheckDesc
		thcOptInOnSvc bool
		want          bool
	}

	testCases := []testCase{}

	for beforeKey, beforeValue := range healthcheckDescBefore {
		for afterKey, afterValue := range thcAfter {
			newCase := testCase{
				desc:          fmt.Sprintf("%s to %s", beforeKey, afterKey),
				hcDesc:        beforeValue,
				thcOptInOnSvc: afterValue,
				want:          beforeKey == "THC" && afterKey == "NO-THC",
			}
			testCases = append(testCases, newCase)
		}
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			got := isTHCRemoved(tc.hcDesc, tc.thcOptInOnSvc)
			if got != tc.want {
				t.Errorf("isTHCRemoved(%v, %v): got=%v, want=%v", tc.hcDesc, tc.thcOptInOnSvc, got, tc.want)
			}
		})
	}
}

// TODO(bowei): test regional delete
// TODO(bowei): test errors from GCE
