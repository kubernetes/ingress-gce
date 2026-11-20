/*
Copyright 2026 The Kubernetes Authors.

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

package standaloneneg

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	v1 "k8s.io/api/core/v1"
)

func TestServiceServesTCPPort(t *testing.T) {
	testCases := []struct {
		desc  string
		ports []v1.ServicePort
		port  int64
		want  bool
	}{
		{
			desc: "no ports",
			port: 8080,
			want: false,
		},
		{
			desc:  "TCP port matches",
			ports: []v1.ServicePort{tcpPort(80), tcpPort(8080)},
			port:  8080,
			want:  true,
		},
		{
			desc:  "empty protocol defaults to TCP",
			ports: []v1.ServicePort{{Port: 8080}},
			port:  8080,
			want:  true,
		},
		{
			desc:  "UDP port does not match",
			ports: []v1.ServicePort{udpPort(8080)},
			port:  8080,
			want:  false,
		},
		{
			desc:  "no matching port",
			ports: []v1.ServicePort{tcpPort(80), udpPort(53)},
			port:  8080,
			want:  false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			svc := &v1.Service{Spec: v1.ServiceSpec{Ports: tc.ports}}
			if got := serviceServesTCPPort(svc, tc.port); got != tc.want {
				t.Errorf("serviceServesTCPPort(%v, %d) = %v, want %v", tc.ports, tc.port, got, tc.want)
			}
		})
	}
}

func TestExternalHealthCheckAddrs(t *testing.T) {
	testCases := []struct {
		desc           string
		svc            *v1.Service
		usable         []usableFR
		wantAddrs      []string
		wantCollisions map[int64][]string
	}{
		{
			desc: "health check traffic of all VIPs is redirected",
			svc:  standaloneSvc("fr1,fr2"),
			usable: []usableFR{
				{ref: frRef{raw: "fr1"}, vips: []string{"10.0.0.1", "2001:db8::1"}, hcPort: 8080},
				{ref: frRef{raw: "fr2"}, vips: []string{"10.0.0.2"}, hcPort: 8081},
			},
			wantAddrs: []string{"10.0.0.1:8080", "[2001:db8::1]:8080", "10.0.0.2:8081"},
		},
		{
			desc: "duplicate addresses are skipped",
			svc:  standaloneSvc("fr1,fr2"),
			usable: []usableFR{
				{ref: frRef{raw: "fr1"}, vips: []string{"10.0.0.1"}, hcPort: 8080},
				{ref: frRef{raw: "fr2"}, vips: []string{"10.0.0.1"}, hcPort: 8080},
			},
			wantAddrs: []string{"10.0.0.1:8080"},
		},
		{
			desc: "forwarding rules without HTTP health check are skipped",
			svc:  standaloneSvc("fr1"),
			usable: []usableFR{
				{ref: frRef{raw: "fr1"}, vips: []string{"10.0.0.1"}},
			},
		},
		{
			desc: "health check targeting the health check node port is not redirected",
			svc:  standaloneSvc("fr1", withHealthCheckNodePort(30000)),
			usable: []usableFR{
				{ref: frRef{raw: "fr1"}, vips: []string{"10.0.0.1"}, hcPort: 30000},
			},
		},
		{
			desc: "health check ports colliding with service TCP ports are skipped",
			svc:  standaloneSvc("fr1,fr2,fr3", withServicePorts(tcpPort(80), udpPort(8080))),
			usable: []usableFR{
				{ref: frRef{raw: "fr1"}, vips: []string{"10.0.0.1"}, hcPort: 80},
				{ref: frRef{raw: "fr2"}, vips: []string{"10.0.0.2"}, hcPort: 80},
				{ref: frRef{raw: "fr3"}, vips: []string{"10.0.0.3"}, hcPort: 8080},
			},
			wantAddrs:      []string{"10.0.0.3:8080"},
			wantCollisions: map[int64][]string{80: {"fr1", "fr2"}},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			addrs, collisions := externalHealthCheckAddrs(tc.svc, tc.usable)
			if diff := cmp.Diff(tc.wantAddrs, addrs); diff != "" {
				t.Errorf("externalHealthCheckAddrs() addrs mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.wantCollisions, collisions, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("externalHealthCheckAddrs() collisions mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
