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
	"strings"
	"testing"

	computealpha "google.golang.org/api/compute/v0.alpha"
	compute "google.golang.org/api/compute/v1"
	"google.golang.org/api/googleapi"
	api_v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/kubernetes/pkg/cloudprovider/providers/gce"
	"k8s.io/kubernetes/pkg/cloudprovider/providers/gce/cloud"
	"k8s.io/kubernetes/pkg/cloudprovider/providers/gce/cloud/meta"
	"k8s.io/kubernetes/pkg/cloudprovider/providers/gce/cloud/mock"

	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/healthchecks"
	"k8s.io/ingress-gce/pkg/instances"
	"k8s.io/ingress-gce/pkg/neg"
	"k8s.io/ingress-gce/pkg/storage"
	"k8s.io/ingress-gce/pkg/utils"
)

const defaultZone = "zone-a"

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

func newTestJig(gce *gce.GCECloud, fakeIGs instances.InstanceGroups, syncWithCloud bool) (*Backends, healthchecks.HealthCheckProvider) {
	negGetter := neg.NewFakeNetworkEndpointGroupCloud("test-subnetwork", "test-network")
	nodePool := instances.NewNodePool(fakeIGs, defaultNamer)
	nodePool.Init(&instances.FakeZoneLister{Zones: []string{defaultZone}})
	healthCheckProvider := healthchecks.NewFakeHealthCheckProvider()
	healthChecks := healthchecks.NewHealthChecker(healthCheckProvider, "/", "/healthz", defaultNamer, defaultBackendSvc)
	bp := NewBackendPool(gce, negGetter, healthChecks, nodePool, defaultNamer, syncWithCloud)
	probes := map[utils.ServicePort]*api_v1.Probe{{NodePort: 443, Protocol: annotations.ProtocolHTTPS}: existingProbe}
	bp.Init(NewFakeProbeProvider(probes))

	// Add standard hooks for mocking update calls. Each test can set a different update hook if it chooses to.
	(gce.Compute().(*cloud.MockGCE)).MockAlphaBackendServices.UpdateHook = mock.UpdateAlphaBackendServiceHook
	(gce.Compute().(*cloud.MockGCE)).MockBackendServices.UpdateHook = mock.UpdateBackendServiceHook

	return bp, healthCheckProvider
}

func TestBackendPoolAdd(t *testing.T) {
	fakeGCE := gce.FakeGCECloud(gce.DefaultTestClusterValues())
	fakeIGs := instances.NewFakeInstanceGroups(sets.NewString(), defaultNamer)
	pool, _ := newTestJig(fakeGCE, fakeIGs, false)

	testCases := []utils.ServicePort{
		{NodePort: 80, Protocol: annotations.ProtocolHTTP},
		{NodePort: 443, Protocol: annotations.ProtocolHTTPS},
		{NodePort: 3000, Protocol: annotations.ProtocolHTTP2},
	}

	for _, sp := range testCases {
		// For simplicity, these tests use 80/443 as nodeports
		t.Run(fmt.Sprintf("Port:%v Protocol:%v", sp.NodePort, sp.Protocol), func(t *testing.T) {
			igs, err := pool.nodePool.EnsureInstanceGroupsAndPorts(defaultNamer.InstanceGroup(), []int64{sp.NodePort})
			if err != nil {
				t.Fatalf("Did not expect error when ensuring IG for ServicePort %+v: %v", sp, err)
			}
			// Add a backend for a port, then re-add the same port and
			// make sure it corrects a broken link from the backend to
			// the instance group.
			err = pool.Ensure([]utils.ServicePort{sp}, utils.IGLinks(igs))
			if err != nil {
				t.Fatalf("Did not expect error when ensuring a ServicePort %+v: %v", sp, err)
			}
			beName := sp.BackendName(defaultNamer)

			// Check that the new backend has the right port
			be, err := fakeGCE.GetGlobalBackendService(beName)
			if err != nil {
				t.Fatalf("Did not find expected backend %v", beName)
			}
			if be.Port != sp.NodePort {
				t.Fatalf("Backend %v has wrong port %v, expected %v", be.Name, be.Port, sp)
			}

			// Check that the instance group has the new port.
			ig, err := fakeIGs.GetInstanceGroup(defaultNamer.InstanceGroup(), defaultZone)
			if err != nil {
				t.Fatalf("Did not expect error when getting IG's: %v", err)
			}
			var found bool
			for _, port := range ig.NamedPorts {
				if port.Port == sp.NodePort {
					found = true
				}
			}
			if !found {
				t.Fatalf("Port %v not added to instance group", sp)
			}

			// Check the created healthcheck is the correct protocol
			isAlpha := sp.Protocol == annotations.ProtocolHTTP2
			hc, err := pool.healthChecker.Get(beName, isAlpha)
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

func TestBackendPoolAddWithoutWhitelist(t *testing.T) {
	fakeGCE := gce.FakeGCECloud(gce.DefaultTestClusterValues())
	fakeIGs := instances.NewFakeInstanceGroups(sets.NewString(), defaultNamer)
	pool, _ := newTestJig(fakeGCE, fakeIGs, false)

	sp := utils.ServicePort{NodePort: 3000, Protocol: annotations.ProtocolHTTP2}

	// Add hook to simulate the forbidden error (i.e no alpha whitelist).
	(fakeGCE.Compute().(*cloud.MockGCE)).MockAlphaBackendServices.InsertHook = mock.InsertAlphaBackendServiceUnauthorizedErrHook

	err := pool.Ensure([]utils.ServicePort{sp}, nil)
	if !utils.IsHTTPErrorCode(err, http.StatusForbidden) {
		t.Fatalf("Expected creating %+v through alpha API to be forbidden, got %v", sp, err)
	}
}

func TestHealthCheckMigration(t *testing.T) {
	fakeGCE := gce.FakeGCECloud(gce.DefaultTestClusterValues())
	fakeIGs := instances.NewFakeInstanceGroups(sets.NewString(), defaultNamer)
	pool, hcp := newTestJig(fakeGCE, fakeIGs, false)

	p := utils.ServicePort{NodePort: 7000, Protocol: annotations.ProtocolHTTP}
	beName := p.BackendName(defaultNamer)

	// Create a legacy health check and insert it into the HC provider.
	legacyHC := &compute.HttpHealthCheck{
		Name:               beName,
		RequestPath:        "/my-healthz-path",
		Host:               "k8s.io",
		Description:        "My custom HC",
		UnhealthyThreshold: 30,
		CheckIntervalSec:   40,
	}
	if err := hcp.CreateHttpHealthCheck(legacyHC); err != nil {
		t.Fatalf("unexpected error creating http health check %v", err)
	}

	// Verify legacy check exists
	legacyHC, err := hcp.GetHttpHealthCheck(beName)
	if err != nil {
		t.Fatalf("unexpected error getting http health check %v", err)
	}

	// Create backend service with expected name and link to legacy health check
	fakeGCE.CreateGlobalBackendService(&compute.BackendService{
		Name:         beName,
		HealthChecks: []string{legacyHC.SelfLink},
	})

	// Add the service port to the backend pool
	pool.Ensure([]utils.ServicePort{p}, nil)

	// Assert the proper health check was created
	hc, _ := pool.healthChecker.Get(beName, p.IsAlpha())
	if hc == nil || hc.Protocol() != p.Protocol {
		t.Fatalf("Expected %s health check, received %v: ", p.Protocol, hc)
	}

	// Assert the newer health check has the legacy health check settings
	if hc.RequestPath != legacyHC.RequestPath ||
		hc.Host != legacyHC.Host ||
		hc.UnhealthyThreshold != legacyHC.UnhealthyThreshold ||
		hc.CheckIntervalSec != legacyHC.CheckIntervalSec ||
		hc.Description != legacyHC.Description {
		t.Fatalf("Expected newer health check to have identical settings to legacy health check. Legacy: %+v, New: %+v", legacyHC, hc)
	}
}

func TestBackendPoolUpdateHTTPS(t *testing.T) {
	fakeGCE := gce.FakeGCECloud(gce.DefaultTestClusterValues())
	fakeIGs := instances.NewFakeInstanceGroups(sets.NewString(), defaultNamer)
	pool, _ := newTestJig(fakeGCE, fakeIGs, false)

	p := utils.ServicePort{NodePort: 3000, Protocol: annotations.ProtocolHTTP}
	pool.Ensure([]utils.ServicePort{p}, nil)
	beName := p.BackendName(defaultNamer)

	be, err := fakeGCE.GetGlobalBackendService(beName)
	if err != nil {
		t.Fatalf("Unexpected err: %v", err)
	}

	if annotations.AppProtocol(be.Protocol) != p.Protocol {
		t.Fatalf("Expected scheme %v but got %v", p.Protocol, be.Protocol)
	}

	// Assert the proper health check was created
	hc, _ := pool.healthChecker.Get(beName, p.IsAlpha())
	if hc == nil || hc.Protocol() != p.Protocol {
		t.Fatalf("Expected %s health check, received %v: ", p.Protocol, hc)
	}

	// Update service port to encrypted
	p.Protocol = annotations.ProtocolHTTPS
	pool.Ensure([]utils.ServicePort{p}, nil)

	be, err = fakeGCE.GetGlobalBackendService(beName)
	if err != nil {
		t.Fatalf("Unexpected err retrieving backend service after update: %v", err)
	}

	// Assert the backend has the correct protocol
	if annotations.AppProtocol(be.Protocol) != p.Protocol {
		t.Fatalf("Expected scheme %v but got %v", p.Protocol, annotations.AppProtocol(be.Protocol))
	}

	// Assert the proper health check was created
	hc, _ = pool.healthChecker.Get(beName, p.IsAlpha())
	if hc == nil || hc.Protocol() != p.Protocol {
		t.Fatalf("Expected %s health check, received %v: ", p.Protocol, hc)
	}
}

func TestBackendPoolUpdateHTTP2(t *testing.T) {
	fakeGCE := gce.FakeGCECloud(gce.DefaultTestClusterValues())
	fakeIGs := instances.NewFakeInstanceGroups(sets.NewString(), defaultNamer)
	pool, _ := newTestJig(fakeGCE, fakeIGs, false)

	p := utils.ServicePort{NodePort: 3000, Protocol: annotations.ProtocolHTTP}
	pool.Ensure([]utils.ServicePort{p}, nil)
	beName := p.BackendName(defaultNamer)

	be, err := fakeGCE.GetGlobalBackendService(beName)
	if err != nil {
		t.Fatalf("Unexpected err: %v", err)
	}

	if annotations.AppProtocol(be.Protocol) != p.Protocol {
		t.Fatalf("Expected scheme %v but got %v", p.Protocol, be.Protocol)
	}

	// Assert the proper health check was created
	hc, _ := pool.healthChecker.Get(beName, p.IsAlpha())
	if hc == nil || hc.Protocol() != p.Protocol {
		t.Fatalf("Expected %s health check, received %v: ", p.Protocol, hc)
	}

	// Update service port to HTTP2
	p.Protocol = annotations.ProtocolHTTP2
	pool.Ensure([]utils.ServicePort{p}, nil)

	beAlpha, err := fakeGCE.GetAlphaGlobalBackendService(beName)
	if err != nil {
		t.Fatalf("Unexpected err retrieving backend service after update: %v", err)
	}

	// Assert the backend has the correct protocol
	if annotations.AppProtocol(beAlpha.Protocol) != p.Protocol {
		t.Fatalf("Expected scheme %v but got %v", p.Protocol, annotations.AppProtocol(beAlpha.Protocol))
	}

	// Assert the proper health check was created
	hc, _ = pool.healthChecker.Get(beName, true)
	if hc == nil || hc.Protocol() != p.Protocol {
		t.Fatalf("Expected %s health check, received %v: ", p.Protocol, hc)
	}
}

func TestBackendPoolUpdateHTTP2WithoutWhitelist(t *testing.T) {
	fakeGCE := gce.FakeGCECloud(gce.DefaultTestClusterValues())
	fakeIGs := instances.NewFakeInstanceGroups(sets.NewString(), defaultNamer)
	pool, _ := newTestJig(fakeGCE, fakeIGs, false)

	p := utils.ServicePort{NodePort: 3000, Protocol: annotations.ProtocolHTTP}
	pool.Ensure([]utils.ServicePort{p}, nil)
	beName := p.BackendName(defaultNamer)

	be, err := fakeGCE.GetGlobalBackendService(beName)
	if err != nil {
		t.Fatalf("Unexpected err: %v", err)
	}

	if annotations.AppProtocol(be.Protocol) != p.Protocol {
		t.Fatalf("Expected scheme %v but got %v", p.Protocol, be.Protocol)
	}

	// Add hook to simulate the forbidden error (i.e no alpha whitelist).
	(fakeGCE.Compute().(*cloud.MockGCE)).MockAlphaBackendServices.UpdateHook = mock.UpdateAlphaBackendServiceUnauthorizedErrHook

	// Update service port to HTTP2
	p.Protocol = annotations.ProtocolHTTP2
	err = pool.Ensure([]utils.ServicePort{p}, nil)

	if !utils.IsHTTPErrorCode(err, http.StatusForbidden) {
		t.Fatalf("Expected getting %+v through alpha API to be forbidden, got %v", p, err)
	}
}

func TestBackendPoolChaosMonkey(t *testing.T) {
	fakeGCE := gce.FakeGCECloud(gce.DefaultTestClusterValues())
	fakeIGs := instances.NewFakeInstanceGroups(sets.NewString(), defaultNamer)
	pool, _ := newTestJig(fakeGCE, fakeIGs, false)

	sp := utils.ServicePort{NodePort: 8080, Protocol: annotations.ProtocolHTTP}
	igs, err := pool.nodePool.EnsureInstanceGroupsAndPorts(defaultNamer.InstanceGroup(), []int64{sp.NodePort})
	if err != nil {
		t.Fatalf("Did not expect error when ensuring IG for ServicePort %+v: %v", sp, err)
	}
	pool.Ensure([]utils.ServicePort{sp}, utils.IGLinks(igs))
	beName := sp.BackendName(defaultNamer)

	be, _ := fakeGCE.GetGlobalBackendService(beName)

	// Mess up the link between backend service and instance group.
	// This simulates a user doing foolish things through the UI.
	be.Backends = []*compute.Backend{
		{Group: "/zones/edge-hop-test"},
	}

	// Add hook to keep track of how many calls are made.
	// TODO(rramkumar): This is a hack. Implement function call counters in generated code.
	createCalls := 0
	(fakeGCE.Compute().(*cloud.MockGCE)).MockBackendServices.InsertHook = func(ctx context.Context, key *meta.Key, obj *compute.BackendService, m *cloud.MockBackendServices) (bool, error) {
		createCalls += 1
		return false, nil
	}

	fakeGCE.UpdateGlobalBackendService(be)

	igs, err = pool.nodePool.EnsureInstanceGroupsAndPorts(defaultNamer.InstanceGroup(), []int64{sp.NodePort})
	if err != nil {
		t.Fatalf("Did not expect error when ensuring IG for ServicePort %+v: %v", sp, err)
	}
	pool.Ensure([]utils.ServicePort{sp}, utils.IGLinks(igs))
	if createCalls > 0 {
		t.Fatalf("Unexpected create for existing backend service")
	}
	gotBackend, err := fakeGCE.GetGlobalBackendService(beName)
	if err != nil {
		t.Fatalf("Failed to find a backend with name %v: %v", beName, err)
	}
	gotGroup, err := fakeIGs.GetInstanceGroup(defaultNamer.InstanceGroup(), defaultZone)
	if err != nil {
		t.Fatalf("Failed to find instance group %v", defaultNamer.InstanceGroup())
	}
	backendLinks := sets.NewString()
	for _, be := range gotBackend.Backends {
		backendLinks.Insert(be.Group)
	}
	if !backendLinks.Has(gotGroup.SelfLink) {
		t.Fatalf(
			"Broken instance group link, got: %+v expected: %v",
			backendLinks.List(),
			gotGroup.SelfLink)
	}
}

func TestBackendPoolSync(t *testing.T) {
	// Call sync on a backend pool with a list of ports, make sure the pool
	// creates/deletes required ports.
	svcNodePorts := []utils.ServicePort{{NodePort: 81, Protocol: annotations.ProtocolHTTP}, {NodePort: 82, Protocol: annotations.ProtocolHTTPS}, {NodePort: 83, Protocol: annotations.ProtocolHTTP}}
	fakeGCE := gce.FakeGCECloud(gce.DefaultTestClusterValues())
	fakeIGs := instances.NewFakeInstanceGroups(sets.NewString(), defaultNamer)
	pool, _ := newTestJig(fakeGCE, fakeIGs, true)
	pool.Ensure([]utils.ServicePort{svcNodePorts[0]}, nil)
	pool.Ensure([]utils.ServicePort{svcNodePorts[1]}, nil)
	if err := pool.Ensure(svcNodePorts, nil); err != nil {
		t.Errorf("Expected backend pool to add node ports, err: %v", err)
	}
	if err := pool.GC(svcNodePorts); err != nil {
		t.Errorf("Expected backend pool to GC, err: %v", err)
	}
	if _, err := pool.Get(defaultNamer.IGBackend(90), ""); err == nil {
		t.Fatalf("Did not expect to find port 90")
	}
	for _, port := range svcNodePorts {
		if _, err := pool.Get(port.BackendName(defaultNamer), port.Version()); err != nil {
			t.Fatalf("Expected to find port %v", port)
		}
	}

	deletedPorts := []utils.ServicePort{svcNodePorts[1], svcNodePorts[2]}
	svcNodePorts = []utils.ServicePort{svcNodePorts[0]}
	if err := pool.GC(svcNodePorts); err != nil {
		t.Fatalf("Expected backend pool to GC, err: %v", err)
	}

	for _, port := range deletedPorts {
		if _, err := pool.Get(port.BackendName(defaultNamer), port.Version()); err == nil {
			t.Fatalf("Pool contains %v after deletion", port)
		}
	}

	// All these backends should be ignored because they don't belong to the cluster.
	// foo - non k8s managed backend
	// k8s-be-foo - foo is not a nodeport
	// k8s--bar--foo - too many cluster delimiters
	// k8s-be-3001--uid - another cluster tagged with uid
	unrelatedBackends := sets.NewString([]string{"foo", "k8s-be-foo", "k8s--bar--foo", "k8s-be-30001--uid"}...)
	for _, name := range unrelatedBackends.List() {
		fakeGCE.CreateGlobalBackendService(&compute.BackendService{Name: name})
	}

	// This backend should get deleted again since it is managed by this cluster.
	fakeGCE.CreateGlobalBackendService(&compute.BackendService{Name: deletedPorts[0].BackendName(defaultNamer)})

	// TODO: Avoid casting.
	// Repopulate the pool with a cloud list, which now includes the 82 port
	// backend. This would happen if, say, an ingress backend is removed
	// while the controller is restarting.
	pool.snapshotter.(*storage.CloudListingPool).ReplenishPool()

	pool.GC(svcNodePorts)

	currBackends, _ := fakeGCE.ListGlobalBackendServices()
	currSet := sets.NewString()
	for _, b := range currBackends {
		currSet.Insert(b.Name)
	}
	// Port 81 still exists because it's an in-use service NodePort.
	knownBe := defaultNamer.IGBackend(81)
	if !currSet.Has(knownBe) {
		t.Fatalf("Expected %v to exist in backend pool", knownBe)
	}
	currSet.Delete(knownBe)
	if !currSet.Equal(unrelatedBackends) {
		t.Fatalf("Some unrelated backends were deleted. Expected %+v, got %+v", unrelatedBackends, currSet)
	}
}

func TestBackendPoolSyncNEG(t *testing.T) {
	// Convert a BackendPool from non-NEG to NEG.
	// Expect the old BackendServices to be GC'ed
	svcPort := utils.ServicePort{NodePort: 81, Protocol: annotations.ProtocolHTTP}
	fakeGCE := gce.FakeGCECloud(gce.DefaultTestClusterValues())
	fakeIGs := instances.NewFakeInstanceGroups(sets.NewString(), defaultNamer)
	pool, _ := newTestJig(fakeGCE, fakeIGs, true)
	if err := pool.Ensure([]utils.ServicePort{svcPort}, nil); err != nil {
		t.Errorf("Expected backend pool to add node ports, err: %v", err)
	}

	nodePortName := svcPort.BackendName(defaultNamer)
	_, err := fakeGCE.GetGlobalBackendService(nodePortName)
	if err != nil {
		t.Fatalf("Failed to get backend service: %v", err)
	}

	// Convert to NEG
	svcPort.NEGEnabled = true
	if err := pool.Ensure([]utils.ServicePort{svcPort}, nil); err != nil {
		t.Errorf("Expected backend pool to add node ports, err: %v", err)
	}

	negName := svcPort.BackendName(defaultNamer)
	_, err = fakeGCE.GetGlobalBackendService(negName)
	if err != nil {
		t.Fatalf("Failed to get backend service with name %v: %v", negName, err)
	}
	// GC should garbage collect the Backend on the old naming schema
	pool.GC([]utils.ServicePort{svcPort})

	bs, err := fakeGCE.GetGlobalBackendService(nodePortName)
	if err == nil {
		t.Fatalf("Expected not to get BackendService with name %v, got: %+v", nodePortName, bs)
	}

	// Convert back to non-NEG
	svcPort.NEGEnabled = false
	if err := pool.Ensure([]utils.ServicePort{svcPort}, nil); err != nil {
		t.Errorf("Expected backend pool to add node ports, err: %v", err)
	}

	pool.GC([]utils.ServicePort{svcPort})

	_, err = fakeGCE.GetGlobalBackendService(nodePortName)
	if err != nil {
		t.Fatalf("Failed to get backend service with name %v: %v", nodePortName, err)
	}
}

func TestBackendPoolSyncQuota(t *testing.T) {
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
		// Need to fill the SvcTargetPort field on ServicePort to make sure
		// NEG Backend naming is unique
		{
			[]utils.ServicePort{{NodePort: 8080}, {NodePort: 443}},
			[]utils.ServicePort{
				{NodePort: 8080, SvcTargetPort: "testport8080", NEGEnabled: true},
				{NodePort: 443, SvcTargetPort: "testport443", NEGEnabled: true},
			},
			true,
			"Same port converted to NEG, plus one new NEG port",
		},
		{
			[]utils.ServicePort{
				{NodePort: 80, SvcTargetPort: "testport80", NEGEnabled: true},
				{NodePort: 90, SvcTargetPort: "testport90"},
			},
			[]utils.ServicePort{
				{NodePort: 80, SvcTargetPort: "testport80"},
				{NodePort: 90, SvcTargetPort: "testport90", NEGEnabled: true},
			},
			true,
			"Mixed NEG and non-NEG ports",
		},
		{
			[]utils.ServicePort{
				{NodePort: 100, SvcTargetPort: "testport100", NEGEnabled: true},
				{NodePort: 110, SvcTargetPort: "testport110", NEGEnabled: true},
				{NodePort: 120, SvcTargetPort: "testport120", NEGEnabled: true},
			},
			[]utils.ServicePort{
				{NodePort: 100, SvcTargetPort: "testport100"},
				{NodePort: 110, SvcTargetPort: "testport110"},
				{NodePort: 120, SvcTargetPort: "testport120"},
			},
			true,
			"Same ports as NEG, then non-NEG",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			fakeGCE := gce.FakeGCECloud(gce.DefaultTestClusterValues())
			fakeIGs := instances.NewFakeInstanceGroups(sets.NewString(), defaultNamer)
			pool, _ := newTestJig(fakeGCE, fakeIGs, false)

			bsCreated := 0
			quota := len(tc.newPorts)

			// Add hooks to simulate quota changes & errors.
			(fakeGCE.Compute().(*cloud.MockGCE)).MockBackendServices.InsertHook = func(ctx context.Context, key *meta.Key, be *compute.BackendService, m *cloud.MockBackendServices) (bool, error) {
				if bsCreated+1 > quota {
					return true, &googleapi.Error{Code: http.StatusForbidden, Body: be.Name}
				}
				bsCreated += 1
				return false, nil
			}
			(fakeGCE.Compute().(*cloud.MockGCE)).MockBackendServices.DeleteHook = func(ctx context.Context, key *meta.Key, m *cloud.MockBackendServices) (bool, error) {
				bsCreated -= 1
				return false, nil
			}

			if err := pool.Ensure(tc.oldPorts, nil); err != nil {
				t.Errorf("Expected backend pool to add node ports, err: %v", err)
			}

			// Ensuring these ports again without first Garbage Collecting goes over
			// the set quota. Expect an error here, until GC is called.
			err := pool.Ensure(tc.newPorts, nil)
			if tc.expectSyncErr && err == nil {
				t.Errorf("Expect initial sync to go over quota, but received no error")
			}

			pool.GC(tc.newPorts)
			if err := pool.Ensure(tc.newPorts, nil); err != nil {
				t.Errorf("Expected backend pool to add node ports, err: %v", err)
			}

			if bsCreated != quota {
				t.Errorf("Expected to create %v BackendServices, got: %v", quota, bsCreated)
			}
		})
	}
}

func TestBackendPoolDeleteLegacyHealthChecks(t *testing.T) {
	fakeGCE := gce.FakeGCECloud(gce.DefaultTestClusterValues())
	fakeIGs := instances.NewFakeInstanceGroups(sets.NewString(), defaultNamer)
	negGetter := neg.NewFakeNetworkEndpointGroupCloud("test-subnetwork", "test-network")
	nodePool := instances.NewNodePool(fakeIGs, defaultNamer)
	nodePool.Init(&instances.FakeZoneLister{Zones: []string{defaultZone}})
	hcp := healthchecks.NewFakeHealthCheckProvider()
	healthChecks := healthchecks.NewHealthChecker(hcp, "/", "/healthz", defaultNamer, defaultBackendSvc)
	bp := NewBackendPool(fakeGCE, negGetter, healthChecks, nodePool, defaultNamer, false)
	probes := map[utils.ServicePort]*api_v1.Probe{}
	bp.Init(NewFakeProbeProvider(probes))

	// Create a legacy HTTP health check
	beName := defaultNamer.IGBackend(80)
	if err := hcp.CreateHttpHealthCheck(&compute.HttpHealthCheck{
		Name: beName,
		Port: 80,
	}); err != nil {
		t.Fatalf("unexpected error creating http health check %v", err)
	}

	// Verify health check exists
	hc, err := hcp.GetHttpHealthCheck(beName)
	if err != nil {
		t.Fatalf("unexpected error getting http health check %v", err)
	}

	// Create backend service with expected name and link to legacy health check
	fakeGCE.CreateGlobalBackendService(&compute.BackendService{
		Name:         beName,
		HealthChecks: []string{hc.SelfLink},
	})

	// Have pool sync the above backend service
	bp.Ensure([]utils.ServicePort{{NodePort: 80, Protocol: annotations.ProtocolHTTPS}}, nil)

	// Verify the legacy health check has been deleted
	_, err = hcp.GetHttpHealthCheck(beName)
	if err == nil {
		t.Fatalf("expected error getting http health check %v", err)
	}

	// Verify a newer health check exists
	hcNew, err := hcp.GetHealthCheck(beName)
	if err != nil {
		t.Fatalf("unexpected error getting http health check %v", err)
	}

	// Verify the newer health check is of type HTTPS
	if hcNew.Type != string(annotations.ProtocolHTTPS) {
		t.Fatalf("expected health check type to be %v, actual %v", string(annotations.ProtocolHTTPS), hcNew.Type)
	}
}

func TestBackendPoolShutdown(t *testing.T) {
	fakeGCE := gce.FakeGCECloud(gce.DefaultTestClusterValues())
	fakeIGs := instances.NewFakeInstanceGroups(sets.NewString(), defaultNamer)
	pool, _ := newTestJig(fakeGCE, fakeIGs, false)

	// Add a backend-service and verify that it doesn't exist after Shutdown()
	pool.Ensure([]utils.ServicePort{{NodePort: 80}}, nil)
	pool.Shutdown()
	if _, err := fakeGCE.GetGlobalBackendService(defaultNamer.IGBackend(80)); err == nil {
		t.Fatalf("%v", err)
	}
}

func TestBackendInstanceGroupClobbering(t *testing.T) {
	fakeGCE := gce.FakeGCECloud(gce.DefaultTestClusterValues())
	fakeIGs := instances.NewFakeInstanceGroups(sets.NewString(), defaultNamer)
	pool, _ := newTestJig(fakeGCE, fakeIGs, false)

	sp := utils.ServicePort{NodePort: 80}
	igs, err := pool.nodePool.EnsureInstanceGroupsAndPorts(defaultNamer.InstanceGroup(), []int64{sp.NodePort})
	if err != nil {
		t.Fatalf("Did not expect error when ensuring IG for ServicePort %+v: %v", sp, err)
	}
	pool.Ensure([]utils.ServicePort{sp}, utils.IGLinks(igs))

	be, err := fakeGCE.GetGlobalBackendService(defaultNamer.IGBackend(80))
	if err != nil {
		t.Fatalf("f.GetGlobalBackendService(defaultNamer.IGBackend(80)) = _, %v, want _, nil", err)
	}
	// Simulate another controller updating the same backend service with
	// a different instance group
	newGroups := []*compute.Backend{
		{Group: fmt.Sprintf("/zones/%s/instanceGroups/%s", defaultZone, "k8s-ig-bar")},
		{Group: fmt.Sprintf("/zones/%s/instanceGroups/%s", defaultZone, "k8s-ig-foo")},
	}
	be.Backends = append(be.Backends, newGroups...)
	if err = fakeGCE.UpdateGlobalBackendService(be); err != nil {
		t.Fatalf("Failed to update backend service %v", be.Name)
	}

	// Make sure repeated adds don't clobber the inserted instance group
	igs, err = pool.nodePool.EnsureInstanceGroupsAndPorts(defaultNamer.InstanceGroup(), []int64{sp.NodePort})
	if err != nil {
		t.Fatalf("Did not expect error when ensuring IG for ServicePort %+v: %v", sp, err)
	}
	pool.Ensure([]utils.ServicePort{sp}, utils.IGLinks(igs))
	be, err = fakeGCE.GetGlobalBackendService(defaultNamer.IGBackend(80))
	if err != nil {
		t.Fatalf("%v", err)
	}
	gotGroups := sets.NewString()
	for _, g := range be.Backends {
		gotGroups.Insert(comparableGroupPath(g.Group))
	}

	// seed expectedGroups with the first group native to this controller
	expectedGroups := sets.NewString(fmt.Sprintf("/zones/%s/instanceGroups/%s", defaultZone, "k8s-ig--uid1"))
	for _, newGroup := range newGroups {
		expectedGroups.Insert(comparableGroupPath(newGroup.Group))
	}
	if !expectedGroups.Equal(gotGroups) {
		t.Fatalf("Expected %v Got %v", expectedGroups, gotGroups)
	}
}

func TestBackendCreateBalancingMode(t *testing.T) {
	fakeGCE := gce.FakeGCECloud(gce.DefaultTestClusterValues())
	fakeIGs := instances.NewFakeInstanceGroups(sets.NewString(), defaultNamer)
	pool, _ := newTestJig(fakeGCE, fakeIGs, false)
	sp := utils.ServicePort{NodePort: 8080, Protocol: annotations.ProtocolHTTP}
	modes := []BalancingMode{Rate, Utilization}

	// block the creation of Backends with the given balancingMode
	// and verify that a backend with the other balancingMode is
	// created
	for i, bm := range modes {
		(fakeGCE.Compute().(*cloud.MockGCE)).MockBackendServices.UpdateHook = func(ctx context.Context, key *meta.Key, be *compute.BackendService, m *cloud.MockBackendServices) error {
			for _, b := range be.Backends {
				if b.BalancingMode == string(bm) {
					return &googleapi.Error{Code: http.StatusBadRequest}
				}
			}
			return mock.UpdateBackendServiceHook(ctx, key, be, m)
		}

		igs, err := pool.nodePool.EnsureInstanceGroupsAndPorts(defaultNamer.InstanceGroup(), []int64{sp.NodePort})
		if err != nil {
			t.Fatalf("Did not expect error when ensuring IG for ServicePort %+v: %v", sp, err)
		}
		pool.Ensure([]utils.ServicePort{sp}, utils.IGLinks(igs))
		be, err := fakeGCE.GetGlobalBackendService(sp.BackendName(defaultNamer))
		if err != nil {
			t.Fatalf("%v", err)
		}

		if len(be.Backends) == 0 {
			t.Fatalf("Expected Backends to be created")
		}

		for _, b := range be.Backends {
			if b.BalancingMode != string(modes[(i+1)%len(modes)]) {
				t.Fatalf("Wrong balancing mode, expected %v got %v", modes[(i+1)%len(modes)], b.BalancingMode)
			}
		}
		pool.GC([]utils.ServicePort{})
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

func TestLinkBackendServiceToNEG(t *testing.T) {
	zones := []string{"zone1", "zone2"}
	namespace, name, port := "ns", "name", "port"
	fakeGCE := gce.FakeGCECloud(gce.DefaultTestClusterValues())
	fakeIGs := instances.NewFakeInstanceGroups(sets.NewString(), defaultNamer)
	fakeNEG := neg.NewFakeNetworkEndpointGroupCloud("test-subnetwork", "test-network")
	nodePool := instances.NewNodePool(fakeIGs, defaultNamer)
	nodePool.Init(&instances.FakeZoneLister{Zones: []string{defaultZone}})
	hcp := healthchecks.NewFakeHealthCheckProvider()
	healthChecks := healthchecks.NewHealthChecker(hcp, "/", "/healthz", defaultNamer, defaultBackendSvc)
	bp := NewBackendPool(fakeGCE, fakeNEG, healthChecks, nodePool, defaultNamer, false)

	// Add standard hooks for mocking update calls. Each test can set a update different hook if it chooses to.
	(fakeGCE.Compute().(*cloud.MockGCE)).MockAlphaBackendServices.UpdateHook = mock.UpdateAlphaBackendServiceHook
	(fakeGCE.Compute().(*cloud.MockGCE)).MockBackendServices.UpdateHook = mock.UpdateBackendServiceHook

	svcPort := utils.ServicePort{
		ID: utils.ServicePortID{
			Service: types.NamespacedName{
				Namespace: namespace,
				Name:      name,
			},
			Port: intstr.FromInt(80),
		},
		NodePort:      30001,
		Protocol:      annotations.ProtocolHTTP,
		SvcTargetPort: port,
		NEGEnabled:    true,
	}
	if err := bp.Ensure([]utils.ServicePort{svcPort}, nil); err != nil {
		t.Fatalf("Failed to ensure backend service: %v", err)
	}

	for _, zone := range zones {
		err := fakeNEG.CreateNetworkEndpointGroup(&computealpha.NetworkEndpointGroup{
			Name: defaultNamer.NEG(namespace, name, port),
		}, zone)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	if err := bp.Link(svcPort, zones); err != nil {
		t.Fatalf("Failed to link backend service to NEG: %v", err)
	}

	beName := svcPort.BackendName(defaultNamer)
	bs, err := fakeGCE.GetGlobalBackendService(beName)
	if err != nil {
		t.Fatalf("Failed to retrieve backend service: %v", err)
	}
	if len(bs.Backends) != len(zones) {
		t.Errorf("Expect %v backends, but got %v.", len(zones), len(bs.Backends))
	}

	for _, be := range bs.Backends {
		neg := "NetworkEndpointGroup"
		if !strings.Contains(be.Group, neg) {
			t.Errorf("Expect backend to be a NEG, but got %q", be.Group)
		}
	}
}

func TestRetrieveObjectName(t *testing.T) {
	testCases := []struct {
		url    string
		expect string
	}{
		{
			"",
			"",
		},
		{
			"a/b/c/d/",
			"",
		},
		{
			"a/b/c/d",
			"d",
		},
		{
			"compute",
			"compute",
		},
	}

	for _, tc := range testCases {
		if retrieveObjectName(tc.url) != tc.expect {
			t.Errorf("expect %q, but got %q", tc.expect, retrieveObjectName(tc.url))
		}
	}
}

func TestComparableGroupPath(t *testing.T) {
	testCases := []struct {
		igPath   string
		expected string
	}{
		{
			"https://www.googleapis.com/compute/beta/projects/project-id/zones/us-central1-a/instanceGroups/example-group",
			"/zones/us-central1-a/instanceGroups/example-group",
		},
		{
			"https://www.googleapis.com/compute/alpha/projects/project-id/zones/us-central1-b/instanceGroups/test-group",
			"/zones/us-central1-b/instanceGroups/test-group",
		},
		{
			"https://www.googleapis.com/compute/v1/projects/project-id/zones/us-central1-c/instanceGroups/another-group",
			"/zones/us-central1-c/instanceGroups/another-group",
		},
	}

	for _, tc := range testCases {
		if comparableGroupPath(tc.igPath) != tc.expected {
			t.Errorf("expected %s, but got %s", tc.expected, comparableGroupPath(tc.igPath))
		}
	}
}

func TestEnsureBackendServiceProtocol(t *testing.T) {
	fakeGCE := gce.FakeGCECloud(gce.DefaultTestClusterValues())
	fakeIGs := instances.NewFakeInstanceGroups(sets.NewString(), defaultNamer)
	pool, _ := newTestJig(fakeGCE, fakeIGs, false)

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
					pool.Ensure([]utils.ServicePort{oldPort}, nil)
					be, err := pool.Get(oldPort.BackendName(defaultNamer), oldPort.Version())
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
	fakeGCE := gce.FakeGCECloud(gce.DefaultTestClusterValues())
	fakeIGs := instances.NewFakeInstanceGroups(sets.NewString(), defaultNamer)
	pool, _ := newTestJig(fakeGCE, fakeIGs, false)

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
					pool.Ensure([]utils.ServicePort{oldPort}, nil)
					be, err := pool.Get(oldPort.BackendName(defaultNamer), oldPort.Version())
					if err != nil {
						t.Fatalf("%v", err)
					}

					needsDescriptionUpdate := ensureDescription(be, newPort.Description())
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
	fakeGCE := gce.FakeGCECloud(gce.DefaultTestClusterValues())
	fakeIGs := instances.NewFakeInstanceGroups(sets.NewString(), defaultNamer)
	pool, _ := newTestJig(fakeGCE, fakeIGs, false)

	p := utils.ServicePort{NodePort: 80, Protocol: annotations.ProtocolHTTP, ID: utils.ServicePortID{Port: intstr.FromInt(1)}}
	pool.Ensure([]utils.ServicePort{p}, nil)
	be, err := pool.Get(p.BackendName(defaultNamer), p.Version())
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
