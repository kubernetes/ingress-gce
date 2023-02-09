/*
Copyright 2021 The Kubernetes Authors.

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

package slice

import (
	"reflect"
	"testing"
)

func TestContains(t *testing.T) {
	src := []string{"aa", "bb", "cc"}
	if !Contains(src, "bb", nil) {
		t.Errorf("Contains didn't find the string as expected")
	}

	modifier := func(s string) string {
		if s == "cc" {
			return "ee"
		}
		return s
	}
	if !Contains(src, "ee", modifier) {
		t.Errorf("Contains didn't find the string by modifier")
	}
}

func TestRemove(t *testing.T) {
	modifier := func(s string) string {
		if s == "ab" {
			return "ee"
		}
		return s
	}
	tests := []struct {
		testName string
		input    []string
		remove   string
		modifier func(s string) string
		want     []string
	}{
		{
			testName: "Nil input slice",
			input:    nil,
			remove:   "",
			modifier: nil,
			want:     nil,
		},
		{
			testName: "Slice doesn't contain the string",
			input:    []string{"a", "ab", "cdef"},
			remove:   "NotPresentInSlice",
			modifier: nil,
			want:     []string{"a", "ab", "cdef"},
		},
		{
			testName: "All strings removed, result is nil",
			input:    []string{"a"},
			remove:   "a",
			modifier: nil,
			want:     nil,
		},
		{
			testName: "No modifier func, one string removed",
			input:    []string{"a", "ab", "cdef"},
			remove:   "ab",
			modifier: nil,
			want:     []string{"a", "cdef"},
		},
		{
			testName: "No modifier func, all(three) strings removed",
			input:    []string{"ab", "a", "ab", "cdef", "ab"},
			remove:   "ab",
			modifier: nil,
			want:     []string{"a", "cdef"},
		},
		{
			testName: "Removed both the string and the modifier func result",
			input:    []string{"a", "cd", "ab", "ee"},
			remove:   "ee",
			modifier: modifier,
			want:     []string{"a", "cd"},
		},
	}
	for _, tt := range tests {
		if got := Remove(tt.input, tt.remove, tt.modifier); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%v: Remove(%v, %q, %T) = %v WANT %v", tt.testName, tt.input, tt.remove, tt.modifier, got, tt.want)
		}
	}
}
