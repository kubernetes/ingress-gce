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
	"strings"
	"testing"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/mock"
	computebeta "google.golang.org/api/compute/v0.beta"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/ingress-gce/pkg/annotations"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/kubernetes/pkg/cloudprovider/providers/gce"
)

func newTestNEGLinker(fakeNEG negtypes.NetworkEndpointGroupCloud, fakeGCE *gce.Cloud) *negLinker {
	fakeBackendPool := NewPool(fakeGCE, defaultNamer)

	// Add standard hooks for mocking update calls. Each test can set a update different hook if it chooses to.
	(fakeGCE.Compute().(*cloud.MockGCE)).MockAlphaBackendServices.UpdateHook = mock.UpdateAlphaBackendServiceHook
	(fakeGCE.Compute().(*cloud.MockGCE)).MockBetaBackendServices.UpdateHook = mock.UpdateBetaBackendServiceHook
	(fakeGCE.Compute().(*cloud.MockGCE)).MockBackendServices.UpdateHook = mock.UpdateBackendServiceHook

	return &negLinker{fakeBackendPool, fakeNEG, defaultNamer}
}

func TestLinkBackendServiceToNEG(t *testing.T) {
	fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
	fakeNEG := negtypes.NewFakeNetworkEndpointGroupCloud("test-subnetwork", "test-network")
	linker := newTestNEGLinker(fakeNEG, fakeGCE)

	zones := []GroupKey{{Zone: "zone1"}, {Zone: "zone2"}}
	namespace, name, port := "ns", "name", "port"

	svcPort := utils.ServicePort{
		ID: utils.ServicePortID{
			Service: types.NamespacedName{
				Namespace: namespace,
				Name:      name,
			},
		},
		Port:       80,
		NodePort:   30001,
		Protocol:   annotations.ProtocolHTTP,
		TargetPort: port,
		NEGEnabled: true,
	}

	// Mimic how the syncer would create the backend.
	linker.backendPool.Create(svcPort, "fake-healthcheck-link")

	for _, key := range zones {
		err := fakeNEG.CreateNetworkEndpointGroup(&computebeta.NetworkEndpointGroup{
			Name: defaultNamer.NEG(namespace, name, svcPort.Port),
		}, key.Zone)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	if err := linker.Link(svcPort, zones); err != nil {
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
		neg := "networkEndpointGroups"
		if !strings.Contains(be.Group, neg) {
			t.Errorf("Got backend link %q, want containing %q", be.Group, neg)
		}
	}
}
