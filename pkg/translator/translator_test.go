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

package translator

import (
	"context"
	"testing"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"github.com/google/go-cmp/cmp"
	api_v1 "k8s.io/api/core/v1"
	"k8s.io/api/networking/v1beta1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/utils"
	namer_util "k8s.io/ingress-gce/pkg/utils/namer"
)

func TestToComputeURLMap(t *testing.T) {
	t.Parallel()

	wantComputeMap := testCompositeURLMap()
	namer := namer_util.NewNamer("uid1", "fw1")
	gceURLMap := &utils.GCEURLMap{
		DefaultBackend: &utils.ServicePort{NodePort: 30000, BackendNamer: namer},
		HostRules: []utils.HostRule{
			{
				Hostname: "abc.com",
				Paths: []utils.PathRule{
					{
						Path:    "/web",
						Backend: utils.ServicePort{NodePort: 32000, BackendNamer: namer},
					},
					{
						Path:    "/other",
						Backend: utils.ServicePort{NodePort: 32500, BackendNamer: namer},
					},
				},
			},
			{
				Hostname: "foo.bar.com",
				Paths: []utils.PathRule{
					{
						Path:    "/",
						Backend: utils.ServicePort{NodePort: 33000, BackendNamer: namer},
					},
					{
						Path:    "/*",
						Backend: utils.ServicePort{NodePort: 33500, BackendNamer: namer},
					},
				},
			},
		},
	}

	namerFactory := namer_util.NewFrontendNamerFactory(namer, "")
	feNamer := namerFactory.NamerForLoadBalancer("lb-name")
	gotComputeURLMap := ToCompositeURLMap(gceURLMap, feNamer, meta.GlobalKey("ns-lb-name"))
	if diff := cmp.Diff(wantComputeMap, gotComputeURLMap); diff != "" {
		t.Errorf("Unexpected diff from ToComputeURLMap() (-want +got):\n%s", diff)
	}
}

func testCompositeURLMap() *composite.UrlMap {
	return &composite.UrlMap{
		Name:           "k8s-um-lb-name",
		DefaultService: "global/backendServices/k8s-be-30000--uid1",
		HostRules: []*composite.HostRule{
			{
				Hosts:       []string{"abc.com"},
				PathMatcher: "host929ba26f492f86d4a9d66a080849865a",
			},
			{
				Hosts:       []string{"foo.bar.com"},
				PathMatcher: "host2d50cf9711f59181be6a5e5658e42c21",
			},
		},
		PathMatchers: []*composite.PathMatcher{
			{
				DefaultService: "global/backendServices/k8s-be-30000--uid1",
				Name:           "host929ba26f492f86d4a9d66a080849865a",
				PathRules: []*composite.PathRule{
					{
						Paths:   []string{"/web"},
						Service: "global/backendServices/k8s-be-32000--uid1",
					},
					{
						Paths:   []string{"/other"},
						Service: "global/backendServices/k8s-be-32500--uid1",
					},
				},
			},
			{
				DefaultService: "global/backendServices/k8s-be-30000--uid1",
				Name:           "host2d50cf9711f59181be6a5e5658e42c21",
				PathRules: []*composite.PathRule{
					{
						Paths:   []string{"/"},
						Service: "global/backendServices/k8s-be-33000--uid1",
					},
					{
						Paths:   []string{"/*"},
						Service: "global/backendServices/k8s-be-33500--uid1",
					},
				},
			},
		},
	}
}

func TestSecrets(t *testing.T) {
	secretsMap := map[string]*api_v1.Secret{
		"first-secret": &api_v1.Secret{
			ObjectMeta: meta_v1.ObjectMeta{
				Name: "first-secret",
			},
			Data: map[string][]byte{
				// TODO(rramkumar): Use real data here.
				api_v1.TLSCertKey:       []byte("cert"),
				api_v1.TLSPrivateKeyKey: []byte("private key"),
			},
		},
		"second-secret": &api_v1.Secret{
			ObjectMeta: meta_v1.ObjectMeta{
				Name: "second-secret",
			},
			Data: map[string][]byte{
				api_v1.TLSCertKey:       []byte("cert"),
				api_v1.TLSPrivateKeyKey: []byte("private key"),
			},
		},
		"third-secret": &api_v1.Secret{
			ObjectMeta: meta_v1.ObjectMeta{
				Name: "third-secret",
			},
			Data: map[string][]byte{
				api_v1.TLSCertKey:       []byte("cert"),
				api_v1.TLSPrivateKeyKey: []byte("private key"),
			},
		},
		"secret-no-cert": &api_v1.Secret{
			ObjectMeta: meta_v1.ObjectMeta{
				Name: "secret-no-cert",
			},
			Data: map[string][]byte{
				api_v1.TLSPrivateKeyKey: []byte("private key"),
			},
		},
		"secret-no-key": &api_v1.Secret{
			ObjectMeta: meta_v1.ObjectMeta{
				Name: "secret-no-key",
			},
			Data: map[string][]byte{
				api_v1.TLSCertKey: []byte("private key"),
			},
		},
	}

	cases := []struct {
		desc    string
		ing     *v1beta1.Ingress
		want    []*api_v1.Secret
		wantErr bool
	}{
		{
			desc: "ingress-single-secret",
			// TODO(rramkumar): Read Ingress spec from a file.
			ing: &v1beta1.Ingress{
				Spec: v1beta1.IngressSpec{
					TLS: []v1beta1.IngressTLS{
						{SecretName: "first-secret"},
					},
				},
			},
			want: []*api_v1.Secret{secretsMap["first-secret"]},
		},
		{
			desc: "ingress-multi-secret",
			ing: &v1beta1.Ingress{
				Spec: v1beta1.IngressSpec{
					TLS: []v1beta1.IngressTLS{
						{SecretName: "first-secret"},
						{SecretName: "second-secret"},
						{SecretName: "third-secret"},
					},
				},
			},
			want: []*api_v1.Secret{secretsMap["first-secret"], secretsMap["second-secret"], secretsMap["third-secret"]},
		},
		{
			desc: "mci-missing-secret",
			ing: &v1beta1.Ingress{
				Spec: v1beta1.IngressSpec{
					TLS: []v1beta1.IngressTLS{
						{SecretName: "does-not-exist-secret"},
					},
				},
			},
			wantErr: true,
		},
		{
			desc: "mci-secret-empty-cert",
			ing: &v1beta1.Ingress{
				Spec: v1beta1.IngressSpec{
					TLS: []v1beta1.IngressTLS{
						{SecretName: "secret-no-cert"},
					},
				},
			},
			wantErr: true,
		},
		{
			desc: "mci-secret-empty-priv-key",
			ing: &v1beta1.Ingress{
				Spec: v1beta1.IngressSpec{
					TLS: []v1beta1.IngressTLS{
						{SecretName: "secret-no-key"},
					},
				},
			},
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			kubeClient := fake.NewSimpleClientset()
			for _, v := range secretsMap {
				kubeClient.CoreV1().Secrets(tc.ing.Namespace).Create(context.TODO(), v, meta_v1.CreateOptions{})
			}

			env, err := NewEnv(tc.ing, kubeClient)
			if err != nil {
				t.Fatalf("NewEnv(): %v", err)
			}

			got, err := Secrets(env)
			if tc.wantErr && err == nil {
				t.Fatal("expected Secrets() to return an error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("Secrets(): %v", err)
			}

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Fatalf("Got diff (-want +got):\n%s", diff)
			}
		})
	}
}
