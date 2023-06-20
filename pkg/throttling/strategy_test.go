package throttling

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"google.golang.org/api/googleapi"
	clocktesting "k8s.io/utils/clock/testing"
)

const (
	testMinDelay                               = 5 * time.Second
	testMaxDelay                               = 30 * time.Minute
	testNoRequestsTimeoutBeforeDecreasingDelay = 1 * time.Hour
	testNoRequestsTimeoutBeforeResettingDelay  = 2 * time.Hour
	testErrorsBeforeIncreasingDelay            = 2
	testSuccessesBeforeDecreasingDelay         = 2
	testSuccessesBeforeResettingDelay          = 5
)

func verifyErrorIsNil(t *testing.T, err error) {
	t.Helper()

	if err != nil {
		t.Errorf("Expect error to be nil, but got %v", err)
	}
}

func verifyExactDelay(t *testing.T, strategy Strategy, expectedDelay time.Duration) {
	t.Helper()

	delay := strategy.Delay()
	if delay != expectedDelay {
		t.Errorf("Expect delay = %v, but got %v", expectedDelay, delay)
	}
}

func verifyIntervalDelay(t *testing.T, strategy Strategy, expectedDelayMin, expectedDelayMax time.Duration) {
	t.Helper()

	delay := strategy.Delay()
	if delay < expectedDelayMin || delay > expectedDelayMax {
		t.Errorf("Expect delay >= %v and delay <= %v, but got %v", expectedDelayMin, expectedDelayMax, delay)
	}
}

func verifyIncreasedDelay(t *testing.T, strategy Strategy, expectedDelayInitial time.Duration) {
	t.Helper()

	expectedDelayMax := expectedDelayInitial * 2
	if expectedDelayMax > testMaxDelay {
		expectedDelayMax = testMaxDelay
	}
	verifyIntervalDelay(t, strategy, expectedDelayInitial, expectedDelayMax)
}

func verifyDecreasedDelay(t *testing.T, strategy Strategy, expectedDelayInitial time.Duration) {
	t.Helper()

	expectedDelayMin := expectedDelayInitial / 2
	if expectedDelayMin < testMinDelay {
		expectedDelayMin = testMinDelay
	}
	verifyIntervalDelay(t, strategy, expectedDelayMin, expectedDelayInitial)
}

func TestDefaultStrategy(t *testing.T) {
	t.Parallel()

	fakeClock := clocktesting.NewFakeClock(time.Now())
	strategy, err := NewDefaultStrategy(testMinDelay, testMaxDelay, fakeClock)
	verifyErrorIsNil(t, err)
	verifyExactDelay(t, strategy, 0)
	quotaErr := &googleapi.Error{Code: 429}
	// Verify that the delay would be testMinDelay after the first quota error
	strategy.Observe(quotaErr)
	expectedDelay := testMinDelay
	verifyExactDelay(t, strategy, expectedDelay)

	// Verify that after quota errors the delay is increased every time until
	// it reaches textMaxDelay
	for expectedDelay != testMaxDelay {
		strategy.Observe(quotaErr)
		verifyIncreasedDelay(t, strategy, expectedDelay)
		expectedDelay = strategy.Delay()
	}

	// Verify that after reaching testMaxDelay another quota error won't affect the delay
	strategy.Observe(quotaErr)
	verifyExactDelay(t, strategy, expectedDelay)
	// Verify that delay is reset after a successful request
	strategy.Observe(nil)
	verifyExactDelay(t, strategy, 0)
}

func TestDifferentErrors(t *testing.T) {
	t.Parallel()

	fakeClock := clocktesting.NewFakeClock(time.Now())
	strategy, err := NewDefaultStrategy(testMinDelay, testMaxDelay, fakeClock)
	verifyErrorIsNil(t, err)
	for _, tc := range []struct {
		desc        string
		err         error
		expectDelay bool
	}{
		{
			desc:        "successful request",
			err:         nil,
			expectDelay: false,
		},
		{
			desc:        "non quota error",
			err:         fmt.Errorf("non quota error"),
			expectDelay: false,
		},
		{
			desc:        "quota exceeded error",
			err:         &googleapi.Error{Code: 429},
			expectDelay: true,
		},
		{
			desc:        "wrapped another error and a quota exceeded error",
			err:         fmt.Errorf("%w: %w", errors.New("another error"), &googleapi.Error{Code: 429}),
			expectDelay: true,
		},
		{
			desc:        "wrapped quota exceeded error and another error",
			err:         fmt.Errorf("%w: %w", &googleapi.Error{Code: 429}, errors.New("another error")),
			expectDelay: true,
		},
		{
			desc: "rate limit exceeded error",
			err: &googleapi.Error{
				Code:    403,
				Message: "quota error",
				Errors: []googleapi.ErrorItem{{
					Reason:  "rateLimitExceeded",
					Message: "quota error",
				}},
			},
			expectDelay: true,
		},
		{
			desc:        "server error",
			err:         &googleapi.Error{Code: 500},
			expectDelay: false,
		},
	} {
		strategy.(*defaultStrategy).resetDelay()
		strategy.Observe(tc.err)
		expectedDelay := time.Duration(0)
		if tc.expectDelay {
			expectedDelay = testMinDelay
		}
		verifyExactDelay(t, strategy, expectedDelay)
	}
}

func TestDelayWithElapsedTime(t *testing.T) {
	t.Parallel()

	fakeClock := clocktesting.NewFakeClock(time.Now())
	strategy, err := NewDefaultStrategy(testMinDelay, testMaxDelay, fakeClock)
	verifyErrorIsNil(t, err)
	quotaErr := &googleapi.Error{Code: 429}

	for _, tc := range []struct {
		desc    string
		elapsed time.Duration
	}{
		{
			desc:    "no elapsed time",
			elapsed: time.Duration(0),
		},
		{
			desc:    "elapsed 1 second",
			elapsed: time.Second,
		},
		{
			desc:    "elapsed currentDelay-1",
			elapsed: testMinDelay - 1,
		},
		{
			desc:    "elapsed currentDelay",
			elapsed: testMinDelay,
		},
		{
			desc:    "elapsed currentDelay+1",
			elapsed: testMinDelay + 1,
		},
	} {
		strategy.(*defaultStrategy).resetDelay()
		strategy.Observe(quotaErr)
		expectedDelay := testMinDelay - tc.elapsed
		if expectedDelay < 0 {
			expectedDelay = 0
		}
		fakeClock.Step(tc.elapsed)
		verifyExactDelay(t, strategy, expectedDelay)
	}
}

func TestTwoWayStrategy(t *testing.T) {
	t.Parallel()

	fakeClock := clocktesting.NewFakeClock(time.Now())
	strategy, err := NewTwoWayStrategy(testMinDelay, testMaxDelay, testNoRequestsTimeoutBeforeDecreasingDelay, testNoRequestsTimeoutBeforeResettingDelay, fakeClock)
	verifyErrorIsNil(t, err)
	verifyExactDelay(t, strategy, 0)
	quotaErr := &googleapi.Error{Code: 429}
	strategy.Observe(quotaErr)
	expectedDelay := testMinDelay
	verifyExactDelay(t, strategy, expectedDelay)

	for i := 0; i < 10; i++ {
		expectedDelay = strategy.Delay()
		strategy.Observe(quotaErr)
		verifyIncreasedDelay(t, strategy, expectedDelay)
	}

	for {
		expectedDelay = strategy.Delay()
		strategy.Observe(nil)

		if expectedDelay == testMinDelay {
			break
		}

		verifyDecreasedDelay(t, strategy, expectedDelay)
	}

	verifyExactDelay(t, strategy, 0)
}

func TestResetDelayAfterTimeout(t *testing.T) {
	t.Parallel()

	startTime := time.Now()
	fakeClock := clocktesting.NewFakeClock(startTime)
	strategy, err := NewTwoWayStrategy(testMinDelay, testMaxDelay, testNoRequestsTimeoutBeforeDecreasingDelay, testNoRequestsTimeoutBeforeResettingDelay, fakeClock)
	verifyErrorIsNil(t, err)
	verifyExactDelay(t, strategy, 0)
	quotaErr := &googleapi.Error{Code: 429}

	for i := 0; i < 100; i++ {
		strategy.Observe(quotaErr)
	}

	// This and subsequent sleep calls are needed to allow the goroutine switch and
	// process events in the fakeClock. The amount of less than a second could result
	// into flakes sometimes.
	time.Sleep(time.Second)
	fakeClock.Step(testNoRequestsTimeoutBeforeDecreasingDelay)
	time.Sleep(time.Second)
	fakeClock.Step(testNoRequestsTimeoutBeforeResettingDelay - testNoRequestsTimeoutBeforeDecreasingDelay)
	time.Sleep(time.Second)
	// it's needed to check the delay without elapsed time from the last request
	fakeClock.SetTime(startTime)
	verifyExactDelay(t, strategy, 0)
}

func TestDynamicStrategy(t *testing.T) {
	t.Parallel()

	fakeClock := clocktesting.NewFakeClock(time.Now())
	strategy, err := NewDynamicStrategy(testMinDelay, testMaxDelay, testErrorsBeforeIncreasingDelay, testSuccessesBeforeDecreasingDelay, testSuccessesBeforeResettingDelay, testNoRequestsTimeoutBeforeDecreasingDelay, testNoRequestsTimeoutBeforeResettingDelay, fakeClock)
	verifyErrorIsNil(t, err)
	quotaErr := &googleapi.Error{Code: 429}

	type expectedDeltaDelay int
	const (
		resetDelay expectedDeltaDelay = iota
		decreaseDelay
		unchangedDelay
		increaseDelay
	)

	for _, tc := range []struct {
		desc string
		errs []error
		// -2 = reset delay, -1 = decrease delay, 0 = no change, 1 = increase delay
		expectedDeltaDelays []expectedDeltaDelay
	}{
		{
			desc:                "2 errors",
			errs:                []error{quotaErr, quotaErr},
			expectedDeltaDelays: []expectedDeltaDelay{increaseDelay, increaseDelay},
		},
		{
			desc:                "4 errors",
			errs:                []error{quotaErr, quotaErr, quotaErr, quotaErr},
			expectedDeltaDelays: []expectedDeltaDelay{increaseDelay, increaseDelay, unchangedDelay, increaseDelay},
		},
		{
			desc:                "4 errors and 2 successes",
			errs:                []error{quotaErr, quotaErr, quotaErr, quotaErr, nil, nil},
			expectedDeltaDelays: []expectedDeltaDelay{increaseDelay, increaseDelay, unchangedDelay, increaseDelay, unchangedDelay, decreaseDelay},
		},
		{
			desc:                "8 errors and 5 successes",
			errs:                []error{quotaErr, quotaErr, quotaErr, quotaErr, quotaErr, quotaErr, quotaErr, quotaErr, nil, nil, nil, nil, nil},
			expectedDeltaDelays: []expectedDeltaDelay{increaseDelay, increaseDelay, unchangedDelay, increaseDelay, unchangedDelay, increaseDelay, unchangedDelay, increaseDelay, unchangedDelay, decreaseDelay, unchangedDelay, unchangedDelay, resetDelay},
		},
		{
			desc:                "alternate error and success",
			errs:                []error{quotaErr, nil, quotaErr, nil, quotaErr},
			expectedDeltaDelays: []expectedDeltaDelay{increaseDelay, unchangedDelay, unchangedDelay, unchangedDelay, unchangedDelay},
		},
		{
			desc:                "4 errors, 4 successes and 2 errors",
			errs:                []error{quotaErr, quotaErr, quotaErr, quotaErr, nil, nil, nil, nil, quotaErr, quotaErr},
			expectedDeltaDelays: []expectedDeltaDelay{increaseDelay, increaseDelay, unchangedDelay, increaseDelay, unchangedDelay, decreaseDelay, unchangedDelay, unchangedDelay, unchangedDelay, increaseDelay},
		},
		{
			desc:                "1 error, 5 successes and 1 error",
			errs:                []error{quotaErr, nil, nil, nil, nil, nil, quotaErr},
			expectedDeltaDelays: []expectedDeltaDelay{increaseDelay, unchangedDelay, decreaseDelay, unchangedDelay, unchangedDelay, unchangedDelay, increaseDelay},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			strategy.(*dynamicStrategy).resetDelay()
			strategy.Observe(nil)
			for i, err := range tc.errs {
				previousDelay := strategy.Delay()
				expectedDeltaDelay := tc.expectedDeltaDelays[i]
				strategy.Observe(err)
				switch expectedDeltaDelay {
				case resetDelay:
				case decreaseDelay:
					if previousDelay == testMinDelay || expectedDeltaDelay < decreaseDelay {
						verifyExactDelay(t, strategy, 0)
					} else {
						verifyDecreasedDelay(t, strategy, previousDelay)
					}
					break
				case increaseDelay:
					if previousDelay == 0 {
						verifyExactDelay(t, strategy, testMinDelay)
					} else {
						verifyIncreasedDelay(t, strategy, previousDelay)
					}
					break
				case unchangedDelay:
					verifyExactDelay(t, strategy, previousDelay)
				}
			}
		})
	}
}
