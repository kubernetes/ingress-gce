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

package main

import (
	"context"
	"fmt"
	"testing"

	computebeta "google.golang.org/api/compute/v0.beta"
	v1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/ingress-gce/pkg/annotations"
	backendconfigbeta "k8s.io/ingress-gce/pkg/apis/backendconfig/v1beta1"
	"k8s.io/ingress-gce/pkg/e2e"
	"k8s.io/ingress-gce/pkg/e2e/adapter"
	"k8s.io/ingress-gce/pkg/fuzz"
	"k8s.io/ingress-gce/pkg/fuzz/features"
	"k8s.io/ingress-gce/pkg/test"
)

func TestV1beta1BackendConfigFeatures(t *testing.T) {
	ctx := context.Background()
	t.Parallel()
	pstring := func(x string) *string { return &x }

	Framework.RunWithSandbox("v1beta1 backendconfig features", t, func(t *testing.T, s *e2e.Sandbox) {
		policies := []*computebeta.SecurityPolicy{
			buildPolicyAllowAll(fmt.Sprintf("enable-test-allow-all-%s", s.Namespace)),
		}
		defer func() {
			if err := cleanupSecurityPolicies(ctx, t, Framework.Cloud, policies); err != nil {
				t.Logf("cleanupSecurityPolicies(...) =  %v, want nil", err)
			}
		}()
		policies, err := createSecurityPolicies(ctx, t, Framework.Cloud, policies)
		if err != nil {
			t.Fatalf("createSecurityPolicies(...) = _, %v, want _, nil", err)
		}
		// Re-assign to get the populated self-link.
		testSecurityPolicy := policies[0]

		testBackendConfigAnnotation := map[string]string{
			annotations.BetaBackendConfigKey: `{"default":"backendconfig-v1beta1"}`,
		}
		testSvc, err := e2e.CreateEchoService(s, "service-1", testBackendConfigAnnotation)
		if err != nil {
			t.Fatalf("e2e.CreateEchoService(s, service-1, %q) = _, _, %v, want _, _, nil", testBackendConfigAnnotation, err)
		}

		backendConfig := &backendconfigbeta.BackendConfig{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: s.Namespace,
				Name:      "backendconfig-v1beta1",
			},
			Spec: backendconfigbeta.BackendConfigSpec{
				Cdn: &backendconfigbeta.CDNConfig{
					Enabled: true,
					CachePolicy: &backendconfigbeta.CacheKeyPolicy{
						IncludeHost:        true,
						IncludeProtocol:    false,
						IncludeQueryString: true,
					},
				},
				CustomRequestHeaders: &backendconfigbeta.CustomRequestHeadersConfig{
					Headers: []string{"X-Client-Geo-Location:{client_region},{client_city}"},
				},
				ConnectionDraining: &backendconfigbeta.ConnectionDrainingConfig{
					DrainingTimeoutSec: int64(30),
				},
				HealthCheck: &backendconfigbeta.HealthCheckConfig{
					CheckIntervalSec:   test.Int64ToPtr(7),
					TimeoutSec:         test.Int64ToPtr(3),
					HealthyThreshold:   test.Int64ToPtr(3),
					UnhealthyThreshold: test.Int64ToPtr(5),
					RequestPath:        pstring("/my-path"),
				},
				SecurityPolicy: &backendconfigbeta.SecurityPolicyConfig{
					Name: testSecurityPolicy.Name,
				},
				TimeoutSec: test.Int64ToPtr(42),
			},
		}
		bcKey := fmt.Sprintf("%s/%s", backendConfig.Namespace, backendConfig.Name)
		backendConfig, err = Framework.BackendConfigClient.CloudV1beta1().BackendConfigs(s.Namespace).Create(context.Background(), backendConfig, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("Error creating backend-config %s: %v", bcKey, err)
		}
		t.Logf("Backend config %s created", bcKey)

		port80 := v1.ServiceBackendPort{Number: 80}
		testIng := fuzz.NewIngressBuilder(s.Namespace, "ingress-1", "").
			DefaultBackend("service-1", port80).
			AddPath("test.com", "/", "service-1", port80).
			Build()
		ingKey := fmt.Sprintf("%s/%s", testIng.Namespace, testIng.Name)
		crud := adapter.IngressCRUD{C: Framework.Clientset}
		testIng, err = crud.Create(testIng)
		if err != nil {
			t.Fatalf("error creating Ingress spec: %v", err)
		}
		t.Logf("Ingress %s created", ingKey)

		testIng, err = e2e.WaitForIngress(s, testIng, nil, nil)
		if err != nil {
			t.Fatalf("e2e.WaitForIngress(_, %q, _, _) = _, %v; want _, nil", ingKey, err)
		}
		if len(testIng.Status.LoadBalancer.Ingress) < 1 {
			t.Fatalf("Ingress does not have an IP: %+v", testIng.Status)
		}

		vip := testIng.Status.LoadBalancer.Ingress[0].IP
		params := &fuzz.GCLBForVIPParams{VIP: vip, Validators: fuzz.FeatureValidators(features.All)}
		gclb, err := fuzz.GCLBForVIP(ctx, Framework.Cloud, params)
		if err != nil {
			t.Fatalf("fuzz.GCLBForVIP(..., %q, _) = _, %v; want _, nil", vip, err)
		}

		t.Logf("Checking on relevant backend service whether cache policy is properly configured")
		cachePolicy := &cachePolicy{
			includeHost:          backendConfig.Spec.Cdn.CachePolicy.IncludeHost,
			includeProtocol:      backendConfig.Spec.Cdn.CachePolicy.IncludeProtocol,
			includeQueryString:   backendConfig.Spec.Cdn.CachePolicy.IncludeQueryString,
			queryStringBlacklist: backendConfig.Spec.Cdn.CachePolicy.QueryStringBlacklist,
			queryStringWhitelist: backendConfig.Spec.Cdn.CachePolicy.QueryStringWhitelist,
		}
		if err := verifyCachePolicies(t, gclb, s.Namespace, testSvc.Name, cachePolicy); err != nil {
			t.Errorf("verifyCachePolicies(_, _, %q, %q, _) = %v, want nil", s.Namespace, testSvc.Name, err)
		}

		t.Logf("Checking on relevant backend service whether custom request headers are properly configured")
		if err := verifyHeaders(t, gclb, s.Namespace, testSvc.Name, &customRequestHeadersConfig{
			headers: backendConfig.Spec.CustomRequestHeaders.Headers,
		}); err != nil {
			t.Error(err)
		}

		t.Logf("Checking on relevant backend service whether connection draining is properly configured")
		drainingTimeout := backendConfig.Spec.ConnectionDraining.DrainingTimeoutSec
		if err := verifyConnectionDrainingTimeout(t, gclb, s.Namespace, testSvc.Name, drainingTimeout); err != nil {
			t.Errorf("verifyConnectionDrainingTimeout(..., %q, %q, %d) = %v, want nil", s.Namespace, testSvc.Name, drainingTimeout, err)
		}

		t.Logf("Checking on relevant backend service whether health-check is properly configured")
		healthCheck := &healthCheckConfig{
			checkIntervalSec:   backendConfig.Spec.HealthCheck.CheckIntervalSec,
			timeoutSec:         backendConfig.Spec.HealthCheck.TimeoutSec,
			healthyThreshold:   backendConfig.Spec.HealthCheck.HealthyThreshold,
			unhealthyThreshold: backendConfig.Spec.HealthCheck.UnhealthyThreshold,
			port:               backendConfig.Spec.HealthCheck.Port,
			hType:              backendConfig.Spec.HealthCheck.Type,
			requestPath:        backendConfig.Spec.HealthCheck.RequestPath,
		}
		if err := verifyHealthCheck(t, gclb, healthCheck); err != nil {
			t.Fatal(err)
		}

		t.Logf("Checking on relevant backend service whether security policy is properly attached")
		if err := verifySecurityPolicy(t, gclb, s.Namespace, testSvc.Name, testSecurityPolicy.SelfLink); err != nil {
			t.Errorf("verifySecurityPolicy(..., %q, %q, %q) = %v, want nil", s.Namespace, testSvc.Name, testSecurityPolicy.SelfLink, err)
		}

		t.Logf("Checking on relevant backend service whether timeout is properly configured")
		timeout := *backendConfig.Spec.TimeoutSec
		if err := verifyTimeout(t, gclb, s.Namespace, testSvc.Name, timeout); err != nil {
			t.Errorf("verifyTimeout(..., %q, %q, %d) = %v, want nil", s.Namespace, testSvc.Name, timeout, err)
		}

		t.Logf("Cleaning up test")

		if err := e2e.WaitForIngressDeletion(ctx, gclb, s, testIng, deleteOptions); err != nil {
			t.Errorf("e2e.WaitForIngressDeletion(..., %q, nil) = %v, want nil", testIng.Name, err)
		}
	})
}
