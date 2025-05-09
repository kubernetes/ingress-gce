/*
Copyright 2025 The Kubernetes Authors.
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
	"testing"

	"github.com/google/go-cmp/cmp"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cloud-provider-gcp/providers/gce"
)

func TestIsLegacyL4ILBService(t *testing.T) {
	t.Parallel()
	svc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "testsvc",
			Namespace:   "default",
			Annotations: map[string]string{gce.ServiceAnnotationLoadBalancerType: string(gce.LBTypeInternal)},
			Finalizers:  []string{LegacyILBFinalizer},
		},
		Spec: v1.ServiceSpec{
			Type: v1.ServiceTypeLoadBalancer,
			Ports: []v1.ServicePort{
				{Name: "testport", Port: int32(80)},
			},
		},
	}
	if !IsLegacyL4ILBService(svc) {
		t.Errorf("Expected True for Legacy service %s, got False", svc.Name)
	}

	// Remove the finalizer and ensure the check returns False.
	svc.ObjectMeta.Finalizers = nil
	if IsLegacyL4ILBService(svc) {
		t.Errorf("Expected False for Legacy service %s, got True", svc.Name)
	}
}

func TestLBBasedOnFinalizer(t *testing.T) {
	type wants struct {
		IsLegacyL4ILBService      bool
		IsSubsettingL4ILBService  bool
		HasLegacyL4ILBFinalizerV1 bool
		HasL4ILBFinalizerV2       bool

		IsRBSL4NetLB                bool
		HasLegacyL4NetLBFinalizerV1 bool
		HasL4NetLBFinalizerV2       bool
		HasL4NetLBFinalizerV3       bool
	}

	testCases := []struct {
		finalizer string
		want      wants
	}{
		{
			finalizer: LegacyILBFinalizer,
			want: wants{
				IsLegacyL4ILBService:      true,
				HasLegacyL4ILBFinalizerV1: true,
			},
		},
		{
			finalizer: ILBFinalizerV2,
			want: wants{
				IsSubsettingL4ILBService: true,
				HasL4ILBFinalizerV2:      true,
			},
		},
		{
			finalizer: LegacyNetLBFinalizerV1,
			want: wants{
				HasLegacyL4NetLBFinalizerV1: true,
			},
		},
		{
			finalizer: NetLBFinalizerV2,
			want: wants{
				IsRBSL4NetLB:          true,
				HasL4NetLBFinalizerV2: true,
			},
		},
		{
			finalizer: NetLBFinalizerV3,
			want: wants{
				IsRBSL4NetLB:          true,
				HasL4NetLBFinalizerV3: true,
			},
		},
	}
	for _, tC := range testCases {
		t.Run(tC.finalizer, func(t *testing.T) {
			svc := &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Finalizers: []string{tC.finalizer},
				},
			}

			got := wants{
				IsLegacyL4ILBService:      IsLegacyL4ILBService(svc),
				IsSubsettingL4ILBService:  IsSubsettingL4ILBService(svc),
				HasLegacyL4ILBFinalizerV1: HasLegacyL4ILBFinalizerV1(svc),
				HasL4ILBFinalizerV2:       HasL4ILBFinalizerV2(svc),

				IsRBSL4NetLB:                IsRBSL4NetLB(svc),
				HasLegacyL4NetLBFinalizerV1: HasLegacyL4NetLBFinalizerV1(svc),
				HasL4NetLBFinalizerV2:       HasL4NetLBFinalizerV2(svc),
				HasL4NetLBFinalizerV3:       HasL4NetLBFinalizerV3(svc),
			}

			if diff := cmp.Diff(tC.want, got); diff != "" {
				t.Errorf("got != want, diff(-tc.want +got) = %s", diff)
			}
		})
	}
}
