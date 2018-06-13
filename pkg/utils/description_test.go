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

package utils

import (
	"reflect"
	"testing"
)

func TestString(t *testing.T) {
	testCases := []struct {
		desc           string
		description    Description
		expectedString string
	}{
		{
			desc:           "empty description",
			description:    Description{},
			expectedString: ``,
		}, {
			desc: "no feature",
			description: Description{
				ServiceName: "my-service",
				ServicePort: "my-port",
			},
			expectedString: `{"kubernetes.io/service-name":"my-service","kubernetes.io/service-port":"my-port"}`,
		},
		{
			desc: "single feature",
			description: Description{
				ServiceName: "my-service",
				ServicePort: "my-port",
				XFeatures:   []string{"feature1"},
			},
			expectedString: `{"kubernetes.io/service-name":"my-service","kubernetes.io/service-port":"my-port","x-features":["feature1"]}`,
		},
		{
			desc: "multiple features",
			description: Description{
				ServiceName: "my-service",
				ServicePort: "my-port",
				XFeatures:   []string{"feature1", "feature2"},
			},
			expectedString: `{"kubernetes.io/service-name":"my-service","kubernetes.io/service-port":"my-port","x-features":["feature1","feature2"]}`,
		},
	}

	for _, tc := range testCases {
		gotDesc := tc.description.String()
		if gotDesc != tc.expectedString {
			t.Errorf("%s: String()=%s, want %s", tc.desc, gotDesc, tc.expectedString)
		}
	}
}

func TestGetDescriptionFromString(t *testing.T) {
	testCases := []struct {
		desc               string
		backendServiceDesc string
		expectedDesc       Description
	}{
		{
			desc:         "empty string",
			expectedDesc: Description{},
		},
		{
			desc:               "invalid format",
			backendServiceDesc: "invalid",
			expectedDesc:       Description{},
		},
		{
			desc:               "no feature",
			backendServiceDesc: `{"kubernetes.io/service-name":"my-service","kubernetes.io/service-port":"my-port"}`,
			expectedDesc:       Description{ServiceName: "my-service", ServicePort: "my-port"},
		},
		{
			desc:               "single feature",
			backendServiceDesc: `{"kubernetes.io/service-name":"my-service","kubernetes.io/service-port":"my-port","x-features":["feature1"]}`,
			expectedDesc:       Description{ServiceName: "my-service", ServicePort: "my-port", XFeatures: []string{"feature1"}},
		},
		{
			desc:               "multiple features",
			backendServiceDesc: `{"kubernetes.io/service-name":"my-service","kubernetes.io/service-port":"my-port","x-features":["feature1","feature2"]}`,
			expectedDesc:       Description{ServiceName: "my-service", ServicePort: "my-port", XFeatures: []string{"feature1", "feature2"}},
		},
	}

	for _, tc := range testCases {
		description := GetDescriptionFromString(tc.backendServiceDesc)
		if !reflect.DeepEqual(description, tc.expectedDesc) {
			t.Errorf("%s: GetDescriptionFromString(%s)=%s, want %s", tc.desc, tc.backendServiceDesc, description, tc.expectedDesc)
		}
	}
}
