/*
Copyright 2017 The Kubernetes Authors.

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
	"testing"

	extensions "k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func TestTrimFieldsEvenly(t *testing.T) {
	t.Parallel()
	longString := "01234567890123456789012345678901234567890123456789"
	testCases := []struct {
		desc   string
		fields []string
		expect []string
		max    int
	}{
		{
			"no change",
			[]string{longString},
			[]string{longString},
			100,
		},
		{
			"equal to max and no change",
			[]string{longString, longString},
			[]string{longString, longString},
			100,
		},
		{
			"equally trimmed to half",
			[]string{longString, longString},
			[]string{longString[:25], longString[:25]},
			50,
		},
		{
			"trimmed to only 10",
			[]string{longString, longString, longString},
			[]string{longString[:4], longString[:3], longString[:3]},
			10,
		},
		{
			"trimmed to only 3",
			[]string{longString, longString, longString},
			[]string{longString[:1], longString[:1], longString[:1]},
			3,
		},
		{
			"one long field with one short field",
			[]string{longString, longString[:1]},
			[]string{longString[:1], ""},
			1,
		},
		{
			"one long field with one short field and trimmed to 5",
			[]string{longString, longString[:1]},
			[]string{longString[:5], ""},
			5,
		},
	}

	for _, tc := range testCases {
		res := trimFieldsEvenly(tc.max, tc.fields...)
		if len(res) != len(tc.expect) {
			t.Fatalf("%s: expect length == %d, got %d", tc.desc, len(tc.expect), len(res))
		}

		totalLen := 0
		for i := range res {
			totalLen += len(res[i])
			if res[i] != tc.expect[i] {
				t.Errorf("%s: the %d field is want to be %q, but got %q", tc.desc, i, tc.expect[i], res[i])
			}
		}

		if tc.max < totalLen {
			t.Errorf("%s: expect totalLen to be less than %d, but got %d", tc.desc, tc.max, totalLen)
		}
	}
}

func TestResourcePath(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		url  string
		want string
	}{
		{
			"global/backendServices/foo",
			"global/backendServices/foo",
		},
		{
			"https://www.googleapis.com/compute/v1/projects/foo/global/backendServices/foo",
			"global/backendServices/foo",
		},
		{
			"https://www.googleapis.com/compute/v1/projects/foo/BAD-INPUT/zones/us-central1-c/backendServices/foo",
			"",
		},
	}

	for _, tc := range testCases {
		res, _ := ResourcePath(tc.url)
		if res != tc.want {
			t.Errorf("ResourcePath(%q) = %q, want %q", tc.url, res, tc.want)
		}
	}
}

func TestToNamespacedName(t *testing.T) {
	t.Parallel()
	cases := []struct {
		input   string
		wantErr bool
		wantOut types.NamespacedName
	}{
		{
			input:   "kube-system/default-http-backend",
			wantOut: types.NamespacedName{Namespace: "kube-system", Name: "default-http-backend"},
		},
		{
			input:   "abc",
			wantErr: true,
		},
		{
			input:   "",
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			gotOut, gotErr := ToNamespacedName(tc.input)
			if tc.wantErr != (gotErr != nil) {
				t.Errorf("ToNamespacedName(%v) = _, %v, want err? %v", tc.input, gotErr, tc.wantErr)
			}
			if tc.wantErr {
				return
			}

			if gotOut != tc.wantOut {
				t.Errorf("ToNamespacedName(%v) = %v, want %v", tc.input, gotOut, tc.wantOut)
			}
		})
	}
}

func TestEqualResourcePaths(t *testing.T) {
	t.Parallel()
	testCases := map[string]struct {
		a    string
		b    string
		want bool
	}{
		"partial vs full": {
			a:    "https://www.googleapis.com/compute/beta/projects/project-id/zones/us-central1-a/instanceGroups/example-group",
			b:    "zones/us-central1-a/instanceGroups/example-group",
			want: true,
		},
		"full vs full": {
			a:    "https://www.googleapis.com/compute/beta/projects/project-id/zones/us-central1-a/instanceGroups/example-group",
			b:    "https://www.googleapis.com/compute/beta/projects/project-id/zones/us-central1-a/instanceGroups/example-group",
			want: true,
		},
		"diff projects and versions": {
			a:    "https://www.googleapis.com/compute/v1/projects/project-A/zones/us-central1-a/instanceGroups/example-group",
			b:    "https://www.googleapis.com/compute/beta/projects/project-B/zones/us-central1-a/instanceGroups/example-group",
			want: true,
		},
		"diff name": {
			a:    "https://www.googleapis.com/compute/v1/projects/project-A/zones/us-central1-a/instanceGroups/example-groupA",
			b:    "https://www.googleapis.com/compute/beta/projects/project-B/zones/us-central1-a/instanceGroups/example-groupB",
			want: false,
		},
		"diff location": {
			a:    "https://www.googleapis.com/compute/v1/projects/project-A/zones/us-central1-a/instanceGroups/example-group",
			b:    "https://www.googleapis.com/compute/beta/projects/project-B/zones/us-central1-b/instanceGroups/example-group",
			want: false,
		},
		"diff resource": {
			a:    "https://www.googleapis.com/compute/v1/projects/project-A/zones/us-central1-a/backendServices/example-group",
			b:    "https://www.googleapis.com/compute/beta/projects/project-B/zones/us-central1-b/instanceGroups/example-group",
			want: false,
		},
		"bad input a": {
			a:    "/project-A/zones/us-central1-a/backendServices/example-group",
			b:    "https://www.googleapis.com/compute/beta/projects/project-B/zones/us-central1-b/instanceGroups/example-group",
			want: false,
		},
		"bad input b": {
			a:    "https://www.googleapis.com/compute/beta/projects/project-B/zones/us-central1-b/instanceGroups/example-group",
			b:    "/project-A/zones/us-central1-a/backendServices/example-group",
			want: false,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			if got := EqualResourcePaths(tc.a, tc.b); got != tc.want {
				t.Errorf("EqualResourcePathsOfURLs(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

func TestEqualResourceIDs(t *testing.T) {
	t.Parallel()
	testCases := map[string]struct {
		a    string
		b    string
		want bool
	}{
		"partial vs full": {
			a:    "https://www.googleapis.com/compute/beta/projects/project-id/zones/us-central1-a/instanceGroups/example-group",
			b:    "projects/project-id/zones/us-central1-a/instanceGroups/example-group",
			want: true,
		},
		"full vs full": {
			a:    "https://www.googleapis.com/compute/beta/projects/project-id/zones/us-central1-a/instanceGroups/example-group",
			b:    "https://www.googleapis.com/compute/beta/projects/project-id/zones/us-central1-a/instanceGroups/example-group",
			want: true,
		},
		"diff versions": {
			a:    "https://www.googleapis.com/compute/v1/projects/project-A/zones/us-central1-a/instanceGroups/example-group",
			b:    "https://www.googleapis.com/compute/beta/projects/project-A/zones/us-central1-a/instanceGroups/example-group",
			want: true,
		},
		"diff name": {
			a:    "https://www.googleapis.com/compute/v1/projects/project-A/zones/us-central1-a/instanceGroups/example-groupA",
			b:    "https://www.googleapis.com/compute/beta/projects/project-B/zones/us-central1-a/instanceGroups/example-groupB",
			want: false,
		},
		"diff location": {
			a:    "https://www.googleapis.com/compute/v1/projects/project-A/zones/us-central1-a/instanceGroups/example-group",
			b:    "https://www.googleapis.com/compute/beta/projects/project-B/zones/us-central1-b/instanceGroups/example-group",
			want: false,
		},
		"diff resource": {
			a:    "https://www.googleapis.com/compute/v1/projects/project-A/zones/us-central1-a/backendServices/example-group",
			b:    "https://www.googleapis.com/compute/beta/projects/project-B/zones/us-central1-b/instanceGroups/example-group",
			want: false,
		},
		"bad input a": {
			a:    "/project-A/zones/us-central1-a/backendServices/example-group",
			b:    "https://www.googleapis.com/compute/beta/projects/project-B/zones/us-central1-b/instanceGroups/example-group",
			want: false,
		},
		"bad input b": {
			a:    "https://www.googleapis.com/compute/beta/projects/project-B/zones/us-central1-b/instanceGroups/example-group",
			b:    "/project-A/zones/us-central1-a/backendServices/example-group",
			want: false,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			if got := EqualResourceIDs(tc.a, tc.b); got != tc.want {
				t.Errorf("EqualResourceIDs(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

func TestTraverseIngressBackends(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		desc           string
		ing            *extensions.Ingress
		expectBackends []extensions.IngressBackend
	}{
		{
			"empty spec",
			&extensions.Ingress{},
			[]extensions.IngressBackend{},
		},
		{
			"one default backend",
			&extensions.Ingress{
				Spec: extensions.IngressSpec{
					Backend: &extensions.IngressBackend{
						ServiceName: "dummy-service",
						ServicePort: intstr.FromInt(80),
					},
					Rules: []extensions.IngressRule{},
				},
			},
			[]extensions.IngressBackend{
				{
					ServiceName: "dummy-service",
					ServicePort: intstr.FromInt(80),
				},
			},
		},
		{
			"one backend in path",
			&extensions.Ingress{
				Spec: extensions.IngressSpec{
					Rules: []extensions.IngressRule{
						{
							Host: "foo.bar",
							IngressRuleValue: extensions.IngressRuleValue{
								HTTP: &extensions.HTTPIngressRuleValue{
									Paths: []extensions.HTTPIngressPath{
										{
											Path: "/foo",
											Backend: extensions.IngressBackend{
												ServiceName: "foo-service",
												ServicePort: intstr.FromInt(80),
											},
										},
									},
								},
							},
						},
					},
				},
			},
			[]extensions.IngressBackend{
				{
					ServiceName: "foo-service",
					ServicePort: intstr.FromInt(80),
				},
			},
		},
		{
			"one rule with only host",
			&extensions.Ingress{
				Spec: extensions.IngressSpec{
					Rules: []extensions.IngressRule{
						{
							Host: "foo.bar",
						},
					},
				},
			},
			[]extensions.IngressBackend{},
		},
		{
			"complex ingress spec",
			&extensions.Ingress{
				Spec: extensions.IngressSpec{
					Backend: &extensions.IngressBackend{
						ServiceName: "backend-service",
						ServicePort: intstr.FromInt(81),
					},
					Rules: []extensions.IngressRule{
						{
							Host: "foo.bar",
							IngressRuleValue: extensions.IngressRuleValue{
								HTTP: &extensions.HTTPIngressRuleValue{
									Paths: []extensions.HTTPIngressPath{
										{
											Path: "/foo",
											Backend: extensions.IngressBackend{
												ServiceName: "foo-service",
												ServicePort: intstr.FromInt(82),
											},
										},
										{
											Path: "/bar",
											Backend: extensions.IngressBackend{
												ServiceName: "bar-service",
												ServicePort: intstr.FromInt(83),
											},
										},
									},
								},
							},
						},
						{
							IngressRuleValue: extensions.IngressRuleValue{
								HTTP: &extensions.HTTPIngressRuleValue{
									Paths: []extensions.HTTPIngressPath{
										{
											Path: "/a",
											Backend: extensions.IngressBackend{
												ServiceName: "a-service",
												ServicePort: intstr.FromInt(84),
											},
										},
										{
											Path: "/b",
											Backend: extensions.IngressBackend{
												ServiceName: "b-service",
												ServicePort: intstr.FromInt(85),
											},
										},
									},
								},
							},
						},
						{
							Host: "a.b.c",
						},
						{
							Host: "e.f.g",
						},
					},
				},
			},
			[]extensions.IngressBackend{
				{
					ServiceName: "backend-service",
					ServicePort: intstr.FromInt(81),
				},
				{
					ServiceName: "foo-service",
					ServicePort: intstr.FromInt(82),
				},
				{
					ServiceName: "bar-service",
					ServicePort: intstr.FromInt(83),
				},
				{
					ServiceName: "a-service",
					ServicePort: intstr.FromInt(84),
				},
				{
					ServiceName: "b-service",
					ServicePort: intstr.FromInt(85),
				},
			},
		},
	}

	for _, tc := range testCases {
		counter := 0
		TraverseIngressBackends(tc.ing, func(id ServicePortID) bool {
			if tc.expectBackends[counter].ServiceName != id.Service.Name || tc.expectBackends[counter].ServicePort != id.Port {
				t.Errorf("Test case %q, for backend %v, expecting service name %q and service port %q, but got %q, %q", tc.desc, counter, tc.expectBackends[counter].ServiceName, tc.expectBackends[counter].ServicePort.String(), id.Service.Name, id.Port.String())
			}
			counter += 1
			return false
		})
	}
}

func getTestIngress() {
	return
}
