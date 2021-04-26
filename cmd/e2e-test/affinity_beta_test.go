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
	"testing"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"k8s.io/ingress-gce/pkg/annotations"
	backendconfigbeta "k8s.io/ingress-gce/pkg/apis/backendconfig/v1beta1"
	"k8s.io/ingress-gce/pkg/e2e"
	"k8s.io/ingress-gce/pkg/e2e/adapter"
	"k8s.io/ingress-gce/pkg/fuzz"
	"k8s.io/ingress-gce/pkg/fuzz/features"
)

func TestAffinityBeta(t *testing.T) {
	t.Parallel()

	affinityTransitions := []affinityTransition{
		// "http with cookie based affinity."
		{affinity: "GENERATED_COOKIE"},
		// no affinity
		{affinity: "NONE"},
		// http with cookie based affinity and 60s ttl.
		{affinity: "GENERATED_COOKIE", ttl: 60},
		// client ip affinity.
		{affinity: "CLIENT_IP"},
	}

	Framework.RunWithSandbox("affinity-v1beta1", t, func(t *testing.T, s *e2e.Sandbox) {
		t.Parallel()
		ctx := context.Background()

		backendConfigAnnotation := map[string]string{
			annotations.BetaBackendConfigKey: `{"default":"backendconfigbeta"}`,
		}
		affinityType := "CLIENT_IP"
		beConfig := &backendconfigbeta.BackendConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name: "backendconfigbeta",
			},
			Spec: backendconfigbeta.BackendConfigSpec{
				SessionAffinity: &backendconfigbeta.SessionAffinityConfig{
					AffinityType: affinityType,
				},
			},
		}
		if _, err := Framework.BackendConfigClient.CloudV1beta1().BackendConfigs(s.Namespace).Create(context.TODO(), beConfig, metav1.CreateOptions{}); err != nil {
			t.Fatalf("CloudV1beta1().BackendConfigs(%q).Create(%#v) = %v, want nil", s.Namespace, beConfig, err)
		}
		t.Logf("BackendConfig created (%s/%s) ", s.Namespace, beConfig.Name)

		svcName := "service-1"
		_, err := e2e.CreateEchoService(s, svcName, backendConfigAnnotation)
		if err != nil {
			t.Fatalf("e2e.CreateEchoService(_, %q, %q) = %v, want nil", svcName, backendConfigAnnotation, err)
		}
		t.Logf("Echo service created (%s/%s)", s.Namespace, svcName)

		ing := fuzz.NewIngressBuilder(s.Namespace, "ingress-1", "").
			AddPath("test.com", "/", svcName, networkingv1.ServiceBackendPort{Number: 80}).
			Build()
		crud := adapter.IngressCRUD{C: Framework.Clientset}
		if _, err := crud.Create(ing); err != nil {
			t.Fatalf("crud.Create(%#v) = %v, want nil", ing, err)
		}
		ingKey := fmt.Sprintf("%s/%s", s.Namespace, ing.Name)
		t.Logf("Ingress created (%s)", ingKey)

		ing, err = e2e.WaitForIngress(s, ing, nil, nil)
		if err != nil {
			t.Fatalf("e2e.WaitForIngress(_, %q, nil) = %v, want nil", ingKey, err)
		}
		t.Logf("GCLB resources created (%s)", ingKey)

		vip := ing.Status.LoadBalancer.Ingress[0].IP
		t.Logf("Ingress %s VIP = %s", ingKey, vip)

		params := &fuzz.GCLBForVIPParams{VIP: vip, Validators: fuzz.FeatureValidators(features.All)}
		gclb, err := fuzz.GCLBForVIP(context.Background(), Framework.Cloud, params)
		if err != nil {
			t.Fatalf("fuzz.GCLBForVIP(_, _, %q) = %v, want nil; fail to get GCP resources for LB with IP(%q)", vip, err, vip)
		}

		// Check conformity.
		if err := verifyAffinity(t, gclb, s.Namespace, svcName, affinityType, 0); err != nil {
			t.Error(err)
		}

		for _, transition := range affinityTransitions {
			t.Run(fmt.Sprintf("%v", transition), func(t *testing.T) {
				// Test modifications.
				if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
					bc, err := Framework.BackendConfigClient.CloudV1beta1().BackendConfigs(s.Namespace).Get(context.TODO(), beConfig.Name, metav1.GetOptions{})
					if err != nil {
						return err
					}
					if bc.Spec.SessionAffinity == nil {
						bc.Spec.SessionAffinity = &backendconfigbeta.SessionAffinityConfig{}
					}
					bc.Spec.SessionAffinity.AffinityType = transition.affinity
					bc.Spec.SessionAffinity.AffinityCookieTtlSec = &transition.ttl
					_, err = Framework.BackendConfigClient.CloudV1beta1().BackendConfigs(s.Namespace).Update(context.TODO(), bc, metav1.UpdateOptions{})
					return err
				}); err != nil {
					t.Errorf("CloudV1beta1().BackendConfigs(%q).Update(%#v) = %v, want nil", s.Namespace, transition, err)
				}

				if waitErr := wait.Poll(transitionPollInterval, transitionPollTimeout, func() (bool, error) {
					gclb, err = fuzz.GCLBForVIP(context.Background(), Framework.Cloud, params)
					if err != nil {
						t.Logf("Error getting GCP resources for LB with IP(%q): %v", vip, err)
						return false, nil
					}
					if err := verifyAffinity(t, gclb, s.Namespace, svcName, transition.affinity, transition.ttl); err != nil {
						return false, nil
					}
					return true, nil
				}); waitErr != nil {
					t.Errorf("Error waiting for BackendConfig affinity transition propagation to GCLB, last seen error: %v", err)
				}
			})
		}

		// Wait for GCLB resources to be deleted.
		if err := crud.Delete(s.Namespace, ing.Name); err != nil {
			t.Errorf("crud.Delete(%q) = %v, want nil", ingKey, err)
		}

		deleteOptions := &fuzz.GCLBDeleteOptions{
			SkipDefaultBackend: true,
		}
		t.Logf("Waiting for GCLB resources to be deleted (%s)", ingKey)
		if err := e2e.WaitForGCLBDeletion(ctx, Framework.Cloud, gclb, deleteOptions); err != nil {
			t.Errorf("e2e.WaitForGCLBDeletion(_, _, %q, %#v) = %v, want nil", gclb.VIP, deleteOptions, err)
		}
		t.Logf("GCLB resources deleted (%s)", ingKey)
	})
}
