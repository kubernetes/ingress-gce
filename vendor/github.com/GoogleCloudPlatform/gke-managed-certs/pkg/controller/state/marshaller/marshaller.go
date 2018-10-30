/*
Copyright 2018 Google LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package marshaller provides utility functions for converting a map into a map which can be stored as a Kubernetes ConfigMap.
package marshaller

import (
	"encoding/json"
	"fmt"
)

// mapEntry stores an entry in a map being marshalled to JSON.
type mapEntry struct {
	Key   string
	Value string
}

// Transforms a map into a map with keys acceptable for a ConfigMap and values that encode entries of initial map.
func Marshal(m map[string]string) map[string]string {
	result := make(map[string]string)
	i := 0
	for k, v := range m {
		i++
		key := fmt.Sprintf("%d", i)
		value, _ := json.Marshal(mapEntry{
			Key:   k,
			Value: v,
		})
		result[key] = string(value)
	}

	return result
}

// Transforms an encoded map back into initial map.
func Unmarshal(m map[string]string) map[string]string {
	result := make(map[string]string)
	for _, v := range m {
		var entry mapEntry
		_ = json.Unmarshal([]byte(v), &entry)
		result[entry.Key] = entry.Value
	}

	return result
}
