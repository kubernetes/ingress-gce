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

import (
	"testing"
)

func TestGCEURLMap(t *testing.T) {
	urlMap := NewGCEURLMap()

	// Add some path rules for a host.
	rules := []PathRule{
		PathRule{Path: "/test1", BackendName: "test1"},
		PathRule{Path: "/test2", BackendName: "test2"},
	}
	urlMap.PutPathRulesForHost("example.com", rules)
	if !urlMap.HostExists("example.com") {
		t.Errorf("Expected hostname example.com to exist in %+v", urlMap)
	}
	if ok, _ := urlMap.PathExists("example.com", "/test1"); !ok {
		t.Errorf("Expected path /test1 for hostname example.com to exist in %+v", urlMap)
	}
	if ok, _ := urlMap.PathExists("example.com", "/test2"); !ok {
		t.Errorf("Expected path /test2 for hostname example.com to exist in %+v", urlMap)
	}

	// Add some path rules for the same host. Ensure this results in an overwrite.
	rules = []PathRule{
		PathRule{Path: "/test3", BackendName: "test3"},
	}
	urlMap.PutPathRulesForHost("example.com", rules)
	if ok, _ := urlMap.PathExists("example.com", "/test1"); ok {
		t.Errorf("Expected path /test1 for hostname example.com not to exist in %+v", urlMap)
	}
	if ok, _ := urlMap.PathExists("example.com", "/test2"); ok {
		t.Errorf("Expected path /test2 for hostname example.com not to exist in %+v", urlMap)
	}
	if ok, _ := urlMap.PathExists("example.com", "/test3"); !ok {
		t.Errorf("Expected path /test3 for hostname example.com to exist in %+v", urlMap)
	}

	// Add some path rules with equal paths. Ensure the last one is taken.
	rules = []PathRule{
		PathRule{Path: "/test4", BackendName: "test4"},
		PathRule{Path: "/test5", BackendName: "test5"},
		PathRule{Path: "/test4", BackendName: "test4-a"},
	}
	urlMap.PutPathRulesForHost("example.com", rules)
	_, backend := urlMap.PathExists("example.com", "/test4")
	if backend != "test4-a" {
		t.Errorf("Expected path /test4 for hostname example.com to point to backend test4-a in %+v", urlMap)
	}
}
