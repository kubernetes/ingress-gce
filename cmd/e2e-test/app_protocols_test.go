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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/e2e"
	"k8s.io/ingress-gce/pkg/fuzz"
	"k8s.io/ingress-gce/pkg/fuzz/features"
)

func TestAppProtocol(t *testing.T) {
	t.Parallel()

	ing := fuzz.NewIngressBuilder("", "ingress-1", "").
		DefaultBackend("service-1", intstr.FromString("https-port")).
		AddPath("test.com", "/", "service-1", intstr.FromString("https-port")).
		Build()

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

			if _, err := Framework.Clientset.ExtensionsV1beta1().Ingresses(s.Namespace).Create(ing); err != nil {
				t.Fatalf("error creating Ingress spec: %v", err)
			}
			t.Logf("Ingress created (%s/%s)", s.Namespace, ing.Name)

			ing, err := e2e.WaitForIngress(s, ing, nil)
			if err != nil {
				t.Fatalf("error waiting for Ingress to stabilize: %v", err)
			}
			t.Logf("GCLB resources created (%s/%s)", s.Namespace, ing.Name)

			vip := ing.Status.LoadBalancer.Ingress[0].IP
			t.Logf("Ingress %s/%s VIP = %s", s.Namespace, ing.Name, vip)
			gclb, err := fuzz.GCLBForVIP(context.Background(), Framework.Cloud, vip, fuzz.FeatureValidators(features.All))
			if err != nil {
				t.Fatalf("Error getting GCP resources for LB with IP = %q: %v", vip, err)
			}

			// Wait for GCLB resources to be deleted.
			if err := Framework.Clientset.ExtensionsV1beta1().Ingresses(s.Namespace).Delete(ing.Name, &metav1.DeleteOptions{}); err != nil {
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

	ing := fuzz.NewIngressBuilder("", "ingress-1", "").
		DefaultBackend("service-1", intstr.FromString("https-port")).
		AddPath("test.com", "/", "service-1", intstr.FromString("https-port")).
		Build()

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
			_, err := e2e.CreateEchoService(s, "service-1", svcAnnotation)
			if err != nil {
				t.Fatalf("Error creating echo service: %v", err)
			}
			t.Logf("Echo service created (%s/%s)", s.Namespace, "service-1")

			if _, err := Framework.Clientset.ExtensionsV1beta1().Ingresses(s.Namespace).Create(ing); err != nil {
				t.Fatalf("error creating Ingress spec: %v", err)
			}
			t.Logf("Ingress created (%s/%s)", s.Namespace, ing.Name)

			_, err = e2e.WaitForIngress(s, ing, nil)
			if err != nil {
				t.Fatalf("Error waiting for Ingress to stabilize: %v", err)
			}
			t.Logf("GCLB resources created (%s/%s)", s.Namespace, ing.Name)

			// Update the service with the new app protocol.
			svcAnnotation = map[string]string{
				annotations.ServiceApplicationProtocolKey: tc.newAnnotationVal,
			}

			// TODO(rramkumar): Replace this with a call to Ensure()
			if _, err = e2e.CreateEchoService(s, "service-1", svcAnnotation); err != nil {
				t.Fatalf("Error updating echo service: %v", err)
			}

			ing, err := e2e.WaitForIngress(s, ing, nil)
			if err != nil {
				t.Fatalf("error waiting for Ingress to stabilize: %v", err)
			}

			vip := ing.Status.LoadBalancer.Ingress[0].IP
			t.Logf("Ingress %s/%s VIP = %s", s.Namespace, ing.Name, vip)
			gclb, err := fuzz.GCLBForVIP(context.Background(), Framework.Cloud, vip, fuzz.FeatureValidators(features.All))
			if err != nil {
				t.Fatalf("Error getting GCP resources for LB with IP = %q: %v", vip, err)
			}

			// Wait for GCLB resources to be deleted.
			if err := Framework.Clientset.ExtensionsV1beta1().Ingresses(s.Namespace).Delete(ing.Name, &metav1.DeleteOptions{}); err != nil {
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
