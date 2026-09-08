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

// Package consistency mitigates informer cache staleness for controllers
// that read the objects they also mutate.
//
// Controllers reconcile from informer caches, which lag behind the API
// server. A controller that patches an object and then reconciles the same
// object from a not-yet-updated cache can revert its own write or loop
// forever. A ConsistencyStore records the ResourceVersion returned by each
// write; before the next reconcile, EnsureReady reports whether the informer
// has caught up to that version so the sync can be requeued instead of
// acting on stale data.
//
// ResourceVersions are opaque strings per the Kubernetes API contract. This
// package relies on them being decimal integers, which holds for the etcd
// storage backend used by GKE and all upstream-supported configurations. To
// stay safe if that ever changes, every comparison fails open: a
// ResourceVersion that does not parse disables the staleness check for that
// record instead of blocking the controller.
package consistency

import (
	"fmt"
	"strconv"
	"sync"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

// LastSyncRVGetter reports the latest ResourceVersion an informer has
// observed. cache.SharedIndexInformer satisfies this interface.
type LastSyncRVGetter interface {
	LastSyncResourceVersion() string
}

// ConsistencyError is returned by EnsureReady when the informer cache has
// not yet observed a recorded write. It signals "requeue and retry later",
// not a permanent failure.
type ConsistencyError struct {
	NamespacedName types.NamespacedName
	Message        string
}

func (e *ConsistencyError) Error() string {
	return e.Message
}

// ConsistencyStore tracks the ResourceVersions written by a controller so
// reconciles can be stalled until the informer cache catches up.
//
// All methods are safe for concurrent use. A store may be shared by several
// controllers watching the same informer; each controller then also stalls
// on the others' writes, which is desirable when multiple controllers
// mutate the same objects (e.g. a service migrating between ILB and NetLB).
type ConsistencyStore interface {
	// EnsureReady returns nil if the informer cache has observed every
	// recorded write for the given object (or if nothing was recorded),
	// and a *ConsistencyError if the cache is still stale. Callers should
	// treat the error as a retriable requeue signal.
	EnsureReady(namespacedName types.NamespacedName) error
	// WroteAt records that the controller wrote the object and observed
	// resourceVersion in the API server's response. Older or equal
	// versions for the same object UID are ignored, so calling it after
	// no-op writes is harmless. A different UID means the object was
	// recreated and replaces the previous record.
	WroteAt(namespacedName types.NamespacedName, uid types.UID, resource schema.GroupResource, resourceVersion string)
	// Clear removes the record for the object. An empty uid clears
	// unconditionally; a non-empty uid only clears a record with the same
	// UID, so a deletion event for an old incarnation cannot drop the
	// record of a recreated object.
	Clear(namespacedName types.NamespacedName, uid types.UID)
}

type record struct {
	uid      types.UID
	rv       int64
	resource schema.GroupResource
}

type consistencyStore struct {
	mu sync.RWMutex
	// getters maps a resource to the informer whose cache serves reads
	// for that resource.
	getters map[schema.GroupResource]LastSyncRVGetter
	records map[types.NamespacedName]record
}

var _ ConsistencyStore = &consistencyStore{}

// NewConsistencyStore returns a ConsistencyStore that checks recorded
// writes against the informers in getters. Writes for resources without a
// configured getter are recorded but never stall a sync.
func NewConsistencyStore(getters map[schema.GroupResource]LastSyncRVGetter) ConsistencyStore {
	return &consistencyStore{
		getters: getters,
		records: make(map[types.NamespacedName]record),
	}
}

func (c *consistencyStore) EnsureReady(namespacedName types.NamespacedName) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	rec, ok := c.records[namespacedName]
	if !ok {
		return nil
	}

	getter, ok := c.getters[rec.resource]
	if !ok {
		// No informer configured for this resource; we cannot tell
		// whether the cache is stale, so fail open.
		return nil
	}

	lastSyncRVStr := getter.LastSyncResourceVersion()
	if lastSyncRVStr == "" {
		// The informer has never completed a list. Controllers wait for
		// HasSynced before processing, so this should not happen; stall
		// rather than reconcile from an unsynced cache.
		return &ConsistencyError{
			NamespacedName: namespacedName,
			Message:        fmt.Sprintf("informer for %v has empty LastSyncResourceVersion", rec.resource),
		}
	}

	lastSyncRV, err := strconv.ParseInt(lastSyncRVStr, 10, 64)
	if err != nil {
		// Opaque, non-numeric ResourceVersion: staleness cannot be
		// compared, fail open (see the package comment).
		return nil
	}

	if lastSyncRV < rec.rv {
		return &ConsistencyError{
			NamespacedName: namespacedName,
			Message:        fmt.Sprintf("informer cache for %v is stale for %v: waiting for ResourceVersion %v but cache is at %v", rec.resource, namespacedName, rec.rv, lastSyncRV),
		}
	}

	return nil
}

func (c *consistencyStore) WroteAt(namespacedName types.NamespacedName, uid types.UID, resource schema.GroupResource, resourceVersion string) {
	rv, err := strconv.ParseInt(resourceVersion, 10, 64)
	if err != nil {
		// Opaque, non-numeric ResourceVersion: fail open (see the
		// package comment).
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Keep the newest ResourceVersion for the same object. A different UID
	// means the object was recreated, so the old record is replaced even
	// if the recorded version is higher.
	if existing, ok := c.records[namespacedName]; ok && existing.uid == uid && existing.rv >= rv {
		return
	}
	c.records[namespacedName] = record{
		uid:      uid,
		rv:       rv,
		resource: resource,
	}
}

func (c *consistencyStore) Clear(namespacedName types.NamespacedName, uid types.UID) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if rec, ok := c.records[namespacedName]; ok {
		if uid == "" || rec.uid == uid {
			delete(c.records, namespacedName)
		}
	}
}

type noopConsistencyStore struct{}

var _ ConsistencyStore = &noopConsistencyStore{}

// NewNoopConsistencyStore returns a ConsistencyStore that records nothing
// and never stalls. It is used when the consistency check is disabled and
// in tests.
func NewNoopConsistencyStore() ConsistencyStore { return &noopConsistencyStore{} }

func (n *noopConsistencyStore) EnsureReady(namespacedName types.NamespacedName) error { return nil }
func (n *noopConsistencyStore) WroteAt(namespacedName types.NamespacedName, uid types.UID, resource schema.GroupResource, resourceVersion string) {
}
func (n *noopConsistencyStore) Clear(namespacedName types.NamespacedName, uid types.UID) {}
