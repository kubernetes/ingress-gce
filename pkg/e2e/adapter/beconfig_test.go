/*
Copyright 2020 The Kubernetes Authors.

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

package adapter

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1 "k8s.io/ingress-gce/pkg/apis/backendconfig/v1"
	"k8s.io/ingress-gce/pkg/apis/backendconfig/v1beta1"
	testutils "k8s.io/ingress-gce/pkg/test"
)

func TestBackendConfigAPIConversions(t *testing.T) {
	for _, tc := range []struct {
		bcV1beta1 *v1beta1.BackendConfig
		bc        *v1.BackendConfig
	}{
		{
			bcV1beta1: &v1beta1.BackendConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name: "backendconfig",
				},
				Spec: v1beta1.BackendConfigSpec{
					SessionAffinity: &v1beta1.SessionAffinityConfig{
						AffinityType: "CLIENT_IP",
					},
				},
			},
			bc: &v1.BackendConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name: "backendconfig",
				},
				Spec: v1.BackendConfigSpec{
					SessionAffinity: &v1.SessionAffinityConfig{
						AffinityType: "CLIENT_IP",
					},
				},
			},
		},
	} {
		gotBC := toV1(tc.bcV1beta1)
		gotV1beta1BC := toV1beta1(tc.bc)

		if diff := cmp.Diff(tc.bc, gotBC); diff != "" {
			t.Errorf("Got diff BackendConfig V1 (-want +got):\n%s", diff)
		}

		if diff := cmp.Diff(tc.bcV1beta1, gotV1beta1BC); diff != "" {
			t.Errorf("Got diff BackendConfig V1beta1 (-want +got):\n%s", diff)
		}
	}
}

func TestBackendConfigListConversion(t *testing.T) {
	for _, tc := range []struct {
		bclV1beta1 *v1beta1.BackendConfigList
		bcl        *v1.BackendConfigList
	}{
		{
			bclV1beta1: &v1beta1.BackendConfigList{
				Items: []v1beta1.BackendConfig{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "backendconfig1",
						},
						Spec: v1beta1.BackendConfigSpec{
							SessionAffinity: &v1beta1.SessionAffinityConfig{
								AffinityType: "CLIENT_IP",
							},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "backendconfig2",
						},
						Spec: v1beta1.BackendConfigSpec{
							Cdn: &v1beta1.CDNConfig{
								Enabled: true,
								CachePolicy: &v1beta1.CacheKeyPolicy{
									IncludeHost:        true,
									IncludeProtocol:    false,
									IncludeQueryString: true,
								},
							},
							SessionAffinity: &v1beta1.SessionAffinityConfig{
								AffinityType:         "GENERATED_COOKIE",
								AffinityCookieTtlSec: testutils.Int64ToPtr(60),
							},
						},
					},
				},
			},
			bcl: &v1.BackendConfigList{
				Items: []v1.BackendConfig{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "backendconfig1",
						},
						Spec: v1.BackendConfigSpec{
							SessionAffinity: &v1.SessionAffinityConfig{
								AffinityType: "CLIENT_IP",
							},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "backendconfig2",
						},
						Spec: v1.BackendConfigSpec{
							Cdn: &v1.CDNConfig{
								Enabled: true,
								CachePolicy: &v1.CacheKeyPolicy{
									IncludeHost:        true,
									IncludeProtocol:    false,
									IncludeQueryString: true,
								},
							},
							SessionAffinity: &v1.SessionAffinityConfig{
								AffinityType:         "GENERATED_COOKIE",
								AffinityCookieTtlSec: testutils.Int64ToPtr(60),
							},
						},
					},
				},
			},
		},
	} {
		gotBC := toV1List(tc.bclV1beta1)

		if diff := cmp.Diff(tc.bcl, gotBC); diff != "" {
			t.Errorf("Got diff BackendConfig V1 List (-want +got):\n%s", diff)
		}
	}
}
