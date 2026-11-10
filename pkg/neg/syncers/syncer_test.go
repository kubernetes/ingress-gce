/*
Copyright 2018 The Kubernetes Authors.

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

package syncers

import (
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/record"
	"k8s.io/ingress-gce/pkg/backoff"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/klog/v2"
)

const (
	testNegName          = "test-neg-name"
	testL4NegName        = "test-neg-name-l4"
	testServiceNamespace = "test-ns"
	testServiceName      = "test-name"
	testNamedPort        = "named-Port"
	clusterID            = "clusterid"
	kubeSystemUID        = "kube-system-id"
)

type syncerTester struct {
	syncer negtypes.NegSyncer
	// keep track of the number of syncs
	syncCount int
	// syncError is true, then sync function return error
	syncError bool
	// blockSync is true, then sync function is blocked on channel
	blockSync bool
	ch        chan interface{}
	mu        sync.Mutex
}

// sync sleeps for 3 seconds
func (t *syncerTester) sync() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.syncCount += 1
	if t.syncError {
		return fmt.Errorf("sync error")
	}
	if t.blockSync {
		<-t.ch
	}
	return nil
}

func newSyncerTester() *syncerTester {
	testNegName := "test-neg-name"
	testContext := negtypes.NewTestContext()
	negSyncerKey := negtypes.NegSyncerKey{
		Namespace: testServiceNamespace,
		Name:      testServiceName,
		PortTuple: negtypes.SvcPortTuple{
			Port:       80,
			TargetPort: "80",
		},
		NegName: testNegName,
	}

	st := &syncerTester{
		syncCount: 0,
		blockSync: false,
		syncError: false,
		ch:        make(chan interface{}),
	}

	s := newSyncer(
		negSyncerKey,
		testContext.ServiceInformer.GetIndexer(),
		record.NewFakeRecorder(100),
		st,
		klog.TODO(),
		testContext.NegMetrics,
	)
	st.syncer = s
	return st
}

func TestStartAndStopNoopSyncer(t *testing.T) {
	syncerTester := newSyncerTester()
	if !syncerTester.syncer.IsStopped() {
		t.Fatalf("Syncer is not stopped after creation.")
	}
	if syncerTester.syncer.IsShuttingDown() {
		t.Fatalf("Syncer is shutting down after creation.")
	}

	if err := syncerTester.syncer.Start(); err != nil {
		t.Fatalf("Failed to start syncer: %v", err)
	}
	if syncerTester.syncer.IsStopped() {
		t.Fatalf("Syncer is stopped after Start.")
	}
	if syncerTester.syncer.IsShuttingDown() {
		t.Fatalf("Syncer is shutting down after Start.")
	}

	// blocks sync function
	syncerTester.mu.Lock()
	syncerTester.blockSync = true
	syncerTester.mu.Unlock()
	syncerTester.syncer.Stop()
	if !syncerTester.syncer.IsShuttingDown() {
		// assume syncer needs 5 second for sync
		t.Fatalf("Syncer is not shutting down after Start.")
	}

	if !syncerTester.syncer.IsStopped() {
		t.Fatalf("Syncer is not stopped after Stop.")
	}

	// unblock sync function
	syncerTester.ch <- struct{}{}
	if err := wait.PollImmediate(time.Second, 3*time.Second, func() (bool, error) {
		return !syncerTester.syncer.IsShuttingDown() && syncerTester.syncer.IsStopped(), nil
	}); err != nil {
		t.Fatalf("Syncer failed to shutdown: %v", err)
	}

	if err := syncerTester.syncer.Start(); err != nil {
		t.Fatalf("Failed to restart syncer: %v", err)
	}
	if syncerTester.syncer.IsStopped() {
		t.Fatalf("Syncer is stopped after restart.")
	}
	if syncerTester.syncer.IsShuttingDown() {
		t.Fatalf("Syncer is shutting down after restart.")
	}

	syncerTester.syncer.Stop()
	if !syncerTester.syncer.IsStopped() {
		t.Fatalf("Syncer is not stopped after Stop.")
	}
}

func TestRetryOnSyncError(t *testing.T) {
	maxRetry := 3
	syncerTester := newSyncerTester()
	syncerTester.mu.Lock()
	syncerTester.syncError = true
	syncerTester.mu.Unlock()
	syncerTester.syncer.(*syncer).backoff = backoff.NewExponentialBackoffHandler(maxRetry, 0, 0)
	if err := syncerTester.syncer.Start(); err != nil {
		t.Fatalf("Failed to start syncer: %v", err)
	}
	defer syncerTester.syncer.Stop()

	if err := wait.PollImmediate(time.Second, 5*time.Second, func() (bool, error) {
		// In 5 seconds, syncer should be able to retry 3 times.
		syncerTester.mu.Lock()
		syncCount := syncerTester.syncCount
		syncerTester.mu.Unlock()
		return syncCount == maxRetry+1, nil
	}); err != nil {
		t.Errorf("Syncer failed to retry and record error: %v", err)
	}

	syncerTester.mu.Lock()
	syncCount := syncerTester.syncCount
	syncerTester.mu.Unlock()
	if syncCount != maxRetry+1 {
		t.Errorf("Expect sync count to be %v, but got %v", maxRetry+1, syncCount)
	}
}

// TestSyncerConcurrentStopAndSync verifies that calling Stop() and Sync()
// concurrently on a running syncer does not trigger a TOCTOU race condition or
// panic with "send on closed channel".
//
// Background:
// In production, background goroutines (e.g., attach/detach endpoint workers in
// commitTransaction, backoffRetryHandler timers, or dualstack.Migrator timers)
// call s.Sync() without holding syncerManager.mu while controller workers may
// concurrently call s.Stop(). If Sync() checks IsStopped() and releases
// stateLock before sending to s.syncCh, Stop() can close s.syncCh in the window
// between the check and the channel send.
func TestSyncerConcurrentStopAndSync(t *testing.T) {
	for i := 0; i < 100000; i++ {
		// Step 1: Initialize a syncer in the running state (stopped=false, open syncCh).
		s := &syncer{}
		s.init()

		var wg sync.WaitGroup
		wg.Add(2)

		// Step 2: Concurrently stop the syncer, which transitions stopped=true
		// and closes s.syncCh under stateLock.
		go func() {
			defer wg.Done()
			s.Stop()
		}()

		// Step 3: Concurrently signal Sync() to attempt a non-blocking send on
		// s.syncCh. Yield the processor first to increase overlap with Stop()
		// right at the check-then-send boundary, and recover any panic so the
		// test reports a clear failure instead of crashing the test binary.
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("unexpected panic on iteration %d: %v", i, r)
				}
			}()
			runtime.Gosched()
			s.Sync()
		}()

		// Step 4: Wait for both goroutines to finish before the next iteration.
		wg.Wait()
	}
}

// TestSyncerConcurrentStartTOCTOU verifies that concurrent Start() calls on a
// stopped syncer are serialized atomically so that:
//  1. Exactly one Start() call succeeds and the other returns an "already running"
//     error (no TOCTOU window between checking IsStopped()/IsShuttingDown() and
//     initializing syncer state).
//  2. Re-initializing s.syncCh in Start() does not race with the syncer loop
//     goroutine reading from s.syncCh.
func TestSyncerConcurrentStartTOCTOU(t *testing.T) {
	for i := 0; i < 1000; i++ {
		// Step 1: Create a new syncer in its initial stopped state.
		syncerTester := newSyncerTester()

		// Step 2: Use a barrier channel so both goroutines invoke Start() at the
		// exact same instant, maximizing contention on the IsStopped() -> init()
		// transition.
		startBarrier := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)

		var err1, err2 error
		go func() {
			defer wg.Done()
			<-startBarrier
			err1 = syncerTester.syncer.Start()
		}()
		go func() {
			defer wg.Done()
			<-startBarrier
			err2 = syncerTester.syncer.Start()
		}()

		// Step 3: Release both goroutines simultaneously and wait for both Start()
		// calls to return, then stop the syncer to clean up its loop goroutine.
		close(startBarrier)
		wg.Wait()
		syncerTester.syncer.Stop()

		// Step 4: Assert mutual exclusion: one Start() must succeed (nil) and the
		// concurrent Start() must fail (non-nil error). If both succeed, the
		// TOCTOU window allowed two loop goroutines to start and leaked the first.
		if (err1 == nil && err2 == nil) || (err1 != nil && err2 != nil) {
			t.Fatalf("iteration %d: expected exactly one Start() to succeed and one to fail, got err1=%v, err2=%v", i, err1, err2)
		}
	}
}
