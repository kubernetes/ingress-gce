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
	"testing"
	"time"
)

const (
	testRetry         = 15
	testMinRetryDelay = 5 * time.Second
	testMaxRetryDelay = 5 * time.Minute
)

func verifyError(t *testing.T, err, expectedErr error) {
	t.Helper()

	if err != expectedErr {
		t.Errorf("Expect error to be %v, but got %v", expectedErr, err)
	}
}

func verifyExactDelay(t *testing.T, delay, expectedDelay time.Duration) {
	t.Helper()

	if delay != expectedDelay {
		t.Errorf("Expect retry delay = %v, but got %v", expectedDelay, delay)
	}
}

func verifyIntervalDelay(t *testing.T, delay, expectedMinDelay, expectedMaxDelay time.Duration) {
	t.Helper()

	if delay < expectedMinDelay || delay > expectedMaxDelay {
		t.Errorf("Expect retry delay between %v and %v, but got %v", expectedMinDelay, expectedMaxDelay, delay)
	}
}

// verifyMaxRetries checks that the delay is increased every time until
// testMaxRetryDelay is reached and after reaching the max number of retries
// it checks that BackoffHandler returns ErrRetriesExceeded error when trying to get the next delay
func verifyMaxRetries(t *testing.T, initialDelay time.Duration, handler BackoffHandler) time.Duration {
	t.Helper()

	expectedDelay := initialDelay
	for i := 0; i < testRetry; i++ {
		delay, err := handler.NextDelay()
		verifyError(t, err, nil)
		expectedMaxDelay := expectedDelay * 2
		if expectedMaxDelay > testMaxRetryDelay {
			expectedMaxDelay = testMaxRetryDelay
		}
		verifyIntervalDelay(t, delay, expectedDelay, expectedMaxDelay)
		expectedDelay = delay
	}
	_, err := handler.NextDelay()
	verifyError(t, err, ErrRetriesExceeded)
	return expectedDelay
}

func TestExponentialBackoffHandler(t *testing.T) {
	t.Parallel()

	handler := NewExponentialBackoffHandler(testRetry, testMinRetryDelay, testMaxRetryDelay)
	verifyMaxRetries(t, testMinRetryDelay, handler)
	handler.ResetDelay()

	delay, err := handler.NextDelay()
	verifyError(t, err, nil)
	verifyExactDelay(t, delay, testMinRetryDelay)
}

func TestExponentialBackoffHandlerDecreaseDelay(t *testing.T) {
	t.Parallel()

	handler := NewExponentialBackoffHandler(testRetry, testMinRetryDelay, testMaxRetryDelay)
	expectedDelay := verifyMaxRetries(t, testMinRetryDelay, handler)

	delay := handler.DecreaseDelay()
	verifyIntervalDelay(t, delay, expectedDelay/2, expectedDelay)
	verifyMaxRetries(t, delay, handler)

	for expectedDelay != testMinRetryDelay {
		delay = handler.DecreaseDelay()
		expectedMinDelay := expectedDelay / 2
		if expectedMinDelay < testMinRetryDelay {
			expectedMinDelay = testMinRetryDelay
		}
		verifyIntervalDelay(t, delay, expectedMinDelay, expectedDelay)
		expectedDelay = delay
	}

	// second iteration is to check that the delay won't go back up to min delay
	for i := 0; i < 2; i++ {
		delay = handler.DecreaseDelay()
		verifyExactDelay(t, delay, time.Duration(0))
	}
}
