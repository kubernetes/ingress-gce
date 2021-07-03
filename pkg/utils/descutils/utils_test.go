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

package descutils

import "testing"

func TestClusterLink(t *testing.T) {
	clusterName := "testCluster"
	clusterLoc := "location"

	testcases := []struct {
		desc         string
		regional     bool
		clusterName  string
		clusterLoc   string
		expectedLink string
	}{
		{
			desc:         "regional cluster",
			regional:     true,
			clusterName:  clusterName,
			clusterLoc:   clusterLoc,
			expectedLink: "/regions/location/clusters/testCluster",
		},
		{
			desc:         "zonal cluster",
			regional:     false,
			clusterName:  clusterName,
			clusterLoc:   clusterLoc,
			expectedLink: "/zones/location/clusters/testCluster",
		},
		{
			desc:         "empty cluster name",
			clusterName:  "",
			clusterLoc:   clusterLoc,
			expectedLink: "",
		},
		{
			desc:         "empty cluster location",
			clusterName:  clusterName,
			clusterLoc:   "",
			expectedLink: "",
		},
	}

	for _, tc := range testcases {
		t.Run(tc.desc, func(t *testing.T) {
			generatedLink := GenerateClusterLink(tc.clusterName, tc.clusterLoc, tc.regional)
			if generatedLink != tc.expectedLink {
				t.Errorf("unexpected cluster link: wanted %s, but got %s", tc.expectedLink, generatedLink)
			}
		})
	}
}

func TestK8sResourceLink(t *testing.T) {
	testNamespace := "testNamespace"
	resourceName := "resourceName"
	resourceType := "resourceType"
	expectedLink := "/namespaces/testNamespace/resourceType/resourceName"

	generatedLink := GenerateK8sResourceLink(testNamespace, resourceType, resourceName)
	if generatedLink != expectedLink {
		t.Errorf("unexpected cluster link: wanted %s, but got %s", expectedLink, generatedLink)
	}
}
