/*
Copyright 2022 The Kubernetes Authors.

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
	"k8s.io/klog/v2"
	"testing"

	backendconfigv1 "k8s.io/ingress-gce/pkg/apis/backendconfig/v1"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/utils"
)

var testCustomResponseHeader = []string{"X-TEST-HEADER:{test-value}"}

func TestEnsureCustomResponseHeaders(t *testing.T) {
	testCases := []struct {
		desc           string
		sp             utils.ServicePort
		be             *composite.BackendService
		updateExpected bool
	}{
		{
			desc:           "Custom Response Headers missing from both ends, no update needed",
			sp:             utils.ServicePort{BackendConfig: &backendconfigv1.BackendConfig{}},
			be:             &composite.BackendService{},
			updateExpected: false,
		},
		{
			desc: "No header in current setting, new headers in backendConfig, update needed",
			sp: utils.ServicePort{BackendConfig: &backendconfigv1.BackendConfig{
				Spec: backendconfigv1.BackendConfigSpec{
					CustomResponseHeaders: &backendconfigv1.CustomResponseHeadersConfig{
						Headers: testCustomResponseHeader,
					},
				},
			}},
			be:             &composite.BackendService{},
			updateExpected: true,
		},
		{
			desc: "Having headers in current setting, no header in backendConfig, update needed",
			sp:   utils.ServicePort{BackendConfig: &backendconfigv1.BackendConfig{}},
			be: &composite.BackendService{
				CustomResponseHeaders: testCustomResponseHeader,
			},
			updateExpected: true,
		},
		{
			desc: "Having headers in current setting, new headers in backendConfig, update needed",
			sp: utils.ServicePort{BackendConfig: &backendconfigv1.BackendConfig{
				Spec: backendconfigv1.BackendConfigSpec{
					CustomResponseHeaders: &backendconfigv1.CustomResponseHeadersConfig{
						Headers: append(testCustomResponseHeader, "X-TEST-HEADER2:{test-value2}"),
					},
				},
			}},
			be: &composite.BackendService{
				CustomResponseHeaders: testCustomResponseHeader,
			},
			updateExpected: true,
		},
		{
			desc: "Identical headers, no update",
			sp: utils.ServicePort{BackendConfig: &backendconfigv1.BackendConfig{
				Spec: backendconfigv1.BackendConfigSpec{
					CustomResponseHeaders: &backendconfigv1.CustomResponseHeadersConfig{
						Headers: testCustomResponseHeader,
					},
				},
			}},
			be: &composite.BackendService{
				CustomResponseHeaders: testCustomResponseHeader,
			},
			updateExpected: false,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			result := EnsureCustomResponseHeaders(tc.sp, tc.be, klog.TODO())
			if result != tc.updateExpected {
				t.Errorf("Expected %v but got %v", tc.updateExpected, result)
			}
		})
	}
}
