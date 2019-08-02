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

func TestUpgrade(t *testing.T) {
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
				AddPath("foo.com", "/", "service-1", port80).
				Build(),
			numForwardingRules: 1,
			numBackendServices: 2,
		},
	} {
		tc := tc // Capture tc as we are running this in parallel.
		Framework.RunWithSandbox(tc.desc, t, func(t *testing.T, s *e2e.Sandbox) {
			t.Parallel()

			_, err := e2e.CreateEchoService(s, "service-1", nil)
			if err != nil {
				t.Fatalf("error creating echo service: %v", err)
			}
			t.Logf("Echo service created (%s/%s)", s.Namespace, "service-1")

			if _, err := Framework.Clientset.ExtensionsV1beta1().Ingresses(s.Namespace).Create(tc.ing); err != nil {
				t.Fatalf("error creating Ingress spec: %v", err)
			}
			t.Logf("Ingress created (%s/%s)", s.Namespace, tc.ing.Name)
			s.PutStatus(e2e.Unstable)

			ing := waitForStableIngress(true, tc.ing, s, t)
			t.Logf("GCLB resources created (%s/%s)", s.Namespace, tc.ing.Name)

			whiteboxTest(ing, s, tc.numForwardingRules, tc.numBackendServices, t)

			for {
				// While k8s master is upgrading, it will return a connection refused
				// error for any k8s resource we try to hit. We loop until the
				// master upgrade has finished.
				if s.MasterUpgrading() {
					continue
				}

				if s.MasterUpgraded() {
					t.Logf("Detected master upgrade, adding a path to Ingress to force Ingress update")
					// force ingress update. only add path once
					newIng := fuzz.NewIngressBuilderFromExisting(tc.ing).
						AddPath("bar.com", "/", "service-1", port80).
						Build()

					if _, err := Framework.Clientset.ExtensionsV1beta1().Ingresses(s.Namespace).Update(newIng); err != nil {
						t.Fatalf("error creating Ingress spec: %v", err)
					} else {
						// If Ingress upgrade succeeds, we update the status on this Ingress
						// to Unstable. It is set back to Stable after WaitForIngress below
						// finishes successfully.
						s.PutStatus(e2e.Unstable)
					}

					break
				}
			}

			// Verify the Ingress has stabilized after the master upgrade and we
			// trigger an Ingress update
			ing = waitForStableIngress(true, ing, s, t)
			t.Logf("GCLB is stable (%s/%s)", s.Namespace, tc.ing.Name)
			whiteboxTest(ing, s, tc.numForwardingRules, tc.numBackendServices, t)

			// If the Master has upgraded and the Ingress is stable,
			// we delete the Ingress and exit out of the loop to indicate that
			// the test is done.
			deleteOptions := &fuzz.GCLBDeleteOptions{
				SkipDefaultBackend: true,
			}

			vip := ing.Status.LoadBalancer.Ingress[0].IP
			gclb, err := fuzz.GCLBForVIP(context.Background(), Framework.Cloud, vip, fuzz.FeatureValidators(features.All))
			if err != nil {
				t.Fatalf("Error getting GCP resources for LB with IP = %q: %v", vip, err)
			}

			if err := e2e.WaitForIngressDeletion(context.Background(), gclb, s, ing, deleteOptions); err != nil { // Sometimes times out waiting
				t.Errorf("e2e.WaitForIngressDeletion(..., %q, nil) = %v, want nil", ing.Name, err)
			}
		})
	}
}

func waitForStableIngress(expectUnreachable bool, ing *v1beta1.Ingress, s *e2e.Sandbox, t *testing.T) *v1beta1.Ingress {
	options := &e2e.WaitForIngressOptions{
		ExpectUnreachable: expectUnreachable,
	}

	ing, err := e2e.WaitForIngress(s, ing, options)
	if err != nil {
		t.Fatalf("error waiting for Ingress to stabilize: %v", err)
	}

	s.PutStatus(e2e.Stable)
	return ing
}

func whiteboxTest(ing *v1beta1.Ingress, s *e2e.Sandbox, numForwardingRules, numBackendServices int, t *testing.T) {
	if len(ing.Status.LoadBalancer.Ingress) < 1 {
		t.Fatalf("Ingress does not have an IP: %+v", ing.Status)
	}

	vip := ing.Status.LoadBalancer.Ingress[0].IP
	t.Logf("Ingress %s/%s VIP = %s", s.Namespace, ing.Name, vip)
	gclb, err := fuzz.GCLBForVIP(context.Background(), Framework.Cloud, vip, fuzz.FeatureValidators(features.All))
	if err != nil {
		t.Fatalf("Error getting GCP resources for LB with IP = %q: %v", vip, err)
	}

	// Do some cursory checks on the GCP objects.
	if len(gclb.ForwardingRule) != numForwardingRules {
		t.Errorf("got %d fowarding rules, want %d;\n%s", len(gclb.ForwardingRule), numForwardingRules, pretty.Sprint(gclb.ForwardingRule))
	}
	if len(gclb.BackendService) != numBackendServices {
		t.Errorf("got %d backend services, want %d;\n%s", len(gclb.BackendService), numBackendServices, pretty.Sprint(gclb.BackendService))
	}
}
