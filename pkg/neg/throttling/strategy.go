package throttling

import (
	"context"
	"fmt"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/util/clock"
	"k8s.io/ingress-gce/pkg/neg/backoff"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog/v2"
)

const (
	defaultMinDelay                               = 5 * time.Millisecond
	defaultMaxDelay                               = 1000 * time.Second
	defaultNoRequestsTimeoutBeforeDecreasingDelay = 30 * time.Second
	defaultNoRequestsTimeoutBeforeResettingDelay  = 1 * time.Minute
	defaultErrorsBeforeIncreasingDelay            = 2
	defaultSuccessesBeforeDecreasingDelay         = 5
	defaultSuccessesBeforeResettingDelay          = 10

	creationErrMsg = "Failed to use provided parameters for the throttling strategy, fallback to default ones"
)

// Strategy handles delays based on feedbacks provided.
type Strategy interface {
	// GetDelay returns the delay for next request.
	GetDelay() time.Duration
	// PushFeedback recalculates the next delay based on the feedback.
	PushFeedback(err error)
}

// defaultStrategy is backed by generic exponential types.BackoffHandler.
// For each group error the delay is increased, otherwise it's reset.
type defaultStrategy struct {
	lock                 sync.Mutex
	backoff              backoff.BackoffHandler
	lastRequestTimestamp time.Time
	currentDelay         time.Duration
	minDelay             time.Duration
	delayed              bool
	clock                clock.Clock
	logger               klog.Logger
}

func NewDefaultStrategy(minDelay, maxDelay time.Duration, clock clock.Clock, logger klog.Logger) *defaultStrategy {
	return newIntermediateDefaultStrategy(
		minDelay,
		maxDelay,
		clock,
		logger.WithName("DefaultThrottlingStrategy"),
	)
}

func newIntermediateDefaultStrategy(
	minDelay time.Duration,
	maxDelay time.Duration,
	clock clock.Clock,
	logger klog.Logger) *defaultStrategy {
	if minDelay > maxDelay {
		err := fmt.Errorf("minDelay=%q should not be greater than maxDelay=%q", minDelay, maxDelay)
		logger.Error(err, creationErrMsg)
		minDelay = defaultMinDelay
		maxDelay = defaultMaxDelay
	}
	return &defaultStrategy{
		backoff:              backoff.NewTwoWayExponentialBackoffHandler(0, minDelay, maxDelay),
		lastRequestTimestamp: clock.Now(),
		currentDelay:         minDelay,
		minDelay:             minDelay,
		delayed:              false,
		clock:                clock,
		logger:               logger,
	}
}

func (strategy *defaultStrategy) increaseDelay() {
	delay, _ := strategy.backoff.NextDelay()
	strategy.currentDelay = delay
	strategy.delayed = true
}

func (strategy *defaultStrategy) resetDelay() {
	strategy.backoff.ResetDelay()
	strategy.currentDelay = strategy.minDelay
	strategy.delayed = false
}

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

// GetDelay returns the delay for next request, taking into account the last request timestamp.
func (strategy *defaultStrategy) GetDelay() time.Duration {
	strategy.lock.Lock()
	defer strategy.lock.Unlock()
	elapsed := strategy.clock.Now().Sub(strategy.lastRequestTimestamp)
	delay := strategy.currentDelay - elapsed
	if delay < 0 || !strategy.delayed {
		delay = 0
	}
	return delay
}

func (strategy *defaultStrategy) PushFeedback(err error) {
	strategy.lock.Lock()
	defer strategy.lock.Unlock()
	strategy.lastRequestTimestamp = strategy.clock.Now()
	if utils.IsQuotaExceededError(err) {
		strategy.increaseDelay()
	} else {
		strategy.resetDelay()
	}
}

// twoWayStrategy is backed by exponential types.TwoWayBackoffHandler.
// For each group error the delay is increased, otherwise it's decreased.
// Also, the delay could be decreased or reset if there were no requests for some time.
type twoWayStrategy struct {
	*defaultStrategy
	noRequestsTimeoutBeforeDecreasingDelay                    time.Duration
	noRequestsTimeoutBeforeResettingDelayAfterDecreasingDelay time.Duration
	clockLock                                                 sync.Mutex
	clockCtx                                                  context.Context
	clockCancelCtx                                            context.CancelFunc
}

func NewTwoWayStrategy(
	minDelay time.Duration,
	maxDelay time.Duration,
	noRequestsTimeoutBeforeDecreasingDelay time.Duration,
	noRequestsTimeoutBeforeResettingDelay time.Duration,
	clock clock.Clock,
	logger klog.Logger) *twoWayStrategy {
	return newIntermediateTwoWayStrategy(
		minDelay,
		maxDelay,
		noRequestsTimeoutBeforeDecreasingDelay,
		noRequestsTimeoutBeforeResettingDelay,
		clock,
		logger.WithName("TwoWayThrottlingStrategy"),
	)
}

func newIntermediateTwoWayStrategy(
	minDelay time.Duration,
	maxDelay time.Duration,
	noRequestsTimeoutBeforeDecreasingDelay time.Duration,
	noRequestsTimeoutBeforeResettingDelay time.Duration,
	clock clock.Clock,
	logger klog.Logger) *twoWayStrategy {
	if noRequestsTimeoutBeforeDecreasingDelay > noRequestsTimeoutBeforeResettingDelay {
		err := fmt.Errorf("noRequestsTimeoutBeforeDecreasingDelay=%q should not be greater than noRequestsTimeoutBeforeResettingDelay=%q", noRequestsTimeoutBeforeDecreasingDelay, noRequestsTimeoutBeforeResettingDelay)
		logger.Error(err, creationErrMsg)
		minDelay = defaultMinDelay
		maxDelay = defaultMaxDelay
		noRequestsTimeoutBeforeDecreasingDelay = defaultNoRequestsTimeoutBeforeDecreasingDelay
		noRequestsTimeoutBeforeResettingDelay = defaultNoRequestsTimeoutBeforeResettingDelay
	}
	strategy := newIntermediateDefaultStrategy(minDelay, maxDelay, clock, logger)
	return &twoWayStrategy{
		defaultStrategy:                                           strategy,
		noRequestsTimeoutBeforeDecreasingDelay:                    noRequestsTimeoutBeforeDecreasingDelay,
		noRequestsTimeoutBeforeResettingDelayAfterDecreasingDelay: noRequestsTimeoutBeforeResettingDelay - noRequestsTimeoutBeforeDecreasingDelay,
	}
}

func (strategy *twoWayStrategy) decreaseOrResetDelayAfterTimeout() {
	strategy.clockLock.Lock()
	strategy.clockCtx, strategy.clockCancelCtx = context.WithCancel(context.Background())
	decreaseDelayTimeout := strategy.noRequestsTimeoutBeforeDecreasingDelay
	resetDelayTimeout := strategy.noRequestsTimeoutBeforeResettingDelayAfterDecreasingDelay
	if strategy.currentDelay*3/2 > decreaseDelayTimeout {
		decreaseDelayTimeout = strategy.currentDelay * 3 / 2
	}
	if strategy.currentDelay*3 > (decreaseDelayTimeout + resetDelayTimeout) {
		resetDelayTimeout = strategy.currentDelay*3 - decreaseDelayTimeout
	}
	go func() {
		if strategy.wait(decreaseDelayTimeout, strategy.decreaseDelay) {
			strategy.wait(resetDelayTimeout, strategy.resetDelay)
		}
		strategy.clockCancelCtx()
		strategy.clockCtx = nil
		strategy.clockCancelCtx = nil
		strategy.clockLock.Unlock()
	}()
}

func (strategy *twoWayStrategy) wait(d time.Duration, f func()) bool {
	select {
	case <-strategy.clock.After(d):
		f()
		return true
	case <-strategy.clockCtx.Done():
		return false
	}
}

func (strategy *twoWayStrategy) preProcessFeedback() {
	strategy.lastRequestTimestamp = strategy.clock.Now()
	if strategy.clockCancelCtx != nil {
		strategy.clockCancelCtx()
	}
}

func (strategy *twoWayStrategy) postProcessFeedback() {
	if strategy.delayed {
		strategy.decreaseOrResetDelayAfterTimeout()
	}
}

func (strategy *twoWayStrategy) PushFeedback(err error) {
	strategy.lock.Lock()
	defer strategy.lock.Unlock()
	strategy.preProcessFeedback()
	if utils.IsQuotaExceededError(err) {
		strategy.increaseDelay()
	} else {
		strategy.decreaseDelay()
	}
	strategy.postProcessFeedback()
}

// dynamicTwoWayStrategy is backed by exponential types.TwoWayBackoffHandler.
// This strategy holds some statistics about previous requests (errors since last success and successes since last error).
// Based on these metrics, the delay may be increased, decreased or reset.
// Also, the delay could be decreased or reset if there were no requests for some time.
type dynamicTwoWayStrategy struct {
	*twoWayStrategy
	errorsCount                    int
	successesCount                 int
	errorsBeforeIncreasingDelay    int
	successesBeforeDecreasingDelay int
	successesBeforeResettingDelay  int
}

func NewDynamicTwoWayStrategy(
	minDelay time.Duration,
	maxDelay time.Duration,
	errorsBeforeIncreasingDelay int,
	successesBeforeDecreasingDelay int,
	successesBeforeResettingDelay int,
	noRequestsTimeoutBeforeDecreasingDelay time.Duration,
	noRequestsTimeoutBeforeResettingDelay time.Duration,
	clock clock.Clock,
	logger klog.Logger) *dynamicTwoWayStrategy {
	logger = logger.WithName("DynamicTwoWayThrottlingStrategy")
	if successesBeforeDecreasingDelay > successesBeforeResettingDelay {
		err := fmt.Errorf("successesBeforeDecreasingDelay=%q should not be greater than successesBeforeResettingDelay=%q", successesBeforeDecreasingDelay, successesBeforeResettingDelay)
		logger.Error(err, creationErrMsg)
		minDelay = defaultMinDelay
		maxDelay = defaultMaxDelay
		errorsBeforeIncreasingDelay = defaultErrorsBeforeIncreasingDelay
		successesBeforeDecreasingDelay = defaultSuccessesBeforeDecreasingDelay
		successesBeforeResettingDelay = defaultSuccessesBeforeResettingDelay
		noRequestsTimeoutBeforeDecreasingDelay = defaultNoRequestsTimeoutBeforeDecreasingDelay
		noRequestsTimeoutBeforeResettingDelay = defaultNoRequestsTimeoutBeforeResettingDelay
	}
	strategy := newIntermediateTwoWayStrategy(minDelay, maxDelay, noRequestsTimeoutBeforeDecreasingDelay, noRequestsTimeoutBeforeResettingDelay, clock, logger)
	return &dynamicTwoWayStrategy{
		twoWayStrategy:                 strategy,
		errorsBeforeIncreasingDelay:    errorsBeforeIncreasingDelay,
		successesBeforeDecreasingDelay: successesBeforeDecreasingDelay,
		successesBeforeResettingDelay:  successesBeforeResettingDelay,
	}
}

func (strategy *dynamicTwoWayStrategy) PushFeedback(err error) {
	strategy.lock.Lock()
	defer strategy.lock.Unlock()
	strategy.preProcessFeedback()
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
	strategy.postProcessFeedback()
}
