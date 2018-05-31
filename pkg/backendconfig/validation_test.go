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
	"testing"

	v1 "k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	backendconfigv1beta1 "k8s.io/ingress-gce/pkg/apis/backendconfig/v1beta1"
)

var (
	beConfig = &backendconfigv1beta1.BackendConfig{
		ObjectMeta: meta_v1.ObjectMeta{
			Namespace: "default",
		},
		Spec: backendconfigv1beta1.BackendConfigSpec{
			Iap: &backendconfigv1beta1.IAPConfig{
				Enabled: true,
				OAuthClientCredentials: &backendconfigv1beta1.OAuthClientCredentials{
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
		expectError bool
	}{
		{
			desc: "secret does not exist",
			init: func(kubeClient kubernetes.Interface) {
				secret := &v1.Secret{
					ObjectMeta: meta_v1.ObjectMeta{
						Namespace: "wrong-namespace",
						Name:      "foo",
					},
				}
				kubeClient.Core().Secrets("wrong-namespace").Create(secret)
			},
			expectError: true,
		},
		{
			desc: "secret does not contain client_id",
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
				kubeClient.Core().Secrets("default").Create(secret)
			},
			expectError: true,
		},
		{
			desc: "secret does not contain client_secret",
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
				kubeClient.Core().Secrets("default").Create(secret)
			},
			expectError: true,
		},
		{
			desc: "validation passes",
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
				kubeClient.Core().Secrets("default").Create(secret)
			},
			expectError: false,
		},
		{
			desc: "iap and cdn enabled at the same time",
			init: func(kubeClient kubernetes.Interface) {
				// TODO(rramkumar): Don't modify in-flight.
				// This works now since this is the last test in the
				// list of cases.
				beConfig.Spec.Cdn = &backendconfigv1beta1.CDNConfig{
					Enabled: true,
				}
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
				kubeClient.Core().Secrets("default").Create(secret)
			},
			expectError: true,
		},
	}

	for _, testCase := range testCases {
		kubeClient := fake.NewSimpleClientset()
		testCase.init(kubeClient)
		err := Validate(kubeClient, beConfig)
		if testCase.expectError && err == nil {
			t.Errorf("%v: Expected error but got nil", testCase.desc)
		}
		if !testCase.expectError && err != nil {
			t.Errorf("%v: Did not expect error but got: %v", testCase.desc, err)
		}
	}
}
