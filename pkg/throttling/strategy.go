package throttling

import (
	"context"
	"fmt"
	"sync"
	"time"

	"k8s.io/ingress-gce/pkg/backoff"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/utils/clock"
)

const creationErrMsg = "failed to create the throttling strategy with provided parameters"

// Strategy handles delays based on feedbacks provided
type Strategy interface {
	// Delay returns the delay for next request.
	Delay() time.Duration
	// Observe recalculates the next delay based on the feedback.
	Observe(err error)
}

type defaultStrategy struct {
	lock sync.Mutex
	// backoff is the backoff.BackoffHandler which backs the Strategy
	backoff backoff.BackoffHandler
	// lastRequestTimestamp holds the timestamp of the last request
	lastRequestTimestamp time.Time
	// currentDelay is the current delay for a strategy which will be used for a Delay call
	currentDelay time.Duration
	// minDelay is the minimum delay that can be used by a Strategy if delayed
	minDelay time.Duration
	// delayed shows whether currently the strategy will return currentDelay or 0
	delayed bool
	clock   clock.Clock
}

func NewDefaultStrategy(minDelay, maxDelay time.Duration, clock clock.Clock) (Strategy, error) {
	if minDelay > maxDelay {
		return nil, fmt.Errorf("%s: minDelay=%q should not be greater than maxDelay=%q", creationErrMsg, minDelay, maxDelay)
	}
	return &defaultStrategy{
		backoff:              backoff.NewExponentialBackoffHandler(0, minDelay, maxDelay),
		lastRequestTimestamp: clock.Now(),
		currentDelay:         minDelay,
		minDelay:             minDelay,
		delayed:              false,
		clock:                clock,
	}, nil
}

// increaseDelay is expected to be called only from Observe function
// as this function requires the strategy lock, which is acquired for the whole
// invocation of Observe function
func (strategy *defaultStrategy) increaseDelay() {
	delay, _ := strategy.backoff.NextDelay()
	strategy.currentDelay = delay
	strategy.delayed = true
}

// resetDelay is expected to be called only from Observe function
// as this function requires the strategy lock, which is acquired for the whole
// invocation of Observe function
func (strategy *defaultStrategy) resetDelay() {
	strategy.backoff.ResetDelay()
	strategy.currentDelay = strategy.minDelay
	strategy.delayed = false
}

// decreaseDelay is expected to be called only from Observe function
// as this function requires the strategy lock, which is acquired for the whole
// invocation of Observe function
func (strategy *defaultStrategy) decreaseDelay() {
	if !strategy.delayed {
		return
	}
	if strategy.currentDelay == strategy.minDelay {
		strategy.resetDelay()
		return
	}
	strategy.currentDelay = strategy.backoff.DecreaseDelay()
}

// Delay returns the delay for next request, taking into account the last request timestamp.
func (strategy *defaultStrategy) Delay() time.Duration {
	strategy.lock.Lock()
	defer strategy.lock.Unlock()
	elapsed := strategy.clock.Now().Sub(strategy.lastRequestTimestamp)
	delay := strategy.currentDelay - elapsed
	if delay < 0 || !strategy.delayed {
		delay = 0
	}
	strategy.lastRequestTimestamp = strategy.clock.Now()
	return delay
}

// Observe increases the next delay if we have a quota error and reset the delay otherwise.
func (strategy *defaultStrategy) Observe(err error) {
	strategy.lock.Lock()
	defer strategy.lock.Unlock()
	if utils.IsQuotaExceededError(err) {
		strategy.increaseDelay()
	} else {
		strategy.resetDelay()
	}
}

// twoWayStrategy is backed by exponential backoff.BackoffHandler
// For each quota error the delay is increased, otherwise it's decreased.
// Also, the delay could be decreased or reset if there were no requests for some time.
type twoWayStrategy struct {
	*defaultStrategy
	noRequestsTimeoutBeforeDecreasingDelay                    time.Duration
	noRequestsTimeoutBeforeResettingDelayAfterDecreasingDelay time.Duration

	clockLock sync.Mutex
	clockCtx  context.Context

	clockCancelCtxLock sync.Mutex
	clockCancelCtx     context.CancelFunc
}

func NewTwoWayStrategy(
	minDelay time.Duration,
	maxDelay time.Duration,
	noRequestsTimeoutBeforeDecreasingDelay time.Duration,
	noRequestsTimeoutBeforeResettingDelay time.Duration,
	clock clock.Clock) (Strategy, error) {
	if noRequestsTimeoutBeforeDecreasingDelay > noRequestsTimeoutBeforeResettingDelay {
		return nil, fmt.Errorf("%s: noRequestsTimeoutBeforeDecreasingDelay=%q should not be greater than noRequestsTimeoutBeforeResettingDelay=%q", creationErrMsg, noRequestsTimeoutBeforeDecreasingDelay, noRequestsTimeoutBeforeResettingDelay)
	}
	strategy, err := NewDefaultStrategy(minDelay, maxDelay, clock)
	if err != nil {
		return nil, err
	}
	return &twoWayStrategy{
		defaultStrategy:                                           strategy.(*defaultStrategy),
		noRequestsTimeoutBeforeDecreasingDelay:                    noRequestsTimeoutBeforeDecreasingDelay,
		noRequestsTimeoutBeforeResettingDelayAfterDecreasingDelay: noRequestsTimeoutBeforeResettingDelay - noRequestsTimeoutBeforeDecreasingDelay,
	}, nil
}

// decreaseOrResetDelayAfterTimeout waits for noRequestsTimeoutBeforeDecreasingDelay
// to decrease the delay, and after that it waits for additional
// noRequestsTimeoutBeforeResettingDelayAfterDecreasingDelay. Any waiting can be
// interrupted by another request, in that case it's expected not to decrease or
// reset the timeout for the strategy.
func (strategy *twoWayStrategy) decreaseOrResetDelayAfterTimeout() {
	strategy.clockLock.Lock()
	strategy.clockCancelCtxLock.Lock()
	strategy.clockCtx, strategy.clockCancelCtx = context.WithCancel(context.Background())
	strategy.clockCancelCtxLock.Unlock()
	decreaseDelayTimeout := strategy.noRequestsTimeoutBeforeDecreasingDelay
	// resetDelayTimeout holds the time to wait before resetting the delay
	// after the decreaseDelayTimeout has passed
	resetDelayTimeout := strategy.noRequestsTimeoutBeforeResettingDelayAfterDecreasingDelay
	// this condition is to prevent resetting the delay if it will be less than
	// the current delay multiplied by 3
	if strategy.currentDelay*3 > (decreaseDelayTimeout + resetDelayTimeout) {
		resetDelayTimeout = strategy.currentDelay*3 - decreaseDelayTimeout
	}
	go func() {
		if strategy.wait(decreaseDelayTimeout, strategy.decreaseDelay) {
			strategy.wait(resetDelayTimeout, strategy.resetDelay)
		}
		strategy.clockCancelCtxLock.Lock()
		strategy.clockCancelCtx()
		strategy.clockCtx = nil
		strategy.clockCancelCtx = nil
		strategy.clockCancelCtxLock.Unlock()
		strategy.clockLock.Unlock()
	}()
}

// wait is expected to be called only from decreaseOrResetDelayAfterTimeout,
// where the clockLock is acquired, to decrease or reset the strategy delay.
func (strategy *twoWayStrategy) wait(d time.Duration, f func()) bool {
	select {
	case <-strategy.clock.After(d):
		f()
		return true
	case <-strategy.clockCtx.Done():
		return false
	}
}

// preObserve is expected to be called only from Observe function
// as this function requires the strategy lock, which is acquired for the whole
// invocation of Observe function.
func (strategy *twoWayStrategy) preObserve() {
	strategy.clockCancelCtxLock.Lock()
	defer strategy.clockCancelCtxLock.Unlock()
	if strategy.clockCancelCtx != nil {
		strategy.clockCancelCtx()
	}
}

// postObserve is expected to be called only from Observe function
// as this function requires the strategy lock, which is acquired for the whole
// invocation of Observe function
func (strategy *twoWayStrategy) postObserve() {
	if strategy.delayed {
		strategy.decreaseOrResetDelayAfterTimeout()
	}
}

// Observe increases the next delay if we have a quota error and decreases the delay otherwise.
func (strategy *twoWayStrategy) Observe(err error) {
	strategy.lock.Lock()
	defer strategy.lock.Unlock()
	strategy.preObserve()
	if utils.IsQuotaExceededError(err) {
		strategy.increaseDelay()
	} else {
		strategy.decreaseDelay()
	}
	strategy.postObserve()
}

// dynamicStrategy is backed by exponential backoff.BackoffHandler.
// This strategy holds some statistics about previous requests (errors since last success and successes since last error).
// Based on these metrics, the delay may be increased, decreased or reset.
// Also, the delay could be decreased or reset if there were no requests for some time
type dynamicStrategy struct {
	*twoWayStrategy
	errorsCount                    int
	successesCount                 int
	errorsBeforeIncreasingDelay    int
	successesBeforeDecreasingDelay int
	successesBeforeResettingDelay  int
}

func NewDynamicStrategy(
	minDelay time.Duration,
	maxDelay time.Duration,
	errorsBeforeIncreasingDelay int,
	successesBeforeDecreasingDelay int,
	successesBeforeResettingDelay int,
	noRequestsTimeoutBeforeDecreasingDelay time.Duration,
	noRequestsTimeoutBeforeResettingDelay time.Duration,
	clock clock.Clock) (Strategy, error) {
	if successesBeforeDecreasingDelay > successesBeforeResettingDelay {
		return nil, fmt.Errorf("%s: successesBeforeDecreasingDelay=%q should not be greater than successesBeforeResettingDelay=%q", creationErrMsg, successesBeforeDecreasingDelay, successesBeforeResettingDelay)
	}
	if errorsBeforeIncreasingDelay <= 0 {
		return nil, fmt.Errorf("%s: errorsBeforeIncreasingDelay=%q should be positive", creationErrMsg, errorsBeforeIncreasingDelay)
	}
	if successesBeforeDecreasingDelay <= 0 {
		return nil, fmt.Errorf("%s: successesBeforeDecreasingDelay=%q should be positive", creationErrMsg, successesBeforeDecreasingDelay)
	}
	if successesBeforeResettingDelay <= 0 {
		return nil, fmt.Errorf("%s: successesBeforeResettingDelay=%q should be positive", creationErrMsg, successesBeforeResettingDelay)
	}
	strategy, err := NewTwoWayStrategy(minDelay, maxDelay, noRequestsTimeoutBeforeDecreasingDelay, noRequestsTimeoutBeforeResettingDelay, clock)
	if err != nil {
		return nil, err
	}
	return &dynamicStrategy{
		twoWayStrategy:                 strategy.(*twoWayStrategy),
		errorsBeforeIncreasingDelay:    errorsBeforeIncreasingDelay,
		successesBeforeDecreasingDelay: successesBeforeDecreasingDelay,
		successesBeforeResettingDelay:  successesBeforeResettingDelay,
	}, nil
}

// Observe calculates the next delay based on previous number of errors and
// successes and the result of the last request.
func (strategy *dynamicStrategy) Observe(err error) {
	strategy.lock.Lock()
	defer strategy.lock.Unlock()
	strategy.preObserve()
	if utils.IsQuotaExceededError(err) {
		strategy.errorsCount++
		strategy.successesCount = 0

		if !strategy.delayed || strategy.errorsCount == strategy.errorsBeforeIncreasingDelay {
			strategy.increaseDelay()
		}

		if strategy.errorsCount == strategy.errorsBeforeIncreasingDelay {
			strategy.errorsCount = 0
		}
	} else {
		strategy.errorsCount = 0

		if strategy.delayed {
			strategy.successesCount++

			if strategy.successesCount == strategy.successesBeforeResettingDelay {
				strategy.resetDelay()
				strategy.successesCount = 0
			} else if strategy.successesCount == strategy.successesBeforeDecreasingDelay {
				strategy.decreaseDelay()
			}
		}
	}
	strategy.postObserve()
}
