/*
Copyright 2021 The Kubernetes Authors.

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

package endpointslices

import (
	"testing"

	discovery "k8s.io/api/discovery/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestFormatEndpointSlicesServiceKey(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		desc        string
		namespace   string
		name        string
		expectedKey string
	}{
		{
			desc:        "Empty namespace",
			namespace:   "",
			name:        "svc",
			expectedKey: "svc",
		},
		{
			desc:        "Not empty namespace",
			namespace:   "namespace",
			name:        "svc",
			expectedKey: "namespace/svc",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			key := FormatEndpointSlicesServiceKey(tc.namespace, tc.name)
			if key != tc.expectedKey {
				t.Errorf("Incorrect key, got: %s, expected %s", key, tc.expectedKey)
			}
		})
	}
}

func TestEndpointSlicesServiceKey(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		desc          string
		slice         *discovery.EndpointSlice
		expectedKey   string
		expectedError bool
	}{
		{
			desc: "Proper key for proper slice",
			slice: &discovery.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "slice",
					Namespace: "nmspc",
					Labels:    map[string]string{discovery.LabelServiceName: "svc"},
				},
			},
			expectedKey:   "nmspc/svc",
			expectedError: false,
		},
		{
			desc: "Proper key without namespace",
			slice: &discovery.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "slice",
					Namespace: "",
					Labels:    map[string]string{discovery.LabelServiceName: "svc"},
				},
			},
			expectedKey:   "svc",
			expectedError: false,
		},
		{
			desc: "Error with slice without service name label",
			slice: &discovery.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "slice",
					Namespace: "",
					Labels:    map[string]string{},
				},
			},
			expectedError: true,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			key, err := EndpointSlicesServiceKey(tc.slice)
			if tc.expectedError {
				if err == nil {
					t.Errorf("Expected error, got key %s", key)
				}
			} else {
				if key != tc.expectedKey {
					t.Errorf("Expected %s, got %s", tc.expectedKey, key)
				}
			}
		})
	}

}
