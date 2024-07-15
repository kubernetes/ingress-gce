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
	"testing"

	"k8s.io/klog/v2"

	"github.com/google/go-cmp/cmp"
	backendconfigv1 "k8s.io/ingress-gce/pkg/apis/backendconfig/v1"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/utils"
)

func TestEnsureIAP(t *testing.T) {
	testCases := []struct {
		desc           string
		sp             utils.ServicePort
		be             *composite.BackendService
		updateExpected bool
		wantBE         *composite.BackendService
		expectErr      bool
	}{
		{
			desc: "iap settings are missing from spec, no update needed",
			sp: utils.ServicePort{
				BackendConfig: &backendconfigv1.BackendConfig{
					Spec: backendconfigv1.BackendConfigSpec{
						Iap: nil,
					},
				},
			},
			be: &composite.BackendService{
				Iap: &composite.BackendServiceIAP{
					Enabled:                  true,
					Oauth2ClientId:           "foo",
					Oauth2ClientSecretSha256: fmt.Sprintf("%x", sha256.Sum256([]byte("bar"))),
				},
			},
			wantBE: &composite.BackendService{
				Iap: &composite.BackendServiceIAP{
					Enabled:                  true,
					Oauth2ClientId:           "foo",
					Oauth2ClientSecretSha256: fmt.Sprintf("%x", sha256.Sum256([]byte("bar"))),
				},
			},
			updateExpected: false,
		},
		{
			desc: "settings are identical, no update needed",
			sp: utils.ServicePort{
				BackendConfig: &backendconfigv1.BackendConfig{
					Spec: backendconfigv1.BackendConfigSpec{
						Iap: &backendconfigv1.IAPConfig{
							Enabled: true,
							OAuthClientCredentials: &backendconfigv1.OAuthClientCredentials{
								ClientID:     "foo",
								ClientSecret: "bar",
							},
						},
					},
				},
			},
			be: &composite.BackendService{
				Iap: &composite.BackendServiceIAP{
					Enabled:                  true,
					Oauth2ClientId:           "foo",
					Oauth2ClientSecretSha256: fmt.Sprintf("%x", sha256.Sum256([]byte("bar"))),
				},
			},
			wantBE: &composite.BackendService{
				Iap: &composite.BackendServiceIAP{
					Enabled:                  true,
					Oauth2ClientId:           "foo",
					Oauth2ClientSecretSha256: fmt.Sprintf("%x", sha256.Sum256([]byte("bar"))),
				},
			},
			updateExpected: false,
		},
		{
			desc: "no existing settings, update needed",
			sp: utils.ServicePort{
				BackendConfig: &backendconfigv1.BackendConfig{
					Spec: backendconfigv1.BackendConfigSpec{
						Iap: &backendconfigv1.IAPConfig{
							Enabled: true,
							OAuthClientCredentials: &backendconfigv1.OAuthClientCredentials{
								ClientID:     "foo",
								ClientSecret: "baz",
							},
						},
					},
				},
			},
			be: &composite.BackendService{
				Iap: nil,
			},
			wantBE: &composite.BackendService{
				Iap: &composite.BackendServiceIAP{
					Enabled:            true,
					Oauth2ClientId:     "foo",
					Oauth2ClientSecret: "baz",
				},
			},
			updateExpected: true,
		},
		{
			desc: "no existing settings, update needed (no credentials provided)",
			sp: utils.ServicePort{
				BackendConfig: &backendconfigv1.BackendConfig{
					Spec: backendconfigv1.BackendConfigSpec{
						Iap: &backendconfigv1.IAPConfig{
							Enabled: true,
						},
					},
				},
			},
			be: &composite.BackendService{
				Iap: nil,
			},
			wantBE: &composite.BackendService{
				Iap: &composite.BackendServiceIAP{
					Enabled: true,
				},
			},
			updateExpected: true,
		},
		{
			desc: "client id is different, update needed",
			sp: utils.ServicePort{
				BackendConfig: &backendconfigv1.BackendConfig{
					Spec: backendconfigv1.BackendConfigSpec{
						Iap: &backendconfigv1.IAPConfig{
							Enabled: true,
							OAuthClientCredentials: &backendconfigv1.OAuthClientCredentials{
								ClientID:     "foo",
								ClientSecret: "baz",
							},
						},
					},
				},
			},
			be: &composite.BackendService{
				Iap: &composite.BackendServiceIAP{
					Enabled:                  true,
					Oauth2ClientId:           "bar",
					Oauth2ClientSecretSha256: fmt.Sprintf("%x", sha256.Sum256([]byte("baz"))),
				},
			},
			wantBE: &composite.BackendService{
				Iap: &composite.BackendServiceIAP{
					Enabled:            true,
					Oauth2ClientId:     "foo",
					Oauth2ClientSecret: "baz",
				},
			},
			updateExpected: true,
		},
		{
			desc: "client secret is different, update needed",
			sp: utils.ServicePort{
				BackendConfig: &backendconfigv1.BackendConfig{
					Spec: backendconfigv1.BackendConfigSpec{
						Iap: &backendconfigv1.IAPConfig{
							Enabled: true,
							OAuthClientCredentials: &backendconfigv1.OAuthClientCredentials{
								ClientID:     "foo",
								ClientSecret: "baz",
							},
						},
					},
				},
			},
			be: &composite.BackendService{
				Iap: &composite.BackendServiceIAP{
					Enabled:                  true,
					Oauth2ClientId:           "foo",
					Oauth2ClientSecretSha256: fmt.Sprintf("%x", sha256.Sum256([]byte("bar"))),
				},
			},
			wantBE: &composite.BackendService{
				Iap: &composite.BackendServiceIAP{
					Enabled:            true,
					Oauth2ClientId:     "foo",
					Oauth2ClientSecret: "baz",
				},
			},
			updateExpected: true,
		},
		{
			desc: "enabled setting is different, update needed",
			sp: utils.ServicePort{
				BackendConfig: &backendconfigv1.BackendConfig{
					Spec: backendconfigv1.BackendConfigSpec{
						Iap: &backendconfigv1.IAPConfig{
							Enabled: false,
							OAuthClientCredentials: &backendconfigv1.OAuthClientCredentials{
								ClientID:     "foo",
								ClientSecret: "baz",
							},
						},
					},
				},
			},
			be: &composite.BackendService{
				Iap: &composite.BackendServiceIAP{
					Enabled:                  true,
					Oauth2ClientId:           "foo",
					Oauth2ClientSecretSha256: fmt.Sprintf("%x", sha256.Sum256([]byte("baz"))),
				},
			},
			wantBE: &composite.BackendService{
				Iap: &composite.BackendServiceIAP{
					Enabled:            false,
					Oauth2ClientId:     "foo",
					Oauth2ClientSecret: "baz",
				},
			},
			updateExpected: true,
		},
		{
			desc: "credentials removed, update will error",
			sp: utils.ServicePort{
				BackendConfig: &backendconfigv1.BackendConfig{
					Spec: backendconfigv1.BackendConfigSpec{
						Iap: &backendconfigv1.IAPConfig{
							Enabled: true,
						},
					},
				},
			},
			be: &composite.BackendService{
				Iap: &composite.BackendServiceIAP{
					Enabled:                  true,
					Oauth2ClientId:           "foo",
					Oauth2ClientSecretSha256: fmt.Sprintf("%x", sha256.Sum256([]byte("baz"))),
				},
			},
			wantBE: &composite.BackendService{
				Iap: &composite.BackendServiceIAP{
					Enabled:                  true,
					Oauth2ClientId:           "foo",
					Oauth2ClientSecretSha256: fmt.Sprintf("%x", sha256.Sum256([]byte("baz"))),
				},
			},
			updateExpected: false,
			expectErr:      true,
		},
		{
			desc: "enabled is changed to false, update needed",
			sp: utils.ServicePort{
				BackendConfig: &backendconfigv1.BackendConfig{
					Spec: backendconfigv1.BackendConfigSpec{
						Iap: &backendconfigv1.IAPConfig{
							Enabled: false,
						},
					},
				},
			},
			be: &composite.BackendService{
				Iap: &composite.BackendServiceIAP{
					Enabled:                  true,
					Oauth2ClientId:           "foo",
					Oauth2ClientSecretSha256: fmt.Sprintf("%x", sha256.Sum256([]byte("baz"))),
				},
			},
			wantBE: &composite.BackendService{
				Iap: &composite.BackendServiceIAP{
					Enabled:                  false,
					Oauth2ClientId:           "",
					Oauth2ClientSecretSha256: "",
				},
			},
			updateExpected: true,
			expectErr:      false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			result, err := EnsureIAP(tc.sp, tc.be, klog.TODO())
			if tc.expectErr && err == nil {
				t.Errorf("EnsureIAP: got nil but expected an error")
			} else if !tc.expectErr && err != nil {
				t.Errorf("EnsureIAP: got err: %+v but expected none", err)
			}

			if result != tc.updateExpected {
				t.Errorf("EnsureIAP: got %v but expected %v", result, tc.updateExpected)
			}

			diff := cmp.Diff(tc.be, tc.wantBE)
			if diff != "" {
				t.Errorf("EnsureIAP: unexpected backend service (-got, +want):\n%s", diff)
			}
		})
	}
}
