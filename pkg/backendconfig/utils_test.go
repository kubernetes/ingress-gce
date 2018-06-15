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

package backendconfig

import (
	"testing"

	apiv1 "k8s.io/api/core/v1"

	"k8s.io/ingress-gce/pkg/annotations"
)

func TestBackendConfigName(t *testing.T) {
	testCases := []struct {
		desc           string
		backendConfigs annotations.BackendConfigs
		svcPort        apiv1.ServicePort
		expected       string
	}{
		{
			desc: "matched on port name",
			backendConfigs: annotations.BackendConfigs{
				Ports:   map[string]string{"http": "config-http"},
				Default: "default",
			},
			svcPort:  apiv1.ServicePort{Name: "http"},
			expected: "config-http",
		},
		{
			desc: "matched on port number",
			backendConfigs: annotations.BackendConfigs{
				Ports:   map[string]string{"80": "config-http"},
				Default: "default",
			},
			svcPort:  apiv1.ServicePort{Name: "foo", Port: 80},
			expected: "config-http",
		},
		{
			desc: "matched on default",
			backendConfigs: annotations.BackendConfigs{
				Ports:   map[string]string{"http": "config-http"},
				Default: "default",
			},
			svcPort:  apiv1.ServicePort{Name: "https", Port: 443},
			expected: "default",
		},
		{
			desc: "no match",
			backendConfigs: annotations.BackendConfigs{
				Ports: map[string]string{"http": "config-http"},
			},
			svcPort:  apiv1.ServicePort{Name: "https", Port: 443},
			expected: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			result := BackendConfigName(tc.backendConfigs, tc.svcPort)
			if result != tc.expected {
				t.Errorf("%s: expected %s but got %s", tc.desc, tc.expected, result)
			}
		})
	}
}
