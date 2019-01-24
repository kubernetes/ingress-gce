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
	"sync"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/util/clock"
	"k8s.io/apimachinery/pkg/util/wait"
)

const (
	testMaxRetries      = 10
	smallTestRetryDelay = 3 * time.Second
)

type retryHandlerTestHelper struct {
	lock  sync.Mutex
	count int
}

func (h *retryHandlerTestHelper) incrementCount() {
	h.lock.Lock()
	defer h.lock.Unlock()
	h.count += 1
}

func (h *retryHandlerTestHelper) getCount() int {
	h.lock.Lock()
	defer h.lock.Unlock()
	return h.count
}

func verifyRetryHandler(t *testing.T, expectCount int, helper *retryHandlerTestHelper) {
	if err := wait.PollImmediate(time.Microsecond, 2*time.Second, func() (bool, error) {
		if helper.getCount() == expectCount {
			return true, nil
		}
		return false, nil
	}); err != nil {
		t.Fatalf("Failed to retry. Expect counter == %d, but got: %v", expectCount, helper.getCount())
	}
}

func TestBackoffRetryHandler_Retry(t *testing.T) {
	helper := &retryHandlerTestHelper{}
	handler := NewDelayRetryHandler(helper.incrementCount, NewExponentialBackendOffHandler(testMaxRetries, smallTestRetryDelay, testMaxRetryDelay))
	fakeClock := clock.NewFakeClock(time.Now())
	handler.clock = fakeClock
	delay := smallTestRetryDelay

	// Trigger 2 Retries and expect one actual retry happens
	if err := handler.Retry(); err != nil {
		t.Fatalf("Expect no error, but got %v", err)
	}

	if err := handler.Retry(); err != ErrHandlerRetrying {
		t.Fatalf("Expect error %v, but got %v", ErrHandlerRetrying, err)
	}
	fakeClock.Step(delay)
	verifyRetryHandler(t, 1, helper)

	// Trigger exponential backoff retry
	if err := handler.Retry(); err != nil {
		t.Fatalf("Expect no error, but got %v", err)
	}
	delay *= 2
	fakeClock.Step(delay)
	verifyRetryHandler(t, 2, helper)

	// Trigger exponential backoff retry #2
	if err := handler.Retry(); err != nil {
		t.Fatalf("Expect no error, but got %v", err)
	}
	delay *= 2
	fakeClock.Step(delay)
	verifyRetryHandler(t, 3, helper)

	// Trigger reset and retry again
	handler.Reset()
	if err := handler.Retry(); err != nil {
		t.Fatalf("Expect no error, but got %v", err)
	}
	fakeClock.Step(smallTestRetryDelay)
	verifyRetryHandler(t, 4, helper)
}
