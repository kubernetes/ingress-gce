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
	"errors"
	"fmt"
	"strconv"
	"sync"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	clientgofeatures "k8s.io/client-go/features"
)

// Supported reports whether a ConsistencyStore can detect staleness in this
// process. It needs informer stores to track the ResourceVersion they have
// applied, which client-go only does with its AtomicFIFO feature gate
// enabled (the default). Without it every check would fail open, so callers
// should use the no-op store instead.
func Supported() bool {
	return clientgofeatures.FeatureGates().Enabled(clientgofeatures.AtomicFIFO)
}

// LastSyncRVGetter reports the ResourceVersion of the latest event an
// informer has applied to the store its listers read from. cache.Store, and
// so the cache.Indexer returned by SharedIndexInformer.GetIndexer, satisfies
// this interface.
//
// The store's version is used rather than
// SharedIndexInformer.LastSyncResourceVersion: that one is advanced by the
// reflector when an event is queued, before the event is applied to the
// store, so it can run ahead of what a lister returns.
type LastSyncRVGetter interface {
	LastStoreSyncResourceVersion() string
}

// ObjectRef identifies a single object of a single resource. Records are
// keyed by the resource as well as the name because a controller may write
// several kinds of object, and an unqualified namespace/name is not unique
// across them: a Service, a Pod and a ServiceNetworkEndpointGroup can all
// share one namespace/name and each is tracked against its own informer.
type ObjectRef struct {
	Resource       schema.GroupResource
	NamespacedName types.NamespacedName
}

func (r ObjectRef) String() string {
	return fmt.Sprintf("%v %v", r.Resource, r.NamespacedName)
}

// ConsistencyError is returned by EnsureReady when the informer cache has
// not yet observed a recorded write. It signals "requeue and retry later",
// not a permanent failure. Use IsStall to recognize it in an error that has
// passed through other layers.
type ConsistencyError struct {
	ObjectRef ObjectRef
	Message   string
}

func (e *ConsistencyError) Error() string {
	return e.Message
}

// IsStall reports whether err consists solely of ConsistencyErrors, i.e.
// whether all it asks for is a requeue. It looks through single- and
// multi-error wrappers (fmt.Errorf with %w, errors.Join) and through
// utilerrors.Aggregate, which standard error unwrapping does not traverse. An
// error that combines a stall with any other failure is not a stall, so that
// the other failure is still reported.
func IsStall(err error) bool {
	if err == nil {
		return false
	}
	if _, ok := err.(*ConsistencyError); ok {
		return true
	}
	if u, ok := err.(interface{ Unwrap() error }); ok {
		return IsStall(u.Unwrap())
	}
	if u, ok := err.(interface{ Unwrap() []error }); ok {
		return allErrorsAreStalls(u.Unwrap())
	}
	if agg, ok := err.(utilerrors.Aggregate); ok {
		return allErrorsAreStalls(agg.Errors())
	}
	var stall *ConsistencyError
	return errors.As(err, &stall)
}

func allErrorsAreStalls(errs []error) bool {
	hasStall := false
	for _, err := range errs {
		if err == nil {
			continue
		}
		if !IsStall(err) {
			return false
		}
		hasStall = true
	}
	return hasStall
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
	EnsureReady(ref ObjectRef) error
	// WroteAt records that the controller wrote the object and observed
	// resourceVersion in the API server's response. Older or equal
	// versions for the same object UID are ignored, so calling it after
	// no-op writes is harmless. A different UID means the object was
	// recreated and replaces the previous record.
	WroteAt(ref ObjectRef, uid types.UID, resourceVersion string)
	// Clear removes the record for the object. An empty uid clears
	// unconditionally; a non-empty uid only clears a record with the same
	// UID, so a deletion event for an old incarnation cannot drop the
	// record of a recreated object.
	Clear(ref ObjectRef, uid types.UID)
}

type record struct {
	uid types.UID
	rv  int64
}

type consistencyStore struct {
	mu sync.RWMutex
	// getters maps a resource to the informer whose cache serves reads
	// for that resource.
	getters map[schema.GroupResource]LastSyncRVGetter
	records map[ObjectRef]record
}

var _ ConsistencyStore = &consistencyStore{}

// NewConsistencyStore returns a ConsistencyStore that checks recorded
// writes against the informer stores in getters. Writes for resources
// without a configured getter are recorded but never stall a sync.
func NewConsistencyStore(getters map[schema.GroupResource]LastSyncRVGetter) ConsistencyStore {
	return &consistencyStore{
		getters: getters,
		records: make(map[ObjectRef]record),
	}
}

func (c *consistencyStore) EnsureReady(ref ObjectRef) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	rec, ok := c.records[ref]
	if !ok {
		return nil
	}

	getter, ok := c.getters[ref.Resource]
	if !ok {
		// No informer configured for this resource; we cannot tell
		// whether the cache is stale, so fail open.
		return nil
	}

	lastSyncRVStr := getter.LastStoreSyncResourceVersion()
	if lastSyncRVStr == "" {
		// The store cannot report a version: client-go only tracks it
		// with the AtomicFIFO feature gate enabled, and it is empty until
		// the first list has been applied. Staleness cannot be compared,
		// so fail open rather than stall on a version that may never
		// arrive.
		return nil
	}

	lastSyncRV, err := strconv.ParseInt(lastSyncRVStr, 10, 64)
	if err != nil {
		// Opaque, non-numeric ResourceVersion: staleness cannot be
		// compared, fail open (see the package comment).
		return nil
	}

	if lastSyncRV < rec.rv {
		return &ConsistencyError{
			ObjectRef: ref,
			Message:   fmt.Sprintf("informer cache is stale for %v: waiting for ResourceVersion %v but cache is at %v", ref, rec.rv, lastSyncRV),
		}
	}

	return nil
}

func (c *consistencyStore) WroteAt(ref ObjectRef, uid types.UID, resourceVersion string) {
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
	if existing, ok := c.records[ref]; ok && existing.uid == uid && existing.rv >= rv {
		return
	}
	c.records[ref] = record{
		uid: uid,
		rv:  rv,
	}
}

func (c *consistencyStore) Clear(ref ObjectRef, uid types.UID) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if rec, ok := c.records[ref]; ok {
		if uid == "" || rec.uid == uid {
			delete(c.records, ref)
		}
	}
}

type noopConsistencyStore struct{}

var _ ConsistencyStore = &noopConsistencyStore{}

// NewNoopConsistencyStore returns a ConsistencyStore that records nothing
// and never stalls. It is used when the consistency check is disabled and
// in tests.
func NewNoopConsistencyStore() ConsistencyStore { return &noopConsistencyStore{} }

// IsNoop reports whether store is the no-op store, i.e. whether consistency
// checking is disabled. Callers use it to keep behavior that only makes
// sense together with a functioning store behind the same switch.
func IsNoop(store ConsistencyStore) bool {
	_, ok := store.(*noopConsistencyStore)
	return ok
}

func (n *noopConsistencyStore) EnsureReady(ref ObjectRef) error                              { return nil }
func (n *noopConsistencyStore) WroteAt(ref ObjectRef, uid types.UID, resourceVersion string) {}
func (n *noopConsistencyStore) Clear(ref ObjectRef, uid types.UID)                           {}
