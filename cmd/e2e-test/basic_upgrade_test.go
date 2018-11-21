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

func TestBasicUpgrade(t *testing.T) {
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
	} {
		tc := tc // Capture tc as we are running this in parallel.
		Framework.RunWithSandbox(tc.desc, t, func(t *testing.T, s *e2e.Sandbox) {
			t.Parallel()

			_, _, err := e2e.CreateEchoService(s, "service-1", nil)
			if err != nil {
				t.Fatalf("error creating echo service: %v", err)
			}
			t.Logf("Echo service created (%s/%s)", s.Namespace, "service-1")

			if _, err := Framework.Clientset.Extensions().Ingresses(s.Namespace).Create(tc.ing); err != nil {
				t.Fatalf("error creating Ingress spec: %v", err)
			}
			t.Logf("Ingress created (%s/%s)", s.Namespace, tc.ing.Name)
			s.PutStatus(e2e.Unstable)

			runs := 0
			// needUpdate indicates that the Ingress sync has NOT yet been triggered
			needUpdate := true
			// Framework.shutdown() kills this loop.
			for {
				if s.MasterUpgraded() && needUpdate {
					// force ingress update. only add path once
					newIng := fuzz.NewIngressBuilderFromExisting(tc.ing).
						AddPath("test.com", "/", "service-1", intstr.FromInt(80)).
						Build()

					if _, err := Framework.Clientset.Extensions().Ingresses(s.Namespace).Create(newIng); err != nil {
						t.Fatalf("error creating Ingress spec: %v", err)
					} else {
						s.PutStatus(e2e.Unstable)
						needUpdate = false
					}
				}

				options := &e2e.WaitForIngressOptions{
					ExpectUnreachable: runs == 0,
				}
				ing, err := e2e.WaitForIngress(s, tc.ing, options)
				if err != nil {
					t.Fatalf("error waiting for Ingress to stabilize: %v", err)
				}

				if runs == 0 {
					t.Logf("GCLB resources created (%s/%s)", s.Namespace, tc.ing.Name)
					s.PutStatus(e2e.Stable)
				} else {
					t.Logf("GCLB is stable (%s/%s)", s.Namespace, tc.ing.Name)
				}

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

				// Do some cursory checks on the GCP objects.
				if len(gclb.ForwardingRule) != tc.numForwardingRules {
					t.Errorf("got %d fowarding rules, want %d;\n%s", len(gclb.ForwardingRule), tc.numForwardingRules, pretty.Sprint(gclb.ForwardingRule))
				}
				if len(gclb.BackendService) != tc.numBackendServices {
					t.Errorf("got %d backend services, want %d;\n%s", len(gclb.BackendService), tc.numBackendServices, pretty.Sprint(gclb.BackendService))
				}

				runs++
			}
		})
	}
}
