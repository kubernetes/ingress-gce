/*
Copyright 2019 The Kubernetes Authors.

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

package main

import (
	"context"
	"testing"

	v1 "k8s.io/api/networking/v1"
	"k8s.io/ingress-gce/pkg/e2e"
	"k8s.io/ingress-gce/pkg/e2e/adapter"
	"k8s.io/ingress-gce/pkg/fuzz"
	"k8s.io/ingress-gce/pkg/utils/common"
)

// TestFinalizer asserts that finalizer is added/ removed appropriately during the life cycle
// of an ingress resource.
func TestFinalizer(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	port80 := v1.ServiceBackendPort{Number: 80}
	svcName := "service-1"
	ing := fuzz.NewIngressBuilder("", "ingress-1", "").
		AddPath("foo.com", "/", svcName, port80).
		SetIngressClass("gce").
		Build()

	Framework.RunWithSandbox("finalizer", t, func(t *testing.T, s *e2e.Sandbox) {
		_, err := e2e.CreateEchoService(s, svcName, nil)
		if err != nil {
			t.Fatalf("CreateEchoService(_, %q, nil): %v, want nil", svcName, err)
		}
		t.Logf("Echo service created (%s/%s)", s.Namespace, svcName)

		crud := adapter.IngressCRUD{C: Framework.Clientset}
		ing.Namespace = s.Namespace
		ingKey := common.NamespacedName(ing)
		if _, err := crud.Create(ing); err != nil {
			t.Fatalf("create(%s) = %v, want nil; Ingress: %v", ingKey, err, ing)
		}
		t.Logf("Ingress created (%s)", ingKey)

		ing, err = e2e.WaitForIngress(s, ing, nil, &e2e.WaitForIngressOptions{ExpectUnreachable: true})
		if err != nil {
			t.Fatalf("error waiting for Ingress %s to stabilize: %v", ingKey, err)
		}

		if err := e2e.CheckForAnyFinalizer(ing); err != nil {
			t.Fatalf("CheckForAnyFinalizer(%s) = %v, want nil", ingKey, err)
		}

		// Perform whitebox testing.
		gclb, err := e2e.WhiteboxTest(ing, nil, Framework.Cloud, "", s)
		if err != nil {
			t.Fatalf("e2e.WhiteboxTest(%s, ...) = %v, want nil", ingKey, err)
		}

		deleteOptions := &fuzz.GCLBDeleteOptions{
			SkipDefaultBackend: true,
		}
		if err := e2e.WaitForIngressDeletion(ctx, gclb, s, ing, deleteOptions); err != nil {
			t.Errorf("e2e.WaitForIngressDeletion(..., %q, nil) = %v, want nil", ingKey, err)
		}
	})
}

// TestFinalizerIngressClassChange asserts that finalizer is removed from ingress after its
// associated resources are cleaned up when ingress class is updated to a non glbc type.
func TestFinalizerIngressClassChange(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	port80 := v1.ServiceBackendPort{Number: 80}
	svcName := "service-1"
	ing := fuzz.NewIngressBuilder("", "ingress-1", "").
		AddPath("foo.com", "/", svcName, port80).
		SetIngressClass("gce").
		Build()

	Framework.RunWithSandbox("finalizer-ingress-class-change", t, func(t *testing.T, s *e2e.Sandbox) {
		_, err := e2e.CreateEchoService(s, svcName, nil)
		if err != nil {
			t.Fatalf("CreateEchoService(_, %q, nil): %v, want nil", svcName, err)
		}
		t.Logf("Echo service created (%s/%s)", s.Namespace, svcName)

		crud := adapter.IngressCRUD{C: Framework.Clientset}
		ing.Namespace = s.Namespace
		ingKey := common.NamespacedName(ing)
		if _, err := crud.Create(ing); err != nil {
			t.Fatalf("create(%s) = %v, want nil; Ingress: %v", ingKey, err, ing)
		}
		t.Logf("Ingress created (%s)", ingKey)
		ing, err := e2e.WaitForIngress(s, ing, nil, &e2e.WaitForIngressOptions{ExpectUnreachable: true})
		if err != nil {
			t.Fatalf("error waiting for Ingress %s to stabilize: %v", ingKey, err)
		}

		if err := e2e.CheckForAnyFinalizer(ing); err != nil {
			t.Fatalf("CheckForAnyFinalizer(%s) = %v, want nil", ingKey, err)
		}

		// Perform whitebox testing.
		gclb, err := e2e.WhiteboxTest(ing, nil, Framework.Cloud, "", s)
		if err != nil {
			t.Fatalf("e2e.WhiteboxTest(%s, ...) = %v, want nil", ingKey, err)
		}

		// Change Ingress class
		newIngClass := "nginx"
		newIng := fuzz.NewIngressBuilderFromExisting(ing).SetIngressClass(newIngClass).Build()

		if _, err := crud.Patch(ing, newIng); err != nil {
			t.Fatalf("Patch(%s) = %v, want nil; current ingress: %+v new ingress %+v", ingKey, err, ing, newIng)
		}
		t.Logf("Ingress (%s) class changed to %s", ingKey, newIngClass)

		deleteOptions := &fuzz.GCLBDeleteOptions{
			SkipDefaultBackend: true,
		}
		if err := e2e.WaitForFinalizerDeletion(ctx, gclb, s, ing.Name, deleteOptions); err != nil {
			t.Errorf("e2e.WaitForFinalizerDeletion(..., %q, _) = %v, want nil", ing.Name, err)
		}
		t.Logf("Finalizer for Ingress (%s) deleted", ingKey)
	})
}

// TestFinalizerIngressesWithSharedResources asserts that sync does not return error with finalizer
// when multiple ingresses with shared resources are created or deleted.
func TestFinalizerIngressesWithSharedResources(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	port80 := v1.ServiceBackendPort{Number: 80}
	svcName := "service-1"
	ing := fuzz.NewIngressBuilder("", "ingress-1", "").
		AddPath("foo.com", "/", svcName, port80).
		SetIngressClass("gce").
		Build()
	otherIng := fuzz.NewIngressBuilder("", "ingress-2", "").
		AddPath("foo.com", "/", svcName, port80).
		SetIngressClass("gce").
		Build()

	Framework.RunWithSandbox("finalizer-ingress-class-change", t, func(t *testing.T, s *e2e.Sandbox) {
		_, err := e2e.CreateEchoService(s, svcName, nil)
		if err != nil {
			t.Fatalf("CreateEchoService(_, %q, nil): %v, want nil", svcName, err)
		}
		t.Logf("Echo service created (%s/%s)", s.Namespace, svcName)

		crud := adapter.IngressCRUD{C: Framework.Clientset}
		ing.Namespace = s.Namespace
		ingKey := common.NamespacedName(ing)
		if _, err := crud.Create(ing); err != nil {
			t.Fatalf("create(%s) = %v, want nil; Ingress: %v", ingKey, err, ing)
		}
		t.Logf("Ingress created (%s)", ingKey)

		otherIng.Namespace = s.Namespace
		otherIngKey := common.NamespacedName(ing)
		if _, err := crud.Create(otherIng); err != nil {
			t.Fatalf("create(%s) = %v, want nil; Ingress: %v", otherIngKey, err, otherIng)
		}
		t.Logf("Ingress created (%s)", otherIngKey)

		if ing, err = e2e.WaitForIngress(s, ing, nil, &e2e.WaitForIngressOptions{ExpectUnreachable: true}); err != nil {
			t.Fatalf("error waiting for Ingress %s to stabilize: %v", ingKey, err)
		}
		if otherIng, err = e2e.WaitForIngress(s, otherIng, nil, &e2e.WaitForIngressOptions{ExpectUnreachable: true}); err != nil {
			t.Fatalf("error waiting for Ingress %s to stabilize: %v", otherIngKey, err)
		}

		if err := e2e.CheckForAnyFinalizer(ing); err != nil {
			t.Fatalf("CheckForAnyFinalizer(%s) = %v, want nil", ingKey, err)
		}
		if err := e2e.CheckForAnyFinalizer(otherIng); err != nil {
			t.Fatalf("CheckForAnyFinalizer(%s) = %v, want nil", otherIngKey, err)
		}

		// Perform whitebox testing.
		gclb, err := e2e.WhiteboxTest(ing, nil, Framework.Cloud, "", s)
		if err != nil {
			t.Fatalf("e2e.WhiteboxTest(%s, ...) = %v, want nil", ingKey, err)
		}
		otherGclb, err := e2e.WhiteboxTest(otherIng, nil, Framework.Cloud, "", s)
		if err != nil {
			t.Fatalf("e2e.WhiteboxTest(%s)", otherIngKey)
		}

		// SkipBackends ensure that we dont wait on deletion of shared backends.
		deleteOptions := &fuzz.GCLBDeleteOptions{
			SkipDefaultBackend: true,
			SkipBackends:       true,
		}
		if err := e2e.WaitForIngressDeletion(ctx, gclb, s, ing, deleteOptions); err != nil {
			t.Errorf("e2e.WaitForIngressDeletion(..., %q, nil) = %v, want nil", ingKey, err)
		}
		deleteOptions = &fuzz.GCLBDeleteOptions{
			SkipDefaultBackend: true,
		}
		if err := e2e.WaitForIngressDeletion(ctx, otherGclb, s, otherIng, deleteOptions); err != nil {
			t.Errorf("e2e.WaitForIngressDeletion(..., %q, nil) = %v, want nil", otherIngKey, err)
		}
	})
}
