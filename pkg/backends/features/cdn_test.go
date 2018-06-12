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
	"reflect"
	"testing"

	backendconfigv1beta1 "k8s.io/ingress-gce/pkg/apis/backendconfig/v1beta1"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/utils"
)

func TestEnsureCDN(t *testing.T) {
	testCases := []struct {
		desc     string
		sp       utils.ServicePort
		be       *composite.BackendService
		expected bool
	}{
		{
			"settings are identical, no update needed",
			utils.ServicePort{
				BackendConfig: &backendconfigv1beta1.BackendConfig{
					Spec: backendconfigv1beta1.BackendConfigSpec{
						Cdn: &backendconfigv1beta1.CDNConfig{
							Enabled: true,
							CachePolicy: &backendconfigv1beta1.CacheKeyPolicy{
								IncludeHost: true,
							},
						},
					},
				},
			},
			&composite.BackendService{
				EnableCDN: true,
				CdnPolicy: &composite.BackendServiceCdnPolicy{
					CacheKeyPolicy: &composite.CacheKeyPolicy{
						IncludeHost: true,
					},
				},
			},
			false,
		},
		{
			"cache settings are different, update needed",
			utils.ServicePort{
				BackendConfig: &backendconfigv1beta1.BackendConfig{
					Spec: backendconfigv1beta1.BackendConfigSpec{
						Cdn: &backendconfigv1beta1.CDNConfig{
							Enabled: true,
							CachePolicy: &backendconfigv1beta1.CacheKeyPolicy{
								QueryStringWhitelist: []string{"foo"},
							},
						},
					},
				},
			},
			&composite.BackendService{
				EnableCDN: true,
			},
			true,
		},
		{
			"enabled setting is different, update needed",
			utils.ServicePort{
				BackendConfig: &backendconfigv1beta1.BackendConfig{
					Spec: backendconfigv1beta1.BackendConfigSpec{
						Cdn: &backendconfigv1beta1.CDNConfig{
							Enabled: true,
						},
					},
				},
			},
			&composite.BackendService{
				EnableCDN: false,
			},
			true,
		},
	}

	for _, testCase := range testCases {
		result := EnsureCDN(testCase.sp, testCase.be)
		if result != testCase.expected {
			t.Errorf("%v: expected %v but got %v", testCase.desc, testCase.expected, result)
		}
	}
}

func TestApplyCDNSettings(t *testing.T) {
	testCases := []struct {
		desc     string
		sp       utils.ServicePort
		be       *composite.BackendService
		expected composite.BackendService
	}{
		{
			"apply settings on empty BackendService",
			utils.ServicePort{
				BackendConfig: &backendconfigv1beta1.BackendConfig{
					Spec: backendconfigv1beta1.BackendConfigSpec{
						Cdn: &backendconfigv1beta1.CDNConfig{
							Enabled: true,
							CachePolicy: &backendconfigv1beta1.CacheKeyPolicy{
								IncludeHost: true,
							},
						},
					},
				},
			},
			&composite.BackendService{},
			composite.BackendService{
				EnableCDN: true,
				CdnPolicy: &composite.BackendServiceCdnPolicy{
					CacheKeyPolicy: &composite.CacheKeyPolicy{
						IncludeHost: true,
					},
				},
			},
		},
		{
			"overwrite some fields on existing settings",
			utils.ServicePort{
				BackendConfig: &backendconfigv1beta1.BackendConfig{
					Spec: backendconfigv1beta1.BackendConfigSpec{
						Cdn: &backendconfigv1beta1.CDNConfig{
							Enabled: true,
							CachePolicy: &backendconfigv1beta1.CacheKeyPolicy{
								IncludeHost:        true,
								IncludeProtocol:    false,
								IncludeQueryString: true,
							},
						},
					},
				},
			},
			&composite.BackendService{
				EnableCDN: false,
				CdnPolicy: &composite.BackendServiceCdnPolicy{
					CacheKeyPolicy: &composite.CacheKeyPolicy{
						IncludeHost:        false,
						IncludeProtocol:    true,
						IncludeQueryString: true,
					},
				},
			},
			composite.BackendService{
				EnableCDN: true,
				CdnPolicy: &composite.BackendServiceCdnPolicy{
					CacheKeyPolicy: &composite.CacheKeyPolicy{
						IncludeHost:        true,
						IncludeProtocol:    false,
						IncludeQueryString: true,
					},
				},
			},
		},
		{
			"no feature settings in spec",
			utils.ServicePort{
				BackendConfig: &backendconfigv1beta1.BackendConfig{
					Spec: backendconfigv1beta1.BackendConfigSpec{
						Cdn: nil,
					},
				},
			},
			&composite.BackendService{
				EnableCDN: true,
				CdnPolicy: &composite.BackendServiceCdnPolicy{
					CacheKeyPolicy: &composite.CacheKeyPolicy{
						IncludeHost: true,
					},
				},
			},
			composite.BackendService{
				EnableCDN: false,
				CdnPolicy: &composite.BackendServiceCdnPolicy{
					CacheKeyPolicy: &composite.CacheKeyPolicy{
						IncludeHost: false,
					},
				},
			},
		},
	}

	for _, testCase := range testCases {
		applyCDNSettings(testCase.sp, testCase.be)
		if !reflect.DeepEqual(testCase.expected, *testCase.be) {
			t.Errorf("%v: expected %+v but got %+v", testCase.desc, testCase.expected, *testCase.be)
		}
	}
}
