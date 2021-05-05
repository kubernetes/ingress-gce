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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/e2e"
	"k8s.io/ingress-gce/pkg/e2e/adapter"
	"k8s.io/ingress-gce/pkg/fuzz"
	"k8s.io/ingress-gce/pkg/fuzz/features"
	"k8s.io/ingress-gce/pkg/utils"
)

var (
	negVal        = annotations.NegAnnotation{Ingress: true}
	negAnnotation = map[string]string{
		annotations.NEGAnnotationKey: negVal.String()}
)

func TestILB(t *testing.T) {
	t.Parallel()

	// These names are useful when reading the debug logs
	ingressPrefix := "ing1-"
	serviceName := "svc-1"

	port80 := v1.ServiceBackendPort{Number: 80}

	for _, tc := range []struct {
		desc string
		ing  *v1.Ingress

		numForwardingRules int
		numBackendServices int
	}{
		{
			desc: "http ILB default backend",
			ing: fuzz.NewIngressBuilder("", ingressPrefix+"1", "").
				DefaultBackend(serviceName, port80).
				ConfigureForILB().
				Build(),
			numForwardingRules: 1,
			numBackendServices: 1,
		},
		{
			desc: "http ILB one path",
			ing: fuzz.NewIngressBuilder("", ingressPrefix+"2", "").
				AddPath("test.com", "/", serviceName, port80).
				ConfigureForILB().
				Build(),
			numForwardingRules: 1,
			numBackendServices: 2,
		},
		{
			desc: "http ILB multiple paths",
			ing: fuzz.NewIngressBuilder("", ingressPrefix+"3", "").
				AddPath("test.com", "/foo", serviceName, port80).
				AddPath("test.com", "/bar", serviceName, port80).
				ConfigureForILB().
				Build(),
			numForwardingRules: 1,
			numBackendServices: 2,
		},
	} {
		tc := tc // Capture tc as we are running this in parallel.
		Framework.RunWithSandbox(tc.desc, t, func(t *testing.T, s *e2e.Sandbox) {
			t.Parallel()
			t.Logf("Ingress = %s", tc.ing.String())
			crud := adapter.IngressCRUD{C: Framework.Clientset}

			if Framework.CreateILBSubnet {
				if err := e2e.CreateILBSubnet(s); err != nil && err != e2e.ErrSubnetExists {
					t.Fatalf("e2e.CreateILBSubnet(%+v) = %v", s, err)
				}
			}

			_, err := e2e.CreateEchoService(s, serviceName, negAnnotation)
			if err != nil {
				t.Fatalf("error creating echo service: %v", err)
			}
			t.Logf("Echo service created (%s/%s)", s.Namespace, serviceName)

			tc.ing.Namespace = s.Namespace
			if _, err := crud.Create(tc.ing); err != nil {
				t.Fatalf("error creating Ingress spec: %v", err)
			}
			t.Logf("Ingress created (%s/%s)", s.Namespace, tc.ing.Name)

			ing, err := e2e.WaitForIngress(s, tc.ing, nil, nil)
			if err != nil {
				t.Fatalf("error waiting for Ingress to stabilize: %v", err)
			}
			t.Logf("GCLB resources createdd (%s/%s)", s.Namespace, tc.ing.Name)

			// Perform whitebox testing.
			if len(ing.Status.LoadBalancer.Ingress) < 1 {
				t.Fatalf("Ingress does not have an IP: %+v", ing.Status)
			}

			vip := ing.Status.LoadBalancer.Ingress[0].IP
			t.Logf("Ingress %s/%s VIP = %s", s.Namespace, tc.ing.Name, vip)
			if !e2e.IsRfc1918Addr(vip) {
				t.Fatalf("got %v, want RFC1918 address, ing: %v", vip, ing)
			}

			params := &fuzz.GCLBForVIPParams{VIP: vip, Validators: fuzz.FeatureValidators(features.All), Region: Framework.Region, Network: Framework.Network}
			gclb, err := fuzz.GCLBForVIP(context.Background(), Framework.Cloud, params)
			if err != nil {
				t.Fatalf("Error getting GCP resources for LB with IP = %q: %v", vip, err)
			}

			if err = e2e.CheckGCLB(gclb, tc.numForwardingRules, tc.numBackendServices); err != nil {
				t.Error(err)
			}

			deleteOptions := &fuzz.GCLBDeleteOptions{
				SkipDefaultBackend: true,
			}
			if err := e2e.WaitForIngressDeletion(context.Background(), gclb, s, ing, deleteOptions); err != nil {
				t.Errorf("e2e.WaitForIngressDeletion(..., %q, nil) = %v, want nil", ing.Name, err)
			}
		})
	}
}

// TestILBStaticIP is a transition test:
// 1) static IP disabled
// 2) static IP enabled
// 3) static IP disabled
func TestILBStaticIP(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	Framework.RunWithSandbox("ilb-static-ip", t, func(t *testing.T, s *e2e.Sandbox) {
		if Framework.CreateILBSubnet {
			if err := e2e.CreateILBSubnet(s); err != nil && err != e2e.ErrSubnetExists {
				t.Fatalf("e2e.CreateILBSubnet(%+v) = %v", s, err)
			}
		}

		_, err := e2e.CreateEchoService(s, "service-1", nil)
		if err != nil {
			t.Fatalf("e2e.CreateEchoService(s, service-1, nil) = _, %v; want _, nil", err)
		}

		addrName := fmt.Sprintf("test-addr-%s", s.Namespace)
		if err := e2e.NewGCPAddress(s, addrName, Framework.Region); err != nil {
			t.Fatalf("e2e.NewGCPAddress(..., %s) = %v, want nil", addrName, err)
		}
		defer e2e.DeleteGCPAddress(s, addrName, Framework.Region)

		testIngEnabled := fuzz.NewIngressBuilder(s.Namespace, "ingress-1", "").
			DefaultBackend("service-1", v1.ServiceBackendPort{Number: 80}).
			ConfigureForILB().
			AddStaticIP(addrName, true).
			Build()
		testIngDisabled := fuzz.NewIngressBuilder(s.Namespace, "ingress-1", "").
			DefaultBackend("service-1", v1.ServiceBackendPort{Number: 80}).
			ConfigureForILB().
			Build()

		// Create original ingress
		crud := adapter.IngressCRUD{C: Framework.Clientset}
		ing, err := crud.Create(testIngDisabled)
		if err != nil {
			t.Fatalf("error creating Ingress spec: %v", err)
		}
		t.Logf("Ingress %s/%s created", s.Namespace, ing.Name)

		var gclb *fuzz.GCLB
		for i, testIng := range []*v1.Ingress{testIngDisabled, testIngEnabled, testIngDisabled} {
			t.Run(fmt.Sprintf("Transition-%d", i), func(t *testing.T) {
				newIng := ing.DeepCopy()
				newIng.Spec = testIng.Spec
				ing, err = crud.Patch(ing, newIng)
				if err != nil {
					t.Fatalf("error patching Ingress spec: %v", err)
				}
				t.Logf("Ingress %s/%s updated", s.Namespace, testIng.Name)

				ing, err = e2e.WaitForIngress(s, ing, nil, nil)
				if err != nil {
					t.Fatalf("e2e.WaitForIngress(s, %q) = _, %v; want _, nil", testIng.Name, err)
				}
				if len(ing.Status.LoadBalancer.Ingress) < 1 {
					t.Fatalf("Ingress does not have an IP: %+v", ing.Status)
				}

				vip := ing.Status.LoadBalancer.Ingress[0].IP
				params := &fuzz.GCLBForVIPParams{VIP: vip, Validators: fuzz.FeatureValidators(features.All), Region: Framework.Region, Network: Framework.Network}
				gclb, err = fuzz.GCLBForVIP(context.Background(), Framework.Cloud, params)
				if err != nil {
					t.Fatalf("Error getting GCP resources for LB with IP = %q: %v", vip, err)
				}
			})
		}
		if err := e2e.WaitForIngressDeletion(ctx, gclb, s, ing, deleteOptions); err != nil {
			t.Errorf("e2e.WaitForIngressDeletion(..., %q, nil) = %v, want nil", ing.Name, err)
		}
	})
}

// TODO(shance): Remove the SetAllowHttp() calls once L7-ILB supports sharing VIPs
func TestILBHttps(t *testing.T) {
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
			desc: "https ILB one path, pre-shared cert",
			ingBuilder: fuzz.NewIngressBuilder("", ingressPrefix+"1", "").
				AddPath("test.com", "/", serviceName, port80).
				ConfigureForILB().
				SetAllowHttp(false),
			numForwardingRules: 1,
			numBackendServices: 2,
			certType:           e2e.GCPCert,
			hosts:              []string{"test.com"},
		},
		{
			desc: "https ILB one path, tls",
			ingBuilder: fuzz.NewIngressBuilder("", ingressPrefix+"2", "").
				AddPath("test.com", "/", serviceName, port80).
				ConfigureForILB().
				SetAllowHttp(false),
			numForwardingRules: 1,
			numBackendServices: 2,
			certType:           e2e.K8sCert,
			hosts:              []string{"test.com"},
		},
		{
			desc: "https ILB multiple paths, pre-shared cert",
			ingBuilder: fuzz.NewIngressBuilder("", ingressPrefix+"3", "").
				AddPath("test.com", "/foo", serviceName, port80).
				AddPath("baz.com", "/bar", serviceName, port80).
				ConfigureForILB().
				SetAllowHttp(false),
			numForwardingRules: 1,
			numBackendServices: 2,
			certType:           e2e.GCPCert,
			hosts:              []string{"test.com", "baz.com"},
		},
		{
			desc: "https ILB multiple paths, tls",
			ingBuilder: fuzz.NewIngressBuilder("", ingressPrefix+"4", "").
				AddPath("test.com", "/foo", serviceName, port80).
				AddPath("baz.com", "/bar", serviceName, port80).
				ConfigureForILB().
				SetAllowHttp(false),
			numForwardingRules: 1,
			numBackendServices: 2,
			certType:           e2e.K8sCert,
			hosts:              []string{"test.com", "baz.com"},
		},
	} {
		tc := tc // Capture tc as we are running this in parallel.
		Framework.RunWithSandbox(tc.desc, t, func(t *testing.T, s *e2e.Sandbox) {
			t.Parallel()

			crud := adapter.IngressCRUD{C: Framework.Clientset}
			if Framework.CreateILBSubnet {
				if err := e2e.CreateILBSubnet(s); err != nil && err != e2e.ErrSubnetExists {
					t.Fatalf("e2e.CreateILBSubnet(%+v) = %v", s, err)
				}
			}

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
			t.Logf("GCLB resources createdd (%s/%s)", s.Namespace, ing.Name)

			// Perform whitebox testing.
			if len(ing.Status.LoadBalancer.Ingress) < 1 {
				t.Fatalf("Ingress does not have an IP: %+v", ing.Status)
			}

			vip := ing.Status.LoadBalancer.Ingress[0].IP
			t.Logf("Ingress %s/%s VIP = %s", s.Namespace, ing.Name, vip)
			if !e2e.IsRfc1918Addr(vip) {
				t.Fatalf("got %v, want RFC1918 address, ing: %v", vip, ing)
			}

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

// Test Updating ILB and transitioning between ILB/ELB
func TestILBUpdate(t *testing.T) {
	t.Parallel()

	// These names are useful when reading the debug logs
	ingressPrefix := "ing3-"
	serviceName := "svc-3"

	port80 := v1.ServiceBackendPort{Number: 80}

	for _, tc := range []struct {
		desc      string
		ing       *v1.Ingress
		ingUpdate *v1.Ingress

		numForwardingRules       int
		numBackendServices       int
		numForwardingRulesUpdate int
		numBackendServicesUpdate int
	}{
		{
			desc: "http ILB default backend to one path",
			ing: fuzz.NewIngressBuilder("", ingressPrefix+"1", "").
				DefaultBackend(serviceName, port80).
				ConfigureForILB().
				Build(),
			numForwardingRules: 1,
			numBackendServices: 1,
			ingUpdate: fuzz.NewIngressBuilder("", ingressPrefix+"1", "").
				AddPath("test.com", "/", serviceName, port80).
				ConfigureForILB().
				Build(),
			numForwardingRulesUpdate: 1,
			numBackendServicesUpdate: 2,
		},
		{
			desc: "http ILB one path to default backend",
			ing: fuzz.NewIngressBuilder("", ingressPrefix+"2", "").
				AddPath("test.com", "/", serviceName, port80).
				ConfigureForILB().
				Build(),
			numForwardingRules: 1,
			numBackendServices: 2,
			ingUpdate: fuzz.NewIngressBuilder("", ingressPrefix+"2", "").
				DefaultBackend(serviceName, port80).
				ConfigureForILB().
				Build(),
			numForwardingRulesUpdate: 1,
			numBackendServicesUpdate: 1,
		},
		{
			desc: "http ILB default backend to ELB default backend",
			ing: fuzz.NewIngressBuilder("", ingressPrefix+"3", "").
				DefaultBackend(serviceName, port80).
				ConfigureForILB().
				Build(),
			numForwardingRules: 1,
			numBackendServices: 1,
			ingUpdate: fuzz.NewIngressBuilder("", ingressPrefix+"3", "").
				DefaultBackend(serviceName, port80).
				Build(),
			numForwardingRulesUpdate: 1,
			numBackendServicesUpdate: 1,
		},
		{
			desc: "ELB default backend to ILB default backend",
			ing: fuzz.NewIngressBuilder("", ingressPrefix+"4", "").
				DefaultBackend(serviceName, port80).
				ConfigureForILB().
				Build(),
			numForwardingRules: 1,
			numBackendServices: 1,
			ingUpdate: fuzz.NewIngressBuilder("", ingressPrefix+"4", "").
				DefaultBackend(serviceName, port80).
				Build(),
			numForwardingRulesUpdate: 1,
			numBackendServicesUpdate: 1,
		},
	} {
		tc := tc // Capture tc as we are running this in parallel.
		Framework.RunWithSandbox(tc.desc, t, func(t *testing.T, s *e2e.Sandbox) {
			t.Parallel()

			t.Logf("Ingress = %s", tc.ing.String())

			if Framework.CreateILBSubnet {
				if err := e2e.CreateILBSubnet(s); err != nil && err != e2e.ErrSubnetExists {
					t.Fatalf("e2e.CreateILBSubnet(%+v) = %v", s, err)
				}
			}

			_, err := e2e.CreateEchoService(s, serviceName, negAnnotation)
			if err != nil {
				t.Fatalf("error creating echo service: %v", err)
			}
			t.Logf("Echo service created (%s/%s)", s.Namespace, serviceName)

			crud := adapter.IngressCRUD{C: Framework.Clientset}
			tc.ing.Namespace = s.Namespace
			if _, err := crud.Create(tc.ing); err != nil {
				t.Fatalf("error creating Ingress spec: %v", err)
			}
			t.Logf("Ingress created (%s/%s)", s.Namespace, tc.ing.Name)

			ing, err := e2e.WaitForIngress(s, tc.ing, nil, nil)
			if err != nil {
				t.Fatalf("error waiting for Ingress to stabilize: %v", err)
			}
			t.Logf("GCLB resources createdd (%s/%s)", s.Namespace, tc.ing.Name)

			// Perform whitebox testing.
			if len(ing.Status.LoadBalancer.Ingress) < 1 {
				t.Fatalf("Ingress does not have an IP: %+v", ing.Status)
			}

			vip := ing.Status.LoadBalancer.Ingress[0].IP
			t.Logf("Ingress %s/%s VIP = %s", s.Namespace, tc.ing.Name, vip)

			if utils.IsGCEL7ILBIngress(ing) && !e2e.IsRfc1918Addr(vip) {
				t.Fatalf("got %v, want RFC1918 address, ing: %v", vip, ing)
			}

			params := &fuzz.GCLBForVIPParams{VIP: vip, Region: Framework.Region, Network: Framework.Network, Validators: fuzz.FeatureValidators(features.All)}
			gclb, err := fuzz.GCLBForVIP(context.Background(), Framework.Cloud, params)
			if err != nil {
				t.Fatalf("Error getting GCP resources for LB with IP = %q: %v", vip, err)
			}

			if err = e2e.CheckGCLB(gclb, tc.numForwardingRules, tc.numBackendServices); err != nil {
				t.Error(err)
			}

			tc.ingUpdate.Namespace = s.Namespace
			// Perform update
			if _, err := crud.Update(tc.ingUpdate); err != nil {
				t.Fatalf("error updating ingress spec: %v", err)
			}

			// Verify everything works
			ing, err = e2e.WaitForIngress(s, tc.ingUpdate, nil, nil)
			if err != nil {
				t.Fatalf("error waiting for Ingress to stabilize: %v", err)
			}
			t.Logf("GCLB resources createdd (%s/%s)", s.Namespace, tc.ingUpdate.Name)

			// Perform whitebox testing.
			if len(ing.Status.LoadBalancer.Ingress) < 1 {
				t.Fatalf("Ingress does not have an IP: %+v", ing.Status)
			}

			vip = ing.Status.LoadBalancer.Ingress[0].IP
			t.Logf("Ingress %s/%s VIP = %s", s.Namespace, tc.ingUpdate.Name, vip)
			if utils.IsGCEL7ILBIngress(ing) && !e2e.IsRfc1918Addr(vip) {
				t.Fatalf("got %v, want RFC1918 address, ing: %v", vip, ing)
			}

			deleteOptions := &fuzz.GCLBDeleteOptions{
				SkipDefaultBackend: true,
			}
			if err := e2e.WaitForIngressDeletion(context.Background(), gclb, s, ing, deleteOptions); err != nil {
				t.Errorf("e2e.WaitForIngressDeletion(..., %q, nil) = %v, want nil", ing.Name, err)
			}
		})
	}
}

// TODO(shance): Add unsupported features here
func TestILBError(t *testing.T) {
	t.Parallel()

	// These names are useful when reading the debug logs
	ingressPrefix := "ing4-"
	serviceName := "svc-4"

	port80 := v1.ServiceBackendPort{Number: 80}

	for _, tc := range []struct {
		desc           string
		ing            *v1.Ingress
		svcAnnotations map[string]string
	}{
		{
			desc: "No neg annotation",
			ing: fuzz.NewIngressBuilder("", ingressPrefix+"1", "").
				DefaultBackend(serviceName, port80).
				ConfigureForILB().
				Build(),
			svcAnnotations: map[string]string{},
		},
	} {
		tc := tc // Capture tc as we are running this in parallel.
		Framework.RunWithSandbox(tc.desc, t, func(t *testing.T, s *e2e.Sandbox) {
			t.Parallel()

			t.Logf("Ingress = %s", tc.ing.String())

			if Framework.CreateILBSubnet {
				if err := e2e.CreateILBSubnet(s); err != nil && err != e2e.ErrSubnetExists {
					t.Fatalf("e2e.CreateILBSubnet(%+v) = %v", s, err)
				}
			}

			_, err := e2e.CreateEchoService(s, serviceName, tc.svcAnnotations)
			if err != nil {
				t.Fatalf("error creating echo service: %v", err)
			}
			t.Logf("Echo service created (%s/%s)", s.Namespace, serviceName)

			tc.ing.Namespace = s.Namespace
			crud := adapter.IngressCRUD{C: Framework.Clientset}
			if _, err := crud.Create(tc.ing); err != nil {
				t.Fatalf("error creating Ingress spec: %v", err)
			}
			t.Logf("Ingress created (%s/%s)", s.Namespace, tc.ing.Name)

			// Retrieve service NEG annotation
			// TODO(freehan): remove this once NEG is default for all qualified cluster versions
			svc, err := Framework.Clientset.CoreV1().Services(s.Namespace).Get(context.TODO(), serviceName, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("expect err = nil, but got %v", err)
			}
			svcAnnotation := annotations.FromService(svc)
			negAnnotation, ok, err := svcAnnotation.NEGAnnotation()
			if err != nil {
				t.Fatalf("expect err = nil, but got %v", err)
			}

			// Proceed with error expectation according to NEG annotation
			// TODO(freehan): remove this once NEG is default for all qualified cluster versions
			expectError := true
			if ok && negAnnotation.Ingress {
				expectError = false
			}

			_, err = e2e.WaitForIngress(s, tc.ing, nil, nil)

			if expectError && err == nil {
				t.Fatalf("want err, got nil")
			}

			if !expectError && err != nil {
				t.Fatalf("want err nil, got err %v", err)
			}
		})
	}
}

// Test ILB and ELB sharing same service
func TestILBShared(t *testing.T) {
	t.Parallel()

	// These names are useful when reading the debug logs
	ingressPrefix := "ing5-"
	serviceName := "svc-5"

	port80 := v1.ServiceBackendPort{Number: 80}

	for _, tc := range []struct {
		desc               string
		ilbIng             *v1.Ingress
		elbIng             *v1.Ingress
		numForwardingRules int
		numBackendServices int
	}{
		{
			desc: "default backend",
			ilbIng: fuzz.NewIngressBuilder("", ingressPrefix+"i-1", "").
				DefaultBackend(serviceName, port80).
				ConfigureForILB().
				Build(),
			elbIng: fuzz.NewIngressBuilder("", ingressPrefix+"e-1", "").
				DefaultBackend(serviceName, port80).
				Build(),
			numForwardingRules: 1,
			numBackendServices: 1,
		},
		{
			desc: "one path",
			ilbIng: fuzz.NewIngressBuilder("", ingressPrefix+"i-2", "").
				AddPath("test.com", "/", serviceName, port80).
				ConfigureForILB().
				Build(),
			elbIng: fuzz.NewIngressBuilder("", ingressPrefix+"e-2", "").
				AddPath("test.com", "/", serviceName, port80).
				Build(),
			numForwardingRules: 1,
			numBackendServices: 2,
		},
		{
			desc: "multiple paths",
			ilbIng: fuzz.NewIngressBuilder("", ingressPrefix+"i-3", "").
				AddPath("test.com", "/foo", serviceName, port80).
				AddPath("test.com", "/bar", serviceName, port80).
				ConfigureForILB().
				Build(),
			elbIng: fuzz.NewIngressBuilder("", ingressPrefix+"e-3", "").
				AddPath("test.com", "/foo", serviceName, port80).
				AddPath("test.com", "/bar", serviceName, port80).
				Build(),
			numForwardingRules: 1,
			numBackendServices: 2,
		},
	} {
		tc := tc // Capture tc as we are running this in parallel.
		Framework.RunWithSandbox(tc.desc, t, func(t *testing.T, s *e2e.Sandbox) {
			t.Parallel()

			if Framework.CreateILBSubnet {
				if err := e2e.CreateILBSubnet(s); err != nil && err != e2e.ErrSubnetExists {
					t.Fatalf("e2e.CreateILBSubnet(%+v) = %v", s, err)
				}
			}

			_, err := e2e.CreateEchoService(s, serviceName, negAnnotation)
			if err != nil {
				t.Fatalf("error creating echo service: %v", err)
			}
			t.Logf("Echo service created (%s/%s)", s.Namespace, serviceName)

			var gclb *fuzz.GCLB
			for _, ing := range []*v1.Ingress{tc.ilbIng, tc.elbIng} {

				t.Logf("Ingress = %s", ing.String())

				crud := adapter.IngressCRUD{C: Framework.Clientset}
				ing.Namespace = s.Namespace
				if _, err := crud.Create(ing); err != nil {
					t.Fatalf("error creating Ingress spec: %v", err)
				}
				t.Logf("Ingress created (%s/%s)", s.Namespace, ing.Name)

				ing, err := e2e.WaitForIngress(s, ing, nil, nil)
				if err != nil {
					t.Fatalf("error waiting for Ingress to stabilize: %v", err)
				}
				t.Logf("GCLB resources createdd (%s/%s)", s.Namespace, ing.Name)

				// Perform whitebox testing.
				if len(ing.Status.LoadBalancer.Ingress) < 1 {
					t.Fatalf("Ingress does not have an IP: %+v", ing.Status)
				}

				vip := ing.Status.LoadBalancer.Ingress[0].IP
				t.Logf("Ingress %s/%s VIP = %s", s.Namespace, ing.Name, vip)
				if utils.IsGCEL7ILBIngress(ing) && !e2e.IsRfc1918Addr(vip) {
					t.Fatalf("got %v, want RFC1918 address, ing: %v", vip, ing)
				}

				params := &fuzz.GCLBForVIPParams{VIP: vip, Region: Framework.Region, Network: Framework.Network, Validators: fuzz.FeatureValidators(features.All)}
				gclb, err = fuzz.GCLBForVIP(context.Background(), Framework.Cloud, params)
				if err != nil {
					t.Fatalf("Error getting GCP resources for LB with IP = %q: %v", vip, err)
				}

				if err = e2e.CheckGCLB(gclb, tc.numForwardingRules, tc.numBackendServices); err != nil {
					t.Error(err)
				}
			}
		})
	}
}
