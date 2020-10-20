/*
Copyright 2020 The Kubernetes Authors.

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

package utils

import (
	"reflect"
	"testing"
)

func TestNegString(t *testing.T) {
	testCases := []struct {
		desc           string
		description    NegDescription
		expectedString string
	}{
		{
			desc: "all fields",
			description: NegDescription{
				ClusterUID:  "00000000001",
				Namespace:   "my-namespace",
				ServiceName: "my-service",
				Port:        "80",
			},
			expectedString: `{"cluster-uid":"00000000001","namespace":"my-namespace","service-name":"my-service","port":"80"}`,
		},
		{
			desc:           "empty",
			description:    NegDescription{},
			expectedString: "{}",
		},
	}

	for _, tc := range testCases {
		stringDesc := tc.description.String()
		if stringDesc != tc.expectedString {
			t.Errorf("%s: String()=%s, want %s", tc.desc, stringDesc, tc.expectedString)
		}
	}
}

func TestNegDescriptionFromString(t *testing.T) {
	testCases := []struct {
		desc         string
		negDesc      string
		expectedDesc NegDescription
		expectError  bool
	}{
		{
			desc:        "empty string",
			expectError: true,
		},
		{
			desc:        "invalid format",
			negDesc:     "invalid",
			expectError: true,
		},
		{
			desc:    "no feature",
			negDesc: `{"cluster-uid":"00000000001", "namespace":"my-namespace", "service-name":"my-service", "port":"80"}`,
			expectedDesc: NegDescription{
				ClusterUID:  "00000000001",
				Namespace:   "my-namespace",
				ServiceName: "my-service",
				Port:        "80",
			},
			expectError: false,
		},
		{
			desc:    "missing a field",
			negDesc: `{"cluster-uid":"00000000001","service-name":"my-service", "port":"80"}`,
			expectedDesc: NegDescription{
				ClusterUID:  "00000000001",
				ServiceName: "my-service",
				Port:        "80",
			},
			expectError: false,
		},
	}

	for _, tc := range testCases {
		description, err := NegDescriptionFromString(tc.negDesc)
		if err != nil && !tc.expectError {
			t.Errorf("%s: NegDescriptionFromString(%s) resulted in error: %s", tc.desc, tc.negDesc, err)
		}
		if !reflect.DeepEqual(*description, tc.expectedDesc) {
			t.Errorf("%s: NegDescriptionFromString(%s)=%s, want %s", tc.desc, tc.negDesc, description, tc.expectedDesc)
		}
	}
}

func TestVerifyDescription(t *testing.T) {
	negDesc := NegDescription{
		ClusterUID:  "00000000001",
		Namespace:   "my-namespace",
		ServiceName: "my-service",
		Port:        "80",
	}.String()

	testCases := []struct {
		desc          string
		negDescString string
		expectNegDesc NegDescription
		shouldMatch   bool
	}{
		{
			desc:          "fields match",
			negDescString: negDesc,
			expectNegDesc: NegDescription{
				ClusterUID:  "00000000001",
				Namespace:   "my-namespace",
				ServiceName: "my-service",
				Port:        "80",
			},
			shouldMatch: true,
		},
		{
			desc:          "empty description",
			negDescString: "",
			expectNegDesc: NegDescription{
				ClusterUID:  "00000000001",
				Namespace:   "my-namespace",
				ServiceName: "my-service",
				Port:        "80",
			},
			shouldMatch: true,
		},
		{
			desc:          "cluster uid doesn't match",
			negDescString: negDesc,
			expectNegDesc: NegDescription{
				ClusterUID:  "00000000002",
				Namespace:   "my-namespace",
				ServiceName: "my-service",
				Port:        "80",
			},
			shouldMatch: false,
		},
		{
			desc:          "namespace doesn't match",
			negDescString: negDesc,
			expectNegDesc: NegDescription{
				ClusterUID:  "00000000002",
				Namespace:   "not-my-namespace",
				ServiceName: "my-service",
				Port:        "80",
			},
			shouldMatch: false,
		},
		{
			desc:          "service name doesn't match",
			negDescString: negDesc,
			expectNegDesc: NegDescription{
				ClusterUID:  "00000000002",
				Namespace:   "my-namespace",
				ServiceName: "not-my-service",
				Port:        "80",
			},
			shouldMatch: false,
		},
		{
			desc:          "port doesn't match",
			negDescString: negDesc,
			expectNegDesc: NegDescription{
				ClusterUID:  "00000000002",
				Namespace:   "my-namespace",
				ServiceName: "my-service",
				Port:        "81",
			},
			shouldMatch: false,
		},
	}

	for _, tc := range testCases {
		matches, err := VerifyDescription(tc.expectNegDesc, tc.negDescString, "my-neg", "zone")
		if tc.shouldMatch && err != nil {
			t.Errorf("%s: VerifyDescription(%+v, %s) had an unexpected error: %s", tc.desc, tc.expectNegDesc, tc.negDescString, err)
		} else if !tc.shouldMatch && err == nil {
			t.Errorf("%s: VerifyDescription(%+v, %s) should have returned an error", tc.desc, tc.expectNegDesc, tc.negDescString)
		}

		if matches != tc.shouldMatch {
			t.Errorf("%s: VerifyDescription(%+v, %s) should result in %t, but was %t", tc.desc, tc.expectNegDesc, tc.negDescString, tc.shouldMatch, matches)
		}
	}
}
