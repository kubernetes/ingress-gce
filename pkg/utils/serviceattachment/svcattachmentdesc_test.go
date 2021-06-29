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

func TestNewServiceAttachmentDesc(t *testing.T) {
	testCases := []struct {
		desc            string
		regional        bool
		expectedCluster string
		cluster         string
		location        string
	}{
		{
			desc:            "regional cluster",
			regional:        true,
			expectedCluster: "/regions/loc/clusters/cluster-name",
			cluster:         "cluster-name",
			location:        "loc",
		},
		{
			desc:            "zonal cluster",
			regional:        false,
			expectedCluster: "/zones/loc/clusters/cluster-name",
			cluster:         "cluster-name",
			location:        "loc",
		},
		{
			desc:            "empty cluster name",
			regional:        false,
			expectedCluster: "",
			location:        "loc",
		},
		{
			desc:            "empty location",
			regional:        false,
			expectedCluster: "",
			cluster:         "cluster-name",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			desc := NewServiceAttachmentDesc("ns", "sa-name", tc.cluster, tc.location, tc.regional)
			expectedDesc := ServiceAttachmentDesc{
				K8sResource: "/namespaces/ns/serviceattachments/sa-name",
				K8sCluster:  tc.expectedCluster,
			}

			if !reflect.DeepEqual(desc, expectedDesc) {
				t.Errorf("Expected NewServiceAttachmentDesc to return %+v, but got %+v", expectedDesc, desc)
			}
		})
	}

}

func TestServiceAttachmentDescString(t *testing.T) {
	testCases := []struct {
		desc           string
		description    ServiceAttachmentDesc
		expectedString string
	}{
		{
			desc: "populated fields",
			description: ServiceAttachmentDesc{
				K8sResource: "resource",
				K8sCluster:  "cluster",
			},
			expectedString: `{"k8sResource":"resource","k8sCluster":"cluster"}`,
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
		saDesc       string
		expectedDesc ServiceAttachmentDesc
		expectError  bool
	}{
		{
			desc:        "empty string",
			expectError: true,
		},
		{
			desc:        "invalid format",
			saDesc:      "invalid",
			expectError: true,
		},
		{
			desc:   "populated description",
			saDesc: `{"k8sResource":"resource","k8sCluster":"cluster"}`,
			expectedDesc: ServiceAttachmentDesc{
				K8sResource: "resource",
				K8sCluster:  "cluster",
			},
			expectError: false,
		},
		{
			desc:         "empty description",
			saDesc:       `{}`,
			expectedDesc: ServiceAttachmentDesc{},
			expectError:  false,
		},
	}

	for _, tc := range testCases {
		description, err := ServiceAttachmentDescFromString(tc.saDesc)
		if err != nil && !tc.expectError {
			t.Errorf("%s: ServiceAttachmentDescFromString(%s) resulted in error: %s", tc.desc, tc.saDesc, err)
		}
		if !reflect.DeepEqual(*description, tc.expectedDesc) {
			t.Errorf("%s: ServiceAttachmentDescFromString(%s)=%s, want %s", tc.desc, tc.saDesc, description, tc.expectedDesc)
		}
	}
}
