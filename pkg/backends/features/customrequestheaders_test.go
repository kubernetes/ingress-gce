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

package features

import (
	"testing"

	backendconfigv1 "k8s.io/ingress-gce/pkg/apis/backendconfig/v1"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/utils"
)

var testCustomHeader = []string{"X-TEST-HEADER:{test-value}"}

func TestEnsureCustomRequestHeaders(t *testing.T) {
	testCases := []struct {
		desc           string
		sp             utils.ServicePort
		be             *composite.BackendService
		updateExpected bool
	}{
		{
			desc:           "custom Request Headers missing from both ends, no update needed",
			sp:             utils.ServicePort{BackendConfig: &backendconfigv1.BackendConfig{}},
			be:             &composite.BackendService{},
			updateExpected: false,
		},
		{
			desc: "settings are identical, no update needed",
			sp: utils.ServicePort{
				BackendConfig: &backendconfigv1.BackendConfig{
					Spec: backendconfigv1.BackendConfigSpec{
						CustomRequestHeaders: &backendconfigv1.CustomRequestHeadersConfig{
							Headers: testCustomHeader,
						},
					},
				},
			},
			be: &composite.BackendService{
				CustomRequestHeaders: testCustomHeader,
			},
			updateExpected: false,
		},
		{
			desc: "settings are different, update needed",
			sp: utils.ServicePort{
				BackendConfig: &backendconfigv1.BackendConfig{
					Spec: backendconfigv1.BackendConfigSpec{
						CustomRequestHeaders: &backendconfigv1.CustomRequestHeadersConfig{
							Headers: testCustomHeader,
						},
					},
				},
			},
			be: &composite.BackendService{
				CustomRequestHeaders: append(testCustomHeader, "X-TEST-HEADER2:{test-value2}"),
			},
			updateExpected: true,
		},
		{
			desc: "backend config empty",
			sp: utils.ServicePort{
				BackendConfig: &backendconfigv1.BackendConfig{},
			},
			be: &composite.BackendService{
				CustomRequestHeaders: testCustomHeader,
			},
			updateExpected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			result := EnsureCustomRequestHeaders(tc.sp, tc.be)
			if result != tc.updateExpected {
				t.Errorf("Expected %v but got %v", tc.updateExpected, result)
			}
		})
	}
}
