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

package types

import (
	"fmt"
	"k8s.io/ingress-gce/pkg/annotations"
	"reflect"
	"testing"
)

type negNamer struct{}

func (*negNamer) NEG(namespace, name string, svcPort int32) string {
	return fmt.Sprintf("%v-%v-%v", namespace, name, svcPort)
}

func (*negNamer) IsNEG(name string) bool {
	return false
}

func TestPortInfoMapMerge(t *testing.T) {
	namer := &negNamer{}
	namespace := "namespace"
	name := "name"
	testcases := []struct {
		desc        string
		p1          PortInfoMap
		p2          PortInfoMap
		expectedMap PortInfoMap
		expectErr   bool
	}{
		{
			"empty map union empty map",
			PortInfoMap{},
			PortInfoMap{},
			PortInfoMap{},
			false,
		},
		{
			"empty map union a non-empty map is the non-empty map",
			PortInfoMap{},
			NewPortInfoMap(namespace, name, SvcPortMap{80: "namedport", 443: "3000"}, namer),
			NewPortInfoMap(namespace, name, SvcPortMap{80: "namedport", 443: "3000"}, namer),
			false,
		},
		{
			"union of two non-empty maps",
			NewPortInfoMap(namespace, name, SvcPortMap{443: "3000", 5000: "6000"}, namer),
			NewPortInfoMap(namespace, name, SvcPortMap{80: "namedport", 8080: "9000"}, namer),
			NewPortInfoMap(namespace, name, SvcPortMap{80: "namedport", 443: "3000", 5000: "6000", 8080: "9000"}, namer),
			false,
		},
		{
			"error on inconsistent value",
			NewPortInfoMap(namespace, name, SvcPortMap{80: "3000"}, namer),
			NewPortInfoMap(namespace, name, SvcPortMap{80: "namedport", 8000: "9000"}, namer),
			NewPortInfoMap(namespace, name, SvcPortMap{80: "namedport", 443: "3000", 5000: "6000", 8080: "9000"}, namer),
			true,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.desc, func(t *testing.T) {
			err := tc.p1.Merge(tc.p2)
			if tc.expectErr && err == nil {
				t.Errorf("Expect error != nil, got %v", err)
			}

			if !tc.expectErr && err != nil {
				t.Errorf("Expect error == nil, got %v", err)
			}

			if !tc.expectErr {
				if !reflect.DeepEqual(tc.p1, tc.expectedMap) {
					t.Errorf("Expected p1.Merge(p2) to equal: %v; got: %v", tc.expectedMap, tc.p1)
				}
			}
		})
	}
}

func TestPortInfoMapDifference(t *testing.T) {
	namer := &negNamer{}
	namespace := "namespace"
	name := "name"
	testcases := []struct {
		desc        string
		p1          PortInfoMap
		p2          PortInfoMap
		expectedMap PortInfoMap
	}{
		{
			"empty map difference empty map",
			PortInfoMap{},
			PortInfoMap{},
			PortInfoMap{},
		},
		{
			"empty map difference a non-empty map is empty map",
			PortInfoMap{},
			NewPortInfoMap(namespace, name, SvcPortMap{80: "namedport", 443: "3000"}, namer),
			PortInfoMap{},
		},
		{
			"non-empty map difference a non-empty map is the non-empty map",
			NewPortInfoMap(namespace, name, SvcPortMap{80: "namedport", 443: "3000"}, namer),
			PortInfoMap{},
			NewPortInfoMap(namespace, name, SvcPortMap{80: "namedport", 443: "3000"}, namer),
		},
		{
			"difference of two non-empty maps with the same elements",
			NewPortInfoMap(namespace, name, SvcPortMap{80: "namedport", 443: "3000"}, namer),
			NewPortInfoMap(namespace, name, SvcPortMap{80: "namedport", 443: "3000"}, namer),
			PortInfoMap{},
		},
		{
			"difference of two non-empty maps with no elements in common returns p1",
			NewPortInfoMap(namespace, name, SvcPortMap{443: "3000", 5000: "6000"}, namer),
			NewPortInfoMap(namespace, name, SvcPortMap{80: "namedport", 8080: "9000"}, namer),
			NewPortInfoMap(namespace, name, SvcPortMap{443: "3000", 5000: "6000"}, namer),
		},
		{
			"difference of two non-empty maps with elements in common",
			NewPortInfoMap(namespace, name, SvcPortMap{80: "namedport", 443: "3000", 5000: "6000", 8080: "9000"}, namer),
			NewPortInfoMap(namespace, name, SvcPortMap{80: "namedport", 8080: "9000"}, namer),
			NewPortInfoMap(namespace, name, SvcPortMap{443: "3000", 5000: "6000"}, namer),
		},
		{
			"difference of two non-empty maps with a key in common but different in value",
			NewPortInfoMap(namespace, name, SvcPortMap{80: "namedport"}, namer),
			NewPortInfoMap(namespace, name, SvcPortMap{80: "8080", 8080: "9000"}, namer),
			NewPortInfoMap(namespace, name, SvcPortMap{80: "namedport"}, namer),
		},
		{
			"difference of two non-empty maps with 2 keys in common but different in values",
			NewPortInfoMap(namespace, name, SvcPortMap{80: "namedport", 443: "8443"}, namer),
			NewPortInfoMap(namespace, name, SvcPortMap{80: "8080", 443: "9443"}, namer),
			NewPortInfoMap(namespace, name, SvcPortMap{80: "namedport", 443: "8443"}, namer),
		},
	}

	for _, tc := range testcases {
		t.Run(tc.desc, func(t *testing.T) {
			result := tc.p1.Difference(tc.p2)
			if !reflect.DeepEqual(result, tc.expectedMap) {
				t.Errorf("Expected p1.Difference(p2) to equal: %v; got: %v", tc.expectedMap, result)
			}
		})
	}
}

func TestPortInfoMapToPortNegMap(t *testing.T) {
	for _, tc := range []struct {
		desc             string
		portInfoMap      PortInfoMap
		expectPortNegMap annotations.PortNegMap
	}{
		{
			desc:             "Test empty struct",
			portInfoMap:      PortInfoMap{},
			expectPortNegMap: annotations.PortNegMap{},
		},
		{
			desc:             "1 port",
			portInfoMap:      PortInfoMap{int32(80): PortInfo{NegName: "neg1"}},
			expectPortNegMap: annotations.PortNegMap{"80": "neg1"},
		},
		{
			desc:             "2 ports",
			portInfoMap:      PortInfoMap{int32(80): PortInfo{NegName: "neg1"}, int32(8080): PortInfo{NegName: "neg2"}},
			expectPortNegMap: annotations.PortNegMap{"80": "neg1", "8080": "neg2"},
		},
		{
			desc:             "3 ports",
			portInfoMap:      PortInfoMap{int32(80): PortInfo{NegName: "neg1"}, int32(443): PortInfo{NegName: "neg2"}, int32(8080): PortInfo{NegName: "neg3"}},
			expectPortNegMap: annotations.PortNegMap{"80": "neg1", "443": "neg2", "8080": "neg3"},
		},
	} {
		res := tc.portInfoMap.ToPortNegMap()
		if !reflect.DeepEqual(res, tc.expectPortNegMap) {
			t.Errorf("For test case %q, expect %v, but got %v", tc.desc, tc.expectPortNegMap, res)
		}
	}

}
