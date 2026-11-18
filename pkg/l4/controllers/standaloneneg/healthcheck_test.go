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
