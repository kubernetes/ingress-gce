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
	"reflect"
)

// NetworkEndpoint contains the essential information for each network endpoint in a NEG
type NetworkEndpoint struct {
	IP   string
	Port string
	Node string
}

// sets.NetworkEndpointSet is a set of NetworkEndpoints, implemented via map[NetworkEndpoint]struct{} for minimal memory consumption.
type NetworkEndpointSet map[NetworkEndpoint]struct{}

// NewNetworkEndpointSet creates a NetworkEndpointSet from a list of values.
func NewNetworkEndpointSet(items ...NetworkEndpoint) NetworkEndpointSet {
	ss := NetworkEndpointSet{}
	ss.Insert(items...)
	return ss
}

// NetworkEndpointSetKeySet creates a NetworkEndpointSet from a keys of a map[NetworkEndpoint](? extends interface{}).
// If the value passed in is not actually a map, this will panic.
func NetworkEndpointSetKeySet(theMap interface{}) NetworkEndpointSet {
	v := reflect.ValueOf(theMap)
	ret := NetworkEndpointSet{}

	for _, keyValue := range v.MapKeys() {
		ret.Insert(keyValue.Interface().(NetworkEndpoint))
	}
	return ret
}

// Insert adds items to the set.
func (s NetworkEndpointSet) Insert(items ...NetworkEndpoint) {
	for _, item := range items {
		s[item] = struct{}{}
	}
}

// Delete removes all items from the set.
func (s NetworkEndpointSet) Delete(items ...NetworkEndpoint) {
	for _, item := range items {
		delete(s, item)
	}
}

// Has returns true if and only if item is contained in the set.
func (s NetworkEndpointSet) Has(item NetworkEndpoint) bool {
	_, contained := s[item]
	return contained
}

// HasAll returns true if and only if all items are contained in the set.
func (s NetworkEndpointSet) HasAll(items ...NetworkEndpoint) bool {
	for _, item := range items {
		if !s.Has(item) {
			return false
		}
	}
	return true
}

// HasAny returns true if any items are contained in the set.
func (s NetworkEndpointSet) HasAny(items ...NetworkEndpoint) bool {
	for _, item := range items {
		if s.Has(item) {
			return true
		}
	}
	return false
}

// Difference returns a set of objects that are not in s2
// For example:
// s1 = {a1, a2, a3}
// s2 = {a1, a2, a4, a5}
// s1.Difference(s2) = {a3}
// s2.Difference(s1) = {a4, a5}
func (s NetworkEndpointSet) Difference(s2 NetworkEndpointSet) NetworkEndpointSet {
	result := NewNetworkEndpointSet()
	for key := range s {
		if !s2.Has(key) {
			result.Insert(key)
		}
	}
	return result
}

// Union returns a new set which includes items in either s1 or s2.
// For example:
// s1 = {a1, a2}
// s2 = {a3, a4}
// s1.Union(s2) = {a1, a2, a3, a4}
// s2.Union(s1) = {a1, a2, a3, a4}
func (s1 NetworkEndpointSet) Union(s2 NetworkEndpointSet) NetworkEndpointSet {
	result := NewNetworkEndpointSet()
	for key := range s1 {
		result.Insert(key)
	}
	for key := range s2 {
		result.Insert(key)
	}
	return result
}

// Intersection returns a new set which includes the item in BOTH s1 and s2
// For example:
// s1 = {a1, a2}
// s2 = {a2, a3}
// s1.Intersection(s2) = {a2}
func (s1 NetworkEndpointSet) Intersection(s2 NetworkEndpointSet) NetworkEndpointSet {
	var walk, other NetworkEndpointSet
	result := NewNetworkEndpointSet()
	if s1.Len() < s2.Len() {
		walk = s1
		other = s2
	} else {
		walk = s2
		other = s1
	}
	for key := range walk {
		if other.Has(key) {
			result.Insert(key)
		}
	}
	return result
}

// IsSuperset returns true if and only if s1 is a superset of s2.
func (s1 NetworkEndpointSet) IsSuperset(s2 NetworkEndpointSet) bool {
	for item := range s2 {
		if !s1.Has(item) {
			return false
		}
	}
	return true
}

// Equal returns true if and only if s1 is equal (as a set) to s2.
// Two sets are equal if their membership is identical.
// (In practice, this means same elements, order doesn't matter)
func (s1 NetworkEndpointSet) Equal(s2 NetworkEndpointSet) bool {
	return len(s1) == len(s2) && s1.IsSuperset(s2)
}

// List returns the slice with contents in random order.
func (s NetworkEndpointSet) List() []NetworkEndpoint {
	res := make([]NetworkEndpoint, 0, len(s))
	for key := range s {
		res = append(res, key)
	}
	return []NetworkEndpoint(res)
}

// Returns a single element from the set.
func (s NetworkEndpointSet) PopAny() (NetworkEndpoint, bool) {
	for key := range s {
		s.Delete(key)
		return key, true
	}
	var zeroValue NetworkEndpoint
	return zeroValue, false
}

// Len returns the size of the set.
func (s NetworkEndpointSet) Len() int {
	return len(s)
}
