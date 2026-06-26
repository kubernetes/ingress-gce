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

package shared

import (
	"testing"

	"k8s.io/apimachinery/pkg/util/sets"
)

func TestZonesPerSubnetMapEqual(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		desc           string
		a              ZonesPerSubnetMap
		b              ZonesPerSubnetMap
		expectedResult bool
	}{
		{
			desc:           "both nil",
			a:              nil,
			b:              nil,
			expectedResult: true,
		},
		{
			desc:           "both empty",
			a:              ZonesPerSubnetMap{},
			b:              ZonesPerSubnetMap{},
			expectedResult: true,
		},
		{
			desc:           "one nil, one empty",
			a:              nil,
			b:              ZonesPerSubnetMap{},
			expectedResult: true,
		},
		{
			desc: "equal maps, single subnet",
			a: ZonesPerSubnetMap{
				"subnet1": sets.New("zone1", "zone2"),
			},
			b: ZonesPerSubnetMap{
				"subnet1": sets.New("zone1", "zone2"),
			},
			expectedResult: true,
		},
		{
			desc: "equal maps, multiple subnets",
			a: ZonesPerSubnetMap{
				"subnet1": sets.New("zone1", "zone2"),
				"subnet2": sets.New("zone3"),
			},
			b: ZonesPerSubnetMap{
				"subnet1": sets.New("zone2", "zone1"),
				"subnet2": sets.New("zone3"),
			},
			expectedResult: true,
		},
		{
			desc: "different lengths",
			a: ZonesPerSubnetMap{
				"subnet1": sets.New("zone1"),
			},
			b: ZonesPerSubnetMap{
				"subnet1": sets.New("zone1"),
				"subnet2": sets.New("zone2"),
			},
			expectedResult: false,
		},
		{
			desc: "same subnets, different zones",
			a: ZonesPerSubnetMap{
				"subnet1": sets.New("zone1", "zone2"),
			},
			b: ZonesPerSubnetMap{
				"subnet1": sets.New("zone1", "zone3"),
			},
			expectedResult: false,
		},
		{
			desc: "different subnets, same zones",
			a: ZonesPerSubnetMap{
				"subnet1": sets.New("zone1"),
			},
			b: ZonesPerSubnetMap{
				"subnet2": sets.New("zone1"),
			},
			expectedResult: false,
		},
		{
			desc: "one set is subset of another",
			a: ZonesPerSubnetMap{
				"subnet1": sets.New("zone1", "zone2"),
			},
			b: ZonesPerSubnetMap{
				"subnet1": sets.New("zone1"),
			},
			expectedResult: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			result := tc.a.Equal(tc.b)
			if result != tc.expectedResult {
				t.Errorf("ZonesPerSubnetMap.Equal() = %t, want %t", result, tc.expectedResult)
			}
		})
	}
}
