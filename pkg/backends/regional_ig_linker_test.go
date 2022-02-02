/*
Copyright 2021 The Kubernetes Authors.
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
	"testing"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/mock"
	apiv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/ingress-gce/pkg/instances"
	"k8s.io/ingress-gce/pkg/test"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/legacy-cloud-providers/gce"
)

const (
	uscentralzone = "us-central1-a"
	hcLink        = "some_hc_link"
)

func linkerTestClusterValues() gce.TestClusterValues {
	return gce.TestClusterValues{
		ProjectID:   "mock-project",
		Region:      "us-central1",
		ZoneName:    "us-central1-a",
		ClusterID:   "test-cluster-id",
		ClusterName: "Test Cluster Name",
	}
}

func newTestRegionalIgLinker(fakeGCE *gce.Cloud, backendPool *Backends, l4Namer *namer.L4Namer) *RegionalInstanceGroupLinker {
	fakeIGs := instances.NewEmptyFakeInstanceGroups()
	fakeZL := &instances.FakeZoneLister{Zones: []string{uscentralzone}}
	fakeInstancePool := instances.NewNodePool(fakeIGs, l4Namer, &test.FakeRecorderSource{}, utils.GetBasePath(fakeGCE), fakeZL)

	(fakeGCE.Compute().(*cloud.MockGCE)).MockRegionBackendServices.UpdateHook = mock.UpdateRegionBackendServiceHook

	return &RegionalInstanceGroupLinker{fakeInstancePool, backendPool}
}

func TestRegionalLink(t *testing.T) {
	t.Parallel()
	fakeGCE := gce.NewFakeGCECloud(linkerTestClusterValues())
	clusterID, _ := fakeGCE.ClusterID.GetID()
	l4Namer := namer.NewL4Namer("uid1", namer.NewNamer(clusterID, ""))
	sp := utils.ServicePort{NodePort: 8080, BackendNamer: l4Namer}
	fakeBackendPool := NewPool(fakeGCE, l4Namer)
	linker := newTestRegionalIgLinker(fakeGCE, fakeBackendPool, l4Namer)

	if err := linker.Link(sp, fakeGCE.ProjectID(), []string{uscentralzone}); err == nil {
		t.Fatalf("Linking when instances does not exist should return error")
	}
	if _, err := linker.instancePool.EnsureInstanceGroupsAndPorts(l4Namer.InstanceGroup(), []int64{sp.NodePort}); err != nil {
		t.Fatalf("Unexpected error when ensuring IG for ServicePort %+v: %v", sp, err)
	}
	if err := linker.Link(sp, fakeGCE.ProjectID(), []string{uscentralzone}); err == nil {
		t.Fatalf("Linking when backend service does not exist should return error")
	}
	createBackendService(t, sp, fakeBackendPool)

	if err := linker.Link(sp, fakeGCE.ProjectID(), []string{uscentralzone}); err != nil {
		t.Fatalf("Unexpected error in Link. Error: %v", err)
	}

	be, err := fakeGCE.GetRegionBackendService(sp.BackendName(), fakeGCE.Region())
	if err != nil {
		t.Fatalf("Get Regional Backend Service failed %v", err)
	}
	if len(be.Backends) == 0 {
		t.Fatalf("Expected Backends to be created")
	}
	ig, _ := linker.instancePool.Get(sp.IGName(), uscentralzone)
	expectedLink, _ := utils.RelativeResourceName(ig.SelfLink)
	if be.Backends[0].Group != expectedLink {
		t.Fatalf("Expected Backend Group: %s received: %s", expectedLink, be.Backends[0].Group)
	}
}

func createBackendService(t *testing.T, sp utils.ServicePort, backendPool *Backends) {
	t.Helper()
	namespacedName := types.NamespacedName{Name: "service.Name", Namespace: "service.Namespace"}
	protocol := string(apiv1.ProtocolTCP)
	if _, err := backendPool.EnsureL4BackendService(sp.BackendName(), hcLink, protocol, string(apiv1.ServiceAffinityNone), string(cloud.SchemeExternal), namespacedName, meta.VersionGA); err != nil {
		t.Fatalf("Error creating backend service %v", err)
	}
}
