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
	"strings"
	"testing"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/mock"
	apiv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/instancegroups"
	"k8s.io/ingress-gce/pkg/network"
	"k8s.io/ingress-gce/pkg/test"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/ingress-gce/pkg/utils/slice"
	"k8s.io/ingress-gce/pkg/utils/zonegetter"
	"k8s.io/klog/v2"
)

const (
	usCentral1AZone = "us-central1-a"
	usCentral1CZone = "us-central1-c"
	hcLink          = "some_hc_link"
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
	fakeIGs := instancegroups.NewEmptyFakeInstanceGroups()

	nodeInformer := zonegetter.FakeNodeInformer()
	fakeZoneGetter := zonegetter.NewFakeZoneGetter(nodeInformer)
	zonegetter.AddFakeNodes(fakeZoneGetter, usCentral1AZone, "test-instance1")
	zonegetter.AddFakeNodes(fakeZoneGetter, "us-central1-c", "test-instance2")

	fakeInstancePool := instancegroups.NewManager(&instancegroups.ManagerConfig{
		Cloud:      fakeIGs,
		Namer:      l4Namer,
		Recorders:  &test.FakeRecorderSource{},
		BasePath:   utils.GetBasePath(fakeGCE),
		ZoneGetter: fakeZoneGetter,
		MaxIGSize:  1000,
	}, klog.TODO())

	(fakeGCE.Compute().(*cloud.MockGCE)).MockRegionBackendServices.UpdateHook = mock.UpdateRegionBackendServiceHook

	return &RegionalInstanceGroupLinker{fakeInstancePool, backendPool, klog.TODO()}
}

func TestRegionalLink(t *testing.T) {
	t.Parallel()
	fakeGCE := gce.NewFakeGCECloud(linkerTestClusterValues())
	clusterID, _ := fakeGCE.ClusterID.GetID()
	l4Namer := namer.NewL4Namer("uid1", namer.NewNamer(clusterID, "", klog.TODO()))
	sp := utils.ServicePort{NodePort: 8080, BackendNamer: l4Namer}
	fakeBackendPool := NewPool(fakeGCE, l4Namer)
	linker := newTestRegionalIgLinker(fakeGCE, fakeBackendPool, l4Namer)

	if err := linker.Link(sp, fakeGCE.ProjectID(), []string{usCentral1AZone}); err == nil {
		t.Fatalf("Linking when instances does not exist should return error")
	}
	if _, err := linker.instancePool.EnsureInstanceGroupsAndPorts(l4Namer.InstanceGroup(), []int64{sp.NodePort}); err != nil {
		t.Fatalf("Unexpected error when ensuring IG for ServicePort %+v: %v", sp, err)
	}
	if err := linker.Link(sp, fakeGCE.ProjectID(), []string{usCentral1AZone}); err == nil {
		t.Fatalf("Linking when backend service does not exist should return error")
	}
	createBackendService(t, sp, fakeBackendPool)

	if err := linker.Link(sp, fakeGCE.ProjectID(), []string{usCentral1AZone}); err != nil {
		t.Fatalf("Unexpected error in Link. Error: %v", err)
	}

	be, err := fakeGCE.GetRegionBackendService(sp.BackendName(), fakeGCE.Region())
	if err != nil {
		t.Fatalf("Get Regional Backend Service failed %v", err)
	}
	if len(be.Backends) == 0 {
		t.Fatalf("Expected Backends to be created")
	}
	ig, _ := linker.instancePool.Get(sp.IGName(), usCentral1AZone)
	expectedLink, _ := utils.RelativeResourceName(ig.SelfLink)
	if be.Backends[0].Group != expectedLink {
		t.Fatalf("Expected Backend Group: %s received: %s", expectedLink, be.Backends[0].Group)
	}
}

func TestRegionalUpdateLink(t *testing.T) {
	t.Parallel()
	fakeGCE := gce.NewFakeGCECloud(linkerTestClusterValues())
	clusterID, _ := fakeGCE.ClusterID.GetID()
	l4Namer := namer.NewL4Namer("uid1", namer.NewNamer(clusterID, "", klog.TODO()))
	sp := utils.ServicePort{NodePort: 8080, BackendNamer: l4Namer}
	fakeBackendPool := NewPool(fakeGCE, l4Namer)
	linker := newTestRegionalIgLinker(fakeGCE, fakeBackendPool, l4Namer)
	if _, err := linker.instancePool.EnsureInstanceGroupsAndPorts(l4Namer.InstanceGroup(), []int64{sp.NodePort}); err != nil {
		t.Fatalf("Unexpected error when ensuring IG for ServicePort %+v: %v", sp, err)
	}
	createBackendService(t, sp, fakeBackendPool)

	if err := linker.Link(sp, fakeGCE.ProjectID(), []string{usCentral1AZone}); err != nil {
		t.Fatalf("Unexpected error in Link(_). Error: %v", err)
	}
	// Add error hook to check that Link function will not call Backend Service Update when no IG was added
	(fakeGCE.Compute().(*cloud.MockGCE)).MockRegionBackendServices.UpdateHook = test.UpdateRegionBackendServiceWithErrorHookUpdate

	if err := linker.Link(sp, fakeGCE.ProjectID(), []string{usCentral1AZone}); err != nil {
		t.Fatalf("Unexpected error in Link(_). Error: %v", err)
	}
	be, err := fakeGCE.GetRegionBackendService(sp.BackendName(), fakeGCE.Region())
	if err != nil {
		t.Fatalf("Get Regional Backend Service failed %v", err)
	}
	if len(be.Backends) == 0 {
		t.Fatalf("Expected Backends to be created")
	}
	ig, _ := linker.instancePool.Get(sp.IGName(), usCentral1AZone)
	expectedLink, _ := utils.RelativeResourceName(ig.SelfLink)
	if be.Backends[0].Group != expectedLink {
		t.Fatalf("Expected Backend Group: %s received: %s", expectedLink, be.Backends[0].Group)
	}
}

func TestRegionalUpdateLinkWithNewBackends(t *testing.T) {
	t.Parallel()
	fakeGCE := gce.NewFakeGCECloud(linkerTestClusterValues())
	clusterID, _ := fakeGCE.ClusterID.GetID()
	l4Namer := namer.NewL4Namer("uid1", namer.NewNamer(clusterID, "", klog.TODO()))
	sp := utils.ServicePort{NodePort: 8080, BackendNamer: l4Namer}
	fakeBackendPool := NewPool(fakeGCE, l4Namer)
	linker := newTestRegionalIgLinker(fakeGCE, fakeBackendPool, l4Namer)
	if _, err := linker.instancePool.EnsureInstanceGroupsAndPorts(l4Namer.InstanceGroup(), []int64{sp.NodePort}); err != nil {
		t.Fatalf("Unexpected error when ensuring IG for ServicePort %+v: %v", sp, err)
	}
	createBackendService(t, sp, fakeBackendPool)

	if err := linker.Link(sp, fakeGCE.ProjectID(), []string{usCentral1AZone}); err != nil {
		t.Fatalf("Unexpected error in Link(_). Error: %v", err)
	}

	if err := linker.Link(sp, fakeGCE.ProjectID(), []string{usCentral1AZone, usCentral1CZone}); err != nil {
		t.Fatalf("Unexpected error in Link(_). Error: %v", err)
	}
	be, err := fakeGCE.GetRegionBackendService(sp.BackendName(), fakeGCE.Region())
	if err != nil {
		t.Fatalf("Get Regional Backend Service failed %v", err)
	}
	var backendGroups []string
	for _, b := range be.Backends {
		backendGroups = append(backendGroups, b.Group)
	}
	if len(be.Backends) != 2 {
		t.Errorf("Expected that Backends are updated with the added zone with len=2 but got=%+v", strings.Join(backendGroups, ", "))
	}

	igA, _ := linker.instancePool.Get(sp.IGName(), usCentral1AZone)
	expectedLinkA, _ := utils.RelativeResourceName(igA.SelfLink)
	igC, _ := linker.instancePool.Get(sp.IGName(), usCentral1CZone)
	expectedLinkC, _ := utils.RelativeResourceName(igC.SelfLink)
	if !slice.ContainsString(backendGroups, expectedLinkA, nil) {
		t.Errorf("expected that BackendService backend contains %v, got=%v", expectedLinkA, backendGroups)
	}
	if !slice.ContainsString(backendGroups, expectedLinkC, nil) {
		t.Errorf("expected that BackendService backend contains %v, got=%v", expectedLinkC, backendGroups)
	}
}

func TestRegionalUpdateLinkWithRemovedBackends(t *testing.T) {
	t.Parallel()
	fakeGCE := gce.NewFakeGCECloud(linkerTestClusterValues())
	clusterID, _ := fakeGCE.ClusterID.GetID()
	l4Namer := namer.NewL4Namer("uid1", namer.NewNamer(clusterID, "", klog.TODO()))
	sp := utils.ServicePort{NodePort: 8080, BackendNamer: l4Namer}
	fakeBackendPool := NewPool(fakeGCE, l4Namer)
	linker := newTestRegionalIgLinker(fakeGCE, fakeBackendPool, l4Namer)
	if _, err := linker.instancePool.EnsureInstanceGroupsAndPorts(l4Namer.InstanceGroup(), []int64{sp.NodePort}); err != nil {
		t.Fatalf("Unexpected error when ensuring IG for ServicePort %+v: %v", sp, err)
	}
	createBackendService(t, sp, fakeBackendPool)

	if err := linker.Link(sp, fakeGCE.ProjectID(), []string{usCentral1AZone, usCentral1CZone}); err != nil {
		t.Fatalf("Unexpected error in Link(_). Error: %v", err)
	}

	if err := linker.Link(sp, fakeGCE.ProjectID(), []string{usCentral1AZone}); err != nil {
		t.Fatalf("Unexpected error in Link(_). Error: %v", err)
	}
	be, err := fakeGCE.GetRegionBackendService(sp.BackendName(), fakeGCE.Region())
	if err != nil {
		t.Fatalf("Get Regional Backend Service failed %v", err)
	}
	var backendGroups []string
	for _, b := range be.Backends {
		backendGroups = append(backendGroups, b.Group)
	}
	if len(be.Backends) != 1 {
		t.Errorf("Expected that Backends are updated with the added zone with len=1 but got=%+v", strings.Join(backendGroups, ", "))
	}

	igA, _ := linker.instancePool.Get(sp.IGName(), usCentral1AZone)
	expectedLinkA, _ := utils.RelativeResourceName(igA.SelfLink)
	igC, _ := linker.instancePool.Get(sp.IGName(), usCentral1CZone)
	expectedLinkC, _ := utils.RelativeResourceName(igC.SelfLink)
	if !slice.ContainsString(backendGroups, expectedLinkA, nil) {
		t.Errorf("expected that BackendService backend contains %v, got=%v", expectedLinkA, backendGroups)
	}
	if slice.ContainsString(backendGroups, expectedLinkC, nil) {
		t.Errorf("expected that BackendService backend does not contain %v, got=%v", expectedLinkC, backendGroups)
	}
}

func createBackendService(t *testing.T, sp utils.ServicePort, backendPool *Backends) {
	t.Helper()
	namespacedName := types.NamespacedName{Name: "service.Name", Namespace: "service.Namespace"}
	protocol := string(apiv1.ProtocolTCP)
	serviceAffinityNone := string(apiv1.ServiceAffinityNone)
	schemeExternal := string(cloud.SchemeExternal)
	defaultNetworkInfo := network.NetworkInfo{IsDefault: true}
	var noConnectionTrackingPolicy *composite.BackendServiceConnectionTrackingPolicy = nil
	if _, err := backendPool.EnsureL4BackendService(sp.BackendName(), hcLink, protocol, serviceAffinityNone, schemeExternal, namespacedName, defaultNetworkInfo, noConnectionTrackingPolicy, klog.TODO()); err != nil {
		t.Fatalf("Error creating backend service %v", err)
	}
}
