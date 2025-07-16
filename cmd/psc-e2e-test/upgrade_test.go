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
	"testing"

	upgrade "k8s.io/ingress-gce/cmd/psc-e2e-test/upgrade"
	"k8s.io/ingress-gce/pkg/e2e"
)

func TestGenericUpgrade(t *testing.T) {
	t.Parallel()
	// Only run the PSC-related upgrade test.
	runUpgradeTest(t, upgrade.NewPSCUpgradeTest())
}

func runUpgradeTest(t *testing.T, test e2e.UpgradeTest) {
	desc := test.Name()
	Framework.RunWithSandbox(desc, t, func(t *testing.T, s *e2e.Sandbox) {
		t.Parallel()

		t.Logf("Running upgrade test %v", desc)
		if err := test.Init(t, s, Framework); err != nil {
			t.Fatalf("For upgrade test %v, step Init failed due to %v", desc, err)
		}

		s.PutStatus(e2e.Unstable)
		func() {
			// always mark the test as stable in order to unblock other upgrade tests.
			defer s.PutStatus(e2e.Stable)
			if err := test.PreUpgrade(); err != nil {
				t.Fatalf("For upgrade test %v, step PreUpgrade failed due to %v", desc, err)
			}
		}()

		for {
			// While k8s master is upgrading, it will return a connection refused
			// error for any k8s resource we try to hit. We loop until the
			// master upgrade has finished.
			if s.MasterUpgrading() {
				if err := test.DuringUpgrade(); err != nil {
					t.Fatalf("For upgrade test %v, step DuringUpgrade failed due to %v", desc, err)
				}
				continue
			}

			if s.MasterUpgraded() {
				t.Logf("Detected master upgrade, continuing upgrade test %v", desc)
				break
			}
		}
		if err := test.PostUpgrade(); err != nil {
			t.Fatalf("For upgrade test %v, step PostUpgrade failed due to %v", desc, err)
		}
	})
}