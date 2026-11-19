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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	negv1beta1 "k8s.io/ingress-gce/pkg/apis/svcneg/v1beta1"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/l4/annotations"
	"k8s.io/ingress-gce/pkg/l4/resources"
	"k8s.io/ingress-gce/pkg/test"
)

// negMapKey converts a NEG self link into the key returned by getServiceNEGLinks.
func negMapKey(t *testing.T, selfLink string) cloud.ResourceMapKey {
	t.Helper()
	resID, err := cloud.ParseResourceURL(selfLink)
	if err != nil {
		t.Fatalf("ParseResourceURL(%q) returned error: %v", selfLink, err)
	}
	return resID.MapKey()
}

func TestGetServiceNEGLinks_CustomNEGName(t *testing.T) {
	flags.F.EnableL4CustomStandaloneNEGNames = true
	defer func() { flags.F.EnableL4CustomStandaloneNEGNames = false }()

	const (
		project    = "test-project"
		zone       = "us-central1-a"
		namespace  = "default"
		svcName    = "svc-custom-neg"
		customName = "my-custom-neg"
	)
	lbClass := annotations.StandalonePassthroughNegLoadBalancerClass

	newSvc := func(annots map[string]string) *v1.Service {
		return &v1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:        svcName,
				Namespace:   namespace,
				Annotations: annots,
			},
			Spec: v1.ServiceSpec{
				Type:              v1.ServiceTypeLoadBalancer,
				LoadBalancerClass: &lbClass,
			},
		}
	}

	testCases := []struct {
		desc string
		svc  *v1.Service
		// negNamesInStore are the SvcNeg CR names to register, each pointing at a NEG of the same name.
		negNamesInStore func(generatedName string) []string
		isOwnedBySvc    bool
		// wantNEGNames are the NEG names expected in the returned set.
		wantNEGNames func(generatedName string) []string
		wantErr      bool
	}{
		{
			desc:            "no annotation: falls back to the generated L4Backend name",
			svc:             newSvc(nil),
			isOwnedBySvc:    true,
			negNamesInStore: func(generated string) []string { return []string{generated, customName} },
			wantNEGNames:    func(generated string) []string { return []string{generated} },
		},
		{
			desc: "annotation set: NEG name matches the annotation value, generated name is ignored",
			svc: newSvc(map[string]string{
				annotations.StandalonePassthroughLBNegNameKey: customName,
			}),
			isOwnedBySvc:    true,
			negNamesInStore: func(generated string) []string { return []string{generated, customName} },
			wantNEGNames:    func(generated string) []string { return []string{customName} },
		},
		{
			desc: "annotation set: NEG name matches the annotation value, but is not owned by controller",
			svc: newSvc(map[string]string{
				annotations.StandalonePassthroughLBNegNameKey: customName,
			}),
			isOwnedBySvc:    false,
			negNamesInStore: func(generated string) []string { return []string{generated, customName} },
			wantErr:         true,
		},
		{
			desc: "annotation set but only the generated NEG exists: nothing is returned",
			svc: newSvc(map[string]string{
				annotations.StandalonePassthroughLBNegNameKey: customName,
			}),
			isOwnedBySvc:    true,
			negNamesInStore: func(generated string) []string { return []string{generated} },
			wantNEGNames:    func(generated string) []string { return nil },
		},
		{
			desc: "invalid custom name is rejected",
			svc: newSvc(map[string]string{
				annotations.StandalonePassthroughLBNegNameKey: "Invalid_NEG_Name",
			}),
			isOwnedBySvc:    true,
			negNamesInStore: func(generated string) []string { return []string{generated} },
			wantErr:         true,
		},
		{
			desc: "empty custom name is rejected",
			svc: newSvc(map[string]string{
				annotations.StandalonePassthroughLBNegNameKey: "",
			}),
			isOwnedBySvc:    true,
			negNamesInStore: func(generated string) []string { return []string{generated} },
			wantErr:         true,
		},
		{
			desc: "path traversal name is rejected",
			svc: newSvc(map[string]string{
				annotations.StandalonePassthroughLBNegNameKey: "../../us-central1-c/networkEndpointGroups/custom-neg",
			}),
			isOwnedBySvc:    true,
			negNamesInStore: func(generated string) []string { return []string{generated} },
			wantErr:         true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			_, _, lc, stopCh := setupControllerContext(t)
			defer close(stopCh)

			generatedName := lc.namer.L4Backend(namespace, svcName)
			if generatedName == customName {
				t.Fatalf("test setup is broken: generated name equals the custom name %q", customName)
			}

			for _, negName := range tc.negNamesInStore(generatedName) {
				svcNeg := test.NewSvcNeg(
					types.NamespacedName{Namespace: namespace, Name: negName},
					negv1beta1.ServiceNetworkEndpointGroupStatus{
						NetworkEndpointGroups: []negv1beta1.NegObjectReference{
							{SelfLink: negSelfLink(project, zone, negName)},
						},
					},
				)
				if tc.isOwnedBySvc {
					svcNeg.SetOwnerReferences([]metav1.OwnerReference{
						*metav1.NewControllerRef(tc.svc, tc.svc.GroupVersionKind()),
					})
				}
				if err := lc.ctx.SvcNegInformer.GetIndexer().Add(svcNeg); err != nil {
					t.Fatalf("Failed to add SvcNeg %q: %v", negName, err)
				}
			}

			got, err := lc.serviceNEGLinks(tc.svc)
			if gotErr := err != nil; gotErr != tc.wantErr {
				t.Fatalf("getServiceNEGLinks() error = %v, wantErr = %v", err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}

			want := tc.wantNEGNames(generatedName)
			if got.Len() != len(want) {
				t.Fatalf("getServiceNEGLinks() returned %d NEG(s) %v, want %d %v", got.Len(), got.UnsortedList(), len(want), want)
			}
			for _, negName := range want {
				if key := negMapKey(t, negSelfLink(project, zone, negName)); !got.Has(key) {
					t.Errorf("getServiceNEGLinks() = %v, missing NEG %q", got.UnsortedList(), negName)
				}
			}
			// The generated name must not leak in when a custom name is requested.
			if len(want) == 1 && want[0] == customName {
				if key := negMapKey(t, negSelfLink(project, zone, generatedName)); got.Has(key) {
					t.Errorf("getServiceNEGLinks() still returned the generated NEG %q", generatedName)
				}
			}
		})
	}
}

func TestServiceNEGKeys(t *testing.T) {
	svc := standaloneSvc("fr")
	svc.UID = "svc-uid"
	neg1 := negSelfLink(snegTestProject, snegTestZone, "neg-1")
	neg2 := negSelfLink(snegTestProject, "us-central1-b", "neg-1")

	testCases := []struct {
		desc      string
		owner     *v1.Service
		negRefs   []negv1beta1.NegObjectReference
		wantKeys  []string
		wantError bool
	}{
		{
			desc:     "NEGs of the service",
			owner:    svc,
			negRefs:  []negv1beta1.NegObjectReference{{SelfLink: neg1}, {SelfLink: neg2}},
			wantKeys: []string{neg1, neg2},
		},
		{
			desc:     "invalid NEG references are skipped",
			owner:    svc,
			negRefs:  []negv1beta1.NegObjectReference{{SelfLink: ""}, {SelfLink: "invalid"}, {SelfLink: neg1}},
			wantKeys: []string{neg1},
		},
		{
			desc:      "ServiceNetworkEndpointGroup not controlled by the service",
			negRefs:   []negv1beta1.NegObjectReference{{SelfLink: neg1}},
			wantError: true,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			svcNeg := &negv1beta1.ServiceNetworkEndpointGroup{
				ObjectMeta: metav1.ObjectMeta{Name: "neg-1", Namespace: svc.Namespace},
				Status:     negv1beta1.ServiceNetworkEndpointGroupStatus{NetworkEndpointGroups: tc.negRefs},
			}
			if tc.owner != nil {
				svcNeg.SetOwnerReferences([]metav1.OwnerReference{*metav1.NewControllerRef(tc.owner, v1.SchemeGroupVersion.WithKind("Service"))})
			}

			keys, err := serviceNEGKeys(svc, svcNeg)
			if tc.wantError {
				if err == nil || !resources.IsUserError(err) {
					t.Errorf("serviceNEGKeys() error = %v, want a user error", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("serviceNEGKeys() error = %v", err)
			}
			wantKeys := sets.New[cloud.ResourceMapKey]()
			for _, selfLink := range tc.wantKeys {
				wantKeys.Insert(negMapKey(t, selfLink))
			}
			if !keys.Equal(wantKeys) {
				t.Errorf("serviceNEGKeys() = %v, want %v", keys, wantKeys)
			}
		})
	}
}
