/*
Copyright 2026 The Kubernetes Authors.

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

package forwardingrules

import (
	"errors"
	"strings"
	"testing"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	api_v1 "k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/klog/v2"

	"k8s.io/cloud-provider-gcp/providers/gce"
)

type fakeNetwork struct {
	legacy bool
}

func (n fakeNetwork) IsLegacyNetwork() bool { return n.legacy }

func TestCheckRecreateAllowed(t *testing.T) {
	t.Parallel()
	existing := &composite.ForwardingRule{Name: "fr-name", IPAddress: "1.2.3.4"}

	testCases := []struct {
		desc       string
		ipReserved bool
		legacy     bool
		wantErr    bool
	}{
		{desc: "reserved IP", ipReserved: true, wantErr: false},
		{desc: "reserved IP in legacy network", ipReserved: true, legacy: true, wantErr: false},
		{desc: "unreserved IP in legacy network", ipReserved: false, legacy: true, wantErr: false},
		{desc: "unreserved IP", ipReserved: false, wantErr: true},
	}
	for _, tc := range testCases {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			err := CheckRecreateAllowed(fakeNetwork{legacy: tc.legacy}, existing, tc.ipReserved)
			if (err != nil) != tc.wantErr {
				t.Fatalf("CheckRecreateAllowed(legacy=%v, _, ipReserved=%v) = %v, want error: %v", tc.legacy, tc.ipReserved, err, tc.wantErr)
			}
			if !tc.wantErr {
				return
			}
			if !errors.Is(err, ErrIPNotReserved) {
				t.Errorf("CheckRecreateAllowed() = %v, want an error wrapping ErrIPNotReserved", err)
			}
			for _, want := range []string{existing.Name, existing.IPAddress} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("CheckRecreateAllowed() = %q, want it to mention %q", err, want)
				}
			}
		})
	}
}

// TestMixedManagerNetLB_EnsureRequiresReservedIPToRecreate verifies that ensure refuses to
// delete and re-create a forwarding rule when the IP is not reserved, and leaves it untouched.
func TestMixedManagerNetLB_EnsureRequiresReservedIPToRecreate(t *testing.T) {
	t.Parallel()
	const (
		ip     = "1.2.3.4"
		bsLink = "http://compute.googleapis.com/projects/test/regions/us-central1/backendServices/bs1"
	)

	fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
	m := &MixedManagerNetLB{
		Namer:    namer.NewL4Namer("ksuid123", nil),
		Provider: New(fakeGCE, meta.VersionGA, meta.Regional, klog.TODO()),
		Recorder: &record.FakeRecorder{},
		Logger:   klog.TODO(),
		Cloud:    fakeGCE,
		Service: &api_v1.Service{
			ObjectMeta: meta_v1.ObjectMeta{UID: "ksuid123", Namespace: "test-ns", Name: "test-svc"},
			Spec: api_v1.ServiceSpec{
				Ports: []api_v1.ServicePort{{Protocol: api_v1.ProtocolTCP, Port: 80}, {Protocol: api_v1.ProtocolUDP, Port: 53}},
			},
		},
	}
	// The existing TCP rule forwards another port, so it can't be patched and must be re-created.
	existing := &composite.ForwardingRule{
		Name:                m.name("TCP"),
		IPAddress:           ip,
		IPProtocol:          "TCP",
		Ports:               []string{"8080"},
		LoadBalancingScheme: "EXTERNAL",
		NetworkTier:         "PREMIUM",
		BackendService:      bsLink,
	}
	if err := m.Provider.Create(existing); err != nil {
		t.Fatalf("Provider.Create(%v) returned error %v, want nil", existing, err)
	}

	_, _, err := m.ensure(existing, bsLink, "TCP", ip, false)
	if !errors.Is(err, ErrIPNotReserved) {
		t.Fatalf("ensure() with an unreserved IP returned error %v, want an error wrapping ErrIPNotReserved", err)
	}
	got, err := m.Provider.Get(existing.Name)
	if err != nil || got == nil {
		t.Fatalf("Provider.Get(%q) = %v, %v, want the existing rule to be left in place", existing.Name, got, err)
	}
	if got.IPAddress != ip || len(got.Ports) != 1 || got.Ports[0] != "8080" {
		t.Errorf("Provider.Get(%q) = %+v, want the existing rule unchanged", existing.Name, got)
	}

	recreated, syncStatus, err := m.ensure(existing, bsLink, "TCP", ip, true)
	if err != nil {
		t.Fatalf("ensure() with a reserved IP returned error %v, want nil", err)
	}
	if !bool(syncStatus) || recreated == nil || len(recreated.Ports) != 1 || recreated.Ports[0] != "80" {
		t.Errorf("ensure() with a reserved IP = %+v, %v, want the rule re-created with port 80", recreated, syncStatus)
	}
}
