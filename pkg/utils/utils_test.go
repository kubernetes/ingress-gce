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
	"fmt"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/flags"
	"testing"
	"time"

	api_v1 "k8s.io/api/core/v1"
	"k8s.io/api/networking/v1beta1"
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
		res := TrimFieldsEvenly(tc.max, tc.fields...)
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
		ing            *v1beta1.Ingress
		expectBackends []v1beta1.IngressBackend
	}{
		{
			"empty spec",
			&v1beta1.Ingress{},
			[]v1beta1.IngressBackend{},
		},
		{
			"one default backend",
			&v1beta1.Ingress{
				Spec: v1beta1.IngressSpec{
					Backend: &v1beta1.IngressBackend{
						ServiceName: "dummy-service",
						ServicePort: intstr.FromInt(80),
					},
					Rules: []v1beta1.IngressRule{},
				},
			},
			[]v1beta1.IngressBackend{
				{
					ServiceName: "dummy-service",
					ServicePort: intstr.FromInt(80),
				},
			},
		},
		{
			"one backend in path",
			&v1beta1.Ingress{
				Spec: v1beta1.IngressSpec{
					Rules: []v1beta1.IngressRule{
						{
							Host: "foo.bar",
							IngressRuleValue: v1beta1.IngressRuleValue{
								HTTP: &v1beta1.HTTPIngressRuleValue{
									Paths: []v1beta1.HTTPIngressPath{
										{
											Path: "/foo",
											Backend: v1beta1.IngressBackend{
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
			[]v1beta1.IngressBackend{
				{
					ServiceName: "foo-service",
					ServicePort: intstr.FromInt(80),
				},
			},
		},
		{
			"one rule with only host",
			&v1beta1.Ingress{
				Spec: v1beta1.IngressSpec{
					Rules: []v1beta1.IngressRule{
						{
							Host: "foo.bar",
						},
					},
				},
			},
			[]v1beta1.IngressBackend{},
		},
		{
			"complex ingress spec",
			&v1beta1.Ingress{
				Spec: v1beta1.IngressSpec{
					Backend: &v1beta1.IngressBackend{
						ServiceName: "backend-service",
						ServicePort: intstr.FromInt(81),
					},
					Rules: []v1beta1.IngressRule{
						{
							Host: "foo.bar",
							IngressRuleValue: v1beta1.IngressRuleValue{
								HTTP: &v1beta1.HTTPIngressRuleValue{
									Paths: []v1beta1.HTTPIngressPath{
										{
											Path: "/foo",
											Backend: v1beta1.IngressBackend{
												ServiceName: "foo-service",
												ServicePort: intstr.FromInt(82),
											},
										},
										{
											Path: "/bar",
											Backend: v1beta1.IngressBackend{
												ServiceName: "bar-service",
												ServicePort: intstr.FromInt(83),
											},
										},
									},
								},
							},
						},
						{
							IngressRuleValue: v1beta1.IngressRuleValue{
								HTTP: &v1beta1.HTTPIngressRuleValue{
									Paths: []v1beta1.HTTPIngressPath{
										{
											Path: "/a",
											Backend: v1beta1.IngressBackend{
												ServiceName: "a-service",
												ServicePort: intstr.FromInt(84),
											},
										},
										{
											Path: "/b",
											Backend: v1beta1.IngressBackend{
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
			[]v1beta1.IngressBackend{
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

func TestGetNodeConditionPredicate(t *testing.T) {
	tests := []struct {
		node         api_v1.Node
		expectAccept bool
		name         string
	}{
		{
			node:         api_v1.Node{},
			expectAccept: false,
			name:         "empty",
		},
		{
			node: api_v1.Node{
				Status: api_v1.NodeStatus{
					Conditions: []api_v1.NodeCondition{
						{Type: api_v1.NodeReady, Status: api_v1.ConditionTrue},
					},
				},
			},
			expectAccept: true,
			name:         "basic",
		},
		{
			node: api_v1.Node{
				Spec: api_v1.NodeSpec{Unschedulable: true},
				Status: api_v1.NodeStatus{
					Conditions: []api_v1.NodeCondition{
						{Type: api_v1.NodeReady, Status: api_v1.ConditionTrue},
					},
				},
			},
			expectAccept: false,
			name:         "unschedulable",
		}, {
			node: api_v1.Node{
				Spec: api_v1.NodeSpec{
					Taints: []api_v1.Taint{
						api_v1.Taint{
							Key:    ToBeDeletedTaint,
							Value:  fmt.Sprint(time.Now().Unix()),
							Effect: api_v1.TaintEffectNoSchedule,
						},
					},
				},
				Status: api_v1.NodeStatus{
					Conditions: []api_v1.NodeCondition{
						{Type: api_v1.NodeReady, Status: api_v1.ConditionTrue},
					},
				},
			},
			expectAccept: false,
			name:         "ToBeDeletedByClusterAutoscaler-taint",
		},
	}
	pred := GetNodeConditionPredicate()
	for _, test := range tests {
		accept := pred(&test.node)
		if accept != test.expectAccept {
			t.Errorf("Test failed for %s, expected %v, saw %v", test.name, test.expectAccept, accept)
		}
	}
}

// Do not run in parallel since modifies global flags
// TODO(shance): remove l7-ilb flag tests once flag is removed
func TestIsGCEIngress(t *testing.T) {
	testCases := []struct {
		desc             string
		ingress          *v1beta1.Ingress
		ingressClassFlag string
		enableL7IlbFlag  bool
		expected         bool
	}{
		{
			desc: "No ingress class",
			ingress: &v1beta1.Ingress{
				ObjectMeta: v1.ObjectMeta{
					Annotations: map[string]string{},
				},
			},
			expected: true,
		},
		{
			desc: "unknown ingress class",
			ingress: &v1beta1.Ingress{
				ObjectMeta: v1.ObjectMeta{
					Annotations: map[string]string{
						annotations.IngressClassKey: "foo"},
				},
			},
			expected: false,
		},
		{
			desc: "L7 ILB ingress class with flag disabled",
			ingress: &v1beta1.Ingress{
				ObjectMeta: v1.ObjectMeta{
					Annotations: map[string]string{
						annotations.IngressClassKey: annotations.GceL7ILBIngressClass},
				},
			},
			expected: false,
		},
		{
			desc: "L7 ILB ingress class with flag enabled",
			ingress: &v1beta1.Ingress{
				ObjectMeta: v1.ObjectMeta{
					Annotations: map[string]string{
						annotations.IngressClassKey: annotations.GceL7ILBIngressClass},
				},
			},
			expected:        true,
			enableL7IlbFlag: true,
		},
		{
			desc: "Set by flag with non-matching class",
			ingress: &v1beta1.Ingress{
				ObjectMeta: v1.ObjectMeta{
					Annotations: map[string]string{
						annotations.IngressClassKey: "wrong-class"},
				},
			},
			ingressClassFlag: "right-class",
			expected:         false,
		},
		{
			desc: "Set by flag with matching class",
			ingress: &v1beta1.Ingress{
				ObjectMeta: v1.ObjectMeta{
					Annotations: map[string]string{
						annotations.IngressClassKey: "right-class"},
				},
			},
			ingressClassFlag: "right-class",
			expected:         true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			if tc.ingressClassFlag != "" {
				flags.F.IngressClass = tc.ingressClassFlag
			}

			if tc.enableL7IlbFlag {
				flags.F.EnableL7Ilb = true
			}

			result := IsGCEIngress(tc.ingress)
			if result != tc.expected {
				t.Fatalf("want %v, got %v", tc.expected, result)
			}
		})
	}
}

func TestIsGCEL7ILBIngress(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		desc     string
		ingress  *v1beta1.Ingress
		expected bool
	}{
		{
			desc: "No ingress class",
			ingress: &v1beta1.Ingress{
				ObjectMeta: v1.ObjectMeta{
					Annotations: map[string]string{},
				},
			},
			expected: false,
		},
		{
			desc:     "Empty Annotations",
			ingress:  &v1beta1.Ingress{},
			expected: false,
		},
		{
			desc: "L7 ILB ingress class",
			ingress: &v1beta1.Ingress{
				ObjectMeta: v1.ObjectMeta{
					Annotations: map[string]string{
						annotations.IngressClassKey: annotations.GceL7ILBIngressClass},
				},
			},
			expected: true,
		},
		{
			desc: "foo ingress class",
			ingress: &v1beta1.Ingress{
				ObjectMeta: v1.ObjectMeta{
					Annotations: map[string]string{
						annotations.IngressClassKey: "foo-class"},
				},
			},
			expected: false,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			result := IsGCEL7ILBIngress(tc.ingress)
			if result != tc.expected {
				t.Fatalf("want %v, got %v", tc.expected, result)
			}
		})
	}
}
