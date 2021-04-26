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
	"fmt"
	"strings"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/ingress-gce/pkg/e2e/adapter"

	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/ingress-gce/pkg/annotations"
	backendconfigv1 "k8s.io/ingress-gce/pkg/apis/backendconfig/v1"
	"k8s.io/ingress-gce/pkg/e2e"
	"k8s.io/ingress-gce/pkg/fuzz"
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
