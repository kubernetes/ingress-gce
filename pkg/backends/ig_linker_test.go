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
	"net/http"
	"testing"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/mock"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/googleapi"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/backends/features"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/instances"
	"k8s.io/ingress-gce/pkg/test"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/legacy-cloud-providers/gce"
)

const defaultZone = "zone-a"

func newTestIGLinker(fakeGCE *gce.Cloud, fakeInstancePool instances.NodePool) *instanceGroupLinker {
	fakeInstancePool.Init(&instances.FakeZoneLister{Zones: []string{defaultZone}})
	fakeBackendPool := NewPool(fakeGCE, defaultNamer)

	// Add standard hooks for mocking update calls. Each test can set a different update hook if it chooses to.
	(fakeGCE.Compute().(*cloud.MockGCE)).MockAlphaBackendServices.UpdateHook = mock.UpdateAlphaBackendServiceHook
	(fakeGCE.Compute().(*cloud.MockGCE)).MockBetaBackendServices.UpdateHook = mock.UpdateBetaBackendServiceHook
	(fakeGCE.Compute().(*cloud.MockGCE)).MockBackendServices.UpdateHook = mock.UpdateBackendServiceHook

	return &instanceGroupLinker{fakeInstancePool, fakeBackendPool}
}

func TestLink(t *testing.T) {
	fakeIGs := instances.NewFakeInstanceGroups(sets.NewString(), defaultNamer)
	fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
	fakeNodePool := instances.NewNodePool(fakeIGs, defaultNamer, &test.FakeRecorderSource{}, utils.GetBasePath(fakeGCE))
	linker := newTestIGLinker(fakeGCE, fakeNodePool)

	sp := utils.ServicePort{NodePort: 8080, Protocol: annotations.ProtocolHTTP, BackendNamer: defaultNamer}

	// Mimic the instance group being created
	if _, err := linker.instancePool.EnsureInstanceGroupsAndPorts(defaultNamer.InstanceGroup(), []int64{sp.NodePort}); err != nil {
		t.Fatalf("Did not expect error when ensuring IG for ServicePort %+v: %v", sp, err)
	}

	// Mimic the syncer creating the backend.
	linker.backendPool.Create(sp, "fake-health-check-link")

	if err := linker.Link(sp, []GroupKey{{Zone: defaultZone}}); err != nil {
		t.Fatalf("%v", err)
	}

	be, err := fakeGCE.GetGlobalBackendService(sp.BackendName())
	if err != nil {
		t.Fatalf("%v", err)
	}

	if len(be.Backends) == 0 {
		t.Fatalf("Expected Backends to be created")
	}
}

func TestLinkWithCreationModeError(t *testing.T) {
	fakeIGs := instances.NewFakeInstanceGroups(sets.NewString(), defaultNamer)
	fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
	fakeNodePool := instances.NewNodePool(fakeIGs, defaultNamer, &test.FakeRecorderSource{}, utils.GetBasePath(fakeGCE))
	linker := newTestIGLinker(fakeGCE, fakeNodePool)

	sp := utils.ServicePort{NodePort: 8080, Protocol: annotations.ProtocolHTTP, BackendNamer: defaultNamer}
	modes := []BalancingMode{Rate, Utilization}

	// block the update of Backends with the given balancingMode
	// and verify that a backend with the other balancingMode is
	// updated properly.
	for i, bm := range modes {
		(fakeGCE.Compute().(*cloud.MockGCE)).MockBackendServices.UpdateHook = func(ctx context.Context, key *meta.Key, be *compute.BackendService, m *cloud.MockBackendServices) error {
			for _, b := range be.Backends {
				if b.BalancingMode == string(bm) {
					return &googleapi.Error{Code: http.StatusBadRequest}
				}
			}
			return mock.UpdateBackendServiceHook(ctx, key, be, m)
		}

		// Mimic the instance group being created
		if _, err := linker.instancePool.EnsureInstanceGroupsAndPorts(defaultNamer.InstanceGroup(), []int64{sp.NodePort}); err != nil {
			t.Fatalf("Did not expect error when ensuring IG for ServicePort %+v: %v", sp, err)
		}

		// Mimic the syncer creating the backend.
		linker.backendPool.Create(sp, "fake-health-check-link")

		if err := linker.Link(sp, []GroupKey{{Zone: defaultZone}}); err != nil {
			t.Fatalf("%v", err)
		}

		be, err := fakeGCE.GetGlobalBackendService(sp.BackendName())
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
		linker.backendPool.Delete(sp.BackendName(), features.VersionFromServicePort(&sp), features.ScopeFromServicePort(&sp))
	}
}

func TestBackendsForIG(t *testing.T) {
	for _, tc := range []struct {
		name    string
		igLinks []string
		sp      utils.ServicePort

		want []*composite.Backend
	}{
		{
			name: "no backends",
		},
		{
			name:    "default",
			igLinks: []string{"backend1"},
			want: []*composite.Backend{
				{BalancingMode: "RATE", Group: "backend1", MaxRatePerInstance: 1},
			},
		},
		{
			name:    "multiple",
			igLinks: []string{"backend1", "backend2"},
			want: []*composite.Backend{
				{BalancingMode: "RATE", Group: "backend1", MaxRatePerInstance: 1},
				{BalancingMode: "RATE", Group: "backend2", MaxRatePerInstance: 1},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := backendsForIGs(tc.igLinks, Rate, &tc.sp)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("backendsForIGs(_), diff(-tc.want +got) = %s", diff)
			}
		})
	}
}
