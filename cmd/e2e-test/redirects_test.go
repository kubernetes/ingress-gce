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
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	frontendconfig "k8s.io/ingress-gce/pkg/apis/frontendconfig/v1beta1"
	"k8s.io/ingress-gce/pkg/e2e"
	"k8s.io/ingress-gce/pkg/e2e/adapter"
	"k8s.io/ingress-gce/pkg/fuzz"
)

// Does not run in parallel since this is a transition test
func TestHttpsRedirects(t *testing.T) {
	ctx := context.Background()
	port80 := v1.ServiceBackendPort{Number: 80}

	// Run all test cases in the same sandbox to share certs
	Framework.RunWithSandbox("https redirects transition_e2e", t, func(t *testing.T, s *e2e.Sandbox) {
		// Setup Certificates
		hosts := []string{"test.com"}
		var certs []*e2e.Cert
		for i, h := range hosts {
			name := fmt.Sprintf("cert%d--%s", i, s.Namespace)
			cert, err := e2e.NewCert(name, h, e2e.GCPCert, false)
			if err != nil {
				t.Fatalf("Error initializing cert: %v", err)
			}
			if err := cert.Create(s); err != nil {
				t.Fatalf("Error creating cert %s: %v", cert.Name, err)
			}
			certs = append(certs, cert)

			defer cert.Delete(s)
		}

		// Each test is a transition on the original ingress
		var gclb *fuzz.GCLB
		var ing *v1.Ingress
		for _, tc := range []struct {
			desc       string
			ingBuilder *fuzz.IngressBuilder
			config     *frontendconfig.HttpsRedirectConfig
		}{
			{
				desc: "Ingress with no redirects",
				ingBuilder: fuzz.NewIngressBuilder("", "ingress-1", "").
					DefaultBackend("service-1", port80).
					AddPath("test.com", "/", "service-1", port80),
			},
			{
				desc: "Enable Https Redirects and response code name",
				ingBuilder: fuzz.NewIngressBuilder("", "ingress-1", "").
					DefaultBackend("service-1", port80).
					AddPath("test.com", "/", "service-1", port80),
				config: &frontendconfig.HttpsRedirectConfig{Enabled: true, ResponseCodeName: "FOUND"},
			},
			{
				desc: "Disable Https Redirects and response code name",
				ingBuilder: fuzz.NewIngressBuilder("", "ingress-1", "").
					DefaultBackend("service-1", port80).
					AddPath("test.com", "/", "service-1", port80),
				config: &frontendconfig.HttpsRedirectConfig{Enabled: false},
			},
		} {
			t.Run(tc.desc, func(t *testing.T) {

				for _, cert := range certs {
					tc.ingBuilder.AddPresharedCerts([]string{cert.Name})
				}

				// FrontendConfig Ensure
				var feConfig *frontendconfig.FrontendConfig
				if tc.config != nil {
					tc.ingBuilder.SetFrontendConfig("e2e-test")
					feConfig = &frontendconfig.FrontendConfig{ObjectMeta: metav1.ObjectMeta{Name: "e2e-test"}, Spec: frontendconfig.FrontendConfigSpec{RedirectToHttps: tc.config}}
					if _, err := e2e.EnsureFrontendConfig(s, feConfig); err != nil {
						t.Fatalf("Error ensuring frontendconfig: %v", err)
					}
				}

				ing = tc.ingBuilder.Build()
				ing.Namespace = s.Namespace // namespace depends on sandbox

				_, err := e2e.CreateEchoService(s, "service-1", nil)
				if err != nil {
					t.Fatalf("Error creating echo service: %v", err)
				}
				t.Logf("Echo service created (%s/%s)", s.Namespace, "service-1")

				// Ensure Ingress
				crud := adapter.IngressCRUD{C: Framework.Clientset}
				_, err = crud.Create(ing)
				if err != nil {
					if errors.IsAlreadyExists(err) {
						if _, err := crud.Update(ing); err != nil {
							t.Fatalf("Error updating Ingress: %v", err)
						}
					} else {
						t.Fatalf("Error Creating Ingress: %v", err)
					}
				}

				ing, err = e2e.WaitForIngress(s, ing, feConfig, nil)
				if err != nil {
					t.Fatalf("Error waiting for Ingress to stabilize: %v", err)
				}
				t.Logf("GCLB resources created (%s/%s)", s.Namespace, ing.Name)

				// Check just for URL map deletion.  This URL map should have been created in another transition test case
				// TODO(shance): uncomment this once GC has been fixed
				//if tc.config != nil && !tc.config.Enabled {
				//	if err := e2e.WaitForRedirectURLMapDeletion(ctx, Framework.Cloud, gclb); err != nil {
				//		t.Errorf("WaitForRedirectURLMapDeletion(%v) = %v", gclb, err)
				//	}
				//}

				// Perform whitebox testing.
				gclb, err = e2e.WhiteboxTest(ing, feConfig, Framework.Cloud, "", s)
				if err != nil {
					t.Fatalf("e2e.WhiteboxTest(%s/%s, ...) = %v, want nil", ing.Namespace, ing.Name, err)
				}
			})
		}

		deleteOptions := &fuzz.GCLBDeleteOptions{
			SkipDefaultBackend: true,
		}
		if err := e2e.WaitForIngressDeletion(ctx, gclb, s, ing, deleteOptions); err != nil {
			t.Errorf("e2e.WaitForIngressDeletion(..., %q, nil) = %v, want nil", ing.Name, err)
		}
	})
}
