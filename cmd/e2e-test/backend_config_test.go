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
	"fmt"
	"strings"
	"testing"
	"time"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"

	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/e2e"
	"k8s.io/ingress-gce/pkg/fuzz"
)

var (
	eventPollInterval = 15 * time.Second
	eventPollTimeout  = 3 * time.Minute
)

func TestBackendConfigNotExist(t *testing.T) {
	t.Parallel()

	Framework.RunWithSandbox("BackendConfig not exist", t, func(t *testing.T, s *e2e.Sandbox) {
		testBackendConfigAnnotation := map[string]string{
			annotations.BackendConfigKey: `{"default":"not-exist"}`,
		}
		if _, _, err := e2e.CreateEchoService(s, "service-1", testBackendConfigAnnotation); err != nil {
			t.Fatalf("e2e.CreateEchoService(s, service-1, %q) = _, _, %v, want _, _, nil", testBackendConfigAnnotation, err)
		}

		port80 := intstr.FromInt(80)
		testIng := fuzz.NewIngressBuilder("", "ingress-1", "").
			AddPath("test.com", "/", "service-1", port80).
			Build()
		testIng, err := Framework.Clientset.Extensions().Ingresses(s.Namespace).Create(testIng)
		if err != nil {
			t.Fatalf("error creating Ingress spec: %v", err)
		}
		t.Logf("Ingress %s/%s created", s.Namespace, testIng.Name)

		t.Logf("Waiting for BackendConfig warning event to be emitted")
		if err := wait.Poll(eventPollInterval, eventPollTimeout, func() (bool, error) {
			events, err := Framework.Clientset.CoreV1().Events(s.Namespace).List(metav1.ListOptions{})
			if err != nil {
				return false, fmt.Errorf("error in listing events: %s", err)
			}
			for _, event := range events.Items {
				if event.InvolvedObject.Kind != "Ingress" ||
					event.InvolvedObject.Name != "ingress-1" ||
					event.Type != v1.EventTypeWarning {
					continue
				}
				if strings.Contains(event.Message, "no BackendConfig") {
					t.Logf("BackendConfig warning event emitted")
					return true, nil
				}
			}
			t.Logf("No BackendConfig warning event is emitted yet")
			return false, nil
		}); err != nil {
			t.Fatalf("error waiting for BackendConfig warning event: %v", err)
		}

		testIng, err = Framework.Clientset.Extensions().Ingresses(s.Namespace).Get(testIng.Name, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("error retrieving Ingress %q: %v", testIng.Name, err)
		}
		if len(testIng.Status.LoadBalancer.Ingress) > 0 {
			t.Fatalf("Ingress should not have an IP: %+v", testIng.Status)
		}
	})
}
