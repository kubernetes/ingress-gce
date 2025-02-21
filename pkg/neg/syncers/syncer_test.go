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
	if err := syncerTester.syncer.Start(); err != nil {
		t.Fatalf("Failed to start syncer: %v", err)
	}
	syncerTester.syncer.(*syncer).backoff = backoff.NewExponentialBackoffHandler(maxRetry, 0, 0)

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
