/*
Copyright 2018 The Kubernetes Authors.

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

package backendconfig

import (
	"context"
	"testing"

	v1 "k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	backendconfigv1 "k8s.io/ingress-gce/pkg/apis/backendconfig/v1"
	testutils "k8s.io/ingress-gce/pkg/test"
)

var (
	goodTTL int64 = 86400
	badTTL  int64 = 86400 + 1

	defaultBeConfig = &backendconfigv1.BackendConfig{
		ObjectMeta: meta_v1.ObjectMeta{
			Namespace: "default",
		},
		Spec: backendconfigv1.BackendConfigSpec{
			Iap: &backendconfigv1.IAPConfig{
				Enabled: true,
				OAuthClientCredentials: &backendconfigv1.OAuthClientCredentials{
					SecretName: "foo",
				},
			},
		},
	}

	defaultBeConfigForCdn = &backendconfigv1.BackendConfig{
		ObjectMeta: meta_v1.ObjectMeta{
			Namespace: "default",
		},
		Spec: backendconfigv1.BackendConfigSpec{
			Cdn: &backendconfigv1.CDNConfig{
				Enabled: true,
				SignedUrlKeys: []*backendconfigv1.SignedUrlKey{
					{KeyName: "key", SecretName: "foo"},
				},
			},
		},
	}
)

func TestValidateIAP(t *testing.T) {
	testCases := []struct {
		desc        string
		init        func(kubeClient kubernetes.Interface)
		beConfig    *backendconfigv1.BackendConfig
		expectError bool
	}{
		{
			desc:     "secret does not exist",
			beConfig: defaultBeConfig,
			init: func(kubeClient kubernetes.Interface) {
				secret := &v1.Secret{
					ObjectMeta: meta_v1.ObjectMeta{
						Namespace: "wrong-namespace",
						Name:      "foo",
					},
				}
				kubeClient.CoreV1().Secrets("wrong-namespace").Create(context.TODO(), secret, meta_v1.CreateOptions{})
			},
			expectError: true,
		},
		{
			desc:     "secret does not contain client_id",
			beConfig: defaultBeConfig,
			init: func(kubeClient kubernetes.Interface) {
				secret := &v1.Secret{
					ObjectMeta: meta_v1.ObjectMeta{
						Namespace: "default",
						Name:      "foo",
					},
					Data: map[string][]byte{
						"client_secret": []byte("my-secret"),
					},
				}
				kubeClient.CoreV1().Secrets("default").Create(context.TODO(), secret, meta_v1.CreateOptions{})
			},
			expectError: true,
		},
		{
			desc:     "secret does not contain client_secret",
			beConfig: defaultBeConfig,
			init: func(kubeClient kubernetes.Interface) {
				secret := &v1.Secret{
					ObjectMeta: meta_v1.ObjectMeta{
						Namespace: "default",
						Name:      "foo",
					},
					Data: map[string][]byte{
						"client_id": []byte("my-id"),
					},
				}
				kubeClient.CoreV1().Secrets("default").Create(context.TODO(), secret, meta_v1.CreateOptions{})
			},
			expectError: true,
		},
		{
			desc:     "validation passes",
			beConfig: defaultBeConfig,
			init: func(kubeClient kubernetes.Interface) {
				secret := &v1.Secret{
					ObjectMeta: meta_v1.ObjectMeta{
						Namespace: "default",
						Name:      "foo",
					},
					Data: map[string][]byte{
						"client_id":     []byte("my-id"),
						"client_secret": []byte("my-secret"),
					},
				}
				kubeClient.CoreV1().Secrets("default").Create(context.TODO(), secret, meta_v1.CreateOptions{})
			},
			expectError: false,
		},
		{
			desc: "iap and cdn enabled at the same time",
			beConfig: &backendconfigv1.BackendConfig{
				ObjectMeta: meta_v1.ObjectMeta{
					Namespace: "default",
				},
				Spec: backendconfigv1.BackendConfigSpec{
					Iap: &backendconfigv1.IAPConfig{
						Enabled: true,
					},
					Cdn: &backendconfigv1.CDNConfig{
						Enabled: true,
					},
				},
			},
			init: func(kubeClient kubernetes.Interface) {
				secret := &v1.Secret{
					ObjectMeta: meta_v1.ObjectMeta{
						Namespace: "default",
						Name:      "foo",
					},
					Data: map[string][]byte{
						"client_id":     []byte("my-id"),
						"client_secret": []byte("my-secret"),
					},
				}
				kubeClient.CoreV1().Secrets("default").Create(context.TODO(), secret, meta_v1.CreateOptions{})
			},
			expectError: true,
		},
	}

	for _, testCase := range testCases {
		kubeClient := fake.NewSimpleClientset()
		testCase.init(kubeClient)
		err := Validate(kubeClient, testCase.beConfig)
		if testCase.expectError && err == nil {
			t.Errorf("%v: Expected error but got nil", testCase.desc)
		}
		if !testCase.expectError && err != nil {
			t.Errorf("%v: Did not expect error but got: %v", testCase.desc, err)
		}
	}
}

func TestValidateSessionAffinity(t *testing.T) {
	testCases := []struct {
		desc        string
		beConfig    *backendconfigv1.BackendConfig
		expectError bool
	}{

		{
			desc: "unsupported affinity type",
			beConfig: &backendconfigv1.BackendConfig{
				ObjectMeta: meta_v1.ObjectMeta{
					Namespace: "default",
				},
				Spec: backendconfigv1.BackendConfigSpec{
					SessionAffinity: &backendconfigv1.SessionAffinityConfig{
						AffinityType: "WRONG_TYPE",
					},
				},
			},
			expectError: true,
		},
		{
			desc: "supported affinity type",
			beConfig: &backendconfigv1.BackendConfig{
				ObjectMeta: meta_v1.ObjectMeta{
					Namespace: "default",
				},
				Spec: backendconfigv1.BackendConfigSpec{
					SessionAffinity: &backendconfigv1.SessionAffinityConfig{
						AffinityType: "CLIENT_IP",
					},
				},
			},
			expectError: false,
		},
		{
			desc: "unsupported ttl value",
			beConfig: &backendconfigv1.BackendConfig{
				ObjectMeta: meta_v1.ObjectMeta{
					Namespace: "default",
				},
				Spec: backendconfigv1.BackendConfigSpec{
					SessionAffinity: &backendconfigv1.SessionAffinityConfig{
						AffinityCookieTtlSec: &badTTL,
					},
				},
			},
			expectError: true,
		},
		{
			desc: "supported ttl value",
			beConfig: &backendconfigv1.BackendConfig{
				ObjectMeta: meta_v1.ObjectMeta{
					Namespace: "default",
				},
				Spec: backendconfigv1.BackendConfigSpec{
					SessionAffinity: &backendconfigv1.SessionAffinityConfig{
						AffinityCookieTtlSec: &goodTTL,
					},
				},
			},
			expectError: false,
		},
	}

	for _, testCase := range testCases {
		kubeClient := fake.NewSimpleClientset()
		err := Validate(kubeClient, testCase.beConfig)
		if testCase.expectError && err == nil {
			t.Errorf("%v: Expected error but got nil", testCase.desc)
		}
		if !testCase.expectError && err != nil {
			t.Errorf("%v: Did not expect error but got: %v", testCase.desc, err)
		}
	}
}

func TestValidateLogging(t *testing.T) {
	for _, tc := range []struct {
		desc        string
		beConfig    *backendconfigv1.BackendConfig
		expectError bool
	}{
		{
			desc: "nil access log config",
			beConfig: &backendconfigv1.BackendConfig{
				ObjectMeta: meta_v1.ObjectMeta{
					Namespace: "default",
				},
				Spec: backendconfigv1.BackendConfigSpec{},
			},
			expectError: false,
		},
		{
			desc: "empty access log config",
			beConfig: &backendconfigv1.BackendConfig{
				ObjectMeta: meta_v1.ObjectMeta{
					Namespace: "default",
				},
				Spec: backendconfigv1.BackendConfigSpec{
					Logging: &backendconfigv1.LogConfig{},
				},
			},
			expectError: false,
		},
		{
			desc: "invalid sample rate",
			beConfig: &backendconfigv1.BackendConfig{
				ObjectMeta: meta_v1.ObjectMeta{
					Namespace: "default",
				},
				Spec: backendconfigv1.BackendConfigSpec{
					Logging: &backendconfigv1.LogConfig{
						Enable:     false,
						SampleRate: testutils.Float64ToPtr(1.01),
					},
				},
			},
			expectError: true,
		},
		{
			desc: "valid sample rate",
			beConfig: &backendconfigv1.BackendConfig{
				ObjectMeta: meta_v1.ObjectMeta{
					Namespace: "default",
				},
				Spec: backendconfigv1.BackendConfigSpec{
					Logging: &backendconfigv1.LogConfig{
						Enable:     true,
						SampleRate: testutils.Float64ToPtr(0.5),
					},
				},
			},
			expectError: false,
		},
		{
			desc: "valid integer sample rate",
			beConfig: &backendconfigv1.BackendConfig{
				ObjectMeta: meta_v1.ObjectMeta{
					Namespace: "default",
				},
				Spec: backendconfigv1.BackendConfigSpec{
					Logging: &backendconfigv1.LogConfig{
						Enable:     true,
						SampleRate: testutils.Float64ToPtr(1),
					},
				},
			},
			expectError: false,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			kubeClient := fake.NewSimpleClientset()
			err := Validate(kubeClient, tc.beConfig)
			if tc.expectError && err == nil {
				t.Errorf("Expected error but got nil")
			}
			if !tc.expectError && err != nil {
				t.Errorf("Did not expect error but got: %v", err)
			}
		})
	}
}

func TestValidateCDN(t *testing.T) {
	testCases := []struct {
		desc        string
		init        func(kubeClient kubernetes.Interface)
		beConfig    *backendconfigv1.BackendConfig
		expectError bool
	}{
		{
			desc:     "secret does not exist",
			beConfig: defaultBeConfigForCdn,
			init: func(kubeClient kubernetes.Interface) {
				secret := &v1.Secret{
					ObjectMeta: meta_v1.ObjectMeta{
						Namespace: "wrong-namespace",
						Name:      "foo",
					},
				}
				kubeClient.CoreV1().Secrets("wrong-namespace").Create(context.TODO(), secret, meta_v1.CreateOptions{})
			},
			expectError: true,
		},
		{
			desc:     "secret does not contain key_value",
			beConfig: defaultBeConfigForCdn,
			init: func(kubeClient kubernetes.Interface) {
				secret := &v1.Secret{
					ObjectMeta: meta_v1.ObjectMeta{
						Namespace: "default",
						Name:      "foo",
					},
					Data: map[string][]byte{
						"any_key_value": []byte("my-secret"),
					},
				}
				kubeClient.CoreV1().Secrets("default").Create(context.TODO(), secret, meta_v1.CreateOptions{})
			},
			expectError: true,
		},
		{
			desc:     "validation passes",
			beConfig: defaultBeConfigForCdn,
			init: func(kubeClient kubernetes.Interface) {
				secret := &v1.Secret{
					ObjectMeta: meta_v1.ObjectMeta{
						Namespace: "default",
						Name:      "foo",
					},
					Data: map[string][]byte{
						"key_value": []byte("my-secret"),
					},
				}
				kubeClient.CoreV1().Secrets("default").Create(context.TODO(), secret, meta_v1.CreateOptions{})
			},
			expectError: false,
		},
	}

	for _, testCase := range testCases {
		kubeClient := fake.NewSimpleClientset()
		testCase.init(kubeClient)
		err := Validate(kubeClient, testCase.beConfig)
		if testCase.expectError && err == nil {
			t.Errorf("%v: Expected error but got nil", testCase.desc)
		}
		if !testCase.expectError && err != nil {
			t.Errorf("%v: Did not expect error but got: %v", testCase.desc, err)
		}
	}
}
