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

package utils

import (
	"sync"
	"time"
)

type TimeTracker struct {
	lock      sync.Mutex
	timestamp time.Time
}

// Track records the current time and returns it
func (t *TimeTracker) Track() time.Time {
	t.lock.Lock()
	defer t.lock.Unlock()
	t.timestamp = time.Now()
	return t.timestamp
}

// Get returns previous recorded time
func (t *TimeTracker) Get() time.Time {
	t.lock.Lock()
	defer t.lock.Unlock()
	return t.timestamp
}

// Set records input timestamp
func (t *TimeTracker) Set(timestamp time.Time) {
	t.lock.Lock()
	defer t.lock.Unlock()
	t.timestamp = timestamp
	return
}

func NewTimeTracker() TimeTracker {
	return TimeTracker{
		timestamp: time.Now(),
	}
}
