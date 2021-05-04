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
	"net/http"
	"testing"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"google.golang.org/api/compute/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/ingress-gce/pkg/e2e"
	"k8s.io/ingress-gce/pkg/e2e/adapter"
	"k8s.io/ingress-gce/pkg/fuzz"
	"k8s.io/ingress-gce/pkg/utils"
)

const policyName = "e2e-ssl-policy"

// TestSSLPolicy is a transition test
func TestSSLPolicy(t *testing.T) {
	ctx := context.Background()
	port80 := networkingv1.ServiceBackendPort{Number: 80}

	// Run all test cases in the same sandbox to share certs
	Framework.RunWithSandbox("sslpolicy_e2e", t, func(t *testing.T, s *e2e.Sandbox) {
		// Shared amongst all tests
		sslPolicy := &compute.SslPolicy{Name: policyName, MinTlsVersion: "TLS_1_0", Profile: "COMPATIBLE"}
		ingBuilder := fuzz.NewIngressBuilder("", "ingress-1", "").
			DefaultBackend("service-1", port80).
			AddPath("test.com", "/", "service-1", port80)

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

		var gclb *fuzz.GCLB
		var ing *networkingv1.Ingress
		for _, tc := range []struct {
			desc             string
			configPolicyName string
		}{
			{
				desc:             "Set SslPolicy",
				configPolicyName: policyName,
			},
			{
				desc:             "Remove SslPolicy",
				configPolicyName: "",
			},
		} {
			t.Run(tc.desc, func(t *testing.T) {
				for _, cert := range certs {
					ingBuilder.AddPresharedCerts([]string{cert.Name})
				}

				// Ensure Ssl Policy exists, we re-use it for all e2e tests that run in the same project.
				err := Framework.Cloud.SslPolicies().Insert(ctx, meta.GlobalKey(policyName), sslPolicy)
				if err != nil {
					if !utils.IsHTTPErrorCode(err, http.StatusConflict) {
						t.Errorf("SslPolicies().Insert(%v, %v) = %v, want nil", meta.GlobalKey(policyName), sslPolicy, err)
					}
				} else {
					t.Logf("SslPolicy %q Created", policyName)
				}

				// Ensure FrontendConfig
				feConfig, err := e2e.EnsureFrontendConfig(s, fuzz.NewFrontendConfigBuilder(s.Namespace, "e2e-feconfig").SetSslPolicy(tc.configPolicyName).Build())
				if err != nil {
					t.Errorf("EnsureFrontendConfig(%v) = %v, want nil", feConfig, err)
				}

				ing = ingBuilder.SetFrontendConfig(feConfig.Name).Build()
				ing.Namespace = s.Namespace // namespace depends on sandbox

				_, err = e2e.CreateEchoService(s, "service-1", nil)
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

				ing, err = e2e.WaitForIngress(s, ing, nil, nil)
				if err != nil {
					t.Fatalf("Error waiting for Ingress to stabilize: %v", err)
				}
				t.Logf("GCLB resources created (%s/%s)", s.Namespace, ing.Name)

				// Perform whitebox testing.
				gclb, err = e2e.WhiteboxTest(ing, nil, Framework.Cloud, "", s)
				if err != nil {
					t.Fatalf("e2e.WhiteboxTest(%s/%s, ...) = %v, want nil", ing.Namespace, ing.Name, err)
				}

				// Check that SslPolicy is added to Target Proxy
				if len(gclb.TargetHTTPSProxy) == 0 {
					t.Errorf("No target https proxy found")
				}

				for _, tps := range gclb.TargetHTTPSProxy {
					if tps.GA.SslPolicy != "" {
						resourceID, err := cloud.ParseResourceURL(tps.GA.SslPolicy)
						if err != nil {
							t.Fatalf("ParseResourceURL(%q) = %v, want nil", tps.GA.SslPolicy, err)
						}

						if resourceID.Key == nil || resourceID.Key.Name != tc.configPolicyName {
							t.Errorf("Incorrect SslPolicy set for TargetHttpsProxy: %q, want %q", tps.GA.SslPolicy, tc.configPolicyName)
						}
					} else {
						if tc.configPolicyName != "" {
							t.Errorf("Incorrect SslPolicy set for TargetHttpsProxy: %q, want %q", tps.GA.SslPolicy, tc.configPolicyName)
						}
					}
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
