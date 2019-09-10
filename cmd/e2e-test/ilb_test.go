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
	"fmt"
	"k8s.io/api/networking/v1beta1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/e2e"
	"k8s.io/ingress-gce/pkg/fuzz"
	"k8s.io/ingress-gce/pkg/utils"
	"testing"
)

var negVal = annotations.NegAnnotation{
	Ingress: true,
}
var negAnnotation = map[string]string{
	annotations.NEGAnnotationKey: negVal.String()}

func TestILB(t *testing.T) {
	t.Parallel()

	port80 := intstr.FromInt(80)

	for _, tc := range []struct {
		desc string
		ing  *v1beta1.Ingress

		numForwardingRules int
		numBackendServices int
	}{
		{
			desc: "http ILB default backend",
			ing: fuzz.NewIngressBuilder("", "ingress-1", "").
				DefaultBackend("service-1", port80).
				ConfigureForILB().
				Build(),
			numForwardingRules: 1,
			numBackendServices: 1,
		},
		{
			desc: "http ILB one path",
			ing: fuzz.NewIngressBuilder("", "ingress-1", "").
				AddPath("test.com", "/", "service-1", port80).
				ConfigureForILB().
				Build(),
			numForwardingRules: 1,
			numBackendServices: 2,
		},
		{
			desc: "http ILB multiple paths",
			ing: fuzz.NewIngressBuilder("", "ingress-1", "").
				AddPath("test.com", "/foo", "service-1", port80).
				AddPath("test.com", "/bar", "service-1", port80).
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

			_, err := e2e.CreateEchoService(s, "service-1", negAnnotation)
			if err != nil {
				t.Fatalf("error creating echo service: %v", err)
			}
			t.Logf("Echo service created (%s/%s)", s.Namespace, "service-1")

			if _, err := Framework.Clientset.NetworkingV1beta1().Ingresses(s.Namespace).Create(tc.ing); err != nil {
				t.Fatalf("error creating Ingress spec: %v", err)
			}
			t.Logf("Ingress created (%s/%s)", s.Namespace, tc.ing.Name)

			ing, err := e2e.WaitForIngress(s, tc.ing, nil)
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

			// TODO(shance): update gcp.go for regional resources so that we can check GC here
		})
	}
}

func TestILBHttps(t *testing.T) {
	t.Parallel()

	port80 := intstr.FromInt(80)

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
			ingBuilder: fuzz.NewIngressBuilder("", "ingress-1", "").
				AddPath("test.com", "/", "service-1", port80).
				ConfigureForILB(),
			numForwardingRules: 1,
			numBackendServices: 2,
			certType:           e2e.GCPCert,
			hosts:              []string{"test.com"},
		},
		{
			desc: "https ILB one path, tls",
			ingBuilder: fuzz.NewIngressBuilder("", "ingress-1", "").
				AddPath("test.com", "/", "service-1", port80).
				ConfigureForILB(),
			numForwardingRules: 1,
			numBackendServices: 2,
			certType:           e2e.K8sCert,
			hosts:              []string{"test.com"},
		},
		{
			desc: "https ILB multiple paths, pre-shared cert",
			ingBuilder: fuzz.NewIngressBuilder("", "ingress-1", "").
				AddPath("test.com", "/foo", "service-1", port80).
				AddPath("baz.com", "/bar", "service-1", port80).
				ConfigureForILB(),
			numForwardingRules: 1,
			numBackendServices: 2,
			certType:           e2e.GCPCert,
			hosts:              []string{"test.com", "baz.com"},
		},
		{
			desc: "https ILB multiple paths, tls",
			ingBuilder: fuzz.NewIngressBuilder("", "ingress-1", "").
				AddPath("test.com", "/foo", "service-1", port80).
				AddPath("baz.com", "/bar", "service-1", port80).
				ConfigureForILB(),
			numForwardingRules: 1,
			numBackendServices: 2,
			certType:           e2e.K8sCert,
			hosts:              []string{"test.com", "baz.com"},
		},
	} {
		tc := tc // Capture tc as we are running this in parallel.
		Framework.RunWithSandbox(tc.desc, t, func(t *testing.T, s *e2e.Sandbox) {
			t.Parallel()

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

			_, err := e2e.CreateEchoService(s, "service-1", negAnnotation)
			if err != nil {
				t.Fatalf("error creating echo service: %v", err)
			}
			t.Logf("Echo service created (%s/%s)", s.Namespace, "service-1")

			if _, err := Framework.Clientset.NetworkingV1beta1().Ingresses(s.Namespace).Create(ing); err != nil {
				t.Fatalf("error creating Ingress spec: %v", err)
			}
			t.Logf("Ingress created (%s/%s)", s.Namespace, ing.Name)

			ing, err = e2e.WaitForIngress(s, ing, nil)
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

			// TODO(shance): update gcp.go for regional resources so that we can check GC here
		})
	}
}

// Test Updating ILB and transitioning between ILB/ELB
func TestILBUpdate(t *testing.T) {
	t.Parallel()

	port80 := intstr.FromInt(80)

	for _, tc := range []struct {
		desc      string
		ing       *v1beta1.Ingress
		ingUpdate *v1beta1.Ingress

		numForwardingRules       int
		numBackendServices       int
		numForwardingRulesUpdate int
		numBackendServicesUpdate int
	}{
		{
			desc: "http ILB default backend to one path",
			ing: fuzz.NewIngressBuilder("", "ingress-1", "").
				DefaultBackend("service-1", port80).
				ConfigureForILB().
				Build(),
			numForwardingRules: 1,
			numBackendServices: 1,
			ingUpdate: fuzz.NewIngressBuilder("", "ingress-1", "").
				AddPath("test.com", "/", "service-1", port80).
				ConfigureForILB().
				Build(),
			numForwardingRulesUpdate: 1,
			numBackendServicesUpdate: 2,
		},
		{
			desc: "http ILB one path to default backend",
			ing: fuzz.NewIngressBuilder("", "ingress-1", "").
				AddPath("test.com", "/", "service-1", port80).
				ConfigureForILB().
				Build(),
			numForwardingRules: 1,
			numBackendServices: 2,
			ingUpdate: fuzz.NewIngressBuilder("", "ingress-1", "").
				DefaultBackend("service-1", port80).
				ConfigureForILB().
				Build(),
			numForwardingRulesUpdate: 1,
			numBackendServicesUpdate: 1,
		},
		{
			desc: "http ILB default backend to ELB default backend",
			ing: fuzz.NewIngressBuilder("", "ingress-1", "").
				DefaultBackend("service-1", port80).
				ConfigureForILB().
				Build(),
			numForwardingRules: 1,
			numBackendServices: 1,
			ingUpdate: fuzz.NewIngressBuilder("", "ingress-1", "").
				DefaultBackend("service-1", port80).
				Build(),
			numForwardingRulesUpdate: 1,
			numBackendServicesUpdate: 1,
		},
		{
			desc: "ELB default backend to ILB default backend",
			ing: fuzz.NewIngressBuilder("", "ingress-1", "").
				DefaultBackend("service-1", port80).
				ConfigureForILB().
				Build(),
			numForwardingRules: 1,
			numBackendServices: 1,
			ingUpdate: fuzz.NewIngressBuilder("", "ingress-1", "").
				DefaultBackend("service-1", port80).
				Build(),
			numForwardingRulesUpdate: 1,
			numBackendServicesUpdate: 1,
		},
	} {
		tc := tc // Capture tc as we are running this in parallel.
		Framework.RunWithSandbox(tc.desc, t, func(t *testing.T, s *e2e.Sandbox) {
			t.Parallel()

			t.Logf("Ingress = %s", tc.ing.String())

			_, err := e2e.CreateEchoService(s, "service-1", negAnnotation)
			if err != nil {
				t.Fatalf("error creating echo service: %v", err)
			}
			t.Logf("Echo service created (%s/%s)", s.Namespace, "service-1")

			if _, err := Framework.Clientset.NetworkingV1beta1().Ingresses(s.Namespace).Create(tc.ing); err != nil {
				t.Fatalf("error creating Ingress spec: %v", err)
			}
			t.Logf("Ingress created (%s/%s)", s.Namespace, tc.ing.Name)

			ing, err := e2e.WaitForIngress(s, tc.ing, nil)
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

			// Perform update
			if _, err := Framework.Clientset.NetworkingV1beta1().Ingresses(s.Namespace).Update(tc.ingUpdate); err != nil {
				t.Fatalf("error updating ingress spec: %v", err)
			}

			// Verify everything works
			ing, err = e2e.WaitForIngress(s, tc.ingUpdate, nil)
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

			// TODO(shance): update gcp.go for regional resources so that we can check GC here
		})
	}
}

// TODO(shance): Add unsupported features here
func TestILBError(t *testing.T) {
	t.Parallel()

	port80 := intstr.FromInt(80)

	for _, tc := range []struct {
		desc           string
		ing            *v1beta1.Ingress
		svcAnnotations map[string]string
	}{
		{
			desc: "No neg annotation",
			ing: fuzz.NewIngressBuilder("", "ingress-1", "").
				DefaultBackend("service-1", port80).
				ConfigureForILB().
				Build(),
			svcAnnotations: map[string]string{},
		},
	} {
		tc := tc // Capture tc as we are running this in parallel.
		Framework.RunWithSandbox(tc.desc, t, func(t *testing.T, s *e2e.Sandbox) {
			t.Parallel()

			t.Logf("Ingress = %s", tc.ing.String())

			_, err := e2e.CreateEchoService(s, "service-1", tc.svcAnnotations)
			if err != nil {
				t.Fatalf("error creating echo service: %v", err)
			}
			t.Logf("Echo service created (%s/%s)", s.Namespace, "service-1")

			if _, err := Framework.Clientset.NetworkingV1beta1().Ingresses(s.Namespace).Create(tc.ing); err != nil {
				t.Fatalf("error creating Ingress spec: %v", err)
			}
			t.Logf("Ingress created (%s/%s)", s.Namespace, tc.ing.Name)

			_, err = e2e.WaitForIngress(s, tc.ing, nil)
			if err == nil {
				t.Fatalf("want err, got nil")
			}

			// TODO(shance): update gcp.go for regional resources so that we can check GC here
		})
	}
}

// Test ILB and ELB sharing same service
func TestILBShared(t *testing.T) {
	t.Parallel()

	port80 := intstr.FromInt(80)

	for _, tc := range []struct {
		desc               string
		ilbIng             *v1beta1.Ingress
		elbIng             *v1beta1.Ingress
		numForwardingRules int
		numBackendServices int
	}{
		{
			desc: "default backend",
			ilbIng: fuzz.NewIngressBuilder("", "ilb-ingress-1", "").
				DefaultBackend("service-1", port80).
				ConfigureForILB().
				Build(),
			elbIng: fuzz.NewIngressBuilder("", "elb-ingress-1", "").
				DefaultBackend("service-1", port80).
				Build(),
			numForwardingRules: 1,
			numBackendServices: 1,
		},
		{
			desc: "one path",
			ilbIng: fuzz.NewIngressBuilder("", "ilb-ingress-1", "").
				AddPath("test.com", "/", "service-1", port80).
				ConfigureForILB().
				Build(),
			elbIng: fuzz.NewIngressBuilder("", "elb-ingress-1", "").
				AddPath("test.com", "/", "service-1", port80).
				Build(),
			numForwardingRules: 1,
			numBackendServices: 2,
		},
		{
			desc: "multiple paths",
			ilbIng: fuzz.NewIngressBuilder("", "ilb-ingress-1", "").
				AddPath("test.com", "/foo", "service-1", port80).
				AddPath("test.com", "/bar", "service-1", port80).
				ConfigureForILB().
				Build(),
			elbIng: fuzz.NewIngressBuilder("", "elb-ingress-1", "").
				AddPath("test.com", "/foo", "service-1", port80).
				AddPath("test.com", "/bar", "service-1", port80).
				Build(),
			numForwardingRules: 1,
			numBackendServices: 2,
		},
	} {
		tc := tc // Capture tc as we are running this in parallel.
		Framework.RunWithSandbox(tc.desc, t, func(t *testing.T, s *e2e.Sandbox) {
			t.Parallel()

			_, err := e2e.CreateEchoService(s, "service-1", negAnnotation)
			if err != nil {
				t.Fatalf("error creating echo service: %v", err)
			}
			t.Logf("Echo service created (%s/%s)", s.Namespace, "service-1")

			for _, ing := range []*v1beta1.Ingress{tc.ilbIng, tc.elbIng} {

				t.Logf("Ingress = %s", ing.String())

				if _, err := Framework.Clientset.NetworkingV1beta1().Ingresses(s.Namespace).Create(ing); err != nil {
					t.Fatalf("error creating Ingress spec: %v", err)
				}
				t.Logf("Ingress created (%s/%s)", s.Namespace, ing.Name)

				ing, err := e2e.WaitForIngress(s, ing, nil)
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

				// TODO(shance): update gcp.go for regional resources so that we can check GC here
			}
		})
	}
}
