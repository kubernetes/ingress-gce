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
	"k8s.io/api/networking/v1beta1"
	"k8s.io/ingress-gce/pkg/fuzz/features"
	"testing"

	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/ingress-gce/pkg/e2e"
	"k8s.io/ingress-gce/pkg/fuzz"
	"k8s.io/ingress-gce/pkg/utils/common"
)

// TestFinalizer asserts that finalizer is added/ removed appropriately during the life cycle
// of an ingress resource.
func TestFinalizer(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	port80 := intstr.FromInt(80)
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

		crud := e2e.IngressCRUD{C: Framework.Clientset}
		ing.Namespace = s.Namespace
		if _, err := crud.Create(ing); err != nil {
			t.Fatalf("create(%s/%s) = %v, want nil; Ingress: %v", ing.Namespace, ing.Name, err, ing)
		}
		t.Logf("Ingress created (%s/%s)", s.Namespace, ing.Name)
		ing = waitForStableIngress(true, ing, s, t)

		ingFinalizers := ing.GetFinalizers()
		if len(ingFinalizers) != 1 || ingFinalizers[0] != common.FinalizerKey {
			t.Fatalf("GetFinalizers() = %+v, want [%q]", ingFinalizers, common.FinalizerKey)
		}

		// Perform whitebox testing.
		gclb := whiteboxTest(ing, s, t, "")

		deleteOptions := &fuzz.GCLBDeleteOptions{
			SkipDefaultBackend: true,
		}
		if err := e2e.WaitForIngressDeletion(ctx, gclb, s, ing, deleteOptions); err != nil {
			t.Errorf("e2e.WaitForIngressDeletion(..., %q, nil) = %v, want nil", ing.Name, err)
		}
	})
}

// TestFinalizerIngressClassChange asserts that finalizer is removed from ingress after its
// associated resources are cleaned up when ingress class is updated to a non glbc type.
func TestFinalizerIngressClassChange(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	port80 := intstr.FromInt(80)
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

		crud := e2e.IngressCRUD{C: Framework.Clientset}
		ing.Namespace = s.Namespace
		if _, err := crud.Create(ing); err != nil {
			t.Fatalf("create(%s/%s) = %v, want nil; Ingress: %v", ing.Namespace, ing.Name, err, ing)
		}
		t.Logf("Ingress created (%s/%s)", s.Namespace, ing.Name)
		ing = waitForStableIngress(true, ing, s, t)

		ingFinalizers := ing.GetFinalizers()
		if len(ingFinalizers) != 1 || ingFinalizers[0] != common.FinalizerKey {
			t.Fatalf("GetFinalizers() = %+v, want [%q]", ingFinalizers, common.FinalizerKey)
		}

		// Perform whitebox testing.
		gclb := whiteboxTest(ing, s, t, "")

		// Change Ingress class
		newIngClass := "nginx"
		ing = fuzz.NewIngressBuilderFromExisting(ing).SetIngressClass(newIngClass).Build()

		if _, err := crud.Update(ing); err != nil {
			t.Fatalf("update(%s/%s) = %v, want nil; ingress: %v", ing.Namespace, ing.Name, err, ing)
		}
		t.Logf("Ingress (%s/%s) class changed to %s", s.Namespace, ing.Name, newIngClass)

		deleteOptions := &fuzz.GCLBDeleteOptions{
			SkipDefaultBackend: true,
		}
		if err := e2e.WaitForFinalizerDeletion(ctx, gclb, s, ing.Name, deleteOptions); err != nil {
			t.Errorf("e2e.WaitForFinalizerDeletion(..., %q, _) = %v, want nil", ing.Name, err)
		}
		t.Logf("Finalizer for Ingress (%s/%s) deleted", s.Namespace, ing.Name)
	})
}

// TestFinalizerIngressesWithSharedResources asserts that sync does not return error with finalizer
// when multiple ingresses with shared resources are created or deleted.
func TestFinalizerIngressesWithSharedResources(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	port80 := intstr.FromInt(80)
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

		crud := e2e.IngressCRUD{C: Framework.Clientset}
		ing.Namespace = s.Namespace
		if _, err := crud.Create(ing); err != nil {
			t.Fatalf("create(%s/%s) = %v, want nil; Ingress: %v", ing.Namespace, ing.Name, err, ing)
		}
		t.Logf("Ingress created (%s/%s)", s.Namespace, ing.Name)

		otherIng.Namespace = s.Namespace
		if _, err := crud.Create(otherIng); err != nil {
			t.Fatalf("create(%s/%s) = %v, want nil; Ingress: %v", otherIng.Namespace, otherIng.Name, err, otherIng)
		}
		t.Logf("Ingress created (%s/%s)", s.Namespace, otherIng.Name)

		ing = waitForStableIngress(true, ing, s, t)
		otherIng = waitForStableIngress(true, otherIng, s, t)

		ingFinalizers := ing.GetFinalizers()
		if len(ingFinalizers) != 1 || ingFinalizers[0] != common.FinalizerKey {
			t.Fatalf("GetFinalizers() = %+v, want [%q]", ingFinalizers, common.FinalizerKey)
		}
		otherIngFinalizers := otherIng.GetFinalizers()
		if len(otherIngFinalizers) != 1 || otherIngFinalizers[0] != common.FinalizerKey {
			t.Fatalf("GetFinalizers() = %+v, want [%q]", otherIngFinalizers, common.FinalizerKey)
		}

		// Perform whitebox testing.
		gclb := whiteboxTest(ing, s, t, "")
		otherGclb := whiteboxTest(otherIng, s, t, "")

		// SkipBackends ensure that we dont wait on deletion of shared backends.
		deleteOptions := &fuzz.GCLBDeleteOptions{
			SkipDefaultBackend: true,
			SkipBackends:       true,
		}
		if err := e2e.WaitForIngressDeletion(ctx, gclb, s, ing, deleteOptions); err != nil {
			t.Errorf("e2e.WaitForIngressDeletion(..., %q, nil) = %v, want nil", ing.Name, err)
		}
		deleteOptions = &fuzz.GCLBDeleteOptions{
			SkipDefaultBackend: true,
		}
		if err := e2e.WaitForIngressDeletion(ctx, otherGclb, s, otherIng, deleteOptions); err != nil {
			t.Errorf("e2e.WaitForIngressDeletion(..., %q, nil) = %v, want nil", otherIng.Name, err)
		}
	})
}

// TestUpdateTo1dot7 asserts that finalizer is added to an ingress when upgraded from a version
// without finalizer to version 1.7. Also, verifies that ingress is deleted with finalizer enabled.
// Note: The test is named in such a way that it does run as a normal test or an upgrade test for
// other versions.
func TestUpdateTo1dot7(t *testing.T) {
	port80 := intstr.FromInt(80)
	svcName := "service-1"
	ing := fuzz.NewIngressBuilder("", "ingress-1", "").
		AddPath("foo.com", "/", svcName, port80).
		SetIngressClass("gce").
		Build()
	Framework.RunWithSandbox("finalizer-master-upgrade", t, func(t *testing.T, s *e2e.Sandbox) {
		t.Parallel()

		_, err := e2e.CreateEchoService(s, svcName, nil)
		if err != nil {
			t.Fatalf("CreateEchoService(_, %q, nil): %v, want nil", svcName, err)
		}
		t.Logf("Echo service created (%s/%s)", s.Namespace, svcName)

		crud := e2e.IngressCRUD{C: Framework.Clientset}
		ing.Namespace = s.Namespace
		if _, err := crud.Create(ing); err != nil {
			t.Fatalf("create(%s/%s) = %v, want nil; Ingress: %v", ing.Namespace, ing.Name, err, ing)
		}
		t.Logf("Ingress created (%s/%s)", s.Namespace, ing.Name)
		ing = waitForStableIngress(true, ing, s, t)

		// Check that finalizer is not added in old version in which finalizer add is not enabled.
		ingFinalizers := ing.GetFinalizers()
		if l := len(ingFinalizers); l != 0 {
			t.Fatalf("GetFinalizers() = %d, want 0", l)
		}
		// Perform whitebox testing.
		whiteboxTest(ing, s, t, "")

		for {
			// While k8s master is upgrading, it will return a connection refused
			// error for any k8s resource we try to hit. We loop until the
			// master upgrade has finished.
			if s.MasterUpgrading() {
				continue
			}

			if s.MasterUpgraded() {
				t.Logf("Detected master upgrade")
				break
			}
		}

		// Wait for finalizer to be added and verify that correct finalizer is added to the ingress after the upgrade.
		if err := e2e.WaitForFinalizer(s, ing.Name); err != nil {
			t.Errorf("e2e.WaitForFinalizer(_, %q) = %v, want nil", ing.Name, err)
		}

		// Perform whitebox testing.
		gclb := whiteboxTest(ing, s, t, "")

		// If the Master has upgraded and the Ingress is stable,
		// we delete the Ingress and exit out of the loop to indicate that
		// the test is done.
		deleteOptions := &fuzz.GCLBDeleteOptions{
			SkipDefaultBackend: true,
		}
		if err := e2e.WaitForIngressDeletion(context.Background(), gclb, s, ing, deleteOptions); err != nil {
			t.Errorf("e2e.WaitForIngressDeletion(..., %q, nil) = %v, want nil", ing.Name, err)
		}
	})
}

func checkGCLB(t *testing.T, s *e2e.Sandbox, ing *v1beta1.Ingress, numForwardingRules, numBackendServices int) *fuzz.GCLB {
	// Perform whitebox testing.
	if len(ing.Status.LoadBalancer.Ingress) < 1 {
		t.Fatalf("Ingress does not have an IP: %+v", ing.Status)
	}
	vip := ing.Status.LoadBalancer.Ingress[0].IP
	t.Logf("Ingress %s/%s VIP = %s", s.Namespace, ing.Name, vip)
	params := &fuzz.GCLBForVIPParams{VIP: vip, Validators: fuzz.FeatureValidators(features.All)}
	gclb, err := fuzz.GCLBForVIP(context.Background(), Framework.Cloud, params)
	if err != nil {
		t.Fatalf("GCLBForVIP(..., %q, _) = %v, want nil; error getting GCP resources for LB with IP", vip, err)
	}

	if err = e2e.CheckGCLB(gclb, numForwardingRules, numBackendServices); err != nil {
		t.Error(err)
	}
	return gclb
}
