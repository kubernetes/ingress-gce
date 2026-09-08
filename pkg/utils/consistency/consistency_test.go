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

	"github.com/go-logr/logr/funcr"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	clientgofeatures "k8s.io/client-go/features"
	"k8s.io/klog/v2"
)

var servicesGR = schema.GroupResource{Resource: "services"}

// svcRef builds a services ObjectRef, the reference used by most tests here.
func svcRef(namespace, name string) ObjectRef {
	return ObjectRef{
		Resource:       servicesGR,
		NamespacedName: types.NamespacedName{Namespace: namespace, Name: name},
	}
}

func newTestStore(rv string) (ConsistencyStore, *FakeRVGetter) {
	getter := NewFakeRVGetter(rv)
	store := NewConsistencyStore(map[schema.GroupResource]LastSyncRVGetter{servicesGR: getter}, klog.TODO())
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
			desc:      "store that cannot report a version fails open",
			cacheRV:   "",
			writtenRV: "100",
			wantStale: false,
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

	// A record for a resource whose informer store was not registered means
	// the store was built wrong. That must surface as a failure: neither as
	// a stall, which is requeued quietly, nor as a pass.
	store := NewConsistencyStore(nil, klog.TODO())
	ref := svcRef("ns", "svc")
	store.WroteAt(ref, "uid-1", "100")

	err := store.EnsureReady(ref)
	if err == nil {
		t.Fatalf("EnsureReady(%v) without a registered informer store = nil, want an error", ref)
	}
	if IsStall(err) {
		t.Errorf("IsStall(%v) = true, want false: a missing informer store is not a stall", err)
	}
}

// TestFailOpenIsLoggedOnce covers the paths on which the store gives up
// checking. Each must be logged, so that a store that does nothing is
// noticed, and only once, so that it does not flood the log.
func TestFailOpenIsLoggedOnce(t *testing.T) {
	t.Parallel()

	ref := svcRef("ns", "svc")
	for _, tc := range []struct {
		desc      string
		cacheRV   string
		writtenRV string
	}{
		{
			desc:      "written ResourceVersion is not numeric",
			cacheRV:   "1",
			writtenRV: "not-a-number",
		},
		{
			desc:      "informer store reports no ResourceVersion",
			cacheRV:   "",
			writtenRV: "100",
		},
		{
			desc:      "informer store ResourceVersion is not numeric",
			cacheRV:   "not-a-number",
			writtenRV: "100",
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			var logged []string
			logger := funcr.New(func(_, args string) {
				logged = append(logged, args)
			}, funcr.Options{})
			store := NewConsistencyStore(map[schema.GroupResource]LastSyncRVGetter{
				servicesGR: NewFakeRVGetter(tc.cacheRV),
			}, logger)

			for range 3 {
				store.WroteAt(ref, "uid-1", tc.writtenRV)
				if err := store.EnsureReady(ref); err != nil {
					t.Fatalf("EnsureReady(%v) = %v, want nil", ref, err)
				}
			}

			if len(logged) != 1 {
				t.Errorf("store logged %d lines for 3 writes and checks that failed open, want 1: %q", len(logged), logged)
			}
		})
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
	getter.SetRV("100")
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

// TestRecordsAreIndependentPerResource covers the case that makes the record
// key include the resource: a controller writing several kinds of object can
// legitimately see the same namespace/name for each of them. Keyed by name
// alone the records would overwrite one another, and because the UIDs differ
// each write would take the "object was recreated" path, so a stale record
// for one resource would be checked against another resource's informer.
func TestRecordsAreIndependentPerResource(t *testing.T) {
	t.Parallel()

	podsGR := schema.GroupResource{Resource: "pods"}
	svcGetter := NewFakeRVGetter("100")
	podGetter := NewFakeRVGetter("500")
	store := NewConsistencyStore(map[schema.GroupResource]LastSyncRVGetter{
		servicesGR: svcGetter,
		podsGR:     podGetter,
	}, klog.TODO())

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
			ref := svcRef("ns", fmt.Sprintf("svc-%d", i%3))
			for j := 0; j < 100; j++ {
				store.WroteAt(ref, "uid-1", fmt.Sprintf("%d", j))
				getter.SetRV(fmt.Sprintf("%d", j))
				// The result does not matter: this test only exercises
				// the locking under the race detector.
				_ = store.EnsureReady(ref)
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
	if !IsNoop(store) {
		t.Errorf("IsNoop(NewNoopConsistencyStore()) = false, want true")
	}
	if realStore, _ := newTestStore("100"); IsNoop(realStore) {
		t.Errorf("IsNoop(NewConsistencyStore(...)) = true, want false")
	}
	ref := svcRef("ns", "svc")
	store.WroteAt(ref, "uid-1", "100")
	if err := store.EnsureReady(ref); err != nil {
		t.Fatalf("noop EnsureReady = %v, want nil", err)
	}
	store.Clear(ref, "uid-1")
}

// TestIsStall covers the error shapes a stall reaches a controller's error
// handler in. Controllers aggregate errors with utilerrors.NewAggregate,
// which errors.As does not look through, so the aggregated cases are the
// ones a plain errors.As check gets wrong.
func TestIsStall(t *testing.T) {
	t.Parallel()

	stall := &ConsistencyError{ObjectRef: svcRef("ns", "svc"), Message: "stale"}
	wrappedStall := fmt.Errorf("failed to ensure svc neg cr: %w", stall)
	otherErr := errors.New("quota exceeded")
	var noErr error

	for _, tc := range []struct {
		desc string
		err  error
		want bool
	}{
		{
			desc: "nil error",
			err:  nil,
			want: false,
		},
		{
			desc: "stall",
			err:  stall,
			want: true,
		},
		{
			desc: "wrapped stall",
			err:  wrappedStall,
			want: true,
		},
		{
			desc: "other error",
			err:  otherErr,
			want: false,
		},
		{
			desc: "aggregate of a wrapped stall",
			err:  utilerrors.NewAggregate([]error{wrappedStall}),
			want: true,
		},
		{
			desc: "nested aggregate of a wrapped stall and a nil error",
			err:  utilerrors.NewAggregate([]error{utilerrors.NewAggregate([]error{wrappedStall}), nil}),
			want: true,
		},
		{
			desc: "aggregate of stalls",
			err:  utilerrors.NewAggregate([]error{stall, wrappedStall}),
			want: true,
		},
		{
			desc: "aggregate of a stall and another error",
			err:  utilerrors.NewAggregate([]error{wrappedStall, otherErr}),
			want: false,
		},
		{
			desc: "nested aggregate of a stall and another error",
			err:  utilerrors.NewAggregate([]error{utilerrors.NewAggregate([]error{wrappedStall}), otherErr}),
			want: false,
		},
		{
			desc: "aggregate of nil only",
			err:  utilerrors.NewAggregate([]error{nil}),
			want: false,
		},
		{
			desc: "wrapped aggregate of a stall",
			err:  fmt.Errorf("failed to sync service: %w", utilerrors.NewAggregate([]error{wrappedStall})),
			want: true,
		},
		{
			desc: "joined stalls",
			err:  errors.Join(stall, wrappedStall),
			want: true,
		},
		{
			desc: "joined stall and another error",
			err:  errors.Join(wrappedStall, otherErr),
			want: false,
		},
		{
			desc: "multi-wrapped stall and another error",
			err:  fmt.Errorf("sync failed: %w; %w", stall, otherErr),
			want: false,
		},
		{
			desc: "aggregate of joined stall and another error",
			err:  utilerrors.NewAggregate([]error{errors.Join(wrappedStall, otherErr)}),
			want: false,
		},
		{
			// A non-nil error that wraps an empty list of errors.
			desc: "multi-wrapped nil errors",
			err:  fmt.Errorf("first: %w, second: %w", noErr, noErr),
			want: false,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			if got := IsStall(tc.err); got != tc.want {
				t.Errorf("IsStall(%v) = %t, want %t", tc.err, got, tc.want)
			}
		})
	}
}

type fakeFeatureGates struct {
	atomicFIFO bool
}

func (g fakeFeatureGates) Enabled(key clientgofeatures.Feature) bool {
	if key == clientgofeatures.AtomicFIFO {
		return g.atomicFIFO
	}
	return true
}

func TestSupported(t *testing.T) {
	orig := clientgofeatures.FeatureGates()
	t.Cleanup(func() { clientgofeatures.ReplaceFeatureGates(orig) })

	for _, tc := range []struct {
		name       string
		atomicFIFO bool
		want       bool
	}{
		{name: "AtomicFIFO_enabled", atomicFIFO: true, want: true},
		{name: "AtomicFIFO_disabled", atomicFIFO: false, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clientgofeatures.ReplaceFeatureGates(fakeFeatureGates{atomicFIFO: tc.atomicFIFO})
			if got := Supported(); got != tc.want {
				t.Errorf("Supported() with AtomicFIFO=%t = %t, want %t", tc.atomicFIFO, got, tc.want)
			}
		})
	}
}
