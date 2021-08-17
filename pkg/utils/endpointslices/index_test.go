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

func TestEndpointSlicesByServiceFunc(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		desc         string
		obj          interface{}
		expectError  bool
		expectedKeys []string
	}{
		{
			desc:         "ReturnsEmptyForOtherObjects",
			obj:          "Not an Endpoint Slice",
			expectedKeys: []string{},
		},
		{
			desc: "ReturnsErrorForSlicesWithoutService",
			obj: &discovery.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "slice",
					Namespace: "nmspc",
				},
			},
			expectError: true,
		},
		{
			desc: "ReturnsKeyForProperSlices",
			obj: &discovery.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "slice",
					Namespace: "nmspc",
					Labels:    map[string]string{discovery.LabelServiceName: "srvc"},
				},
			},
			expectedKeys: []string{"nmspc/srvc"},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			keys, e := EndpointSlicesByServiceFunc(tc.obj)
			if tc.expectError && e == nil {
				t.Errorf("Expected error, got no error and keys %s", keys)
			}
			if !tc.expectError && e != nil {
				t.Errorf("Incorrect error, got: %v, expected nil", e)
			}
			if len(keys) != len(tc.expectedKeys) || (len(tc.expectedKeys) == 1 && keys[0] != tc.expectedKeys[0]) {
				t.Errorf("Incorrect keys, got: %s, expected %s", keys, tc.expectedKeys)
			}
		})
	}
}
