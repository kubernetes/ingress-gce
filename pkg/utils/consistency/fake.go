/*
Copyright 2026 The Kubernetes Authors.

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

package consistency

import (
	"sync"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog/v2"
)

// FakeRVGetter is a LastSyncRVGetter for tests. It reports a ResourceVersion
// the test controls, so a test can hold an informer behind a recorded write
// and then let it catch up. It is safe for concurrent use.
type FakeRVGetter struct {
	mu sync.Mutex
	rv string
}

// NewFakeRVGetter returns a FakeRVGetter that reports rv.
func NewFakeRVGetter(rv string) *FakeRVGetter {
	return &FakeRVGetter{rv: rv}
}

// LastStoreSyncResourceVersion implements LastSyncRVGetter.
func (f *FakeRVGetter) LastStoreSyncResourceVersion() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rv
}

// SetRV changes the ResourceVersion the getter reports.
func (f *FakeRVGetter) SetRV(rv string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rv = rv
}

// NewStoreWithFakeRV returns a ConsistencyStore for tests that tracks only
// resource, together with the FakeRVGetter that stands in for the informer
// store of that resource and initially reports rv.
func NewStoreWithFakeRV(resource schema.GroupResource, rv string) (ConsistencyStore, *FakeRVGetter) {
	getter := NewFakeRVGetter(rv)
	return NewConsistencyStore(map[schema.GroupResource]LastSyncRVGetter{resource: getter}, klog.TODO()), getter
}
