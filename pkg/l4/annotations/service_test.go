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

package annotations

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/ingress-gce/pkg/negannotation"
)

func TestOnlyStatusAnnotationsChanged(t *testing.T) {
	for _, tc := range []struct {
		desc           string
		service1       *v1.Service
		service2       *v1.Service
		expectedResult bool
	}{
		{
			desc: "Test add neg annotation",
			service1: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name: "service1",
				},
			},
			service2: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name: "service2",
					Annotations: map[string]string{
						negannotation.NEGStatusKey: `{"network_endpoint_groups":{"80":"neg-name"},"zones":["us-central1-a"]}`,
					},
				},
			},
			expectedResult: true,
		},
		{
			desc: "Test valid diff",
			service1: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name: "service1",
					Annotations: map[string]string{
						negannotation.NEGStatusKey: `{"network_endpoint_groups":{"80":"neg-name"},"zones":["us-central1-a"]}`,
					},
				},
			},
			service2: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name: "service2",
					Annotations: map[string]string{
						"RandomAnnotation": "abcde",
					},
				},
			},
			expectedResult: false,
		},
		{
			desc: "Test no change",
			service1: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name: "service1",
					Annotations: map[string]string{
						"RandomAnnotation": "abcde",
					},
				},
			},
			service2: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name: "service2",
					Annotations: map[string]string{
						"RandomAnnotation": "abcde",
					},
				},
			},
			expectedResult: true,
		},
		{
			desc: "Test remove NEG annotation",
			service1: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name: "service1",
					Annotations: map[string]string{
						negannotation.NEGStatusKey: `{"network_endpoint_groups":{"80":"neg-name"},"zones":["us-central1-a"]}`,
						"RandomAnnotation":         "abcde",
					},
				},
			},
			service2: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name: "service2",
					Annotations: map[string]string{
						"RandomAnnotation": "abcde",
					},
				},
			},
			expectedResult: true,
		},
		{
			desc: "Test only ILB ForwardingRule annotation diff",
			service1: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name: "service1",
					Annotations: map[string]string{
						FirewallRuleKey:            "rule1",
						TCPForwardingRuleKey:       "tcprule",
						negannotation.NEGStatusKey: `{"network_endpoint_groups":{"80":"neg-name"},"zones":["us-central1-a"]}`,
						"RandomAnnotation":         "abcde",
					},
				},
			},
			service2: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name: "service2",
					Annotations: map[string]string{
						FirewallRuleKey:            "rule1",
						UDPForwardingRuleKey:       "udprule",
						negannotation.NEGStatusKey: `{"network_endpoint_groups":{"80":"neg-name"},"zones":["us-central1-a"]}`,
						"RandomAnnotation":         "abcde",
					},
				},
			},
			expectedResult: true,
		},
		{
			desc: "Test all status annotations removed",
			service1: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name: "service1",
					Annotations: map[string]string{
						FirewallRuleKey:            "rule1",
						TCPForwardingRuleKey:       "tcprule",
						negannotation.NEGStatusKey: `{"network_endpoint_groups":{"80":"neg-name"},"zones":["us-central1-a"]}`,
						"RandomAnnotation":         "abcde",
					},
				},
			},
			service2: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name: "service2",
					Annotations: map[string]string{
						"RandomAnnotation": "abcde",
					},
				},
			},
			expectedResult: true,
		},
		{
			desc: "Test change value of non-status annotation",
			service1: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name: "service1",
					Annotations: map[string]string{
						FirewallRuleKey:            "rule1",
						TCPForwardingRuleKey:       "tcprule",
						negannotation.NEGStatusKey: `{"network_endpoint_groups":{"80":"neg-name"},"zones":["us-central1-a"]}`,
						"RandomAnnotation":         "abcde",
					},
				},
			},
			service2: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name: "service2",
					Annotations: map[string]string{
						FirewallRuleKey:            "rule1",
						TCPForwardingRuleKey:       "tcprule",
						negannotation.NEGStatusKey: `{"network_endpoint_groups":{"80":"neg-name"},"zones":["us-central1-a"]}`,
						"RandomAnnotation":         "xyz",
					},
				},
			},
			expectedResult: false,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			result := OnlyStatusAnnotationsChanged(tc.service1, tc.service2)
			if result != tc.expectedResult {
				t.Errorf("%s: Expected result for input %v, %v to be %v, got %v instead", tc.desc, tc.service1.Annotations, tc.service2.Annotations, tc.expectedResult, result)
			}
		})
	}
}

func TestRBSAnnotation(t *testing.T) {
	for _, tc := range []struct {
		desc string
		svc  *v1.Service
		want bool
	}{
		{
			desc: "RBS annotation not specified",
			svc:  &v1.Service{},
			want: false,
		},
		{
			desc: "RBS annotation enabled",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						RBSAnnotationKey: RBSEnabled,
					},
				},
			},
			want: true,
		},
		{
			desc: "RBS annotation present but not with enabled value",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						RBSAnnotationKey: "otherValue",
					},
				},
			},
			want: false,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			got := HasRBSAnnotation(tc.svc)
			if tc.want != got {
				t.Errorf("output of HasRBSAnnotaiton differed, want=%v, got=%v", tc.want, got)
			}
		})
	}
}

func TestHasStrongSessionAffinityAnnotation(t *testing.T) {
	for _, tc := range []struct {
		desc string
		svc  *v1.Service
		want bool
	}{
		{
			desc: "Strong Session Affinity annotation was not specified",
			svc:  &v1.Service{},
			want: false,
		},
		{
			desc: "Strong Session Affinity annotation was correctly specified",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						StrongSessionAffinityAnnotationKey: StrongSessionAffinityEnabled,
					},
				},
			},
			want: true,
		},
		{
			desc: "Strong Session Affinity annotation has wrong value",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						StrongSessionAffinityAnnotationKey: "otherValue",
					},
				},
			},
			want: false,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			got := HasStrongSessionAffinityAnnotation(tc.svc)
			if tc.want != got {
				t.Errorf("output of HasStrongSessionAffinityAnnotation differed, want=%v, got=%v", tc.want, got)
			}
		})
	}
}

func TestWantsL4NetLB(t *testing.T) {
	// sPtr is a helper to return a pointer to a string,
	// useful for setting LoadBalancerClass.
	sPtr := func(s string) *string { return &s }

	for _, tc := range []struct {
		desc string
		svc  *v1.Service
		want bool
	}{
		{
			desc: "Nil service",
			svc:  nil,
			want: false,
		},
		{
			desc: "ClusterIP service should not want L4 NetLB",
			svc: &v1.Service{
				Spec: v1.ServiceSpec{
					Type: v1.ServiceTypeClusterIP,
				},
			},
			want: false,
		},
		{
			desc: "Standard LoadBalancer service defaults to External (NetLB)",
			svc: &v1.Service{
				Spec: v1.ServiceSpec{
					Type: v1.ServiceTypeLoadBalancer,
				},
			},
			want: true,
		},
		{
			desc: "LoadBalancer with Internal annotation should not want NetLB",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"cloud.google.com/load-balancer-type": string(LBTypeInternal),
					},
				},
				Spec: v1.ServiceSpec{
					Type: v1.ServiceTypeLoadBalancer,
				},
			},
			want: false,
		},
		{
			desc: "LoadBalancer with Regional Internal Class does not wants NetLB",
			svc: &v1.Service{
				Spec: v1.ServiceSpec{
					Type:              v1.ServiceTypeLoadBalancer,
					LoadBalancerClass: sPtr(RegionalInternalLoadBalancerClass),
				},
			},
			want: false,
		},
		{
			desc: "LoadBalancer with matching Regional External Class wants NetLB",
			svc: &v1.Service{
				Spec: v1.ServiceSpec{
					Type:              v1.ServiceTypeLoadBalancer,
					LoadBalancerClass: sPtr(RegionalExternalLoadBalancerClass),
				},
			},
			want: true,
		},
		{
			desc: "LoadBalancer with mismatching Class does not want NetLB",
			svc: &v1.Service{
				Spec: v1.ServiceSpec{
					Type:              v1.ServiceTypeLoadBalancer,
					LoadBalancerClass: sPtr("some-other-custom-class"),
				},
			},
			want: false,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			got, _ := WantsL4NetLB(tc.svc)
			if got != tc.want {
				t.Errorf("WantsL4NetLB() = %v, want %v", got, tc.want)
			}
		})
	}
}
