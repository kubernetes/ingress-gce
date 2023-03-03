/*
Copyright 2023 The Kubernetes Authors.

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

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/ingress-gce/pkg/test"
)

func TestIPv6FRName(t *testing.T) {
	testCases := []struct {
		desc               string
		svcUID             types.UID
		expectedIPv6FRName string
	}{
		{
			desc:               "Should add -ipv6 suffix to normal RBS forwarding rule name",
			svcUID:             "09e0afadb26f4dccbc25d00f971b289",
			expectedIPv6FRName: "a09e0afadb26f4dccbc25d00f971b289-ipv6",
		},
		{
			// this test checks theoretical situation, in reality, we never have services with such a long UIDs
			desc:               "Should trim and add -ipv6 suffix to normal RBS forwarding rule name",
			svcUID:             "09e0afadb26f4dccbc25d00f971b289a09e0afadb26f4dccbc25d00f971b289",
			expectedIPv6FRName: "a09e0afadb26f4dccbc25d00f971b289-ipv6",
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			svc := test.NewL4NetLBRBSService(8080)
			svc.UID = tc.svcUID
			l4NetLBParams := &L4NetLBParams{
				Service: svc,
			}
			l4NetLB := NewL4NetLB(l4NetLBParams)

			ipv6FRName := l4NetLB.ipv6FRName()
			if ipv6FRName != tc.expectedIPv6FRName {
				t.Errorf("Expecetd ipv6 forwarding rule name: %s, got %s", tc.expectedIPv6FRName, ipv6FRName)
			}
		})
	}
}
