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
	"testing"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/networking/v1"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/e2e"
	"k8s.io/ingress-gce/pkg/e2e/adapter"
	"k8s.io/ingress-gce/pkg/fuzz"
	"k8s.io/ingress-gce/pkg/fuzz/features"
)

func TestAppProtocol(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		desc          string
		annotationVal string
	}{
		{
			desc:          "https",
			annotationVal: `{"https-port":"HTTPS"}`,
		},
		{
			desc:          "http2",
			annotationVal: `{"https-port":"HTTP2"}`,
		},
	} {
		tc := tc // Capture tc as we are running this in parallel.
		Framework.RunWithSandbox(tc.desc, t, func(t *testing.T, s *e2e.Sandbox) {
			t.Parallel()

			ctx := context.Background()

			svcAnnotation := map[string]string{
				annotations.ServiceApplicationProtocolKey: tc.annotationVal,
			}
			_, err := e2e.CreateEchoService(s, "service-1", svcAnnotation)
			if err != nil {
				t.Fatalf("Error creating echo service: %v", err)
			}
			t.Logf("Echo service created (%s/%s)", s.Namespace, "service-1")

			ing := fuzz.NewIngressBuilder(s.Namespace, "ingress-1", "").
				DefaultBackend("service-1", v1.ServiceBackendPort{Name: "https-port"}).
				AddPath("test.com", "/", "service-1", v1.ServiceBackendPort{Name: "https-port"}).
				Build()
			crud := adapter.IngressCRUD{C: Framework.Clientset}
			if _, err := crud.Create(ing); err != nil {
				t.Fatalf("error creating Ingress spec: %v", err)
			}
			t.Logf("Ingress created (%s/%s)", s.Namespace, ing.Name)

			ing, err = e2e.WaitForIngress(s, ing, nil, nil)
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

func TestAppProtocolTransition(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		desc             string
		annotationVal    string
		newAnnotationVal string
	}{
		{
			desc:             "https",
			annotationVal:    `{"https-port":"HTTPS"}`,
			newAnnotationVal: `{"https-port":"HTTP2"}`,
		},
	} {
		tc := tc // Capture tc as we are running this in parallel.
		Framework.RunWithSandbox(tc.desc, t, func(t *testing.T, s *e2e.Sandbox) {
			t.Parallel()

			ctx := context.Background()

			svcAnnotation := map[string]string{
				annotations.ServiceApplicationProtocolKey: tc.annotationVal,
			}
			_, err := e2e.EnsureEchoService(s, "service-1", svcAnnotation, corev1.ServiceTypeNodePort, 1)
			if err != nil {
				t.Fatalf("Error creating echo service: %v", err)
			}
			t.Logf("Echo service created (%s/%s)", s.Namespace, "service-1")

			ing := fuzz.NewIngressBuilder(s.Namespace, "ingress-1", "").
				DefaultBackend("service-1", v1.ServiceBackendPort{Name: "https-port"}).
				AddPath("test.com", "/", "service-1", v1.ServiceBackendPort{Name: "https-port"}).
				Build()
			crud := adapter.IngressCRUD{C: Framework.Clientset}
			if _, err := crud.Create(ing); err != nil {
				t.Fatalf("error creating Ingress spec: %v", err)
			}
			t.Logf("Ingress created (%s/%s)", s.Namespace, ing.Name)

			_, err = e2e.WaitForIngress(s, ing, nil, nil)
			if err != nil {
				t.Fatalf("Error waiting for Ingress to stabilize: %v", err)
			}
			t.Logf("GCLB resources created (%s/%s)", s.Namespace, ing.Name)

			// Update the service with the new app protocol.
			svcAnnotation = map[string]string{
				annotations.ServiceApplicationProtocolKey: tc.newAnnotationVal,
			}

			if _, err = e2e.EnsureEchoService(s, "service-1", svcAnnotation, corev1.ServiceTypeNodePort, 1); err != nil {
				t.Fatalf("Error updating echo service: %v", err)
			}

			ing, err = e2e.WaitForIngress(s, ing, nil, nil)
			if err != nil {
				t.Fatalf("error waiting for Ingress to stabilize: %v", err)
			}

			vip := ing.Status.LoadBalancer.Ingress[0].IP
			t.Logf("Ingress %s/%s VIP = %s", s.Namespace, ing.Name, vip)
			params := &fuzz.GCLBForVIPParams{VIP: vip, Validators: fuzz.FeatureValidators(features.All)}
			gclb, err := fuzz.GCLBForVIP(context.Background(), Framework.Cloud, params)
			if err != nil {
				t.Fatalf("Error getting GCP resources for LB with IP = %q: %v", vip, err)
			}

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

func TestRegionalXLBAppProtocol(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		desc          string
		annotationVal string
	}{
		{
			desc:          "https",
			annotationVal: `{"https-port":"HTTPS"}`,
		},
		{
			desc:          "http2",
			annotationVal: `{"https-port":"HTTP2"}`,
		},
	} {
		tc := tc // Capture tc as we are running this in parallel.
		Framework.RunWithSandbox(tc.desc, t, func(t *testing.T, s *e2e.Sandbox) {
			t.Parallel()

			ctx := context.Background()

			svcAnnotation := map[string]string{
				annotations.GoogleServiceApplicationProtocolKey: tc.annotationVal,
				annotations.NEGAnnotationKey:                    negVal.String(),
			}
			_, err := e2e.CreateEchoService(s, "service-1", svcAnnotation)
			if err != nil {
				t.Fatalf("Error creating echo service: %v", err)
			}
			t.Logf("Echo service created (%s/%s)", s.Namespace, "service-1")

			ing := fuzz.NewIngressBuilder(s.Namespace, "ingress-1", "").
				DefaultBackend("service-1", v1.ServiceBackendPort{Name: "https-port"}).
				AddPath("test.com", "/", "service-1", v1.ServiceBackendPort{Name: "https-port"}).
				ConfigureForRegionalXLB().
				Build()
			crud := adapter.IngressCRUD{C: Framework.Clientset}
			if _, err := crud.Create(ing); err != nil {
				t.Fatalf("error creating Ingress spec: %v", err)
			}
			t.Logf("Ingress created (%s/%s)", s.Namespace, ing.Name)

			ing, err = e2e.WaitForIngress(s, ing, nil, nil)
			if err != nil {
				t.Fatalf("error waiting for Ingress to stabilize: %v", err)
			}
			t.Logf("GCLB resources created (%s/%s)", s.Namespace, ing.Name)

			vip := ing.Status.LoadBalancer.Ingress[0].IP
			t.Logf("Ingress %s/%s VIP = %s", s.Namespace, ing.Name, vip)
			params := &fuzz.GCLBForVIPParams{VIP: vip, Validators: fuzz.FeatureValidators(features.All), Region: Framework.Region}
			gclb, err := fuzz.GCLBForVIP(context.Background(), Framework.Cloud, params)
			if err != nil {
				t.Fatalf("Error getting GCP resources for LB with IP = %q: %v", vip, err)
			}

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

func TestRegionalXLBProtocolTransition(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		desc             string
		annotationVal    string
		newAnnotationVal string
	}{
		{
			desc:             "https",
			annotationVal:    `{"https-port":"HTTPS"}`,
			newAnnotationVal: `{"https-port":"HTTP2"}`,
		},
	} {
		tc := tc // Capture tc as we are running this in parallel.
		Framework.RunWithSandbox(tc.desc, t, func(t *testing.T, s *e2e.Sandbox) {
			t.Parallel()

			ctx := context.Background()

			svcAnnotation := map[string]string{
				annotations.ServiceApplicationProtocolKey: tc.annotationVal,
				annotations.NEGAnnotationKey:              negVal.String(),
			}
			_, err := e2e.EnsureEchoService(s, "service-1", svcAnnotation, corev1.ServiceTypeNodePort, 1)
			if err != nil {
				t.Fatalf("Error creating echo service: %v", err)
			}
			t.Logf("Echo service created (%s/%s)", s.Namespace, "service-1")

			ing := fuzz.NewIngressBuilder(s.Namespace, "ingress-1", "").
				DefaultBackend("service-1", v1.ServiceBackendPort{Name: "https-port"}).
				AddPath("test.com", "/", "service-1", v1.ServiceBackendPort{Name: "https-port"}).
				ConfigureForRegionalXLB().
				Build()
			crud := adapter.IngressCRUD{C: Framework.Clientset}
			if _, err := crud.Create(ing); err != nil {
				t.Fatalf("error creating Ingress spec: %v", err)
			}
			t.Logf("Ingress created (%s/%s)", s.Namespace, ing.Name)

			_, err = e2e.WaitForIngress(s, ing, nil, nil)
			if err != nil {
				t.Fatalf("Error waiting for Ingress to stabilize: %v", err)
			}
			t.Logf("GCLB resources created (%s/%s)", s.Namespace, ing.Name)

			// Update the service with the new app protocol.
			svcAnnotation = map[string]string{
				annotations.ServiceApplicationProtocolKey: tc.newAnnotationVal,
				annotations.NEGAnnotationKey:              negVal.String(),
			}

			if _, err = e2e.EnsureEchoService(s, "service-1", svcAnnotation, corev1.ServiceTypeNodePort, 1); err != nil {
				t.Fatalf("Error updating echo service: %v", err)
			}

			ing, err = e2e.WaitForIngress(s, ing, nil, nil)
			if err != nil {
				t.Fatalf("error waiting for Ingress to stabilize: %v", err)
			}

			vip := ing.Status.LoadBalancer.Ingress[0].IP
			t.Logf("Ingress %s/%s VIP = %s", s.Namespace, ing.Name, vip)
			params := &fuzz.GCLBForVIPParams{VIP: vip, Validators: fuzz.FeatureValidators(features.All), Region: Framework.Region}
			gclb, err := fuzz.GCLBForVIP(context.Background(), Framework.Cloud, params)
			if err != nil {
				t.Fatalf("Error getting GCP resources for LB with IP = %q: %v", vip, err)
			}

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
