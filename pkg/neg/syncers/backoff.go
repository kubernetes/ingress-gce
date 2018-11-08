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
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
)

var ErrRetriesExceeded = fmt.Errorf("maximum retry exceeded")

// backoffHandler handles delays for back off retry
type backoffHandler interface {
	// NextRetryDelay returns the delay for next retry or error if maximum number of retries exceeded.
	NextRetryDelay() (time.Duration, error)
	// ResetRetryDelay resets the retry delay
	ResetRetryDelay()
}

// exponentialBackOffHandler is a backoff handler that returns retry delays semi-exponentially with random jitter within boundary.
// exponentialBackOffHandler returns ErrRetriesExceeded when maximum number of retries has reached.
type exponentialBackOffHandler struct {
	lock           sync.Mutex
	lastRetryDelay time.Duration
	retryCount     int
	maxRetries     int
	minRetryDelay  time.Duration
	maxRetryDelay  time.Duration
}

func NewExponentialBackendOffHandler(maxRetries int, minRetryDelay, maxRetryDelay time.Duration) *exponentialBackOffHandler {
	return &exponentialBackOffHandler{
		lastRetryDelay: time.Duration(0),
		retryCount:     0,
		maxRetries:     maxRetries,
		minRetryDelay:  minRetryDelay,
		maxRetryDelay:  maxRetryDelay,
	}
}

// NextRetryDelay returns the next back off delay for retry.
func (handler *exponentialBackOffHandler) NextRetryDelay() (time.Duration, error) {
	handler.lock.Lock()
	defer handler.lock.Unlock()
	handler.retryCount += 1
	if handler.maxRetries > 0 && handler.retryCount > handler.maxRetries {
		return 0, ErrRetriesExceeded
	}
	handler.lastRetryDelay = wait.Jitter(handler.lastRetryDelay, 1.0)
	if handler.lastRetryDelay < handler.minRetryDelay {
		handler.lastRetryDelay = handler.minRetryDelay
	} else if handler.lastRetryDelay > handler.maxRetryDelay {
		handler.lastRetryDelay = handler.maxRetryDelay
	}
	return handler.lastRetryDelay, nil
}

// ResetRetryDelay resets the retry delay.
func (handler *exponentialBackOffHandler) ResetRetryDelay() {
	handler.lock.Lock()
	defer handler.lock.Unlock()
	handler.retryCount = 0
	handler.lastRetryDelay = time.Duration(0)
}
