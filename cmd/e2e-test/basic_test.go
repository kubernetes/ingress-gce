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

	"github.com/kr/pretty"
	"k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/ingress-gce/pkg/e2e"
	"k8s.io/ingress-gce/pkg/fuzz"
	"k8s.io/ingress-gce/pkg/fuzz/features"
)

func TestBasic(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		desc string
		ing  *v1beta1.Ingress

		numForwardingRules int
		numBackendServices int
	}{
		{
			desc:               "http default backend",
			ing:                fuzz.NewIngressBuilder("", "ingress-1", "").DefaultBackend("service-1", intstr.FromInt(80)).I,
			numForwardingRules: 1,
			numBackendServices: 1,
		},
		{
			desc:               "http one path",
			ing:                fuzz.NewIngressBuilder("", "ingress-1", "").AddPath("test.com", "/", "service-1", intstr.FromInt(80)).I,
			numForwardingRules: 1,
			numBackendServices: 2,
		},
		{
			desc:               "http multiple paths",
			ing:                fuzz.NewIngressBuilder("", "ingress-1", "").AddPath("test.com", "/foo", "service-1", intstr.FromInt(80)).AddPath("test.com", "/bar", "service-1", intstr.FromInt(80)).I,
			numForwardingRules: 1,
			numBackendServices: 2,
		},
	} {
		tc := tc // Capture tc as we are running this in parallel.
		Framework.RunWithSandbox(tc.desc, t, func(t *testing.T, s *e2e.Sandbox) {
			t.Parallel()

			_, _, err := e2e.CreateEchoService(s, "service-1")
			if err != nil {
				t.Fatalf("error creating echo service: %v", err)
			}

			if _, err := Framework.Clientset.Extensions().Ingresses(s.Namespace).Create(tc.ing); err != nil {
				t.Fatalf("error creating Ingress spec: %v", err)
			}

			ing, err := e2e.WaitForIngress(s, tc.ing)
			if err != nil {
				t.Fatalf("error waiting for Ingress to stabilize: %v", err)
			}

			// Perform whitebox testing.
			if len(ing.Status.LoadBalancer.Ingress) < 1 {
				t.Fatalf("Ingress does not have an IP: %+v", ing.Status)
			}

			vip := ing.Status.LoadBalancer.Ingress[0].IP
			gclb, err := fuzz.GCLBForVIP(context.Background(), Framework.Cloud, vip, fuzz.FeatureValidators(features.All))
			if err != nil {
				t.Fatalf("Error getting GCP resources for LB with IP = %q", vip)
			}
			// Do some cursory checks on the GCP objects.
			if len(gclb.ForwardingRule) != tc.numForwardingRules {
				t.Errorf("got %d fowarding rules, want %d;\n%s", len(gclb.ForwardingRule), tc.numForwardingRules, pretty.Sprint(gclb.ForwardingRule))
			}
			if len(gclb.BackendService) != tc.numBackendServices {
				t.Errorf("got %d backend services, want %d;\n%s", len(gclb.BackendService), tc.numBackendServices, pretty.Sprint(gclb.BackendService))
			}
		})
	}
}
