/*
Copyright 2022 The Kubernetes Authors.

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

func TestCustomResponseHeaders(t *testing.T) {
	t.Parallel()

	ing := fuzz.NewIngressBuilder("", "ingress-1", "").
		AddPath("test.com", "/", "service-1", v1.ServiceBackendPort{Number: 80}).
		Build()

	for _, tc := range []struct {
		desc     string
		beConfig *backendconfig.BackendConfig
	}{
		{
			desc: "http one path w/ Custom Response header.",
			beConfig: fuzz.NewBackendConfigBuilder("", "backendconfig-1").
				AddCustomResponseHeader("X-Test-Header: test").
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
				t.Fatalf(appendDesc(tc.desc, "error creating BackendConfig: %v"), err)
			}
			t.Logf(appendDesc(tc.desc, "BackendConfig created (%s/%s) "), s.Namespace, tc.beConfig.Name)

			_, err := e2e.CreateEchoService(s, "service-1", backendConfigAnnotation)
			if err != nil {
				t.Fatalf(appendDesc(tc.desc, "error creating echo service: %v"), err)
			}
			t.Logf(appendDesc(tc.desc, "Echo service created (%s/%s)"), s.Namespace, "service-1")

			ing.Namespace = s.Namespace
			crud := adapter.IngressCRUD{C: Framework.Clientset}
			if _, err := crud.Create(ing); err != nil {
				t.Fatalf(appendDesc(tc.desc, "error creating Ingress spec: %v"), err)
			}
			t.Logf(appendDesc(tc.desc, "Ingress created (%s/%s)"), s.Namespace, ing.Name)

			ing, err := e2e.WaitForIngress(s, ing, nil, nil)
			if err != nil {
				t.Fatalf(appendDesc(tc.desc, "error waiting for Ingress to stabilize: %v"), err)
			}
			t.Logf(appendDesc(tc.desc, "GCLB resources created (%s/%s)"), s.Namespace, ing.Name)

			vip := ing.Status.LoadBalancer.Ingress[0].IP
			t.Logf(appendDesc(tc.desc, "Ingress %s/%s VIP = %s"), s.Namespace, ing.Name, vip)
			params := &fuzz.GCLBForVIPParams{VIP: vip, Validators: fuzz.FeatureValidators(features.All)}
			gclb, err := fuzz.GCLBForVIP(context.Background(), Framework.Cloud, params)
			if err != nil {
				t.Fatalf(appendDesc(tc.desc, "Error getting GCP resources for LB with IP = %q: %v"), vip, err)
			}
			if tc.beConfig.Spec.CustomResponseHeaders != nil {
				verifyResponseHeaders(t, gclb, s.Namespace, "service-1", tc.beConfig.Spec.CustomResponseHeaders, tc.desc)
			}

			// Wait for GCLB resources to be deleted.
			if err := crud.Delete(ing.Namespace, ing.Name); err != nil {
				t.Errorf(appendDesc(tc.desc, "Delete(%q) = %v, want nil"), ing.Name, err)
			}

			deleteOptions := &fuzz.GCLBDeleteOptions{
				SkipDefaultBackend: true,
			}
			t.Logf(appendDesc(tc.desc, "Waiting for GCLB resources to be deleted (%s/%s)"), s.Namespace, ing.Name)
			if err := e2e.WaitForGCLBDeletion(ctx, Framework.Cloud, gclb, deleteOptions); err != nil {
				t.Errorf("e2e.WaitForGCLBDeletion(...) = %v, want nil", err)
			}
			t.Logf(appendDesc(tc.desc, "GCLB resources deleted (%s/%s)"), s.Namespace, ing.Name)
		})
	}
}

func verifyResponseHeaders(t *testing.T, gclb *fuzz.GCLB, svcNamespace, svcName string, expectedCustomResponseHeaders *backendconfig.CustomResponseHeadersConfig, testDesc string) error {
	numBsWithCRH := 0
	for _, bs := range gclb.BackendService {
		desc := utils.DescriptionFromString(bs.GA.Description)
		if desc.ServiceName != fmt.Sprintf("%s/%s", svcNamespace, svcName) {
			continue
		}
		headers := bs.GA.CustomResponseHeaders
		if !reflect.DeepEqual(headers, expectedCustomResponseHeaders.Headers) {
			return fmt.Errorf("backend service %q has custom response headers %v, want %v", bs.GA.Name, headers, expectedCustomResponseHeaders.Headers)
		}

		t.Logf(appendDesc(testDesc, "Backend service %q has expected custom response headers"), bs.GA.Name)
		numBsWithCRH = numBsWithCRH + 1
	}
	if numBsWithCRH != 1 {
		return fmt.Errorf("unexpected number of backend service has custom response headers: got %d, want 1", numBsWithCRH)
	}
	return nil
}

// Append test description to the log for debugging
func appendDesc(desc string, format string) string {
	return fmt.Sprintf("[%s]: %s", desc, format)
}
