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

func TestExponentialBackendOffHandler(t *testing.T) {
	handler := NewExponentialBackendOffHandler(testRetry, testMinRetryDelay, testMaxRetryDelay)
	expectDelay := testMinRetryDelay

	for i := 0; i < testRetry; i++ {
		delay, err := handler.NextRetryDelay()
		if err != nil {
			t.Errorf("Expect error to be nil, but got %v", err)
		}

		if !(delay >= expectDelay && delay <= 2*expectDelay) {
			t.Errorf("Expect retry delay >= %v and delay <= %v, but got %v", expectDelay, 2*expectDelay, delay)
		}

		if delay > testMaxRetryDelay {
			t.Errorf("Expect delay to be <= %v, but got %v", testMaxRetryDelay, delay)
		}
		expectDelay = delay
	}

	_, err := handler.NextRetryDelay()
	if err != ErrRetriesExceeded {
		t.Errorf("Expect error to be %v, but got %v", ErrRetriesExceeded, err)
	}

	handler.ResetRetryDelay()

	delay, err := handler.NextRetryDelay()
	if err != nil {
		t.Errorf("Expect error to be nil, but got %v", err)
	}

	if testMinRetryDelay != delay {
		t.Errorf("Expect retry delay = %v, but got %v", expectDelay, delay)
	}
}
