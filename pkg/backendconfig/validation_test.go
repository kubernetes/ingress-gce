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

	"github.com/google/go-cmp/cmp"
	v1 "k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	dynamicfake "k8s.io/client-go/dynamic/fake"
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

func TestOverrideUnsupportedSpec(t *testing.T) {
	backendConfigClient := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())

	for _, tc := range []struct {
		desc           string
		namespacedName types.NamespacedName
		backendConfig  *unstructured.Unstructured
		expected       *unstructured.Unstructured
	}{
		{
			desc: "invalid custom resource headers and non-compatible",
			namespacedName: types.NamespacedName{
				Namespace: "ns1",
				Name:      "invalid-custom-req-headers-1",
			},
			backendConfig: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"spec": map[string]interface{}{
						"customResourceHeaders": []interface{}{"abcde"},
					},
				},
			},
			expected: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"spec": map[string]interface{}{},
				},
			},
		},
		{
			desc: "invalid custom resource headers but compatible",
			namespacedName: types.NamespacedName{
				Namespace: "ns1",
				Name:      "invalid-custom-req-headers-2",
			},
			backendConfig: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"spec": map[string]interface{}{
						"customResourceHeaders": map[string]interface{}{
							"headers":         []interface{}{"abcde"},
							"unsupported-key": "unsupported-value",
						},
					},
				},
			},
			expected: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"spec": map[string]interface{}{
						"customResourceHeaders": map[string]interface{}{
							"headers":         []interface{}{"abcde"},
							"unsupported-key": "unsupported-value",
						},
					},
				},
			},
		},
		{
			desc: "valid custom resource headers",
			namespacedName: types.NamespacedName{
				Namespace: "ns1",
				Name:      "valid-custom-req-headers",
			},
			backendConfig: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"spec": map[string]interface{}{
						"customResourceHeaders": map[string]interface{}{
							"headers": []interface{}{"abcde"},
						},
					},
				},
			},
			expected: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"spec": map[string]interface{}{
						"customResourceHeaders": map[string]interface{}{
							"headers": []interface{}{"abcde"},
						},
					},
				},
			},
		},
		{
			desc: "invalid logging",
			namespacedName: types.NamespacedName{
				Namespace: "ns1",
				Name:      "invalid-logging",
			},
			backendConfig: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"spec": map[string]interface{}{
						"logging": map[string]interface{}{
							"enable": "non-bool",
						},
					},
				},
			},
			expected: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"spec": map[string]interface{}{},
				},
			},
		},
		{
			desc: "some valid",
			namespacedName: types.NamespacedName{
				Namespace: "ns1",
				Name:      "some-valid-spec",
			},
			backendConfig: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"spec": map[string]interface{}{
						"customResourceHeaders": []interface{}{"abcde"},
						"logging": map[string]interface{}{
							"enable": interface{}(false),
						},
						"healthCheck": false,
					},
				},
			},
			expected: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"spec": map[string]interface{}{
						"logging": map[string]interface{}{
							"enable": false,
						},
					},
				},
			},
		},
		{
			desc: "some valid in default namespace",
			namespacedName: types.NamespacedName{
				Name: "some-valid-spec",
			},
			backendConfig: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"spec": map[string]interface{}{
						"customResourceHeaders": []interface{}{"abcde"},
						"logging": map[string]interface{}{
							"enable": interface{}(false),
						},
						"healthCheck": false,
					},
				},
			},
			expected: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"spec": map[string]interface{}{
						"logging": map[string]interface{}{
							"enable": false,
						},
					},
				},
			},
		},
		{
			desc: "all invalid",
			namespacedName: types.NamespacedName{
				Namespace: "ns1",
				Name:      "invalid-spec",
			},
			backendConfig: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"spec": map[string]interface{}{
						"customResourceHeaders": []interface{}{"abcde"},
						"logging": map[string]interface{}{
							"enable": "non-bool",
						},
						"healthCheck": false,
					},
				},
			},
			expected: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"spec": map[string]interface{}{},
				},
			},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			tc.backendConfig.Object["kind"] = "BackendConfig"
			tc.expected.Object["kind"] = "BackendConfig"
			objectMeta := map[string]interface{}{
				"name": tc.namespacedName.Name,
			}
			if tc.namespacedName.Namespace != "" {
				objectMeta["namespace"] = tc.namespacedName.Namespace
			}
			tc.backendConfig.Object["metadata"] = objectMeta
			tc.expected.Object["metadata"] = objectMeta
			if _, err := backendConfigClient.Resource(groupVersionResource).Namespace(tc.namespacedName.Namespace).Create(context.TODO(), tc.backendConfig, meta_v1.CreateOptions{}); err != nil {
				t.Fatal(err)
			}

			if err := OverrideUnsupportedSpec(backendConfigClient); err != nil {
				t.Fatal(err)
			}

			gotBC, err := backendConfigClient.Resource(groupVersionResource).Namespace(tc.namespacedName.Namespace).Get(context.TODO(), tc.namespacedName.Name, meta_v1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}

			if diff := cmp.Diff(tc.expected, gotBC); diff != "" {
				t.Fatalf("Got diff for backendconfig %s (-want +got):%s\n", tc.namespacedName, diff)
			}
		})
	}
}
