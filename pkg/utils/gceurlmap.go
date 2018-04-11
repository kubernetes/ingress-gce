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

package utils

import "fmt"

// GCEURLMap is a nested map of hostname-> path regex-> backend name
type GCEURLMap map[string]map[string]string

// GetDefaultBackendName performs a destructive read and returns
// the name of the default backend in the urlmap.
func (g GCEURLMap) GetDefaultBackendName() string {
	var name string
	var exists bool
	if h, ok := g[DefaultBackendKey]; ok {
		if name, exists = h[DefaultBackendKey]; exists {
			delete(h, DefaultBackendKey)
		}
		delete(g, DefaultBackendKey)
	}
	return name
}

// String implements the string interface for the GCEURLMap.
func (g GCEURLMap) String() string {
	msg := ""
	for host, um := range g {
		msg += fmt.Sprintf("%v\n", host)
		for url, beName := range um {
			msg += fmt.Sprintf("\t%v: ", url)
			if beName == "" {
				msg += fmt.Sprintf("No backend\n")
			} else {
				msg += fmt.Sprintf("%v\n", beName)
			}
		}
	}
	return msg
}

// PutDefaultBackendName performs a destructive write replacing
// the existing name of the default backend in the url map with
// the name of the given backend.
func (g GCEURLMap) PutDefaultBackendName(name string) {
	g[DefaultBackendKey] = map[string]string{
		DefaultBackendKey: name,
	}
}
