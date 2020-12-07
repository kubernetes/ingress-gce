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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/ingress-gce/pkg/e2e"
	"k8s.io/ingress-gce/pkg/e2e/adapter"
	"k8s.io/ingress-gce/pkg/fuzz"
	"k8s.io/ingress-gce/pkg/utils"
)

func TestSSLPolicy(t *testing.T) {
	ctx := context.Background()
	port80 := intstr.FromInt(80)

	// Run all test cases in the same sandbox to share certs
	Framework.RunWithSandbox("sslpolicy_e2e", t, func(t *testing.T, s *e2e.Sandbox) {
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

		for _, tc := range []struct {
			desc       string
			ingBuilder *fuzz.IngressBuilder
			policy     *compute.SslPolicy
		}{
			{
				desc: "Set SslPolicy",
				ingBuilder: fuzz.NewIngressBuilder("", "ingress-1", "").
					DefaultBackend("service-1", port80).
					AddPath("test.com", "/", "service-1", port80),
				policy: &compute.SslPolicy{Name: e2e.Truncate("e2e-ssl-policy-" + s.Namespace), MinTlsVersion: "TLS_1_0", Profile: "COMPATIBLE"},
			},
		} {
			tc := tc // Capture tc as we are running this in parallel.
			t.Parallel()

			for _, cert := range certs {
				tc.ingBuilder.AddPresharedCerts([]string{cert.Name})
			}

			// Create Ssl Policy
			var policyName string
			if tc.policy != nil && tc.policy.Name != "" {
				err := Framework.Cloud.SslPolicies().Insert(ctx, meta.GlobalKey(tc.policy.Name), tc.policy)
				if err != nil {
					if !utils.IsHTTPErrorCode(err, http.StatusConflict) {
						t.Errorf("SslPolicies().Insert(%v, %v) = %v, want nil", meta.GlobalKey(tc.policy.Name), tc.policy, err)
					} else {
						t.Logf("SslPolicies().Insert(%v, %v) = %v, want nil", meta.GlobalKey(tc.policy.Name), tc.policy, err)
					}
				} else {
					t.Logf("SslPolicy %q Created", tc.policy.Name)
				}
				policyName = tc.policy.Name
			} else {
				t.Logf("Not creating sslPolicy for testcase %q", tc.desc)
			}

			// policyname will be an empty string if no policy/empty policy is provided, and won't be omitted unless nil
			builder := fuzz.NewFrontendConfigBuilder(s.Namespace, "e2e-feconfig")
			if tc.policy != nil {
				builder.SetSslPolicy(policyName)
			}
			feConfig := builder.Build()

			if _, err := Framework.FrontendConfigClient.NetworkingV1beta1().FrontendConfigs(s.Namespace).Create(context.TODO(), feConfig, metav1.CreateOptions{}); err != nil {
				t.Errorf("FrontendConfigs(%q).Create(%v) = %v, want nil", s.Namespace, feConfig, err)
			}

			ing := tc.ingBuilder.SetFrontendConfig(feConfig.Name).Build()
			ing.Namespace = s.Namespace // namespace depends on sandbox

			_, err := e2e.CreateEchoService(s, "service-1", nil)
			if err != nil {
				t.Fatalf("Error creating echo service: %v", err)
			}
			t.Logf("Echo service created (%s/%s)", s.Namespace, "service-1")

			crud := adapter.IngressCRUD{C: Framework.Clientset}
			if _, err := crud.Create(ing); err != nil {
				t.Fatalf("error creating Ingress spec: %v", err)
			}
			t.Logf("Ingress created (%s/%s)", s.Namespace, ing.Name)

			ing, err = e2e.WaitForIngress(s, ing, nil, nil)
			if err != nil {
				t.Fatalf("Error waiting for Ingress to stabilize: %v", err)
			}
			t.Logf("GCLB resources created (%s/%s)", s.Namespace, ing.Name)

			// Perform whitebox testing.
			gclb, err := e2e.WhiteboxTest(ing, nil, nil, nil, Framework.Cloud, "", s)
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

					if resourceID.Key == nil || resourceID.Key.Name != policyName {
						t.Errorf("Incorrect SslPolicy set for TargetHttpsProxy: %q, want %q", tps.GA.SslPolicy, policyName)
					}
				} else {
					if policyName != "" {
						t.Errorf("Incorrect SslPolicy set for TargetHttpsProxy: %q, want %q", tps.GA.SslPolicy, policyName)
					}
				}
			}

			deleteOptions := &fuzz.GCLBDeleteOptions{
				SkipDefaultBackend: true,
			}
			if err := e2e.WaitForIngressDeletion(ctx, gclb, s, ing, deleteOptions); err != nil {
				t.Errorf("e2e.WaitForIngressDeletion(..., %q, nil) = %v, want nil", ing.Name, err)
			}

			// Cleanup resource but do not fail if we can't
			if err := Framework.Cloud.SslPolicies().Delete(ctx, meta.GlobalKey(policyName)); err != nil {
				t.Logf("Error deleting SslPolicy: %v", err)
			}
		}
	})
}
