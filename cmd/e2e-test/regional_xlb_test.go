/*
Copyright 2023 The Kubernetes Authors.

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
	"k8s.io/ingress-gce/pkg/fuzz/features"
)

// TestRegionalXLB simple test that creates and deletes gce-regional-external
// ingress. Should be run only when ingress-gce has
// --enable-ingress-regional-external flag enabled.
func TestRegionalXLB(t *testing.T) {
	t.Parallel()

	// These names are useful when reading the debug logs
	ingressPrefix := "ing1-"
	serviceName := "svc-1"

	port80 := v1.ServiceBackendPort{Number: 80}

	for _, tc := range []struct {
		desc string
		ing  *v1.Ingress

		numForwardingRules int
		numBackendServices int
	}{
		{
			desc: "http Regional XLB default backend",
			ing: fuzz.NewIngressBuilder("", ingressPrefix+"1", "").
				DefaultBackend(serviceName, port80).
				ConfigureForRegionalXLB().
				Build(),
			numForwardingRules: 1,
			numBackendServices: 1,
		},
		{
			desc: "http Regional XLB one path",
			ing: fuzz.NewIngressBuilder("", ingressPrefix+"2", "").
				AddPath("test.com", "/", serviceName, port80).
				ConfigureForRegionalXLB().
				Build(),
			numForwardingRules: 1,
			numBackendServices: 2,
		},
		{
			desc: "http Regional XLB multiple paths",
			ing: fuzz.NewIngressBuilder("", ingressPrefix+"3", "").
				AddPath("test.com", "/foo", serviceName, port80).
				AddPath("test.com", "/bar", serviceName, port80).
				ConfigureForRegionalXLB().
				Build(),
			numForwardingRules: 1,
			numBackendServices: 2,
		},
	} {
		tc := tc // Capture tc as we are running this in parallel.
		Framework.RunWithSandbox(tc.desc, t, func(t *testing.T, s *e2e.Sandbox) {
			t.Parallel()
			t.Logf("Ingress = %s", tc.ing.String())
			crud := adapter.IngressCRUD{C: Framework.Clientset}

			_, err := e2e.CreateEchoService(s, serviceName, negAnnotation)
			if err != nil {
				t.Fatalf("error creating echo service: %v", err)
			}
			t.Logf("Echo service created (%s/%s)", s.Namespace, serviceName)

			tc.ing.Namespace = s.Namespace
			if _, err := crud.Create(tc.ing); err != nil {
				t.Fatalf("error creating Ingress spec: %v", err)
			}
			t.Logf("Ingress created (%s/%s)", s.Namespace, tc.ing.Name)

			ing, err := e2e.WaitForIngress(s, tc.ing, nil, nil)
			if err != nil {
				t.Fatalf("error waiting for Ingress to stabilize: %v", err)
			}
			t.Logf("GCLB resources created (%s/%s)", s.Namespace, tc.ing.Name)

			// Perform whitebox testing.
			if len(ing.Status.LoadBalancer.Ingress) < 1 {
				t.Fatalf("Ingress does not have an IP: %+v", ing.Status)
			}

			vip := ing.Status.LoadBalancer.Ingress[0].IP
			t.Logf("Ingress %s/%s VIP = %s", s.Namespace, tc.ing.Name, vip)

			params := &fuzz.GCLBForVIPParams{VIP: vip, Validators: fuzz.FeatureValidators(features.All), Region: Framework.Region, Network: Framework.Network}
			gclb, err := fuzz.GCLBForVIP(context.Background(), Framework.Cloud, params)
			if err != nil {
				t.Fatalf("Error getting GCP resources for LB with IP = %q: %v", vip, err)
			}

			if err = e2e.CheckGCLB(gclb, tc.numForwardingRules, tc.numBackendServices); err != nil {
				t.Error(err)
			}

			deleteOptions := &fuzz.GCLBDeleteOptions{
				SkipDefaultBackend: true,
			}
			if err := e2e.WaitForIngressDeletion(context.Background(), gclb, s, ing, deleteOptions); err != nil {
				t.Errorf("e2e.WaitForIngressDeletion(..., %q, nil) = %v, want nil", ing.Name, err)
			}
		})
	}
}
