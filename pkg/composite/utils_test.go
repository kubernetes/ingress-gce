/*
Copyright 2019 The Kubernetes Authors.

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

package composite

import (
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"k8s.io/kubernetes/pkg/cloudprovider/providers/gce"
	"reflect"
	"testing"
)

func TestCreateKey(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		desc         string
		resourceType meta.KeyType
		expected     *meta.Key
		wantErr      bool
	}{
		{
			desc:         "Global Key",
			resourceType: meta.Global,
			expected:     meta.GlobalKey("test"),
		},
		{
			desc:         "Regional Key",
			resourceType: meta.Regional,
			expected:     meta.RegionalKey("test", gce.DefaultTestClusterValues().Region),
		},
		{
			desc:         "Zonal Key",
			resourceType: meta.Zonal,
			expected:     nil,
			wantErr:      true,
		},
	}
	fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			result, err := CreateKey(fakeGCE, "test", tc.resourceType)

			if tc.wantErr {
				if err == nil {
					t.Fatalf("err = nil, want non-nil")
				} else {
					return
				}
			}

			if err != nil {
				t.Fatalf("CreateKey(fakeGCE, test, %s) = %v, want %v", tc.resourceType, err, tc.expected)
			}

			if !reflect.DeepEqual(result, tc.expected) {
				t.Fatalf("CreateKey(fakeGCE, test, %s) = %v, want %v", tc.resourceType, result, tc.expected)
			}
		})
	}
}

func TestIsRegionalResource(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		desc     string
		selfLink string
		expected bool
		wantErr  bool
	}{
		{
			desc:     "Regional resource",
			selfLink: "https://www.googleapis.com/compute/alpha/projects/gke-dev/regions/us-central1/urlMaps/desc",
			expected: true,
		},
		{
			desc:     "Global resource",
			selfLink: "https://www.googleapis.com/compute/alpha/projects/gke-dev/global/urlMaps/desc",
			expected: false,
		},
		{
			desc:     "Invalid resource",
			selfLink: "a/b/c/d/e/f/",
			expected: false,
			wantErr:  true,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			result, err := IsRegionalResource(tc.selfLink)

			if tc.wantErr {
				if err == nil {
					t.Fatalf("err = nil, want non-nil")
				} else {
					return
				}
			}

			if err != nil || result != tc.expected {
				t.Fatalf("IsRegionalResource(%v) = %v, want %v", tc.selfLink, err, tc.expected)
			}
		})
	}
}

func TestParseScope(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		desc     string
		selfLink string
		expected meta.KeyType
		wantErr  bool
	}{
		{
			desc:     "Regional resource",
			selfLink: "https://www.googleapis.com/compute/alpha/projects/gke-dev/regions/us-central1/urlMaps/desc",
			expected: meta.Regional,
		},
		{
			desc:     "Global resource",
			selfLink: "https://www.googleapis.com/compute/alpha/projects/gke-dev/global/urlMaps/desc",
			expected: meta.Global,
		},
		{
			desc:     "Zonal resource",
			selfLink: "https://www.googleapis.com/compute/alpha/projects/gke-dev/zones/us-central1-a/urlMaps/desc",
			expected: meta.Zonal,
		},
		{
			desc:     "Invalid resource",
			selfLink: "a/b/c/d/e/f/",
			expected: "",
			wantErr:  true,
		},
		{
			desc:     "Empty string",
			selfLink: "",
			expected: "",
			wantErr:  true,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			result, err := ScopeFromSelfLink(tc.selfLink)

			if tc.wantErr {
				if err == nil {
					t.Fatalf("err = nil, want non-nil")
				}
				return
			}

			if err != nil || result != tc.expected {
				t.Fatalf("IsRegionalResource(%v) = %v, want %v", tc.selfLink, err, tc.expected)
			}
		})
	}
}
