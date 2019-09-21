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
	"testing"
	"time"

	apiv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/record"
	backendconfigclient "k8s.io/ingress-gce/pkg/backendconfig/client/clientset/versioned/fake"
	"k8s.io/ingress-gce/pkg/context"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	namer_util "k8s.io/ingress-gce/pkg/utils/namer"
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
}

// sync sleeps for 3 seconds
func (t *syncerTester) sync() error {
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
	kubeClient := fake.NewSimpleClientset()
	backendConfigClient := backendconfigclient.NewSimpleClientset()
	namer := namer_util.NewNamer(clusterID, "")
	ctxConfig := context.ControllerContextConfig{
		Namespace:             apiv1.NamespaceAll,
		ResyncPeriod:          1 * time.Second,
		DefaultBackendSvcPort: defaultBackend,
	}
	context := context.NewControllerContext(kubeClient, nil, backendConfigClient, nil, nil, namer, ctxConfig)
	negSyncerKey := negtypes.NegSyncerKey{
		Namespace:  testServiceNamespace,
		Name:       testServiceName,
		Port:       80,
		TargetPort: "80",
	}

	st := &syncerTester{
		syncCount: 0,
		blockSync: false,
		syncError: false,
		ch:        make(chan interface{}),
	}

	s := newSyncer(
		negSyncerKey,
		testNegName,
		context.ServiceInformer.GetIndexer(),
		record.NewFakeRecorder(100),
		st,
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
	syncerTester.blockSync = true
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
	syncerTester.syncError = true
	if err := syncerTester.syncer.Start(); err != nil {
		t.Fatalf("Failed to start syncer: %v", err)
	}
	syncerTester.syncer.(*syncer).backoff = NewExponentialBackendOffHandler(maxRetry, 0, 0)

	if err := wait.PollImmediate(time.Second, 5*time.Second, func() (bool, error) {
		// In 5 seconds, syncer should be able to retry 3 times.
		return syncerTester.syncCount == maxRetry+1, nil
	}); err != nil {
		t.Errorf("Syncer failed to retry and record error: %v", err)
	}

	if syncerTester.syncCount != maxRetry+1 {
		t.Errorf("Expect sync count to be %v, but got %v", maxRetry+1, syncerTester.syncCount)
	}
}
