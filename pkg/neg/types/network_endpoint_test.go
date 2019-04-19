/*
Copyright 2019 The Kubernetes Authors.

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
	"testing"
)

func TestNetworkEndpointSetSet(t *testing.T) {
	s := NetworkEndpointSet{}
	s2 := NetworkEndpointSet{}
	if len(s) != 0 {
		t.Errorf("Expected len=0: %d", len(s))
	}
	s.Insert(genNetworkEndpoint("a"), genNetworkEndpoint("b"))
	if len(s) != 2 {
		t.Errorf("Expected len=2: %d", len(s))
	}
	s.Insert(genNetworkEndpoint("c"))
	if s.Has(genNetworkEndpoint("d")) {
		t.Errorf("Unexpected contents: %#v", s)
	}
	if !s.Has(genNetworkEndpoint("a")) {
		t.Errorf("Missing contents: %#v", s)
	}
	s.Delete(genNetworkEndpoint("a"))
	if s.Has(genNetworkEndpoint("a")) {
		t.Errorf("Unexpected contents: %#v", s)
	}
	s.Insert(genNetworkEndpoint("a"))
	if s.HasAll(genNetworkEndpoint("a"), genNetworkEndpoint("b"), genNetworkEndpoint("d")) {
		t.Errorf("Unexpected contents: %#v", s)
	}
	if !s.HasAll(genNetworkEndpoint("a"), genNetworkEndpoint("b")) {
		t.Errorf("Missing contents: %#v", s)
	}
	s2.Insert(genNetworkEndpoint("a"), genNetworkEndpoint("b"), genNetworkEndpoint("d"))
	if s.IsSuperset(s2) {
		t.Errorf("Unexpected contents: %#v", s)
	}
	s2.Delete(genNetworkEndpoint("d"))
	if !s.IsSuperset(s2) {
		t.Errorf("Missing contents: %#v", s)
	}
}

func TestNetworkEndpointSetSetDeleteMultiples(t *testing.T) {
	s := NetworkEndpointSet{}
	s.Insert(genNetworkEndpoint("a"), genNetworkEndpoint("b"), genNetworkEndpoint("c"))
	if len(s) != 3 {
		t.Errorf("Expected len=3: %d", len(s))
	}

	s.Delete(genNetworkEndpoint("a"), genNetworkEndpoint("c"))
	if len(s) != 1 {
		t.Errorf("Expected len=1: %d", len(s))
	}
	if s.Has(genNetworkEndpoint("a")) {
		t.Errorf("Unexpected contents: %#v", s)
	}
	if s.Has(genNetworkEndpoint("c")) {
		t.Errorf("Unexpected contents: %#v", s)
	}
	if !s.Has(genNetworkEndpoint("b")) {
		t.Errorf("Missing contents: %#v", s)
	}

}

func TestNewNetworkEndpointSetSet(t *testing.T) {
	s := NewNetworkEndpointSet(genNetworkEndpoint("a"), genNetworkEndpoint("b"), genNetworkEndpoint("c"))
	if len(s) != 3 {
		t.Errorf("Expected len=3: %d", len(s))
	}
	if !s.Has(genNetworkEndpoint("a")) || !s.Has(genNetworkEndpoint("b")) || !s.Has(genNetworkEndpoint("c")) {
		t.Errorf("Unexpected contents: %#v", s)
	}
}

func TestNetworkEndpointSetSetList(t *testing.T) {
	s := NewNetworkEndpointSet(genNetworkEndpoint("z"), genNetworkEndpoint("y"), genNetworkEndpoint("x"), genNetworkEndpoint("a"))
	list := s.List()
	if len(s) != 4 {
		t.Errorf("Expected len=4: %d", len(s))
	}

	for _, ne := range list {
		if !s.Has(ne) {
			t.Errorf("Item %v from the list is not in set", ne)
		}
	}
}

func TestNetworkEndpointSetSetDifference(t *testing.T) {
	a := NewNetworkEndpointSet(genNetworkEndpoint("1"), genNetworkEndpoint("2"), genNetworkEndpoint("3"))
	b := NewNetworkEndpointSet(genNetworkEndpoint("1"), genNetworkEndpoint("2"), genNetworkEndpoint("4"), genNetworkEndpoint("5"))
	c := a.Difference(b)
	d := b.Difference(a)
	if len(c) != 1 {
		t.Errorf("Expected len=1: %d", len(c))
	}
	if !c.Has(genNetworkEndpoint("3")) {
		t.Errorf("Unexpected contents: %#v", c.List())
	}
	if len(d) != 2 {
		t.Errorf("Expected len=2: %d", len(d))
	}
	if !d.Has(genNetworkEndpoint("4")) || !d.Has(genNetworkEndpoint("5")) {
		t.Errorf("Unexpected contents: %#v", d.List())
	}
}

func TestNetworkEndpointSetSetHasAny(t *testing.T) {
	a := NewNetworkEndpointSet(genNetworkEndpoint("1"), genNetworkEndpoint("2"), genNetworkEndpoint("3"))

	if !a.HasAny(genNetworkEndpoint("1"), genNetworkEndpoint("4")) {
		t.Errorf("expected true, got false")
	}

	if a.HasAny(genNetworkEndpoint("0"), genNetworkEndpoint("4")) {
		t.Errorf("expected false, got true")
	}
}

func TestNetworkEndpointSetSetEquals(t *testing.T) {
	// Simple case (order doesn't matter)
	a := NewNetworkEndpointSet(genNetworkEndpoint("1"), genNetworkEndpoint("2"))
	b := NewNetworkEndpointSet(genNetworkEndpoint("2"), genNetworkEndpoint("1"))
	if !a.Equal(b) {
		t.Errorf("Expected to be equal: %v vs %v", a, b)
	}

	// It is a set; duplicates are ignored
	b = NewNetworkEndpointSet(genNetworkEndpoint("2"), genNetworkEndpoint("2"), genNetworkEndpoint("1"))
	if !a.Equal(b) {
		t.Errorf("Expected to be equal: %v vs %v", a, b)
	}

	// Edge cases around empty sets / empty NetworkEndpoints
	a = NewNetworkEndpointSet()
	b = NewNetworkEndpointSet()
	if !a.Equal(b) {
		t.Errorf("Expected to be equal: %v vs %v", a, b)
	}

	b = NewNetworkEndpointSet(genNetworkEndpoint("1"), genNetworkEndpoint("2"), genNetworkEndpoint("3"))
	if a.Equal(b) {
		t.Errorf("Expected to be not-equal: %v vs %v", a, b)
	}

	b = NewNetworkEndpointSet(genNetworkEndpoint("1"), genNetworkEndpoint("2"), genNetworkEndpoint(""))
	if a.Equal(b) {
		t.Errorf("Expected to be not-equal: %v vs %v", a, b)
	}

	// Check for equality after mutation
	a = NewNetworkEndpointSet()
	a.Insert(genNetworkEndpoint("1"))
	if a.Equal(b) {
		t.Errorf("Expected to be not-equal: %v vs %v", a, b)
	}

	a.Insert(genNetworkEndpoint("2"))
	if a.Equal(b) {
		t.Errorf("Expected to be not-equal: %v vs %v", a, b)
	}

	a.Insert(genNetworkEndpoint(""))
	if !a.Equal(b) {
		t.Errorf("Expected to be equal: %v vs %v", a, b)
	}

	a.Delete(genNetworkEndpoint(""))
	if a.Equal(b) {
		t.Errorf("Expected to be not-equal: %v vs %v", a, b)
	}
}

func TestNetworkEndpointSetUnion(t *testing.T) {
	tests := []struct {
		s1       NetworkEndpointSet
		s2       NetworkEndpointSet
		expected NetworkEndpointSet
	}{
		{
			NewNetworkEndpointSet(genNetworkEndpoint("1"), genNetworkEndpoint("2"), genNetworkEndpoint("3"), genNetworkEndpoint("4")),
			NewNetworkEndpointSet(genNetworkEndpoint("3"), genNetworkEndpoint("4"), genNetworkEndpoint("5"), genNetworkEndpoint("6")),
			NewNetworkEndpointSet(genNetworkEndpoint("1"), genNetworkEndpoint("2"), genNetworkEndpoint("3"), genNetworkEndpoint("4"), genNetworkEndpoint("5"), genNetworkEndpoint("6")),
		},
		{
			NewNetworkEndpointSet(genNetworkEndpoint("1"), genNetworkEndpoint("2"), genNetworkEndpoint("3"), genNetworkEndpoint("4")),
			NewNetworkEndpointSet(),
			NewNetworkEndpointSet(genNetworkEndpoint("1"), genNetworkEndpoint("2"), genNetworkEndpoint("3"), genNetworkEndpoint("4")),
		},
		{
			NewNetworkEndpointSet(),
			NewNetworkEndpointSet(genNetworkEndpoint("1"), genNetworkEndpoint("2"), genNetworkEndpoint("3"), genNetworkEndpoint("4")),
			NewNetworkEndpointSet(genNetworkEndpoint("1"), genNetworkEndpoint("2"), genNetworkEndpoint("3"), genNetworkEndpoint("4")),
		},
		{
			NewNetworkEndpointSet(),
			NewNetworkEndpointSet(),
			NewNetworkEndpointSet(),
		},
	}

	for _, test := range tests {
		union := test.s1.Union(test.s2)
		if union.Len() != test.expected.Len() {
			t.Errorf("Expected union.Len()=%d but got %d", test.expected.Len(), union.Len())
		}

		if !union.Equal(test.expected) {
			t.Errorf("Expected union.Equal(expected) but not true.  union:%v expected:%v", union.List(), test.expected.List())
		}
	}
}

func TestNetworkEndpointSetIntersection(t *testing.T) {
	tests := []struct {
		s1       NetworkEndpointSet
		s2       NetworkEndpointSet
		expected NetworkEndpointSet
	}{
		{
			NewNetworkEndpointSet(genNetworkEndpoint("1"), genNetworkEndpoint("2"), genNetworkEndpoint("3"), genNetworkEndpoint("4")),
			NewNetworkEndpointSet(genNetworkEndpoint("3"), genNetworkEndpoint("4"), genNetworkEndpoint("5"), genNetworkEndpoint("6")),
			NewNetworkEndpointSet(genNetworkEndpoint("3"), genNetworkEndpoint("4")),
		},
		{
			NewNetworkEndpointSet(genNetworkEndpoint("1"), genNetworkEndpoint("2"), genNetworkEndpoint("3"), genNetworkEndpoint("4")),
			NewNetworkEndpointSet(genNetworkEndpoint("1"), genNetworkEndpoint("2"), genNetworkEndpoint("3"), genNetworkEndpoint("4")),
			NewNetworkEndpointSet(genNetworkEndpoint("1"), genNetworkEndpoint("2"), genNetworkEndpoint("3"), genNetworkEndpoint("4")),
		},
		{
			NewNetworkEndpointSet(genNetworkEndpoint("1"), genNetworkEndpoint("2"), genNetworkEndpoint("3"), genNetworkEndpoint("4")),
			NewNetworkEndpointSet(),
			NewNetworkEndpointSet(),
		},
		{
			NewNetworkEndpointSet(),
			NewNetworkEndpointSet(genNetworkEndpoint("1"), genNetworkEndpoint("2"), genNetworkEndpoint("3"), genNetworkEndpoint("4")),
			NewNetworkEndpointSet(),
		},
		{
			NewNetworkEndpointSet(),
			NewNetworkEndpointSet(),
			NewNetworkEndpointSet(),
		},
	}

	for _, test := range tests {
		intersection := test.s1.Intersection(test.s2)
		if intersection.Len() != test.expected.Len() {
			t.Errorf("Expected intersection.Len()=%d but got %d", test.expected.Len(), intersection.Len())
		}

		if !intersection.Equal(test.expected) {
			t.Errorf("Expected intersection.Equal(expected) but not true.  intersection:%v expected:%v", intersection.List(), test.expected.List())
		}
	}
}

func genNetworkEndpoint(key string) NetworkEndpoint {
	return NetworkEndpoint{
		IP:   key,
		Port: key,
		Node: key,
	}
}
