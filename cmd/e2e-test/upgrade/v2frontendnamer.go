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

// V2FrontendNamer implements e2e.UpgradeTest interface.
type V2FrontendNamer struct {
	t         *testing.T
	s         *e2e.Sandbox
	framework *e2e.Framework
	crud      adapter.IngressCRUD
	ing       *v1.Ingress
}

// NewV2FrontendNamerTest returns upgrade test for v2 frontend namer.
// This test asserts that v1 finalizer is retained/attached and ingress continues
// to use v1 naming naming scheme when master is upgraded to a gke version that use v1.8.
func NewV2FrontendNamerTest() e2e.UpgradeTest {
	return &V2FrontendNamer{}
}

// Name implements e2e.UpgradeTest.Init.
func (vf *V2FrontendNamer) Name() string {
	return "V2FrontendNamerUpgrade"
}

// Init implements e2e.UpgradeTest.Init.
func (vf *V2FrontendNamer) Init(t *testing.T, s *e2e.Sandbox, framework *e2e.Framework) error {
	vf.t = t
	vf.s = s
	vf.framework = framework
	return nil
}

// PreUpgrade implements e2e.UpgradeTest.PreUpgrade.
func (vf *V2FrontendNamer) PreUpgrade() error {
	if _, err := e2e.CreateEchoService(vf.s, svcName, nil); err != nil {
		vf.t.Fatalf("CreateEchoService(_, %q, nil): %v, want nil", svcName, err)
	}
	vf.t.Logf("Echo service created (%s/%s)", vf.s.Namespace, svcName)

	vf.ing = fuzz.NewIngressBuilder(vf.s.Namespace, ingName, "").
		AddPath("foo.com", "/", svcName, port80).
		SetIngressClass("gce").
		Build()
	ingKey := common.NamespacedName(vf.ing)
	vf.crud = adapter.IngressCRUD{C: vf.framework.Clientset}

	if _, err := vf.crud.Create(vf.ing); err != nil {
		vf.t.Fatalf("Create(%s) = %v, want nil; Ingress: %v", ingKey, err, vf.ing)
	}
	vf.t.Logf("Ingress created (%s)", ingKey)
	ing, err := e2e.UpgradeTestWaitForIngress(vf.s, vf.ing, &e2e.WaitForIngressOptions{ExpectUnreachable: true})
	if err != nil {
		vf.t.Fatalf("error waiting for Ingress %s to stabilize: %v", ingKey, err)
	}

	// Assert that v1 finalizer is added.
	if err := e2e.CheckV1Finalizer(ing); err != nil {
		vf.t.Fatalf("CheckV1Finalizer(%s) = %v, want nil", ingKey, err)
	}

	// Perform whitebox testing.
	if _, err := e2e.WhiteboxTest(ing, nil, vf.framework.Cloud, "", vf.s); err != nil {
		vf.t.Fatalf("e2e.WhiteboxTest(%s, ...) = %v, want nil", ingKey, err)
	}
	return nil
}

// DuringUpgrade implements e2e.UpgradeTest.DuringUpgrade.
func (vf *V2FrontendNamer) DuringUpgrade() error {
	return nil
}

// PostUpgrade implements e2e.UpgradeTest.PostUpgrade
func (vf *V2FrontendNamer) PostUpgrade() error {
	// Force ingress update by adding a new path and mark sandbox unstable.
	newIng := fuzz.NewIngressBuilderFromExisting(vf.ing).
		AddPath("bar.com", "/", svcName, port80).
		Build()
	ingKey := common.NamespacedName(newIng)
	if _, err := vf.crud.Patch(vf.ing, newIng); err != nil {
		vf.t.Fatalf("error patching ingress %s: %v; current ingress: %+v new ingress: %+v", ingKey, err, vf.ing, newIng)
	} else {
		// If Ingress upgrade succeeds, we update the status on this Ingress
		// to Unstable. It is set back to Stable after WaitForFinalizer below
		// finishes successfully.
		vf.s.PutStatus(e2e.Unstable)
	}
	// Wait for ingress to stabilize after the master upgrade.
	if _, err := e2e.WaitForIngress(vf.s, vf.ing, nil, nil); err != nil {
		vf.t.Fatalf("error waiting for Ingress %s to stabilize: %v", ingKey, err)
	}
	// Wait for an ingress finalizer to be added.
	ing, err := e2e.WaitForFinalizer(vf.s, vf.ing)
	if err != nil {
		vf.t.Fatalf("e2e.WaitForFinalizer(_, %q) = _, %v, want nil", ingKey, err)
	}

	// Assert that v1 finalizer is retained.
	if err := e2e.CheckV1Finalizer(ing); err != nil {
		vf.t.Fatalf("CheckV1Finalizer(%s) = %v, want nil", ingKey, err)
	}

	// Perform whitebox testing.
	gclb, err := e2e.WhiteboxTest(ing, nil, vf.framework.Cloud, "", vf.s)
	if err != nil {
		vf.t.Fatalf("e2e.WhiteboxTest(%s, ...) = %v, want nil", ingKey, err)
	}

	// If the Master has upgraded and the Ingress is stable,
	// we delete the Ingress and exit out of the loop to indicate that
	// the test is done.
	deleteOptions := &fuzz.GCLBDeleteOptions{
		SkipDefaultBackend: true,
	}
	if err := e2e.WaitForIngressDeletion(context.Background(), gclb, vf.s, ing, deleteOptions); err != nil {
		vf.t.Errorf("e2e.WaitForIngressDeletion(..., %q, nil) = %v, want nil", ing.Name, err)
	}
	return nil
}
