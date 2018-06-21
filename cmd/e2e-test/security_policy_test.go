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
	"testing"
	"time"

	"github.com/golang/glog"
	computebeta "google.golang.org/api/compute/v0.beta"

	"k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/kubernetes/pkg/cloudprovider/providers/gce/cloud"
	"k8s.io/kubernetes/pkg/cloudprovider/providers/gce/cloud/meta"

	"k8s.io/ingress-gce/pkg/annotations"
	backendconfig "k8s.io/ingress-gce/pkg/apis/backendconfig/v1beta1"
	"k8s.io/ingress-gce/pkg/e2e"
	"k8s.io/ingress-gce/pkg/fuzz"
	"k8s.io/ingress-gce/pkg/fuzz/features"
	"k8s.io/ingress-gce/pkg/utils"
)

const (
	policyUpdateInterval = 15 * time.Second
	policyUpdateTimeout  = 3 * time.Minute
)

func buildPolicyAllowAll(name string) *computebeta.SecurityPolicy {
	return &computebeta.SecurityPolicy{
		Name: name,
	}
}

func buildPolicyDisallowAll(name string) *computebeta.SecurityPolicy {
	return &computebeta.SecurityPolicy{
		Name: name,
		Rules: []*computebeta.SecurityPolicyRule{
			&computebeta.SecurityPolicyRule{
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
	t.Parallel()

	Framework.RunWithSandbox("Security Policy Enable", t, func(t *testing.T, s *e2e.Sandbox) {
		// ------ Step: Preparing test ------

		ctx := context.Background()
		policies := []*computebeta.SecurityPolicy{
			buildPolicyAllowAll("enable-test-allow-all"),
		}
		defer func() {
			if err := cleanupSecurityPolicies(ctx, Framework.Cloud, policies); err != nil {
				t.Errorf("Failed to cleanup policies: %v", err)
			}
		}()
		policies, err := prepareSecurityPolicies(ctx, Framework.Cloud, policies)
		if err != nil {
			t.Fatalf("Failed to prepare policies: %v", err)
		}
		// Re-assign to get the populated self-link.
		testSecurityPolicy := policies[0]

		_, testSvc, testIng, err := prepareK8sResourcesForPolicyTest(ctx, Framework.Cloud, s, testSecurityPolicy.Name)
		if err != nil {
			t.Fatalf("Failed to prepare k8s resources: %v", err)
		}

		// ------ Step: Executing test ------

		testIng, err = e2e.WaitForIngress(s, testIng)
		if err != nil {
			t.Fatalf("Error waiting for Ingress to stabilize: %v", err)
		}
		if len(testIng.Status.LoadBalancer.Ingress) < 1 {
			t.Fatalf("Ingress does not have an IP: %+v", testIng.Status)
		}

		vip := testIng.Status.LoadBalancer.Ingress[0].IP
		gclb, err := fuzz.GCLBForVIP(ctx, Framework.Cloud, vip, fuzz.FeatureValidators([]fuzz.Feature{features.SecurityPolicy}))
		if err != nil {
			t.Fatalf("Error getting GCP resources for LB with IP = %q", vip)
		}

		if err := verifySecurityPolicy(gclb, s.Namespace, testSvc.Name, testSecurityPolicy.SelfLink); err != nil {
			t.Errorf("Failed to verify security policy: %v", err)
		}

		// ------ Step: Cleaning up test ------

		if err := e2e.WaitForIngressDeletion(ctx, Framework.Cloud, gclb, s, testIng, false); err != nil {
			t.Errorf("Failed to wait for ingress deletion: %v", err)
		}
	})
}

func TestSecurityPolicyTransition(t *testing.T) {
	t.Parallel()

	Framework.RunWithSandbox("Security Policy Transition", t, func(t *testing.T, s *e2e.Sandbox) {
		// ------ Step: Preparing test ------

		ctx := context.Background()
		policies := []*computebeta.SecurityPolicy{
			buildPolicyAllowAll("transition-test-allow-all"),
			buildPolicyDisallowAll("transition-test-disallow-all"),
		}
		defer func() {
			if err := cleanupSecurityPolicies(ctx, Framework.Cloud, policies); err != nil {
				t.Errorf("Failed to cleanup policies: %v", err)
			}
		}()
		policies, err := prepareSecurityPolicies(ctx, Framework.Cloud, policies)
		if err != nil {
			t.Fatalf("Failed to prepare policies: %v", err)
		}
		// Re-assign to get the populated self-link.
		testSecurityPolicyAllow, testSecurityPolicyDisallow := policies[0], policies[1]

		testCfg, testSvc, testIng, err := prepareK8sResourcesForPolicyTest(ctx, Framework.Cloud, s, testSecurityPolicyAllow.Name)
		if err != nil {
			t.Fatalf("Failed to prepare k8s resources: %v", err)
		}

		// ------ Step: Executing test ------

		ing, err := e2e.WaitForIngress(s, testIng)
		if err != nil {
			t.Fatalf("Error waiting for Ingress to stabilize: %v", err)
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
			testCfg.Spec.SecurityPolicy.Name = step.securityPolicyToSet
			testCfg, err = Framework.BackendConfigClient.CloudV1beta1().BackendConfigs(s.Namespace).Update(testCfg)
			if err != nil {
				t.Fatalf("Error updating test backend config: %v", err)
			}
			glog.Infof("Backend config %s/%s updated", testCfg.Name, s.Namespace)

			// Wait for security policy to be updated
			if err := wait.Poll(policyUpdateInterval, policyUpdateTimeout, func() (bool, error) {
				gclb, err = fuzz.GCLBForVIP(ctx, Framework.Cloud, vip, fuzz.FeatureValidators([]fuzz.Feature{features.SecurityPolicy}))
				if err != nil {
					t.Fatalf("Error getting GCP resources for LB with IP = %q", vip)
				}

				if err := verifySecurityPolicy(gclb, s.Namespace, testSvc.Name, step.expectedpolicyLink); err != nil {
					glog.Errorf("Failed to verify security policy: %v", err)
					return false, nil
				}
				return true, nil
			}); err != nil {
				t.Errorf("Failed to wait for security policy updated: %v", err)
			}
		}

		// ------ Step: Cleaning up test ------

		if err := e2e.WaitForIngressDeletion(ctx, Framework.Cloud, gclb, s, ing, false); err != nil {
			t.Errorf("Failed to wait for ingress deletion: %v", err)
		}
	})
}

func prepareSecurityPolicies(ctx context.Context, c cloud.Cloud, policies []*computebeta.SecurityPolicy) ([]*computebeta.SecurityPolicy, error) {
	glog.Infof("Creating security policies...")
	createdPolicies := []*computebeta.SecurityPolicy{}
	for _, policy := range policies {
		if err := c.BetaSecurityPolicies().Insert(ctx, meta.GlobalKey(policy.Name), policy); err != nil {
			return nil, fmt.Errorf("error creating security policy %q: %v", policy.Name, err)
		}
		glog.Infof("Security policy %q created", policy.Name)
		policy, err := c.BetaSecurityPolicies().Get(ctx, meta.GlobalKey(policy.Name))
		if err != nil {
			return nil, fmt.Errorf("error getting security policy %q: %v", policy.Name, err)
		}
		createdPolicies = append(createdPolicies, policy)
	}
	return createdPolicies, nil
}

func cleanupSecurityPolicies(ctx context.Context, c cloud.Cloud, policies []*computebeta.SecurityPolicy) error {
	glog.Infof("Deleting security policies...")
	for _, policy := range policies {
		if err := c.BetaSecurityPolicies().Delete(ctx, meta.GlobalKey(policy.Name)); err != nil {
			return fmt.Errorf("failed to delete security policy %q: %v", policy.Name, err)
		}
	}
	return nil
}

func prepareK8sResourcesForPolicyTest(ctx context.Context, c cloud.Cloud, s *e2e.Sandbox, initialPolicyName string) (*backendconfig.BackendConfig, *v1.Service, *v1beta1.Ingress, error) {
	port80 := intstr.FromInt(80)
	testIng := fuzz.NewIngressBuilder("", "ingress-1", "").DefaultBackend("service-1", port80).AddPath("test.com", "/", "service-1", port80).Build()
	testBackendConfig := fuzz.NewBackendConfigBuilder("", "backendconfig-1").SetSecurityPolicy(initialPolicyName).Build()
	testBackendConfigAnnotation := map[string]string{
		annotations.BackendConfigKey: `{"default":"backendconfig-1"}`,
	}

	testBackendConfig, err := Framework.BackendConfigClient.CloudV1beta1().BackendConfigs(s.Namespace).Create(testBackendConfig)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("error creating test backend config: %v", err)
	}
	glog.Infof("Backend config %s/%s created", testBackendConfig.Name, s.Namespace)

	_, testSvc, err := e2e.CreateEchoService(s, "service-1", testBackendConfigAnnotation)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("error creating echo service: %v", err)
	}

	testIng, err = Framework.Clientset.Extensions().Ingresses(s.Namespace).Create(testIng)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("error creating Ingress spec: %v", err)
	}
	return testBackendConfig, testSvc, testIng, nil
}

func verifySecurityPolicy(gclb *fuzz.GCLB, svcNamespace, svcName, policyLink string) error {
	numBsWithPolicy := 0
	for _, bs := range gclb.BackendService {
		// Check on relevant backend services.
		desc := utils.DescriptionFromString(bs.GA.Description)
		if desc.ServiceName != fmt.Sprintf("%s/%s", svcNamespace, svcName) {
			continue
		}

		if bs.Beta == nil {
			return fmt.Errorf("Beta BackendService resource not found: %v", bs)
		}
		if bs.Beta.SecurityPolicy != policyLink {
			return fmt.Errorf("backend service %q has security policy %q, want %q", bs.Beta.Name, bs.Beta.SecurityPolicy, policyLink)
		}
		glog.Infof("Backend service %q has the expected security policy %q attached", bs.Beta.Name, bs.Beta.SecurityPolicy)
		numBsWithPolicy = numBsWithPolicy + 1
	}
	if numBsWithPolicy != 1 {
		return fmt.Errorf("unexpected number of backend service has security policy attached: got %d, want 1", numBsWithPolicy)
	}
	return nil
}
