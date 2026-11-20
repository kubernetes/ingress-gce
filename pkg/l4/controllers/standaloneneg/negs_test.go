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

package standaloneneg

import (
	"testing"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	negv1beta1 "k8s.io/ingress-gce/pkg/apis/svcneg/v1beta1"
	"k8s.io/ingress-gce/pkg/l4/annotations"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
)

// negKeys converts NEG self links into the keys returned by listServiceNEGs.
func negKeys(t *testing.T, selfLinks ...string) sets.Set[cloud.ResourceMapKey] {
	t.Helper()
	keys := sets.New[cloud.ResourceMapKey]()
	for _, selfLink := range selfLinks {
		resID, err := cloud.ParseResourceURL(selfLink)
		if err != nil {
			t.Fatalf("ParseResourceURL(%q) returned error: %v", selfLink, err)
		}
		keys.Insert(resID.MapKey())
	}
	return keys
}

func TestListServiceNEGs(t *testing.T) {
	svc := standaloneSvc("fr")
	svc.UID = "svc-uid"
	// A service with the same name that was deleted and created again.
	previousSvc := svc.DeepCopy()
	previousSvc.UID = "previous-svc-uid"
	otherSvc := standaloneSvc("fr")
	otherSvc.Name = "other-svc"
	otherSvc.UID = "other-svc-uid"

	neg := negSelfLink(snegTestProject, snegTestZone, "neg")
	negZoneB := negSelfLink(snegTestProject, "us-central1-b", "neg")
	negOtherSubnet := negSelfLink(snegTestProject, snegTestZone, "neg-a1b2c3")
	oldNEG := negSelfLink(snegTestProject, snegTestZone, "old-neg")
	otherNEG := negSelfLink(snegTestProject, snegTestZone, "other-neg")

	ref := func(selfLink string) negv1beta1.NegObjectReference {
		return negv1beta1.NegObjectReference{SelfLink: selfLink, NetworkEndpointType: negv1beta1.VmIpEndpointType}
	}
	toBeDeleted := func(selfLink string) negv1beta1.NegObjectReference {
		r := ref(selfLink)
		r.State = negv1beta1.ToBeDeletedState
		return r
	}
	svcNegOf := func(owner *v1.Service, name string, refs ...negv1beta1.NegObjectReference) *negv1beta1.ServiceNetworkEndpointGroup {
		return vmIPSvcNegOf(testSvcNeg(name, refs...), owner)
	}

	testCases := []struct {
		desc                      string
		svcNegs                   []*negv1beta1.ServiceNetworkEndpointGroup
		wantActive                []string
		wantDraining              []string
		wantMultipleActiveSvcNegs bool
	}{
		{
			desc: "no ServiceNetworkEndpointGroup",
		},
		{
			// The ServiceNetworkEndpointGroup is found by ownership, a custom name needs neither the
			// annotation nor the flag.
			desc:       "NEGs of all zones and subnets of a ServiceNetworkEndpointGroup with any name",
			svcNegs:    []*negv1beta1.ServiceNetworkEndpointGroup{svcNegOf(svc, "custom-name", ref(neg), ref(negZoneB), ref(negOtherSubnet))},
			wantActive: []string{neg, negZoneB, negOtherSubnet},
		},
		{
			desc:         "NEG in the TO_BE_DELETED state is draining",
			svcNegs:      []*negv1beta1.ServiceNetworkEndpointGroup{svcNegOf(svc, "neg", ref(neg), toBeDeleted(negOtherSubnet))},
			wantActive:   []string{neg},
			wantDraining: []string{negOtherSubnet},
		},
		{
			desc: "NEGs of a ServiceNetworkEndpointGroup being deleted after a NEG name change are draining",
			svcNegs: []*negv1beta1.ServiceNetworkEndpointGroup{
				svcNegOf(svc, "neg", ref(neg)),
				deletingSvcNeg(svcNegOf(svc, "old-neg", ref(oldNEG))),
			},
			wantActive:   []string{neg},
			wantDraining: []string{oldNEG},
		},
		{
			desc: "NEGs of multiple ServiceNetworkEndpointGroups not being deleted are active",
			svcNegs: []*negv1beta1.ServiceNetworkEndpointGroup{
				svcNegOf(svc, "neg", ref(neg)),
				svcNegOf(svc, "old-neg", ref(oldNEG)),
			},
			wantActive:                []string{neg, oldNEG},
			wantMultipleActiveSvcNegs: true,
		},
		{
			desc: "ServiceNetworkEndpointGroups of other services, of a previous service with the same name and without owner are ignored",
			svcNegs: []*negv1beta1.ServiceNetworkEndpointGroup{
				svcNegOf(svc, "neg", ref(neg)),
				svcNegOf(otherSvc, "other-neg", ref(otherNEG)),
				svcNegOf(previousSvc, "previous-neg", ref(oldNEG)),
				testSvcNeg("no-owner", ref(otherNEG)),
			},
			wantActive: []string{neg},
		},
		{
			desc: "GCE_VM_IP_PORT ServiceNetworkEndpointGroup of the service is ignored",
			svcNegs: []*negv1beta1.ServiceNetworkEndpointGroup{
				svcNegOf(svc, "neg", ref(neg)),
				func() *negv1beta1.ServiceNetworkEndpointGroup {
					// The reference has no type, the ServiceNetworkEndpointGroup is only excluded by its port label.
					l7SvcNeg := testSvcNeg("l7-neg", negv1beta1.NegObjectReference{SelfLink: otherNEG})
					l7SvcNeg.Labels = map[string]string{negtypes.NegCRServicePortKey: "80"}
					return vmIPSvcNegOf(l7SvcNeg, svc)
				}(),
			},
			wantActive: []string{neg},
		},
		{
			desc: "invalid and non GCE_VM_IP NEG references are skipped, references without type are used",
			svcNegs: []*negv1beta1.ServiceNetworkEndpointGroup{svcNegOf(svc, "neg",
				negv1beta1.NegObjectReference{SelfLink: ""},
				negv1beta1.NegObjectReference{SelfLink: "invalid"},
				negv1beta1.NegObjectReference{SelfLink: otherNEG, NetworkEndpointType: negv1beta1.VmIpPortEndpointType},
				negv1beta1.NegObjectReference{SelfLink: neg},
			)},
			wantActive: []string{neg},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			_, _, lc, stopCh := setupControllerContext(t)
			defer close(stopCh)
			for _, svcNeg := range tc.svcNegs {
				if err := lc.ctx.SvcNegInformer.GetIndexer().Add(svcNeg); err != nil {
					t.Fatalf("Failed to add ServiceNetworkEndpointGroup %s: %v", svcNeg.Name, err)
				}
			}

			got, err := lc.listServiceNEGs(svc)
			if err != nil {
				t.Fatalf("listServiceNEGs() error = %v", err)
			}
			if want := negKeys(t, tc.wantActive...); !got.active.Equal(want) {
				t.Errorf("listServiceNEGs() active = %v, want %v", got.active.UnsortedList(), want.UnsortedList())
			}
			if want := negKeys(t, tc.wantDraining...); !got.draining.Equal(want) {
				t.Errorf("listServiceNEGs() draining = %v, want %v", got.draining.UnsortedList(), want.UnsortedList())
			}
			if got.multipleActiveSvcNegs != tc.wantMultipleActiveSvcNegs {
				t.Errorf("listServiceNEGs() multipleActiveSvcNegs = %v, want %v", got.multipleActiveSvcNegs, tc.wantMultipleActiveSvcNegs)
			}
		})
	}
}

// TestListServiceNEGsIgnoresAnnotation verifies that the NEG name annotation is not interpreted by the
// controller, the NEGs are only read from the ServiceNetworkEndpointGroups created by the NEG controller.
func TestListServiceNEGsIgnoresAnnotation(t *testing.T) {
	svc := standaloneSvc("fr")
	svc.Annotations[annotations.StandalonePassthroughLBNegNameKey] = "Invalid_NEG_Name"
	neg := negSelfLink(snegTestProject, snegTestZone, "neg")

	_, _, lc, stopCh := setupControllerContext(t)
	defer close(stopCh)
	if err := lc.ctx.SvcNegInformer.GetIndexer().Add(vmIPSvcNegOf(testSvcNeg("generated-neg", negv1beta1.NegObjectReference{SelfLink: neg}), svc)); err != nil {
		t.Fatalf("Failed to add ServiceNetworkEndpointGroup: %v", err)
	}

	got, err := lc.listServiceNEGs(svc)
	if err != nil {
		t.Fatalf("listServiceNEGs() error = %v, want nil", err)
	}
	if want := negKeys(t, neg); !got.active.Equal(want) {
		t.Errorf("listServiceNEGs() active = %v, want %v", got.active.UnsortedList(), want.UnsortedList())
	}
}
