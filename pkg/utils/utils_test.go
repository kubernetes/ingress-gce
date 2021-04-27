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
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/api/googleapi"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/utils/common"

	api_v1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/legacy-cloud-providers/gce"
)

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
		ing            *networkingv1.Ingress
		expectBackends []networkingv1.IngressBackend
	}{
		{
			"empty spec",
			&networkingv1.Ingress{},
			[]networkingv1.IngressBackend{},
		},
		{
			"one default backend",
			&networkingv1.Ingress{
				Spec: networkingv1.IngressSpec{
					DefaultBackend: &networkingv1.IngressBackend{
						Service: &networkingv1.IngressServiceBackend{
							Name: "dummy-service",
							Port: networkingv1.ServiceBackendPort{
								Number: 80,
							},
						},
					},
					Rules: []networkingv1.IngressRule{},
				},
			},
			[]networkingv1.IngressBackend{
				{
					Service: &networkingv1.IngressServiceBackend{
						Name: "dummy-service",
						Port: networkingv1.ServiceBackendPort{
							Number: 80,
						},
					},
				},
			},
		},
		{
			"one backend in path",
			&networkingv1.Ingress{
				Spec: networkingv1.IngressSpec{
					Rules: []networkingv1.IngressRule{
						{
							Host: "foo.bar",
							IngressRuleValue: networkingv1.IngressRuleValue{
								HTTP: &networkingv1.HTTPIngressRuleValue{
									Paths: []networkingv1.HTTPIngressPath{
										{
											Path: "/foo",
											Backend: networkingv1.IngressBackend{
												Service: &networkingv1.IngressServiceBackend{
													Name: "foo-service",
													Port: networkingv1.ServiceBackendPort{
														Number: 80,
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			[]networkingv1.IngressBackend{
				{
					Service: &networkingv1.IngressServiceBackend{
						Name: "foo-service",
						Port: networkingv1.ServiceBackendPort{
							Number: 80,
						},
					},
				},
			},
		},
		{
			"one rule with only host",
			&networkingv1.Ingress{
				Spec: networkingv1.IngressSpec{
					Rules: []networkingv1.IngressRule{
						{
							Host: "foo.bar",
						},
					},
				},
			},
			[]networkingv1.IngressBackend{},
		},
		{
			"complex ingress spec",
			&networkingv1.Ingress{
				Spec: networkingv1.IngressSpec{
					DefaultBackend: &networkingv1.IngressBackend{
						Service: &networkingv1.IngressServiceBackend{
							Name: "backend-service",
							Port: networkingv1.ServiceBackendPort{
								Number: 81,
							},
						},
					},
					Rules: []networkingv1.IngressRule{
						{
							Host: "foo.bar",
							IngressRuleValue: networkingv1.IngressRuleValue{
								HTTP: &networkingv1.HTTPIngressRuleValue{
									Paths: []networkingv1.HTTPIngressPath{
										{
											Path: "/foo",
											Backend: networkingv1.IngressBackend{
												Service: &networkingv1.IngressServiceBackend{
													Name: "foo-service",
													Port: networkingv1.ServiceBackendPort{
														Number: 82,
													},
												},
											},
										},
										{
											Path: "/bar",
											Backend: networkingv1.IngressBackend{
												Service: &networkingv1.IngressServiceBackend{
													Name: "bar-service",
													Port: networkingv1.ServiceBackendPort{
														Number: 83,
													},
												},
											},
										},
									},
								},
							},
						},
						{
							IngressRuleValue: networkingv1.IngressRuleValue{
								HTTP: &networkingv1.HTTPIngressRuleValue{
									Paths: []networkingv1.HTTPIngressPath{
										{
											Path: "/a",
											Backend: networkingv1.IngressBackend{
												Service: &networkingv1.IngressServiceBackend{
													Name: "a-service",
													Port: networkingv1.ServiceBackendPort{
														Number: 84,
													},
												},
											},
										},
										{
											Path: "/b",
											Backend: networkingv1.IngressBackend{
												Service: &networkingv1.IngressServiceBackend{
													Name: "b-service",
													Port: networkingv1.ServiceBackendPort{
														Number: 85,
													},
												},
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
			[]networkingv1.IngressBackend{
				{
					Service: &networkingv1.IngressServiceBackend{
						Name: "backend-service",
						Port: networkingv1.ServiceBackendPort{
							Number: 81,
						},
					},
				},
				{
					Service: &networkingv1.IngressServiceBackend{
						Name: "foo-service",
						Port: networkingv1.ServiceBackendPort{
							Number: 82,
						},
					},
				},
				{
					Service: &networkingv1.IngressServiceBackend{
						Name: "bar-service",
						Port: networkingv1.ServiceBackendPort{
							Number: 83,
						},
					},
				},
				{
					Service: &networkingv1.IngressServiceBackend{
						Name: "a-service",
						Port: networkingv1.ServiceBackendPort{
							Number: 84,
						},
					},
				},
				{
					Service: &networkingv1.IngressServiceBackend{
						Name: "b-service",
						Port: networkingv1.ServiceBackendPort{
							Number: 85,
						},
					},
				},
			},
		},
		{
			"non service backend",
			&networkingv1.Ingress{
				Spec: networkingv1.IngressSpec{
					Rules: []networkingv1.IngressRule{
						{
							Host: "foo.bar",
							IngressRuleValue: networkingv1.IngressRuleValue{
								HTTP: &networkingv1.HTTPIngressRuleValue{
									Paths: []networkingv1.HTTPIngressPath{
										{
											Path: "/foo",
											Backend: networkingv1.IngressBackend{
												Service: &networkingv1.IngressServiceBackend{
													Name: "foo-service",
													Port: networkingv1.ServiceBackendPort{
														Number: 80,
													},
												},
											},
										},
										{
											Path:    "/non-service",
											Backend: networkingv1.IngressBackend{},
										},
									},
								},
							},
						},
					},
				},
			},
			[]networkingv1.IngressBackend{
				{
					Service: &networkingv1.IngressServiceBackend{
						Name: "foo-service",
						Port: networkingv1.ServiceBackendPort{
							Number: 80,
						},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		counter := 0
		TraverseIngressBackends(tc.ing, func(id ServicePortID) bool {
			if tc.expectBackends[counter].Service.Name != id.Service.Name || tc.expectBackends[counter].Service.Port != id.Port {
				t.Errorf("Test case %q, for backend %v, expecting service name %q and service port %+v, but got %q, %q", tc.desc, counter, tc.expectBackends[counter].Service.Name, tc.expectBackends[counter].Service.Port, id.Service.Name, id.Port.String())
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
	var wrongClassName = "wrong-class"
	testCases := []struct {
		desc             string
		ingress          *networkingv1.Ingress
		ingressClassFlag string
		expected         bool
	}{
		{
			desc: "No ingress class",
			ingress: &networkingv1.Ingress{
				ObjectMeta: v1.ObjectMeta{
					Annotations: map[string]string{},
				},
			},
			expected: true,
		},
		{
			desc: "unknown ingress class",
			ingress: &networkingv1.Ingress{
				ObjectMeta: v1.ObjectMeta{
					Annotations: map[string]string{
						annotations.IngressClassKey: "foo"},
				},
			},
			expected: false,
		},
		{
			desc: "L7 ILB ingress class",
			ingress: &networkingv1.Ingress{
				ObjectMeta: v1.ObjectMeta{
					Annotations: map[string]string{
						annotations.IngressClassKey: annotations.GceL7ILBIngressClass},
				},
			},
			expected: true,
		},
		{
			desc: "Set by flag with non-matching class",
			ingress: &networkingv1.Ingress{
				ObjectMeta: v1.ObjectMeta{
					Annotations: map[string]string{
						annotations.IngressClassKey: wrongClassName},
				},
			},
			ingressClassFlag: "right-class",
			expected:         false,
		},
		{
			desc: "Set by flag with matching class",
			ingress: &networkingv1.Ingress{
				ObjectMeta: v1.ObjectMeta{
					Annotations: map[string]string{
						annotations.IngressClassKey: "right-class"},
				},
			},
			ingressClassFlag: "right-class",
			expected:         true,
		},
		{
			desc: "No ingress class annotation, ingressClassName set",
			ingress: &networkingv1.Ingress{
				ObjectMeta: v1.ObjectMeta{
					Annotations: map[string]string{},
				},
				Spec: networkingv1.IngressSpec{
					IngressClassName: &wrongClassName,
				},
			},
			expected: false,
		},
		{
			// Annotation supercedes spec.ingressClassName
			desc: "Set by flag with matching class, and ingressClassName set",
			ingress: &networkingv1.Ingress{
				ObjectMeta: v1.ObjectMeta{
					Annotations: map[string]string{
						annotations.IngressClassKey: "right-class"},
				},
				Spec: networkingv1.IngressSpec{
					IngressClassName: &wrongClassName,
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
		ingress  *networkingv1.Ingress
		expected bool
	}{
		{
			desc: "No ingress class",
			ingress: &networkingv1.Ingress{
				ObjectMeta: v1.ObjectMeta{
					Annotations: map[string]string{},
				},
			},
			expected: false,
		},
		{
			desc:     "Empty Annotations",
			ingress:  &networkingv1.Ingress{},
			expected: false,
		},
		{
			desc: "L7 ILB ingress class",
			ingress: &networkingv1.Ingress{
				ObjectMeta: v1.ObjectMeta{
					Annotations: map[string]string{
						annotations.IngressClassKey: annotations.GceL7ILBIngressClass},
				},
			},
			expected: true,
		},
		{
			desc: "foo ingress class",
			ingress: &networkingv1.Ingress{
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

func TestNeedsCleanup(t *testing.T) {
	testCases := []struct {
		isGLBCIngress       bool
		withFinalizer       bool
		withDeleteTimestamp bool
		expectNeedsCleanup  bool
	}{
		{false, false, false, true},
		{false, false, true, true},
		{false, true, false, true},
		{false, true, true, true},
		{true, false, false, false},
		{true, false, true, false},
		{true, true, false, false},
		{true, true, true, true},
	}

	for _, tc := range testCases {
		desc := fmt.Sprintf("isGLBCIngress %t withFinalizer %t withDeleteTimestamp %t", tc.isGLBCIngress, tc.withFinalizer, tc.withDeleteTimestamp)
		t.Run(desc, func(t *testing.T) {
			ingressClass := "gce"
			if !tc.isGLBCIngress {
				ingressClass = "nginx"
			}
			ingress := &networkingv1.Ingress{
				ObjectMeta: v1.ObjectMeta{
					Name:      "ing",
					Namespace: "default",
					Annotations: map[string]string{
						"kubernetes.io/ingress.class": ingressClass,
					},
				},
				Spec: networkingv1.IngressSpec{
					DefaultBackend: &networkingv1.IngressBackend{
						Service: &networkingv1.IngressServiceBackend{
							Name: "my-service",
							Port: networkingv1.ServiceBackendPort{
								Number: 80,
							},
						},
					},
				},
			}

			if tc.withFinalizer {
				ingress.ObjectMeta.Finalizers = []string{common.FinalizerKey}
			}

			if tc.withDeleteTimestamp {
				ts := v1.NewTime(time.Now())
				ingress.SetDeletionTimestamp(&ts)
			}

			if gotNeedsCleanup := NeedsCleanup(ingress); gotNeedsCleanup != tc.expectNeedsCleanup {
				t.Errorf("NeedsCleanup() = %t, want %t (tc = %+v)", gotNeedsCleanup, tc.expectNeedsCleanup, tc)
			}
		})
	}
}

func TestHasVIP(t *testing.T) {
	for _, tc := range []struct {
		desc         string
		ing          *networkingv1.Ingress
		expectHasVIP bool
	}{
		{"nil", nil, false},
		{"empty ingress status", &networkingv1.Ingress{
			Status: networkingv1.IngressStatus{},
		},
			false,
		},
		{"empty load-balancer status", &networkingv1.Ingress{
			Status: networkingv1.IngressStatus{
				LoadBalancer: api_v1.LoadBalancerStatus{},
			},
		},
			false,
		},
		{"empty load-balancer ingress", &networkingv1.Ingress{
			Status: networkingv1.IngressStatus{
				LoadBalancer: api_v1.LoadBalancerStatus{
					Ingress: []api_v1.LoadBalancerIngress{},
				},
			},
		},
			false,
		},
		{"empty IP", &networkingv1.Ingress{
			Status: networkingv1.IngressStatus{
				LoadBalancer: api_v1.LoadBalancerStatus{
					Ingress: []api_v1.LoadBalancerIngress{
						{IP: ""},
					},
				},
			},
		},
			false,
		},
		{"valid IP", &networkingv1.Ingress{
			Status: networkingv1.IngressStatus{
				LoadBalancer: api_v1.LoadBalancerStatus{
					Ingress: []api_v1.LoadBalancerIngress{
						{IP: "0.0.0.0"},
					},
				},
			},
		},
			true,
		},
		{"random", &networkingv1.Ingress{
			Status: networkingv1.IngressStatus{
				LoadBalancer: api_v1.LoadBalancerStatus{
					Ingress: []api_v1.LoadBalancerIngress{
						{IP: "xxxxxx"},
					},
				},
			},
		},
			true,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			if gotHasVIP := HasVIP(tc.ing); tc.expectHasVIP != gotHasVIP {
				t.Errorf("Got diff HasVIP, expected %t got %t", tc.expectHasVIP, gotHasVIP)
			}
		})
	}
}

func TestGetNodePrimaryIP(t *testing.T) {
	t.Parallel()
	internalIP := "1.2.3.4"
	node := &api_v1.Node{
		Status: api_v1.NodeStatus{
			Addresses: []api_v1.NodeAddress{
				{
					Type:    api_v1.NodeInternalIP,
					Address: internalIP,
				},
			},
		},
	}
	out := GetNodePrimaryIP(node)
	if out != internalIP {
		t.Errorf("Expected Primary IP %s, got %s", internalIP, out)
	}

	node = &api_v1.Node{
		Status: api_v1.NodeStatus{
			Addresses: []api_v1.NodeAddress{
				{
					Type:    api_v1.NodeExternalIP,
					Address: "11.12.13.14",
				},
			},
		},
	}
	out = GetNodePrimaryIP(node)
	if out != "" {
		t.Errorf("Expected Primary IP '', got %s", out)
	}
}

func TestIsLegacyL4ILBService(t *testing.T) {
	t.Parallel()
	svc := &api_v1.Service{
		ObjectMeta: v1.ObjectMeta{
			Name:        "testsvc",
			Namespace:   "default",
			Annotations: map[string]string{gce.ServiceAnnotationLoadBalancerType: string(gce.LBTypeInternal)},
			Finalizers:  []string{common.LegacyILBFinalizer},
		},
		Spec: api_v1.ServiceSpec{
			Type: api_v1.ServiceTypeLoadBalancer,
			Ports: []api_v1.ServicePort{
				{Name: "testport", Port: int32(80)},
			},
		},
	}
	if !IsLegacyL4ILBService(svc) {
		t.Errorf("Expected True for Legacy service %s, got False", svc.Name)
	}

	// Remove the finalizer and ensure the check returns False.
	svc.ObjectMeta.Finalizers = nil
	if IsLegacyL4ILBService(svc) {
		t.Errorf("Expected False for Legacy service %s, got True", svc.Name)
	}
}

func TestGetPortRanges(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		Desc   string
		Input  []int
		Result []string
	}{
		{Desc: "All Unique", Input: []int{8, 66, 23, 13, 89}, Result: []string{"8", "13", "23", "66", "89"}},
		{Desc: "All Unique Sorted", Input: []int{1, 7, 9, 16, 26}, Result: []string{"1", "7", "9", "16", "26"}},
		{Desc: "Ranges", Input: []int{56, 78, 67, 79, 21, 80, 12}, Result: []string{"12", "21", "56", "67", "78-80"}},
		{Desc: "Ranges Sorted", Input: []int{5, 7, 90, 1002, 1003, 1004, 1005, 2501}, Result: []string{"5", "7", "90", "1002-1005", "2501"}},
		{Desc: "Ranges Duplicates", Input: []int{15, 37, 900, 2002, 2003, 2003, 2004, 2004}, Result: []string{"15", "37", "900", "2002-2004"}},
		{Desc: "Duplicates", Input: []int{10, 10, 10, 10, 10}, Result: []string{"10"}},
		{Desc: "Only ranges", Input: []int{18, 19, 20, 21, 22, 55, 56, 77, 78, 79, 3504, 3505, 3506}, Result: []string{"18-22", "55-56", "77-79", "3504-3506"}},
		{Desc: "Single Range", Input: []int{6000, 6001, 6002, 6003, 6004, 6005}, Result: []string{"6000-6005"}},
		{Desc: "One value", Input: []int{12}, Result: []string{"12"}},
		{Desc: "Empty", Input: []int{}, Result: nil},
	} {
		result := GetPortRanges(tc.Input)
		if diff := cmp.Diff(result, tc.Result); diff != "" {
			t.Errorf("GetPortRanges(%s) mismatch, (-want +got): \n%s", tc.Desc, diff)
		}
	}
}

func TestIsHTTPErrorCode(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		err  error
		code int
		want bool
	}{
		{nil, 400, false},
		{errors.New("xxx"), 400, false},
		{&googleapi.Error{Code: 200}, 400, false},
		{&googleapi.Error{Code: 400}, 400, true},
	} {
		got := IsHTTPErrorCode(tc.err, tc.code)
		if got != tc.want {
			t.Errorf("IsHTTPErrorCode(%v, %d) = %t; want %t", tc.err, tc.code, got, tc.want)
		}
	}
}

func TestBackendToServicePortID(t *testing.T) {
	testNS := "test-namespace"
	for _, tc := range []struct {
		desc      string
		backend   networkingv1.IngressBackend
		expectErr bool
	}{
		{
			desc: "service is populated",
			backend: networkingv1.IngressBackend{
				Service: &networkingv1.IngressServiceBackend{
					Name: "my-svc",
					Port: networkingv1.ServiceBackendPort{
						Number: 80,
					},
				},
			},
		},
		{
			desc:      "service is nil",
			backend:   networkingv1.IngressBackend{},
			expectErr: true,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {

			svcPortID, err := BackendToServicePortID(tc.backend, testNS)
			if !tc.expectErr && err != nil {
				t.Errorf("unexpected error: %q", err)
			} else if tc.expectErr && err == nil {
				t.Errorf("expected an error, but got none")
			}

			expectedID := ServicePortID{}
			if !tc.expectErr {
				expectedID = ServicePortID{
					Service: types.NamespacedName{
						Name:      tc.backend.Service.Name,
						Namespace: testNS,
					},
					Port: tc.backend.Service.Port,
				}
			}

			if !reflect.DeepEqual(expectedID, svcPortID) {
				t.Errorf("expected svc port id to be %+v, but got %+v", expectedID, svcPortID)
			}
		})
	}
}
