/*
Copyright 2015 The Kubernetes Authors.

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

package backends

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"testing"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/mock"
	computebeta "google.golang.org/api/compute/v0.beta"
	compute "google.golang.org/api/compute/v1"
	"google.golang.org/api/googleapi"
	api_v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/backends/features"
	"k8s.io/ingress-gce/pkg/healthchecks"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/kubernetes/pkg/cloudprovider/providers/gce"
)

var (
	defaultNamer      = utils.NewNamer("uid1", "fw1")
	defaultBackendSvc = types.NamespacedName{Namespace: "system", Name: "default"}
	existingProbe     = &api_v1.Probe{
		Handler: api_v1.Handler{
			HTTPGet: &api_v1.HTTPGetAction{
				Scheme: api_v1.URISchemeHTTPS,
				Path:   "/my-special-path",
				Port: intstr.IntOrString{
					Type:   intstr.Int,
					IntVal: 443,
				},
			},
		},
	}
)

func newTestSyncer(fakeGCE *gce.Cloud) *backendSyncer {
	fakeHealthCheckProvider := healthchecks.NewFakeHealthCheckProvider()
	fakeHealthChecks := healthchecks.NewHealthChecker(fakeHealthCheckProvider, "/", "/healthz", defaultNamer, defaultBackendSvc)

	fakeBackendPool := NewPool(fakeGCE, defaultNamer)

	syncer := &backendSyncer{
		backendPool:   fakeBackendPool,
		healthChecker: fakeHealthChecks,
		namer:         defaultNamer,
	}

	probes := map[utils.ServicePort]*api_v1.Probe{{NodePort: 443, Protocol: annotations.ProtocolHTTPS}: existingProbe}
	syncer.Init(NewFakeProbeProvider(probes))

	// Add standard hooks for mocking update calls. Each test can set a different update hook if it chooses to.
	(fakeGCE.Compute().(*cloud.MockGCE)).MockAlphaBackendServices.UpdateHook = mock.UpdateAlphaBackendServiceHook
	(fakeGCE.Compute().(*cloud.MockGCE)).MockBetaBackendServices.UpdateHook = mock.UpdateBetaBackendServiceHook
	(fakeGCE.Compute().(*cloud.MockGCE)).MockBackendServices.UpdateHook = mock.UpdateBackendServiceHook

	return syncer
}

func TestSync(t *testing.T) {
	fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
	syncer := newTestSyncer(fakeGCE)

	testCases := []utils.ServicePort{
		{NodePort: 80, Protocol: annotations.ProtocolHTTP},
		// Note: 443 gets its healthcheck from a probe
		{NodePort: 443, Protocol: annotations.ProtocolHTTPS},
		{NodePort: 3000, Protocol: annotations.ProtocolHTTP2},
	}

	for _, sp := range testCases {
		t.Run(fmt.Sprintf("Port: %v Protocol: %v", sp.NodePort, sp.Protocol), func(t *testing.T) {
			if err := syncer.Sync([]utils.ServicePort{sp}); err != nil {
				t.Fatalf("Unexpected error when syncing backend with port %v: %v", sp.NodePort, err)
			}
			beName := sp.BackendName(defaultNamer)

			// Check that the new backend has the right port
			be, err := syncer.backendPool.Get(beName, features.VersionFromServicePort(&sp))
			if err != nil {
				t.Fatalf("Did not find expected backend with port %v", sp.NodePort)
			}
			if be.Port != sp.NodePort {
				t.Fatalf("Backend %v has wrong port %v, expected %v", be.Name, be.Port, sp)
			}

			hc, err := syncer.healthChecker.Get(beName, features.VersionFromServicePort(&sp))
			if err != nil {
				t.Fatalf("Unexpected err when querying fake healthchecker: %v", err)
			}

			if hc.Protocol() != sp.Protocol {
				t.Fatalf("Healthcheck scheme does not match nodeport scheme: hc:%v np:%v", hc.Protocol(), sp.Protocol)
			}

			if sp.NodePort == 443 && hc.RequestPath != "/my-special-path" {
				t.Fatalf("Healthcheck for 443 should have special request path from probe")
			}
		})
	}
}

func TestSyncUpdateHTTPS(t *testing.T) {
	fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
	syncer := newTestSyncer(fakeGCE)

	p := utils.ServicePort{NodePort: 3000, Protocol: annotations.ProtocolHTTP}
	syncer.Sync([]utils.ServicePort{p})
	beName := p.BackendName(defaultNamer)

	be, err := syncer.backendPool.Get(beName, features.VersionFromServicePort(&p))
	if err != nil {
		t.Fatalf("Unexpected err: %v", err)
	}

	if annotations.AppProtocol(be.Protocol) != p.Protocol {
		t.Fatalf("Expected scheme %v but got %v", p.Protocol, be.Protocol)
	}

	// Assert the proper health check was created
	hc, _ := syncer.healthChecker.Get(beName, features.VersionFromServicePort(&p))
	if hc == nil || hc.Protocol() != p.Protocol {
		t.Fatalf("Expected %s health check, received %v: ", p.Protocol, hc)
	}

	// Update service port to encrypted
	p.Protocol = annotations.ProtocolHTTPS
	syncer.Sync([]utils.ServicePort{p})

	be, err = syncer.backendPool.Get(beName, features.VersionFromServicePort(&p))
	if err != nil {
		t.Fatalf("Unexpected err retrieving backend service after update: %v", err)
	}

	// Assert the backend has the correct protocol
	if annotations.AppProtocol(be.Protocol) != p.Protocol {
		t.Fatalf("Expected scheme %v but got %v", p.Protocol, annotations.AppProtocol(be.Protocol))
	}

	// Assert the proper health check was created
	hc, _ = syncer.healthChecker.Get(beName, features.VersionFromServicePort(&p))
	if hc == nil || hc.Protocol() != p.Protocol {
		t.Fatalf("Expected %s health check, received %v: ", p.Protocol, hc)
	}
}

func TestSyncUpdateHTTP2(t *testing.T) {
	fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
	syncer := newTestSyncer(fakeGCE)

	p := utils.ServicePort{NodePort: 3000, Protocol: annotations.ProtocolHTTP}
	syncer.Sync([]utils.ServicePort{p})
	beName := p.BackendName(defaultNamer)

	be, err := syncer.backendPool.Get(beName, features.VersionFromServicePort(&p))
	if err != nil {
		t.Fatalf("Unexpected err: %v", err)
	}

	if annotations.AppProtocol(be.Protocol) != p.Protocol {
		t.Fatalf("Expected scheme %v but got %v", p.Protocol, be.Protocol)
	}

	// Assert the proper health check was created
	hc, _ := syncer.healthChecker.Get(beName, features.VersionFromServicePort(&p))
	if hc == nil || hc.Protocol() != p.Protocol {
		t.Fatalf("Expected %s health check, received %v: ", p.Protocol, hc)
	}

	// Update service port to HTTP2
	p.Protocol = annotations.ProtocolHTTP2
	syncer.Sync([]utils.ServicePort{p})

	beBeta, err := syncer.backendPool.Get(beName, features.VersionFromServicePort(&p))
	if err != nil {
		t.Fatalf("Unexpected err retrieving backend service after update: %v", err)
	}

	// Assert the backend has the correct protocol
	if annotations.AppProtocol(beBeta.Protocol) != p.Protocol {
		t.Fatalf("Expected scheme %v but got %v", p.Protocol, annotations.AppProtocol(beBeta.Protocol))
	}

	// Assert the proper health check was created
	hc, _ = syncer.healthChecker.Get(beName, meta.VersionAlpha)
	if hc == nil || hc.Protocol() != p.Protocol {
		t.Fatalf("Expected %s health check, received %v: ", p.Protocol, hc)
	}
}

func TestGC(t *testing.T) {
	fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
	syncer := newTestSyncer(fakeGCE)

	svcNodePorts := []utils.ServicePort{
		{NodePort: 81, Protocol: annotations.ProtocolHTTP},
		{NodePort: 82, Protocol: annotations.ProtocolHTTPS},
		{NodePort: 83, Protocol: annotations.ProtocolHTTP},
	}

	if err := syncer.Sync(svcNodePorts); err != nil {
		t.Fatalf("Expected syncer to add backends with error, err: %v", err)
	}
	// Check that all backends were created.
	for _, sp := range svcNodePorts {
		beName := sp.BackendName(defaultNamer)
		if _, err := fakeGCE.GetGlobalBackendService(beName); err != nil {
			t.Fatalf("Expected to find backend for port %v, err: %v", sp.NodePort, err)
		}
	}
	// Run a no-op GC (i.e nothing is actually cleaned up)
	if err := syncer.GC(svcNodePorts); err != nil {
		t.Fatalf("Expected backend pool to GC, err: %v", err)
	}
	// Ensure that no backends were actually deleted
	for _, sp := range svcNodePorts {
		beName := sp.BackendName(defaultNamer)
		if _, err := fakeGCE.GetGlobalBackendService(beName); err != nil {
			t.Fatalf("Expected to find backend for port %v, err: %v", sp.NodePort, err)
		}
	}

	deletedPorts := []utils.ServicePort{svcNodePorts[1], svcNodePorts[2]}
	svcNodePorts = []utils.ServicePort{svcNodePorts[0]}
	if err := syncer.GC(svcNodePorts); err != nil {
		t.Fatalf("Expected backend pool to GC, err: %v", err)
	}

	// Ensure that 2 out of the 3 backends were deleted
	for _, sp := range deletedPorts {
		beName := sp.BackendName(defaultNamer)
		if _, err := fakeGCE.GetGlobalBackendService(beName); err == nil {
			t.Fatalf("Expected to not find backend for port %v", sp.NodePort)
		}
	}

	// Ensure that the 1 remaining backend exists
	for _, sp := range svcNodePorts {
		beName := sp.BackendName(defaultNamer)
		if _, err := fakeGCE.GetGlobalBackendService(beName); err != nil {
			t.Fatalf("Expected to find backend for port %v, err: %v", sp.NodePort, err)
		}
	}
}

func TestSyncQuota(t *testing.T) {
	testCases := []struct {
		oldPorts      []utils.ServicePort
		newPorts      []utils.ServicePort
		expectSyncErr bool
		desc          string
	}{
		{
			[]utils.ServicePort{{NodePort: 8080}},
			[]utils.ServicePort{{NodePort: 8080}},
			false,
			"Same port",
		},
		{
			[]utils.ServicePort{{NodePort: 8080}},
			[]utils.ServicePort{{NodePort: 9000}},
			true,
			"Different port",
		},
		{
			[]utils.ServicePort{{NodePort: 8080}},
			[]utils.ServicePort{{NodePort: 8080}, {NodePort: 443}},
			false,
			"Same port plus additional port",
		},
		{
			[]utils.ServicePort{{NodePort: 8080}},
			[]utils.ServicePort{{NodePort: 3000}, {NodePort: 4000}, {NodePort: 5000}},
			true,
			"New set of ports not including the same port",
		},
		// Need to fill the TargetPort field on ServicePort to make sure
		// NEG Backend naming is unique
		{
			[]utils.ServicePort{{NodePort: 8080}, {NodePort: 443}},
			[]utils.ServicePort{
				{Port: 8080, NodePort: 8080, NEGEnabled: true},
				{Port: 443, NodePort: 443, NEGEnabled: true},
			},
			true,
			"Same port converted to NEG, plus one new NEG port",
		},
		{
			[]utils.ServicePort{
				{Port: 80, NodePort: 80, NEGEnabled: true},
				{Port: 90, NodePort: 90},
			},
			[]utils.ServicePort{
				{Port: 80},
				{Port: 90, NEGEnabled: true},
			},
			true,
			"Mixed NEG and non-NEG ports",
		},
		{
			[]utils.ServicePort{
				{Port: 100, NodePort: 100, NEGEnabled: true},
				{Port: 110, NodePort: 110, NEGEnabled: true},
				{Port: 120, NodePort: 120, NEGEnabled: true},
			},
			[]utils.ServicePort{
				{Port: 100, NodePort: 100},
				{Port: 110, NodePort: 110},
				{Port: 120, NodePort: 120},
			},
			true,
			"Same ports as NEG, then non-NEG",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
			syncer := newTestSyncer(fakeGCE)

			bsCreated := 0
			quota := len(tc.newPorts)

			// Add hooks to simulate quota changes & errors.
			insertFunc := func(ctx context.Context, key *meta.Key, beName string) (bool, error) {
				if bsCreated+1 > quota {
					return true, &googleapi.Error{Code: http.StatusForbidden, Body: beName}
				}
				bsCreated += 1
				return false, nil
			}
			(fakeGCE.Compute().(*cloud.MockGCE)).MockBackendServices.InsertHook = func(ctx context.Context, key *meta.Key, be *compute.BackendService, m *cloud.MockBackendServices) (bool, error) {
				return insertFunc(ctx, key, be.Name)
			}
			(fakeGCE.Compute().(*cloud.MockGCE)).MockBetaBackendServices.InsertHook = func(ctx context.Context, key *meta.Key, be *computebeta.BackendService, m *cloud.MockBetaBackendServices) (bool, error) {
				return insertFunc(ctx, key, be.Name)
			}
			(fakeGCE.Compute().(*cloud.MockGCE)).MockBackendServices.DeleteHook = func(ctx context.Context, key *meta.Key, m *cloud.MockBackendServices) (bool, error) {
				bsCreated -= 1
				return false, nil
			}

			if err := syncer.Sync(tc.oldPorts); err != nil {
				t.Errorf("Expected backend pool to add node ports, err: %v", err)
			}

			// Ensuring these ports again without first Garbage Collecting goes over
			// the set quota. Expect an error here, until GC is called.
			err := syncer.Sync(tc.newPorts)
			if tc.expectSyncErr && err == nil {
				t.Errorf("Expect initial sync to go over quota, but received no error")
			}

			syncer.GC(tc.newPorts)
			if err := syncer.Sync(tc.newPorts); err != nil {
				t.Errorf("Expected backend pool to add node ports, err: %v", err)
			}

			if bsCreated != quota {
				t.Errorf("Expected to create %v BackendServices, got: %v", quota, bsCreated)
			}
		})
	}
}

func TestSyncNEG(t *testing.T) {
	// Convert a BackendPool from non-NEG to NEG.
	// Expect the old BackendServices to be GC'ed
	fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
	syncer := newTestSyncer(fakeGCE)

	svcPort := utils.ServicePort{NodePort: 81, Protocol: annotations.ProtocolHTTP}
	if err := syncer.Sync([]utils.ServicePort{svcPort}); err != nil {
		t.Errorf("Expected backend pool to add node ports, err: %v", err)
	}

	nodePortName := svcPort.BackendName(defaultNamer)
	_, err := fakeGCE.GetGlobalBackendService(nodePortName)
	if err != nil {
		t.Fatalf("Failed to get backend service: %v", err)
	}

	// Convert to NEG
	svcPort.NEGEnabled = true
	if err := syncer.Sync([]utils.ServicePort{svcPort}); err != nil {
		t.Errorf("Expected backend pool to add node ports, err: %v", err)
	}

	negName := svcPort.BackendName(defaultNamer)
	_, err = fakeGCE.GetGlobalBackendService(negName)
	if err != nil {
		t.Fatalf("Failed to get backend service with name %v: %v", negName, err)
	}
	// GC should garbage collect the Backend on the old naming schema
	syncer.GC([]utils.ServicePort{svcPort})

	bs, err := syncer.backendPool.Get(nodePortName, features.VersionFromServicePort(&svcPort))
	if err == nil {
		t.Fatalf("Expected not to get BackendService with name %v, got: %+v", nodePortName, bs)
	}

	// Convert back to non-NEG
	svcPort.NEGEnabled = false
	if err := syncer.Sync([]utils.ServicePort{svcPort}); err != nil {
		t.Errorf("Expected backend pool to add node ports, err: %v", err)
	}

	syncer.GC([]utils.ServicePort{svcPort})

	_, err = fakeGCE.GetGlobalBackendService(nodePortName)
	if err != nil {
		t.Fatalf("Failed to get backend service with name %v: %v", nodePortName, err)
	}
}

func TestShutdown(t *testing.T) {
	fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
	syncer := newTestSyncer(fakeGCE)

	// Sync a backend and verify that it doesn't exist after Shutdown()
	syncer.Sync([]utils.ServicePort{{NodePort: 80}})
	syncer.Shutdown()
	if _, err := fakeGCE.GetGlobalBackendService(defaultNamer.IGBackend(80)); err == nil {
		t.Fatalf("%v", err)
	}
}

func TestApplyProbeSettingsToHC(t *testing.T) {
	p := "healthz"
	hc := healthchecks.DefaultHealthCheck(8080, annotations.ProtocolHTTPS)
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

	applyProbeSettingsToHC(probe, hc)

	if hc.Protocol() != annotations.ProtocolHTTPS || hc.Port != 8080 {
		t.Errorf("Basic HC settings changed")
	}
	if hc.RequestPath != "/"+p {
		t.Errorf("Failed to apply probe's requestpath")
	}
}

func TestEnsureBackendServiceProtocol(t *testing.T) {
	fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
	syncer := newTestSyncer(fakeGCE)

	svcPorts := []utils.ServicePort{
		{NodePort: 80, Protocol: annotations.ProtocolHTTP, ID: utils.ServicePortID{Port: intstr.FromInt(1)}},
		{NodePort: 443, Protocol: annotations.ProtocolHTTPS, ID: utils.ServicePortID{Port: intstr.FromInt(2)}},
		{NodePort: 3000, Protocol: annotations.ProtocolHTTP2, ID: utils.ServicePortID{Port: intstr.FromInt(3)}},
	}

	for _, oldPort := range svcPorts {
		for _, newPort := range svcPorts {
			t.Run(
				fmt.Sprintf("Updating Port:%v Protocol:%v to Port:%v Protocol:%v", oldPort.NodePort, oldPort.Protocol, newPort.NodePort, newPort.Protocol),
				func(t *testing.T) {
					syncer.Sync([]utils.ServicePort{oldPort})
					be, err := syncer.backendPool.Get(oldPort.BackendName(defaultNamer), features.VersionFromServicePort(&oldPort))
					if err != nil {
						t.Fatalf("%v", err)
					}
					needsProtocolUpdate := ensureProtocol(be, newPort)

					if reflect.DeepEqual(oldPort, newPort) {
						if needsProtocolUpdate {
							t.Fatalf("Expected ensureProtocol for the same port to return false, got %v", needsProtocolUpdate)
						}

					} else {
						if !needsProtocolUpdate {
							t.Fatalf("Expected ensureProtocol for updating to a new port to return true, got %v", needsProtocolUpdate)
						}
					}

					if newPort.Protocol == annotations.ProtocolHTTP2 {
						if be.Protocol != string(annotations.ProtocolHTTP2) {
							t.Fatalf("Expected HTTP2 protocol to be set on BackendService, got %v", be.Protocol)
						}
					}
				},
			)
		}
	}
}

func TestEnsureBackendServiceDescription(t *testing.T) {
	fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
	syncer := newTestSyncer(fakeGCE)

	svcPorts := []utils.ServicePort{
		{NodePort: 80, Protocol: annotations.ProtocolHTTP, ID: utils.ServicePortID{Port: intstr.FromInt(1)}},
		{NodePort: 443, Protocol: annotations.ProtocolHTTPS, ID: utils.ServicePortID{Port: intstr.FromInt(2)}},
		{NodePort: 3000, Protocol: annotations.ProtocolHTTP2, ID: utils.ServicePortID{Port: intstr.FromInt(3)}},
	}

	for _, oldPort := range svcPorts {
		for _, newPort := range svcPorts {
			t.Run(
				fmt.Sprintf("Updating Port:%v Protocol:%v to Port:%v Protocol:%v", oldPort.NodePort, oldPort.Protocol, newPort.NodePort, newPort.Protocol),
				func(t *testing.T) {
					syncer.Sync([]utils.ServicePort{oldPort})
					be, err := syncer.backendPool.Get(oldPort.BackendName(defaultNamer), features.VersionFromServicePort(&oldPort))
					if err != nil {
						t.Fatalf("%v", err)
					}

					needsDescriptionUpdate := ensureDescription(be, &newPort)
					if reflect.DeepEqual(oldPort, newPort) {
						if needsDescriptionUpdate {
							t.Fatalf("Expected ensureDescription for the same port to return false, got %v", needsDescriptionUpdate)
						}

					} else {
						if !needsDescriptionUpdate {
							t.Fatalf("Expected ensureDescription for updating to a new port to return true, got %v", needsDescriptionUpdate)
						}
					}
				},
			)
		}
	}
}

func TestEnsureBackendServiceHealthCheckLink(t *testing.T) {
	fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
	syncer := newTestSyncer(fakeGCE)

	p := utils.ServicePort{NodePort: 80, Protocol: annotations.ProtocolHTTP, ID: utils.ServicePortID{Port: intstr.FromInt(1)}}
	syncer.Sync([]utils.ServicePort{p})
	be, err := syncer.backendPool.Get(p.BackendName(defaultNamer), features.VersionFromServicePort(&p))
	if err != nil {
		t.Fatalf("%v", err)
	}

	hcLink := getHealthCheckLink(be)
	needsHcUpdate := ensureHealthCheckLink(be, hcLink)
	if needsHcUpdate {
		t.Fatalf("Expected ensureHealthCheckLink for the same link to return false, got %v", needsHcUpdate)
	}

	baseUrl := "https://www.googleapis.com/compute/%s/projects/project-id/zones/us-central1-b/healthChecks/%s"
	alphaHcLink := fmt.Sprintf(baseUrl, "alpha", "hc-link")
	needsHcUpdate = ensureHealthCheckLink(be, alphaHcLink)
	if !needsHcUpdate {
		t.Fatalf("Expected ensureHealthCheckLink for a new healthcheck link to return true, got %v", needsHcUpdate)
	}

	gaHcLink := fmt.Sprintf(baseUrl, "v1", "hc-link")
	needsHcUpdate = ensureHealthCheckLink(be, gaHcLink)
	if needsHcUpdate {
		t.Fatalf("Expected ensureHealthCheckLink for healthcheck with the same name to return false, got %v", needsHcUpdate)
	}
}
