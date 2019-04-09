/*
Copyright 2018 The Kubernetes Authors.

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

	"k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/ingress-gce/pkg/e2e"
	"k8s.io/ingress-gce/pkg/fuzz"
	"k8s.io/ingress-gce/pkg/fuzz/features"
)

func TestBasic(t *testing.T) {
	t.Parallel()

	port80 := intstr.FromInt(80)

	for _, tc := range []struct {
		desc string
		ing  *v1beta1.Ingress

		numForwardingRules int
		numBackendServices int
	}{
		{
			desc: "http default backend",
			ing: fuzz.NewIngressBuilder("", "ingress-1", "").
				DefaultBackend("service-1", port80).
				Build(),
			numForwardingRules: 1,
			numBackendServices: 1,
		},
		{
			desc: "http one path",
			ing: fuzz.NewIngressBuilder("", "ingress-1", "").
				AddPath("test.com", "/", "service-1", port80).
				Build(),
			numForwardingRules: 1,
			numBackendServices: 2,
		},
		{
			desc: "http multiple paths",
			ing: fuzz.NewIngressBuilder("", "ingress-1", "").
				AddPath("test.com", "/foo", "service-1", port80).
				AddPath("test.com", "/bar", "service-1", port80).
				Build(),
			numForwardingRules: 1,
			numBackendServices: 2,
		},
	} {
		tc := tc // Capture tc as we are running this in parallel.
		Framework.RunWithSandbox(tc.desc, t, func(t *testing.T, s *e2e.Sandbox) {
			t.Parallel()

			ctx := context.Background()

			_, err := e2e.CreateEchoService(s, "service-1", nil)
			if err != nil {
				t.Fatalf("error creating echo service: %v", err)
			}
			t.Logf("Echo service created (%s/%s)", s.Namespace, "service-1")

			if _, err := Framework.Clientset.ExtensionsV1beta1().Ingresses(s.Namespace).Create(tc.ing); err != nil {
				t.Fatalf("error creating Ingress spec: %v", err)
			}
			t.Logf("Ingress created (%s/%s)", s.Namespace, tc.ing.Name)

			ing, err := e2e.WaitForIngress(s, tc.ing, nil)
			if err != nil {
				t.Fatalf("error waiting for Ingress to stabilize: %v", err)
			}
			t.Logf("GCLB resources createdd (%s/%s)", s.Namespace, tc.ing.Name)

			// Perform whitebox testing.
			if len(ing.Status.LoadBalancer.Ingress) < 1 {
				t.Fatalf("Ingress does not have an IP: %+v", ing.Status)
			}

			vip := ing.Status.LoadBalancer.Ingress[0].IP
			t.Logf("Ingress %s/%s VIP = %s", s.Namespace, tc.ing.Name, vip)
			gclb, err := fuzz.GCLBForVIP(context.Background(), Framework.Cloud, vip, fuzz.FeatureValidators(features.All))
			if err != nil {
				t.Fatalf("Error getting GCP resources for LB with IP = %q: %v", vip, err)
			}

			if err = e2e.CheckGCLB(gclb, tc.numForwardingRules, tc.numBackendServices); err != nil {
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
}

// TestBasicStaticIP tests that the static-ip annotation works as expected.
func TestBasicStaticIP(t *testing.T) {
	ctx := context.Background()
	t.Parallel()

	Framework.RunWithSandbox("static-ip", t, func(t *testing.T, s *e2e.Sandbox) {
		_, err := e2e.CreateEchoService(s, "service-1", nil)
		if err != nil {
			t.Fatalf("e2e.CreateEchoService(s, service-1, nil) = _, %v; want _, nil", err)
		}

		addrName := fmt.Sprintf("test-addr-%s", s.Namespace)
		if err := e2e.NewGCPAddress(s, addrName); err != nil {
			t.Fatalf("e2e.NewGCPAddress(..., %s) = %v, want nil", addrName, err)
		}
		defer e2e.DeleteGCPAddress(s, addrName)

		testIng := fuzz.NewIngressBuilder("", "ingress-1", "").
			DefaultBackend("service-1", intstr.FromInt(80)).
			AddStaticIP(addrName).
			Build()
		testIng, err = Framework.Clientset.ExtensionsV1beta1().Ingresses(s.Namespace).Create(testIng)
		if err != nil {
			t.Fatalf("error creating Ingress spec: %v", err)
		}
		t.Logf("Ingress %s/%s created", s.Namespace, testIng.Name)

		testIng, err = e2e.WaitForIngress(s, testIng, nil)
		if err != nil {
			t.Fatalf("e2e.WaitForIngress(s, %q) = _, %v; want _, nil", testIng.Name, err)
		}
		if len(testIng.Status.LoadBalancer.Ingress) < 1 {
			t.Fatalf("Ingress does not have an IP: %+v", testIng.Status)
		}

		vip := testIng.Status.LoadBalancer.Ingress[0].IP
		gclb, err := fuzz.GCLBForVIP(ctx, Framework.Cloud, vip, fuzz.FeatureValidators([]fuzz.Feature{features.SecurityPolicy}))
		if err != nil {
			t.Fatalf("fuzz.GCLBForVIP(..., %q, %q) = _, %v; want _, nil", vip, features.SecurityPolicy, err)
		}

		if err := e2e.WaitForIngressDeletion(ctx, gclb, s, testIng, deleteOptions); err != nil {
			t.Errorf("e2e.WaitForIngressDeletion(..., %q, nil) = %v, want nil", testIng.Name, err)
		}
	})

	// TODO(rramkumar): Add transition
}

// TestEdge exercises some basic edge cases that previously have caused bugs.
func TestEdge(t *testing.T) {
	t.Parallel()

	port80 := intstr.FromInt(80)

	for _, tc := range []struct {
		desc string
		ing  *v1beta1.Ingress

		numForwardingRules int
		numBackendServices int
	}{
		{
			desc: "long ingress name",
			ing: fuzz.NewIngressBuilder("", "long-ingress-name", "").
				DefaultBackend("service-1", port80).
				Build(),
			numForwardingRules: 1,
			numBackendServices: 1,
		},
	} {
		tc := tc // Capture tc as we are running this in parallel.
		Framework.RunWithSandbox(tc.desc, t, func(t *testing.T, s *e2e.Sandbox) {
			t.Parallel()

			ctx := context.Background()

			_, err := e2e.CreateEchoService(s, "service-1", nil)
			if err != nil {
				t.Fatalf("error creating echo service: %v", err)
			}
			t.Logf("Echo service created (%s/%s)", s.Namespace, "service-1")

			if _, err := Framework.Clientset.ExtensionsV1beta1().Ingresses(s.Namespace).Create(tc.ing); err != nil {
				t.Fatalf("error creating Ingress spec: %v", err)
			}
			t.Logf("Ingress created (%s/%s)", s.Namespace, tc.ing.Name)

			ing, err := e2e.WaitForIngress(s, tc.ing, nil)
			if err != nil {
				t.Fatalf("error waiting for Ingress to stabilize: %v", err)
			}
			t.Logf("GCLB resources createdd (%s/%s)", s.Namespace, tc.ing.Name)

			// Perform whitebox testing.
			if len(ing.Status.LoadBalancer.Ingress) < 1 {
				t.Fatalf("Ingress does not have an IP: %+v", ing.Status)
			}

			vip := ing.Status.LoadBalancer.Ingress[0].IP
			t.Logf("Ingress %s/%s VIP = %s", s.Namespace, tc.ing.Name, vip)
			gclb, err := fuzz.GCLBForVIP(context.Background(), Framework.Cloud, vip, fuzz.FeatureValidators(features.All))
			if err != nil {
				t.Fatalf("Error getting GCP resources for LB with IP = %q: %v", vip, err)
			}

			if err = e2e.CheckGCLB(gclb, tc.numForwardingRules, tc.numBackendServices); err != nil {
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
}
