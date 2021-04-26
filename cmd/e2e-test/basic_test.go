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

	v1 "k8s.io/api/networking/v1"
	"k8s.io/ingress-gce/pkg/e2e"
	"k8s.io/ingress-gce/pkg/e2e/adapter"
	"k8s.io/ingress-gce/pkg/fuzz"
	"k8s.io/ingress-gce/pkg/fuzz/features"
	"k8s.io/ingress-gce/pkg/utils/common"
)

func TestWindows(t *testing.T) {
	testBasicOS(t, e2e.Windows)
}

func TestBasic(t *testing.T) {
	testBasicOS(t, e2e.Linux)
}

func testBasicOS(t *testing.T, os e2e.OS) {
	t.Parallel()

	port80 := v1.ServiceBackendPort{Number: 80}

	for _, tc := range []struct {
		desc string
		ing  *v1.Ingress
	}{
		{
			desc: "http default backend",
			ing: fuzz.NewIngressBuilder("", "ingress-1", "").
				DefaultBackend("service-1", port80).
				Build(),
		},
		{
			desc: "http one path",
			ing: fuzz.NewIngressBuilder("", "ingress-1", "").
				AddPath("test.com", "/", "service-1", port80).
				Build(),
		},
		{
			desc: "http multiple paths",
			ing: fuzz.NewIngressBuilder("", "ingress-1", "").
				AddPath("test.com", "/foo", "service-1", port80).
				AddPath("test.com", "/bar", "service-1", port80).
				Build(),
		},
	} {
		tc := tc // Capture tc as we are running this in parallel.
		Framework.RunWithSandbox(tc.desc, t, func(t *testing.T, s *e2e.Sandbox) {
			t.Parallel()

			ctx := context.Background()

			_, err := e2e.CreateEchoServiceWithOS(s, "service-1", nil, os)
			if err != nil {
				t.Fatalf("error creating echo service: %v", err)
			}
			t.Logf("Echo service created (%s/%s)", s.Namespace, "service-1")

			crud := adapter.IngressCRUD{C: Framework.Clientset}
			tc.ing.Namespace = s.Namespace // namespace depends on sandbox
			if _, err = crud.Create(tc.ing); err != nil {
				t.Fatalf("error creating Ingress spec: %v", err)
			}
			t.Logf("Ingress created (%s/%s)", s.Namespace, tc.ing.Name)

			ing, err := e2e.WaitForIngress(s, tc.ing, nil, nil)
			if err != nil {
				t.Fatalf("error waiting for Ingress to stabilize: %v", err)
			}
			t.Logf("GCLB resources createdd (%s/%s)", s.Namespace, tc.ing.Name)

			// Perform whitebox testing.
			gclb, err := e2e.WhiteboxTest(ing, nil, Framework.Cloud, "", s)
			if err != nil {
				t.Fatalf("e2e.WhiteboxTest(%s/%s, ...) = %v, want nil", ing.Namespace, ing.Name, err)
			}

			deleteOptions := &fuzz.GCLBDeleteOptions{
				SkipDefaultBackend: true,
			}
			if err := e2e.WaitForIngressDeletion(ctx, gclb, s, ing, deleteOptions); err != nil {
				t.Errorf("e2e.WaitForIngressDeletion(..., %q, nil) = %v, want nil", ing.Name, err)
			}
		})
	}
}

// TestBasicStaticIP tests that the static-ip annotation works as expected.
func TestBasicStaticIP(t *testing.T) {
	ctx := context.Background()
	t.Parallel()

	Framework.RunWithSandbox("static-ip", t, func(t *testing.T, s *e2e.Sandbox) {
		_, err := e2e.CreateEchoService(s, "service-1", nil)
		if err != nil {
			t.Fatalf("e2e.CreateEchoService(s, service-1, nil) = _, %v; want _, nil", err)
		}

		addrName := fmt.Sprintf("test-addr-%s", s.Namespace)
		if err := e2e.NewGCPAddress(s, addrName, ""); err != nil {
			t.Fatalf("e2e.NewGCPAddress(..., %s) = %v, want nil", addrName, err)
		}
		defer e2e.DeleteGCPAddress(s, addrName, "")

		testIng := fuzz.NewIngressBuilder(s.Namespace, "ingress-1", "").
			DefaultBackend("service-1", v1.ServiceBackendPort{Number: 80}).
			AddStaticIP(addrName, false).
			Build()
		crud := adapter.IngressCRUD{C: Framework.Clientset}
		testIng, err = crud.Create(testIng)
		if err != nil {
			t.Fatalf("error creating Ingress spec: %v", err)
		}
		t.Logf("Ingress %s/%s created", s.Namespace, testIng.Name)

		testIng, err = e2e.WaitForIngress(s, testIng, nil, nil)
		if err != nil {
			t.Fatalf("e2e.WaitForIngress(s, %q) = _, %v; want _, nil", testIng.Name, err)
		}
		if len(testIng.Status.LoadBalancer.Ingress) < 1 {
			t.Fatalf("Ingress does not have an IP: %+v", testIng.Status)
		}

		vip := testIng.Status.LoadBalancer.Ingress[0].IP
		params := &fuzz.GCLBForVIPParams{
			VIP:        vip,
			Validators: fuzz.FeatureValidators([]fuzz.Feature{features.SecurityPolicy}),
		}
		gclb, err := fuzz.GCLBForVIP(ctx, Framework.Cloud, params)
		if err != nil {
			t.Fatalf("fuzz.GCLBForVIP(..., %q, %q) = _, %v; want _, nil", vip, features.SecurityPolicy, err)
		}

		if err := e2e.WaitForIngressDeletion(ctx, gclb, s, testIng, deleteOptions); err != nil {
			t.Errorf("e2e.WaitForIngressDeletion(..., %q, nil) = %v, want nil", testIng.Name, err)
		}
	})

	// TODO(rramkumar): Add transition
}

// TestEdge exercises some basic edge cases that previously have caused bugs.
func TestEdge(t *testing.T) {
	t.Parallel()

	port80 := v1.ServiceBackendPort{Number: 80}

	for _, tc := range []struct {
		desc string
		ing  *v1.Ingress
	}{
		{
			desc: "long ingress name",
			ing: fuzz.NewIngressBuilder("", "long-ingress-name", "").
				DefaultBackend("service-1", port80).
				Build(),
		},
	} {
		tc := tc // Capture tc as we are running this in parallel.
		Framework.RunWithSandbox(tc.desc, t, func(t *testing.T, s *e2e.Sandbox) {
			t.Parallel()

			ctx := context.Background()

			_, err := e2e.CreateEchoService(s, "service-1", nil)
			if err != nil {
				t.Fatalf("error creating echo service: %v", err)
			}
			t.Logf("Echo service created (%s/%s)", s.Namespace, "service-1")
			crud := adapter.IngressCRUD{C: Framework.Clientset}
			tc.ing.Namespace = s.Namespace // namespace depends on sandbox
			if _, err = crud.Create(tc.ing); err != nil {
				t.Fatalf("error creating Ingress spec: %v", err)
			}
			t.Logf("Ingress created (%s/%s)", s.Namespace, tc.ing.Name)

			ing, err := e2e.WaitForIngress(s, tc.ing, nil, nil)
			if err != nil {
				t.Fatalf("error waiting for Ingress to stabilize: %v", err)
			}
			t.Logf("GCLB resources createdd (%s/%s)", s.Namespace, tc.ing.Name)

			// Perform whitebox testing.
			gclb, err := e2e.WhiteboxTest(ing, nil, Framework.Cloud, "", s)
			if err != nil {
				t.Fatalf("e2e.WhiteboxTest(%s/%s, ...) = %v, want nil", ing.Namespace, ing.Name, err)
			}

			deleteOptions := &fuzz.GCLBDeleteOptions{
				SkipDefaultBackend: true,
			}
			if err := e2e.WaitForIngressDeletion(ctx, gclb, s, ing, deleteOptions); err != nil {
				t.Errorf("e2e.WaitForIngressDeletion(..., %q, nil) = %v, want nil", ing.Name, err)
			}
		})
	}
}

// TestFrontendResourceDeletion asserts that unused GCP frontend resources are
// deleted. This also tests that necessary GCP frontend resources exist.
func TestFrontendResourceDeletion(t *testing.T) {
	t.Parallel()
	port80 := v1.ServiceBackendPort{Number: 80}
	svcName := "service-1"
	host := "foo.com"

	// All the test cases create an ingress with a HTTP + HTTPS load-balancer
	// at the beginning of the test and turnoff these based on the testcase params.
	for _, tc := range []struct {
		desc         string
		disableHTTP  bool
		disableHTTPS bool
	}{
		// Note that disabling both HTTP & HTTPS would result in a sync error, so excluded.
		{"http only", false, true},
		{"https only", true, false},
	} {
		tc := tc
		Framework.RunWithSandbox(tc.desc, t, func(t *testing.T, s *e2e.Sandbox) {
			t.Parallel()
			ctx := context.Background()

			_, err := e2e.CreateEchoService(s, svcName, nil)
			if err != nil {
				t.Fatalf("CreateEchoService(_, %q, nil): %v, want nil", svcName, err)
			}
			t.Logf("Echo service created (%s/%s)", s.Namespace, svcName)

			// Create SSL certificate.
			certName := fmt.Sprintf("cert-1")
			cert, err := e2e.NewCert(certName, host, e2e.K8sCert, false)
			if err != nil {
				t.Fatalf("e2e.NewCert(%q, %q, _, %t) = %v, want nil", certName, host, false, err)
			}
			if err := cert.Create(s); err != nil {
				t.Fatalf("cert.Create(_) = %v, want nil, error creating cert %s", err, cert.Name)
			}
			t.Logf("Cert created %s", certName)
			defer cert.Delete(s)

			ing := fuzz.NewIngressBuilder(s.Namespace, "ing1", "").
				AddPath(host, "/", svcName, port80).AddTLS([]string{}, cert.Name).Build()
			ingKey := common.NamespacedName(ing)

			crud := adapter.IngressCRUD{C: Framework.Clientset}
			if _, err := crud.Create(ing); err != nil {
				t.Fatalf("crud.Create(%s) = %v, want nil; Ingress: %v", ingKey, err, ing)
			}
			t.Logf("Ingress created (%s)", ingKey)
			if ing, err = e2e.WaitForIngress(s, ing, nil, &e2e.WaitForIngressOptions{ExpectUnreachable: true}); err != nil {
				t.Fatalf("error waiting for Ingress %s to stabilize: %v", ingKey, err)
			}
			gclb, err := e2e.WhiteboxTest(ing, nil, Framework.Cloud, "", s)
			if err != nil {
				t.Fatalf("e2e.WhiteboxTest(%s, ...)= %v, want nil", ingKey, err)
			}

			// Update ingress with desired frontend resource configuration.
			ingBuilder := fuzz.NewIngressBuilderFromExisting(ing)
			if tc.disableHTTP {
				ingBuilder = ingBuilder.SetAllowHttp(false)
			}
			if tc.disableHTTPS {
				ingBuilder = ingBuilder.SetTLS(nil)
			}
			newIng := ingBuilder.Build()

			if _, err := crud.Patch(ing, newIng); err != nil {
				t.Fatalf("Patch(%s) = %v, want nil; current ingress: %+v new ingress: %+v", ingKey, err, ing, newIng)
			}
			t.Logf("Ingress patched (%s)", ingKey)
			if ing, err = e2e.WaitForIngress(s, ing, nil, &e2e.WaitForIngressOptions{ExpectUnreachable: true}); err != nil {
				t.Fatalf("error waiting for Ingress %s to stabilize: %v", ingKey, err)
			}

			deleteOptions := &fuzz.GCLBDeleteOptions{
				SkipDefaultBackend:          true,
				CheckHttpFrontendResources:  tc.disableHTTP,
				CheckHttpsFrontendResources: tc.disableHTTPS,
			}
			// Wait for unused frontend resources to be deleted.
			if err := e2e.WaitForFrontendResourceDeletion(ctx, Framework.Cloud, gclb, deleteOptions); err != nil {
				t.Errorf("e2e.WaitForFrontendResourceDeletion(..., %q, _) = %v, want nil", ingKey, err)
			}
			if gclb, err = e2e.WhiteboxTest(ing, nil, Framework.Cloud, "", s); err != nil {
				t.Fatalf("e2e.WhiteboxTest(%s, ...) = %v, want nil", ingKey, err)
			}

			expectedVIP := ing.Status.LoadBalancer.Ingress[0].IP
			// Re-enable http/https and verify that the ingress uses same VIP.
			if ing, err = crud.Get(ing.Namespace, ing.Name); err != nil {
				t.Fatalf("crud.Get(%s) = %v, want nil", ingKey, err)
			}
			ingBuilder = fuzz.NewIngressBuilderFromExisting(ing)
			if tc.disableHTTP {
				ingBuilder = ingBuilder.SetAllowHttp(true)
			}
			if tc.disableHTTPS {
				ingBuilder.SetTLS([]v1.IngressTLS{
					{
						Hosts:      []string{},
						SecretName: cert.Name,
					},
				})
			}
			newIng = ingBuilder.Build()
			if _, err := crud.Patch(ing, newIng); err != nil {
				t.Fatalf("Patch(%s) = %v, want nil; current ingress: %+v new ingress %+v", ingKey, err, ing, newIng)
			}
			t.Logf("Ingress patched (%s)", ingKey)
			if ing, err = e2e.WaitForIngress(s, ing, nil, &e2e.WaitForIngressOptions{ExpectUnreachable: true}); err != nil {
				t.Fatalf("error waiting for Ingress %s to stabilize: %v", ingKey, err)
			}
			if ing, err = e2e.WaitForHTTPResourceAnnotations(s, ing); err != nil {
				t.Fatalf("error waiting for http annotations on Ingress %s: %v", ingKey, err)
			}

			gclb, err = e2e.WhiteboxTest(ing, nil, Framework.Cloud, "", s)
			if err != nil {
				t.Fatalf("e2e.WhiteboxTest(%s, ...)= %v, want nil", ingKey, err)
			}
			// Verify that ingress VIP is retained.
			gotVIP := ing.Status.LoadBalancer.Ingress[0].IP
			if expectedVIP != gotVIP {
				t.Fatalf("Two separate VIPs are created. expected %s, got %s", expectedVIP, gotVIP)
			}

			deleteOptions = &fuzz.GCLBDeleteOptions{
				SkipDefaultBackend: true,
			}
			if err := e2e.WaitForIngressDeletion(ctx, gclb, s, ing, deleteOptions); err != nil {
				t.Errorf("e2e.WaitForIngressDeletion(..., %q, _) = %v, want nil", ingKey, err)
			}
		})
	}
}
