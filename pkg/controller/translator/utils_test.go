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

package translator

import (
	"reflect"
	"testing"

	api_v1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
)

var (
	testSvc = api_v1.Service{
		Spec: api_v1.ServiceSpec{
			Ports: []api_v1.ServicePort{
				{
					Name: "foo",
					Port: 1000,
				},
				{
					Name: "bar",
					Port: 1001,
				},
				{
					Name: "baz",
					Port: 1002,
				},
				{
					Name: "qux",
					Port: 1003,
				},
			},
		},
	}
)

func TestServicePort(t *testing.T) {
	testCases := []struct {
		desc     string
		port     networkingv1.ServiceBackendPort
		expected *api_v1.ServicePort
	}{
		{
			desc: "match on port number",
			port: networkingv1.ServiceBackendPort{Number: 1000},
			expected: &api_v1.ServicePort{
				Name: "foo",
				Port: 1000,
			},
		},
		{
			desc: "match on port name",
			port: networkingv1.ServiceBackendPort{Name: "foo"},
			expected: &api_v1.ServicePort{
				Name: "foo",
				Port: 1000,
			},
		},
		{
			desc: "match on last port number",
			port: networkingv1.ServiceBackendPort{Number: 1003},
			expected: &api_v1.ServicePort{
				Name: "qux",
				Port: 1003,
			},
		},
		{
			desc:     "no match",
			port:     networkingv1.ServiceBackendPort{Number: 3000},
			expected: nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			result := ServicePort(testSvc, tc.port)
			if !reflect.DeepEqual(result, tc.expected) {
				t.Errorf("%s: expected %+v but got %+v", tc.desc, tc.expected, result)
			}
		})
	}
}
