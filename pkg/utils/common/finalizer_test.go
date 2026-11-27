/*
Copyright 2026 The Kubernetes Authors.

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

package common

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/klog/v2"
)

func newFinalizerTestService(finalizers ...string) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:  "test-ns",
			Name:       "test-svc",
			Finalizers: finalizers,
		},
	}
}

// storedFinalizers returns the finalizers of svc as stored in the fake API
// server.
func storedFinalizers(t *testing.T, kubeClient *fake.Clientset, svc *corev1.Service) []string {
	t.Helper()

	stored, err := kubeClient.CoreV1().Services(svc.Namespace).Get(context.TODO(), svc.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Services(%q).Get(%q) = %v, want nil", svc.Namespace, svc.Name, err)
	}
	return stored.Finalizers
}

func TestEnsureServiceFinalizer(t *testing.T) {
	for _, tc := range []struct {
		desc           string
		svc            *corev1.Service
		key            string
		wantFinalizers []string
		// wantPatched is whether a patch is sent, in which case the API
		// server's response is returned instead of the passed-in service.
		wantPatched bool
	}{
		{
			desc:           "adds missing finalizer",
			svc:            newFinalizerTestService(),
			key:            ILBFinalizerV2,
			wantFinalizers: []string{ILBFinalizerV2},
			wantPatched:    true,
		},
		{
			desc:           "keeps other finalizers",
			svc:            newFinalizerTestService(NetLBFinalizerV2),
			key:            ILBFinalizerV2,
			wantFinalizers: []string{NetLBFinalizerV2, ILBFinalizerV2},
			wantPatched:    true,
		},
		{
			desc:           "no-op when finalizer already present",
			svc:            newFinalizerTestService(ILBFinalizerV2),
			key:            ILBFinalizerV2,
			wantFinalizers: []string{ILBFinalizerV2},
			wantPatched:    false,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			kubeClient := fake.NewSimpleClientset(tc.svc)

			gotSvc, err := EnsureServiceFinalizer(tc.svc, tc.key, kubeClient, klog.TODO())
			if err != nil {
				t.Fatalf("EnsureServiceFinalizer(_, %q) = %v, want nil", tc.key, err)
			}

			if diff := cmp.Diff(tc.wantFinalizers, gotSvc.Finalizers); diff != "" {
				t.Errorf("EnsureServiceFinalizer(_, %q) returned finalizers mismatch (-want +got):\n%s", tc.key, diff)
			}
			if diff := cmp.Diff(tc.wantFinalizers, storedFinalizers(t, kubeClient, tc.svc)); diff != "" {
				t.Errorf("EnsureServiceFinalizer(_, %q) stored finalizers mismatch (-want +got):\n%s", tc.key, diff)
			}
			if gotPatched := gotSvc != tc.svc; gotPatched != tc.wantPatched {
				t.Errorf("EnsureServiceFinalizer(_, %q) returned a new object = %t, want %t", tc.key, gotPatched, tc.wantPatched)
			}
		})
	}
}

func TestEnsureDeleteServiceFinalizer(t *testing.T) {
	for _, tc := range []struct {
		desc           string
		svc            *corev1.Service
		key            string
		wantFinalizers []string
		// wantPatched is whether a patch is sent, in which case the API
		// server's response is returned instead of the passed-in service.
		wantPatched bool
	}{
		{
			desc:           "removes present finalizer",
			svc:            newFinalizerTestService(ILBFinalizerV2, NetLBFinalizerV2),
			key:            ILBFinalizerV2,
			wantFinalizers: []string{NetLBFinalizerV2},
			wantPatched:    true,
		},
		{
			desc:           "no-op when finalizer absent",
			svc:            newFinalizerTestService(NetLBFinalizerV2),
			key:            ILBFinalizerV2,
			wantFinalizers: []string{NetLBFinalizerV2},
			wantPatched:    false,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			kubeClient := fake.NewSimpleClientset(tc.svc)

			gotSvc, err := EnsureDeleteServiceFinalizer(tc.svc, tc.key, kubeClient, klog.TODO())
			if err != nil {
				t.Fatalf("EnsureDeleteServiceFinalizer(_, %q) = %v, want nil", tc.key, err)
			}

			if diff := cmp.Diff(tc.wantFinalizers, gotSvc.Finalizers); diff != "" {
				t.Errorf("EnsureDeleteServiceFinalizer(_, %q) returned finalizers mismatch (-want +got):\n%s", tc.key, diff)
			}
			if diff := cmp.Diff(tc.wantFinalizers, storedFinalizers(t, kubeClient, tc.svc)); diff != "" {
				t.Errorf("EnsureDeleteServiceFinalizer(_, %q) stored finalizers mismatch (-want +got):\n%s", tc.key, diff)
			}
			if gotPatched := gotSvc != tc.svc; gotPatched != tc.wantPatched {
				t.Errorf("EnsureDeleteServiceFinalizer(_, %q) returned a new object = %t, want %t", tc.key, gotPatched, tc.wantPatched)
			}
		})
	}
}

func TestEnsureServiceDeleteFinalizers(t *testing.T) {
	for _, tc := range []struct {
		desc           string
		svc            *corev1.Service
		removeKeys     []string
		wantFinalizers []string
		// wantPatched is whether a patch is sent, in which case the API
		// server's response is returned instead of the passed-in service.
		wantPatched bool
	}{
		{
			desc:           "removes multiple finalizers",
			svc:            newFinalizerTestService(NetLBFinalizerV2, NetLBFinalizerV3, ILBFinalizerV2),
			removeKeys:     []string{NetLBFinalizerV2, NetLBFinalizerV3},
			wantFinalizers: []string{ILBFinalizerV2},
			wantPatched:    true,
		},
		{
			desc:           "removes the finalizers that are present",
			svc:            newFinalizerTestService(NetLBFinalizerV3),
			removeKeys:     []string{NetLBFinalizerV2, NetLBFinalizerV3},
			wantFinalizers: nil,
			wantPatched:    true,
		},
		{
			desc:           "no-op when none of the finalizers are present",
			svc:            newFinalizerTestService(ILBFinalizerV2),
			removeKeys:     []string{NetLBFinalizerV2, NetLBFinalizerV3},
			wantFinalizers: []string{ILBFinalizerV2},
			wantPatched:    false,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			kubeClient := fake.NewSimpleClientset(tc.svc)

			gotSvc, err := EnsureServiceDeleteFinalizers(tc.svc, tc.removeKeys, kubeClient, klog.TODO())
			if err != nil {
				t.Fatalf("EnsureServiceDeleteFinalizers(_, %q) = %v, want nil", tc.removeKeys, err)
			}

			if diff := cmp.Diff(tc.wantFinalizers, gotSvc.Finalizers); diff != "" {
				t.Errorf("EnsureServiceDeleteFinalizers(_, %q) returned finalizers mismatch (-want +got):\n%s", tc.removeKeys, diff)
			}
			if diff := cmp.Diff(tc.wantFinalizers, storedFinalizers(t, kubeClient, tc.svc)); diff != "" {
				t.Errorf("EnsureServiceDeleteFinalizers(_, %q) stored finalizers mismatch (-want +got):\n%s", tc.removeKeys, diff)
			}
			if gotPatched := gotSvc != tc.svc; gotPatched != tc.wantPatched {
				t.Errorf("EnsureServiceDeleteFinalizers(_, %q) returned a new object = %t, want %t", tc.removeKeys, gotPatched, tc.wantPatched)
			}
		})
	}
}
