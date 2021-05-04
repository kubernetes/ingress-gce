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
	"fmt"
	"testing"

	v1 "k8s.io/api/networking/v1"
	"k8s.io/ingress-gce/pkg/e2e"
	"k8s.io/ingress-gce/pkg/e2e/adapter"
	"k8s.io/ingress-gce/pkg/fuzz"
	"k8s.io/ingress-gce/pkg/utils/common"
)

// TestV2FrontendNamer tests basic lifecycle of an ingress with v1/v2 frontend naming scheme
// when v2 naming policy is enabled. This test adds v1 finalizer manually to generate an ingress
// with  v1 naming scheme.
func TestV2FrontendNamer(t *testing.T) {
	t.Parallel()
	port80 := v1.ServiceBackendPort{Number: 80}
	svcName := "service-1"
	v1Ing := fuzz.NewIngressBuilder("", "ing-v1", "").
		AddPath("foo.com", "/", svcName, port80).
		Build()
	v1Ing.SetFinalizers([]string{common.FinalizerKey})
	v2Ing := fuzz.NewIngressBuilder("", "ing-v2", "").
		AddPath("foo.com", "/", svcName, port80).
		Build()

	for _, tc := range []struct {
		desc string
		ings []*v1.Ingress
	}{
		{"v2 only", []*v1.Ingress{v2Ing}},
		{"v1 only", []*v1.Ingress{v1Ing}},
		// The following test cases create ingresses with both naming schemes. These
		// test cases assert that GC of v1 naming scheme does not affect ingress with
		// v2 naming scheme and vice-versa.
		// Note that the first element in the list of ingresses is deleted first,
		// which tests that GC of the naming scheme for first ingress does not affect
		// other naming scheme.
		{"both v1 and v2, delete v1 first", []*v1.Ingress{v1Ing, v2Ing}},
		{"both v1 and v2, delete v2 first", []*v1.Ingress{v2Ing, v1Ing}},
	} {
		tc := tc
		desc := fmt.Sprintf("v2 frontend namer %s", tc.desc)
		Framework.RunWithSandbox(desc, t, func(t *testing.T, s *e2e.Sandbox) {
			t.Parallel()
			ctx := context.Background()

			if _, err := e2e.CreateEchoService(s, svcName, nil); err != nil {
				t.Fatalf("CreateEchoService(_, %q, nil): %v, want nil", svcName, err)
			}
			t.Logf("Echo service created (%s/%s)", s.Namespace, svcName)

			crud := adapter.IngressCRUD{C: Framework.Clientset}
			var gclbs []*fuzz.GCLB
			var updatedIngs []*v1.Ingress

			// Create Ingresses.
			for _, ing := range tc.ings {
				ing = ing.DeepCopy()
				ing.Namespace = s.Namespace
				if _, err := crud.Create(ing); err != nil {
					t.Fatalf("create(%s/%s) = %v, want nil; Ingress: %v", ing.Namespace, ing.Name, err, ing)
				}
				t.Logf("Ingress created (%s/%s)", ing.Namespace, ing.Name)
			}
			// Wait for ingress to stabilize and perform whitebox testing.
			for _, ing := range tc.ings {
				ingKey := fmt.Sprintf(s.Namespace, ing.Name)
				// Determine the expected finalizer after ingress creation.
				isV1Finalizer := false
				if len(ing.GetFinalizers()) > 0 {
					isV1Finalizer = true
				}
				var err error
				if ing, err = e2e.WaitForIngress(s, ing, nil, &e2e.WaitForIngressOptions{ExpectUnreachable: true}); err != nil {
					t.Fatalf("error waiting for Ingress %s to stabilize: %v", ingKey, err)
				}
				if isV1Finalizer {
					// Assert that v1 finalizer is added.
					if err := e2e.CheckV1Finalizer(ing); err != nil {
						t.Fatalf("CheckV1Finalizer(%s) = %v, want nil", ingKey, err)
					}
				} else {
					// Assert that v2 finalizer is added.
					if err := e2e.CheckV2Finalizer(ing); err != nil {
						t.Fatalf("CheckV2Finalizer(%s) = %v, want nil", ingKey, err)
					}
				}
				// Perform whitebox testing. This also tests naming scheme.
				gclb, err := e2e.WhiteboxTest(ing, nil, Framework.Cloud, "", s)
				if err != nil {
					t.Fatalf("e2e.WhiteboxTest(%s, ...) = %v, want nil", ingKey, err)
				}
				gclbs = append(gclbs, gclb)
				updatedIngs = append(updatedIngs, ing)
			}
			// Return immediately if there are no ingresses.
			if updatedIngs == nil || len(updatedIngs) == 0 {
				return
			}
			ingCount := len(updatedIngs)

			// Delete the first ingress. After deleting the first ingress, assert that
			// the resources of remaining ingress are unaffected.
			deleteOptions := &fuzz.GCLBDeleteOptions{
				SkipDefaultBackend: true,
			}
			// Skip checking deletion for backends of first ingress as backends are shared.
			if ingCount > 1 {
				deleteOptions.SkipBackends = true
			}
			// Wait for Ingress and GCLB resources to be deleted for first ingress.
			if err := e2e.WaitForIngressDeletion(ctx, gclbs[0], s, updatedIngs[0], deleteOptions); err != nil {
				t.Errorf("e2e.WaitForIngressDeletion(..., %q, nil) = %v, want nil", common.NamespacedName(updatedIngs[0]), err)
			}
			// Return if there are no more ingresses to check.
			if ingCount < 2 {
				return
			}
			ingKey := common.NamespacedName(updatedIngs[1])
			// Verify that GCLB resources of second ingress are intact.
			gclb, err := e2e.WhiteboxTest(updatedIngs[1], nil, Framework.Cloud, "", s)
			if err != nil {
				t.Fatalf("e2e.WhiteboxTest(%s, ...) = %v, want nil", ingKey, err)
			}
			// Delete the second ingress.
			deleteOptions.SkipBackends = false
			if err := e2e.WaitForIngressDeletion(ctx, gclb, s, updatedIngs[1], deleteOptions); err != nil {
				t.Errorf("e2e.WaitForIngressDeletion(..., %q, nil) = %v, want nil", ingKey, err)
			}
		})
	}
}
