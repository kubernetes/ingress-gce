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
	subnetName2 := "subnet-name-2"
	negName2 := "neg-name-2"

	negBindingLister := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	namer := NewNegBindingNamer(namespace, name, negBindingLister)

	// NEGBinding not in store
	_, err := namer.NonDefaultSubnetNEG(namespace, svcName, subnetName, svcPort)
	if err == nil {
		t.Errorf("Expected error when NEGBinding is not in store, got nil")
	} else if !errors.Is(err, ErrNegBindingNotFound) {
		t.Errorf("Expected error to be ErrNegBindingNotFound, got %v", err)
	}

	// Add NEGBinding to store
	binding := &negbindingv1beta1.NetworkEndpointGroupBinding{
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
				{
					Subnet: subnetName2,
					Name:   negName2,
				},
			},
		},
	}
	err = negBindingLister.Add(binding)
	if err != nil {
		t.Fatalf("Failed to add NEGBinding to store: %v", err)
	}

	// Subnet in the NEGBinding's spec
	gotNegName, err := namer.NonDefaultSubnetNEG(namespace, svcName, subnetName, svcPort)
	if err != nil {
		t.Errorf("NonDefaultSubnetNEG: Unexpected error: %v", err)
	}
	if gotNegName != negName {
		t.Errorf("NonDefaultSubnetNEG: Expected %s, got %s", negName, gotNegName)
	}

	gotNegName, err = namer.NonDefaultSubnetNEG(namespace, svcName, subnetName2, svcPort)
	if err != nil {
		t.Errorf("NonDefaultSubnetNEG: Unexpected error: %v", err)
	}
	if gotNegName != negName2 {
		t.Errorf("NonDefaultSubnetNEG: Expected %s, got %s", negName2, gotNegName)
	}

	_, err = namer.NonDefaultSubnetCustomNEG("custom-neg", subnetName)
	if err == nil {
		t.Errorf("NonDefaultSubnetCustomNEG: Expected error, got nil")
	} else if !errors.Is(err, ErrNBNamerNotImplemented) {
		t.Errorf("Expected error to be ErrNBNamerNotImplemented, got %v", err)
	}

	// Subnet not on in the NEGBinding's spec
	_, err = namer.NonDefaultSubnetNEG(namespace, svcName, "unset-subnet", svcPort)
	if err == nil {
		t.Errorf("NonDefaultSubnetNEG: Expected error for subnet which not set in NEGBinding spec, got nil")
	} else if !errors.Is(err, ErrNegNameNotFound) {
		t.Errorf("Expected error to be ErrNegNameNotFound, got %v", err)
	}

	_, err = namer.NonDefaultSubnetCustomNEG("custom-neg", "unset-subnet")
	if err == nil {
		t.Errorf("NonDefaultSubnetCustomNEG: Expected error for subnet which not set in NEGBinding spec, got nil")
	} else if !errors.Is(err, ErrNBNamerNotImplemented) {
		t.Errorf("Expected error to be ErrNBNamerNotImplemented, got %v", err)
	}

	// BackendRef validation
	_, err = namer.NonDefaultSubnetNEG("wrong-ns", svcName, subnetName, svcPort)
	if err == nil {
		t.Errorf("Expected error for wrong namespace, got nil")
	} else if !errors.Is(err, ErrNBNamerInvalidBackendRef) {
		t.Errorf("Expected error to be ErrNBNamerInvalidBackendRef, got %v", err)
	}

	_, err = namer.NonDefaultSubnetNEG(namespace, "wrong-svc", subnetName, svcPort)
	if err == nil {
		t.Errorf("Expected error for wrong service name, got nil")
	} else if !errors.Is(err, ErrNBNamerInvalidBackendRef) {
		t.Errorf("Expected error to be ErrNBNamerInvalidBackendRef, got %v", err)
	}

	_, err = namer.NonDefaultSubnetNEG(namespace, svcName, subnetName, 999)
	if err == nil {
		t.Errorf("Expected error for wrong port, got nil")
	} else if !errors.Is(err, ErrNBNamerInvalidBackendRef) {
		t.Errorf("Expected error to be ErrNBNamerInvalidBackendRef, got %v", err)
	}

	// Test cases for Status lookup (conflict resolution and removed subnets)
	// Reset binding in store with status
	bindingWithStatus := &negbindingv1beta1.NetworkEndpointGroupBinding{
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
					Name:   negName, // Matches status
				},
				{
					Subnet: subnetName2,
					Name:   "new-neg-name-2", // Conflicts with status (status has neg-name-2)
				},
				// subnetName3 is removed from Spec (only exists in status)
			},
		},
		Status: negbindingv1beta1.NetworkEndpointGroupBindingStatus{
			NetworkEndpointGroups: []negbindingv1beta1.StatusNegRef{
				{
					ResourceURL: "https://www.googleapis.com/compute/v1/projects/mock-project/zones/us-central1-a/networkEndpointGroups/" + negName,
					SubnetURL:   "https://www.googleapis.com/compute/v1/projects/mock-project/regions/us-central1/subnetworks/" + subnetName,
				},
				{
					ResourceURL: "https://www.googleapis.com/compute/v1/projects/mock-project/zones/us-central1-a/networkEndpointGroups/" + negName2, // neg-name-2
					SubnetURL:   "https://www.googleapis.com/compute/v1/projects/mock-project/regions/us-central1/subnetworks/" + subnetName2,
				},
				{
					ResourceURL: "https://www.googleapis.com/compute/v1/projects/mock-project/zones/us-central1-a/networkEndpointGroups/neg-name-3",
					SubnetURL:   "https://www.googleapis.com/compute/v1/projects/mock-project/regions/us-central1/subnetworks/subnet-name-3",
				},
			},
		},
	}
	err = negBindingLister.Update(bindingWithStatus)
	if err != nil {
		t.Fatalf("Failed to update NEGBinding in store: %v", err)
	}

	// Case 1: Matches both Spec and Status -> should return Spec/Status name (neg-name)
	gotNegName, err = namer.NonDefaultSubnetNEG(namespace, svcName, subnetName, svcPort)
	if err != nil {
		t.Errorf("Status lookup (match): Unexpected error: %v", err)
	}
	if gotNegName != negName {
		t.Errorf("Status lookup (match): Expected %s, got %s", negName, gotNegName)
	}

	// Case 2: Conflicts (Spec has new-neg-name-2, Status has neg-name-2) -> should return Status name (neg-name-2)
	gotNegName, err = namer.NonDefaultSubnetNEG(namespace, svcName, subnetName2, svcPort)
	if err != nil {
		t.Errorf("Status lookup (conflict): Unexpected error: %v", err)
	}
	if gotNegName != negName2 {
		t.Errorf("Status lookup (conflict): Expected %s (from status), got %s", negName2, gotNegName)
	}

	// Case 3: Removed from Spec (only in Status subnet-name-3) -> should return Status name (neg-name-3)
	gotNegName, err = namer.NonDefaultSubnetNEG(namespace, svcName, "subnet-name-3", svcPort)
	if err != nil {
		t.Errorf("Status lookup (removed): Unexpected error: %v", err)
	}
	if gotNegName != "neg-name-3" {
		t.Errorf("Status lookup (removed): Expected neg-name-3, got %s", gotNegName)
	}

	// Unexpected object type in cache
	invalidObj := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
	}
	err = negBindingLister.Update(invalidObj)
	if err != nil {
		t.Fatalf("Failed to update NEGBinding store with invalid object: %v", err)
	}

	_, err = namer.NonDefaultSubnetNEG(namespace, svcName, subnetName, svcPort)
	if err == nil {
		t.Errorf("Expected error for unexpected object type in cache, got nil")
	}
}
