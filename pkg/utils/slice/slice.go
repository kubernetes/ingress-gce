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

// Contains checks if a given slice contains the provided element.
// If a modifier func is provided, it is called with the slice item before the comparison.
func Contains[T comparable](slice []T, elem T, modifier func(elem T) T) bool {
	for _, item := range slice {
		if item == elem {
			return true
		}
		if modifier != nil && modifier(item) == elem {
			return true
		}
	}
	return false
}

// Remove returns a newly created slice that contains all items from the initial
// slice that are not equal to elem and modifier(item) in case modifier func is provided
// where item is an item in the slice.
func Remove[T comparable](slice []T, elem T, modifier func(elem T) T) []T {
	newSlice := make([]T, 0)
	for _, item := range slice {
		if item == elem {
			continue
		}
		if modifier != nil && modifier(item) == elem {
			continue
		}
		newSlice = append(newSlice, item)
	}
	if len(newSlice) == 0 {
		// Sanitize for unit tests so we don't need to distinguish empty array
		// and nil.
		newSlice = nil
	}
	return newSlice
}
