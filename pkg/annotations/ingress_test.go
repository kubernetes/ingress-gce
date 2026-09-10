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

	v1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestIngress(t *testing.T) {
	for _, tc := range []struct {
		desc              string
		ing               *v1.Ingress
		allowHTTP         bool
		useNamedTLS       string
		staticIPName      string
		ingressClass      string
		allowGlobalAccess bool
		wantErr           bool
	}{
		{
			desc:      "Empty ingress",
			ing:       &v1.Ingress{},
			allowHTTP: true, // defaults to true.
		},
		{
			desc: "Global and Regional StaticIP Specified",
			ing: &v1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						GlobalStaticIPNameKey:   "1.2.3.4",
						RegionalStaticIPNameKey: "10.0.0.0",
						IngressClassKey:         GceL7ILBIngressClass,
					},
				},
			},
			ingressClass: GceL7ILBIngressClass,
			staticIPName: "",
			allowHTTP:    true,
			wantErr:      true,
		},
		{
			desc: "Test most annotations",
			ing: &v1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						AllowHTTPKey:          "false",
						IngressClassKey:       "gce",
						PreSharedCertKey:      "shared-cert-key",
						GlobalStaticIPNameKey: "1.2.3.4",
					},
				},
			},
			allowHTTP:    false,
			useNamedTLS:  "shared-cert-key",
			staticIPName: "1.2.3.4",
			ingressClass: "gce",
		},
		{
			desc: "AllowGlobalAccess set to true",
			ing: &v1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						AllowGlobalAccessKey: "true",
						IngressClassKey:      GceL7ILBIngressClass,
					},
				},
			},
			allowHTTP:         true,
			ingressClass:      GceL7ILBIngressClass,
			allowGlobalAccess: true,
		},
		{
			desc: "AllowGlobalAccess set to false",
			ing: &v1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						AllowGlobalAccessKey: "false",
						IngressClassKey:      GceL7ILBIngressClass,
					},
				},
			},
			allowHTTP:         true,
			ingressClass:      GceL7ILBIngressClass,
			allowGlobalAccess: false,
		},
		{
			desc: "AllowGlobalAccess invalid value defaults to false",
			ing: &v1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						AllowGlobalAccessKey: "invalid",
					},
				},
			},
			allowHTTP:         true,
			allowGlobalAccess: false,
		},
	} {
		ing := FromIngress(tc.ing)

		if x := ing.AllowHTTP(); x != tc.allowHTTP {
			t.Errorf("ingress %+v; AllowHTTP() = %v, want %v", tc.ing, x, tc.allowHTTP)
		}
		if x := ing.UseNamedTLS(); x != tc.useNamedTLS {
			t.Errorf("ingress %+v; UseNamedTLS() = %v, want %v", tc.ing, x, tc.useNamedTLS)
		}
		staticIp, err := ing.StaticIPName()
		if (err != nil) != tc.wantErr {
			t.Errorf("ingress: %+v, err = %v, wantErr = %v", tc.ing, err, tc.wantErr)
		}
		if staticIp != tc.staticIPName {
			t.Errorf("ingress %+v; GlobalStaticIPName() = %v, want %v", tc.ing, staticIp, tc.staticIPName)
		}
		if x := ing.IngressClass(); x != tc.ingressClass {
			t.Errorf("ingress %+v; IngressClass() = %v, want %v", tc.ing, x, tc.ingressClass)
		}
		if x := ing.AllowGlobalAccess(); x != tc.allowGlobalAccess {
			t.Errorf("ingress %+v; AllowGlobalAccess() = %v, want %v", tc.ing, x, tc.allowGlobalAccess)
		}
	}
}
