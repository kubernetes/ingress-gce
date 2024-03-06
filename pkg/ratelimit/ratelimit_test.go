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

package ratelimit

import (
	"context"
	"fmt"
	"k8s.io/klog/v2"
	"sync"
	"testing"
	"time"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/ingress-gce/pkg/flags"
	clocktesting "k8s.io/utils/clock/testing"
)

const testDelay = 5 * time.Second

func verifyError(t *testing.T, desc string, err, expectedErr error) {
	t.Helper()

	if err != expectedErr {
		t.Errorf("Expect error for %v to be %v, but got %v", desc, expectedErr, err)
	}
}

func verifyDelay(t *testing.T, delay, expectedDelay time.Duration) {
	t.Helper()

	if delay != expectedDelay {
		t.Errorf("Expect delay for the strategy to be %v, but got %v", expectedDelay, delay)
	}
}

func verifyBlocked(t *testing.T, blocked bool) {
	t.Helper()

	if !blocked {
		t.Errorf("strategyRateLimiter.Accept() wasn't blocked, but was expected to")
	}
}

// delayRequestTracker is a helper for a fakeThrottlingStrategy which should
// indicate whether the delay was requested from the strategy or not
type delayRequestTracker struct {
	lock sync.Mutex
	// delayRequested shows whether the delay was requested from the strategy after
	// the last reset of this flag
	delayRequested bool
}

func (t *delayRequestTracker) isDelayRequested() bool {
	t.lock.Lock()
	defer t.lock.Unlock()
	return t.delayRequested
}

func (t *delayRequestTracker) setDelayRequested(delayRequested bool) {
	t.lock.Lock()
	defer t.lock.Unlock()
	t.delayRequested = delayRequested
}

type fakeThrottlingStrategy struct {
	lock         sync.Mutex
	currentDelay time.Duration
	delayRequestTracker
}

func (strategy *fakeThrottlingStrategy) Delay() time.Duration {
	strategy.lock.Lock()
	defer strategy.lock.Unlock()
	defer strategy.setDelayRequested(true)
	return strategy.currentDelay
}

func (strategy *fakeThrottlingStrategy) Observe(err error) {
	if err != nil {
		strategy.setDelay(testDelay)
	} else {
		strategy.setDelay(0)
	}
}

func (strategy *fakeThrottlingStrategy) setDelay(delay time.Duration) {
	strategy.lock.Lock()
	defer strategy.lock.Unlock()
	strategy.currentDelay = delay
}

func newFakeThrottlingStrategy() *fakeThrottlingStrategy {
	return &fakeThrottlingStrategy{
		currentDelay:        0,
		delayRequestTracker: delayRequestTracker{delayRequested: false},
	}
}

func TestGCERateLimiter(t *testing.T) {
	validTestCases := [][]string{
		{"ga.Addresses.Get,qps,1.5,5"},
		{"ga.Addresses.List,qps,2,10"},
		{"ga.Addresses.Get,qps,1.5,5", "ga.Firewalls.Get,qps,1.5,5"},
		{"ga.Operations.Get,qps,10,100"},
	}
	invalidTestCases := [][]string{
		{"gaAddresses.Get,qps,1.5,5"},
		{"gaAddresses.Get,qps,0,5"},
		{"gaAddresses.Get,qps,-1,5"},
		{"ga.Addresses.Get,qps,1.5.5"},
		{"gaAddresses.Get,qps,1.5,5.5"},
		{"gaAddressesGet,qps,1.5,5.5"},
		{"gaAddressesGet,qps,1.5"},
		{"ga.Addresses.Get,foo,1.5,5"},
		{"ga.Addresses.Get,1.5,5"},
		{"ga.Addresses.Get,qps,1.5,5", "gaFirewalls.Get,qps,1.5,5"},
	}

	for _, testCase := range validTestCases {
		_, err := NewGCERateLimiter(testCase, time.Second, klog.TODO())
		if err != nil {
			t.Errorf("Did not expect an error for test case: %v", testCase)
		}
	}

	for _, testCase := range invalidTestCases {
		_, err := NewGCERateLimiter(testCase, time.Second, klog.TODO())
		if err == nil {
			t.Errorf("Expected an error for test case: %v", testCase)
		}
	}
}

func TestRateLimitScale(t *testing.T) {
	// no parallel
	oldScale := flags.F.GCERateLimitScale
	defer func() { flags.F.GCERateLimitScale = oldScale }()

	flags.F.GCERateLimitScale = 2
	const cfg = "ga.Addresses.Get,qps,1,5"
	_, err := NewGCERateLimiter([]string{cfg}, time.Second, klog.TODO())
	if err != nil {
		t.Errorf("NewGCERateLimiter([]string{%q}, time.Second) = %v, want nil", cfg, err)
	}
	// TODO(bowei) -- this does not actually test the parameters were scaled.
}

func TestStrategyRateLimiter(t *testing.T) {
	t.Parallel()

	fakeClock := clocktesting.NewFakeClock(time.Now())
	strategy := newFakeThrottlingStrategy()
	rl := &strategyRateLimiter{
		strategy: strategy,
		clock:    fakeClock,
	}
	rlk := &cloud.RateLimitKey{
		ProjectID: "",
		Version:   "ga",
		Service:   "test",
		Operation: "test",
	}
	acceptAndVerify := func() {
		go func() {
			// This and subsequent sleep calls are needed to allow the goroutine switch and
			// process events in the fakeClock. The amount of less than a second could result
			// into flakes sometimes.
			time.Sleep(time.Second)
			// Step is needed to trigger queue processing in the fakeClock.
			fakeClock.Step(0)
		}()
		err := rl.Accept(context.Background(), rlk)
		verifyError(t, "StrategyRateLimiter.Accept()", err, nil)
	}

	// Use context that has been cancelled and expect a context error returned.
	ctxCancelled, cancelled := context.WithCancel(context.Background())
	cancelled()
	// Verify context is cancelled by now.
	<-ctxCancelled.Done()
	err := rl.Accept(ctxCancelled, rlk)
	verifyError(t, "StrategyRateLimiter.Accept()", err, ctxCancelled.Err())

	acceptAndVerify()
	rl.Observe(context.Background(), fmt.Errorf("test error"), nil)
	verifyDelay(t, strategy.Delay(), testDelay)
	strategy.setDelayRequested(false)

	// This block is intended to check that the request will be blocked until
	// the delay from the strategy has passed. It waits until the delay is requested
	// from the strategy, and then sleeps for some time to verify that
	// the strategyRateLimiter will be blocked.
	wg := sync.WaitGroup{}
	wg.Add(1)
	blocked := false
	go func() {
		err := wait.PollImmediate(time.Second, 5*time.Second, func() (bool, error) { return strategy.isDelayRequested(), nil })
		verifyError(t, "wait.PollImmediate()", err, nil)
		time.Sleep(4 * time.Second)
		verifyBlocked(t, blocked)
		fakeClock.Step(testDelay)
		wg.Done()
	}()
	blocked = true
	acceptAndVerify()
	blocked = false
	// Needed if the call to Accept() wasn't blocked, otherwise test would
	// finish without the check for blocked request in the goroutine
	wg.Wait()
}

func TestStrategyRateLimiterGroupBlock(t *testing.T) {
	t.Parallel()

	fakeClock := clocktesting.NewFakeClock(time.Now())
	strategy := newFakeThrottlingStrategy()
	rl := &strategyRateLimiter{
		strategy: strategy,
		clock:    fakeClock,
	}
	rlk := &cloud.RateLimitKey{
		ProjectID: "",
		Version:   "ga",
		Service:   "test",
		Operation: "test",
	}

	finished := 0
	acceptAndVerify := func() {
		go func() {
			// This and subsequent sleep calls are needed to allow the goroutine switch and
			// process events in the fakeClock. The amount of less than a second could result
			// into flakes sometimes.
			time.Sleep(time.Second)
			// Step is needed to trigger queue processing in the fakeClock.
			fakeClock.Step(0)
		}()
		err := rl.Accept(context.Background(), rlk)
		finished++
		verifyError(t, "StrategyRateLimiter.Accept()", err, nil)
	}

	// First request, not blocked
	acceptAndVerify()

	strategy.setDelay(testDelay)
	strategy.setDelayRequested(false)
	finished = 0

	// Second request, should block subsequent requests until delay has passed
	go acceptAndVerify()
	err := wait.PollImmediate(time.Second, 5*time.Second, func() (bool, error) { return strategy.isDelayRequested(), nil })
	verifyError(t, "wait.PollImmediate()", err, nil)
	time.Sleep(2 * time.Second)
	verifyBlocked(t, finished == 0)
	strategy.setDelay(0)

	// Third request
	go acceptAndVerify()
	if finished != 0 {
		t.Errorf("Some calls to StrategyRateLimiter.Accept() were not blocked, but were expected to")
	}

	// Unblock remaining requests by going forward in time
	fakeClock.Step(testDelay)
	err = wait.PollImmediate(time.Second, 10*time.Second, func() (bool, error) {
		fakeClock.Step(0)
		return finished == 2, nil
	})
	verifyError(t, "wait.PollImmediate() for finished requests", err, nil)
	if finished != 2 {
		t.Errorf("Expected 2 finished requests, but got %v", finished)
	}
}
