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

	nn := types.NamespacedName{Namespace: "ns", Name: "svc"}
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
			store.WroteAt(nn, uid, servicesGR, tc.writtenRV)

			err := store.EnsureReady(nn)
			if gotStale := err != nil; gotStale != tc.wantStale {
				t.Fatalf("EnsureReady(%v) = %v, want stale=%v", nn, err, tc.wantStale)
			}
			if err != nil {
				var consistencyErr *ConsistencyError
				if !errors.As(err, &consistencyErr) {
					t.Fatalf("EnsureReady(%v) returned %T, want *ConsistencyError", nn, err)
				}
				if consistencyErr.NamespacedName != nn {
					t.Errorf("ConsistencyError.NamespacedName = %v, want %v", consistencyErr.NamespacedName, nn)
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
	nn := types.NamespacedName{Namespace: "ns", Name: "svc"}
	if err := store.EnsureReady(nn); err != nil {
		t.Fatalf("EnsureReady(%v) = %v, want nil", nn, err)
	}
}

func TestEnsureReadyWithoutGetter(t *testing.T) {
	t.Parallel()

	// A write for a resource without a configured informer cannot be
	// checked and must fail open.
	store := NewConsistencyStore(nil)
	nn := types.NamespacedName{Namespace: "ns", Name: "svc"}
	store.WroteAt(nn, "uid-1", servicesGR, "100")
	if err := store.EnsureReady(nn); err != nil {
		t.Fatalf("EnsureReady(%v) = %v, want nil", nn, err)
	}
}

func TestEnsureReadyRecoversWhenCacheCatchesUp(t *testing.T) {
	t.Parallel()

	store, getter := newTestStore("99")
	nn := types.NamespacedName{Namespace: "ns", Name: "svc"}
	store.WroteAt(nn, "uid-1", servicesGR, "100")

	if err := store.EnsureReady(nn); err == nil {
		t.Fatal("EnsureReady with cache at 99 and write at 100 = nil, want error")
	}
	getter.setRV("100")
	if err := store.EnsureReady(nn); err != nil {
		t.Fatalf("EnsureReady after cache caught up = %v, want nil", err)
	}
}

func TestWroteAtKeepsNewestVersionForSameUID(t *testing.T) {
	t.Parallel()

	store, _ := newTestStore("150")
	nn := types.NamespacedName{Namespace: "ns", Name: "svc"}
	uid := types.UID("uid-1")

	store.WroteAt(nn, uid, servicesGR, "200")
	// An out-of-order older write must not lower the recorded version.
	store.WroteAt(nn, uid, servicesGR, "100")

	if err := store.EnsureReady(nn); err == nil {
		t.Fatal("EnsureReady = nil, want error: record must still be at RV 200 after an older WroteAt")
	}
}

func TestWroteAtReplacesRecordOnRecreation(t *testing.T) {
	t.Parallel()

	store, _ := newTestStore("150")
	nn := types.NamespacedName{Namespace: "ns", Name: "svc"}

	store.WroteAt(nn, "uid-old", servicesGR, "200")
	// Recreated object (new UID) starts a new version history: its lower
	// RV replaces the old record.
	store.WroteAt(nn, "uid-new", servicesGR, "100")

	if err := store.EnsureReady(nn); err != nil {
		t.Fatalf("EnsureReady = %v, want nil: recreation must replace the stale record", err)
	}
}

func TestClear(t *testing.T) {
	t.Parallel()

	nn := types.NamespacedName{Namespace: "ns", Name: "svc"}

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
			store.WroteAt(nn, "uid-1", servicesGR, "100")
			store.Clear(nn, tc.clearUID)

			err := store.EnsureReady(nn)
			if gotStale := err != nil; gotStale != tc.wantStale {
				t.Fatalf("EnsureReady after Clear(uid=%q) = %v, want stale=%v", tc.clearUID, err, tc.wantStale)
			}
		})
	}
}

func TestClearUnknownObjectIsNoop(t *testing.T) {
	t.Parallel()

	store, _ := newTestStore("99")
	store.Clear(types.NamespacedName{Namespace: "ns", Name: "absent"}, "uid-1")
	if err := store.EnsureReady(types.NamespacedName{Namespace: "ns", Name: "absent"}); err != nil {
		t.Fatalf("EnsureReady = %v, want nil", err)
	}
}

func TestRecordsAreIndependentPerObject(t *testing.T) {
	t.Parallel()

	store, _ := newTestStore("99")
	staleNN := types.NamespacedName{Namespace: "ns", Name: "stale"}
	otherNN := types.NamespacedName{Namespace: "ns", Name: "other"}
	store.WroteAt(staleNN, "uid-1", servicesGR, "100")

	if err := store.EnsureReady(staleNN); err == nil {
		t.Error("EnsureReady(stale) = nil, want error")
	}
	if err := store.EnsureReady(otherNN); err != nil {
		t.Errorf("EnsureReady(other) = %v, want nil", err)
	}
}

// TestConcurrentAccess exercises the store from many goroutines so the race
// detector can verify the locking.
func TestConcurrentAccess(t *testing.T) {
	t.Parallel()

	store, getter := newTestStore("0")
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			nn := types.NamespacedName{Namespace: "ns", Name: fmt.Sprintf("svc-%d", i%3)}
			for j := 0; j < 100; j++ {
				store.WroteAt(nn, "uid-1", servicesGR, fmt.Sprintf("%d", j))
				getter.setRV(fmt.Sprintf("%d", j))
				store.EnsureReady(nn)
				if j%10 == 0 {
					store.Clear(nn, "uid-1")
				}
			}
		}(i)
	}
	wg.Wait()
}

func TestNoopConsistencyStore(t *testing.T) {
	t.Parallel()

	store := NewNoopConsistencyStore()
	nn := types.NamespacedName{Namespace: "ns", Name: "svc"}
	store.WroteAt(nn, "uid-1", servicesGR, "100")
	if err := store.EnsureReady(nn); err != nil {
		t.Fatalf("noop EnsureReady = %v, want nil", err)
	}
	store.Clear(nn, "uid-1")
}
