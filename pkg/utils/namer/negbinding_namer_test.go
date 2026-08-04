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

package namer

import (
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	negbindingv1beta1 "k8s.io/ingress-gce/pkg/apis/negbinding/v1beta1"
)

func TestNegBindingNamer(t *testing.T) {
	namespace := "test-ns"
	name := "test-name"
	svcName := "svc-name"
	svcPort := int32(80)

	subnetName := "subnet-name"
	negName := "neg-name"

	testCases := []struct {
		desc         string
		binding      interface{}
		ns           string
		svc          string
		subnet       string
		port         int32
		customNEG    bool
		expectedNEG  string
		expectedErr  error
		expectAnyErr bool
	}{
		{
			desc:        "NEGBinding not in store",
			binding:     nil,
			ns:          namespace,
			svc:         svcName,
			subnet:      subnetName,
			port:        svcPort,
			expectedErr: ErrNegBindingNotFound,
		},
		{
			desc: "Subnet in the NEGBinding's spec",
			binding: &negbindingv1beta1.NetworkEndpointGroupBinding{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      name,
				},
				Spec: negbindingv1beta1.NetworkEndpointGroupBindingSpec{
					BackendRef: &negbindingv1beta1.BackendRefConfig{
						Name: svcName,
						Port: svcPort,
					},
					NetworkEndpointGroups: []negbindingv1beta1.SpecNegRef{
						{
							Subnet: subnetName,
							Name:   negName,
						},
					},
				},
			},
			ns:          namespace,
			svc:         svcName,
			subnet:      subnetName,
			port:        svcPort,
			expectedNEG: negName,
		},
		{
			desc: "NonDefaultSubnetCustomNEG with subnet in spec",
			binding: &negbindingv1beta1.NetworkEndpointGroupBinding{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      name,
				},
				Spec: negbindingv1beta1.NetworkEndpointGroupBindingSpec{
					BackendRef: &negbindingv1beta1.BackendRefConfig{
						Name: svcName,
						Port: svcPort,
					},
					NetworkEndpointGroups: []negbindingv1beta1.SpecNegRef{
						{
							Subnet: subnetName,
							Name:   negName,
						},
					},
				},
			},
			ns:          namespace,
			svc:         svcName,
			subnet:      subnetName,
			port:        svcPort,
			customNEG:   true,
			expectedErr: ErrNBNamerNotImplemented,
		},
		{
			desc: "Subnet not in the NEGBinding's spec",
			binding: &negbindingv1beta1.NetworkEndpointGroupBinding{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      name,
				},
				Spec: negbindingv1beta1.NetworkEndpointGroupBindingSpec{
					BackendRef: &negbindingv1beta1.BackendRefConfig{
						Name: svcName,
						Port: svcPort,
					},
					NetworkEndpointGroups: []negbindingv1beta1.SpecNegRef{},
				},
			},
			ns:          namespace,
			svc:         svcName,
			subnet:      "unset-subnet",
			port:        svcPort,
			expectedErr: ErrNegNameNotFound,
		},
		{
			desc: "NonDefaultSubnetCustomNEG with subnet unset",
			binding: &negbindingv1beta1.NetworkEndpointGroupBinding{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      name,
				},
				Spec: negbindingv1beta1.NetworkEndpointGroupBindingSpec{
					BackendRef: &negbindingv1beta1.BackendRefConfig{
						Name: svcName,
						Port: svcPort,
					},
					NetworkEndpointGroups: []negbindingv1beta1.SpecNegRef{},
				},
			},
			ns:          namespace,
			svc:         svcName,
			subnet:      "unset-subnet",
			port:        svcPort,
			customNEG:   true,
			expectedErr: ErrNBNamerNotImplemented,
		},
		{
			desc: "BackendRef validation: wrong namespace",
			binding: &negbindingv1beta1.NetworkEndpointGroupBinding{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      name,
				},
				Spec: negbindingv1beta1.NetworkEndpointGroupBindingSpec{
					BackendRef: &negbindingv1beta1.BackendRefConfig{
						Name: svcName,
						Port: svcPort,
					},
					NetworkEndpointGroups: []negbindingv1beta1.SpecNegRef{
						{
							Subnet: subnetName,
							Name:   negName,
						},
					},
				},
			},
			ns:          "wrong-ns",
			svc:         svcName,
			subnet:      subnetName,
			port:        svcPort,
			expectedErr: ErrNBNamerInvalidBackendRef,
		},
		{
			desc: "BackendRef validation: wrong service name",
			binding: &negbindingv1beta1.NetworkEndpointGroupBinding{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      name,
				},
				Spec: negbindingv1beta1.NetworkEndpointGroupBindingSpec{
					BackendRef: &negbindingv1beta1.BackendRefConfig{
						Name: svcName,
						Port: svcPort,
					},
					NetworkEndpointGroups: []negbindingv1beta1.SpecNegRef{
						{
							Subnet: subnetName,
							Name:   negName,
						},
					},
				},
			},
			ns:          namespace,
			svc:         "wrong-svc",
			subnet:      subnetName,
			port:        svcPort,
			expectedErr: ErrNBNamerInvalidBackendRef,
		},
		{
			desc: "BackendRef validation: wrong port",
			binding: &negbindingv1beta1.NetworkEndpointGroupBinding{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      name,
				},
				Spec: negbindingv1beta1.NetworkEndpointGroupBindingSpec{
					BackendRef: &negbindingv1beta1.BackendRefConfig{
						Name: svcName,
						Port: svcPort,
					},
					NetworkEndpointGroups: []negbindingv1beta1.SpecNegRef{
						{
							Subnet: subnetName,
							Name:   negName,
						},
					},
				},
			},
			ns:          namespace,
			svc:         svcName,
			subnet:      subnetName,
			port:        999,
			expectedErr: ErrNBNamerInvalidBackendRef,
		},
		{
			desc: "Unexpected object type in cache",
			binding: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      name,
				},
			},
			ns:           namespace,
			svc:          svcName,
			subnet:       subnetName,
			port:         svcPort,
			expectAnyErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			negBindingLister := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
			if tc.binding != nil {
				if err := negBindingLister.Add(tc.binding); err != nil {
					t.Fatalf("Failed to add object to store: %v", err)
				}
			}
			namer := NewNegBindingNamer(namespace, name, negBindingLister)

			var gotNegName string
			var err error
			if tc.customNEG {
				gotNegName, err = namer.NonDefaultSubnetCustomNEG("custom-neg", tc.subnet)
			} else {
				gotNegName, err = namer.NonDefaultSubnetNEG(tc.ns, tc.svc, tc.subnet, tc.port)
			}

			if tc.expectedErr != nil {
				if err == nil {
					t.Fatalf("Expected error %v, got nil", tc.expectedErr)
				}
				if !errors.Is(err, tc.expectedErr) {
					t.Errorf("Expected error %v, got %v", tc.expectedErr, err)
				}
				return
			}

			if tc.expectAnyErr {
				if err == nil {
					t.Errorf("Expected error for unexpected object type in cache, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if gotNegName != tc.expectedNEG {
				t.Errorf("Expected NEG name %s, got %s", tc.expectedNEG, gotNegName)
			}
		})
	}
}

func TestNegBindingNamerStatusLookup(t *testing.T) {
	namespace := "test-ns"
	name := "test-name"
	svcName := "svc-name"
	svcPort := int32(80)

	subnetName := "subnet-name"
	negName := "neg-name"

	testCases := []struct {
		desc        string
		binding     *negbindingv1beta1.NetworkEndpointGroupBinding
		subnet      string
		expectedNEG string
	}{
		{
			desc: "Matches both Spec and Status",
			binding: &negbindingv1beta1.NetworkEndpointGroupBinding{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      name,
				},
				Spec: negbindingv1beta1.NetworkEndpointGroupBindingSpec{
					BackendRef: &negbindingv1beta1.BackendRefConfig{
						Name: svcName,
						Port: svcPort,
					},
					NetworkEndpointGroups: []negbindingv1beta1.SpecNegRef{
						{
							Subnet: subnetName,
							Name:   negName,
						},
					},
				},
				Status: negbindingv1beta1.NetworkEndpointGroupBindingStatus{
					NetworkEndpointGroups: []negbindingv1beta1.StatusNegRef{
						{
							ResourceURL: "https://www.googleapis.com/compute/v1/projects/mock-project/zones/us-central1-a/networkEndpointGroups/" + negName,
							SubnetURL:   "https://www.googleapis.com/compute/v1/projects/mock-project/regions/us-central1/subnetworks/" + subnetName,
						},
					},
				},
			},
			subnet:      subnetName,
			expectedNEG: negName,
		},
		{
			desc: "Conflicts between Spec and Status (Status priority)",
			binding: &negbindingv1beta1.NetworkEndpointGroupBinding{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      name,
				},
				Spec: negbindingv1beta1.NetworkEndpointGroupBindingSpec{
					BackendRef: &negbindingv1beta1.BackendRefConfig{
						Name: svcName,
						Port: svcPort,
					},
					NetworkEndpointGroups: []negbindingv1beta1.SpecNegRef{
						{
							Subnet: subnetName,
							Name:   "new-neg-name",
						},
					},
				},
				Status: negbindingv1beta1.NetworkEndpointGroupBindingStatus{
					NetworkEndpointGroups: []negbindingv1beta1.StatusNegRef{
						{
							ResourceURL: "https://www.googleapis.com/compute/v1/projects/mock-project/zones/us-central1-a/networkEndpointGroups/" + negName,
							SubnetURL:   "https://www.googleapis.com/compute/v1/projects/mock-project/regions/us-central1/subnetworks/" + subnetName,
						},
					},
				},
			},
			subnet:      subnetName,
			expectedNEG: negName,
		},
		{
			desc: "Removed from Spec (only exists in Status)",
			binding: &negbindingv1beta1.NetworkEndpointGroupBinding{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      name,
				},
				Spec: negbindingv1beta1.NetworkEndpointGroupBindingSpec{
					BackendRef: &negbindingv1beta1.BackendRefConfig{
						Name: svcName,
						Port: svcPort,
					},
					NetworkEndpointGroups: []negbindingv1beta1.SpecNegRef{},
				},
				Status: negbindingv1beta1.NetworkEndpointGroupBindingStatus{
					NetworkEndpointGroups: []negbindingv1beta1.StatusNegRef{
						{
							ResourceURL: "https://www.googleapis.com/compute/v1/projects/mock-project/zones/us-central1-a/networkEndpointGroups/" + negName,
							SubnetURL:   "https://www.googleapis.com/compute/v1/projects/mock-project/regions/us-central1/subnetworks/" + subnetName,
						},
					},
				},
			},
			subnet:      subnetName,
			expectedNEG: negName,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			negBindingLister := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
			if err := negBindingLister.Add(tc.binding); err != nil {
				t.Fatalf("Failed to add NEGBinding in store: %v", err)
			}
			namer := NewNegBindingNamer(namespace, name, negBindingLister)

			gotNegName, err := namer.NonDefaultSubnetNEG(namespace, svcName, tc.subnet, svcPort)
			if err != nil {
				t.Fatalf("NonDefaultSubnetNEG(%s) returned unexpected error: %v", tc.subnet, err)
			}
			if gotNegName != tc.expectedNEG {
				t.Errorf("NonDefaultSubnetNEG(%s) = %s, expected %s", tc.subnet, gotNegName, tc.expectedNEG)
			}
		})
	}
}
