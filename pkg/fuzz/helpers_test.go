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

package fuzz

import (
	"reflect"
	"testing"

	"github.com/kr/pretty"
	"k8s.io/api/core/v1"
	"k8s.io/api/networking/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func TestNewService(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, ns string
		port     int
		want     v1.Service
	}{
		{
			name: "svc1",
			ns:   "ns1",
			port: 80,
			want: v1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: "svc1", Namespace: "ns1"},
				Spec: v1.ServiceSpec{
					Ports: []v1.ServicePort{
						{Port: 80, TargetPort: intstr.FromInt(80)},
					},
				},
			},
		},
	} {
		got := NewService(tc.name, tc.ns, tc.port)
		if !reflect.DeepEqual(*got, tc.want) {
			t.Errorf("testcase = %+v, got\n%s\nwant\n%s", tc, pretty.Sprint(got), pretty.Sprint(tc.want))
		}
	}
}

func TestServiceMapFromIngress(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		desc string
		ing  *v1beta1.Ingress
		want ServiceMap
	}{
		{
			desc: "one path",
			ing: NewIngressBuilder("n1", "ing1", "").
				AddPath("test1.com", "/foo", "s", intstr.FromInt(80)).
				Build(),
			want: ServiceMap{
				HostPath{"test1.com", "/foo"}: &v1beta1.IngressBackend{
					ServiceName: "s", ServicePort: intstr.IntOrString{IntVal: 80},
				},
			},
		},
		{
			desc: "multiple paths",
			ing: NewIngressBuilder("n1", "ing1", "").
				AddPath("test1.com", "/foo", "s", intstr.FromInt(80)).
				AddPath("test1.com", "/bar", "s", intstr.FromInt(80)).
				Build(),
			want: ServiceMap{
				HostPath{"test1.com", "/foo"}: &v1beta1.IngressBackend{
					ServiceName: "s", ServicePort: intstr.IntOrString{IntVal: 80},
				},
				HostPath{"test1.com", "/bar"}: &v1beta1.IngressBackend{
					ServiceName: "s", ServicePort: intstr.IntOrString{IntVal: 80},
				},
			},
		},
		{
			desc: "default backend",
			ing: NewIngressBuilder("n1", "ing1", "").
				DefaultBackend("s", intstr.FromInt(80)).
				Build(),
			want: ServiceMap{
				HostPath{}: &v1beta1.IngressBackend{
					ServiceName: "s", ServicePort: intstr.IntOrString{IntVal: 80},
				},
			},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			got := ServiceMapFromIngress(tc.ing)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("reflect.DeepEqual(got, tc.want) = false, want true\ngot=%s\ntc.want=%s", pretty.Sprint(got), pretty.Sprint(tc.want))
			}
		})
	}
}

func TestIngressBuilder(t *testing.T) {
	t.Parallel()

	const (
		name = "ing1"
		ns   = "ns1"
	)

	om := metav1.ObjectMeta{Name: name, Namespace: ns}

	for _, tc := range []struct {
		desc string
		want *v1beta1.Ingress
		got  *v1beta1.Ingress
	}{
		{
			desc: "empty",
			want: &v1beta1.Ingress{
				ObjectMeta: om,
			},
			got: NewIngressBuilder(ns, name, "").Build(),
		},
		{
			desc: "one path",
			want: &v1beta1.Ingress{
				ObjectMeta: om,
				Spec: v1beta1.IngressSpec{
					Rules: []v1beta1.IngressRule{
						{
							Host: "test.com",
							IngressRuleValue: v1beta1.IngressRuleValue{
								HTTP: &v1beta1.HTTPIngressRuleValue{
									Paths: []v1beta1.HTTPIngressPath{
										{
											Path: "/",
											Backend: v1beta1.IngressBackend{
												ServiceName: "svc1",
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
			got: NewIngressBuilder(ns, name, "").
				AddPath("test.com", "/", "svc1", intstr.FromInt(80)).
				Build(),
		},
		{
			desc: "with VIP",
			want: &v1beta1.Ingress{
				ObjectMeta: om,
				Status: v1beta1.IngressStatus{
					LoadBalancer: v1.LoadBalancerStatus{
						Ingress: []v1.LoadBalancerIngress{{IP: "127.0.0.1"}},
					},
				},
			},
			got: NewIngressBuilder(ns, name, "127.0.0.1").Build(),
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			if !reflect.DeepEqual(tc.got, tc.want) {
				t.Errorf("got\n%s\nwant\n%s", pretty.Sprint(tc.got), pretty.Sprint(tc.want))
			}
		})
	}
}
