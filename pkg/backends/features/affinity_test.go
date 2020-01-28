/*
Copyright 2015 The Kubernetes Authors.

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

package features

import (
	"testing"

	backendconfigv1 "k8s.io/ingress-gce/pkg/apis/backendconfig/v1"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/utils"
)

var testTTL = int64(10)

func TestEnsureAffinity(t *testing.T) {
	testCases := []struct {
		desc           string
		sp             utils.ServicePort
		be             *composite.BackendService
		updateExpected bool
	}{
		{
			desc: "affinity settings missing from spec, no update needed",
			sp: utils.ServicePort{
				BackendConfig: &backendconfigv1.BackendConfig{
					Spec: backendconfigv1.BackendConfigSpec{},
				},
			},
			be: &composite.BackendService{
				SessionAffinity:      "GENERATED_COOKIE",
				AffinityCookieTtlSec: 10,
			},
			updateExpected: false,
		},
		{
			desc: "sessionaffinity setting differing, update needed",
			sp: utils.ServicePort{
				BackendConfig: &backendconfigv1.BackendConfig{
					Spec: backendconfigv1.BackendConfigSpec{
						SessionAffinity: &backendconfigv1.SessionAffinityConfig{
							AffinityType: "CLIENT_IP",
						},
					},
				},
			},
			be: &composite.BackendService{
				SessionAffinity: "NONE",
			},
			updateExpected: true,
		},
		{
			desc: "affinity ttl setting differing, update needed",
			sp: utils.ServicePort{
				BackendConfig: &backendconfigv1.BackendConfig{
					Spec: backendconfigv1.BackendConfigSpec{
						SessionAffinity: &backendconfigv1.SessionAffinityConfig{
							AffinityCookieTtlSec: &testTTL,
						},
					},
				},
			},
			be: &composite.BackendService{
				AffinityCookieTtlSec: 20,
			},
			updateExpected: true,
		},
		{
			desc: "sessionaffinity and ttl settings differing, update needed",
			sp: utils.ServicePort{
				BackendConfig: &backendconfigv1.BackendConfig{
					Spec: backendconfigv1.BackendConfigSpec{
						SessionAffinity: &backendconfigv1.SessionAffinityConfig{
							AffinityType:         "CLIENT_IP",
							AffinityCookieTtlSec: &testTTL,
						},
					},
				},
			},
			be: &composite.BackendService{
				SessionAffinity:      "NONE",
				AffinityCookieTtlSec: 20,
			},
			updateExpected: true,
		},
		{
			desc: "affinity settings identical, no updated needed",
			sp: utils.ServicePort{
				BackendConfig: &backendconfigv1.BackendConfig{
					Spec: backendconfigv1.BackendConfigSpec{
						SessionAffinity: &backendconfigv1.SessionAffinityConfig{
							AffinityType:         "CLIENT_IP",
							AffinityCookieTtlSec: &testTTL,
						},
					},
				},
			},
			be: &composite.BackendService{
				SessionAffinity:      "CLIENT_IP",
				AffinityCookieTtlSec: testTTL,
			},
			updateExpected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			result := EnsureAffinity(tc.sp, tc.be)
			if result != tc.updateExpected {
				t.Errorf("%v: expected %v but got %v", tc.desc, tc.updateExpected, result)
			}
		})
	}
}
