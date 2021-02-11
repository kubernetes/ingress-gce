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

package serviceattachment

import (
	"reflect"
	"testing"
)

func TestServiceAttachmentDescString(t *testing.T) {
	testCases := []struct {
		desc           string
		description    ServiceAttachmentDesc
		expectedString string
	}{
		{
			desc: "populated url",
			description: ServiceAttachmentDesc{
				URL: "url",
			},
			expectedString: `{"url":"url"}`,
		},
		{
			desc:           "empty",
			description:    ServiceAttachmentDesc{},
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

func TestServiceAttachmentDescFromString(t *testing.T) {
	testCases := []struct {
		desc         string
		negDesc      string
		expectedDesc ServiceAttachmentDesc
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
			desc:    "populated description",
			negDesc: `{"url":"url"}`,
			expectedDesc: ServiceAttachmentDesc{
				URL: "url",
			},
			expectError: false,
		},
		{
			desc:         "empty description",
			negDesc:      `{}`,
			expectedDesc: ServiceAttachmentDesc{},
			expectError:  false,
		},
	}

	for _, tc := range testCases {
		description, err := ServiceAttachmentDescFromString(tc.negDesc)
		if err != nil && !tc.expectError {
			t.Errorf("%s: ServiceAttachmentDescFromString(%s) resulted in error: %s", tc.desc, tc.negDesc, err)
		}
		if !reflect.DeepEqual(*description, tc.expectedDesc) {
			t.Errorf("%s: ServiceAttachmentDescFromString(%s)=%s, want %s", tc.desc, tc.negDesc, description, tc.expectedDesc)
		}
	}
}
