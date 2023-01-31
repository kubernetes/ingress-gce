package throttling

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"google.golang.org/api/googleapi"
	"k8s.io/apimachinery/pkg/util/clock"
	"k8s.io/klog/v2"
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
	if err != nil {
		t.Errorf("Expect error to be nil, but got %v", err)
	}
}

func getTcPrefix(tcDesc string) string {
	prefix := ""
	if tcDesc != "" {
		prefix = fmt.Sprintf("For test case %q, ", tcDesc)
	}
	return prefix
}

func verifyExactDelay(t *testing.T, strategy Strategy, expectedDelay time.Duration, tcDesc string) {
	delay := strategy.GetDelay()
	if delay != expectedDelay {
		t.Errorf("%vExpect delay = %v, but got %v", getTcPrefix(tcDesc), expectedDelay, delay)
	}
}

func verifyIntervalDelay(t *testing.T, strategy Strategy, expectedDelayMin, expectedDelayMax time.Duration, tcDesc string) {
	delay := strategy.GetDelay()
	if !(delay >= expectedDelayMin && delay <= expectedDelayMax) {
		t.Errorf("%vExpect delay >= %v and delay <= %v, but got %v", getTcPrefix(tcDesc), expectedDelayMin, expectedDelayMax, delay)
	}
}

func verifyIncreasedDelay(t *testing.T, strategy Strategy, expectedDelayInitial time.Duration, tcDesc string) {
	expectDelayMax := expectedDelayInitial * 2
	if expectDelayMax > testMaxDelay {
		expectDelayMax = testMaxDelay
	}
	verifyIntervalDelay(t, strategy, expectedDelayInitial, expectDelayMax, tcDesc)
}

func verifyDecreasedDelay(t *testing.T, strategy Strategy, expectedDelayInitial time.Duration, tcDesc string) {
	expectDelayMin := expectedDelayInitial / 2
	if expectDelayMin < testMinDelay {
		expectDelayMin = testMinDelay
	}
	verifyIntervalDelay(t, strategy, expectDelayMin, expectedDelayInitial, tcDesc)
}

func TestDefaultStrategy(t *testing.T) {
	t.Parallel()

	fakeClock := clock.NewFakeClock(time.Now())
	strategy := NewDefaultStrategy(testMinDelay, testMaxDelay, fakeClock, klog.TODO())
	verifyExactDelay(t, strategy, 0, "")
	groupErr := &googleapi.Error{Code: http.StatusTooManyRequests}
	strategy.PushFeedback(groupErr)
	expectedDelay := testMinDelay
	verifyExactDelay(t, strategy, expectedDelay, "")

	for expectedDelay != testMaxDelay {
		strategy.PushFeedback(groupErr)
		verifyIncreasedDelay(t, strategy, expectedDelay, "")
		expectedDelay = strategy.GetDelay()
	}

	strategy.PushFeedback(groupErr)
	verifyExactDelay(t, strategy, expectedDelay, "")
	strategy.PushFeedback(nil)
	verifyExactDelay(t, strategy, 0, "")
}

func TestDifferentErrors(t *testing.T) {
	t.Parallel()

	fakeClock := clock.NewFakeClock(time.Now())
	strategy := NewDefaultStrategy(testMinDelay, testMaxDelay, fakeClock, klog.TODO())
	for _, tc := range []struct {
		desc        string
		feedback    error
		expectDelay bool
	}{
		{
			desc:        "successful request",
			feedback:    nil,
			expectDelay: false,
		},
		{
			desc:        "individual error",
			feedback:    fmt.Errorf("individual error"),
			expectDelay: false,
		},
		{
			desc:        "quota exceeded error",
			feedback:    &googleapi.Error{Code: http.StatusTooManyRequests},
			expectDelay: true,
		},
		{
			desc:        "server error",
			feedback:    &googleapi.Error{Code: http.StatusInternalServerError},
			expectDelay: false,
		},
	} {
		strategy.resetDelay()
		strategy.PushFeedback(tc.feedback)
		expectedDelay := time.Duration(0)
		if tc.expectDelay {
			expectedDelay = testMinDelay
		}
		verifyExactDelay(t, strategy, expectedDelay, tc.desc)
	}
}

func TestDelayWithElapsedTime(t *testing.T) {
	t.Parallel()

	fakeClock := clock.NewFakeClock(time.Now())
	strategy := NewDefaultStrategy(testMinDelay, testMaxDelay, fakeClock, klog.TODO())
	groupErr := &googleapi.Error{Code: http.StatusTooManyRequests}

	for _, tc := range []struct {
		desc    string
		elapsed time.Duration
	}{
		{
			desc:    "no elapsed time",
			elapsed: 0,
		},
		{
			desc:    "elapsed 1 second",
			elapsed: 1 * time.Second,
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
		strategy.resetDelay()
		strategy.PushFeedback(groupErr)
		expectedDelay := testMinDelay - tc.elapsed
		if expectedDelay < 0 {
			expectedDelay = 0
		}
		fakeClock.Step(tc.elapsed)
		verifyExactDelay(t, strategy, expectedDelay, tc.desc)
	}
}

func TestTwoWayStrategy(t *testing.T) {
	t.Parallel()

	fakeClock := clock.NewFakeClock(time.Now())
	strategy := NewTwoWayStrategy(testMinDelay, testMaxDelay, testNoRequestsTimeoutBeforeDecreasingDelay, testNoRequestsTimeoutBeforeResettingDelay, fakeClock, klog.TODO())
	verifyExactDelay(t, strategy, 0, "")
	groupErr := &googleapi.Error{Code: http.StatusTooManyRequests}
	strategy.PushFeedback(groupErr)
	expectedDelay := testMinDelay
	verifyExactDelay(t, strategy, expectedDelay, "")

	for i := 0; i < 10; i++ {
		expectedDelay = strategy.GetDelay()
		strategy.PushFeedback(groupErr)
		verifyIncreasedDelay(t, strategy, expectedDelay, "")
	}

	for {
		expectedDelay = strategy.GetDelay()
		strategy.PushFeedback(nil)

		if expectedDelay == testMinDelay {
			break
		}

		verifyDecreasedDelay(t, strategy, expectedDelay, "")
	}

	verifyExactDelay(t, strategy, 0, "")
}

func TestResetDelayAfterTimeout(t *testing.T) {
	t.Parallel()

	startTime := time.Now()
	fakeClock := clock.NewFakeClock(startTime)
	strategy := NewTwoWayStrategy(testMinDelay, testMaxDelay, testNoRequestsTimeoutBeforeDecreasingDelay, testNoRequestsTimeoutBeforeResettingDelay, fakeClock, klog.TODO())
	verifyExactDelay(t, strategy, 0, "")
	groupErr := &googleapi.Error{Code: http.StatusTooManyRequests}

	for i := 0; i < 100; i++ {
		strategy.PushFeedback(groupErr)
	}

	time.Sleep(1 * time.Second)
	fakeClock.Step(testNoRequestsTimeoutBeforeDecreasingDelay)
	time.Sleep(1 * time.Second)
	fakeClock.Step(testNoRequestsTimeoutBeforeResettingDelay - testNoRequestsTimeoutBeforeDecreasingDelay)
	time.Sleep(1 * time.Second)
	fakeClock.SetTime(startTime)
	verifyExactDelay(t, strategy, 0, "")
}

func TestDynamicTwoWayStrategy(t *testing.T) {
	t.Parallel()

	fakeClock := clock.NewFakeClock(time.Now())
	strategy := NewDynamicTwoWayStrategy(testMinDelay, testMaxDelay, testErrorsBeforeIncreasingDelay, testSuccessesBeforeDecreasingDelay, testSuccessesBeforeResettingDelay, testNoRequestsTimeoutBeforeDecreasingDelay, testNoRequestsTimeoutBeforeResettingDelay, fakeClock, klog.TODO())
	groupErr := &googleapi.Error{Code: http.StatusTooManyRequests}

	for _, tc := range []struct {
		desc                string
		feedbacks           []error
		expectedDeltaDelays []int
	}{
		{
			desc:                "2 errors",
			feedbacks:           []error{groupErr, groupErr},
			expectedDeltaDelays: []int{1, 1},
		},
		{
			desc:                "4 errors",
			feedbacks:           []error{groupErr, groupErr, groupErr, groupErr},
			expectedDeltaDelays: []int{1, 1, 0, 1},
		},
		{
			desc:                "4 errors and 2 successes",
			feedbacks:           []error{groupErr, groupErr, groupErr, groupErr, nil, nil},
			expectedDeltaDelays: []int{1, 1, 0, 1, 0, -1},
		},
		{
			desc:                "8 errors and 5 successes",
			feedbacks:           []error{groupErr, groupErr, groupErr, groupErr, groupErr, groupErr, groupErr, groupErr, nil, nil, nil, nil, nil},
			expectedDeltaDelays: []int{1, 1, 0, 1, 0, 1, 0, 1, 0, -1, 0, 0, -2},
		},
		{
			desc:                "alternate error and success",
			feedbacks:           []error{groupErr, nil, groupErr, nil, groupErr},
			expectedDeltaDelays: []int{1, 0, 0, 0, 0},
		},
		{
			desc:                "4 errors, 4 successes and 2 errors",
			feedbacks:           []error{groupErr, groupErr, groupErr, groupErr, nil, nil, nil, nil, groupErr, groupErr},
			expectedDeltaDelays: []int{1, 1, 0, 1, 0, -1, 0, 0, 0, 1},
		},
		{
			desc:                "1 error, 5 successes and 1 error",
			feedbacks:           []error{groupErr, nil, nil, nil, nil, nil, groupErr},
			expectedDeltaDelays: []int{1, 0, -2, 0, 0, 0, 1},
		},
	} {
		strategy.resetDelay()
		strategy.PushFeedback(nil)
		for i, feedback := range tc.feedbacks {
			expectedDelay := strategy.GetDelay()
			expectedDeltaDelay := tc.expectedDeltaDelays[i]
			strategy.PushFeedback(feedback)
			if expectedDeltaDelay < 0 {
				if expectedDelay == testMinDelay || expectedDeltaDelay < -1 {
					verifyExactDelay(t, strategy, 0, tc.desc)
				} else {
					verifyDecreasedDelay(t, strategy, expectedDelay, tc.desc)
				}
			} else if expectedDeltaDelay > 0 {
				if expectedDelay == 0 {
					verifyExactDelay(t, strategy, testMinDelay, tc.desc)
				} else {
					verifyIncreasedDelay(t, strategy, expectedDelay, tc.desc)
				}
			} else {
				verifyExactDelay(t, strategy, expectedDelay, tc.desc)
			}
		}
	}
}
