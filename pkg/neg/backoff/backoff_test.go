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
	if !(delay >= expectedMinDelay && delay <= expectedMaxDelay) {
		t.Errorf("Expect retry delay >= %v and delay <= %v, but got %v", expectedMinDelay, expectedMaxDelay, delay)
	}
}

func verifyIncreasedDelay(t *testing.T, delay, expectedInitialDelay time.Duration) {
	expectedMaxDelay := expectedInitialDelay * 2
	if expectedMaxDelay > testMaxRetryDelay {
		expectedMaxDelay = testMaxRetryDelay
	}
	verifyIntervalDelay(t, delay, expectedInitialDelay, expectedMaxDelay)
}

func verifyDecreasedDelay(t *testing.T, delay, expectedInitialDelay time.Duration) {
	expectedMinDelay := expectedInitialDelay / 2
	if expectedMinDelay < testMinRetryDelay {
		expectedMinDelay = testMinRetryDelay
	}
	verifyIntervalDelay(t, delay, expectedMinDelay, expectedInitialDelay)
}

func TestExponentialBackendOffHandler(t *testing.T) {
	t.Parallel()

	handler := NewExponentialBackoffHandler(testRetry, testMinRetryDelay, testMaxRetryDelay)
	expectDelay := testMinRetryDelay

	for i := 0; i < testRetry; i++ {
		delay, err := handler.NextDelay()
		verifyError(t, err, nil)
		verifyIncreasedDelay(t, delay, expectDelay)
		expectDelay = delay
	}

	_, err := handler.NextDelay()
	verifyError(t, err, ErrRetriesExceeded)

	handler.ResetDelay()

	delay, err := handler.NextDelay()
	verifyError(t, err, nil)
	verifyExactDelay(t, delay, testMinRetryDelay)
}

func TestTwoWayExponentialBackoffHandler(t *testing.T) {
	t.Parallel()

	handler := NewTwoWayExponentialBackoffHandler(testRetry, testMinRetryDelay, testMaxRetryDelay)
	expectDelay := testMinRetryDelay

	for i := 0; i < testRetry; i++ {
		delay, err := handler.NextDelay()
		verifyError(t, err, nil)
		verifyIncreasedDelay(t, delay, expectDelay)
		expectDelay = delay
	}

	_, err := handler.NextDelay()
	verifyError(t, err, ErrRetriesExceeded)

	delay := handler.DecreaseDelay()
	verifyDecreasedDelay(t, delay, expectDelay)
	expectDelay = delay

	for i := 0; i < testRetry; i++ {
		delay, err := handler.NextDelay()
		verifyError(t, err, nil)
		verifyIncreasedDelay(t, delay, expectDelay)
		expectDelay = delay
	}

	_, err = handler.NextDelay()
	verifyError(t, err, ErrRetriesExceeded)

	for expectDelay != testMinRetryDelay {
		delay = handler.DecreaseDelay()
		verifyDecreasedDelay(t, delay, expectDelay)
		expectDelay = delay
	}

	delay = handler.DecreaseDelay()
	verifyExactDelay(t, delay, testMinRetryDelay)
}
