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

package upgrade

import (
	"context"
	"testing"

	v1 "k8s.io/api/networking/v1"
	"k8s.io/ingress-gce/pkg/e2e"
	"k8s.io/ingress-gce/pkg/e2e/adapter"
	"k8s.io/ingress-gce/pkg/fuzz"
	"k8s.io/ingress-gce/pkg/utils/common"
)

// Finalizer implements e2e.UpgradeTest interface.
type Finalizer struct {
	t         *testing.T
	s         *e2e.Sandbox
	framework *e2e.Framework
	crud      adapter.IngressCRUD
	ing       *v1.Ingress
}

// NewFinalizerUpgradeTest returns an upgrade test that asserts that finalizer
// is added to an ingress when upgraded from a version without finalizer to v1.7.0.
// Also, verifies that ingress is deleted with finalizer enabled.
func NewFinalizerUpgradeTest() e2e.UpgradeTest {
	return &Finalizer{}
}

// Name implements e2e.UpgradeTest.Init.
func (fr *Finalizer) Name() string {
	return "FinalizerUpgrade"
}

// Init implements e2e.UpgradeTest.Init.
func (fr *Finalizer) Init(t *testing.T, s *e2e.Sandbox, framework *e2e.Framework) error {
	fr.t = t
	fr.s = s
	fr.framework = framework
	return nil
}

// PreUpgrade implements e2e.UpgradeTest.PreUpgrade.
func (fr *Finalizer) PreUpgrade() error {
	_, err := e2e.CreateEchoService(fr.s, svcName, nil)
	if err != nil {
		fr.t.Fatalf("error creating echo service: %v", err)
	}
	fr.t.Logf("Echo service created (%s/%s)", fr.s.Namespace, svcName)

	ing := fuzz.NewIngressBuilder(fr.s.Namespace, ingName, "").
		AddPath("foo.com", "/", svcName, port80).
		Build()
	ingKey := common.NamespacedName(ing)
	fr.crud = adapter.IngressCRUD{C: fr.framework.Clientset}
	if _, err := fr.crud.Create(ing); err != nil {
		fr.t.Fatalf("error creating Ingress %s: %v", ingKey, err)
	}
	fr.t.Logf("Ingress created (%s)", ingKey)

	if fr.ing, err = e2e.UpgradeTestWaitForIngress(fr.s, ing, &e2e.WaitForIngressOptions{ExpectUnreachable: true}); err != nil {
		fr.t.Fatalf("error waiting for Ingress %s to stabilize: %v", ingKey, err)
	}

	fr.t.Logf("GCLB resources created (%s)", ingKey)

	// Check that finalizer is not added in old version in which finalizer add is not enabled.
	ingFinalizers := fr.ing.GetFinalizers()
	if l := len(ingFinalizers); l != 0 {
		fr.t.Fatalf("len(GetFinalizers()) = %d, want 0", l)
	}

	if _, err := e2e.WhiteboxTest(fr.ing, nil, fr.framework.Cloud, "", fr.s); err != nil {
		fr.t.Fatalf("e2e.WhiteboxTest(%s, ...) = %v, want nil", ingKey, err)
	}
	return nil
}

// DuringUpgrade implements e2e.UpgradeTest.DuringUpgrade.
func (fr *Finalizer) DuringUpgrade() error {
	return nil
}

// PostUpgrade implements e2e.UpgradeTest.PostUpgrade
func (fr *Finalizer) PostUpgrade() error {
	ingKey := common.NamespacedName(fr.ing)
	// A finalizer is expected to be added on the ingress after master upgrade.
	// Ingress status is updated to unstable, which would be set back to stable
	// after WaitForFinalizer below finishes successfully.
	fr.s.PutStatus(e2e.Unstable)
	// Wait for an ingress finalizer to be added on the ingress after the upgrade.
	ing, err := e2e.WaitForFinalizer(fr.s, fr.ing)
	if err != nil {
		fr.t.Fatalf("e2e.WaitForFinalizer(_, %q) = _, %v, want nil", ingKey, err)
	}
	// Assert that v1 finalizer is added.
	if err := e2e.CheckV1Finalizer(ing); err != nil {
		fr.t.Fatalf("CheckV1Finalizer(%s) = %v, want nil", ingKey, err)
	}
	gclb, err := e2e.WhiteboxTest(ing, nil, fr.framework.Cloud, "", fr.s)
	if err != nil {
		fr.t.Fatalf("e2e.WhiteboxTest(%s, ...) = %v, want nil", ingKey, err)
	}

	// If the Master has upgraded and the Ingress is stable,
	// we delete the Ingress and exit out of the loop to indicate that
	// the test is done.
	deleteOptions := &fuzz.GCLBDeleteOptions{
		SkipDefaultBackend: true,
	}

	if err := e2e.WaitForIngressDeletion(context.Background(), gclb, fr.s, fr.ing, deleteOptions); err != nil {
		fr.t.Errorf("e2e.WaitForIngressDeletion(..., %q, nil) = %v, want nil", ingKey, err)
	}
	return nil
}
