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
	"testing"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/ingress-gce/pkg/annotations"
	backendconfig "k8s.io/ingress-gce/pkg/apis/backendconfig/v1"
	"k8s.io/ingress-gce/pkg/e2e"
	"k8s.io/ingress-gce/pkg/e2e/adapter"
	"k8s.io/ingress-gce/pkg/fuzz"
	"k8s.io/ingress-gce/pkg/fuzz/features"
)

func TestHealthCheck(t *testing.T) {
	t.Parallel()

	pint64 := func(x int64) *int64 { return &x }
	pstring := func(x string) *string { return &x }

	for _, tc := range []struct {
		desc     string
		beConfig *backendconfig.BackendConfig
		want     *backendconfig.HealthCheckConfig
	}{
		{
			desc:     "override healthcheck path",
			beConfig: fuzz.NewBackendConfigBuilder("", "backendconfig-1").Build(),
			want: &backendconfig.HealthCheckConfig{
				CheckIntervalSec:   pint64(7),
				TimeoutSec:         pint64(3),
				HealthyThreshold:   pint64(3),
				UnhealthyThreshold: pint64(5),
				RequestPath:        pstring("/my-path"),
			},
		},
	} {
		tc := tc // Capture tc as we are running this in parallel.
		Framework.RunWithSandbox(tc.desc, t, func(t *testing.T, s *e2e.Sandbox) {
			t.Parallel()

			ctx := context.Background()

			backendConfigAnnotation := map[string]string{
				annotations.BackendConfigKey: `{"default":"backendconfig-1"}`,
			}
			tc.beConfig.Spec.HealthCheck = tc.want

			if _, err := Framework.BackendConfigClient.CloudV1().BackendConfigs(s.Namespace).Create(tc.beConfig); err != nil {
				t.Fatalf("error creating BackendConfig: %v", err)
			}
			t.Logf("BackendConfig created (%s/%s) ", s.Namespace, tc.beConfig.Name)

			_, err := e2e.CreateEchoService(s, "service-1", backendConfigAnnotation)
			if err != nil {
				t.Fatalf("error creating echo service: %v", err)
			}
			t.Logf("Echo service created (%s/%s)", s.Namespace, "service-1")

			ing := fuzz.NewIngressBuilder(s.Namespace, "ingress-1", "").
				DefaultBackend("service-1", intstr.FromInt(80)).
				Build()
			crud := adapter.IngressCRUD{C: Framework.Clientset}
			if _, err := crud.Create(ing); err != nil {
				t.Fatalf("error creating Ingress spec: %v", err)
			}
			t.Logf("Ingress created (%s/%s)", s.Namespace, ing.Name)

			ing, err = e2e.WaitForIngress(s, ing, nil)
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

			verifyHealthCheck(t, gclb, tc.want)

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

func verifyHealthCheck(t *testing.T, gclb *fuzz.GCLB, want *backendconfig.HealthCheckConfig) {
	// We assume there is a single service for now. The logic will have to be
	// changed if there is more than one backend service.
	for _, bs := range gclb.BackendService {
		for _, hcURL := range bs.GA.HealthChecks {
			rID, err := cloud.ParseResourceURL(hcURL)
			if err != nil {
				t.Fatalf("cloud.ParseResourceURL(%q) = _, %v; want _, nil", hcURL, err)
			}
			hc, ok := gclb.HealthCheck[*rID.Key]
			if !ok {
				t.Fatalf("HealthCheck %s not found in BackendService %+v", rID.Key, bs)
			}

			// Pull out the field that are in common among the different
			// healthchecks per protocol.
			common := struct {
				port        int64
				requestPath string
			}{}
			switch {
			case hc.GA.Http2HealthCheck != nil:
				common.port = hc.GA.Http2HealthCheck.Port
				common.requestPath = hc.GA.Http2HealthCheck.RequestPath
			case hc.GA.HttpHealthCheck != nil:
				common.port = hc.GA.HttpHealthCheck.Port
				common.requestPath = hc.GA.HttpHealthCheck.RequestPath
			case hc.GA.HttpsHealthCheck != nil:
				common.port = hc.GA.HttpsHealthCheck.Port
				common.requestPath = hc.GA.HttpsHealthCheck.RequestPath
			}

			if want.CheckIntervalSec != nil && hc.GA.CheckIntervalSec != *want.CheckIntervalSec {
				t.Errorf("HealthCheck %v checkIntervalSec = %d, want %d", rID.Key, hc.GA.CheckIntervalSec, *want.CheckIntervalSec)
			}
			if want.TimeoutSec != nil && hc.GA.TimeoutSec != *want.TimeoutSec {
				t.Errorf("HealthCheck %v timeoutSec = %d, want %d", rID.Key, hc.GA.TimeoutSec, *want.TimeoutSec)
			}
			if want.HealthyThreshold != nil && hc.GA.HealthyThreshold != *want.HealthyThreshold {
				t.Errorf("HealthCheck %v healthyThreshold = %d, want %d", rID.Key, hc.GA.HealthyThreshold, *want.HealthyThreshold)
			}
			if want.UnhealthyThreshold != nil && hc.GA.UnhealthyThreshold != *want.UnhealthyThreshold {
				t.Errorf("HealthCheck %v unhealthThreshold = %d, want %d", rID.Key, hc.GA.UnhealthyThreshold, *want.UnhealthyThreshold)
			}
			if want.Type != nil {
				t.Errorf("HealthCheck %v type = %s, want %s", rID.Key, hc.GA.Type, *want.Type)
			}
			if want.Port != nil && common.port != *want.Port {
				t.Errorf("HealthCheck %v port = %d, want %d", rID.Key, common.port, *want.Port)
			}
			if want.RequestPath != nil && common.requestPath != *want.RequestPath {
				t.Errorf("HealthCheck %v requestPath = %q, want %q", rID.Key, common.requestPath, *want.RequestPath)
			}
		}
	}
}
