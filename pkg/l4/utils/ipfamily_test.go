/*
Copyright 2022 The Kubernetes Authors.

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

package utils

import (
	"testing"

	"k8s.io/api/core/v1"
)

func TestNeedsIPv6(t *testing.T) {
	testCases := []struct {
		service       *v1.Service
		wantNeedsIPv6 bool
		desc          string
	}{
		{
			desc:          "Should return false for nil pointer",
			service:       nil,
			wantNeedsIPv6: false,
		},
		{
			desc: "Should detect ipv6 for dual-stack ip families",
			service: &v1.Service{Spec: v1.ServiceSpec{
				IPFamilies: []v1.IPFamily{v1.IPv4Protocol, v1.IPv6Protocol},
			}},
			wantNeedsIPv6: true,
		},
		{
			desc: "Should not detect ipv6 for only ipv4 families",
			service: &v1.Service{Spec: v1.ServiceSpec{
				IPFamilies: []v1.IPFamily{v1.IPv4Protocol},
			}},
			wantNeedsIPv6: false,
		},
		{
			desc: "Should detect ipv6 for only ipv6 families",
			service: &v1.Service{Spec: v1.ServiceSpec{
				IPFamilies: []v1.IPFamily{v1.IPv6Protocol},
			}},
			wantNeedsIPv6: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			needsIPv6 := NeedsIPv6(tc.service)

			if needsIPv6 != tc.wantNeedsIPv6 {
				t.Errorf("NeedsIPv6(%v) returned %t, not equal to expected wantNeedsIPv6 = %t", tc.service, needsIPv6, tc.wantNeedsIPv6)
			}
		})
	}
}

func TestNeedsIPv4(t *testing.T) {
	testCases := []struct {
		service       *v1.Service
		wantNeedsIPv4 bool
		desc          string
	}{
		{
			desc:          "Should return false for nil pointer",
			service:       nil,
			wantNeedsIPv4: false,
		},
		{
			desc: "Should handle dual-stack ip families",
			service: &v1.Service{Spec: v1.ServiceSpec{
				IPFamilies: []v1.IPFamily{v1.IPv4Protocol, v1.IPv6Protocol},
			}},
			wantNeedsIPv4: true,
		},
		{
			desc: "Should handle only ipv4 family",
			service: &v1.Service{Spec: v1.ServiceSpec{
				IPFamilies: []v1.IPFamily{v1.IPv4Protocol},
			}},
			wantNeedsIPv4: true,
		},
		{
			desc: "Should not handle only ipv6 family",
			service: &v1.Service{Spec: v1.ServiceSpec{
				IPFamilies: []v1.IPFamily{v1.IPv6Protocol},
			}},
			wantNeedsIPv4: false,
		},
		{
			desc: "Empty families should be recognized as IPv4. Should never happen in real life",
			service: &v1.Service{Spec: v1.ServiceSpec{
				IPFamilies: []v1.IPFamily{},
			}},
			wantNeedsIPv4: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			needsIPv4 := NeedsIPv4(tc.service)

			if needsIPv4 != tc.wantNeedsIPv4 {
				t.Errorf("NeedsIPv4(%v) returned %t, not equal to expected wantNeedsIPv6 = %t", tc.service, needsIPv4, tc.wantNeedsIPv4)
			}
		})
	}
}
