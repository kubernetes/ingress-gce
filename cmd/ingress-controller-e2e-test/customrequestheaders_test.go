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

func TestCustomRequestHeaders(t *testing.T) {
	t.Parallel()

	ing := fuzz.NewIngressBuilder("", "ingress-1", "").
		AddPath("test.com", "/", "service-1", v1.ServiceBackendPort{Number: 80}).
		Build()

	for _, tc := range []struct {
		desc     string
		beConfig *backendconfig.BackendConfig
	}{
		{
			desc: "http one path w/ Custom header.",
			beConfig: fuzz.NewBackendConfigBuilder("", "backendconfig-1").
				AddCustomRequestHeader("X-Client-Geo-Location:{client_region},{client_city}").
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

			ing.Namespace = s.Namespace
			crud := adapter.IngressCRUD{C: Framework.Clientset}
			if _, err := crud.Create(ing); err != nil {
				t.Fatalf("error creating Ingress spec: %v", err)
			}
			t.Logf("Ingress created (%s/%s)", s.Namespace, ing.Name)

			ing, err := e2e.WaitForIngress(s, ing, nil, nil)
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

			if tc.beConfig.Spec.CustomRequestHeaders != nil {
				verifyHeaders(t, gclb, s.Namespace, "service-1", tc.beConfig.Spec.CustomRequestHeaders)
			}

			// Wait for GCLB resources to be deleted.
			if err := crud.Delete(ing.Namespace, ing.Name); err != nil {
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

func verifyHeaders(t *testing.T, gclb *fuzz.GCLB, svcNamespace, svcName string, expectedCustomRequestHeaders *backendconfig.CustomRequestHeadersConfig) error {
	numBsWithCRH := 0
	for _, bs := range gclb.BackendService {
		desc := utils.DescriptionFromString(bs.GA.Description)
		if desc.ServiceName != fmt.Sprintf("%s/%s", svcNamespace, svcName) {
			continue
		}
		headers := bs.GA.CustomRequestHeaders
		if !reflect.DeepEqual(headers, expectedCustomRequestHeaders.Headers) {
			return fmt.Errorf("backend service %q has custom request headers %v, want %v", bs.GA.Name, headers, expectedCustomRequestHeaders.Headers)
		}

		t.Logf("Backend service %q has expected custom request headers", bs.GA.Name)
		numBsWithCRH = numBsWithCRH + 1
	}
	if numBsWithCRH != 1 {
		return fmt.Errorf("unexpected number of backend service has custom request headers: got %d, want 1", numBsWithCRH)
	}
	return nil
}
