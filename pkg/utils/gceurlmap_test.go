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
	"reflect"
	"testing"

	v1 "k8s.io/api/networking/v1"
)

func TestGCEURLMap(t *testing.T) {
	t.Parallel()
	urlMap := NewGCEURLMap()

	// Add an unrelated host to urlmap.
	rules := []PathRule{
		PathRule{Path: "/other", Backend: ServicePort{NodePort: 50000}},
	}
	urlMap.PutPathRulesForHost("foo.bar.com", rules)

	if _, ok := urlMap.PathExists("example.com", "/test1"); ok {
		t.Errorf("Expected path /test1 for hostname example.com not to exist in %+v", urlMap)
	}

	rules = []PathRule{
		PathRule{Path: "/test1", Backend: ServicePort{NodePort: 30000}},
		PathRule{Path: "/test2", Backend: ServicePort{NodePort: 30001}},
	}
	urlMap.PutPathRulesForHost("example.com", rules)
	if !urlMap.HostExists("example.com") {
		t.Errorf("Expected hostname example.com to exist in %+v", urlMap)
	}
	if _, ok := urlMap.PathExists("example.com", "/test1"); !ok {
		t.Errorf("Expected path /test1 for hostname example.com to exist in %+v", urlMap)
	}
	if _, ok := urlMap.PathExists("example.com", "/test2"); !ok {
		t.Errorf("Expected path /test2 for hostname example.com to exist in %+v", urlMap)
	}

	// Add some path rules for the same host. Ensure this results in an overwrite.
	rules = []PathRule{
		PathRule{Path: "/test3", Backend: ServicePort{NodePort: 30002}},
	}
	urlMap.PutPathRulesForHost("example.com", rules)
	if _, ok := urlMap.PathExists("example.com", "/test1"); ok {
		t.Errorf("Expected path /test1 for hostname example.com not to exist in %+v", urlMap)
	}
	if _, ok := urlMap.PathExists("example.com", "/test2"); ok {
		t.Errorf("Expected path /test2 for hostname example.com not to exist in %+v", urlMap)
	}
	if _, ok := urlMap.PathExists("example.com", "/test3"); !ok {
		t.Errorf("Expected path /test3 for hostname example.com to exist in %+v", urlMap)
	}

	// Add some path rules with equal paths. Ensure the last one is taken.
	rules = []PathRule{
		PathRule{Path: "/test4", Backend: ServicePort{NodePort: 30003}},
		PathRule{Path: "/test5", Backend: ServicePort{NodePort: 30004}},
		PathRule{Path: "/test4", Backend: ServicePort{NodePort: 30005}},
	}
	urlMap.PutPathRulesForHost("example.com", rules)
	backend, ok := urlMap.PathExists("example.com", "/test4")
	if !ok || backend.NodePort != 30005 {
		t.Errorf("Expected path /test4 for hostname example.com to point to backend with NodePort 30005 in %+v", urlMap)
	}
}

func TestGCEURLMapDeleteHost(t *testing.T) {
	t.Parallel()
	urlMap := NewGCEURLMap()
	rules := []PathRule{
		PathRule{Path: "/test1", Backend: ServicePort{NodePort: 30000}},
		PathRule{Path: "/test2", Backend: ServicePort{NodePort: 30001}},
	}
	urlMap.PutPathRulesForHost("example.com", rules)
	urlMap.PutPathRulesForHost("foo.bar.com", rules)

	// Delete last
	urlMap.deleteHost("foo.bar.com")

	if !urlMap.HostExists("example.com") {
		t.Errorf("HostExists('example.com') = false, want true")
	}
	if urlMap.HostExists("foo.bar.com") {
		t.Errorf("HostExists('foo.bar.com') = true, want false")
	}

	// Delete remaining host
	urlMap.deleteHost("example.com")

	if urlMap.HostExists("example.com") {
		t.Errorf("HostExists('foo.bar.com') = true, want false")
	}
}

func TestGCEURLMapEquals(t *testing.T) {
	t.Parallel()
	someMap := newTestMap()

	// Test against itself.
	equalMap := newTestMap()
	if !EqualMapping(someMap, equalMap) {
		t.Errorf("EqualMapping(%+v, %+v) = false, want true", someMap, equalMap)
	}

	// Test different DefaultBackend.
	diffDefault := newTestMap()
	b := NewServicePortWithID("svc-Y", "ns", v1.ServiceBackendPort{Number: 80})
	diffDefault.DefaultBackend = &b
	if EqualMapping(someMap, diffDefault) {
		t.Errorf("EqualMapping(%+v, %+v) = true, want false", someMap, diffDefault)
	}

	// Test check of HostRules.
	diffHostRules := newTestMap()
	diffHostRules.PutPathRulesForHost("roger.com", nil)
	if EqualMapping(someMap, diffHostRules) {
		t.Errorf("EqualMapping(%+v, %+v) = true, want false", someMap, diffHostRules)
	}

	// Test check of HostName.
	diffHostName := newTestMap()
	diffHostName.HostRules[0].Hostname = "roger.com"
	if EqualMapping(someMap, diffHostName) {
		t.Errorf("EqualMapping(%+v, %+v) = true, want false", someMap, diffHostName)
	}

	// Test check of PathRules.
	// Change PathRule's path.
	diffPaths := newTestMap()
	diffPaths.HostRules[0].Paths[0].Path = "/other"
	if EqualMapping(someMap, diffPaths) {
		t.Errorf("EqualMapping(%+v, %+v) = true, want false", someMap, diffPaths)
	}
	// Change a PathRule's backend.
	diffPaths.HostRules[0].Paths[0] = PathRule{Path: "/ex1", Backend: NewServicePortWithID("svc-M", "ns", v1.ServiceBackendPort{Number: 80})}
	if EqualMapping(someMap, diffPaths) {
		t.Errorf("EqualMapping(%+v, %+v) = true, want false", someMap, diffPaths)
	}
	// Delete a PathRule.
	diffPaths.HostRules[0].Paths = diffPaths.HostRules[0].Paths[:1]
	if EqualMapping(someMap, diffPaths) {
		t.Errorf("EqualMapping(%+v, %+v) = true, want false", someMap, diffPaths)
	}
}

func TestAllServicePorts(t *testing.T) {
	t.Parallel()
	m := newTestMap()
	wantPorts := []ServicePort{
		NewServicePortWithID("svc-X", "ns", v1.ServiceBackendPort{Number: 80}),
		NewServicePortWithID("svc-A", "ns", v1.ServiceBackendPort{Number: 80}),
		NewServicePortWithID("svc-B", "ns", v1.ServiceBackendPort{Number: 80}),
		NewServicePortWithID("svc-C", "ns", v1.ServiceBackendPort{Number: 80}),
		NewServicePortWithID("svc-D", "ns", v1.ServiceBackendPort{Number: 80}),
	}

	gotPorts := m.AllServicePorts()
	t.Logf("m = %v", m)
	if !reflect.DeepEqual(gotPorts, wantPorts) {
		t.Errorf("AllServicePorts(%+v) = \n%+v\nwant\n%+v", m, gotPorts, wantPorts)
	}

}

func newTestMap() *GCEURLMap {
	m := NewGCEURLMap()
	b := NewServicePortWithID("svc-X", "ns", v1.ServiceBackendPort{Number: 80})
	m.DefaultBackend = &b
	rules := []PathRule{
		PathRule{Path: "/ex1", Backend: NewServicePortWithID("svc-A", "ns", v1.ServiceBackendPort{Number: 80})},
		PathRule{Path: "/ex2", Backend: NewServicePortWithID("svc-B", "ns", v1.ServiceBackendPort{Number: 80})},
	}
	m.PutPathRulesForHost("example.com", rules)
	rules = []PathRule{
		PathRule{Path: "/foo1", Backend: NewServicePortWithID("svc-C", "ns", v1.ServiceBackendPort{Number: 80})},
		PathRule{Path: "/foo2", Backend: NewServicePortWithID("svc-D", "ns", v1.ServiceBackendPort{Number: 80})},
	}
	m.PutPathRulesForHost("foo.bar.com", rules)
	return m
}
