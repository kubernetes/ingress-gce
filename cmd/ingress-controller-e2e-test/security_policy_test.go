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
	"fmt"
	"strings"
	"testing"
	"time"

	computebeta "google.golang.org/api/compute/v0.beta"
	v1 "k8s.io/api/networking/v1"
	"k8s.io/ingress-gce/pkg/e2e/adapter"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"k8s.io/apimachinery/pkg/util/wait"

	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/e2e"
	"k8s.io/ingress-gce/pkg/fuzz"
	"k8s.io/ingress-gce/pkg/fuzz/features"
	"k8s.io/ingress-gce/pkg/utils"
)

const (
	policyUpdateInterval = 15 * time.Second
	policyUpdateTimeout  = 10 * time.Minute
)

var deleteOptions = &fuzz.GCLBDeleteOptions{
	SkipDefaultBackend: true,
}

func buildPolicyAllowAll(name string) *computebeta.SecurityPolicy {
	return &computebeta.SecurityPolicy{
		Name: name,
	}
}

func buildPolicyDisallowAll(name string) *computebeta.SecurityPolicy {
	return &computebeta.SecurityPolicy{
		Name: name,
		Rules: []*computebeta.SecurityPolicyRule{
			{
				Action: "deny(403)",
				Match: &computebeta.SecurityPolicyRuleMatcher{
					Config: &computebeta.SecurityPolicyRuleMatcherConfig{
						SrcIpRanges: []string{"*"},
					},
					VersionedExpr: "SRC_IPS_V1",
				},
				Priority: 2147483647,
			},
		},
	}
}

func TestSecurityPolicyEnable(t *testing.T) {
	ctx := context.Background()
	t.Parallel()

	Framework.RunWithSandbox("Security Policy Enable", t, func(t *testing.T, s *e2e.Sandbox) {
		policies := []*computebeta.SecurityPolicy{
			buildPolicyAllowAll(fmt.Sprintf("enable-test-allow-all-%s", s.Namespace)),
		}
		defer func() {
			if err := cleanupSecurityPolicies(ctx, t, Framework.Cloud, policies); err != nil {
				t.Errorf("cleanupSecurityPolicies(...) =  %v, want nil", err)
			}
		}()
		policies, err := createSecurityPolicies(ctx, t, Framework.Cloud, policies)
		if err != nil {
			t.Fatalf("createSecurityPolicies(...) = _, %v, want _, nil", err)
		}
		// Re-assign to get the populated self-link.
		testSecurityPolicy := policies[0]

		testBackendConfigAnnotation := map[string]string{
			annotations.BetaBackendConfigKey: `{"default":"backendconfig-1"}`,
		}
		testSvc, err := e2e.CreateEchoService(s, "service-1", testBackendConfigAnnotation)
		if err != nil {
			t.Fatalf("e2e.CreateEchoService(s, service-1, %q) = _, _, %v, want _, _, nil", testBackendConfigAnnotation, err)
		}

		testBackendConfig := fuzz.NewBackendConfigBuilder(s.Namespace, "backendconfig-1").SetSecurityPolicy(testSecurityPolicy.Name).Build()
		bcCRUD := adapter.BackendConfigCRUD{C: Framework.BackendConfigClient}
		testBackendConfig, err = bcCRUD.Create(testBackendConfig)
		if err != nil {
			t.Fatalf("Error creating test backend config: %v", err)
		}
		t.Logf("Backend config %s/%s created", s.Namespace, testBackendConfig.Name)

		port80 := v1.ServiceBackendPort{Number: 80}
		testIng := fuzz.NewIngressBuilder(s.Namespace, "ingress-1", "").
			DefaultBackend("service-1", port80).
			AddPath("test.com", "/", "service-1", port80).
			Build()
		crud := adapter.IngressCRUD{C: Framework.Clientset}
		testIng, err = crud.Create(testIng)
		if err != nil {
			t.Fatalf("error creating Ingress spec: %v", err)
		}
		t.Logf("Ingress %s/%s created", s.Namespace, testIng.Name)

		t.Logf("Checking on relevant backend service whether security policy is properly attached")

		testIng, err = e2e.WaitForIngress(s, testIng, nil, nil)
		if err != nil {
			t.Fatalf("e2e.WaitForIngress(s, %q) = _, %v; want _, nil", testIng.Name, err)
		}
		if len(testIng.Status.LoadBalancer.Ingress) < 1 {
			t.Fatalf("Ingress does not have an IP: %+v", testIng.Status)
		}

		vip := testIng.Status.LoadBalancer.Ingress[0].IP
		params := &fuzz.GCLBForVIPParams{VIP: vip, Validators: fuzz.FeatureValidators([]fuzz.Feature{features.SecurityPolicy})}
		gclb, err := fuzz.GCLBForVIP(ctx, Framework.Cloud, params)
		if err != nil {
			t.Fatalf("fuzz.GCLBForVIP(..., %q, %q) = _, %v; want _, nil", vip, features.SecurityPolicy, err)
		}

		if err := verifySecurityPolicy(t, gclb, s.Namespace, testSvc.Name, testSecurityPolicy.SelfLink); err != nil {
			t.Errorf("verifySecurityPolicy(..., %q, %q, %q) = %v, want nil", s.Namespace, testSvc.Name, testSecurityPolicy.SelfLink, err)
		}

		t.Logf("Cleaning up test")

		if err := e2e.WaitForIngressDeletion(ctx, gclb, s, testIng, deleteOptions); err != nil {
			t.Errorf("e2e.WaitForIngressDeletion(..., %q, nil) = %v, want nil", testIng.Name, err)
		}
	})
}

func TestSecurityPolicyTransition(t *testing.T) {
	ctx := context.Background()
	t.Parallel()

	Framework.RunWithSandbox("Security Policy Transition", t, func(t *testing.T, s *e2e.Sandbox) {
		policies := []*computebeta.SecurityPolicy{
			buildPolicyAllowAll(fmt.Sprintf("transition-test-allow-all-%s", s.Namespace)),
			buildPolicyDisallowAll(fmt.Sprintf("transition-test-disallow-all-%s", s.Namespace)),
		}
		defer func() {
			if err := cleanupSecurityPolicies(ctx, t, Framework.Cloud, policies); err != nil {
				t.Errorf("cleanupSecurityPolicies(...) = %v, want nil", err)
			}
		}()
		policies, err := createSecurityPolicies(ctx, t, Framework.Cloud, policies)
		if err != nil {
			t.Fatalf("createSecurityPolicies(...) = _, %v, want _, nil", err)
		}
		// Re-assign to get the populated self-link.
		testSecurityPolicyAllow, testSecurityPolicyDisallow := policies[0], policies[1]

		testBackendConfigAnnotation := map[string]string{
			annotations.BetaBackendConfigKey: `{"default":"backendconfig-1"}`,
		}
		testSvc, err := e2e.CreateEchoService(s, "service-1", testBackendConfigAnnotation)
		if err != nil {
			t.Fatalf("e2e.CreateEchoService(s, service-1, %q) = _, _, %v, want _, _, nil", testBackendConfigAnnotation, err)
		}

		testBackendConfig := fuzz.NewBackendConfigBuilder(s.Namespace, "backendconfig-1").SetSecurityPolicy(testSecurityPolicyAllow.Name).Build()
		bcCRUD := adapter.BackendConfigCRUD{C: Framework.BackendConfigClient}
		testBackendConfig, err = bcCRUD.Create(testBackendConfig)
		if err != nil {
			t.Fatalf("Error creating test backend config: %v", err)
		}
		t.Logf("Backend config %s/%s created", s.Namespace, testBackendConfig.Name)

		port80 := v1.ServiceBackendPort{Number: 80}
		testIng := fuzz.NewIngressBuilder(s.Namespace, "ingress-1", "").
			DefaultBackend("service-1", port80).
			AddPath("test.com", "/", "service-1", port80).
			Build()
		crud := adapter.IngressCRUD{C: Framework.Clientset}
		testIng, err = crud.Create(testIng)
		if err != nil {
			t.Fatalf("error creating Ingress spec: %v", err)
		}
		t.Logf("Ingress %s/%s created", s.Namespace, testIng.Name)

		ing, err := e2e.WaitForIngress(s, testIng, nil, nil)
		if err != nil {
			t.Fatalf("e2e.WaitForIngress(s, %q) = _, %v; want _, nil", testIng.Name, err)
		}
		if len(ing.Status.LoadBalancer.Ingress) < 1 {
			t.Fatalf("Ingress does not have an IP: %+v", ing.Status)
		}

		vip := ing.Status.LoadBalancer.Ingress[0].IP
		var gclb *fuzz.GCLB

		steps := []struct {
			desc                string
			securityPolicyToSet string
			expectedpolicyLink  string
		}{
			{
				desc:                "update to use policy that disallows all",
				securityPolicyToSet: testSecurityPolicyDisallow.Name,
				expectedpolicyLink:  testSecurityPolicyDisallow.SelfLink,
			},
			{
				desc:                "detach policy",
				securityPolicyToSet: "",
				expectedpolicyLink:  "",
			},
		}

		for _, step := range steps {
			t.Run(step.desc, func(t *testing.T) {
				testBackendConfig.Spec.SecurityPolicy.Name = step.securityPolicyToSet
				bcCRUD := adapter.BackendConfigCRUD{C: Framework.BackendConfigClient}
				testBackendConfig, err = bcCRUD.Update(testBackendConfig)
				if err != nil {
					t.Fatalf("Error updating test backend config: %v", err)
				}
				t.Logf("Backend config %s/%s updated", testBackendConfig.Name, s.Namespace)

				t.Logf("Waiting %v for security policy to be updated on relevant backend service", policyUpdateTimeout)
				if err := wait.Poll(policyUpdateInterval, policyUpdateTimeout, func() (bool, error) {
					params := &fuzz.GCLBForVIPParams{VIP: vip, Validators: fuzz.FeatureValidators(features.All)}
					gclb, err = fuzz.GCLBForVIP(ctx, Framework.Cloud, params)
					if err != nil {
						t.Fatalf("fuzz.GCLBForVIP(..., %q, %q) = _, %v; want _, nil", vip, features.SecurityPolicy, err)
					}

					if err := verifySecurityPolicy(t, gclb, s.Namespace, testSvc.Name, step.expectedpolicyLink); err != nil {
						t.Logf("verifySecurityPolicy(..., %q, %q, %q) = %v, want nil", s.Namespace, testSvc.Name, step.expectedpolicyLink, err)
						return false, nil
					}
					return true, nil
				}); err != nil {
					t.Errorf("Failed to wait for security policy updated: %v", err)
				}
			})
		}

		t.Logf("Cleaning up test")

		if err := e2e.WaitForIngressDeletion(ctx, gclb, s, ing, deleteOptions); err != nil {
			t.Errorf("e2e.WaitForIngressDeletion(..., %q, nil) = %v, want nil", ing.Name, err)
		}
	})
}

func createSecurityPolicies(ctx context.Context, t *testing.T, c cloud.Cloud, policies []*computebeta.SecurityPolicy) ([]*computebeta.SecurityPolicy, error) {
	t.Logf("Creating security policies...")
	createdPolicies := []*computebeta.SecurityPolicy{}
	for _, policy := range policies {
		if err := c.BetaSecurityPolicies().Insert(ctx, meta.GlobalKey(policy.Name), policy); err != nil {
			return nil, fmt.Errorf("error creating security policy %q: %v", policy.Name, err)
		}
		t.Logf("Security policy %q created", policy.Name)
		policy, err := c.BetaSecurityPolicies().Get(ctx, meta.GlobalKey(policy.Name))
		if err != nil {
			return nil, fmt.Errorf("error getting security policy %q: %v", policy.Name, err)
		}
		createdPolicies = append(createdPolicies, policy)
	}
	return createdPolicies, nil
}

func cleanupSecurityPolicies(ctx context.Context, t *testing.T, c cloud.Cloud, policies []*computebeta.SecurityPolicy) error {
	t.Logf("Deleting security policies...")
	var errs []string
	for _, policy := range policies {
		if err := c.BetaSecurityPolicies().Delete(ctx, meta.GlobalKey(policy.Name)); err != nil {
			errs = append(errs, err.Error())
		}
		t.Logf("Security policy %q deleted", policy.Name)
	}
	if len(errs) != 0 {
		return fmt.Errorf("failed to delete security policies: %s", strings.Join(errs, "\n"))
	}
	return nil
}

func verifySecurityPolicy(t *testing.T, gclb *fuzz.GCLB, svcNamespace, svcName, policyLink string) error {
	numBsWithPolicy := 0
	for _, bs := range gclb.BackendService {
		// Check on relevant backend services.
		desc := utils.DescriptionFromString(bs.GA.Description)
		if desc.ServiceName != fmt.Sprintf("%s/%s", svcNamespace, svcName) {
			continue
		}

		if bs.Beta == nil {
			return fmt.Errorf("beta BackendService resource not found: %v", bs)
		}
		if bs.Beta.SecurityPolicy != policyLink {
			return fmt.Errorf("backend service %q has security policy %q, want %q", bs.Beta.Name, bs.Beta.SecurityPolicy, policyLink)
		}
		t.Logf("Backend service %q has the expected security policy %q attached", bs.Beta.Name, bs.Beta.SecurityPolicy)
		numBsWithPolicy = numBsWithPolicy + 1
	}
	if numBsWithPolicy != 1 {
		return fmt.Errorf("unexpected number of backend service has security policy attached: got %d, want 1", numBsWithPolicy)
	}
	return nil
}
