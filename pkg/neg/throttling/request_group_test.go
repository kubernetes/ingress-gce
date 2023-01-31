package throttling

import (
	"sync"
	"testing"
	"time"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"k8s.io/apimachinery/pkg/util/clock"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/flowcontrol"
)

const (
	testDelay = 5 * time.Second
)

type delayRequestTracker struct {
	lock           sync.Mutex
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
	*delayRequestTracker
}

func (strategy *fakeThrottlingStrategy) GetDelay() time.Duration {
	strategy.lock.Lock()
	defer strategy.lock.Unlock()
	defer strategy.setDelayRequested(true)
	return strategy.currentDelay
}

func (strategy *fakeThrottlingStrategy) PushFeedback(_ error) {}

func (strategy *fakeThrottlingStrategy) setDelay(delay time.Duration) {
	strategy.lock.Lock()
	defer strategy.lock.Unlock()
	strategy.currentDelay = delay
}

func newFakeThrottlingStrategy() *fakeThrottlingStrategy {
	return &fakeThrottlingStrategy{
		currentDelay:        0,
		delayRequestTracker: &delayRequestTracker{delayRequested: false},
	}
}

type requestHelper struct {
	lock  sync.Mutex
	count int
}

func (helper *requestHelper) increaseCount() {
	helper.lock.Lock()
	defer helper.lock.Unlock()
	helper.count++
}

func (helper *requestHelper) resetCount() {
	helper.lock.Lock()
	defer helper.lock.Unlock()
	helper.count = 0
}

func (helper *requestHelper) getCount() int {
	helper.lock.Lock()
	defer helper.lock.Unlock()
	return helper.count
}

func newRequestHelper() *requestHelper {
	return &requestHelper{
		count: 0,
	}
}

type fakeRateLimiter struct {
	flowcontrol.RateLimiter
	*delayRequestTracker
	wg sync.WaitGroup
}

func newFakeRateLimiter() *fakeRateLimiter {
	return &fakeRateLimiter{
		RateLimiter:         flowcontrol.NewFakeAlwaysRateLimiter(),
		delayRequestTracker: &delayRequestTracker{delayRequested: false},
	}
}

func (rl *fakeRateLimiter) Accept() {
	rl.setDelayRequested(true)
	rl.wg.Wait()
}

func (rl *fakeRateLimiter) setAccept(accept bool) {
	if accept {
		rl.wg.Done()
	} else {
		rl.wg.Add(1)
	}
}

func verifyRequestCount(t *testing.T, rh *requestHelper, expectedCount int) {
	realCount := rh.getCount()
	if realCount != expectedCount {
		t.Errorf("Expect request count %v, but got %v", expectedCount, realCount)
	}
}

func TestDefaultRequestGroup(t *testing.T) {
	t.Parallel()

	fakeClock := clock.NewFakeClock(time.Now())
	strategy := newFakeThrottlingStrategy()
	rh := newRequestHelper()
	rg := &defaultRequestGroup[NoResponse]{
		strategies: map[meta.Version]Strategy{meta.VersionGA: strategy},
		clock:      fakeClock,
	}
	req := func() (NoResponse, error) {
		rh.increaseCount()
		return nil, nil
	}
	verifyRequestCount(t, rh, 0)
	rg.Run(req, meta.VersionGA)
	verifyRequestCount(t, rh, 1)
	strategy.setDelayRequested(false)
	strategy.setDelay(testDelay)
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		err := wait.PollImmediate(time.Second, 5*time.Second, func() (bool, error) { return strategy.isDelayRequested(), nil })
		verifyErrorIsNil(t, err)
		time.Sleep(2 * time.Second)
		verifyRequestCount(t, rh, 1)
		fakeClock.Step(testDelay)
		wg.Done()
	}()
	rg.Run(req, meta.VersionGA)
	verifyRequestCount(t, rh, 2)
	wg.Wait()
}

func TestParallelRequestsNonBlocked(t *testing.T) {
	t.Parallel()

	fakeClock := clock.NewFakeClock(time.Now())
	strategy := newFakeThrottlingStrategy()
	rh := newRequestHelper()
	rg := &defaultRequestGroup[NoResponse]{
		strategies: map[meta.Version]Strategy{meta.VersionGA: strategy},
		clock:      fakeClock,
	}
	req := func() (NoResponse, error) {
		time.Sleep(2 * time.Second)
		rh.increaseCount()
		return nil, nil
	}
	verifyRequestCount(t, rh, 0)
	for i := 0; i < 100; i++ {
		go rg.Run(req, meta.VersionGA)
	}
	err := wait.PollImmediate(time.Millisecond, 10*time.Second, func() (bool, error) { return rh.getCount() == 100, nil })
	verifyErrorIsNil(t, err)
	verifyRequestCount(t, rh, 100)
}

func TestGroupBlock(t *testing.T) {
	t.Parallel()

	fakeClock := clock.NewFakeClock(time.Now())
	strategy := newFakeThrottlingStrategy()
	rh := newRequestHelper()
	rg := &defaultRequestGroup[NoResponse]{
		strategies: map[meta.Version]Strategy{meta.VersionGA: strategy},
		clock:      fakeClock,
	}
	req := func() (NoResponse, error) {
		rh.increaseCount()
		return nil, nil
	}
	verifyRequestCount(t, rh, 0)
	rg.Run(req, meta.VersionGA)
	verifyRequestCount(t, rh, 1)
	strategy.setDelay(testDelay)
	strategy.setDelayRequested(false)
	go rg.Run(req, meta.VersionGA)
	err := wait.PollImmediate(time.Second, 5*time.Second, func() (bool, error) { return strategy.isDelayRequested(), nil })
	verifyErrorIsNil(t, err)
	time.Sleep(2 * time.Second)
	verifyRequestCount(t, rh, 1)
	strategy.setDelay(0)
	time.Sleep(2 * time.Second)
	go rg.Run(req, meta.VersionGA)
	verifyRequestCount(t, rh, 1)
	fakeClock.Step(testDelay)
	err = wait.PollImmediate(time.Millisecond, 5*time.Second, func() (bool, error) { return rh.getCount() == 3, nil })
	verifyErrorIsNil(t, err)
	verifyRequestCount(t, rh, 3)
}

func TestQpsRequestGroup(t *testing.T) {
	t.Parallel()

	rh := newRequestHelper()
	rl := newFakeRateLimiter()
	rg := &qpsRequestGroup[NoResponse]{
		rateLimiters: map[meta.Version]flowcontrol.RateLimiter{meta.VersionGA: rl},
	}
	req := func() (NoResponse, error) {
		rh.increaseCount()
		return nil, nil
	}
	verifyRequestCount(t, rh, 0)
	rg.Run(req, meta.VersionGA)
	verifyRequestCount(t, rh, 1)
	rg.Run(req, meta.VersionGA)
	verifyRequestCount(t, rh, 2)
	rl.setDelayRequested(false)
	rl.setAccept(false)
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		err := wait.PollImmediate(time.Second, 5*time.Second, func() (bool, error) { return rl.isDelayRequested(), nil })
		verifyErrorIsNil(t, err)
		time.Sleep(2 * time.Second)
		verifyRequestCount(t, rh, 2)
		rl.setAccept(true)
		wg.Done()
	}()
	rg.Run(req, meta.VersionGA)
	verifyRequestCount(t, rh, 3)
	wg.Wait()
}
