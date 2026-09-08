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
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
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

func TestEnsureServiceFinalizer(t *testing.T) {
	for _, tc := range []struct {
		desc           string
		svc            *corev1.Service
		key            string
		wantFinalizers []string
		wantPatch      bool
	}{
		{
			desc:           "adds missing finalizer",
			svc:            newFinalizerTestService(),
			key:            ILBFinalizerV2,
			wantFinalizers: []string{ILBFinalizerV2},
			wantPatch:      true,
		},
		{
			desc:           "keeps other finalizers",
			svc:            newFinalizerTestService(NetLBFinalizerV2),
			key:            ILBFinalizerV2,
			wantFinalizers: []string{NetLBFinalizerV2, ILBFinalizerV2},
			wantPatch:      true,
		},
		{
			desc:           "no-op when finalizer already present",
			svc:            newFinalizerTestService(ILBFinalizerV2),
			key:            ILBFinalizerV2,
			wantFinalizers: []string{ILBFinalizerV2},
			wantPatch:      false,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			kubeClient := fake.NewSimpleClientset(tc.svc)
			gotSvc, err := EnsureServiceFinalizer(tc.svc, tc.key, kubeClient, klog.TODO())
			if err != nil {
				t.Fatalf("EnsureServiceFinalizer() = %v, want nil", err)
			}
			verifyReturnedAndStoredFinalizers(t, kubeClient, tc.svc, gotSvc, tc.wantFinalizers, tc.wantPatch)
		})
	}
}

func TestEnsureDeleteServiceFinalizer(t *testing.T) {
	for _, tc := range []struct {
		desc           string
		svc            *corev1.Service
		key            string
		wantFinalizers []string
		wantPatch      bool
	}{
		{
			desc:           "removes present finalizer",
			svc:            newFinalizerTestService(ILBFinalizerV2, NetLBFinalizerV2),
			key:            ILBFinalizerV2,
			wantFinalizers: []string{NetLBFinalizerV2},
			wantPatch:      true,
		},
		{
			desc:           "no-op when finalizer absent",
			svc:            newFinalizerTestService(NetLBFinalizerV2),
			key:            ILBFinalizerV2,
			wantFinalizers: []string{NetLBFinalizerV2},
			wantPatch:      false,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			kubeClient := fake.NewSimpleClientset(tc.svc)
			gotSvc, err := EnsureDeleteServiceFinalizer(tc.svc, tc.key, kubeClient, klog.TODO())
			if err != nil {
				t.Fatalf("EnsureDeleteServiceFinalizer() = %v, want nil", err)
			}
			verifyReturnedAndStoredFinalizers(t, kubeClient, tc.svc, gotSvc, tc.wantFinalizers, tc.wantPatch)
		})
	}
}

func TestEnsureServiceDeleteFinalizers(t *testing.T) {
	for _, tc := range []struct {
		desc           string
		svc            *corev1.Service
		removeKeys     []string
		wantFinalizers []string
		wantPatch      bool
	}{
		{
			desc:           "removes multiple finalizers",
			svc:            newFinalizerTestService(NetLBFinalizerV2, NetLBFinalizerV3, ILBFinalizerV2),
			removeKeys:     []string{NetLBFinalizerV2, NetLBFinalizerV3},
			wantFinalizers: []string{ILBFinalizerV2},
			wantPatch:      true,
		},
		{
			desc:           "removes the finalizers that are present",
			svc:            newFinalizerTestService(NetLBFinalizerV3),
			removeKeys:     []string{NetLBFinalizerV2, NetLBFinalizerV3},
			wantFinalizers: nil,
			wantPatch:      true,
		},
		{
			desc:           "no-op when none of the finalizers are present",
			svc:            newFinalizerTestService(ILBFinalizerV2),
			removeKeys:     []string{NetLBFinalizerV2, NetLBFinalizerV3},
			wantFinalizers: []string{ILBFinalizerV2},
			wantPatch:      false,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			kubeClient := fake.NewSimpleClientset(tc.svc)
			gotSvc, err := EnsureServiceDeleteFinalizers(tc.svc, tc.removeKeys, kubeClient, klog.TODO())
			if err != nil {
				t.Fatalf("EnsureServiceDeleteFinalizers() = %v, want nil", err)
			}
			verifyReturnedAndStoredFinalizers(t, kubeClient, tc.svc, gotSvc, tc.wantFinalizers, tc.wantPatch)
		})
	}
}

// TestEnsureServiceFinalizerReturnsServiceOnError verifies the contract that
// the finalizer helpers never return nil: on a patch failure the passed-in
// service is returned so callers can keep using it.
func TestEnsureServiceFinalizerReturnsServiceOnError(t *testing.T) {
	svc := newFinalizerTestService(NetLBFinalizerV2)
	kubeClient := fake.NewSimpleClientset(svc)
	kubeClient.PrependReactor("patch", "services", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("patch failed")
	})

	for _, tc := range []struct {
		desc string
		run  func() (*corev1.Service, error)
	}{
		{
			desc: "EnsureServiceFinalizer",
			run: func() (*corev1.Service, error) {
				return EnsureServiceFinalizer(svc, ILBFinalizerV2, kubeClient, klog.TODO())
			},
		},
		{
			desc: "EnsureDeleteServiceFinalizer",
			run: func() (*corev1.Service, error) {
				return EnsureDeleteServiceFinalizer(svc, NetLBFinalizerV2, kubeClient, klog.TODO())
			},
		},
		{
			desc: "EnsureServiceDeleteFinalizers",
			run: func() (*corev1.Service, error) {
				return EnsureServiceDeleteFinalizers(svc, []string{NetLBFinalizerV2}, kubeClient, klog.TODO())
			},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			gotSvc, err := tc.run()
			if err == nil {
				t.Fatalf("%s() = nil, want error", tc.desc)
			}
			if gotSvc != svc {
				t.Errorf("%s() returned %v on error, want the passed-in service", tc.desc, gotSvc)
			}
		})
	}
}

func verifyReturnedAndStoredFinalizers(t *testing.T, kubeClient *fake.Clientset, origSvc, gotSvc *corev1.Service, wantFinalizers []string, wantPatch bool) {
	t.Helper()

	if gotSvc == nil {
		t.Fatal("returned service is nil, want non-nil")
	}
	if diff := cmp.Diff(wantFinalizers, gotSvc.Finalizers); diff != "" {
		t.Errorf("Returned service finalizers mismatch (-want +got):\n%s", diff)
	}

	storedSvc, err := kubeClient.CoreV1().Services(origSvc.Namespace).Get(context.TODO(), origSvc.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get() = %v, want nil", err)
	}
	if diff := cmp.Diff(wantFinalizers, storedSvc.Finalizers); diff != "" {
		t.Errorf("Stored service finalizers mismatch (-want +got):\n%s", diff)
	}

	if !wantPatch && gotSvc != origSvc {
		t.Error("returned service is a new object, want the passed-in service when no patch is needed")
	}
}
