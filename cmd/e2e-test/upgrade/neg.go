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

	v1 "k8s.io/api/core/v1"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/e2e"
)

var (
	svcName       = "svc1"
	negAnnotation = annotations.NegAnnotation{
		Ingress: false,
		ExposedPorts: map[int32]annotations.NegAttributes{
			int32(443): {},
			int32(80):  {},
		},
	}
	expectServicePort = []string{"80", "443"}
	expectedNegAttrs  = map[string]string{"80": "", "443": ""}
)

// StandaloneNEGWithSvcNEG implements e2e.UpgradeTest interface
type StandaloneNEGWithSvcNEG struct {
	t         *testing.T
	s         *e2e.Sandbox
	framework *e2e.Framework
}

func NewStandaloneNEGWithSvcNEGUpgradeTest() e2e.UpgradeTest {
	return &StandaloneNEGWithSvcNEG{}
}

// Name implements e2e.UpgradeTest.Init.
func (n *StandaloneNEGWithSvcNEG) Name() string {
	return "StandaloneNEGWithSvcNEGUpgrade"
}

// Init implements e2e.UpgradeTest.Init.
func (n *StandaloneNEGWithSvcNEG) Init(t *testing.T, s *e2e.Sandbox, framework *e2e.Framework) error {
	n.t = t
	n.s = s
	n.framework = framework
	return nil
}

// PreUpgrade implements e2e.UpgradeTest.PreUpgrade.
func (n *StandaloneNEGWithSvcNEG) PreUpgrade() error {
	svcAnnotations := map[string]string{
		annotations.NEGAnnotationKey: negAnnotation.String(),
	}
	_, err := e2e.EnsureEchoService(n.s, svcName, svcAnnotations, v1.ServiceTypeClusterIP, 0)

	if err != nil {
		n.t.Fatalf("error ensuring echo service: %v", err)
	}
	n.t.Logf("Echo service ensured with neg annotation (%s/%s)", n.s.Namespace, svcName)

	negScaleAndValidate(n.t, n.s, n.framework, 1)
	return nil
}

// DuringUpgrade implements e2e.UpgradeTest.DuringUpgrade.
func (n *StandaloneNEGWithSvcNEG) DuringUpgrade() error {
	return nil
}

// PostUpgrade implements e2e.UpgradeTest.PostUpgrade
func (n *StandaloneNEGWithSvcNEG) PostUpgrade() error {
	negValidate(n.t, n.s, n.framework, 1)

	negStatus, err := e2e.WaitForNegCRs(n.s, svcName, expectedNegAttrs)
	if err != nil {
		n.t.Fatalf("error waiting for Neg CRs")
	}
	negScaleAndValidate(n.t, n.s, n.framework, 3)

	_, err = e2e.EnsureEchoService(n.s, svcName, map[string]string{}, v1.ServiceTypeClusterIP, 0)
	if err != nil {
		n.t.Fatalf("error ensuring echo service: %v", err)
	}

	n.t.Logf("Echo service ensured with no annotation (%s/%s)", n.s.Namespace, svcName)

	// Test that garbage collection works properly after upgrade
	for _, port := range expectServicePort {
		if err = e2e.WaitForStandaloneNegDeletion(context.Background(), n.s.ValidatorEnv.Cloud(), n.s, port, negStatus); err != nil {
			n.t.Errorf("error waiting for NEGDeletion: %v", err)
		}
	}

	return nil
}

// negScaleAndValidate scales the deployment and validate if NEGs are reflected.
func negScaleAndValidate(t *testing.T, s *e2e.Sandbox, framework *e2e.Framework, replicas int32) {
	t.Logf("Scaling echo deployment to %v replicas", replicas)
	if err := e2e.EnsureEchoDeployment(s, svcName, replicas, e2e.NoopModify); err != nil {
		t.Fatalf("Error ensuring echo deployment: %v", err)
	}
	if err := e2e.WaitForEchoDeploymentStable(s, svcName); err != nil {
		t.Errorf("Echo deployment failed to become stable: %v", err)
	}
	negValidate(t, s, framework, replicas)
}

// negValidate check if the NEG status annotation and the corresponding NEGs are correctly configured.
func negValidate(t *testing.T, s *e2e.Sandbox, framework *e2e.Framework, replicas int32) {
	// validate neg status
	negStatus, err := e2e.WaitForNegCRs(s, svcName, expectedNegAttrs)
	if err != nil {
		t.Fatalf("error waiting for NEG CRs to update: %v", err)
	}

	// validate neg configurations
	for port, negName := range negStatus.NetworkEndpointGroups {
		ctx := context.Background()
		if err := e2e.WaitForNegs(ctx, framework.Cloud, negName, negStatus.Zones, false, int(replicas)); err != nil {
			t.Errorf("Unexpected port %v and NEG %q in NEG Status %v", port, negName, negStatus)
		}
	}
}
