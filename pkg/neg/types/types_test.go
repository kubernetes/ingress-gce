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
	"reflect"
	"testing"

	istioV1alpha3 "istio.io/api/networking/v1alpha3"
	v1 "k8s.io/api/core/v1"
	discovery "k8s.io/api/discovery/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/ingress-gce/pkg/annotations"
)

type negNamer struct{}

func (*negNamer) NEG(namespace, name string, svcPort int32) string {
	return fmt.Sprintf("%v-%v-%v", namespace, name, svcPort)
}

func (*negNamer) VMIPNEG(namespace, name string) (string, bool) {
	return fmt.Sprintf("%v-%v", namespace, name), true
}

func (*negNamer) NEGWithSubset(namespace, name, subset string, svcPort int32) string {
	return fmt.Sprintf("%v-%v-%v-%v", namespace, name, subset, svcPort)
}

func (*negNamer) IsNEG(name string) bool {
	return false
}

func createDestinationRule(host string, subsets ...string) *istioV1alpha3.DestinationRule {
	ds := istioV1alpha3.DestinationRule{
		Host: host,
	}
	for _, subset := range subsets {
		ds.Subsets = append(ds.Subsets, &istioV1alpha3.Subset{Name: subset})
	}
	return &ds
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
			NewPortInfoMap(namespace, name, NewSvcPortTupleSet(SvcPortTuple{Port: 80, TargetPort: "namedport"}, SvcPortTuple{Port: 443, TargetPort: "3000"}), namer, false, nil),
			NewPortInfoMap(namespace, name, NewSvcPortTupleSet(SvcPortTuple{Port: 80, TargetPort: "namedport"}, SvcPortTuple{Port: 443, TargetPort: "3000"}), namer, false, nil),
			false,
		},
		{
			"empty map union a non-empty map is the non-empty map 2",
			NewPortInfoMap(namespace, name, NewSvcPortTupleSet(SvcPortTuple{Port: 80, TargetPort: "namedport"}, SvcPortTuple{Port: 443, TargetPort: "3000"}), namer, true, nil),
			PortInfoMap{},
			NewPortInfoMap(namespace, name, NewSvcPortTupleSet(SvcPortTuple{Port: 80, TargetPort: "namedport"}, SvcPortTuple{Port: 443, TargetPort: "3000"}), namer, true, nil),
			false,
		},
		{
			"union of two non-empty maps, none has readiness gate enabled",
			NewPortInfoMap(namespace, name, NewSvcPortTupleSet(SvcPortTuple{Port: 443, TargetPort: "3000"}, SvcPortTuple{Port: 5000, TargetPort: "6000"}), namer, false, nil),
			NewPortInfoMap(namespace, name, NewSvcPortTupleSet(SvcPortTuple{Port: 80, TargetPort: "namedport"}, SvcPortTuple{Port: 8080, TargetPort: "9000"}), namer, false, nil),
			NewPortInfoMap(namespace, name, NewSvcPortTupleSet(SvcPortTuple{Port: 443, TargetPort: "3000"}, SvcPortTuple{Port: 5000, TargetPort: "6000"}, SvcPortTuple{Port: 80, TargetPort: "namedport"}, SvcPortTuple{Port: 8080, TargetPort: "9000"}), namer, false, nil),
			false,
		},
		{
			"union of two non-empty maps, all have readiness gate enabled ",
			NewPortInfoMap(namespace, name, NewSvcPortTupleSet(SvcPortTuple{Port: 443, TargetPort: "3000"}, SvcPortTuple{Port: 5000, TargetPort: "6000"}), namer, true, nil),
			NewPortInfoMap(namespace, name, NewSvcPortTupleSet(SvcPortTuple{Port: 80, TargetPort: "namedport"}, SvcPortTuple{Port: 8080, TargetPort: "9000"}), namer, true, nil),
			NewPortInfoMap(namespace, name, NewSvcPortTupleSet(SvcPortTuple{Port: 443, TargetPort: "3000"}, SvcPortTuple{Port: 5000, TargetPort: "6000"}, SvcPortTuple{Port: 80, TargetPort: "namedport"}, SvcPortTuple{Port: 8080, TargetPort: "9000"}), namer, true, nil),
			false,
		},
		{
			"union of two non-empty maps with one overlapping service port",
			NewPortInfoMap(namespace, name, NewSvcPortTupleSet(SvcPortTuple{Port: 80, TargetPort: "3000"}, SvcPortTuple{Port: 5000, TargetPort: "6000"}), namer, false, nil),
			NewPortInfoMap(namespace, name, NewSvcPortTupleSet(SvcPortTuple{Port: 80, TargetPort: "3000"}, SvcPortTuple{Port: 8080, TargetPort: "9000"}), namer, false, nil),
			NewPortInfoMap(namespace, name, NewSvcPortTupleSet(SvcPortTuple{Port: 80, TargetPort: "3000"}, SvcPortTuple{Port: 5000, TargetPort: "6000"}, SvcPortTuple{Port: 8080, TargetPort: "9000"}), namer, false, nil),
			false,
		},
		{
			"union of two non-empty maps with overlapping service port and difference in readiness gate configurations ",
			NewPortInfoMap(namespace, name, NewSvcPortTupleSet(SvcPortTuple{Port: 80, TargetPort: "3000"}, SvcPortTuple{Port: 5000, TargetPort: "6000"}), namer, true, nil),
			NewPortInfoMap(namespace, name, NewSvcPortTupleSet(SvcPortTuple{Port: 80, TargetPort: "3000"}, SvcPortTuple{Port: 8080, TargetPort: "9000"}), namer, false, nil),
			PortInfoMap{
				PortInfoMapKey{80, ""}: PortInfo{
					PortTuple: SvcPortTuple{
						Port:       80,
						TargetPort: "3000",
					},
					NegName:       namer.NEG(namespace, name, 80),
					ReadinessGate: true,
				},
				PortInfoMapKey{5000, ""}: PortInfo{
					PortTuple: SvcPortTuple{
						Port:       5000,
						TargetPort: "6000",
					},
					NegName:       namer.NEG(namespace, name, 5000),
					ReadinessGate: true,
				},
				PortInfoMapKey{8080, ""}: PortInfo{
					PortTuple: SvcPortTuple{
						Port:       8080,
						TargetPort: "9000",
					},
					NegName:       namer.NEG(namespace, name, 8080),
					ReadinessGate: false,
				},
			},
			false,
		},
		{
			"union of two non-empty maps with overlapping service port and difference in readiness gate configurations with named port",
			NewPortInfoMap(namespace, name, NewSvcPortTupleSet(SvcPortTuple{Port: 80, Name: "foo", TargetPort: "3000"}, SvcPortTuple{Port: 5000, Name: "bar", TargetPort: "6000"}), namer, true, nil),
			NewPortInfoMap(namespace, name, NewSvcPortTupleSet(SvcPortTuple{Port: 80, Name: "foo", TargetPort: "3000"}, SvcPortTuple{Port: 8080, TargetPort: "9000"}), namer, false, nil),
			PortInfoMap{
				PortInfoMapKey{80, ""}: PortInfo{
					PortTuple: SvcPortTuple{
						Port:       80,
						Name:       "foo",
						TargetPort: "3000",
					},
					NegName:       namer.NEG(namespace, name, 80),
					ReadinessGate: true,
				},
				PortInfoMapKey{5000, ""}: PortInfo{
					PortTuple: SvcPortTuple{
						Port:       5000,
						Name:       "bar",
						TargetPort: "6000",
					},
					NegName:       namer.NEG(namespace, name, 5000),
					ReadinessGate: true,
				},
				PortInfoMapKey{8080, ""}: PortInfo{
					PortTuple: SvcPortTuple{
						Port:       8080,
						TargetPort: "9000",
					},
					NegName:       namer.NEG(namespace, name, 8080),
					ReadinessGate: false,
				},
			},
			false,
		},
		{
			"union of two non-empty maps with overlapping service port and difference in readiness gate configurations with destination rule subsets",
			helperNewPortInfoMapWithDestinationRule(namespace, name, NewSvcPortTupleSet(SvcPortTuple{Port: 80, TargetPort: "3000"}), namer, true,
				createDestinationRule(name, "v1", "v2")),
			helperNewPortInfoMapWithDestinationRule(namespace, name, NewSvcPortTupleSet(SvcPortTuple{Port: 80, TargetPort: "3000"}, SvcPortTuple{Port: 8080, TargetPort: "9000"}), namer, false,
				createDestinationRule(name, "v3")),
			PortInfoMap{
				PortInfoMapKey{80, "v1"}: PortInfo{
					PortTuple: SvcPortTuple{
						Port:       80,
						TargetPort: "3000",
					},
					Subset:        "v1",
					NegName:       namer.NEGWithSubset(namespace, name, "v1", 80),
					ReadinessGate: true,
				},
				PortInfoMapKey{80, "v2"}: PortInfo{
					PortTuple: SvcPortTuple{
						Port:       80,
						TargetPort: "3000",
					},
					Subset:        "v2",
					NegName:       namer.NEGWithSubset(namespace, name, "v2", 80),
					ReadinessGate: true,
				},
				PortInfoMapKey{80, "v3"}: PortInfo{
					PortTuple: SvcPortTuple{
						Port:       80,
						TargetPort: "3000",
					},
					Subset:        "v3",
					NegName:       namer.NEGWithSubset(namespace, name, "v3", 80),
					ReadinessGate: false,
				},
				PortInfoMapKey{8080, "v3"}: PortInfo{
					PortTuple: SvcPortTuple{
						Port:       8080,
						TargetPort: "9000",
					},
					Subset:        "v3",
					NegName:       namer.NEGWithSubset(namespace, name, "v3", 8080),
					ReadinessGate: false,
				},
			},
			false,
		},
		{
			"error on inconsistent value",
			NewPortInfoMap(namespace, name, NewSvcPortTupleSet(SvcPortTuple{Port: 80, TargetPort: "3000"}), namer, false, nil),
			NewPortInfoMap(namespace, name, NewSvcPortTupleSet(SvcPortTuple{Port: 80, TargetPort: "namedport"}, SvcPortTuple{Port: 8000, TargetPort: "9000"}), namer, false, nil),
			NewPortInfoMap(namespace, name, NewSvcPortTupleSet(SvcPortTuple{Port: 80, TargetPort: "namedport"}, SvcPortTuple{Port: 443, TargetPort: "3000"}, SvcPortTuple{Port: 5000, TargetPort: "6000"}, SvcPortTuple{Port: 8080, TargetPort: "9000"}), namer, false, nil),
			true,
		},
		{
			"error on inconsistent port name",
			NewPortInfoMap(namespace, name, NewSvcPortTupleSet(SvcPortTuple{Port: 80, Name: "foo", TargetPort: "3000"}), namer, false, nil),
			NewPortInfoMap(namespace, name, NewSvcPortTupleSet(SvcPortTuple{Port: 80, Name: "bar", TargetPort: "3000"}, SvcPortTuple{Port: 8000, TargetPort: "9000"}), namer, false, nil),
			PortInfoMap{},
			true,
		},
		{
			"error on inconsistent neg name",
			NewPortInfoMap(namespace, name, NewSvcPortTupleSet(SvcPortTuple{Port: 80, Name: "foo", TargetPort: "3000"}), namer, false, map[SvcPortTuple]string{SvcPortTuple{Port: 80, Name: "foo", TargetPort: "3000"}: "neg-1"}),
			NewPortInfoMap(namespace, name, NewSvcPortTupleSet(SvcPortTuple{Port: 80, Name: "bar", TargetPort: "3000"}, SvcPortTuple{Port: 8000, TargetPort: "9000"}), namer, false, nil),
			PortInfoMap{},
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

func helperNewPortInfoMapWithDestinationRule(namespace, name string, tuples SvcPortTupleSet, namer NetworkEndpointGroupNamer, readinessGate bool,
	destinationRule *istioV1alpha3.DestinationRule) PortInfoMap {
	rsl, _ := NewPortInfoMapWithDestinationRule(namespace, name, tuples, namer, readinessGate, destinationRule)
	return rsl
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
			NewPortInfoMap(namespace, name, NewSvcPortTupleSet(SvcPortTuple{Port: 80, TargetPort: "namedport"}, SvcPortTuple{Port: 443, TargetPort: "3000"}), namer, false, nil),
			PortInfoMap{},
		},
		{
			"non-empty map difference a non-empty map is the non-empty map",
			NewPortInfoMap(namespace, name, NewSvcPortTupleSet(SvcPortTuple{Port: 80, TargetPort: "namedport"}, SvcPortTuple{Port: 443, TargetPort: "3000"}), namer, false, nil),
			PortInfoMap{},
			NewPortInfoMap(namespace, name, NewSvcPortTupleSet(SvcPortTuple{Port: 80, TargetPort: "namedport"}, SvcPortTuple{Port: 443, TargetPort: "3000"}), namer, false, nil),
		},
		{
			"non-empty map difference a non-empty map is the non-empty map 2",
			NewPortInfoMap(namespace, name, NewSvcPortTupleSet(SvcPortTuple{Port: 80, TargetPort: "namedport"}, SvcPortTuple{Port: 443, TargetPort: "3000"}), namer, true, nil),
			PortInfoMap{},
			NewPortInfoMap(namespace, name, NewSvcPortTupleSet(SvcPortTuple{Port: 80, TargetPort: "namedport"}, SvcPortTuple{Port: 443, TargetPort: "3000"}), namer, true, nil),
		},
		{
			"difference of two non-empty maps with the same elements",
			NewPortInfoMap(namespace, name, NewSvcPortTupleSet(SvcPortTuple{Port: 80, TargetPort: "namedport"}, SvcPortTuple{Port: 443, TargetPort: "3000"}), namer, false, nil),
			NewPortInfoMap(namespace, name, NewSvcPortTupleSet(SvcPortTuple{Port: 80, TargetPort: "namedport"}, SvcPortTuple{Port: 443, TargetPort: "3000"}), namer, false, nil),
			PortInfoMap{},
		},
		{
			"difference of two non-empty maps with no elements in common returns p1",
			NewPortInfoMap(namespace, name, NewSvcPortTupleSet(SvcPortTuple{Port: 443, TargetPort: "3000"}, SvcPortTuple{Port: 5000, TargetPort: "6000"}), namer, false, nil),
			NewPortInfoMap(namespace, name, NewSvcPortTupleSet(SvcPortTuple{Port: 80, TargetPort: "namedport"}, SvcPortTuple{Port: 8080, TargetPort: "9000"}), namer, false, nil),
			NewPortInfoMap(namespace, name, NewSvcPortTupleSet(SvcPortTuple{Port: 443, TargetPort: "3000"}, SvcPortTuple{Port: 5000, TargetPort: "6000"}), namer, false, nil),
		},
		{
			"difference of two non-empty maps with elements in common",
			NewPortInfoMap(namespace, name, NewSvcPortTupleSet(SvcPortTuple{Port: 80, TargetPort: "namedport"}, SvcPortTuple{Port: 443, TargetPort: "3000"}, SvcPortTuple{Port: 5000, TargetPort: "6000"}, SvcPortTuple{Port: 8080, TargetPort: "9000"}), namer, false, nil),
			NewPortInfoMap(namespace, name, NewSvcPortTupleSet(SvcPortTuple{Port: 80, TargetPort: "namedport"}, SvcPortTuple{Port: 8080, TargetPort: "9000"}), namer, false, nil),
			NewPortInfoMap(namespace, name, NewSvcPortTupleSet(SvcPortTuple{Port: 443, TargetPort: "3000"}, SvcPortTuple{Port: 5000, TargetPort: "6000"}), namer, false, nil),
		},
		{
			"difference of two non-empty maps with a key in common but different in value",
			NewPortInfoMap(namespace, name, NewSvcPortTupleSet(SvcPortTuple{Port: 80, TargetPort: "namedport"}), namer, false, nil),
			NewPortInfoMap(namespace, name, NewSvcPortTupleSet(SvcPortTuple{Port: 80, TargetPort: "8080"}, SvcPortTuple{Port: 8080, TargetPort: "9000"}), namer, false, nil),
			NewPortInfoMap(namespace, name, NewSvcPortTupleSet(SvcPortTuple{Port: 80, TargetPort: "namedport"}), namer, false, nil),
		},
		{
			"difference of two non-empty maps with 2 keys in common but different in values",
			NewPortInfoMap(namespace, name, NewSvcPortTupleSet(SvcPortTuple{Port: 80, TargetPort: "namedport"}, SvcPortTuple{Port: 443, TargetPort: "8443"}), namer, false, nil),
			NewPortInfoMap(namespace, name, NewSvcPortTupleSet(SvcPortTuple{Port: 80, TargetPort: "8080"}, SvcPortTuple{Port: 443, TargetPort: "9443"}), namer, false, nil),
			NewPortInfoMap(namespace, name, NewSvcPortTupleSet(SvcPortTuple{Port: 80, TargetPort: "namedport"}, SvcPortTuple{Port: 443, TargetPort: "8443"}), namer, false, nil),
		},
		{
			"difference of two non-empty maps with a key in common but different in readiness gate fields",
			NewPortInfoMap(namespace, name, NewSvcPortTupleSet(SvcPortTuple{Port: 80, TargetPort: "8080"}), namer, true, nil),
			NewPortInfoMap(namespace, name, NewSvcPortTupleSet(SvcPortTuple{Port: 80, TargetPort: "8080"}, SvcPortTuple{Port: 8080, TargetPort: "9000"}), namer, false, nil),
			NewPortInfoMap(namespace, name, NewSvcPortTupleSet(SvcPortTuple{Port: 80, TargetPort: "8080"}), namer, true, nil),
		},
		{
			"difference of two non-empty maps with 2 keys in common and 2 more items with different readinessGate",
			NewPortInfoMap(namespace, name, NewSvcPortTupleSet(SvcPortTuple{Port: 80, TargetPort: "namedport"}, SvcPortTuple{Port: 443, TargetPort: "3000"}, SvcPortTuple{Port: 5000, TargetPort: "6000"}, SvcPortTuple{Port: 8080, TargetPort: "9000"}), namer, true, nil),
			NewPortInfoMap(namespace, name, NewSvcPortTupleSet(SvcPortTuple{Port: 80, TargetPort: "namedport"}, SvcPortTuple{Port: 8080, TargetPort: "9000"}), namer, false, nil),
			NewPortInfoMap(namespace, name, NewSvcPortTupleSet(SvcPortTuple{Port: 80, TargetPort: "namedport"}, SvcPortTuple{Port: 443, TargetPort: "3000"}, SvcPortTuple{Port: 5000, TargetPort: "6000"}, SvcPortTuple{Port: 8080, TargetPort: "9000"}), namer, true, nil),
		},
		{
			"difference of two non-empty maps with a key in common but different neg names",
			NewPortInfoMap(namespace, name, NewSvcPortTupleSet(SvcPortTuple{Port: 80, TargetPort: "8080"}), namer, true, map[SvcPortTuple]string{SvcPortTuple{Port: 80, TargetPort: "8080"}: "neg-1"}),
			NewPortInfoMap(namespace, name, NewSvcPortTupleSet(SvcPortTuple{Port: 80, TargetPort: "8080"}, SvcPortTuple{Port: 8080, TargetPort: "9000"}), namer, false, nil),
			NewPortInfoMap(namespace, name, NewSvcPortTupleSet(SvcPortTuple{Port: 80, TargetPort: "8080"}), namer, true, map[SvcPortTuple]string{SvcPortTuple{Port: 80, TargetPort: "8080"}: "neg-1"}),
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
	t.Parallel()

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
			portInfoMap:      PortInfoMap{PortInfoMapKey{80, ""}: PortInfo{NegName: "neg1"}},
			expectPortNegMap: annotations.PortNegMap{"80": "neg1"},
		},
		{
			desc:             "2 ports",
			portInfoMap:      PortInfoMap{PortInfoMapKey{80, ""}: PortInfo{NegName: "neg1"}, PortInfoMapKey{8080, ""}: PortInfo{NegName: "neg2"}},
			expectPortNegMap: annotations.PortNegMap{"80": "neg1", "8080": "neg2"},
		},
		{
			desc:             "3 ports",
			portInfoMap:      PortInfoMap{PortInfoMapKey{80, ""}: PortInfo{NegName: "neg1"}, PortInfoMapKey{443, ""}: PortInfo{NegName: "neg2"}, PortInfoMapKey{8080, ""}: PortInfo{NegName: "neg3"}},
			expectPortNegMap: annotations.PortNegMap{"80": "neg1", "443": "neg2", "8080": "neg3"},
		},
	} {
		res := tc.portInfoMap.ToPortNegMap()
		if !reflect.DeepEqual(res, tc.expectPortNegMap) {
			t.Errorf("For test case %q, expect %v, but got %v", tc.desc, tc.expectPortNegMap, res)
		}
	}
}

func TestNegsWithReadinessGate(t *testing.T) {
	t.Parallel()

	namer := &negNamer{}
	namespace := "namespace"
	name := "name"
	for _, tc := range []struct {
		desc           string
		getPortInfoMap func() PortInfoMap
		expectNegs     sets.String
	}{
		{
			desc:           "empty PortInfoMap",
			getPortInfoMap: func() PortInfoMap { return PortInfoMap{} },
			expectNegs:     sets.NewString(),
		},
		{
			desc: "PortInfoMap with no readiness gate enabled",
			getPortInfoMap: func() PortInfoMap {
				return NewPortInfoMap(namespace, name, NewSvcPortTupleSet(SvcPortTuple{Port: 80, TargetPort: "namedport"}, SvcPortTuple{Port: 443, TargetPort: "3000"}, SvcPortTuple{Port: 5000, TargetPort: "6000"}, SvcPortTuple{Port: 8080, TargetPort: "9000"}), namer, false, nil)
			},
			expectNegs: sets.NewString(),
		},
		{
			desc: "PortInfoMap with all readiness gates enabled",
			getPortInfoMap: func() PortInfoMap {
				return NewPortInfoMap(namespace, name, NewSvcPortTupleSet(SvcPortTuple{Port: 80, TargetPort: "namedport"}, SvcPortTuple{Port: 443, TargetPort: "3000"}, SvcPortTuple{Port: 5000, TargetPort: "6000"}, SvcPortTuple{Port: 8080, TargetPort: "9000"}), namer, true, nil)
			},
			expectNegs: sets.NewString(namer.NEG(namespace, name, 80), namer.NEG(namespace, name, 443), namer.NEG(namespace, name, 5000), namer.NEG(namespace, name, 8080)),
		},
		{
			desc: "PortInfoMap with part of readiness gates enabled",
			getPortInfoMap: func() PortInfoMap {
				p := NewPortInfoMap(namespace, name, NewSvcPortTupleSet(SvcPortTuple{Port: 5000, TargetPort: "6000"}, SvcPortTuple{Port: 8080, TargetPort: "9000"}), namer, true, nil)
				p.Merge(NewPortInfoMap(namespace, name, NewSvcPortTupleSet(SvcPortTuple{Port: 80, TargetPort: "namedport"}, SvcPortTuple{Port: 443, TargetPort: "3000"}), namer, false, nil))
				return p
			},
			expectNegs: sets.NewString(namer.NEG(namespace, name, 5000), namer.NEG(namespace, name, 8080)),
		},
	} {
		negs := tc.getPortInfoMap().NegsWithReadinessGate()
		if !negs.Equal(tc.expectNegs) {
			t.Errorf("For test case %q, expect %v, but got %v", tc.desc, tc.expectNegs, negs)
		}
	}
}

func TestCustomNamedNegs(t *testing.T) {
	var (
		svcNamespace  = "namespace"
		svcName       = "svc-name"
		negName1      = "neg1"
		negName2      = "neg2"
		port1         = int32(80)
		port2         = int32(432)
		targetPort1   = "3000"
		targetPort2   = "3001"
		namer         = &negNamer{}
		svcPortTuple1 = SvcPortTuple{Port: port1, TargetPort: targetPort1}
		svcPortTuple2 = SvcPortTuple{Port: port2, TargetPort: targetPort2}
	)
	testcases := []struct {
		desc                string
		svcPortTuples       SvcPortTupleSet
		customNamedNegs     map[SvcPortTuple]string
		expectedPortInfoMap PortInfoMap
	}{
		{
			desc:          "no custom named negs",
			svcPortTuples: NewSvcPortTupleSet(svcPortTuple1, svcPortTuple2),
			expectedPortInfoMap: PortInfoMap{
				PortInfoMapKey{port1, ""}: PortInfo{PortTuple: svcPortTuple1, NegName: namer.NEG(svcNamespace, svcName, port1), ReadinessGate: false},
				PortInfoMapKey{port2, ""}: PortInfo{PortTuple: svcPortTuple2, NegName: namer.NEG(svcNamespace, svcName, port2), ReadinessGate: false},
			},
		},
		{
			desc:            "all custom named negs",
			svcPortTuples:   NewSvcPortTupleSet(svcPortTuple1, svcPortTuple2),
			customNamedNegs: map[SvcPortTuple]string{svcPortTuple1: negName1, svcPortTuple2: negName2},
			expectedPortInfoMap: PortInfoMap{
				PortInfoMapKey{port1, ""}: PortInfo{PortTuple: svcPortTuple1, NegName: negName1, ReadinessGate: false},
				PortInfoMapKey{port2, ""}: PortInfo{PortTuple: svcPortTuple2, NegName: negName2, ReadinessGate: false},
			},
		},
		{
			desc:            "1 custom named negs, 1 autogenerated named neg",
			svcPortTuples:   NewSvcPortTupleSet(svcPortTuple1, svcPortTuple2),
			customNamedNegs: map[SvcPortTuple]string{svcPortTuple1: negName1},
			expectedPortInfoMap: PortInfoMap{
				PortInfoMapKey{port1, ""}: PortInfo{PortTuple: svcPortTuple1, NegName: negName1, ReadinessGate: false},
				PortInfoMapKey{port2, ""}: PortInfo{PortTuple: svcPortTuple2, NegName: namer.NEG(svcNamespace, svcName, port2), ReadinessGate: false},
			},
		},
	}

	for _, tc := range testcases {
		t.Run(tc.desc, func(t *testing.T) {
			result := NewPortInfoMap(svcNamespace, svcName, tc.svcPortTuples, namer, false, tc.customNamedNegs)
			if !reflect.DeepEqual(tc.expectedPortInfoMap, result) {
				t.Errorf("Expected portInfoMap to equal: %v; got: %v", tc.expectedPortInfoMap, result)
			}
		})
	}
}

func TestEndpointsDataFromEndpoints(t *testing.T) {
	t.Parallel()
	instance1 := TestInstance1
	instance2 := TestInstance2
	instance4 := TestInstance4
	testServiceName := "service"
	testServiceNamespace := "namespace"
	testNamedPort := "port1"
	endpoints := &v1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testServiceName,
			Namespace: testServiceNamespace,
		},
		Subsets: []v1.EndpointSubset{
			{
				Addresses: []v1.EndpointAddress{
					{
						IP:       "10.100.1.1",
						NodeName: &instance1,
						TargetRef: &v1.ObjectReference{
							Namespace: testServiceNamespace,
							Name:      "pod1",
						},
					},
					{
						IP:       "10.100.1.2",
						NodeName: &instance1,
						TargetRef: &v1.ObjectReference{
							Namespace: testServiceNamespace,
							Name:      "pod2",
						},
					},
				},
				NotReadyAddresses: []v1.EndpointAddress{
					{
						IP:       "10.100.1.3",
						NodeName: &instance1,
						TargetRef: &v1.ObjectReference{
							Namespace: testServiceNamespace,
							Name:      "pod5",
						},
					},
					{
						IP:       "10.100.1.4",
						NodeName: &instance1,
						TargetRef: &v1.ObjectReference{
							Namespace: testServiceNamespace,
							Name:      "pod6",
						},
					},
				},
				Ports: []v1.EndpointPort{
					{
						Name:     "",
						Port:     int32(80),
						Protocol: v1.ProtocolTCP,
					},
				},
			},
			{
				Addresses: []v1.EndpointAddress{
					{
						IP:       "10.100.2.2",
						NodeName: &instance2,
						TargetRef: &v1.ObjectReference{
							Namespace: testServiceNamespace,
							Name:      "pod7",
						},
					},
					{
						IP:       "10.100.4.1",
						NodeName: &instance4,
						TargetRef: &v1.ObjectReference{
							Namespace: testServiceNamespace,
							Name:      "pod8",
						},
					},
				},
				NotReadyAddresses: []v1.EndpointAddress{
					{
						IP:       "10.100.4.3",
						NodeName: &instance4,
						TargetRef: &v1.ObjectReference{
							Namespace: testServiceNamespace,
							Name:      "pod9",
						},
					},
				},
				Ports: []v1.EndpointPort{
					{
						Name:     testNamedPort,
						Port:     int32(81),
						Protocol: v1.ProtocolTCP,
					},
				},
			},
		},
	}
	endpointsData := EndpointsDataFromEndpoints(endpoints)

	if len(endpointsData) != 2 {
		t.Errorf("Expected the same number of endpoints subsets and endpoints data, got %d endpoints data for 2 subsets", len(endpointsData))
	}
	for i, subset := range endpoints.Subsets {
		for j, port := range subset.Ports {
			ValidatePortData(endpointsData[i].Ports[j], port.Port, port.Name, t)
		}
		ValidateAddressDataForEndpointsAddresses(endpointsData[i].Addresses, subset.Addresses, true, t)
		ValidateAddressDataForEndpointsAddresses(endpointsData[i].Addresses, subset.NotReadyAddresses, false, t)
		if len(endpointsData[i].Addresses) != len(subset.Addresses)+len(subset.NotReadyAddresses) {
			t.Errorf("Unexpected len of endpointsData adresses, got %d, expected %d", len(endpointsData[i].Addresses), len(subset.Addresses)+len(subset.NotReadyAddresses))
		}
	}
}

func TestEndpointsDataFromEndpointSlices(t *testing.T) {
	t.Parallel()
	instance1 := TestInstance1
	instance2 := TestInstance2
	instance4 := TestInstance4
	testServiceName := "service"
	testServiceNamespace := "namespace"
	testNamedPort := "port1"
	notReady := false
	terminating := true
	emptyNamedPort := ""
	port80 := int32(80)
	port81 := int32(81)
	protocolTCP := v1.ProtocolTCP
	endpointSlices := []*discovery.EndpointSlice{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      testServiceName + "-1",
				Namespace: testServiceNamespace,
			},
			AddressType: "IPv4",
			Endpoints: []discovery.Endpoint{
				{
					Addresses: []string{"10.100.1.1"},
					NodeName:  &instance1,
					TargetRef: &v1.ObjectReference{
						Namespace: testServiceNamespace,
						Name:      "pod1",
					},
				},
				{
					Addresses: []string{"10.100.1.2"},
					NodeName:  &instance1,
					TargetRef: &v1.ObjectReference{
						Namespace: testServiceNamespace,
						Name:      "pod2",
					},
				},
				{
					Addresses: []string{"10.100.1.3"},
					NodeName:  &instance1,
					TargetRef: &v1.ObjectReference{
						Namespace: testServiceNamespace,
						Name:      "pod5",
					},
					Conditions: discovery.EndpointConditions{Ready: &notReady},
				},
				{
					Addresses: []string{"10.100.1.4"},
					NodeName:  &instance1,
					TargetRef: &v1.ObjectReference{
						Namespace: testServiceNamespace,
						Name:      "pod6",
					},
					Conditions: discovery.EndpointConditions{Ready: &notReady},
				},
				{
					Addresses: []string{"10.100.1.5"},
					NodeName:  &instance1,
					TargetRef: &v1.ObjectReference{
						Namespace: testServiceNamespace,
						Name:      "pod9",
					},
					Conditions: discovery.EndpointConditions{Terminating: &terminating},
				},
			},
			Ports: []discovery.EndpointPort{
				{
					Name:     &emptyNamedPort,
					Port:     &port80,
					Protocol: &protocolTCP,
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      testServiceName + "-2",
				Namespace: testServiceNamespace,
			},
			AddressType: "IPv4",
			Endpoints: []discovery.Endpoint{
				{
					Addresses: []string{"10.100.2.2"},
					NodeName:  &instance2,
					TargetRef: &v1.ObjectReference{
						Namespace: testServiceNamespace,
						Name:      "pod7",
					},
				},
				{
					Addresses: []string{"10.100.4.1"},
					NodeName:  &instance4,
					TargetRef: &v1.ObjectReference{
						Namespace: testServiceNamespace,
						Name:      "pod8",
					},
				},
				{
					Addresses: []string{"10.100.4.3"},
					NodeName:  &instance4,
					TargetRef: &v1.ObjectReference{
						Namespace: testServiceNamespace,
						Name:      "pod9",
					},
					Conditions: discovery.EndpointConditions{Ready: &notReady},
				},
			},
			Ports: []discovery.EndpointPort{
				{
					Name:     &testNamedPort,
					Port:     &port81,
					Protocol: &protocolTCP,
				},
			},
		},
	}

	endpointsData := EndpointsDataFromEndpointSlices(endpointSlices)

	if len(endpointsData) != 2 {
		t.Errorf("Expected the same number of endpoints subsets and endpoints data, got %d endpoints data for 2 subsets", len(endpointsData))
	}
	for i, slice := range endpointSlices {
		for j, port := range slice.Ports {
			ValidatePortData(endpointsData[i].Ports[j], *port.Port, *port.Name, t)
		}
		terminatingEndpointsNumber := 0
		for _, endpoint := range slice.Endpoints {
			found := CheckIfAddressIsPresentInData(endpointsData[i].Addresses, endpoint.Conditions.Ready == nil || *endpoint.Conditions.Ready, endpoint.Addresses[0], endpoint.TargetRef, endpoint.NodeName)
			if endpoint.Conditions.Terminating != nil && *endpoint.Conditions.Terminating {
				terminatingEndpointsNumber++
				if found {
					t.Errorf("Terminating endpoint %v is present in endpoints data %v", endpoint, endpointsData[i].Addresses)
				}
			} else {
				if !found {
					t.Errorf("Endpoint %v not found in endpoints data %v", endpoint, endpointsData[i].Addresses)
				}
			}
		}
		if len(endpointsData[i].Addresses) != len(slice.Endpoints)-terminatingEndpointsNumber {
			t.Errorf("Unexpected len of endpointsData adresses, got %d, expected %d", len(endpointsData[i].Addresses), len(slice.Endpoints)-1)
		}
	}
}

func ValidatePortData(portData PortData, port int32, name string, t *testing.T) {
	if portData.Port != port {
		t.Errorf("Invalid port number, got %d expected %d", portData.Port, port)
	}
	if portData.Name != name {
		t.Errorf("Invalid port name, got %s expected %s", portData.Name, name)
	}
}

func CheckIfAddressIsPresentInData(addressData []AddressData, ready bool, address string, targetRef *v1.ObjectReference, nodeName *string) bool {
	for _, data := range addressData {
		if data.Ready == ready && len(data.Addresses) == 1 && data.Addresses[0] == address && data.TargetRef == targetRef && data.NodeName == nodeName {
			return true
		}
	}
	return false
}

func ValidateAddressDataForEndpointsAddresses(addressData []AddressData, addresses []v1.EndpointAddress, ready bool, t *testing.T) {
	for _, addr := range addresses {
		found := CheckIfAddressIsPresentInData(addressData, ready, addr.IP, addr.TargetRef, addr.NodeName)
		if !found {
			t.Errorf("Endpoint address %v couldn't be found in Address Data %v", addr, addressData)
		}
	}
}
