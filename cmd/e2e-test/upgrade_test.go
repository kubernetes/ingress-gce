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

	"k8s.io/api/networking/v1beta1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/ingress-gce/cmd/e2e-test/upgrade"
	"k8s.io/ingress-gce/pkg/e2e"
	"k8s.io/ingress-gce/pkg/fuzz"
)

func TestUpgrade(t *testing.T) {
	t.Parallel()

	port80 := intstr.FromInt(80)

	for _, tc := range []struct {
		desc string
		ing  *v1beta1.Ingress
	}{
		{
			desc: "http default backend",
			ing: fuzz.NewIngressBuilder("", "ingress-1", "").
				AddPath("foo.com", "/", "service-1", port80).
				Build(),
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

			tc.ing.Namespace = s.Namespace // namespace depends on sandbox
			crud := e2e.IngressCRUD{C: Framework.Clientset}
			if _, err := crud.Create(tc.ing); err != nil {
				t.Fatalf("error creating Ingress spec: %v", err)
			}
			t.Logf("Ingress created (%s/%s)", s.Namespace, tc.ing.Name)
			s.PutStatus(e2e.Unstable)

			ing := waitForStableIngress(true, tc.ing, s, t)
			t.Logf("GCLB resources created (%s/%s)", s.Namespace, tc.ing.Name)

			whiteboxTest(ing, s, t)

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
					newIng.Namespace = s.Namespace // namespace depends on sandbox
					// TODO: does the path need to be different for each upgrade
					if _, err := crud.Update(newIng); err != nil {
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
			gclb := whiteboxTest(ing, s, t)

			// If the Master has upgraded and the Ingress is stable,
			// we delete the Ingress and exit out of the loop to indicate that
			// the test is done.
			deleteOptions := &fuzz.GCLBDeleteOptions{
				SkipDefaultBackend: true,
			}

			if err := e2e.WaitForIngressDeletion(context.Background(), gclb, s, ing, deleteOptions); err != nil { // Sometimes times out waiting
				t.Errorf("e2e.WaitForIngressDeletion(..., %q, nil) = %v, want nil", ing.Name, err)
			}
		})
	}
}

// TODO: migrate existing upgrade tests to the generic upgrade test.
func TestGenericUpgrade(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		desc string
		test e2e.UpgradeTest
	}{
		{
			desc: "standalone NEG should work for upgrade",
			test: upgrade.NewStandaloneNegUpgradeTest(),
		},
	} {
		tc := tc // Capture tc as we are running this in parallel.
		Framework.RunWithSandbox(tc.desc, t, func(t *testing.T, s *e2e.Sandbox) {
			t.Logf("Running upgrade test %v", tc.desc)
			if err := tc.test.Init(t, s, Framework); err != nil {
				t.Fatalf("For upgrade test %v, step Init failed due to %v", tc.desc, err)
			}

			s.PutStatus(e2e.Unstable)
			func() {
				// always mark the test as stable in order to unblock other upgrade tests.
				defer s.PutStatus(e2e.Stable)
				if err := tc.test.PreUpgrade(); err != nil {
					t.Fatalf("For upgrade test %v, step PreUpgrade failed due to %v", tc.desc, err)
				}
			}()

			for {
				// While k8s master is upgrading, it will return a connection refused
				// error for any k8s resource we try to hit. We loop until the
				// master upgrade has finished.
				if s.MasterUpgrading() {
					if err := tc.test.DuringUpgrade(); err != nil {
						t.Fatalf("For upgrade test %v, step DuringUpgrade failed due to %v", tc.desc, err)
					}
					continue
				}

				if s.MasterUpgraded() {
					t.Logf("Detected master upgrade, continuing upgrade test %v", tc.desc)
					break
				}
			}
			if err := tc.test.PostUpgrade(); err != nil {
				t.Fatalf("For upgrade test %v, step PostUpgrade failed due to %v", tc.desc, err)
			}

		})
	}
}
