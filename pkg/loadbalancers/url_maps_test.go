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

package loadbalancers

import (
	"testing"

	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/ingress-gce/pkg/composite"
)

func TestComputeURLMapEquals(t *testing.T) {
	t.Parallel()

	m := testCompositeURLMap()
	// Test equality.
	same := testCompositeURLMap()
	if !mapsEqual(m, same) {
		t.Errorf("mapsEqual(%+v, %+v) = false, want true", m, same)
	}

	// Test different default backend.
	diffDefault := testCompositeURLMap()
	diffDefault.DefaultService = "/global/backendServices/some-service"
	if mapsEqual(m, diffDefault) {
		t.Errorf("mapsEqual(%+v, %+v) = true, want false", m, diffDefault)
	}
}

func testCompositeURLMap() *composite.UrlMap {
	return &composite.UrlMap{
		Name:           "k8s-um-lb-name",
		DefaultService: "global/backendServices/k8s-be-30000--uid1",
		HostRules: []*composite.HostRule{
			{
				Hosts:       []string{"abc.com"},
				PathMatcher: "host929ba26f492f86d4a9d66a080849865a",
			},
			{
				Hosts:       []string{"foo.bar.com"},
				PathMatcher: "host2d50cf9711f59181be6a5e5658e42c21",
			},
		},
		PathMatchers: []*composite.PathMatcher{
			{
				DefaultService: "global/backendServices/k8s-be-30000--uid1",
				Name:           "host929ba26f492f86d4a9d66a080849865a",
				PathRules: []*composite.PathRule{
					{
						Paths:   []string{"/web"},
						Service: "global/backendServices/k8s-be-32000--uid1",
					},
					{
						Paths:   []string{"/other"},
						Service: "global/backendServices/k8s-be-32500--uid1",
					},
				},
			},
			{
				DefaultService: "global/backendServices/k8s-be-30000--uid1",
				Name:           "host2d50cf9711f59181be6a5e5658e42c21",
				PathRules: []*composite.PathRule{
					{
						Paths:   []string{"/"},
						Service: "global/backendServices/k8s-be-33000--uid1",
					},
					{
						Paths:   []string{"/*"},
						Service: "global/backendServices/k8s-be-33500--uid1",
					},
				},
			},
		},
	}
}

func TestGetBackendNames(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		urlMap    *composite.UrlMap
		wantNames []string
		wantErr   bool
	}{
		"Valid UrlMap": {
			urlMap: &composite.UrlMap{
				DefaultService: "global/backendServices/service-A",
				PathMatchers: []*composite.PathMatcher{
					{
						DefaultService: "global/backendServices/service-B",
						PathRules: []*composite.PathRule{
							{
								Paths:   []string{"/"},
								Service: "global/backendServices/service-C",
							},
						},
					},
				},
			},
			wantNames: []string{"service-A", "service-B", "service-C"},
		},
		"Invalid DefaultService": {
			urlMap: &composite.UrlMap{
				DefaultService: "/global/backendServices/service-A",
			},
			wantErr: true,
		},
		"Invalid PathMatcher DefaultService": {
			urlMap: &composite.UrlMap{
				DefaultService: "global/backendServices/service-A",
				PathMatchers: []*composite.PathMatcher{
					{
						DefaultService: "/global/backendServices/service-B",
					},
				},
			},
			wantErr: true,
		},
		"Invalid PathRule Service": {
			urlMap: &composite.UrlMap{
				DefaultService: "global/backendServices/service-A",
				PathMatchers: []*composite.PathMatcher{
					{
						DefaultService: "global/backendServices/service-B",
						PathRules: []*composite.PathRule{
							{
								Paths:   []string{"/"},
								Service: "/global/backendServices/service-C",
							},
						},
					},
				},
			},
			wantErr: true,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			gotNames, gotErr := getBackendNames(tc.urlMap)
			if (gotErr != nil) != tc.wantErr {
				t.Errorf("getBackendNames(%v) = _, %v, want err? %v", tc.urlMap, gotErr, tc.wantErr)
			}
			if gotErr != nil {
				return
			}

			if !sets.NewString(gotNames...).Equal(sets.NewString(tc.wantNames...)) {
				t.Errorf("getBackendNames(%v) = %v, want %v", tc.urlMap, gotNames, tc.wantNames)
			}
		})
	}
}
