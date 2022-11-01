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

	"k8s.io/apimachinery/pkg/util/clock"
)

var ErrHandlerRetrying = fmt.Errorf("retry handler is retrying")

// retryHandler encapsulates logic that handles retry
type retryHandler interface {
	// Retry triggers retry
	Retry() error
	// Reset resets handler internals
	Reset()
}

// backoffRetryHandler handles retry with back off delay
// At any time, there is only one ongoing retry flow running
type backoffRetryHandler struct {
	// stateLock protects internal states and channels
	stateLock sync.Mutex
	// internal states
	retrying bool

	// backoff delay handling
	clock   clock.Clock
	backoff backoffHandler

	// retryFunc called on retry
	retryFunc func()
}

func NewDelayRetryHandler(retryFunc func(), backoff backoffHandler) *backoffRetryHandler {
	return &backoffRetryHandler{
		retrying:  false,
		clock:     clock.RealClock{},
		backoff:   backoff,
		retryFunc: retryFunc,
	}
}

// Retry triggers retry with back off
// At any time, there is only one ongoing retry allowed.
func (h *backoffRetryHandler) Retry() error {
	h.stateLock.Lock()
	defer h.stateLock.Unlock()

	if h.retrying {
		return ErrHandlerRetrying
	}

	delay, err := h.backoff.NextRetryDelay()
	if err != nil {
		return err
	}

	h.retrying = true
	ch := h.clock.After(delay)
	go func() {
		<-ch
		func() {
			h.stateLock.Lock()
			defer h.stateLock.Unlock()
			h.retryFunc()
			h.retrying = false
		}()
	}()
	return nil
}

// Reset resets internal back off delay handler
func (h *backoffRetryHandler) Reset() {
	h.backoff.ResetRetryDelay()
}
