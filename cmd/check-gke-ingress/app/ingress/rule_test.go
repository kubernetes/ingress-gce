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
		checker   ServiceChecker
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
		checker := &ServiceChecker{
			client:    client,
			namespace: tc.namespace,
			name:      tc.name,
		}
		_, res, _ := CheckServiceExistence(checker)
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
		checker := &ServiceChecker{
			service: &tc.svc,
		}
		_, res, _ := CheckBackendConfigAnnotation(checker)
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
		checker := &BackendConfigChecker{
			namespace: tc.namespace,
			name:      tc.name,
			client:    client,
		}
		_, res, _ := CheckBackendConfigExistence(checker)
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
		checker := &BackendConfigChecker{
			beConfig: &beconfig,
		}
		_, res, _ := CheckHealthCheckTimeout(checker)
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
		checker := &FrontendConfigChecker{
			namespace: tc.namespace,
			name:      tc.name,
			client:    client,
		}
		_, res, _ := CheckFrontendConfigExistence(checker)
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
		checker := &IngressChecker{
			ingress: &networkingv1.Ingress{
				Spec: networkingv1.IngressSpec{
					Rules: []networkingv1.IngressRule{
						tc.ingressRule,
					},
				},
			},
		}
		_, res, _ := CheckIngressRule(checker)
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
		checker := &IngressChecker{
			ingress: &networkingv1.Ingress{
				Spec: networkingv1.IngressSpec{
					Rules: tc.rules,
				},
			},
		}
		_, res, _ := CheckRuleHostOverwrite(checker)
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
		checker := &ServiceChecker{
			service: &tc.svc,
		}
		_, res, _ := CheckAppProtocolAnnotation(checker)
		if res != tc.expect {
			t.Errorf("For test case %q, expect check result = %s, but got %s", tc.desc, tc.expect, res)
		}
	}
}

func TestCheckL7ILBFrontendConfig(t *testing.T) {
	for _, tc := range []struct {
		desc    string
		ingress networkingv1.Ingress
		expect  string
	}{
		{
			desc: "Not internal ingress",
			ingress: networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test",
					Name:      "ingress-1",
					Annotations: map[string]string{
						annotations.FrontendConfigKey: "feconfig",
					},
				},
			},
			expect: report.Skipped,
		},
		{
			desc: "Internal ingress with feconfig",
			ingress: networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test",
					Name:      "ingress-1",
					Annotations: map[string]string{
						annotations.FrontendConfigKey: "feconfig",
						annotations.IngressClassKey:   annotations.GceL7ILBIngressClass,
					},
				},
			},
			expect: report.Failed,
		},
		{
			desc: "Internal ingress without feconfig",
			ingress: networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test",
					Name:      "ingress-1",
					Annotations: map[string]string{
						annotations.IngressClassKey: annotations.GceL7ILBIngressClass,
					},
				},
			},
			expect: report.Passed,
		},
	} {
		checker := &IngressChecker{
			ingress: &tc.ingress,
		}
		_, res, _ := CheckL7ILBFrontendConfig(checker)
		if res != tc.expect {
			t.Errorf("For test case %q, expect check result = %s, but got %s", tc.desc, tc.expect, res)
		}
	}
}

func TestCheckL7ILBNegAnnotation(t *testing.T) {
	for _, tc := range []struct {
		desc   string
		svc    corev1.Service
		expect string
	}{
		{
			desc: "Service without NEG annotation",
			svc: corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-1",
					Namespace: "test",
				},
			},
			expect: report.Failed,
		},
		{
			desc: "Service with invalid NEG annotation json",
			svc: corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-1",
					Namespace: "test",
					Annotations: map[string]string{
						annotations.NEGAnnotationKey: `{"ingress": true,}`,
					},
				},
			},
			expect: report.Failed,
		},
		{
			desc: "Service with NEG annotation which does not have ingress key",
			svc: corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-1",
					Namespace: "test",
					Annotations: map[string]string{
						annotations.NEGAnnotationKey: `{"exposed_ports": {"80":{"name": "neg1"}}}`,
					},
				},
			},
			expect: report.Failed,
		},
		{
			desc: "Service with correct NEG annotation",
			svc: corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-1",
					Namespace: "test",
					Annotations: map[string]string{
						annotations.NEGAnnotationKey: `{"ingress": true}`,
					},
				},
			},
			expect: report.Passed,
		},
	} {
		checker := &ServiceChecker{
			service: &tc.svc,
			isL7ILB: true,
		}
		_, res, _ := CheckL7ILBNegAnnotation(checker)
		if res != tc.expect {
			t.Errorf("For test case %q, expect check result = %s, but got %s", tc.desc, tc.expect, res)
		}
	}
}

// TestCheckAllIngresses tests whether all the checks are triggered.
func TestCheckAllIngresses(t *testing.T) {

	client := fake.NewSimpleClientset()
	beClient := fakebeconfig.NewSimpleClientset()
	feClient := fakefeconfig.NewSimpleClientset()

	thirtyVar := int64(30)
	twentyVar := int64(20)

	ingress1 := networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ingress1",
			Namespace: "test",
			Annotations: map[string]string{
				annotations.IngressClassKey:   annotations.GceL7ILBIngressClass,
				annotations.FrontendConfigKey: "feConfig-1",
			},
		},
		Spec: networkingv1.IngressSpec{
			DefaultBackend: &networkingv1.IngressBackend{
				Service: &networkingv1.IngressServiceBackend{
					Name: "svc-1",
				},
			},
		},
	}
	ingress2 := networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ingress2",
			Namespace: "test",
			Annotations: map[string]string{
				annotations.FrontendConfigKey: "feConfig-2",
			},
		},
		Spec: networkingv1.IngressSpec{
			Rules: []networkingv1.IngressRule{
				{
					Host: "abc.xyz",
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path: "/*",
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: "svc-2",
										},
									},
								},
							},
						},
					},
				},
				{
					Host: "abc.xyz",
				},
			},
		},
	}
	client.NetworkingV1().Ingresses("test").Create(context.TODO(), &ingress1, metav1.CreateOptions{})
	client.NetworkingV1().Ingresses("test").Create(context.TODO(), &ingress2, metav1.CreateOptions{})

	client.CoreV1().Services("test").Create(context.TODO(), &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc-1",
			Namespace: "test",
			Annotations: map[string]string{
				annotations.BackendConfigKey:              `{"default": "beconfig1"}`,
				annotations.ServiceApplicationProtocolKey: `{"8443":"HTTP3","8080":"HTTP"}`,
			},
		},
	}, metav1.CreateOptions{})

	client.CoreV1().Services("test").Create(context.TODO(), &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc-2",
			Namespace: "test",
			Annotations: map[string]string{
				annotations.BackendConfigKey: `{"default": "beconfig2"}`,
			},
		},
	}, metav1.CreateOptions{})

	beClient.CloudV1().BackendConfigs("test").Create(context.TODO(), &beconfigv1.BackendConfig{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "test",
			Name:      "beconfig1",
		},
	}, metav1.CreateOptions{})

	beClient.CloudV1().BackendConfigs("test").Create(context.TODO(), &beconfigv1.BackendConfig{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "test",
			Name:      "beconfig2",
		},
		Spec: beconfigv1.BackendConfigSpec{
			HealthCheck: &beconfigv1.HealthCheckConfig{
				CheckIntervalSec: &twentyVar,
				TimeoutSec:       &thirtyVar,
			},
		},
	}, metav1.CreateOptions{})

	feClient.NetworkingV1beta1().FrontendConfigs("test").Create(context.TODO(), &feconfigv1beta1.FrontendConfig{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "test",
			Name:      "feConfig-2",
		},
	}, metav1.CreateOptions{})

	result := CheckAllIngresses("test", client, beClient, feClient)
	checkSet := make(map[string]struct{})
	for _, resource := range result.Resources {
		for _, check := range resource.Checks {
			checkSet[check.Name] = struct{}{}
		}
	}

	for _, check := range []string{
		ServiceExistenceCheck,
		BackendConfigAnnotationCheck,
		BackendConfigExistenceCheck,
		HealthCheckTimeoutCheck,
		IngressRuleCheck,
		FrontendConfigExistenceCheck,
		RuleHostOverwriteCheck,
		AppProtocolAnnotationCheck,
		L7ILBFrontendConfigCheck,
		L7ILBNegAnnotationCheck,
	} {
		if _, ok := checkSet[check]; !ok {
			t.Errorf("Missing check %s in CheckAllIngresses", check)
		}
	}

	expect := report.Report{
		Resources: []*report.Resource{
			{
				Kind:      "Ingress",
				Namespace: "test",
				Name:      "ingress1",
				Checks: []*report.Check{
					{Name: "IngressRuleCheck", Result: "PASSED"},
					{Name: "L7ILBFrontendConfigCheck", Result: "FAILED"},
					{Name: "RuleHostOverwriteCheck", Result: "PASSED"},
					{Name: "FrontendConfigExistenceCheck", Result: "FAILED"},
					{Name: "ServiceExistenceCheck", Result: "PASSED"},
					{Name: "BackendConfigAnnotationCheck", Result: "PASSED"},
					{Name: "AppProtocolAnnotationCheck", Result: "FAILED"},
					{Name: "L7ILBNegAnnotationCheck", Result: "FAILED"},
					{Name: "BackendConfigExistenceCheck", Result: "PASSED"},
					{Name: "HealthCheckTimeoutCheck", Result: "SKIPPED"},
				},
			},
			{
				Kind:      "Ingress",
				Namespace: "test",
				Name:      "ingress2",
				Checks: []*report.Check{
					{Name: "IngressRuleCheck", Result: "FAILED"},
					{Name: "L7ILBFrontendConfigCheck", Result: "SKIPPED"},
					{Name: "RuleHostOverwriteCheck", Result: "FAILED"},
					{Name: "FrontendConfigExistenceCheck", Result: "PASSED"},
					{Name: "ServiceExistenceCheck", Result: "PASSED"},
					{Name: "BackendConfigAnnotationCheck", Result: "PASSED"},
					{Name: "AppProtocolAnnotationCheck", Result: "SKIPPED"},
					{Name: "L7ILBNegAnnotationCheck", Result: "SKIPPED"},
					{Name: "BackendConfigExistenceCheck", Result: "PASSED"},
					{Name: "HealthCheckTimeoutCheck", Result: "FAILED"},
				},
			},
		},
	}
	for i, resource := range result.Resources {
		for j, check := range resource.Checks {
			if diff := cmp.Diff(expect.Resources[i].Checks[j].Result, check.Result); diff != "" {
				t.Errorf("For ingress check %s for ingress %s/%s, (-want +got):\n%s", check.Name, resource.Namespace, resource.Name, diff)
			}
		}
	}
}
