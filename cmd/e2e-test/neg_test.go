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
	"testing"

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/e2e"
	"k8s.io/ingress-gce/pkg/fuzz"
	"k8s.io/ingress-gce/pkg/fuzz/features"
)

func TestNEG(t *testing.T) {
	t.Parallel()

	port80 := intstr.FromInt(80)

	ctx := context.Background()

	for _, tc := range []struct {
		desc               string
		annotations        annotations.NegAnnotation
		negExpected        bool
		numForwardingRules int
		numBackendServices int
	}{
		{
			desc:               "Create a basic NEG",
			annotations:        annotations.NegAnnotation{Ingress: true},
			negExpected:        true,
			numForwardingRules: 1,
			numBackendServices: 1,
		},
		{
			desc:               "Annotation with no NEG",
			annotations:        annotations.NegAnnotation{Ingress: false},
			negExpected:        false,
			numForwardingRules: 1,
			numBackendServices: 1,
		},
	} {
		tc := tc // Capture tc as we are running this in parallel.
		Framework.RunWithSandbox(tc.desc, t, func(t *testing.T, s *e2e.Sandbox) {
			_, err := e2e.EnsureEchoService(s, "service-1", map[string]string{
				annotations.NEGAnnotationKey: tc.annotations.String()}, v1.ProtocolTCP, 80, v1.ServiceTypeNodePort, 1)
			if err != nil {
				t.Fatalf("error ensuring echo service: %v", err)
			}
			t.Logf("Echo service ensured (%s/%s)", s.Namespace, "service-1")

			// Create the ingress
			ing := fuzz.NewIngressBuilder("", "ingress-1", "").
				DefaultBackend("service-1", port80).
				Build()
			ing, err = e2e.EnsureIngress(s, ing)
			if err != nil {
				t.Fatalf("error ensuring Ingress spec: %v", err)
			}
			t.Logf("Ingress ensured (%s/%s)", s.Namespace, ing.Name)

			ing, err = e2e.WaitForIngress(s, ing, nil)
			if err != nil {
				t.Fatalf("error waiting for Ingress to stabilize: %v", err)
			}
			t.Logf("GCLB resources created (%s/%s)", s.Namespace, ing.Name)

			// Perform whitebox testing.
			if len(ing.Status.LoadBalancer.Ingress) < 1 {
				t.Fatalf("Ingress does not have an IP: %+v", ing.Status)
			}

			vip := ing.Status.LoadBalancer.Ingress[0].IP
			t.Logf("Ingress %s/%s VIP = %s", s.Namespace, ing.Name, vip)
			gclb, err := fuzz.GCLBForVIP(context.Background(), Framework.Cloud, vip,
				fuzz.FeatureValidators(features.All))
			if err != nil {
				t.Fatalf("Error getting GCP resources for LB with IP = %q: %v", vip, err)
			}

			if err = e2e.CheckGCLB(gclb, tc.numForwardingRules, tc.numBackendServices); err != nil {
				t.Error(err)
			}

			if (len(gclb.NetworkEndpointGroup) > 0) != tc.negExpected {
				t.Errorf("Error: negExpected = %v, %d negs found for gclb %v", tc.negExpected, len(gclb.NetworkEndpointGroup), gclb)
			}

			if err := e2e.WaitForIngressDeletion(ctx, gclb, s, ing, &fuzz.GCLBDeleteOptions{}); err != nil {
				t.Errorf("e2e.WaitForIngressDeletion(..., %q, nil) = %v, want nil", ing.Name, err)
			}
		})
	}
}

func TestNEGTransition(t *testing.T) {
	t.Parallel()

	port80 := intstr.FromInt(80)

	ctx := context.Background()

	Framework.RunWithSandbox("Basic NEG Tests", t, func(t *testing.T, s *e2e.Sandbox) {

		ing := fuzz.NewIngressBuilder("", "ingress-1", "").
			DefaultBackend("service-1", port80).
			Build()

		var previousGCLBState *fuzz.GCLB

		for _, tc := range []struct {
			desc        string
			annotations annotations.NegAnnotation
			// negGC is true if a NEG should be garbage collected after applying the annotations
			negGC              bool
			numForwardingRules int
			numBackendServices int
		}{
			{
				desc:               "Using ingress only",
				annotations:        annotations.NegAnnotation{Ingress: true},
				negGC:              false,
				numForwardingRules: 1,
				numBackendServices: 1,
			},
			{
				desc:               "Disable NEG for ingress",
				annotations:        annotations.NegAnnotation{Ingress: false},
				negGC:              true,
				numForwardingRules: 1,
				numBackendServices: 1,
			},
			{
				desc:               "Re-enable NEG for ingress",
				annotations:        annotations.NegAnnotation{Ingress: true},
				negGC:              false,
				numForwardingRules: 1,
				numBackendServices: 1,
			},
			{
				desc:               "No annotations",
				annotations:        annotations.NegAnnotation{},
				negGC:              true,
				numForwardingRules: 1,
				numBackendServices: 1,
			},
		} {
			// First create the echo service, we will be adapting it throughout the basic tests
			_, err := e2e.EnsureEchoService(s, "service-1", map[string]string{
				annotations.NEGAnnotationKey: tc.annotations.String()}, v1.ProtocolTCP, 80, v1.ServiceTypeNodePort, 1)
			if err != nil {
				t.Fatalf("error ensuring echo service: %v", err)
			}
			t.Logf("Echo service ensured (%s/%s)", s.Namespace, "service-1")

			// Create the ingress
			ing, err = e2e.EnsureIngress(s, ing)
			if err != nil {
				t.Fatalf("error ensuring Ingress spec: %v", err)
			}
			t.Logf("Ingress ensured (%s/%s)", s.Namespace, ing.Name)

			ing, err = e2e.WaitForIngress(s, ing, nil)
			if err != nil {
				t.Fatalf("error waiting for Ingress to stabilize: %v", err)
			}
			t.Logf("GCLB resources created (%s/%s)", s.Namespace, ing.Name)

			// Perform whitebox testing.
			if len(ing.Status.LoadBalancer.Ingress) < 1 {
				t.Fatalf("Ingress does not have an IP: %+v", ing.Status)
			}

			vip := ing.Status.LoadBalancer.Ingress[0].IP
			t.Logf("Ingress %s/%s VIP = %s", s.Namespace, ing.Name, vip)
			gclb, err := fuzz.GCLBForVIP(context.Background(), Framework.Cloud, vip,
				fuzz.FeatureValidators(features.All))
			if err != nil {
				t.Fatalf("Error getting GCP resources for LB with IP = %q: %v", vip, err)
			}

			if err = e2e.CheckGCLB(gclb, tc.numForwardingRules, tc.numBackendServices); err != nil {
				t.Error(err)
			}

			if tc.negGC {
				if len(gclb.NetworkEndpointGroup) != 0 {
					t.Errorf("NegGC = true, expected 0 negs for gclb %v, got %d", gclb, len(gclb.NetworkEndpointGroup))
				}
				if err = e2e.WaitForNEGDeletion(ctx, s.ValidatorEnv.Cloud(), previousGCLBState, nil); err != nil {
					t.Errorf("Error waiting for NEGDeletion: %v", err)
				}
			} else {
				if len(gclb.NetworkEndpointGroup) < 1 {
					t.Errorf("Error, no NEGS associated with gclb %v, expected at least one", gclb)
				}
			}
			previousGCLBState = gclb
		}

		if ing != nil && previousGCLBState != nil {
			if err := e2e.WaitForIngressDeletion(ctx, previousGCLBState, s, ing, &fuzz.GCLBDeleteOptions{}); err != nil {
				t.Errorf("e2e.WaitForIngressDeletion(..., %q, nil) = %v, want nil", ing.Name, err)
			}
		}
	})
}
