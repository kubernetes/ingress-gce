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

	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/ingress-gce/pkg/e2e"
	"k8s.io/ingress-gce/pkg/fuzz"
	"k8s.io/ingress-gce/pkg/fuzz/features"
	"k8s.io/ingress-gce/pkg/utils"
)

func TestFinalizer(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	port80 := intstr.FromInt(80)
	ing := fuzz.NewIngressBuilder("", "ingress-1", "").
		AddPath("foo.com", "/", "service-1", port80).
		SetIngressClass("gce").
		Build()

	Framework.RunWithSandbox("finalizer", t, func(t *testing.T, s *e2e.Sandbox) {
		_, err := e2e.CreateEchoService(s, "service-1", nil)
		if err != nil {
			t.Fatalf("error creating echo service: %v", err)
		}
		t.Logf("Echo service created (%s/%s)", s.Namespace, "service-1")

		crud := e2e.IngressCRUD{C: Framework.Clientset}
		ing.Namespace = s.Namespace
		if _, err := crud.Create(ing); err != nil {
			t.Fatalf("error creating Ingress spec: %v", err)
		}
		t.Logf("Ingress created (%s/%s)", s.Namespace, ing.Name)

		ing, err := e2e.WaitForIngress(s, ing, nil)
		if err != nil {
			t.Fatalf("error waiting for Ingress to stabilize: %v", err)
		}
		t.Logf("GCLB resources created (%s/%s)", s.Namespace, ing.Name)

		// Perform whitebox testing.
		if len(ing.Status.LoadBalancer.Ingress) < 1 {
			t.Fatalf("Ingress does not have an IP: %+v", ing.Status)
		}

		ingFinalizers := ing.GetFinalizers()
		if len(ingFinalizers) != 1 || ingFinalizers[0] != utils.FinalizerKey {
			t.Fatalf("GetFinalizers() = %+v, want [%s]", ingFinalizers, utils.FinalizerKey)
		}

		vip := ing.Status.LoadBalancer.Ingress[0].IP
		t.Logf("Ingress %s/%s VIP = %s", s.Namespace, ing.Name, vip)
		gclb, err := fuzz.GCLBForVIP(context.Background(), Framework.Cloud, vip, fuzz.FeatureValidators(features.All))
		if err != nil {
			t.Fatalf("Error getting GCP resources for LB with IP = %q: %v", vip, err)
		}

		if err = e2e.CheckGCLB(gclb, 1, 2); err != nil {
			t.Error(err)
		}

		deleteOptions := &fuzz.GCLBDeleteOptions{
			SkipDefaultBackend: true,
		}
		if err := e2e.WaitForIngressDeletion(ctx, gclb, s, ing, deleteOptions); err != nil {
			t.Errorf("e2e.WaitForIngressDeletion(..., %q, nil) = %v, want nil", ing.Name, err)
		}
	})
}

func TestFinalizerIngressClassChange(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	port80 := intstr.FromInt(80)
	ing := fuzz.NewIngressBuilder("", "ingress-1", "").
		AddPath("foo.com", "/", "service-1", port80).
		SetIngressClass("gce").
		Build()

	Framework.RunWithSandbox("finalizer-ingress-class-change", t, func(t *testing.T, s *e2e.Sandbox) {
		_, err := e2e.CreateEchoService(s, "service-1", nil)
		if err != nil {
			t.Fatalf("error creating echo service: %v", err)
		}
		t.Logf("Echo service created (%s/%s)", s.Namespace, "service-1")

		crud := e2e.IngressCRUD{C: Framework.Clientset}
		ing.Namespace = s.Namespace
		if _, err := crud.Create(ing); err != nil {
			t.Fatalf("error creating Ingress spec: %v", err)
		}
		t.Logf("Ingress created (%s/%s)", s.Namespace, ing.Name)

		ing, err := e2e.WaitForIngress(s, ing, nil)
		if err != nil {
			t.Fatalf("error waiting for Ingress to stabilize: %v", err)
		}
		t.Logf("GCLB resources created (%s/%s)", s.Namespace, ing.Name)

		ingFinalizers := ing.GetFinalizers()
		if len(ingFinalizers) != 1 || ingFinalizers[0] != utils.FinalizerKey {
			t.Fatalf("GetFinalizers() = %+v, want [%q]", ingFinalizers, utils.FinalizerKey)
		}

		// Perform whitebox testing.
		if len(ing.Status.LoadBalancer.Ingress) < 1 {
			t.Fatalf("Ingress does not have an IP: %+v", ing.Status)
		}

		vip := ing.Status.LoadBalancer.Ingress[0].IP
		t.Logf("Ingress %s/%s VIP = %s", s.Namespace, ing.Name, vip)
		gclb, err := fuzz.GCLBForVIP(context.Background(), Framework.Cloud, vip, fuzz.FeatureValidators(features.All))
		if err != nil {
			t.Fatalf("Error getting GCP resources for LB with IP = %q: %v", vip, err)
		}

		if err = e2e.CheckGCLB(gclb, 1, 2); err != nil {
			t.Error(err)
		}

		// Change Ingress class
		newIngClass := "nginx"
		ing = fuzz.NewIngressBuilderFromExisting(ing).SetIngressClass(newIngClass).Build()

		if _, err := crud.Update(ing); err != nil {
			t.Fatalf("error updating Ingress spec: %v", err)
		}
		t.Logf("Ingress (%s/%s) class changed to %s", s.Namespace, ing.Name, newIngClass)

		deleteOptions := &fuzz.GCLBDeleteOptions{
			SkipDefaultBackend: true,
		}
		if err := e2e.WaitForFinalizerDeletion(ctx, gclb, s, ing.Name, deleteOptions); err != nil {
			t.Errorf("e2e.WaitForFinalizerDeletion(...) = %v, want nil", err)
		}
		t.Logf("Finalizer for Ingress (%s/%s) deleted", s.Namespace, ing.Name)

		if err := e2e.WaitForIngressDeletion(ctx, gclb, s, ing, deleteOptions); err != nil {
			t.Errorf("e2e.WaitForIngressDeletion(..., %q, nil) = %v, want nil", ing.Name, err)
		}
	})
}
