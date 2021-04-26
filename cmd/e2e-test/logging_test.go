/*
Copyright 2020 The Kubernetes Authors.

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

	v1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"k8s.io/ingress-gce/pkg/annotations"
	backendconfig "k8s.io/ingress-gce/pkg/apis/backendconfig/v1"
	"k8s.io/ingress-gce/pkg/e2e"
	"k8s.io/ingress-gce/pkg/e2e/adapter"
	"k8s.io/ingress-gce/pkg/fuzz"
	"k8s.io/ingress-gce/pkg/fuzz/features"
	"k8s.io/ingress-gce/pkg/test"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/common"
)

type logging struct {
	// whether logging is enabled.
	enabled bool
	// log sampling rate, takes a value between 0.0 and 1.0.
	sampleRate float64
}

func TestLogging(t *testing.T) {
	t.Parallel()
	const svcName = "service1"

	for _, tc := range []struct {
		desc       string
		beConfig   *backendconfig.BackendConfig
		expect     logging
		transition logging
	}{
		{
			desc: "nil logging config",
			beConfig: fuzz.NewBackendConfigBuilder("", "nil-log-config-beconfig").
				Build(),
			// Logging is expected to be on by default when it is not configured.
			expect: logging{
				enabled:    true,
				sampleRate: 1.0,
			},
			transition: logging{
				enabled: false,
			},
		},
		{
			desc: "logging disabled",
			beConfig: fuzz.NewBackendConfigBuilder("", "logging-disabled-beconfig").
				EnableLogging(false).
				Build(),
			expect: logging{
				enabled: false,
			},
			transition: logging{
				enabled:    true,
				sampleRate: 0.5,
			},
		},
		{
			desc: "update sample rate",
			beConfig: fuzz.NewBackendConfigBuilder("", "sample-rate-beconfig").
				EnableLogging(true).SetSampleRate(test.Float64ToPtr(0.5)).
				Build(),
			expect: logging{
				enabled:    true,
				sampleRate: 0.5,
			},
			transition: logging{
				enabled:    true,
				sampleRate: 0.75,
			},
		},
	} {
		tc := tc // Capture tc as we are running this in parallel.
		Framework.RunWithSandbox(tc.desc, t, func(t *testing.T, s *e2e.Sandbox) {
			t.Parallel()

			ctx := context.Background()

			backendConfigAnnotation := map[string]string{
				annotations.BackendConfigKey: fmt.Sprintf(`{"default":"%s"}`, tc.beConfig.Name),
			}

			bcCRUD := adapter.BackendConfigCRUD{C: Framework.BackendConfigClient}
			tc.beConfig.Namespace = s.Namespace

			if _, err := bcCRUD.Create(tc.beConfig); err != nil {
				t.Fatalf("Failed to create BackendConfig: %v", err)
			}
			t.Logf("BackendConfig created (%s/%s) ", s.Namespace, tc.beConfig.Name)

			_, err := e2e.CreateEchoService(s, svcName, backendConfigAnnotation)
			if err != nil {
				t.Fatalf("Failed to create echo service: %v", err)
			}
			t.Logf("Echo service created (%s/%s)", s.Namespace, svcName)

			ing := fuzz.NewIngressBuilder(s.Namespace, "ingress-1", "").
				AddPath("test.com", "/", svcName, v1.ServiceBackendPort{Number: 80}).
				Build()
			ingKey := common.NamespacedName(ing)
			crud := adapter.IngressCRUD{C: Framework.Clientset}
			if _, err := crud.Create(ing); err != nil {
				t.Fatalf("crud.Create(%s) = %v, want nil; Ingress: %v", ingKey, err, ing)
			}
			t.Logf("Ingress created (%s)", ingKey)

			ing, err = e2e.WaitForIngress(s, ing, nil, &e2e.WaitForIngressOptions{ExpectUnreachable: true})
			if err != nil {
				t.Fatalf("Error waiting for Ingress %s to stabilize: %v", ingKey, err)
			}
			t.Logf("GCLB resources created (%s)", ingKey)

			if len(ing.Status.LoadBalancer.Ingress) < 1 {
				t.Fatalf("Ingress %s does not have a VIP: %+v", ingKey, ing.Status)
			}
			vip := ing.Status.LoadBalancer.Ingress[0].IP
			t.Logf("Ingress %s/%s VIP = %s", s.Namespace, ing.Name, vip)
			params := &fuzz.GCLBForVIPParams{VIP: vip, Validators: fuzz.FeatureValidators(features.All)}
			gclb, err := fuzz.GCLBForVIP(context.Background(), Framework.Cloud, params)
			if err != nil {
				t.Fatalf("Failed to get GCP resources for LB with IP = %q: %v", vip, err)
			}

			// Verify logging configuration.
			if err := verifyLogging(t, gclb, s.Namespace, svcName, tc.expect); err != nil {
				t.Error(err)
			}

			// Test transitions.
			if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
				bc, err := bcCRUD.Get(tc.beConfig.Namespace, tc.beConfig.Name)
				if err != nil {
					return err
				}
				// Update backend config.
				bc = fuzz.NewBackendConfigBuilderFromExisting(bc).
					EnableLogging(tc.transition.enabled).
					SetSampleRate(&tc.transition.sampleRate).Build()
				_, err = bcCRUD.Update(bc)
				return err
			}); err != nil {
				t.Fatalf("Failed to update BackendConfig logging settings: %v", err)
			}

			// Wait for transition settings to be propagated.
			if waitErr := wait.Poll(transitionPollInterval, transitionPollTimeout, func() (bool, error) {
				gclb, err = fuzz.GCLBForVIP(context.Background(), Framework.Cloud, params)
				if err != nil {
					t.Logf("Failed to GCP resources for LB with IP = %q: %v", vip, err)
					return false, nil
				}
				if err := verifyLogging(t, gclb, s.Namespace, svcName, tc.transition); err != nil {
					return false, nil
				}
				return true, nil
			}); waitErr != nil {
				t.Errorf("Timeout waiting for BackendConfig logging transition propagation to GCLB, last seen error: %v", err)
			}

			// Wait for GCLB resources to be deleted.
			if err := crud.Delete(s.Namespace, ing.Name); err != nil {
				t.Errorf("Delete(%q) = %v, want nil", ingKey, err)
			}

			deleteOptions := &fuzz.GCLBDeleteOptions{
				SkipDefaultBackend: true,
			}
			t.Logf("Waiting for GCLB resources to be deleted (%s)", ingKey)
			if err := e2e.WaitForGCLBDeletion(ctx, Framework.Cloud, gclb, deleteOptions); err != nil {
				t.Errorf("e2e.WaitForGCLBDeletion(_, _, %q, _) = %v, want nil", vip, err)
			}
			t.Logf("GCLB resources deleted (%s)", ingKey)
		})
	}
}

func verifyLogging(t *testing.T, gclb *fuzz.GCLB, svcNamespace, svcName string, expectedLogConfig logging) error {
	for _, bs := range gclb.BackendService {
		desc := utils.DescriptionFromString(bs.GA.Description)
		if desc.ServiceName != fmt.Sprintf("%s/%s", svcNamespace, svcName) {
			continue
		}
		logConfig := bs.GA.LogConfig
		if logConfig.Enable != expectedLogConfig.enabled {
			return fmt.Errorf("expected logging to be %t but got %t for backend service %q", expectedLogConfig.enabled, logConfig.Enable, bs.GA.Name)
		}
		// Verify sample rate only if logging is enabled.
		if logConfig.Enable && logConfig.SampleRate != expectedLogConfig.sampleRate {
			return fmt.Errorf("expected sample rate %f but got %f for backend service %q", expectedLogConfig.sampleRate, logConfig.SampleRate, bs.GA.Name)
		}
		t.Logf("Backend service %q has expected logging configuration", bs.GA.Name)
	}
	return nil
}
