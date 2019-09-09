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
	"k8s.io/api/networking/v1beta1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/e2e"
	"k8s.io/ingress-gce/pkg/fuzz"
	"testing"
)

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

			negAnnotation := annotations.NegAnnotation{Ingress: true}
			annotation := map[string]string{annotations.NEGAnnotationKey: negAnnotation.String()}

			_, err := e2e.CreateEchoService(s, "service-1", annotation)
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
