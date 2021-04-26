/*
Copyright 2020 The Kubernetes Authors.

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

package loadbalancers

import (
	"testing"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"github.com/google/go-cmp/cmp"
	v1 "k8s.io/api/networking/v1"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/composite"
)

func TestNewStaticAddress(t *testing.T) {
	testCases := []struct {
		desc       string
		ip         string
		name       string
		isInternal bool
		expected   *composite.Address
	}{
		{
			desc:       "external static address",
			ip:         "1.2.3.4",
			name:       "external-addr",
			isInternal: false,
			expected:   &composite.Address{Name: "external-addr", Version: meta.VersionGA, Address: "1.2.3.4"},
		},
		{
			desc:       "internal static address",
			ip:         "10.2.3.4",
			name:       "internal-addr",
			isInternal: true,
			expected:   &composite.Address{Name: "internal-addr", Version: meta.VersionGA, Address: "10.2.3.4", AddressType: "INTERNAL"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			l7 := &L7{
				ingress: v1.Ingress{Spec: v1.IngressSpec{}},
				fw:      &composite.ForwardingRule{IPAddress: tc.ip},
			}

			if tc.isInternal {
				l7.ingress.Annotations = map[string]string{annotations.IngressClassKey: "gce-internal"}
			}

			result := l7.newStaticAddress(tc.name)
			if diff := cmp.Diff(tc.expected, result); diff != "" {
				t.Errorf("Got diff for Address (-want +got):\n%s", diff)
			}
		})
	}
}
