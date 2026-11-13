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

package recorders

import (
	"testing"

	apiv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"
	sav1 "k8s.io/ingress-gce/pkg/apis/serviceattachment/v1"
	sav1beta1 "k8s.io/ingress-gce/pkg/apis/serviceattachment/v1beta1"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/klog/v2"
)

// TestNewScheme verifies which types events can be recorded for.
func TestNewScheme(t *testing.T) {
	testCases := []struct {
		desc                   string
		enablePSC              bool
		wantServiceAttachments bool
	}{
		{
			desc:                   "PSC disabled",
			enablePSC:              false,
			wantServiceAttachments: false,
		},
		{
			desc:                   "PSC enabled",
			enablePSC:              true,
			wantServiceAttachments: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			s := newScheme(tc.enablePSC, klog.TODO())

			if _, _, err := s.ObjectKinds(&apiv1.Service{}); err != nil {
				t.Errorf("newScheme(%t).ObjectKinds(%T) returned error %v, want nil", tc.enablePSC, &apiv1.Service{}, err)
			}
			for _, obj := range []runtime.Object{&sav1beta1.ServiceAttachment{}, &sav1.ServiceAttachment{}} {
				_, _, err := s.ObjectKinds(obj)
				if registered := err == nil; registered != tc.wantServiceAttachments {
					t.Errorf("newScheme(%t).ObjectKinds(%T) returned error %v, want type registered = %t", tc.enablePSC, obj, err, tc.wantServiceAttachments)
				}
			}
		})
	}
}

// TestRecorderLeavesClientGoSchemeUnmodified verifies that creating recorders
// does not register types in scheme.Scheme, which the rest of the process
// reads without synchronization.
func TestRecorderLeavesClientGoSchemeUnmodified(t *testing.T) {
	// No t.Parallel().
	oldFlag := flags.F.EnablePSC
	flags.F.EnablePSC = true
	defer func() { flags.F.EnablePSC = oldFlag }()

	m := NewManager(fake.NewSimpleClientset(), klog.TODO())
	m.Recorder("namespace-1")
	m.Recorder("namespace-2")

	for _, obj := range []runtime.Object{&sav1beta1.ServiceAttachment{}, &sav1.ServiceAttachment{}} {
		if _, _, err := scheme.Scheme.ObjectKinds(obj); !runtime.IsNotRegisteredError(err) {
			t.Errorf("scheme.Scheme.ObjectKinds(%T) returned error %v, want a not registered error", obj, err)
		}
	}
}
