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
	"errors"
	"fmt"
	"sync"
	"testing"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

var servicesGR = schema.GroupResource{Resource: "services"}

// svcRef builds a services ObjectRef, the reference used by most tests here.
func svcRef(namespace, name string) ObjectRef {
	return ObjectRef{
		Resource:       servicesGR,
		NamespacedName: types.NamespacedName{Namespace: namespace, Name: name},
	}
}

type fakeRVGetter struct {
	mu sync.Mutex
	rv string
}

func (f *fakeRVGetter) LastSyncResourceVersion() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rv
}

func (f *fakeRVGetter) setRV(rv string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rv = rv
}

func newTestStore(rv string) (ConsistencyStore, *fakeRVGetter) {
	getter := &fakeRVGetter{rv: rv}
	store := NewConsistencyStore(map[schema.GroupResource]LastSyncRVGetter{servicesGR: getter})
	return store, getter
}

func TestEnsureReady(t *testing.T) {
	t.Parallel()

	ref := svcRef("ns", "svc")
	uid := types.UID("uid-1")

	for _, tc := range []struct {
		desc      string
		cacheRV   string
		writtenRV string
		wantStale bool
	}{
		{
			desc:      "cache behind recorded write is stale",
			cacheRV:   "99",
			writtenRV: "100",
			wantStale: true,
		},
		{
			desc:      "cache at recorded write is ready",
			cacheRV:   "100",
			writtenRV: "100",
			wantStale: false,
		},
		{
			desc:      "cache ahead of recorded write is ready",
			cacheRV:   "101",
			writtenRV: "100",
			wantStale: false,
		},
		{
			desc:      "unsynced informer with recorded write stalls",
			cacheRV:   "",
			writtenRV: "100",
			wantStale: true,
		},
		{
			desc:      "non-numeric cache ResourceVersion fails open",
			cacheRV:   "not-a-number",
			writtenRV: "100",
			wantStale: false,
		},
		{
			desc:      "non-numeric written ResourceVersion is not recorded and fails open",
			cacheRV:   "1",
			writtenRV: "not-a-number",
			wantStale: false,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			store, _ := newTestStore(tc.cacheRV)
			store.WroteAt(ref, uid, tc.writtenRV)

			err := store.EnsureReady(ref)
			if gotStale := err != nil; gotStale != tc.wantStale {
				t.Fatalf("EnsureReady(%v) = %v, want stale=%v", ref, err, tc.wantStale)
			}
			if err != nil {
				var consistencyErr *ConsistencyError
				if !errors.As(err, &consistencyErr) {
					t.Fatalf("EnsureReady(%v) returned %T, want *ConsistencyError", ref, err)
				}
				if consistencyErr.ObjectRef != ref {
					t.Errorf("ConsistencyError.ObjectRef = %v, want %v", consistencyErr.ObjectRef, ref)
				}
			}
		})
	}
}

func TestEnsureReadyWithoutRecord(t *testing.T) {
	t.Parallel()

	// With no recorded write there is nothing to wait for, even on an
	// unsynced informer.
	store, _ := newTestStore("")
	ref := svcRef("ns", "svc")
	if err := store.EnsureReady(ref); err != nil {
		t.Fatalf("EnsureReady(%v) = %v, want nil", ref, err)
	}
}

func TestEnsureReadyWithoutGetter(t *testing.T) {
	t.Parallel()

	// A write for a resource without a configured informer cannot be
	// checked and must fail open.
	store := NewConsistencyStore(nil)
	ref := svcRef("ns", "svc")
	store.WroteAt(ref, "uid-1", "100")
	if err := store.EnsureReady(ref); err != nil {
		t.Fatalf("EnsureReady(%v) = %v, want nil", ref, err)
	}
}

func TestEnsureReadyRecoversWhenCacheCatchesUp(t *testing.T) {
	t.Parallel()

	store, getter := newTestStore("99")
	ref := svcRef("ns", "svc")
	store.WroteAt(ref, "uid-1", "100")

	if err := store.EnsureReady(ref); err == nil {
		t.Fatal("EnsureReady with cache at 99 and write at 100 = nil, want error")
	}
	getter.setRV("100")
	if err := store.EnsureReady(ref); err != nil {
		t.Fatalf("EnsureReady after cache caught up = %v, want nil", err)
	}
}

func TestWroteAtKeepsNewestVersionForSameUID(t *testing.T) {
	t.Parallel()

	store, _ := newTestStore("150")
	ref := svcRef("ns", "svc")
	uid := types.UID("uid-1")

	store.WroteAt(ref, uid, "200")
	// An out-of-order older write must not lower the recorded version.
	store.WroteAt(ref, uid, "100")

	if err := store.EnsureReady(ref); err == nil {
		t.Fatal("EnsureReady = nil, want error: record must still be at RV 200 after an older WroteAt")
	}
}

func TestWroteAtReplacesRecordOnRecreation(t *testing.T) {
	t.Parallel()

	store, _ := newTestStore("150")
	ref := svcRef("ns", "svc")

	store.WroteAt(ref, "uid-old", "200")
	// Recreated object (new UID) starts a new version history: its lower
	// RV replaces the old record.
	store.WroteAt(ref, "uid-new", "100")

	if err := store.EnsureReady(ref); err != nil {
		t.Fatalf("EnsureReady = %v, want nil: recreation must replace the stale record", err)
	}
}

func TestClear(t *testing.T) {
	t.Parallel()

	ref := svcRef("ns", "svc")

	for _, tc := range []struct {
		desc      string
		clearUID  types.UID
		wantStale bool
	}{
		{
			desc:      "matching UID clears the record",
			clearUID:  "uid-1",
			wantStale: false,
		},
		{
			desc:      "empty UID clears unconditionally",
			clearUID:  "",
			wantStale: false,
		},
		{
			desc:      "mismatched UID keeps the record",
			clearUID:  "uid-other",
			wantStale: true,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			store, _ := newTestStore("99")
			store.WroteAt(ref, "uid-1", "100")
			store.Clear(ref, tc.clearUID)

			err := store.EnsureReady(ref)
			if gotStale := err != nil; gotStale != tc.wantStale {
				t.Fatalf("EnsureReady after Clear(uid=%q) = %v, want stale=%v", tc.clearUID, err, tc.wantStale)
			}
		})
	}
}

func TestClearUnknownObjectIsNoop(t *testing.T) {
	t.Parallel()

	store, _ := newTestStore("99")
	store.Clear(svcRef("ns", "absent"), "uid-1")
	if err := store.EnsureReady(svcRef("ns", "absent")); err != nil {
		t.Fatalf("EnsureReady = %v, want nil", err)
	}
}

func TestRecordsAreIndependentPerObject(t *testing.T) {
	t.Parallel()

	store, _ := newTestStore("99")
	staleRef := svcRef("ns", "stale")
	otherRef := svcRef("ns", "other")
	store.WroteAt(staleRef, "uid-1", "100")

	if err := store.EnsureReady(staleRef); err == nil {
		t.Error("EnsureReady(stale) = nil, want error")
	}
	if err := store.EnsureReady(otherRef); err != nil {
		t.Errorf("EnsureReady(other) = %v, want nil", err)
	}
}

// TestConcurrentAccess exercises the store from many goroutines so the race
// detector can verify the locking.
// TestRecordsAreIndependentPerResource covers the case that makes the record
// key include the resource: a controller writing several kinds of object can
// legitimately see the same namespace/name for each of them. Keyed by name
// alone the records would overwrite one another, and because the UIDs differ
// each write would take the "object was recreated" path, so a stale record
// for one resource would be checked against another resource's informer.
func TestRecordsAreIndependentPerResource(t *testing.T) {
	t.Parallel()

	podsGR := schema.GroupResource{Resource: "pods"}
	svcGetter := &fakeRVGetter{rv: "100"}
	podGetter := &fakeRVGetter{rv: "500"}
	store := NewConsistencyStore(map[schema.GroupResource]LastSyncRVGetter{
		servicesGR: svcGetter,
		podsGR:     podGetter,
	})

	nn := types.NamespacedName{Namespace: "ns", Name: "shared-name"}
	serviceRef := ObjectRef{Resource: servicesGR, NamespacedName: nn}
	podRef := ObjectRef{Resource: podsGR, NamespacedName: nn}

	// The service write is ahead of the service informer, the pod write is
	// not ahead of the pod informer.
	store.WroteAt(serviceRef, "svc-uid", "200")
	store.WroteAt(podRef, "pod-uid", "400")

	if err := store.EnsureReady(serviceRef); err == nil {
		t.Errorf("EnsureReady(%v) = nil, want stale: service cache is at 100 and the write was at 200", serviceRef)
	}
	if err := store.EnsureReady(podRef); err != nil {
		t.Errorf("EnsureReady(%v) = %v, want nil: pod cache is at 500 and the write was at 400", podRef, err)
	}

	// Clearing one resource must leave the other's record intact.
	store.Clear(podRef, "pod-uid")
	if err := store.EnsureReady(serviceRef); err == nil {
		t.Errorf("EnsureReady(%v) after clearing the pod record = nil, want still stale", serviceRef)
	}
}

func TestConcurrentAccess(t *testing.T) {
	t.Parallel()

	store, getter := newTestStore("0")
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ref := svcRef("ns", fmt.Sprintf("svc-%d", i%3))
			for j := 0; j < 100; j++ {
				store.WroteAt(ref, "uid-1", fmt.Sprintf("%d", j))
				getter.setRV(fmt.Sprintf("%d", j))
				store.EnsureReady(ref)
				if j%10 == 0 {
					store.Clear(ref, "uid-1")
				}
			}
		}(i)
	}
	wg.Wait()
}

func TestNoopConsistencyStore(t *testing.T) {
	t.Parallel()

	store := NewNoopConsistencyStore()
	ref := svcRef("ns", "svc")
	store.WroteAt(ref, "uid-1", "100")
	if err := store.EnsureReady(ref); err != nil {
		t.Fatalf("noop EnsureReady = %v, want nil", err)
	}
	store.Clear(ref, "uid-1")
}
