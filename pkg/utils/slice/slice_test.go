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

	srcInts := []int{1, 2, 3}
	if !Contains(srcInts, 2, nil) {
		t.Errorf("Contains didn't find the int as expected")
	}

	modifierInt := func(i int) int {
		if i == 3 {
			return 5
		}
		return i
	}
	if !Contains(srcInts, 5, modifierInt) {
		t.Errorf("Contains didn't find the int by modifier")
	}
}

func TestRemove(t *testing.T) {
	modifier := func(s string) string {
		if s == "ab" {
			return "ee"
		}
		return s
	}
	modifierInt := func(i int) int {
		if i == 3 {
			return 5
		}
		return i
	}
	tests := []struct {
		testName    string
		input       []string
		inputInts   []int
		remove      string
		removeInt   int
		modifier    func(s string) string
		modifierInt func(i int) int
		want        []string
		wantInts    []int
	}{
		{
			testName:    "Nil input slice",
			input:       nil,
			inputInts:   nil,
			remove:      "",
			removeInt:   0,
			modifier:    nil,
			modifierInt: nil,
			want:        nil,
			wantInts:    nil,
		},
		{
			testName:    "Slice doesn't contain the element",
			input:       []string{"a", "ab", "cdef"},
			inputInts:   []int{1, 2, 3},
			remove:      "NotPresentInSlice",
			removeInt:   0,
			modifier:    nil,
			modifierInt: nil,
			want:        []string{"a", "ab", "cdef"},
			wantInts:    []int{1, 2, 3},
		},
		{
			testName:    "All elements removed, result is nil",
			input:       []string{"a"},
			inputInts:   []int{1},
			remove:      "a",
			removeInt:   1,
			modifier:    nil,
			modifierInt: nil,
			want:        nil,
			wantInts:    nil,
		},
		{
			testName:    "No modifier func, one element removed",
			input:       []string{"a", "ab", "cdef"},
			inputInts:   []int{1, 2, 3},
			remove:      "ab",
			removeInt:   2,
			modifier:    nil,
			modifierInt: nil,
			want:        []string{"a", "cdef"},
			wantInts:    []int{1, 3},
		},
		{
			testName:    "No modifier func, all(three) elements removed",
			input:       []string{"ab", "a", "ab", "cdef", "ab"},
			inputInts:   []int{2, 1, 2, 3, 2},
			remove:      "ab",
			removeInt:   2,
			modifier:    nil,
			modifierInt: nil,
			want:        []string{"a", "cdef"},
			wantInts:    []int{1, 3},
		},
		{
			testName:    "Removed both the element and the modifier func result",
			input:       []string{"a", "cd", "ab", "ee"},
			inputInts:   []int{1, 2, 3, 5},
			remove:      "ee",
			removeInt:   5,
			modifier:    modifier,
			modifierInt: modifierInt,
			want:        []string{"a", "cd"},
			wantInts:    []int{1, 2},
		},
	}
	for _, tt := range tests {
		if got := Remove(tt.input, tt.remove, tt.modifier); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%v: Remove(%v, %q, %T) = %v WANT %v", tt.testName, tt.input, tt.remove, tt.modifier, got, tt.want)
		}
		if got := Remove(tt.inputInts, tt.removeInt, tt.modifierInt); !reflect.DeepEqual(got, tt.wantInts) {
			t.Errorf("%v: Remove(%v, %q, %T) = %v WANT %v", tt.testName, tt.inputInts, tt.removeInt, tt.modifierInt, got, tt.wantInts)
		}
	}
}
