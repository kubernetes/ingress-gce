/*
Copyright 2023 The Kubernetes Authors.

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

package ingress

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/ingress-gce/cmd/check-gke-ingress/app/report"
	"k8s.io/ingress-gce/pkg/annotations"
	beconfigv1 "k8s.io/ingress-gce/pkg/apis/backendconfig/v1"
	feconfigv1beta1 "k8s.io/ingress-gce/pkg/apis/frontendconfig/v1beta1"
	fakebeconfig "k8s.io/ingress-gce/pkg/backendconfig/client/clientset/versioned/fake"
	fakefeconfig "k8s.io/ingress-gce/pkg/frontendconfig/client/clientset/versioned/fake"
)

func TestCheckServiceExistence(t *testing.T) {
	ns := "namespace1"
	client := fake.NewSimpleClientset()
	client.CoreV1().Services(ns).Create(context.TODO(), &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns,
			Name:      "service-1",
		},
	}, metav1.CreateOptions{})

	for _, tc := range []struct {
		desc      string
		namespace string
		name      string
		expect    string
	}{
		{
			desc:   "empty input",
			expect: report.Failed,
		},
		{
			desc:      "correct namespace and correct name",
			namespace: "namespace1",
			name:      "service-1",
			expect:    report.Passed,
		},
		{
			desc:      "correct namespace and wrong name",
			namespace: "namespace1",
			name:      "service-2",
			expect:    report.Failed,
		},
		{
			desc:      "wrong namespace and correct name",
			namespace: "namespace2",
			name:      "service-1",
			expect:    report.Failed,
		},
	} {
		_, res, _ := CheckServiceExistence(tc.namespace, tc.name, client)
		if res != tc.expect {
			t.Errorf("For test case %q, expect check result = %s, but got %s", tc.desc, tc.expect, res)
		}
	}
}

func TestCheckBackendConfigAnnotation(t *testing.T) {
	for _, tc := range []struct {
		desc   string
		svc    corev1.Service
		expect string
	}{
		{
			desc:   "empty input",
			expect: report.Skipped,
		},
		{
			desc: "service without beconfig annotation",
			svc: corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-1",
					Namespace: "test",
				},
			},
			expect: report.Skipped,
		},
		{
			desc: "service with beconfig annotation",
			svc: corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-1",
					Namespace: "test",
					Annotations: map[string]string{
						annotations.BackendConfigKey: `{"ports": {"port1": "beconfig"}}`,
					},
				},
			},
			expect: report.Passed,
		},
		{
			desc: "service with malformed default field in beconfig annotation",
			svc: corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-1",
					Namespace: "test",
					Annotations: map[string]string{
						annotations.BackendConfigKey: `{"default": {"port1": "beconfig"}}`,
					},
				},
			},
			expect: report.Failed,
		},
		{
			desc: "service with malformed ports field in beconfig annotation",
			svc: corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-1",
					Namespace: "test",
					Annotations: map[string]string{
						annotations.BackendConfigKey: `{"port1": "beconfig1", "port2": "beconfig2"}`,
					},
				},
			},
			expect: report.Failed,
		},
	} {
		_, res, _ := CheckBackendConfigAnnotation(&tc.svc)
		if res != tc.expect {
			t.Errorf("For test case %q, expect check result = %s, but got %s", tc.desc, tc.expect, res)
		}
	}
}

func TestCheckBackendConfigExistence(t *testing.T) {
	client := fakebeconfig.NewSimpleClientset()
	client.CloudV1().BackendConfigs("test").Create(context.TODO(), &beconfigv1.BackendConfig{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "test",
			Name:      "foo-beconfig",
		},
	}, metav1.CreateOptions{})

	for _, tc := range []struct {
		desc      string
		namespace string
		name      string
		expect    string
	}{
		{
			desc:   "empty input",
			expect: report.Failed,
		},
		{
			desc:      "correct namespace and correct name",
			namespace: "test",
			name:      "foo-beconfig",
			expect:    report.Passed,
		},
		{
			desc:      "correct namespace and wrong name",
			namespace: "test",
			name:      "bar-beconfig",
			expect:    report.Failed,
		},
		{
			desc:      "wrong namespace and correct name",
			namespace: "namespace2",
			name:      "foo-beconfig",
			expect:    report.Failed,
		},
	} {
		_, res, _ := CheckBackendConfigExistence(tc.namespace, tc.name, "svc-1", client)
		if res != tc.expect {
			t.Errorf("For test case %q, expect check result = %s, but got %s", tc.desc, tc.expect, res)
		}
	}
}

func TestCheckHealthCheckConfig(t *testing.T) {

	thirtyVar := int64(30)
	twentyVar := int64(20)
	for _, tc := range []struct {
		desc   string
		spec   beconfigv1.BackendConfigSpec
		expect string
	}{
		{
			desc: "TimeoutSec equals to CheckIntervalSec",
			spec: beconfigv1.BackendConfigSpec{
				HealthCheck: &beconfigv1.HealthCheckConfig{
					CheckIntervalSec: &thirtyVar,
					TimeoutSec:       &thirtyVar,
				},
			},
			expect: report.Passed,
		},
		{
			desc: "TimeoutSec smaller than CheckIntervalSec",
			spec: beconfigv1.BackendConfigSpec{
				HealthCheck: &beconfigv1.HealthCheckConfig{
					CheckIntervalSec: &thirtyVar,
					TimeoutSec:       &twentyVar,
				},
			},
			expect: report.Passed,
		},
		{
			desc: "TimeoutSec larger than CheckIntervalSec",
			spec: beconfigv1.BackendConfigSpec{
				HealthCheck: &beconfigv1.HealthCheckConfig{
					CheckIntervalSec: &twentyVar,
					TimeoutSec:       &thirtyVar,
				},
			},
			expect: report.Failed,
		},
		{
			desc:   "No healthCheck specified",
			spec:   beconfigv1.BackendConfigSpec{},
			expect: report.Skipped,
		},
		{
			desc: "TimeoutSec not specified",
			spec: beconfigv1.BackendConfigSpec{
				HealthCheck: &beconfigv1.HealthCheckConfig{
					CheckIntervalSec: &twentyVar,
				},
			},
			expect: report.Skipped,
		},
		{
			desc: "CheckIntervalSec not specified",
			spec: beconfigv1.BackendConfigSpec{
				HealthCheck: &beconfigv1.HealthCheckConfig{
					TimeoutSec: &twentyVar,
				},
			},
			expect: report.Skipped,
		},
	} {
		beconfig := beconfigv1.BackendConfig{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "test",
				Name:      "foo-beconfig",
			},
			Spec: tc.spec,
		}
		res, _ := CheckHealthCheckTimeout(&beconfig, "")
		if diff := cmp.Diff(tc.expect, res); diff != "" {
			t.Errorf("For test case %s,  (-want +got):\n%s", tc.desc, diff)
		}
	}
}

func TestCheckFrontendConfigExistence(t *testing.T) {
	client := fakefeconfig.NewSimpleClientset()
	client.NetworkingV1beta1().FrontendConfigs("test").Create(context.TODO(), &feconfigv1beta1.FrontendConfig{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "test",
			Name:      "foo-feconfig",
		},
	}, metav1.CreateOptions{})

	for _, tc := range []struct {
		desc      string
		namespace string
		name      string
		expect    string
	}{
		{
			desc:   "empty input",
			expect: report.Failed,
		},
		{
			desc:      "correct namespace and correct name",
			namespace: "test",
			name:      "foo-feconfig",
			expect:    report.Passed,
		},
		{
			desc:      "correct namespace and wrong name",
			namespace: "test",
			name:      "bar-feconfig",
			expect:    report.Failed,
		},
		{
			desc:      "wrong namespace and correct name",
			namespace: "namespace2",
			name:      "foo-feconfig",
			expect:    report.Failed,
		},
	} {
		_, res, _ := CheckFrontendConfigExistence(tc.namespace, tc.name, client)
		if res != tc.expect {
			t.Errorf("For test case %q, expect check result = %s, but got %s", tc.desc, tc.expect, res)
		}
	}
}

func TestCheckIngressRule(t *testing.T) {

	for _, tc := range []struct {
		desc        string
		ingressRule networkingv1.IngressRule
		expect      string
	}{
		{
			desc: "Ingress rule with http field",
			ingressRule: networkingv1.IngressRule{
				IngressRuleValue: networkingv1.IngressRuleValue{
					HTTP: &networkingv1.HTTPIngressRuleValue{
						Paths: []networkingv1.HTTPIngressPath{
							{
								Path: "/*",
							},
						},
					},
				},
			},
			expect: report.Passed,
		},
		{
			desc: "Ingress rule without http field",
			ingressRule: networkingv1.IngressRule{
				IngressRuleValue: networkingv1.IngressRuleValue{},
			},
			expect: report.Failed,
		},
	} {
		_, res, _ := CheckIngressRule(&tc.ingressRule)
		if diff := cmp.Diff(tc.expect, res); diff != "" {
			t.Errorf("For test case %s,  (-want +got):\n%s", tc.desc, diff)
		}
	}
}

func TestCheckRuleHostOverwrite(t *testing.T) {
	for _, tc := range []struct {
		desc   string
		rules  []networkingv1.IngressRule
		expect string
	}{
		{
			desc:   "Empty rules",
			rules:  []networkingv1.IngressRule{},
			expect: report.Passed,
		},
		{
			desc: "Rules with identical host",
			rules: []networkingv1.IngressRule{
				{
					Host: "foo.bar.com",
				},
				{
					Host: "foo.bar.com",
				},
			},
			expect: report.Failed,
		},
		{
			desc: "Rules with unique hosts",
			rules: []networkingv1.IngressRule{
				{
					Host: "foo.bar.com",
				},
				{
					Host: "abc.xyz.com",
				},
			},
			expect: report.Passed,
		},
	} {
		res, _ := CheckRuleHostOverwrite(tc.rules)
		if res != tc.expect {
			t.Errorf("For test case %q, expect check result = %s, but got %s", tc.desc, tc.expect, res)
		}
	}
}

func TestCheckAppProtocolAnnotation(t *testing.T) {
	for _, tc := range []struct {
		desc   string
		svc    corev1.Service
		expect string
	}{
		{
			desc:   "empty input",
			expect: report.Skipped,
		},
		{
			desc: "service without AppProtocol annotation",
			svc: corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-1",
					Namespace: "test",
				},
			},
			expect: report.Skipped,
		},
		{
			desc: "service with valid AppProtocol annotation",
			svc: corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-1",
					Namespace: "test",
					Annotations: map[string]string{
						annotations.GoogleServiceApplicationProtocolKey: `{"port1": "HTTP", "port2": "HTTPS"}`,
					},
				},
			},
			expect: report.Passed,
		},
		{
			desc: "service with invalid AppProtocol annotation format",
			svc: corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-1",
					Namespace: "test",
					Annotations: map[string]string{
						annotations.GoogleServiceApplicationProtocolKey: `{"port1": ["HTTP", "port2", "HTTPS"]}`,
					},
				},
			},
			expect: report.Failed,
		},
		{
			desc: "service with invalid malformed AppProtocol annotation",
			svc: corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-1",
					Namespace: "test",
					Annotations: map[string]string{
						annotations.GoogleServiceApplicationProtocolKey: "{port1: HTTP, port2: HTTPS}",
					},
				},
			},
			expect: report.Failed,
		},
		{
			desc: "service with invalid AppProtocol annotation value",
			svc: corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-1",
					Namespace: "test",
					Annotations: map[string]string{
						annotations.GoogleServiceApplicationProtocolKey: `{"port1": "HTTP", "port2": "HTTP3"}`,
					},
				},
			},
			expect: report.Failed,
		},
	} {
		res, _ := CheckAppProtocolAnnotation(&tc.svc)
		if res != tc.expect {
			t.Errorf("For test case %q, expect check result = %s, but got %s", tc.desc, tc.expect, res)
		}
	}
}
