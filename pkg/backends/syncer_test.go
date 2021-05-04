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
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/googleapi"
	api_v1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/backends/features"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/healthchecks"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/legacy-cloud-providers/gce"
)

// portset helps keep track of service ports during GC tests
type portset struct {
	// all represents the set all of service ports in the test
	all map[utils.ServicePort]bool
	// existing represents what should exist in GCE
	existing map[utils.ServicePort]bool
}

func newPortset(ports []utils.ServicePort) *portset {
	ps := portset{all: map[utils.ServicePort]bool{}, existing: map[utils.ServicePort]bool{}}
	for _, sp := range ports {
		ps.all[sp] = true
	}
	return &ps
}

func (p *portset) existingPorts() []utils.ServicePort {
	var result []utils.ServicePort
	for sp, _ := range p.existing {
		result = append(result, sp)
	}
	return result
}

// Add to 'existing' from all
func (p *portset) add(ports []utils.ServicePort) error {
	for _, sp := range ports {
		// Sanity check
		if found := p.all[sp]; !found {
			return fmt.Errorf("%+v not found in p.all", sp)
		}
		p.existing[sp] = true
	}
	return nil
}

// Delete from 'existing'
func (p *portset) del(ports []utils.ServicePort) error {
	for _, sp := range ports {
		found := p.existing[sp]
		if !found {
			return fmt.Errorf("%+v not found in p.existing", sp)
		}
		delete(p.existing, sp)
	}
	return nil
}

// check() iterates through all and checks that the ports in 'existing' exist in gce, and that those
// that are not in 'existing' do not exist
func (p *portset) check(fakeGCE *gce.Cloud) error {
	for sp, _ := range p.all {
		_, found := p.existing[sp]
		beName := sp.BackendName()
		key, err := composite.CreateKey(fakeGCE, beName, features.ScopeFromServicePort(&sp))
		if err != nil {
			return fmt.Errorf("Error creating key for backend service %s: %v", beName, err)
		}

		if found {
			if _, err := composite.GetBackendService(fakeGCE, key, features.VersionFromServicePort(&sp)); err != nil {
				return fmt.Errorf("backend for port %+v should exist, but got: %v", sp.NodePort, err)
			}
		} else {
			bs, err := composite.GetBackendService(fakeGCE, key, features.VersionFromServicePort(&sp))
			if err == nil || !utils.IsHTTPErrorCode(err, http.StatusNotFound) {
				if sp.VMIPNEGEnabled {
					// It is expected that these Backends should not get cleaned up in the GC loop.
					continue
				}
				return fmt.Errorf("backend for port %+v should not exist, but got %v", sp, bs)
			}
		}
	}
	return nil
}

var (
	defaultNamer      = namer.NewNamer("uid1", "fw1")
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
	fakeHealthChecks := healthchecks.NewHealthChecker(fakeGCE, "/", defaultBackendSvc)

	fakeBackendPool := NewPool(fakeGCE, defaultNamer)

	syncer := &backendSyncer{
		backendPool:   fakeBackendPool,
		healthChecker: fakeHealthChecks,
		cloud:         fakeGCE,
	}

	probes := map[utils.ServicePort]*api_v1.Probe{{NodePort: 443, Protocol: annotations.ProtocolHTTPS, BackendNamer: defaultNamer}: existingProbe}
	syncer.Init(NewFakeProbeProvider(probes))

	// Add standard hooks for mocking update calls. Each test can set a different update hook if it chooses to.
	(fakeGCE.Compute().(*cloud.MockGCE)).MockAlphaBackendServices.UpdateHook = mock.UpdateAlphaBackendServiceHook
	(fakeGCE.Compute().(*cloud.MockGCE)).MockBetaBackendServices.UpdateHook = mock.UpdateBetaBackendServiceHook
	(fakeGCE.Compute().(*cloud.MockGCE)).MockBackendServices.UpdateHook = mock.UpdateBackendServiceHook
	(fakeGCE.Compute().(*cloud.MockGCE)).MockHealthChecks.UpdateHook = mock.UpdateHealthCheckHook
	(fakeGCE.Compute().(*cloud.MockGCE)).MockAlphaHealthChecks.UpdateHook = mock.UpdateAlphaHealthCheckHook
	(fakeGCE.Compute().(*cloud.MockGCE)).MockAlphaRegionHealthChecks.UpdateHook = mock.UpdateAlphaRegionHealthCheckHook
	(fakeGCE.Compute().(*cloud.MockGCE)).MockBetaHealthChecks.UpdateHook = mock.UpdateBetaHealthCheckHook

	return syncer
}

func TestSync(t *testing.T) {
	fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
	syncer := newTestSyncer(fakeGCE)

	testCases := []utils.ServicePort{
		{NodePort: 80, Protocol: annotations.ProtocolHTTP, BackendNamer: defaultNamer},
		// Note: 443 gets its healthcheck from a probe
		{NodePort: 443, Protocol: annotations.ProtocolHTTPS, BackendNamer: defaultNamer},
		{NodePort: 3000, Protocol: annotations.ProtocolHTTP2, BackendNamer: defaultNamer},
	}

	for _, sp := range testCases {
		t.Run(fmt.Sprintf("Port: %v Protocol: %v", sp.NodePort, sp.Protocol), func(t *testing.T) {
			if err := syncer.Sync([]utils.ServicePort{sp}); err != nil {
				t.Fatalf("Unexpected error when syncing backend with port %v: %v", sp.NodePort, err)
			}
			beName := sp.BackendName()

			// Check that the new backend has the right port
			be, err := syncer.backendPool.Get(beName, features.VersionFromServicePort(&sp), features.ScopeFromServicePort(&sp))
			if err != nil {
				t.Fatalf("Did not find expected backend with port %v", sp.NodePort)
			}
			if be.Port != sp.NodePort {
				t.Fatalf("Backend %v has wrong port %v, expected %v", be.Name, be.Port, sp)
			}

			hc, err := syncer.healthChecker.Get(beName, features.VersionFromServicePort(&sp), features.ScopeFromServicePort(&sp))
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

	p := utils.ServicePort{NodePort: 3000, Protocol: annotations.ProtocolHTTP, BackendNamer: defaultNamer}
	syncer.Sync([]utils.ServicePort{p})
	beName := p.BackendName()

	be, err := syncer.backendPool.Get(beName, features.VersionFromServicePort(&p), features.ScopeFromServicePort(&p))
	if err != nil {
		t.Fatalf("Unexpected err: %v", err)
	}

	if annotations.AppProtocol(be.Protocol) != p.Protocol {
		t.Fatalf("Expected scheme %v but got %v", p.Protocol, be.Protocol)
	}

	// Assert the proper health check was created
	hc, _ := syncer.healthChecker.Get(beName, features.VersionFromServicePort(&p), features.ScopeFromServicePort(&p))
	if hc == nil || hc.Protocol() != p.Protocol {
		t.Fatalf("Expected %s health check, received %v: ", p.Protocol, hc)
	}

	// Update service port to encrypted
	p.Protocol = annotations.ProtocolHTTPS
	syncer.Sync([]utils.ServicePort{p})

	be, err = syncer.backendPool.Get(beName, features.VersionFromServicePort(&p), features.ScopeFromServicePort(&p))
	if err != nil {
		t.Fatalf("Unexpected err retrieving backend service after update: %v", err)
	}

	// Assert the backend has the correct protocol
	if annotations.AppProtocol(be.Protocol) != p.Protocol {
		t.Fatalf("Expected scheme %v but got %v", p.Protocol, annotations.AppProtocol(be.Protocol))
	}

	// Assert the proper health check was created
	hc, _ = syncer.healthChecker.Get(beName, features.VersionFromServicePort(&p), features.ScopeFromServicePort(&p))
	if hc == nil || hc.Protocol() != p.Protocol {
		t.Fatalf("Expected %s health check, received %v: ", p.Protocol, hc)
	}
}

func TestSyncUpdateHTTP2(t *testing.T) {
	fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
	syncer := newTestSyncer(fakeGCE)

	p := utils.ServicePort{NodePort: 3000, Protocol: annotations.ProtocolHTTP, BackendNamer: defaultNamer}
	syncer.Sync([]utils.ServicePort{p})
	beName := p.BackendName()

	be, err := syncer.backendPool.Get(beName, features.VersionFromServicePort(&p), features.ScopeFromServicePort(&p))
	if err != nil {
		t.Fatalf("Unexpected err: %v", err)
	}

	if annotations.AppProtocol(be.Protocol) != p.Protocol {
		t.Fatalf("Expected scheme %v but got %v", p.Protocol, be.Protocol)
	}

	// Assert the proper health check was created
	hc, _ := syncer.healthChecker.Get(beName, features.VersionFromServicePort(&p), features.ScopeFromServicePort(&p))
	if hc == nil || hc.Protocol() != p.Protocol {
		t.Fatalf("Expected %s health check, received %v: ", p.Protocol, hc)
	}

	// Update service port to HTTP2
	p.Protocol = annotations.ProtocolHTTP2
	syncer.Sync([]utils.ServicePort{p})

	beBeta, err := syncer.backendPool.Get(beName, features.VersionFromServicePort(&p), features.ScopeFromServicePort(&p))
	if err != nil {
		t.Fatalf("Unexpected err retrieving backend service after update: %v", err)
	}

	// Assert the backend has the correct protocol
	if annotations.AppProtocol(beBeta.Protocol) != p.Protocol {
		t.Fatalf("Expected scheme %v but got %v", p.Protocol, annotations.AppProtocol(beBeta.Protocol))
	}

	// Assert the proper health check was created
	hc, _ = syncer.healthChecker.Get(beName, features.VersionFromServicePort(&p), features.ScopeFromServicePort(&p))
	if hc == nil || hc.Protocol() != p.Protocol {
		t.Fatalf("Expected %s health check, received %v: ", p.Protocol, hc)
	}
}

// Test GC with both ELB and ILBs
func TestGC(t *testing.T) {
	fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
	syncer := newTestSyncer(fakeGCE)

	svcNodePorts := []utils.ServicePort{
		{NodePort: 81, Protocol: annotations.ProtocolHTTP, BackendNamer: defaultNamer},
		{NodePort: 82, Protocol: annotations.ProtocolHTTPS, BackendNamer: defaultNamer},
		{NodePort: 83, Protocol: annotations.ProtocolHTTP, BackendNamer: defaultNamer},
	}
	ps := newPortset(svcNodePorts)
	if err := ps.add(svcNodePorts); err != nil {
		t.Fatal(err)
	}

	if err := syncer.Sync(ps.existingPorts()); err != nil {
		t.Fatalf("syncer.Sync(%+v) = %v, want nil ", ps.existingPorts(), err)
	}

	if err := ps.check(fakeGCE); err != nil {
		t.Fatal(err)
	}

	// Run a no-op GC (i.e nothing is actually cleaned up)
	if err := syncer.GC(ps.existingPorts()); err != nil {
		t.Fatalf("syncer.GC(%+v) = %v, want nil", ps.existingPorts(), err)
	}

	// Check that nothing was deleted
	if err := ps.check(fakeGCE); err != nil {
		t.Fatal(err)
	}

	if err := ps.del([]utils.ServicePort{svcNodePorts[1], svcNodePorts[2]}); err != nil {
		t.Fatal(err)
	}

	if err := syncer.GC(ps.existingPorts()); err != nil {
		t.Fatalf("syncer.GC(%+v) = %v, want nil", ps.existingPorts(), err)
	}

	if err := ps.check(fakeGCE); err != nil {
		t.Fatal(err)
	}
}

// Test GC with both ELB and ILBs. Add in an L4 ILB NEG which should not be deleted as part of GC.
func TestGCMixed(t *testing.T) {
	fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
	syncer := newTestSyncer(fakeGCE)

	svcNodePorts := []utils.ServicePort{
		{NodePort: 81, Protocol: annotations.ProtocolHTTP, BackendNamer: defaultNamer},
		{NodePort: 82, Protocol: annotations.ProtocolHTTPS, BackendNamer: defaultNamer},
		{NodePort: 83, Protocol: annotations.ProtocolHTTP, BackendNamer: defaultNamer},
		{NodePort: 84, Protocol: annotations.ProtocolHTTP, NEGEnabled: true, L7ILBEnabled: true, BackendNamer: defaultNamer},
		{NodePort: 85, Protocol: annotations.ProtocolHTTPS, NEGEnabled: true, L7ILBEnabled: true, BackendNamer: defaultNamer},
		{NodePort: 86, Protocol: annotations.ProtocolHTTP, NEGEnabled: true, L7ILBEnabled: true, BackendNamer: defaultNamer},
		{ID: utils.ServicePortID{Service: types.NamespacedName{Name: "testsvc"}}, VMIPNEGEnabled: true, BackendNamer: defaultNamer},
	}
	ps := newPortset(svcNodePorts)
	if err := ps.add(svcNodePorts); err != nil {
		t.Fatal(err)
	}

	if err := syncer.Sync(ps.existingPorts()); err != nil {
		t.Fatalf("syncer.Sync(%+v) = %v, want nil ", ps.existingPorts(), err)
	}

	if err := ps.check(fakeGCE); err != nil {
		t.Fatal(err)
	}

	// Run a no-op GC (i.e nothing is actually cleaned up)
	if err := syncer.GC(ps.existingPorts()); err != nil {
		t.Fatalf("syncer.GC(%+v) = %v, want nil", ps.existingPorts(), err)
	}

	// Check that nothing was deleted
	if err := ps.check(fakeGCE); err != nil {
		t.Fatal(err)
	}

	if err := ps.del([]utils.ServicePort{svcNodePorts[1], svcNodePorts[2]}); err != nil {
		t.Fatal(err)
	}

	if err := syncer.GC(ps.existingPorts()); err != nil {
		t.Fatalf("syncer.GC(%+v) = %v, want nil", ps.existingPorts(), err)
	}

	if err := ps.check(fakeGCE); err != nil {
		t.Fatal(err)
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
			[]utils.ServicePort{{NodePort: 8080, BackendNamer: defaultNamer}},
			[]utils.ServicePort{{NodePort: 8080, BackendNamer: defaultNamer}},
			false,
			"Same port",
		},
		{
			[]utils.ServicePort{{NodePort: 8080, BackendNamer: defaultNamer}},
			[]utils.ServicePort{{NodePort: 9000, BackendNamer: defaultNamer}},
			true,
			"Different port",
		},
		{
			[]utils.ServicePort{{NodePort: 8080, BackendNamer: defaultNamer}},
			[]utils.ServicePort{{NodePort: 8080, BackendNamer: defaultNamer}, {NodePort: 443, BackendNamer: defaultNamer}},
			false,
			"Same port plus additional port",
		},
		{
			[]utils.ServicePort{{NodePort: 8080, BackendNamer: defaultNamer}},
			[]utils.ServicePort{{NodePort: 3000, BackendNamer: defaultNamer}, {NodePort: 4000, BackendNamer: defaultNamer}, {NodePort: 5000, BackendNamer: defaultNamer}},
			true,
			"New set of ports not including the same port",
		},
		// Need to fill the TargetPort field on ServicePort to make sure
		// NEG Backend naming is unique
		{
			[]utils.ServicePort{{NodePort: 8080, BackendNamer: defaultNamer}, {NodePort: 443, BackendNamer: defaultNamer}},
			[]utils.ServicePort{
				{Port: 8080, NodePort: 8080, NEGEnabled: true, BackendNamer: defaultNamer},
				{Port: 443, NodePort: 443, NEGEnabled: true, BackendNamer: defaultNamer},
			},
			true,
			"Same port converted to NEG, plus one new NEG port",
		},
		{
			[]utils.ServicePort{
				{Port: 80, NodePort: 80, NEGEnabled: true, BackendNamer: defaultNamer},
				{Port: 90, NodePort: 90, BackendNamer: defaultNamer},
			},
			[]utils.ServicePort{
				{Port: 80, BackendNamer: defaultNamer},
				{Port: 90, NEGEnabled: true, BackendNamer: defaultNamer},
			},
			true,
			"Mixed NEG and non-NEG ports",
		},
		{
			[]utils.ServicePort{
				{Port: 100, NodePort: 100, NEGEnabled: true, BackendNamer: defaultNamer},
				{Port: 110, NodePort: 110, NEGEnabled: true, BackendNamer: defaultNamer},
				{Port: 120, NodePort: 120, NEGEnabled: true, BackendNamer: defaultNamer},
			},
			[]utils.ServicePort{
				{Port: 100, NodePort: 100, BackendNamer: defaultNamer},
				{Port: 110, NodePort: 110, BackendNamer: defaultNamer},
				{Port: 120, NodePort: 120, BackendNamer: defaultNamer},
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

	svcPort := utils.ServicePort{NodePort: 81, Protocol: annotations.ProtocolHTTP, BackendNamer: defaultNamer}
	if err := syncer.Sync([]utils.ServicePort{svcPort}); err != nil {
		t.Errorf("Expected backend pool to add node ports, err: %v", err)
	}

	nodePortName := svcPort.BackendName()
	_, err := fakeGCE.GetGlobalBackendService(nodePortName)
	if err != nil {
		t.Fatalf("Failed to get backend service: %v", err)
	}

	// Convert to NEG
	svcPort.NEGEnabled = true
	if err := syncer.Sync([]utils.ServicePort{svcPort}); err != nil {
		t.Errorf("Expected backend pool to add node ports, err: %v", err)
	}

	negName := svcPort.BackendName()
	_, err = fakeGCE.GetGlobalBackendService(negName)
	if err != nil {
		t.Fatalf("Failed to get backend service with name %v: %v", negName, err)
	}
	// GC should garbage collect the Backend on the old naming schema
	syncer.GC([]utils.ServicePort{svcPort})

	bs, err := syncer.backendPool.Get(nodePortName, features.VersionFromServicePort(&svcPort), features.ScopeFromServicePort(&svcPort))
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
	syncer.Sync([]utils.ServicePort{{NodePort: 80, BackendNamer: defaultNamer}})
	syncer.Shutdown()
	if _, err := fakeGCE.GetGlobalBackendService(defaultNamer.IGBackend(80)); err == nil {
		t.Fatalf("%v", err)
	}
}

func TestEnsureBackendServiceProtocol(t *testing.T) {
	fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
	syncer := newTestSyncer(fakeGCE)

	svcPorts := []utils.ServicePort{
		{NodePort: 80, Protocol: annotations.ProtocolHTTP, ID: utils.ServicePortID{Port: networkingv1.ServiceBackendPort{Number: 1}}, BackendNamer: defaultNamer},
		{NodePort: 443, Protocol: annotations.ProtocolHTTPS, ID: utils.ServicePortID{Port: networkingv1.ServiceBackendPort{Number: 2}}, BackendNamer: defaultNamer},
		{NodePort: 3000, Protocol: annotations.ProtocolHTTP2, ID: utils.ServicePortID{Port: networkingv1.ServiceBackendPort{Number: 3}}, BackendNamer: defaultNamer},
	}

	for _, oldPort := range svcPorts {
		for _, newPort := range svcPorts {
			t.Run(
				fmt.Sprintf("Updating Port:%v Protocol:%v to Port:%v Protocol:%v", oldPort.NodePort, oldPort.Protocol, newPort.NodePort, newPort.Protocol),
				func(t *testing.T) {
					syncer.Sync([]utils.ServicePort{oldPort})
					be, err := syncer.backendPool.Get(oldPort.BackendName(), features.VersionFromServicePort(&oldPort), features.ScopeFromServicePort(&oldPort))
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
		{NodePort: 80, Protocol: annotations.ProtocolHTTP, ID: utils.ServicePortID{Port: networkingv1.ServiceBackendPort{Number: 1}}, BackendNamer: defaultNamer},
		{NodePort: 443, Protocol: annotations.ProtocolHTTPS, ID: utils.ServicePortID{Port: networkingv1.ServiceBackendPort{Number: 2}}, BackendNamer: defaultNamer},
		{NodePort: 3000, Protocol: annotations.ProtocolHTTP2, ID: utils.ServicePortID{Port: networkingv1.ServiceBackendPort{Number: 3}}, BackendNamer: defaultNamer},
	}

	for _, oldPort := range svcPorts {
		for _, newPort := range svcPorts {
			t.Run(
				fmt.Sprintf("Updating Port:%v Protocol:%v to Port:%v Protocol:%v", oldPort.NodePort, oldPort.Protocol, newPort.NodePort, newPort.Protocol),
				func(t *testing.T) {
					syncer.Sync([]utils.ServicePort{oldPort})
					be, err := syncer.backendPool.Get(oldPort.BackendName(), features.VersionFromServicePort(&oldPort), features.ScopeFromServicePort(&oldPort))
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

	p := utils.ServicePort{NodePort: 80, Protocol: annotations.ProtocolHTTP, ID: utils.ServicePortID{Port: networkingv1.ServiceBackendPort{Number: 1}}, BackendNamer: defaultNamer}
	syncer.Sync([]utils.ServicePort{p})
	be, err := syncer.backendPool.Get(p.BackendName(), features.VersionFromServicePort(&p), features.ScopeFromServicePort(&p))
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
