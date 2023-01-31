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
	"fmt"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
)

var ErrRetriesExceeded = fmt.Errorf("maximum retry exceeded")

// BackoffHandler handles delays for back off retry
type BackoffHandler interface {
	// NextDelay returns the delay for next retry or error if maximum number of retries exceeded.
	NextDelay() (time.Duration, error)
	// DecreaseDelay returns the decreased delay for next retry
	DecreaseDelay() time.Duration
	// ResetDelay resets the retry delay
	ResetDelay()
}

// exponentialBackoffHandler is a backoff handler that returns retry delays semi-exponentially with random jitter within boundary.
// exponentialBackoffHandler returns ErrRetriesExceeded when maximum number of retries has reached.
type exponentialBackoffHandler struct {
	lock           sync.Mutex
	lastRetryDelay time.Duration
	retryCount     int
	maxRetries     int
	minRetryDelay  time.Duration
	maxRetryDelay  time.Duration
}

func NewExponentialBackoffHandler(maxRetries int, minRetryDelay, maxRetryDelay time.Duration) *exponentialBackoffHandler {
	return &exponentialBackoffHandler{
		lastRetryDelay: time.Duration(0),
		retryCount:     0,
		maxRetries:     maxRetries,
		minRetryDelay:  minRetryDelay,
		maxRetryDelay:  maxRetryDelay,
	}
}

// NextDelay returns the next back off delay for retry.
func (handler *exponentialBackoffHandler) NextDelay() (time.Duration, error) {
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

// ResetDelay resets the retry delay.
func (handler *exponentialBackoffHandler) ResetDelay() {
	handler.lock.Lock()
	defer handler.lock.Unlock()
	handler.retryCount = 0
	handler.lastRetryDelay = time.Duration(0)
}

// DecreaseDelay just returns the current delay, as it's not supported for exponentialBackoffHandler.
func (handler *exponentialBackoffHandler) DecreaseDelay() time.Duration {
	handler.lock.Lock()
	defer handler.lock.Unlock()
	return handler.lastRetryDelay
}

// twoWayExponentialBackoffHandler is a backoff handler that returns retry delays semi-exponentially both ways with random jitter within boundary.
// twoWayExponentialBackoffHandler returns ErrRetriesExceeded when maximum number of retries with increased delay has reached.
type twoWayExponentialBackoffHandler struct {
	*exponentialBackoffHandler
}

func NewTwoWayExponentialBackoffHandler(maxRetries int, minRetryDelay, maxRetryDelay time.Duration) *twoWayExponentialBackoffHandler {
	return &twoWayExponentialBackoffHandler{
		exponentialBackoffHandler: NewExponentialBackoffHandler(maxRetries, minRetryDelay, maxRetryDelay),
	}
}

// DecreaseDelay returns the decreased delay for next retry.
func (handler *twoWayExponentialBackoffHandler) DecreaseDelay() time.Duration {
	handler.lock.Lock()
	defer handler.lock.Unlock()
	handler.retryCount = 0
	handler.lastRetryDelay = handler.lastRetryDelay*2 - wait.Jitter(handler.lastRetryDelay, 0.5)
	if handler.lastRetryDelay < handler.minRetryDelay {
		handler.lastRetryDelay = handler.minRetryDelay
	}
	return handler.lastRetryDelay
}
