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

package types

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	negv1beta1 "k8s.io/ingress-gce/pkg/apis/svcneg/v1beta1"
)

func TestSvcNegServiceIndexFunc(t *testing.T) {
	svc := &apiv1.Service{ObjectMeta: metav1.ObjectMeta{Name: "svc", Namespace: "ns", UID: "svc-uid"}}
	controllerRef := *metav1.NewControllerRef(svc, apiv1.SchemeGroupVersion.WithKind("Service"))
	nonControllerRef := controllerRef
	nonControllerRef.Controller = nil
	otherKindRef := *metav1.NewControllerRef(svc, apiv1.SchemeGroupVersion.WithKind("Pod"))

	testCases := []struct {
		desc    string
		obj     interface{}
		want    []string
		wantErr bool
	}{
		{
			desc: "controlled by a service",
			obj:  &negv1beta1.ServiceNetworkEndpointGroup{ObjectMeta: metav1.ObjectMeta{Name: "neg", Namespace: "ns", OwnerReferences: []metav1.OwnerReference{controllerRef}}},
			want: []string{"ns/svc"},
		},
		{
			desc: "owned but not controlled by a service",
			obj:  &negv1beta1.ServiceNetworkEndpointGroup{ObjectMeta: metav1.ObjectMeta{Name: "neg", Namespace: "ns", OwnerReferences: []metav1.OwnerReference{nonControllerRef}}},
		},
		{
			desc: "controlled by another kind",
			obj:  &negv1beta1.ServiceNetworkEndpointGroup{ObjectMeta: metav1.ObjectMeta{Name: "neg", Namespace: "ns", OwnerReferences: []metav1.OwnerReference{otherKindRef}}},
		},
		{
			desc: "no owner",
			obj:  &negv1beta1.ServiceNetworkEndpointGroup{ObjectMeta: metav1.ObjectMeta{Name: "neg", Namespace: "ns"}},
		},
		{
			desc:    "unexpected object",
			obj:     svc,
			wantErr: true,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			got, err := SvcNegServiceIndexFunc(tc.obj)
			if gotErr := err != nil; gotErr != tc.wantErr {
				t.Fatalf("SvcNegServiceIndexFunc() error = %v, wantErr %v", err, tc.wantErr)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("SvcNegServiceIndexFunc() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSvcNegIndexers(t *testing.T) {
	indexers := SvcNegIndexers()
	for _, name := range []string{cache.NamespaceIndex, SvcNegServiceIndex} {
		if _, ok := indexers[name]; !ok {
			t.Errorf("SvcNegIndexers() is missing the %q index", name)
		}
	}
}

func TestControllingService(t *testing.T) {
	svc := &apiv1.Service{ObjectMeta: metav1.ObjectMeta{Name: "svc", Namespace: "ns", UID: "svc-uid"}}
	controllerRef := *metav1.NewControllerRef(svc, apiv1.SchemeGroupVersion.WithKind("Service"))
	otherKindRef := *metav1.NewControllerRef(svc, apiv1.SchemeGroupVersion.WithKind("Pod"))
	newSvcNeg := func(refs ...metav1.OwnerReference) *negv1beta1.ServiceNetworkEndpointGroup {
		return &negv1beta1.ServiceNetworkEndpointGroup{ObjectMeta: metav1.ObjectMeta{Name: "neg", Namespace: "ns", OwnerReferences: refs}}
	}

	if got := ControllingService(newSvcNeg(controllerRef)); got == nil || got.Name != "svc" || got.UID != "svc-uid" {
		t.Errorf("ControllingService() = %+v, want the owner reference of the service", got)
	}
	if got := ControllingService(newSvcNeg(otherKindRef)); got != nil {
		t.Errorf("ControllingService() = %+v, want nil for a controller of another kind", got)
	}
	if got := ControllingService(newSvcNeg()); got != nil {
		t.Errorf("ControllingService() = %+v, want nil without owner", got)
	}
}
