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

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	v1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/ingress-gce/pkg/annotations"
	backendconfigv1 "k8s.io/ingress-gce/pkg/apis/backendconfig/v1"
	backendconfigv1beta1 "k8s.io/ingress-gce/pkg/apis/backendconfig/v1beta1"
	"k8s.io/ingress-gce/pkg/e2e"
	"k8s.io/ingress-gce/pkg/e2e/adapter"
	"k8s.io/ingress-gce/pkg/fuzz"
	"k8s.io/ingress-gce/pkg/test"
)

var (
	eventPollInterval = 15 * time.Second
	eventPollTimeout  = 10 * time.Minute
)

func TestBackendConfigNegatives(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		desc           string
		svcAnnotations map[string]string
		backendConfig  *backendconfigv1.BackendConfig
		secretName     string
		expectedMsg    string
	}{
		{
			desc: "backend config not exist",
			svcAnnotations: map[string]string{
				annotations.BetaBackendConfigKey: `{"default":"backendconfig-1"}`,
			},
			expectedMsg: "no BackendConfig",
		},
		{
			desc: "invalid format in backend config annotation",
			svcAnnotations: map[string]string{
				annotations.BetaBackendConfigKey: `invalid`,
			},
			expectedMsg: fmt.Sprintf("%v", annotations.ErrBackendConfigInvalidJSON),
		},
		{
			desc: "enable both IAP and CDN in backend config",
			svcAnnotations: map[string]string{
				annotations.BetaBackendConfigKey: `{"default":"backendconfig-1"}`,
			},
			backendConfig: fuzz.NewBackendConfigBuilder("", "backendconfig-1").
				EnableCDN(true).
				SetIAPConfig(true, "bar").
				Build(),
			secretName:  "bar",
			expectedMsg: "iap and cdn cannot be enabled at the same time",
		},
	} {
		tc := tc // Capture tc as we are running this in parallel.
		Framework.RunWithSandbox(tc.desc, t, func(t *testing.T, s *e2e.Sandbox) {
			t.Parallel()

			if tc.backendConfig != nil {
				tc.backendConfig.Namespace = s.Namespace
				bcCRUD := adapter.BackendConfigCRUD{C: Framework.BackendConfigClient}
				if _, err := bcCRUD.Create(tc.backendConfig); err != nil {
					t.Fatalf("Error creating backend config: %v", err)
				}
				t.Logf("Backend config %s/%s created", s.Namespace, tc.backendConfig.Name)
			}

			if tc.secretName != "" {
				if _, err := e2e.CreateSecret(s, tc.secretName,
					map[string][]byte{
						"client_id":     []byte("my-id"),
						"client_secret": []byte("my-secret"),
					}); err != nil {
					t.Fatalf("Error creating secret %q: %v", tc.secretName, err)
				}
			}

			if _, err := e2e.CreateEchoService(s, "service-1", tc.svcAnnotations); err != nil {
				t.Fatalf("e2e.CreateEchoService(s, service-1, %q) = _, _, %v, want _, _, nil", tc.svcAnnotations, err)
			}

			port80 := networkingv1.ServiceBackendPort{Number: 80}
			testIng := fuzz.NewIngressBuilder(s.Namespace, "ingress-1", "").
				AddPath("test.com", "/", "service-1", port80).
				Build()
			crud := adapter.IngressCRUD{C: Framework.Clientset}
			testIng, err := crud.Create(testIng)
			if err != nil {
				t.Fatalf("error creating Ingress spec: %v", err)
			}
			t.Logf("Ingress %s/%s created", s.Namespace, testIng.Name)

			t.Logf("Waiting %v for warning event to be emitted", eventPollTimeout)
			if err := wait.Poll(eventPollInterval, eventPollTimeout, func() (bool, error) {
				events, err := Framework.Clientset.CoreV1().Events(s.Namespace).List(context.TODO(), metav1.ListOptions{})
				if err != nil {
					return false, fmt.Errorf("error in listing events: %s", err)
				}
				for _, event := range events.Items {
					if event.InvolvedObject.Kind != "Ingress" ||
						event.InvolvedObject.Name != "ingress-1" ||
						event.Type != v1.EventTypeWarning {
						continue
					}
					if strings.Contains(event.Message, tc.expectedMsg) {
						t.Logf("Warning event emitted")
						return true, nil
					}
				}
				t.Logf("No warning event is emitted yet")
				return false, nil
			}); err != nil {
				t.Fatalf("error waiting for BackendConfig warning event: %v", err)
			}

			testIng, err = crud.Get(s.Namespace, testIng.Name)
			if err != nil {
				t.Fatalf("error retrieving Ingress %q: %v", testIng.Name, err)
			}
			if len(testIng.Status.LoadBalancer.Ingress) > 0 {
				t.Fatalf("Ingress should not have an IP: %+v", testIng.Status)
			}
		})
	}
}

// TestBackendConfigAPI creates a backend config resource with v1 API and
// retrieves it with the v1beta1 API clientset. Also, tests v1beta1 => v1.
func TestBackendConfigAPI(t *testing.T) {
	t.Parallel()
	pstring := func(x string) *string { return &x }
	Framework.RunWithSandbox("API conversion", t, func(t *testing.T, s *e2e.Sandbox) {
		backendConfig := fuzz.NewBackendConfigBuilder(s.Namespace, "bc1").
			SetSessionAffinity("GENERATED_COOKIE").SetAffinityCookieTtlSec(60).
			EnableCDN(true).
			SetCachePolicy(&backendconfigv1.CacheKeyPolicy{
				IncludeHost:        true,
				IncludeProtocol:    false,
				IncludeQueryString: true,
			}).
			AddCustomRequestHeader("X-Client-Geo-Location:{client_region},{client_city}").
			SetConnectionDrainingTimeout(60).
			SetIAPConfig(true, "bar").
			SetSecurityPolicy("secpol1").SetTimeout(42).Build()
		backendConfig.Spec.HealthCheck = &backendconfigv1.HealthCheckConfig{
			CheckIntervalSec:   test.Int64ToPtr(7),
			TimeoutSec:         test.Int64ToPtr(3),
			HealthyThreshold:   test.Int64ToPtr(3),
			UnhealthyThreshold: test.Int64ToPtr(5),
			Port:               test.Int64ToPtr(8080),
			RequestPath:        pstring("/my-path"),
		}
		bcKey := fmt.Sprintf("%s/%s", backendConfig.Namespace, backendConfig.Name)
		bcData, err := json.Marshal(backendConfig)
		if err != nil {
			t.Fatalf("Failed to marshall backendconfig %s: %v", bcKey, err)
		}
		v1beta1BackendConfig := &backendconfigv1beta1.BackendConfig{}
		if err := json.Unmarshal(bcData, v1beta1BackendConfig); err != nil {
			t.Fatalf("Failed to unmarshall backendconfig %s into v1beta1: %v", bcKey, err)
		}

		// Create BackendConfig using v1 API and retrieve it using v1beta1 API.
		v1BcCRUD := adapter.BackendConfigCRUD{C: Framework.BackendConfigClient}
		if _, err := v1BcCRUD.Create(backendConfig); err != nil {
			t.Fatalf("Error creating v1 backendconfig %s: %v", bcKey, err)
		}
		t.Logf("BackendConfig %s created using V1 API", bcKey)
		gotV1beta1BC, err := Framework.BackendConfigClient.CloudV1beta1().BackendConfigs(backendConfig.Namespace).
			Get(context.Background(), backendConfig.Name, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("Error getting v1beta1 backendconfig %s: %v", bcKey, err)
		}

		if diff := cmp.Diff(v1beta1BackendConfig.Spec, gotV1beta1BC.Spec); diff != "" {
			t.Fatalf("Unexpected v1beta1 backendconfig spec (-want +got):\n%s", diff)
		}
		// Create BackendConfig using v1beta1 API and retrieve it using v1 API.
		backendConfig.Name = "bc2"
		v1beta1BackendConfig.Name = backendConfig.Name
		bcKey = fmt.Sprintf("%s/%s", backendConfig.Namespace, backendConfig.Name)
		if _, err := Framework.BackendConfigClient.CloudV1beta1().BackendConfigs(v1beta1BackendConfig.Namespace).
			Create(context.Background(), v1beta1BackendConfig, metav1.CreateOptions{}); err != nil {
			t.Fatalf("Error creating v1beta1 backendconfig %s: %v", bcKey, err)
		}
		gotV1BC, err := v1BcCRUD.Get(backendConfig.Namespace, backendConfig.Name)
		if err != nil {
			t.Fatalf("Error getting v1 backendconfig %s: %v", bcKey, err)
		}
		if diff := cmp.Diff(backendConfig.Spec, gotV1BC.Spec); diff != "" {
			t.Fatalf("Unexpected v1 backendconfig spec (-want +got):\n%s", diff)
		}
	})
}
