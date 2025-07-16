/*
Copyright 2019 The Kubernetes Authors.

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
)

func TestBasicHTTPS(t *testing.T) {
	t.Parallel()

	port80 := v1.ServiceBackendPort{Number: 80}

	for _, tc := range []struct {
		desc       string
		ingBuilder *fuzz.IngressBuilder
		hosts      []string
		certType   e2e.CertType
	}{
		{
			desc: "http(s) one path via pre-shared cert",
			ingBuilder: fuzz.NewIngressBuilder("", "ingress-1", "").
				DefaultBackend("service-1", port80).
				AddPath("test.com", "/", "service-1", port80),
			hosts:    []string{"test.com"},
			certType: e2e.GCPCert,
		},
		{
			desc: "http(s) multi-path multi-TLS",
			ingBuilder: fuzz.NewIngressBuilder("", "ingress-1", "").
				DefaultBackend("service-1", port80).
				AddPath("foo.com", "/", "service-1", port80).
				AddPath("bar.com", "/", "service-1", port80).
				AddPath("baz.com", "/", "service-1", port80),
			hosts:    []string{"foo.com", "bar.com", "baz.com"},
			certType: e2e.K8sCert,
		},
		{
			desc:  "http(s) multi-path multi-pre-shared cert",
			hosts: []string{"foo.com", "bar.com", "baz.com"},
			ingBuilder: fuzz.NewIngressBuilder("", "ingress-1", "").
				DefaultBackend("service-1", port80).
				AddPath("foo.com", "/", "service-1", port80).
				AddPath("bar.com", "/", "service-1", port80).
				AddPath("baz.com", "/", "service-1", port80),
			certType: e2e.GCPCert,
		},
	} {
		tc := tc // Capture tc as we are running this in parallel.
		Framework.RunWithSandbox(tc.desc, t, func(t *testing.T, s *e2e.Sandbox) {
			t.Parallel()

			ctx := context.Background()

			for i, h := range tc.hosts {
				name := fmt.Sprintf("cert%d--%s", i, s.Namespace)
				cert, err := e2e.NewCert(name, h, tc.certType, false)
				if err != nil {
					t.Fatalf("Error initializing cert: %v", err)
				}
				if err := cert.Create(s); err != nil {
					t.Fatalf("Error creating cert %s: %v", cert.Name, err)
				}
				defer cert.Delete(s)

				if tc.certType == e2e.K8sCert {
					tc.ingBuilder.AddTLS([]string{}, cert.Name)
				} else {
					tc.ingBuilder.AddPresharedCerts([]string{cert.Name})
				}
			}
			ing := tc.ingBuilder.Build()
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

func TestRegionalXLBHTTPS(t *testing.T) {
	t.Parallel()

	// These names are useful when reading the debug logs
	ingressPrefix := "ing2-"
	serviceName := "svc-2"

	port80 := v1.ServiceBackendPort{Number: 80}

	for _, tc := range []struct {
		desc       string
		ingBuilder *fuzz.IngressBuilder
		hosts      []string
		certType   e2e.CertType

		numForwardingRules int
		numBackendServices int
	}{
		{
			desc: "https RXLB one path, pre-shared cert",
			ingBuilder: fuzz.NewIngressBuilder("", ingressPrefix+"1", "").
				AddPath("test.com", "/", serviceName, port80).
				ConfigureForRegionalXLB(),
			numForwardingRules: 2,
			numBackendServices: 2,
			certType:           e2e.GCPCert,
			hosts:              []string{"test.com"},
		},
		{
			desc: "https RXLB one path, tls",
			ingBuilder: fuzz.NewIngressBuilder("", ingressPrefix+"2", "").
				AddPath("test.com", "/", serviceName, port80).
				ConfigureForRegionalXLB(),
			numForwardingRules: 2,
			numBackendServices: 2,
			certType:           e2e.K8sCert,
			hosts:              []string{"test.com"},
		},
		{
			desc: "https RXLB multiple paths, pre-shared cert",
			ingBuilder: fuzz.NewIngressBuilder("", ingressPrefix+"3", "").
				AddPath("test.com", "/foo", serviceName, port80).
				AddPath("baz.com", "/bar", serviceName, port80).
				ConfigureForRegionalXLB(),
			numForwardingRules: 2,
			numBackendServices: 2,
			certType:           e2e.GCPCert,
			hosts:              []string{"test.com", "baz.com"},
		},
		{
			desc: "https RXLB multiple paths, tls",
			ingBuilder: fuzz.NewIngressBuilder("", ingressPrefix+"4", "").
				AddPath("test.com", "/foo", serviceName, port80).
				AddPath("baz.com", "/bar", serviceName, port80).
				ConfigureForRegionalXLB(),
			numForwardingRules: 2,
			numBackendServices: 2,
			certType:           e2e.K8sCert,
			hosts:              []string{"test.com", "baz.com"},
		},
	} {
		tc := tc // Capture tc as we are running this in parallel.
		Framework.RunWithSandbox(tc.desc, t, func(t *testing.T, s *e2e.Sandbox) {
			t.Parallel()

			crud := adapter.IngressCRUD{C: Framework.Clientset}

			for i, h := range tc.hosts {
				name := fmt.Sprintf("cert%d--%s", i, s.Namespace)
				cert, err := e2e.NewCert(name, h, tc.certType, true)
				if err != nil {
					t.Fatalf("Error initializing cert: %v", err)
				}
				if err := cert.Create(s); err != nil {
					t.Fatalf("Error creating cert %s: %v", cert.Name, err)
				}
				defer cert.Delete(s)

				if tc.certType == e2e.K8sCert {
					tc.ingBuilder.AddTLS([]string{}, cert.Name)
				} else {
					tc.ingBuilder.AddPresharedCerts([]string{cert.Name})
				}
			}
			ing := tc.ingBuilder.Build()
			ing.Namespace = s.Namespace // namespace depends on sandbox

			t.Logf("Ingress = %s", ing.String())

			_, err := e2e.CreateEchoService(s, serviceName, negAnnotation)
			if err != nil {
				t.Fatalf("error creating echo service: %v", err)
			}
			t.Logf("Echo service created (%s/%s)", s.Namespace, serviceName)

			if _, err := crud.Create(ing); err != nil {
				t.Fatalf("error creating Ingress spec: %v", err)
			}
			t.Logf("Ingress created (%s/%s)", s.Namespace, ing.Name)

			ing, err = e2e.WaitForIngress(s, ing, nil, nil)
			if err != nil {
				t.Fatalf("error waiting for Ingress to stabilize: %v", err)
			}
			t.Logf("GCLB resources created (%s/%s)", s.Namespace, ing.Name)

			// Perform whitebox testing.
			if len(ing.Status.LoadBalancer.Ingress) < 1 {
				t.Fatalf("Ingress does not have an IP: %+v", ing.Status)
			}

			vip := ing.Status.LoadBalancer.Ingress[0].IP
			t.Logf("Ingress %s/%s VIP = %s", s.Namespace, ing.Name, vip)

			params := &fuzz.GCLBForVIPParams{VIP: vip, Region: Framework.Region, Network: Framework.Network, Validators: fuzz.FeatureValidators(features.All)}
			gclb, err := fuzz.GCLBForVIP(context.Background(), Framework.Cloud, params)
			if err != nil {
				t.Fatalf("Error getting GCP resources for LB with IP = %q: %v", vip, err)
			}

			if err = e2e.CheckGCLB(gclb, tc.numForwardingRules, tc.numBackendServices); err != nil {
				t.Error(err)
			}

			deleteOptions := &fuzz.GCLBDeleteOptions{
				SkipDefaultBackend: false,
			}
			if err := e2e.WaitForIngressDeletion(context.Background(), gclb, s, ing, deleteOptions); err != nil {
				t.Errorf("e2e.WaitForIngressDeletion(..., %q, nil) = %v, want nil", ing.Name, err)
			}
		})
	}
}
