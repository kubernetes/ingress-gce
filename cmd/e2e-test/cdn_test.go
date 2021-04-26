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
	"reflect"
	"testing"

	v1 "k8s.io/api/networking/v1"
	"k8s.io/ingress-gce/pkg/annotations"
	backendconfig "k8s.io/ingress-gce/pkg/apis/backendconfig/v1"
	"k8s.io/ingress-gce/pkg/e2e"
	"k8s.io/ingress-gce/pkg/e2e/adapter"
	"k8s.io/ingress-gce/pkg/fuzz"
	"k8s.io/ingress-gce/pkg/fuzz/features"
	"k8s.io/ingress-gce/pkg/utils"
)

// TODO(rramkumar): Add transition test.

func TestCDN(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		desc     string
		beConfig *backendconfig.BackendConfig
	}{
		{
			desc: "http one path w/ CDN.",
			beConfig: fuzz.NewBackendConfigBuilder("", "backendconfig-1").
				EnableCDN(true).
				Build(),
		},
		{
			desc: "http one path w/ CDN & cache policies.",
			beConfig: fuzz.NewBackendConfigBuilder("", "backendconfig-1").
				EnableCDN(true).
				SetCachePolicy(&backendconfig.CacheKeyPolicy{
					IncludeHost:        true,
					IncludeProtocol:    false,
					IncludeQueryString: true,
				}).
				Build(),
		},
		{
			desc: "http one path w/ no CDN.",
			beConfig: fuzz.NewBackendConfigBuilder("", "backendconfig-1").
				EnableCDN(false).
				Build(),
		},
	} {
		tc := tc // Capture tc as we are running this in parallel.
		Framework.RunWithSandbox(tc.desc, t, func(t *testing.T, s *e2e.Sandbox) {
			t.Parallel()

			ctx := context.Background()

			backendConfigAnnotation := map[string]string{
				annotations.BetaBackendConfigKey: `{"default":"backendconfig-1"}`,
			}

			bcCRUD := adapter.BackendConfigCRUD{C: Framework.BackendConfigClient}
			tc.beConfig.Namespace = s.Namespace

			if _, err := bcCRUD.Create(tc.beConfig); err != nil {
				t.Fatalf("error creating BackendConfig: %v", err)
			}
			t.Logf("BackendConfig created (%s/%s) ", s.Namespace, tc.beConfig.Name)

			_, err := e2e.CreateEchoService(s, "service-1", backendConfigAnnotation)
			if err != nil {
				t.Fatalf("error creating echo service: %v", err)
			}
			t.Logf("Echo service created (%s/%s)", s.Namespace, "service-1")

			ing := fuzz.NewIngressBuilder(s.Namespace, "ingress-1", "").
				AddPath("test.com", "/", "service-1", v1.ServiceBackendPort{Number: 80}).
				Build()
			crud := adapter.IngressCRUD{C: Framework.Clientset}
			if _, err := crud.Create(ing); err != nil {
				t.Fatalf("error creating Ingress spec: %v", err)
			}
			t.Logf("Ingress created (%s/%s)", s.Namespace, ing.Name)

			ing, err = e2e.WaitForIngress(s, ing, nil, nil)
			if err != nil {
				t.Fatalf("error waiting for Ingress to stabilize: %v", err)
			}
			t.Logf("GCLB resources created (%s/%s)", s.Namespace, ing.Name)

			vip := ing.Status.LoadBalancer.Ingress[0].IP
			t.Logf("Ingress %s/%s VIP = %s", s.Namespace, ing.Name, vip)
			params := &fuzz.GCLBForVIPParams{VIP: vip, Validators: fuzz.FeatureValidators(features.All)}
			gclb, err := fuzz.GCLBForVIP(context.Background(), Framework.Cloud, params)
			if err != nil {
				t.Fatalf("Error getting GCP resources for LB with IP = %q: %v", vip, err)
			}

			// If needed, verify the cache policies were applied.
			if tc.beConfig.Spec.Cdn.CachePolicy != nil {
				verifyCachePolicies(t, gclb, s.Namespace, "service-1", tc.beConfig.Spec.Cdn.CachePolicy)
			}

			// Wait for GCLB resources to be deleted.
			if err := crud.Delete(s.Namespace, ing.Name); err != nil {
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

func verifyCachePolicies(t *testing.T, gclb *fuzz.GCLB, svcNamespace, svcName string, expectedCachePolicies *backendconfig.CacheKeyPolicy) error {
	numBsWithPolicy := 0
	for _, bs := range gclb.BackendService {
		desc := utils.DescriptionFromString(bs.GA.Description)
		if desc.ServiceName != fmt.Sprintf("%s/%s", svcNamespace, svcName) {
			continue
		}
		if bs.GA.CdnPolicy == nil || bs.GA.CdnPolicy.CacheKeyPolicy == nil {
			return fmt.Errorf("backend service %q has no cache policy", bs.GA.Name)
		}
		cachePolicy := bs.GA.CdnPolicy.CacheKeyPolicy
		if expectedCachePolicies.IncludeHost != cachePolicy.IncludeHost ||
			expectedCachePolicies.IncludeProtocol != cachePolicy.IncludeProtocol ||
			expectedCachePolicies.IncludeQueryString != cachePolicy.IncludeQueryString ||
			!reflect.DeepEqual(expectedCachePolicies.QueryStringBlacklist, cachePolicy.QueryStringBlacklist) ||
			!reflect.DeepEqual(expectedCachePolicies.QueryStringWhitelist, cachePolicy.QueryStringWhitelist) {
			return fmt.Errorf("backend service %q has cache policy %v, want %v", bs.GA.Name, cachePolicy, expectedCachePolicies)
		}
		t.Logf("Backend service %q has expected cache policy", bs.GA.Name)
		numBsWithPolicy = numBsWithPolicy + 1
	}
	if numBsWithPolicy != 1 {
		return fmt.Errorf("unexpected number of backend service has cache policy attached: got %d, want 1", numBsWithPolicy)
	}
	return nil
}
