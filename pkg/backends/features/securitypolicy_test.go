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

package features

import (
	"context"
	"fmt"
	"sync"
	"testing"

	computebeta "google.golang.org/api/compute/v0.beta"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"k8s.io/kubernetes/pkg/cloudprovider/providers/gce"

	backendconfigv1beta1 "k8s.io/ingress-gce/pkg/apis/backendconfig/v1beta1"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/utils"
)

func TestEnsureSecurityPolicy(t *testing.T) {
	mockSecurityPolcies := make(map[string]*computebeta.SecurityPolicyReference)
	mockSecurityPolicyLock := sync.Mutex{}
	setSecurityPolicyHook := func(_ context.Context, key *meta.Key, ref *computebeta.SecurityPolicyReference, _ *cloud.MockBetaBackendServices) error {
		mockSecurityPolicyLock.Lock()
		mockSecurityPolcies[key.Name] = ref
		mockSecurityPolicyLock.Unlock()
		return nil
	}

	testCases := []struct {
		desc                  string
		currentBackendService *composite.BackendService
		desiredConfig         *backendconfigv1beta1.BackendConfig
		expectSetCall         bool
	}{
		{
			desc:                  "attach-policy",
			currentBackendService: &composite.BackendService{},
			desiredConfig: &backendconfigv1beta1.BackendConfig{
				Spec: backendconfigv1beta1.BackendConfigSpec{
					SecurityPolicy: &backendconfigv1beta1.SecurityPolicyConfig{
						Name: "policy-1",
					},
				},
			},
			expectSetCall: true,
		},
		{
			desc: "update-policy",
			currentBackendService: &composite.BackendService{
				SecurityPolicy: "https://www.googleapis.com/compute/beta/projects/test-project/global/securityPolicies/policy-2",
			},
			desiredConfig: &backendconfigv1beta1.BackendConfig{
				Spec: backendconfigv1beta1.BackendConfigSpec{
					SecurityPolicy: &backendconfigv1beta1.SecurityPolicyConfig{
						Name: "policy-1",
					},
				},
			},
			expectSetCall: true,
		},
		{
			desc: "remove-policy",
			currentBackendService: &composite.BackendService{
				SecurityPolicy: "https://www.googleapis.com/compute/beta/projects/test-project/global/securityPolicies/policy-1",
			},
			desiredConfig: &backendconfigv1beta1.BackendConfig{
				Spec: backendconfigv1beta1.BackendConfigSpec{
					SecurityPolicy: &backendconfigv1beta1.SecurityPolicyConfig{
						Name: "",
					},
				},
			},
			expectSetCall: true,
		},
		{
			desc: "same-policy",
			currentBackendService: &composite.BackendService{
				SecurityPolicy: "https://www.googleapis.com/compute/beta/projects/test-project/global/securityPolicies/policy-1",
			},
			desiredConfig: &backendconfigv1beta1.BackendConfig{
				Spec: backendconfigv1beta1.BackendConfigSpec{
					SecurityPolicy: &backendconfigv1beta1.SecurityPolicyConfig{
						Name: "policy-1",
					},
				},
			},
		},
		{
			desc:                  "empty-policy",
			currentBackendService: &composite.BackendService{},
			desiredConfig:         &backendconfigv1beta1.BackendConfig{},
		},
		{
			desc: "no-specified-policy",
			currentBackendService: &composite.BackendService{
				SecurityPolicy: "https://www.googleapis.com/compute/beta/projects/test-project/global/securityPolicies/policy-1",
			},
			desiredConfig: &backendconfigv1beta1.BackendConfig{},
			expectSetCall: false,
		},
	}

	for i, tc := range testCases {
		tc := tc
		i := i
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
			fakeBeName := fmt.Sprintf("be-name-XXX-%d", i)

			(fakeGCE.Compute().(*cloud.MockGCE)).MockBetaBackendServices.SetSecurityPolicyHook = setSecurityPolicyHook

			if err := EnsureSecurityPolicy(fakeGCE, utils.ServicePort{BackendConfig: tc.desiredConfig}, tc.currentBackendService, fakeBeName); err != nil {
				t.Errorf("EnsureSecurityPolicy()=%v, want nil", err)
			}

			if tc.expectSetCall {
				// Verify whether the desired policy is set.
				mockSecurityPolicyLock.Lock()
				policyRef, ok := mockSecurityPolcies[fakeBeName]
				mockSecurityPolicyLock.Unlock()
				if !ok {
					t.Errorf("policy not set for backend service %s", fakeBeName)
					return
				}
				policyLink := ""
				if policyRef != nil {
					policyLink = policyRef.SecurityPolicy
				}
				desiredPolicyName := ""
				if tc.desiredConfig != nil && tc.desiredConfig.Spec.SecurityPolicy != nil {
					desiredPolicyName = tc.desiredConfig.Spec.SecurityPolicy.Name
				}
				if utils.EqualResourceIDs(policyLink, desiredPolicyName) {
					t.Errorf("got policy %q, want %q", policyLink, desiredPolicyName)
				}
			} else {
				// Verify no set call is made.
				mockSecurityPolicyLock.Lock()
				policyRef, ok := mockSecurityPolcies[fakeBeName]
				mockSecurityPolicyLock.Unlock()
				if ok {
					t.Errorf("unexpected policy %q is set for backend service %s", policyRef, fakeBeName)
				}
			}
		})

	}
}
