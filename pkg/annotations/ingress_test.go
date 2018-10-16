/*
Copyright 2017 The Kubernetes Authors.

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

package annotations

import (
	"testing"

	extensions "k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestIngress(t *testing.T) {
	for _, tc := range []struct {
		ing          *extensions.Ingress
		allowHTTP    bool
		useNamedTLS  string
		useSSLPolicy string
		staticIPName string
		ingressClass string
	}{
		{
			ing:       &extensions.Ingress{},
			allowHTTP: true, // defaults to true.
		},
		{
			ing: &extensions.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						AllowHTTPKey:     "false",
						IngressClassKey:  "gce",
						PreSharedCertKey: "shared-cert-key",
						StaticIPNameKey:  "1.2.3.4",
						SSLPolicyKey:     "https://google.com/sslpolicy/selflink",
					},
				},
			},
			allowHTTP:    false,
			useNamedTLS:  "shared-cert-key",
			useSSLPolicy: "https://google.com/sslpolicy/selflink",
			staticIPName: "1.2.3.4",
			ingressClass: "gce",
		},
	} {
		ing := FromIngress(tc.ing)
		if x := ing.AllowHTTP(); x != tc.allowHTTP {
			t.Errorf("ingress %+v; AllowHTTP() = %v, want %v", tc.ing, x, tc.allowHTTP)
		}
		if x := ing.UseNamedTLS(); x != tc.useNamedTLS {
			t.Errorf("ingress %+v; UseNamedTLS() = %v, want %v", tc.ing, x, tc.useNamedTLS)
		}
		if x := ing.StaticIPName(); x != tc.staticIPName {
			t.Errorf("ingress %+v; StaticIPName() = %v, want %v", tc.ing, x, tc.staticIPName)
		}
		if x := ing.IngressClass(); x != tc.ingressClass {
			t.Errorf("ingress %+v; IngressClass() = %v, want %v", tc.ing, x, tc.ingressClass)
		}
		if x := ing.UseSSLPolicy(); x != tc.useSSLPolicy {
			t.Errorf("ingress %+v; UseSSLPolicy() = %v, want %v", tc.ing, x, tc.useSSLPolicy)
		}
	}
}
