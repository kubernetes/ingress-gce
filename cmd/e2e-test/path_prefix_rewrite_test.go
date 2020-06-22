/*
Copyright 2020 The Kubernetes Authors.

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

	"k8s.io/apimachinery/pkg/util/intstr"

	"k8s.io/ingress-gce/pkg/e2e"
	"k8s.io/ingress-gce/pkg/e2e/adapter"
	"k8s.io/ingress-gce/pkg/fuzz"
	"k8s.io/ingress-gce/pkg/fuzz/features"
)

func TestPathPrefixRewrite(t *testing.T) {
	t.Parallel()

	port80 := intstr.FromInt(80)
	Framework.RunWithSandbox("with path prefix rewrite", t, func(t *testing.T, s *e2e.Sandbox) {
		t.Parallel()

		ctx := context.Background()

		_, err := e2e.CreateEchoServiceWithOS(s, "service-1", nil, e2e.Linux)
		if err != nil {
			t.Fatalf("error creating echo service: %v", err)
		}
		t.Logf("Echo service created (%s/%s)", s.Namespace, "service-1")

		testIng := fuzz.NewIngressBuilder(s.Namespace, "ingress-1", "").
			AddPath("test.com", "/foo", "service-1", port80).
			AddPath("test.com", "/bar", "service-1", port80).
			SetPathPrefixRewrite("/").
			Build()

		crud := adapter.IngressCRUD{C: Framework.Clientset}
		if _, err = crud.Create(testIng); err != nil {
			t.Fatalf("error creating Ingress spec: %v", err)
		}
		t.Logf("Ingress created (%s/%s)", s.Namespace, testIng.Name)

		ing, err := e2e.WaitForIngress(s, testIng, nil)
		if err != nil {
			t.Fatalf("error waiting for Ingress to stabilize: %v", err)
		}
		t.Logf("GCLB resources created (%s/%s)", s.Namespace, ing.Name)

		vip := ing.Status.LoadBalancer.Ingress[0].IP
		t.Logf("Ingress %s/%s VIP = %s", s.Namespace, ing.Name, vip)
		params := &fuzz.GCLBForVIPParams{VIP: vip, Validators: fuzz.FeatureValidators(features.All)}
		gclb, err := fuzz.GCLBForVIP(context.Background(), Framework.Cloud, params)
		if err != nil {
			t.Fatalf("Error getting GCP resources for LB with IP = %q: %v", vip, err)
		}
		if err := verifyRewrite(gclb, "/"); err != nil {
			t.Fatal(err)
		}

		deleteOptions := &fuzz.GCLBDeleteOptions{
			SkipDefaultBackend: true,
		}
		if err := e2e.WaitForIngressDeletion(ctx, gclb, s, ing, deleteOptions); err != nil {
			t.Errorf("e2e.WaitForIngressDeletion(..., %q, nil) = %v, want nil", ing.Name, err)
		}
	})
}

func verifyRewrite(gclb *fuzz.GCLB, want string) error {
	for _, um := range gclb.URLMap {
		for _, pm := range um.GA.PathMatchers {
			for _, rule := range pm.PathRules {
				actual := rule.RouteAction.UrlRewrite.PathPrefixRewrite
				if actual != want {
					return fmt.Errorf("PathPrefixRewrite = %s, want %s", actual, want)
				}
			}
		}
	}
}
