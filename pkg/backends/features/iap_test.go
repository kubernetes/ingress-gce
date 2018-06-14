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
	"crypto/sha256"
	"fmt"
	"reflect"
	"testing"

	backendconfigv1beta1 "k8s.io/ingress-gce/pkg/apis/backendconfig/v1beta1"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/utils"
)

func TestApplyIAPSettings(t *testing.T) {
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
						Iap: &backendconfigv1beta1.IAPConfig{
							Enabled: true,
							OAuthClientCredentials: &backendconfigv1beta1.OAuthClientCredentials{
								ClientID:     "foo",
								ClientSecret: "bar",
							},
						},
					},
				},
			},
			&composite.BackendService{},
			composite.BackendService{
				Iap: &composite.BackendServiceIAP{
					Enabled:            true,
					Oauth2ClientId:     "foo",
					Oauth2ClientSecret: "bar",
				},
			},
		},
		{
			"overwrite some fields on existing settings",
			utils.ServicePort{
				BackendConfig: &backendconfigv1beta1.BackendConfig{
					Spec: backendconfigv1beta1.BackendConfigSpec{
						Iap: &backendconfigv1beta1.IAPConfig{
							Enabled: true,
							OAuthClientCredentials: &backendconfigv1beta1.OAuthClientCredentials{
								ClientID:     "foo",
								ClientSecret: "baz",
							},
						},
					},
				},
			},
			&composite.BackendService{
				Iap: &composite.BackendServiceIAP{
					Enabled:            false,
					Oauth2ClientId:     "foo",
					Oauth2ClientSecret: "bar",
				},
			},
			composite.BackendService{
				Iap: &composite.BackendServiceIAP{
					Enabled:            true,
					Oauth2ClientId:     "foo",
					Oauth2ClientSecret: "baz",
				},
			},
		},
		{
			"no feature settings in spec",
			utils.ServicePort{
				BackendConfig: &backendconfigv1beta1.BackendConfig{
					Spec: backendconfigv1beta1.BackendConfigSpec{
						Iap: nil,
					},
				},
			},
			&composite.BackendService{
				Iap: &composite.BackendServiceIAP{
					Enabled:            false,
					Oauth2ClientId:     "foo",
					Oauth2ClientSecret: "bar",
				},
			},
			composite.BackendService{
				Iap: &composite.BackendServiceIAP{
					Enabled:            false,
					Oauth2ClientId:     "",
					Oauth2ClientSecret: "",
				},
			},
		},
	}

	for _, testCase := range testCases {
		applyIAPSettings(testCase.sp, testCase.be)
		if !reflect.DeepEqual(testCase.expected, *testCase.be) {
			t.Errorf("%v: expected %+v but got %+v", testCase.desc, testCase.expected, *testCase.be)
		}
	}
}

func TestEnsureIAP(t *testing.T) {
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
						Iap: &backendconfigv1beta1.IAPConfig{
							Enabled: true,
							OAuthClientCredentials: &backendconfigv1beta1.OAuthClientCredentials{
								ClientID:     "foo",
								ClientSecret: "bar",
							},
						},
					},
				},
			},
			&composite.BackendService{
				Iap: &composite.BackendServiceIAP{
					Enabled:                  true,
					Oauth2ClientId:           "foo",
					Oauth2ClientSecretSha256: fmt.Sprintf("%x", sha256.Sum256([]byte("bar"))),
				},
			},
			false,
		},
		{
			"credential settings are different, update needed",
			utils.ServicePort{
				BackendConfig: &backendconfigv1beta1.BackendConfig{
					Spec: backendconfigv1beta1.BackendConfigSpec{
						Iap: &backendconfigv1beta1.IAPConfig{
							Enabled: true,
							OAuthClientCredentials: &backendconfigv1beta1.OAuthClientCredentials{
								ClientID:     "foo",
								ClientSecret: "baz",
							},
						},
					},
				},
			},
			&composite.BackendService{
				Iap: &composite.BackendServiceIAP{
					Enabled:            true,
					Oauth2ClientId:     "foo",
					Oauth2ClientSecret: "bar",
				},
			},
			true,
		},
	}

	for _, testCase := range testCases {
		result := EnsureIAP(testCase.sp, testCase.be)
		if result != testCase.expected {
			t.Errorf("%v: expected %v but got %v", testCase.desc, testCase.expected, result)
		}
	}
}
