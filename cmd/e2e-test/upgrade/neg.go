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
)

// StandaloneNeg implements e2e.UpgradeTest interface
type StandaloneNeg struct {
	t         *testing.T
	s         *e2e.Sandbox
	framework *e2e.Framework
}

func NewStandaloneNegUpgradeTest() e2e.UpgradeTest {
	return &StandaloneNeg{}
}

// Name implements e2e.UpgradeTest.Init.
func (sn *StandaloneNeg) Name() string {
	return "StandaloneNegUpgrade"
}

// Init implements e2e.UpgradeTest.Init.
func (sn *StandaloneNeg) Init(t *testing.T, s *e2e.Sandbox, framework *e2e.Framework) error {
	sn.t = t
	sn.s = s
	sn.framework = framework
	return nil
}

// PreUpgrade implements e2e.UpgradeTest.PreUpgrade.
func (sn *StandaloneNeg) PreUpgrade() error {
	svcAnnotations := map[string]string{
		annotations.NEGAnnotationKey: negAnnotation.String(),
	}
	_, err := e2e.EnsureEchoService(sn.s, svcName, svcAnnotations, v1.ServiceTypeClusterIP, 0)

	if err != nil {
		sn.t.Fatalf("error ensuring echo service: %v", err)
	}
	sn.t.Logf("Echo service ensured (%s/%s)", sn.s.Namespace, svcName)

	sn.scaleAndValidate(1)
	sn.scaleAndValidate(3)
	return nil
}

// DuringUpgrade implements e2e.UpgradeTest.DuringUpgrade.
func (sn *StandaloneNeg) DuringUpgrade() error {
	return nil
}

// PostUpgrade implements e2e.UpgradeTest.PostUpgrade
func (sn *StandaloneNeg) PostUpgrade() error {
	sn.validate(3)
	sn.scaleAndValidate(5)
	sn.scaleAndValidate(2)
	return nil
}

// scaleAndValidate scales the deployment and validate if NEGs are reflected.
func (sn *StandaloneNeg) scaleAndValidate(replicas int32) {
	sn.t.Logf("Scaling echo deployment to %v replicas", replicas)
	if err := e2e.EnsureEchoDeployment(sn.s, svcName, replicas, e2e.NoopModify); err != nil {
		sn.t.Fatalf("Error ensuring echo deployment: %v", err)
	}
	if err := e2e.WaitForEchoDeploymentStable(sn.s, svcName); err != nil {
		sn.t.Errorf("Echo deployment failed to become stable: %v", err)
	}
	sn.validate(replicas)
}

// validate check if the NEG status annotation and the corresponding NEGs are correctly configured.
func (sn *StandaloneNeg) validate(replicas int32) {
	// validate neg status
	negStatus, err := e2e.WaitForNegStatus(sn.s, svcName, expectServicePort, false)
	if err != nil {
		sn.t.Fatalf("error waiting for NEG status to update: %v", err)
	}

	// validate neg configurations
	for port, negName := range negStatus.NetworkEndpointGroups {
		ctx := context.Background()
		if err := e2e.WaitForNegs(ctx, sn.framework.Cloud, negName, negStatus.Zones, false, int(replicas)); err != nil {
			sn.t.Errorf("Unexpected port %v and NEG %q in NEG Status %v", port, negName, negStatus)
		}
	}
}
