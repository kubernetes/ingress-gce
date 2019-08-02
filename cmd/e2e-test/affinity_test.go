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
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"k8s.io/ingress-gce/pkg/annotations"
	backendconfig "k8s.io/ingress-gce/pkg/apis/backendconfig/v1beta1"
	"k8s.io/ingress-gce/pkg/e2e"
	"k8s.io/ingress-gce/pkg/fuzz"
	"k8s.io/ingress-gce/pkg/fuzz/features"
	"k8s.io/ingress-gce/pkg/utils"
)

const (
	transitionPollTimeout = 5 * time.Minute
	tansitionPollInterval = 30 * time.Second
)

type affinityTransition struct {
	affinity string
	ttl      int64
}

func TestAffinity(t *testing.T) {
	t.Parallel()

	ing := fuzz.NewIngressBuilder("", "ingress-1", "").
		AddPath("test.com", "/", "service-1", intstr.FromInt(80)).
		Build()

	for _, tc := range []struct {
		desc       string
		ttl        int64
		expect     string
		transition affinityTransition
		beConfig   *backendconfig.BackendConfig
	}{
		{
			desc:       "http with no affinity.",
			beConfig:   fuzz.NewBackendConfigBuilder("", "backendconfig-1").Build(),
			expect:     "NONE",
			transition: affinityTransition{"CLIENT_IP", 10},
		},
		{
			desc: "http with cookie based affinity.",
			beConfig: fuzz.NewBackendConfigBuilder("", "backendconfig-1").
				SetSessionAffinity("GENERATED_COOKIE").
				Build(),
			expect:     "GENERATED_COOKIE",
			transition: affinityTransition{"NONE", 20},
		},
		{
			desc: "http with cookie based affinity and 60s ttl.",
			beConfig: fuzz.NewBackendConfigBuilder("", "backendconfig-1").
				SetSessionAffinity("GENERATED_COOKIE").
				SetAffinityCookieTtlSec(60).
				Build(),
			expect:     "GENERATED_COOKIE",
			ttl:        60,
			transition: affinityTransition{"CLIENT_IP", 30},
		},
	} {
		tc := tc // Capture tc as we are running this in parallel.
		Framework.RunWithSandbox(tc.desc, t, func(t *testing.T, s *e2e.Sandbox) {
			t.Parallel()

			ctx := context.Background()

			backendConfigAnnotation := map[string]string{
				annotations.BackendConfigKey: `{"default":"backendconfig-1"}`,
			}

			if _, err := Framework.BackendConfigClient.CloudV1beta1().BackendConfigs(s.Namespace).Create(tc.beConfig); err != nil {
				t.Fatalf("error creating BackendConfig: %v", err)
			}
			t.Logf("BackendConfig created (%s/%s) ", s.Namespace, tc.beConfig.Name)

			_, err := e2e.CreateEchoService(s, "service-1", backendConfigAnnotation)
			if err != nil {
				t.Fatalf("error creating echo service: %v", err)
			}
			t.Logf("Echo service created (%s/%s)", s.Namespace, "service-1")

			if _, err := Framework.Clientset.ExtensionsV1beta1().Ingresses(s.Namespace).Create(ing); err != nil {
				t.Fatalf("error creating Ingress spec: %v", err)
			}
			t.Logf("Ingress created (%s/%s)", s.Namespace, ing.Name)

			ing, err := e2e.WaitForIngress(s, ing, nil)
			if err != nil {
				t.Fatalf("error waiting for Ingress to stabilize: %v", err)
			}
			t.Logf("GCLB resources created (%s/%s)", s.Namespace, ing.Name)

			vip := ing.Status.LoadBalancer.Ingress[0].IP
			t.Logf("Ingress %s/%s VIP = %s", s.Namespace, ing.Name, vip)
			gclb, err := fuzz.GCLBForVIP(context.Background(), Framework.Cloud, vip, fuzz.FeatureValidators(features.All))
			if err != nil {
				t.Fatalf("Error getting GCP resources for LB with IP = %q: %v", vip, err)
			}

			// Check conformity
			if err := verifyAffinity(t, gclb, s.Namespace, "service-1", tc.expect, tc.ttl); err != nil {
				t.Error(err)
			}

			// Test modifications
			if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
				bc, err := Framework.BackendConfigClient.CloudV1beta1().BackendConfigs(s.Namespace).Get(tc.beConfig.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}
				if bc.Spec.SessionAffinity == nil {
					bc.Spec.SessionAffinity = &backendconfig.SessionAffinityConfig{}
				}
				bc.Spec.SessionAffinity.AffinityType = tc.transition.affinity
				bc.Spec.SessionAffinity.AffinityCookieTtlSec = &tc.transition.ttl
				_, err = Framework.BackendConfigClient.CloudV1beta1().BackendConfigs(s.Namespace).Update(bc)
				return err
			}); err != nil {
				t.Errorf("Failed to update BackendConfig affinity settings for %s: %v", t.Name(), err)
			}

			if err := wait.Poll(tansitionPollInterval, transitionPollTimeout, func() (bool, error) {
				gclb, err = fuzz.GCLBForVIP(context.Background(), Framework.Cloud, vip, fuzz.FeatureValidators(features.All))
				if err != nil {
					t.Logf("error getting GCP resources for LB with IP = %q: %v", vip, err)
					return false, nil
				}
				if err := verifyAffinity(t, gclb, s.Namespace, "service-1", tc.transition.affinity, tc.transition.ttl); err != nil {
					return false, nil
				}
				return true, nil
			}); err != nil {
				t.Errorf("error waiting for BackendConfig affinity transition propagation to GCLB: %v", err)
			}

			// Wait for GCLB resources to be deleted.
			if err := Framework.Clientset.ExtensionsV1beta1().Ingresses(s.Namespace).Delete(ing.Name, &metav1.DeleteOptions{}); err != nil {
				t.Errorf("Delete(%q) = %v, want nil", ing.Name, err)
			}

			deleteOptions := &fuzz.GCLBDeleteOptions{
				SkipDefaultBackend: true,
			}
			t.Logf("Waiting for GCLB resources to be deleted (%s/%s)", s.Namespace, ing.Name)
			if err := e2e.WaitForGCLBDeletion(ctx, Framework.Cloud, gclb, deleteOptions); err != nil {
				t.Errorf("e2e.WaitForGCLBDeletion(...) = %v, want nil", err)
			}
			t.Logf("GCLB resources deleted (%s/%s)", s.Namespace, ing.Name)
		})
	}
}

func verifyAffinity(t *testing.T, gclb *fuzz.GCLB, svcNamespace, svcName string, expect string, ttl int64) error {
	for _, bs := range gclb.BackendService {
		desc := utils.DescriptionFromString(bs.GA.Description)
		if desc.ServiceName != fmt.Sprintf("%s/%s", svcNamespace, svcName) {
			continue
		}
		if bs.GA.SessionAffinity != expect {
			return fmt.Errorf("verifyAffinity(..., %q, %q, ...) = %s, want %s (SessionAffinity)",
				svcNamespace, svcName, bs.GA.SessionAffinity, expect)
		}
		if bs.GA.AffinityCookieTtlSec != ttl {
			return fmt.Errorf("verifyAffinity(..., %q, %q, ...) = %v, want %v (AffinityCookieTtlSec)",
				svcNamespace, svcName, bs.GA.SessionAffinity, ttl)
		}
	}
	return nil
}
