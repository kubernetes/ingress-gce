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

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/api/compute/v1"
	"k8s.io/legacy-cloud-providers/gce"

	backendconfigv1 "k8s.io/ingress-gce/pkg/apis/backendconfig/v1"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/utils"
)

func TestEnsureSecurityPolicy(t *testing.T) {
	mockSecurityPolicies := make(map[string]*compute.SecurityPolicyReference)
	mockSecurityPolicyLock := sync.Mutex{}
	setSecurityPolicyHook := func(_ context.Context, key *meta.Key, ref *compute.SecurityPolicyReference, _ *cloud.MockBackendServices) error {
		mockSecurityPolicyLock.Lock()
		mockSecurityPolicies[key.Name] = ref
		mockSecurityPolicyLock.Unlock()
		return nil
	}

	testCases := []struct {
		desc                  string
		currentBackendService *composite.BackendService
		desiredConfig         *backendconfigv1.BackendConfig
		expectSetCall         bool
		expectError           bool
	}{
		{
			desc:                  "attach-policy",
			currentBackendService: &composite.BackendService{Scope: meta.Global},
			desiredConfig: &backendconfigv1.BackendConfig{
				Spec: backendconfigv1.BackendConfigSpec{
					SecurityPolicy: &backendconfigv1.SecurityPolicyConfig{
						Name: "policy-1",
					},
				},
			},
			expectSetCall: true,
		},
		{
			desc: "update-policy",
			currentBackendService: &composite.BackendService{
				Scope:          meta.Global,
				SecurityPolicy: "https://www.googleapis.com/compute/projects/test-project/global/securityPolicies/policy-2",
			},
			desiredConfig: &backendconfigv1.BackendConfig{
				Spec: backendconfigv1.BackendConfigSpec{
					SecurityPolicy: &backendconfigv1.SecurityPolicyConfig{
						Name: "policy-1",
					},
				},
			},
			expectSetCall: true,
		},
		{
			desc: "remove-policy",
			currentBackendService: &composite.BackendService{
				Scope:          meta.Global,
				SecurityPolicy: "https://www.googleapis.com/compute/projects/test-project/global/securityPolicies/policy-1",
			},
			desiredConfig: &backendconfigv1.BackendConfig{
				Spec: backendconfigv1.BackendConfigSpec{
					SecurityPolicy: &backendconfigv1.SecurityPolicyConfig{
						Name: "",
					},
				},
			},
			expectSetCall: true,
		},
		{
			desc: "same-policy",
			currentBackendService: &composite.BackendService{
				Scope:          meta.Global,
				SecurityPolicy: "https://www.googleapis.com/compute/projects/test-project/global/securityPolicies/policy-1",
			},
			desiredConfig: &backendconfigv1.BackendConfig{
				Spec: backendconfigv1.BackendConfigSpec{
					SecurityPolicy: &backendconfigv1.SecurityPolicyConfig{
						Name: "policy-1",
					},
				},
			},
		},
		{
			desc:                  "empty-policy",
			currentBackendService: &composite.BackendService{Scope: meta.Global},
			desiredConfig:         &backendconfigv1.BackendConfig{},
		},
		{
			desc: "no-specified-policy",
			currentBackendService: &composite.BackendService{
				Scope:          meta.Global,
				SecurityPolicy: "https://www.googleapis.com/compute/projects/test-project/global/securityPolicies/policy-1",
			},
			desiredConfig: &backendconfigv1.BackendConfig{},
			expectSetCall: false,
		},
		{
			desc: "regional backend service",
			currentBackendService: &composite.BackendService{
				Scope: meta.Regional,
			},
			desiredConfig: &backendconfigv1.BackendConfig{
				Spec: backendconfigv1.BackendConfigSpec{
					SecurityPolicy: &backendconfigv1.SecurityPolicyConfig{
						Name: "policy-1",
					},
				},
			},
			expectSetCall: false,
			expectError:   true,
		},
	}

	for i, tc := range testCases {
		tc := tc
		i := i
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
			tc.currentBackendService.Name = fmt.Sprintf("be-name-XXX-%d", i)

			(fakeGCE.Compute().(*cloud.MockGCE)).MockBackendServices.SetSecurityPolicyHook = setSecurityPolicyHook

			err := EnsureSecurityPolicy(fakeGCE, utils.ServicePort{BackendConfig: tc.desiredConfig}, tc.currentBackendService)
			if !tc.expectError && err != nil {
				t.Errorf("EnsureSecurityPolicy()=%v, want nil", err)
			}
			if tc.expectError && err == nil {
				t.Errorf("EnsureSecurityPolicy()=nil, want non-nil error")
			}

			if tc.expectSetCall {
				// Verify whether the desired policy is set.
				mockSecurityPolicyLock.Lock()
				policyRef, ok := mockSecurityPolicies[tc.currentBackendService.Name]
				mockSecurityPolicyLock.Unlock()
				if !ok {
					t.Errorf("policy not set for backend service %s", tc.currentBackendService.Name)
					return
				}
				var desiredPolicyRef *compute.SecurityPolicyReference
				if tc.desiredConfig.Spec.SecurityPolicy != nil && tc.desiredConfig.Spec.SecurityPolicy.Name != "" {
					desiredPolicyRef = &compute.SecurityPolicyReference{
						SecurityPolicy: cloud.SelfLink(meta.VersionGA, fakeGCE.ProjectID(), "securityPolicies", meta.GlobalKey(tc.desiredConfig.Spec.SecurityPolicy.Name)),
					}
				}
				if diff := cmp.Diff(desiredPolicyRef, policyRef); diff != "" {
					t.Errorf("Got diff for policy reference (-want +got):\n%s", diff)
				}
			} else {
				// Verify no set call is made.
				mockSecurityPolicyLock.Lock()
				policyRef, ok := mockSecurityPolicies[tc.currentBackendService.Name]
				mockSecurityPolicyLock.Unlock()
				if ok {
					t.Errorf("unexpected policy %v is set for backend service %s", policyRef, tc.currentBackendService.Name)
				}
			}
		})
	}
}
