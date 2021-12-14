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
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/ingress-gce/pkg/e2e"
	"k8s.io/legacy-cloud-providers/gce"
)

var (
	pscSvc = "psc-svc"
	saName = "sa1"
)

// PSC implements e2e.UpgradeTest interface
type PSC struct {
	t         *testing.T
	s         *e2e.Sandbox
	framework *e2e.Framework
	gceSAURL  string
}

func NewPSCUpgradeTest() e2e.UpgradeTest {
	return &PSC{}
}

// Name implements e2e.UpgradeTest.Init.
func (p *PSC) Name() string {
	return "PSCUpgrade"
}

// Init implements e2e.UpgradeTest.Init.
func (p *PSC) Init(t *testing.T, s *e2e.Sandbox, framework *e2e.Framework) error {
	p.t = t
	p.s = s
	p.framework = framework
	return nil
}

// PreUpgrade implements e2e.UpgradeTest.PreUpgrade.
func (p *PSC) PreUpgrade() error {
	err := e2e.CreateSubnet(p.s, e2e.PSCSubnetName, e2e.PSCSubnetPurpose)
	if err != nil {
		// Subnet Creation could fail because of a leaked subnet from a previous run.
		// Instead of failing the test, attempt to reuse subnet.
		p.t.Logf("Error creating psc subnet %s: %q", e2e.PSCSubnetName, err)
	}
	l4ILBAnnotation := map[string]string{gce.ServiceAnnotationLoadBalancerType: "Internal"}
	if _, err := e2e.EnsureEchoService(p.s, pscSvc, l4ILBAnnotation, v1.ServiceTypeLoadBalancer, 1); err != nil {
		p.t.Fatalf("error ensuring echo service %s/%s: %q", p.s.Namespace, pscSvc, err)
	}
	p.t.Logf("Echo service ensured (%s/%s)", p.s.Namespace, pscSvc)

	if _, err := e2e.EnsureServiceAttachment(p.s, saName, pscSvc, e2e.PSCSubnetName); err != nil {
		p.t.Fatalf("error creating service attachment cr %s/%s: %q", p.s.Namespace, saName, err)
	}

	_, err = e2e.WaitForServiceAttachment(p.s, saName)
	if err != nil {
		p.t.Fatalf("failed waiting for service attachment: %q", err)
	}
	return nil
}

// DuringUpgrade implements e2e.UpgradeTest.DuringUpgrade.
func (p *PSC) DuringUpgrade() error {
	return nil
}

// PostUpgrade implements e2e.UpgradeTest.PostUpgrade
func (p *PSC) PostUpgrade() error {
	// ensure that service attachment is queryable and is configured as expected
	if _, err := e2e.WaitForServiceAttachment(p.s, saName); err != nil {
		p.t.Fatalf("failed waiting for service attachment: %q", err)
	}

	// validate that the CR can be updated with the V1 CRD and that changes
	// are properly propagated to the GCE ServiceAttachment resource using the
	// GA GCE PSC API.
	gceSAURL := pscUpdateAndValidate(p.t, p.s, p.framework)
	if err := e2e.DeleteServiceAttachment(p.s, saName); err != nil {
		p.t.Fatalf("failed to delete service attachment %s/%s: %q", p.s.Namespace, saName, err)
	}

	if err := e2e.WaitForServiceAttachmentDeletion(p.s, saName, gceSAURL); err != nil {
		p.t.Fatalf("errored while waiting for service attachment %s/%s deletion: %q", p.s.Namespace, saName, err)
	}

	if err := e2e.DeleteSubnet(p.s, e2e.PSCSubnetName); err != nil {
		p.t.Fatalf("failed to delete psc subnet %s : %s", e2e.PSCSubnetName, err)
	}
	return nil
}

func pscUpdateAndValidate(t *testing.T, s *e2e.Sandbox, f *e2e.Framework) string {
	saCR, err := f.SAClient.Get(s.Namespace, saName)
	if saCR == nil || err != nil {
		t.Fatalf("failed to get service attachment %s/%s: %v", s.Namespace, saName, err)
	}

	updatedCR := saCR.DeepCopy()
	updatedCR.Spec.ConnectionPreference = "ACCEPT_MANUAL"
	_, err = f.SAClient.Update(updatedCR)
	if err != nil {
		t.Fatalf("failed to update service attachment %s/%s: %s", s.Namespace, saName, err)
	}

	gceSAURL, err := e2e.WaitForServiceAttachment(s, saName)
	if err != nil {
		t.Fatalf("failed waiting for service attachment: %q", err)
	}

	return gceSAURL
}
