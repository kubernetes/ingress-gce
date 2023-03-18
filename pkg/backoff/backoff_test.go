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

package backoff

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
	if err != expectedErr {
		t.Errorf("Expect error to be %v, but got %v", expectedErr, err)
	}
}

func verifyExactDelay(t *testing.T, delay, expectedDelay time.Duration) {
	if delay != expectedDelay {
		t.Errorf("Expect retry delay = %v, but got %v", expectedDelay, delay)
	}
}

func verifyIntervalDelay(t *testing.T, delay, expectedMinDelay, expectedMaxDelay time.Duration) {
	if delay < expectedMinDelay || delay > expectedMaxDelay {
		t.Errorf("Expect retry delay between %v and %v, but got %v", expectedMinDelay, expectedMaxDelay, delay)
	}
}

func verifyIncreasedDelay(t *testing.T, delay, initialDelay time.Duration) {
	expectedMaxDelay := initialDelay * 2
	if expectedMaxDelay > testMaxRetryDelay {
		expectedMaxDelay = testMaxRetryDelay
	}
	verifyIntervalDelay(t, delay, initialDelay, expectedMaxDelay)
}

func verifyDecreasedDelay(t *testing.T, delay, initialDelay time.Duration) {
	expectedMinDelay := initialDelay / 2
	if expectedMinDelay < testMinRetryDelay {
		expectedMinDelay = testMinRetryDelay
	}
	verifyIntervalDelay(t, delay, expectedMinDelay, initialDelay)
}

func verifyMaxRetries(t *testing.T, initialDelay time.Duration, handler BackoffHandler) time.Duration {
	expectDelay := initialDelay
	for i := 0; i < testRetry; i++ {
		delay, err := handler.NextDelay()
		verifyError(t, err, nil)
		verifyIncreasedDelay(t, delay, expectDelay)
		expectDelay = delay
	}
	_, err := handler.NextDelay()
	verifyError(t, err, ErrRetriesExceeded)
	return expectDelay
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
	expectDelay := verifyMaxRetries(t, testMinRetryDelay, handler)

	delay := handler.DecreaseDelay()
	verifyDecreasedDelay(t, delay, expectDelay)
	verifyMaxRetries(t, delay, handler)

	for expectDelay != testMinRetryDelay {
		delay = handler.DecreaseDelay()
		verifyDecreasedDelay(t, delay, expectDelay)
		expectDelay = delay
	}

	// second iteration is to check that the delay won't go back up to min delay
	for i := 0; i < 2; i++ {
		delay = handler.DecreaseDelay()
		verifyExactDelay(t, delay, time.Duration(0))
	}
}
