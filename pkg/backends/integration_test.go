/*
Copyright 2018 The Kubernetes Authors.

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
	"testing"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/mock"
	compute "google.golang.org/api/compute/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/healthchecks"
	"k8s.io/ingress-gce/pkg/instances"
	"k8s.io/ingress-gce/pkg/test"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/legacy-cloud-providers/gce"
)

type Jig struct {
	fakeInstancePool instances.NodePool
	linker           Linker
	syncer           Syncer
	pool             Pool
}

func newTestJig(fakeGCE *gce.Cloud) *Jig {
	fakeHealthChecks := healthchecks.NewHealthChecker(fakeGCE, "/", defaultBackendSvc)
	fakeBackendPool := NewPool(fakeGCE, defaultNamer)

	fakeIGs := instances.NewFakeInstanceGroups(sets.NewString(), defaultNamer)
	fakeInstancePool := instances.NewNodePool(fakeIGs, defaultNamer, &test.FakeRecorderSource{}, utils.GetBasePath(fakeGCE))
	fakeInstancePool.Init(&instances.FakeZoneLister{Zones: []string{defaultZone}})

	// Add standard hooks for mocking update calls. Each test can set a different update hook if it chooses to.
	(fakeGCE.Compute().(*cloud.MockGCE)).MockAlphaBackendServices.UpdateHook = mock.UpdateAlphaBackendServiceHook
	(fakeGCE.Compute().(*cloud.MockGCE)).MockBetaBackendServices.UpdateHook = mock.UpdateBetaBackendServiceHook
	(fakeGCE.Compute().(*cloud.MockGCE)).MockBackendServices.UpdateHook = mock.UpdateBackendServiceHook
	(fakeGCE.Compute().(*cloud.MockGCE)).MockHealthChecks.UpdateHook = mock.UpdateHealthCheckHook
	(fakeGCE.Compute().(*cloud.MockGCE)).MockAlphaHealthChecks.UpdateHook = mock.UpdateAlphaHealthCheckHook
	(fakeGCE.Compute().(*cloud.MockGCE)).MockBetaHealthChecks.UpdateHook = mock.UpdateBetaHealthCheckHook

	return &Jig{
		fakeInstancePool: fakeInstancePool,
		linker:           NewInstanceGroupLinker(fakeInstancePool, fakeBackendPool),
		syncer:           NewBackendSyncer(fakeBackendPool, fakeHealthChecks, fakeGCE),
		pool:             fakeBackendPool,
	}
}

func TestBackendInstanceGroupClobbering(t *testing.T) {
	fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
	jig := newTestJig(fakeGCE)

	sp := utils.ServicePort{NodePort: 80, BackendNamer: defaultNamer}
	_, err := jig.fakeInstancePool.EnsureInstanceGroupsAndPorts(defaultNamer.InstanceGroup(), []int64{sp.NodePort})
	if err != nil {
		t.Fatalf("Did not expect error when ensuring IG for ServicePort %+v: %v", sp, err)
	}

	if err := jig.syncer.Sync([]utils.ServicePort{sp}); err != nil {
		t.Fatalf("Did not expect error when syncing backend with port %v", sp.NodePort)
	}
	if err := jig.linker.Link(sp, []GroupKey{{Zone: defaultZone}}); err != nil {
		t.Fatalf("Did not expect error when linking backend with port %v to groups", sp.NodePort)
	}

	be, err := fakeGCE.GetGlobalBackendService(defaultNamer.IGBackend(80))
	if err != nil {
		t.Fatalf("f.GetGlobalBackendService(defaultNamer.IGBackend(80)) = _, %v, want _, nil", err)
	}
	// Simulate another controller updating the same backend service with
	// a different instance group
	newGroups := []*compute.Backend{
		{Group: fmt.Sprintf("zones/%s/instanceGroups/%s", defaultZone, "k8s-ig-bar")},
		{Group: fmt.Sprintf("zones/%s/instanceGroups/%s", defaultZone, "k8s-ig-foo")},
	}
	be.Backends = append(be.Backends, newGroups...)
	if err = fakeGCE.UpdateGlobalBackendService(be); err != nil {
		t.Fatalf("Failed to update backend service %v", be.Name)
	}

	// Make sure repeated adds don't clobber the inserted instance group
	_, err = jig.fakeInstancePool.EnsureInstanceGroupsAndPorts(defaultNamer.InstanceGroup(), []int64{sp.NodePort})
	if err != nil {
		t.Fatalf("Did not expect error when ensuring IG for ServicePort %+v: %v", sp, err)
	}

	if err := jig.syncer.Sync([]utils.ServicePort{sp}); err != nil {
		t.Fatalf("Did not expect error when syncing backend with port %v", sp.NodePort)
	}
	if err := jig.linker.Link(sp, []GroupKey{{Zone: defaultZone}}); err != nil {
		t.Fatalf("Did not expect error when linking backend with port %v to groups", sp.NodePort)
	}

	be, err = fakeGCE.GetGlobalBackendService(defaultNamer.IGBackend(80))
	if err != nil {
		t.Fatalf("%v", err)
	}
	gotGroups := sets.NewString()
	for _, g := range be.Backends {
		igPath, err := utils.ResourcePath(g.Group)
		if err != nil {
			t.Fatalf("%v", err)
		}
		gotGroups.Insert(igPath)
	}

	// seed expectedGroups with the first group native to this controller
	expectedGroups := sets.NewString(fmt.Sprintf("zones/%s/instanceGroups/%s", defaultZone, "k8s-ig--uid1"))
	for _, newGroup := range newGroups {
		igPath, err := utils.ResourcePath(newGroup.Group)
		if err != nil {
			t.Fatalf("%v", err)
		}
		expectedGroups.Insert(igPath)
	}
	if !expectedGroups.Equal(gotGroups) {
		t.Fatalf("Expected %v Got %v", expectedGroups, gotGroups)
	}
}

func TestSyncChaosMonkey(t *testing.T) {
	fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
	jig := newTestJig(fakeGCE)

	sp := utils.ServicePort{NodePort: 8080, Protocol: annotations.ProtocolHTTP, BackendNamer: defaultNamer}

	_, err := jig.fakeInstancePool.EnsureInstanceGroupsAndPorts(defaultNamer.InstanceGroup(), []int64{sp.NodePort})
	if err != nil {
		t.Fatalf("Did not expect error when ensuring IG for ServicePort %+v, err %v", sp, err)
	}

	if err := jig.syncer.Sync([]utils.ServicePort{sp}); err != nil {
		t.Fatalf("Did not expect error when syncing backend with port %v, err: %v", sp.NodePort, err)
	}
	if err := jig.linker.Link(sp, []GroupKey{{Zone: defaultZone}}); err != nil {
		t.Fatalf("Did not expect error when linking backend with port %v to groups, err: %v", sp.NodePort, err)
	}

	beName := sp.BackendName()
	be, err := fakeGCE.GetGlobalBackendService(beName)
	if err != nil {
		t.Fatalf("%v", err)
	}

	// Mess up the link between backend service and instance group.
	// This simulates a user doing foolish things through the UI.
	be.Backends = []*compute.Backend{
		{Group: "zones/us-central1-c/instanceGroups/edge-hop-test"},
	}
	fakeGCE.UpdateGlobalBackendService(be)

	// Add hook to keep track of how many calls are made.
	// TODO(rramkumar): This is a hack. Implement function call counters in generated code.
	createCalls := 0
	(fakeGCE.Compute().(*cloud.MockGCE)).MockBackendServices.InsertHook = func(ctx context.Context, key *meta.Key, obj *compute.BackendService, m *cloud.MockBackendServices) (bool, error) {
		createCalls += 1
		return false, nil
	}

	_, err = jig.fakeInstancePool.EnsureInstanceGroupsAndPorts(defaultNamer.InstanceGroup(), []int64{sp.NodePort})
	if err != nil {
		t.Fatalf("Did not expect error when ensuring IG for ServicePort %+v: %v", sp, err)
	}

	if err := jig.syncer.Sync([]utils.ServicePort{sp}); err != nil {
		t.Fatalf("Did not expect error when syncing backend with port %v", sp.NodePort)
	}
	if err := jig.linker.Link(sp, []GroupKey{{Zone: defaultZone}}); err != nil {
		t.Fatalf("Did not expect error when linking backend with port %v to groups", sp.NodePort)
	}
	if createCalls > 0 {
		t.Fatalf("Unexpected create for existing backend service")
	}

	gotBackend, err := fakeGCE.GetGlobalBackendService(beName)
	if err != nil {
		t.Fatalf("Failed to find a backend with name %v: %v", beName, err)
	}
	gotGroup, err := jig.fakeInstancePool.Get(defaultNamer.InstanceGroup(), defaultZone)
	if err != nil {
		t.Fatalf("Failed to find instance group %v", defaultNamer.InstanceGroup())
	}
	groupPath, err := utils.RelativeResourceName(gotGroup.SelfLink)
	if err != nil {
		t.Fatalf("Failed to get resource path from %q", gotGroup.SelfLink)
	}

	for _, be := range gotBackend.Backends {
		if be.Group == groupPath {
			return
		}
	}
	t.Fatalf("Failed to find %q in backend groups %+v", groupPath, be.Backends)
}
