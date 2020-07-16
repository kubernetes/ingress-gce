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

	"k8s.io/ingress-gce/pkg/annotations"
	befeatures "k8s.io/ingress-gce/pkg/backends/features"
	"k8s.io/ingress-gce/pkg/composite"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/mock"
	"k8s.io/apimachinery/pkg/types"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/legacy-cloud-providers/gce"
)

func newTestNEGLinker(fakeNEG negtypes.NetworkEndpointGroupCloud, fakeGCE *gce.Cloud) *negLinker {
	fakeBackendPool := NewPool(fakeGCE, defaultNamer)

	// Add standard hooks for mocking update calls. Each test can set a update different hook if it chooses to.
	(fakeGCE.Compute().(*cloud.MockGCE)).MockAlphaBackendServices.UpdateHook = mock.UpdateAlphaBackendServiceHook
	(fakeGCE.Compute().(*cloud.MockGCE)).MockBetaBackendServices.UpdateHook = mock.UpdateBetaBackendServiceHook
	(fakeGCE.Compute().(*cloud.MockGCE)).MockBackendServices.UpdateHook = mock.UpdateBackendServiceHook
	(fakeGCE.Compute().(*cloud.MockGCE)).MockAlphaRegionBackendServices.UpdateHook = mock.UpdateAlphaRegionBackendServiceHook
	(fakeGCE.Compute().(*cloud.MockGCE)).MockBetaRegionBackendServices.UpdateHook = mock.UpdateBetaRegionBackendServiceHook
	(fakeGCE.Compute().(*cloud.MockGCE)).MockRegionBackendServices.UpdateHook = mock.UpdateRegionBackendServiceHook
	return &negLinker{fakeBackendPool, fakeNEG, fakeGCE}
}

func TestLinkBackendServiceToNEG(t *testing.T) {
	fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
	fakeNEG := negtypes.NewFakeNetworkEndpointGroupCloud("test-subnetwork", "test-network")
	linker := newTestNEGLinker(fakeNEG, fakeGCE)

	zones := []GroupKey{{Zone: "zone1"}, {Zone: "zone2"}}
	namespace, name, port := "ns", "name", "port"
	svc := types.NamespacedName{Namespace: namespace, Name: name}

	for _, svcPort := range []utils.ServicePort{
		utils.ServicePort{
			ID:             utils.ServicePortID{Service: svc},
			BackendNamer:   defaultNamer,
			VMIPNEGEnabled: true},
		utils.ServicePort{
			ID:           utils.ServicePortID{Service: svc},
			Port:         80,
			NodePort:     30001,
			Protocol:     annotations.ProtocolHTTP,
			TargetPort:   port,
			NEGEnabled:   true,
			BackendNamer: defaultNamer},
	} {
		// Mimic how the syncer would create the backend.
		if _, err := linker.backendPool.Create(svcPort, "fake-healthcheck-link"); err != nil {
			t.Fatalf("Failed to create backend service to NEG for svcPort %v: %v", svcPort, err)
		}

		version := befeatures.VersionFromServicePort(&svcPort)

		for _, key := range zones {
			neg := &composite.NetworkEndpointGroup{
				Name:    svcPort.BackendName(),
				Version: version,
			}
			if svcPort.VMIPNEGEnabled {
				neg.NetworkEndpointType = string(negtypes.VmIpEndpointType)
			}
			err := fakeNEG.CreateNetworkEndpointGroup(neg, key.Zone)
			if err != nil {
				t.Fatalf("unexpected error creating NEG for svcPort %v: %v", svcPort, err)
			}
		}

		if err := linker.Link(svcPort, zones); err != nil {
			t.Fatalf("Failed to link backend service to NEG for svcPort %v: %v", svcPort, err)
		}

		beName := svcPort.BackendName()
		scope := befeatures.ScopeFromServicePort(&svcPort)

		key, err := composite.CreateKey(fakeGCE, beName, scope)
		if err != nil {
			t.Fatalf("Failed to create composite key - %v", err)
		}
		bs, err := composite.GetBackendService(fakeGCE, key, version)
		if err != nil {
			t.Fatalf("Failed to retrieve backend service using key %+v for svcPort %v: %v", key, svcPort, err)
		}
		if len(bs.Backends) != len(zones) {
			t.Errorf("Expect %v backends in backend service %s, but got %v.key %+v %+v", len(zones), beName, len(bs.Backends), key, bs)
		}

		for _, be := range bs.Backends {
			neg := "networkEndpointGroups"
			if !strings.Contains(be.Group, neg) {
				t.Errorf("Got backend link %q, want containing %q", be.Group, neg)
			}
			if svcPort.VMIPNEGEnabled {
				// Balancing mode should be connection, rate should be unset
				if be.BalancingMode != string(Connections) || be.MaxRatePerEndpoint != 0 {
					t.Errorf("Only 'Connection' balancing mode is supported with VM_IP NEGs, Got %q with max rate %v", be.BalancingMode, be.MaxRatePerEndpoint)
				}
			}
		}
	}
}
