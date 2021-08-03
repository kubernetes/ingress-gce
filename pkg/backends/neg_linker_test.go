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
	"github.com/google/go-cmp/cmp"
	"github.com/kr/pretty"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/ingress-gce/pkg/annotations"
	befeatures "k8s.io/ingress-gce/pkg/backends/features"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/flags"
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

func TestDiffBackends(t *testing.T) {
	// No t.Parallel().
	oldFlag := flags.F.EnableTrafficScaling
	flags.F.EnableTrafficScaling = true
	defer func() { flags.F.EnableTrafficScaling = oldFlag }()

	for _, tc := range []struct {
		name string
		old  []*composite.Backend
		new  []*composite.Backend

		isEqual  bool
		toRemove sets.String
		toAdd    sets.String
		changed  sets.String
	}{
		{
			name:    "empty",
			isEqual: true,
		},
		{
			name:    "same",
			old:     []*composite.Backend{{Group: "a"}},
			new:     []*composite.Backend{{Group: "a"}},
			isEqual: true,
		},
		{
			name:    "same (multiple)",
			old:     []*composite.Backend{{Group: "a"}, {Group: "b"}},
			new:     []*composite.Backend{{Group: "b"}, {Group: "a"}},
			isEqual: true,
		},
		{
			name:  "add backend",
			old:   []*composite.Backend{{Group: "a"}},
			new:   []*composite.Backend{{Group: "b"}, {Group: "a"}},
			toAdd: sets.NewString("b"),
		},
		{
			name:     "remove backend",
			old:      []*composite.Backend{{Group: "a"}, {Group: "b"}},
			new:      []*composite.Backend{{Group: "b"}},
			toRemove: sets.NewString("a"),
		},
		{
			name:     "add and remove",
			old:      []*composite.Backend{{Group: "a"}, {Group: "b"}, {Group: "c"}},
			new:      []*composite.Backend{{Group: "b"}, {Group: "a"}, {Group: "d"}},
			toAdd:    sets.NewString("d"),
			toRemove: sets.NewString("c"),
		},
		{
			name:    "update rate",
			old:     []*composite.Backend{{Group: "a", MaxRatePerEndpoint: 1}},
			new:     []*composite.Backend{{Group: "a", MaxRatePerEndpoint: 3}},
			changed: sets.NewString("a"),
		},
		{
			name:    "update capacity scaler",
			old:     []*composite.Backend{{Group: "a", CapacityScaler: 1.0}},
			new:     []*composite.Backend{{Group: "a", CapacityScaler: 0.5}},
			changed: sets.NewString("a"),
		},
		{
			name:    "no change",
			old:     []*composite.Backend{{Group: "a", CapacityScaler: 1.0}},
			new:     []*composite.Backend{{Group: "a", CapacityScaler: 1.0}},
			isEqual: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			diff := diffBackends(tc.old, tc.new)
			if got := diff.isEqual(); got != tc.isEqual {
				t.Errorf("diff := diffBackends(%s, %s); diff.isEqual() = %t, want %t", pretty.Sprint(tc.old), pretty.Sprint(tc.new), got, tc.isEqual)
			}
			if got := diff.toRemove(); !got.Equal(tc.toRemove) {
				t.Errorf("diff := diffBackends(%s, %s); diff.toRemove() = %s, want %s", pretty.Sprint(tc.old), pretty.Sprint(tc.new), got, tc.toRemove)
			}
			if got := diff.toAdd(); !got.Equal(tc.toAdd) {
				t.Errorf("diff := diffBackends(%s, %s); diff.toAdd() = %s, want %s", pretty.Sprint(tc.old), pretty.Sprint(tc.new), got, tc.toAdd)
			}
			if got := diff.changed; !got.Equal(tc.changed) {
				t.Errorf("diff := diffBackends(%s, %s); diff.changed = %s, want %s", pretty.Sprint(tc.old), pretty.Sprint(tc.new), got, tc.changed)
			}
		})
	}
}

func TestBackendsForNEG(t *testing.T) {
	// No t.Parallel().
	oldFlag := flags.F.EnableTrafficScaling
	flags.F.EnableTrafficScaling = true
	defer func() { flags.F.EnableTrafficScaling = oldFlag }()

	f64 := func(x float64) *float64 { return &x }

	for _, tc := range []struct {
		name string
		negs []*composite.NetworkEndpointGroup
		sp   *utils.ServicePort
		want []*composite.Backend
	}{
		{
			name: "vm ip endpoint uses connections balancing mode",
			negs: []*composite.NetworkEndpointGroup{
				{
					NetworkEndpointType: string(negtypes.VmIpEndpointType),
					SelfLink:            "/neg1",
				},
			},
			sp: &utils.ServicePort{},
			want: []*composite.Backend{
				{
					BalancingMode: "CONNECTION",
					Group:         "/neg1",
				},
			},
		},
		{
			name: "vm ip endpoint (multiple)",
			negs: []*composite.NetworkEndpointGroup{
				{
					NetworkEndpointType: string(negtypes.VmIpEndpointType),
					SelfLink:            "/neg1",
				},
				{
					NetworkEndpointType: string(negtypes.VmIpEndpointType),
					SelfLink:            "/neg2",
				},
			},
			sp: &utils.ServicePort{},
			want: []*composite.Backend{
				{
					BalancingMode: "CONNECTION",
					Group:         "/neg1",
				},
				{
					BalancingMode: "CONNECTION",
					Group:         "/neg2",
				},
			},
		},
		{
			name: "neg endpoint defaults",
			negs: []*composite.NetworkEndpointGroup{
				{
					NetworkEndpointType: string(negtypes.VmIpPortEndpointType),
					SelfLink:            "/neg1",
				},
			},
			sp: &utils.ServicePort{},
			want: []*composite.Backend{
				{
					BalancingMode:      "RATE",
					MaxRatePerEndpoint: maxRPS,
					CapacityScaler:     1.0,
					Group:              "/neg1",
				},
			},
		},
		{
			name: "neg endpoint (traffic policy rate)",
			negs: []*composite.NetworkEndpointGroup{
				{
					NetworkEndpointType: string(negtypes.VmIpPortEndpointType),
					SelfLink:            "/neg1",
				},
			},
			sp: &utils.ServicePort{
				MaxRatePerEndpoint: f64(1234),
			},
			want: []*composite.Backend{
				{
					BalancingMode:      "RATE",
					MaxRatePerEndpoint: 1234,
					CapacityScaler:     1.0,
					Group:              "/neg1",
				},
			},
		},
		{
			name: "neg endpoint (traffic policy capacity scaler)",
			negs: []*composite.NetworkEndpointGroup{
				{
					NetworkEndpointType: string(negtypes.VmIpPortEndpointType),
					SelfLink:            "/neg1",
				},
			},
			sp: &utils.ServicePort{
				CapacityScaler: f64(0.5),
			},
			want: []*composite.Backend{
				{
					BalancingMode:      "RATE",
					MaxRatePerEndpoint: maxRPS,
					CapacityScaler:     0.5,
					Group:              "/neg1",
				},
			},
		},
		{
			name: "neg endpoint (traffic policy)",
			negs: []*composite.NetworkEndpointGroup{
				{
					NetworkEndpointType: string(negtypes.VmIpPortEndpointType),
					SelfLink:            "/neg1",
				},
			},
			sp: &utils.ServicePort{
				MaxRatePerEndpoint: f64(1234),
				CapacityScaler:     f64(0.5),
			},
			want: []*composite.Backend{
				{
					BalancingMode:      "RATE",
					MaxRatePerEndpoint: 1234,
					CapacityScaler:     0.5,
					Group:              "/neg1",
				},
			},
		},
		{
			name: "neg endpoint (multiple, traffic policy)",
			negs: []*composite.NetworkEndpointGroup{
				{
					NetworkEndpointType: string(negtypes.VmIpPortEndpointType),
					SelfLink:            "/neg1",
				},
				{
					NetworkEndpointType: string(negtypes.VmIpPortEndpointType),
					SelfLink:            "/neg2",
				},
			},
			sp: &utils.ServicePort{
				MaxRatePerEndpoint: f64(1234),
				CapacityScaler:     f64(0.5),
			},
			want: []*composite.Backend{
				{
					BalancingMode:      "RATE",
					MaxRatePerEndpoint: 1234,
					CapacityScaler:     0.5,
					Group:              "/neg1",
				},
				{
					BalancingMode:      "RATE",
					MaxRatePerEndpoint: 1234,
					CapacityScaler:     0.5,
					Group:              "/neg2",
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := backendsForNEGs(tc.negs, tc.sp)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("backendForNEGs(_), diff(-tc.want +got) = %s", diff)
			}
		})
	}
}
