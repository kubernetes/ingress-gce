package controllers

import (
	"fmt"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/common"
	"k8s.io/klog/v2"
)

func TestPeriodicEnqueuerOffset(t *testing.T) {
	const (
		period = 10 * time.Minute
		keys   = 1000
	)
	testCases := []struct {
		desc         string
		resyncPeriod time.Duration
		spreadFactor float64
		// Every offset must be within [0, wantMax).
		wantMax time.Duration
		// wantSpread requires offsets to be spread over the window: both
		// halves of [0, wantMax) must be hit. With 1000 keys hashed into the
		// window, the probability of a spurious failure is 2 * 0.5^1000.
		wantSpread bool
	}{
		{
			desc:         "spreading disabled",
			resyncPeriod: period,
			spreadFactor: 0,
			wantMax:      1,
		},
		{
			desc:         "negative factor is treated as disabled",
			resyncPeriod: period,
			spreadFactor: -0.5,
			wantMax:      1,
		},
		{
			desc:         "zero resync period disables spreading",
			resyncPeriod: 0,
			spreadFactor: 1,
			wantMax:      1,
		},
		{
			desc:         "spread over half of the period",
			resyncPeriod: period,
			spreadFactor: 0.5,
			wantMax:      5 * time.Minute,
			wantSpread:   true,
		},
		{
			desc:         "spread over the whole period",
			resyncPeriod: period,
			spreadFactor: 1,
			wantMax:      period,
			wantSpread:   true,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			p := newPeriodicEnqueuer(tc.resyncPeriod, tc.spreadFactor)
			mid := tc.wantMax / 2
			var belowMid, fromMid int
			for i := 0; i < keys; i++ {
				key := fmt.Sprintf("namespace/service-%d", i)
				got := p.offset(key)
				if got < 0 || got >= tc.wantMax {
					t.Fatalf("offset(%q) = %v, want in [0, %v)", key, got, tc.wantMax)
				}
				if again := p.offset(key); again != got {
					t.Fatalf("offset(%q) = %v on the second call, want the stable value %v", key, again, got)
				}
				if got < mid {
					belowMid++
				} else {
					fromMid++
				}
			}
			if tc.wantSpread && (belowMid == 0 || fromMid == 0) {
				t.Errorf("offset(): %d of %d keys below %v and %d from %v on, want both halves of [0, %v) to be hit", belowMid, keys, mid, fromMid, mid, tc.wantMax)
			}
		})
	}
}

func TestPeriodicEnqueuerOffsetIsSalted(t *testing.T) {
	const keys = 100
	a := newPeriodicEnqueuer(10*time.Minute, 1)
	b := newPeriodicEnqueuer(10*time.Minute, 1)
	if a.salt == b.salt {
		t.Fatalf("two periodicEnqueuers got the same salt %d", a.salt)
	}
	for i := 0; i < keys; i++ {
		key := fmt.Sprintf("namespace/service-%d", i)
		if a.offset(key) != b.offset(key) {
			return
		}
	}
	t.Errorf("offset() returned the same values for %d keys in two differently salted periodicEnqueuers, want them to differ", keys)
}

func TestPeriodicEnqueuerEnqueue(t *testing.T) {
	const period = 10 * time.Minute
	svc := &v1.Service{ObjectMeta: metav1.ObjectMeta{Namespace: "namespace", Name: "service"}}
	key := utils.ServiceKeyFunc(svc.Namespace, svc.Name)

	testCases := []struct {
		desc         string
		spreadFactor float64
		wantDelayed  bool
	}{
		{
			desc:         "spreading disabled enqueues immediately",
			spreadFactor: 0,
			wantDelayed:  false,
		},
		{
			desc:         "spreading enabled enqueues after the offset",
			spreadFactor: 1,
			wantDelayed:  true,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			p := newPeriodicEnqueuer(period, tc.spreadFactor)
			// Make the offset deterministic and non-zero for the delayed case:
			// with this salt, the test key hashes to a positive offset.
			p.salt = 42
			if got := p.offset(key); tc.wantDelayed && got <= 0 {
				t.Fatalf("offset(%q) = %v with salt %d, want > 0 for this test case", key, got, p.salt)
			}
			var scheduled []func()
			var scheduledDelay time.Duration
			p.afterFunc = func(d time.Duration, f func()) *time.Timer {
				scheduledDelay = d
				scheduled = append(scheduled, f)
				return nil
			}
			queue := utils.NewPeriodicTaskQueue("", "test", func(string) error { return nil }, klog.TODO())
			// Zero value, so that an update is distinguishable from the initial timestamp.
			var tracker utils.TimeTracker
			before := time.Now()

			p.enqueue(queue, &tracker, svc, klog.TODO())

			if !tc.wantDelayed {
				if len(scheduled) != 0 {
					t.Fatalf("enqueue() scheduled %d delayed enqueues, want 0", len(scheduled))
				}
				if queue.Len() != 1 {
					t.Fatalf("queue.Len() = %d after enqueue(), want 1", queue.Len())
				}
				if tracker.Get().Before(before) {
					t.Errorf("enqueue tracker was not updated by enqueue()")
				}
				return
			}

			if len(scheduled) != 1 {
				t.Fatalf("enqueue() scheduled %d delayed enqueues, want 1", len(scheduled))
			}
			if want := p.offset(key); scheduledDelay != want {
				t.Errorf("enqueue() scheduled the enqueue after %v, want the key's offset %v", scheduledDelay, want)
			}
			if queue.Len() != 0 {
				t.Fatalf("queue.Len() = %d right after enqueue(), want 0 until the delay elapses", queue.Len())
			}
			if !tracker.Get().IsZero() {
				t.Errorf("enqueue tracker was updated when the enqueue was scheduled, want it updated only when the service is enqueued")
			}

			scheduled[0]()

			if queue.Len() != 1 {
				t.Fatalf("queue.Len() = %d after the delay elapsed, want 1", queue.Len())
			}
			if tracker.Get().Before(before) {
				t.Errorf("enqueue tracker was not updated when the service was enqueued")
			}
		})
	}
}

func TestPeriodicEnqueuerCancelAndStop(t *testing.T) {
	const period = 10 * time.Minute
	svc := &v1.Service{ObjectMeta: metav1.ObjectMeta{Namespace: "namespace", Name: "service"}}
	key := utils.ServiceKeyFunc(svc.Namespace, svc.Name)

	newTestEnqueuer := func(t *testing.T) (*periodicEnqueuer, *[]func()) {
		t.Helper()
		p := newPeriodicEnqueuer(period, 1)
		p.salt = 42
		if got := p.offset(key); got <= 0 {
			t.Fatalf("offset(%q) = %v with salt %d, want > 0", key, got, p.salt)
		}
		var scheduled []func()
		p.afterFunc = func(_ time.Duration, f func()) *time.Timer {
			scheduled = append(scheduled, f)
			return time.NewTimer(time.Hour)
		}
		return p, &scheduled
	}

	t.Run("cancel prevents pending timer from enqueueing", func(t *testing.T) {
		p, scheduled := newTestEnqueuer(t)
		queue := utils.NewPeriodicTaskQueue("", "test", func(string) error { return nil }, klog.TODO())
		var tracker utils.TimeTracker

		p.enqueue(queue, &tracker, svc, klog.TODO())
		p.cancel(key)
		(*scheduled)[0]()

		if queue.Len() != 0 {
			t.Errorf("queue.Len() = %d after cancelled timer fired, want 0", queue.Len())
		}
		if !tracker.Get().IsZero() {
			t.Errorf("enqueue tracker was updated by a cancelled timer")
		}
	})

	t.Run("cancelObj handles Service and DeletedFinalStateUnknown tombstone", func(t *testing.T) {
		for _, obj := range []interface{}{
			svc,
			cache.DeletedFinalStateUnknown{Key: key, Obj: svc},
		} {
			p, scheduled := newTestEnqueuer(t)
			queue := utils.NewPeriodicTaskQueue("", "test", func(string) error { return nil }, klog.TODO())
			var tracker utils.TimeTracker

			p.enqueue(queue, &tracker, svc, klog.TODO())
			p.cancelObj(obj)
			(*scheduled)[0]()

			if queue.Len() != 0 {
				t.Errorf("queue.Len() = %d after cancelObj(%T), want 0", queue.Len(), obj)
			}
		}
	})

	t.Run("re-enqueue replaces previous pending timer", func(t *testing.T) {
		p, scheduled := newTestEnqueuer(t)
		queue := utils.NewPeriodicTaskQueue("", "test", func(string) error { return nil }, klog.TODO())
		var tracker utils.TimeTracker

		p.enqueue(queue, &tracker, svc, klog.TODO())
		p.enqueue(queue, &tracker, svc, klog.TODO())
		if len(*scheduled) != 2 {
			t.Fatalf("scheduled %d timers, want 2", len(*scheduled))
		}

		// Stale first timer must be a no-op.
		(*scheduled)[0]()
		if queue.Len() != 0 {
			t.Fatalf("queue.Len() = %d after replaced first timer fired, want 0", queue.Len())
		}

		// Latest second timer must enqueue.
		(*scheduled)[1]()
		if queue.Len() != 1 {
			t.Errorf("queue.Len() = %d after second timer fired, want 1", queue.Len())
		}
	})

	t.Run("stop cancels pending timers and ignores subsequent enqueues", func(t *testing.T) {
		p, scheduled := newTestEnqueuer(t)
		queue := utils.NewPeriodicTaskQueue("", "test", func(string) error { return nil }, klog.TODO())
		var tracker utils.TimeTracker

		p.enqueue(queue, &tracker, svc, klog.TODO())
		p.stop()
		(*scheduled)[0]()

		if queue.Len() != 0 {
			t.Errorf("queue.Len() = %d after stop(), want 0", queue.Len())
		}

		p.enqueue(queue, &tracker, svc, klog.TODO())
		if len(*scheduled) != 1 {
			t.Errorf("enqueue() after stop() scheduled a new timer, want no-op")
		}
	})

	t.Run("skips delayed enqueue when service is in rate-limited retry", func(t *testing.T) {
		p, scheduled := newTestEnqueuer(t)
		queue := utils.NewPeriodicTaskQueueWithMultipleWorkers("", "test", 1, func(string) error {
			return fmt.Errorf("transient GCE error")
		}, klog.TODO())
		var tracker utils.TimeTracker

		p.enqueue(queue, &tracker, svc, klog.TODO())

		// Simulate the service failing a sync during the delay window and
		// entering exponential backoff (NumRequeues > 0).
		queue.Run()
		defer queue.Shutdown()
		queue.Enqueue(svc)
		deadline := time.Now().Add(2 * time.Second)
		for queue.NumRequeues(svc) == 0 && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		if queue.NumRequeues(svc) == 0 {
			t.Fatalf("expected service to have NumRequeues > 0 after failing sync")
		}

		(*scheduled)[0]()
		if !tracker.Get().IsZero() {
			t.Errorf("delayed periodic timer enqueued service while in rate-limited retry")
		}
	})
}

func TestFinalizerWasRemovedUnexpectedly(t *testing.T) {
	testCases := []struct {
		desc           string
		oldService     *v1.Service
		newService     *v1.Service
		finalizerName  string
		expectedResult bool
	}{
		{
			desc:           "Clean service",
			oldService:     &v1.Service{},
			newService:     &v1.Service{},
			finalizerName:  "random",
			expectedResult: false,
		},
		{
			desc: "Empty finalizers",
			oldService: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Finalizers: []string{},
				},
			},
			newService: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Finalizers: []string{},
				},
			},
			finalizerName:  "random",
			expectedResult: false,
		},
		{
			desc: "Changed L4 Finalizer",
			oldService: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Finalizers: []string{common.LegacyILBFinalizer, "random"},
				},
			},
			newService: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Finalizers: []string{"random", "gke.networking.io/l4-ilb-v1-fake"},
				},
			},
			finalizerName:  common.LegacyILBFinalizer,
			expectedResult: true,
		},
		{
			desc: "Removed L4 Finalizer",
			oldService: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Finalizers: []string{common.LegacyILBFinalizer, "random"},
				},
			},
			newService: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Finalizers: []string{"random"},
				},
			},
			finalizerName:  common.LegacyILBFinalizer,
			expectedResult: true,
		},
		{
			desc: "Added L4 ILB v2 Finalizer",
			oldService: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Finalizers: []string{"random"},
				},
			},
			newService: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Finalizers: []string{"random", common.ILBFinalizerV2},
				},
			},
			finalizerName:  common.ILBFinalizerV2,
			expectedResult: false,
		},
		{
			desc: "Service with NetLB Finalizer hasn't changed",
			oldService: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Finalizers: []string{common.NetLBFinalizerV2, "random"},
				},
			},
			newService: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Finalizers: []string{"random", common.NetLBFinalizerV2},
				},
			},
			finalizerName:  common.NetLBFinalizerV2,
			expectedResult: false,
		},
		{
			desc: "Finalizer was removed but given name is wrong",
			oldService: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Finalizers: []string{common.NetLBFinalizerV2, "random"},
				},
			},
			newService: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Finalizers: []string{"random"},
				},
			},
			finalizerName:  common.ILBFinalizerV2,
			expectedResult: false,
		},
		{
			desc: "Finalizer was removed and service to be deleted",
			oldService: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Finalizers: []string{common.NetLBFinalizerV2, "random"},
				},
			},
			newService: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					DeletionTimestamp: &metav1.Time{Time: time.Date(2024, 12, 30, 0, 0, 0, 0, time.Local)},
					Finalizers:        []string{common.ILBFinalizerV2},
				},
			},
			finalizerName:  common.ILBFinalizerV2,
			expectedResult: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			gotResult := finalizerWasRemovedUnexpectedly(tc.oldService, tc.newService, tc.finalizerName)
			if gotResult != tc.expectedResult {
				t.Errorf("finalizerWasRemoved(oldSvc=%v, newSvc=%v, finalizer=%s) returned %v, but expected %v", tc.oldService, tc.newService, tc.finalizerName, gotResult, tc.expectedResult)
			}
		})
	}
}

func TestConditionsEqual(t *testing.T) {
	testCases := []struct {
		desc           string
		left           []metav1.Condition
		right          []metav1.Condition
		expectedResult bool
	}{
		{
			desc:           "Both empty",
			left:           []metav1.Condition{},
			right:          []metav1.Condition{},
			expectedResult: true,
		},
		{
			desc:           "One nil, one empty",
			left:           nil,
			right:          []metav1.Condition{},
			expectedResult: true,
		},
		{
			desc: "Different lengths",
			left: []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionTrue},
			},
			right:          []metav1.Condition{},
			expectedResult: false,
		},
		{
			desc: "Same conditions, same order",
			left: []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionTrue, Reason: "ReasonA", Message: "MsgA"},
				{Type: "Synced", Status: metav1.ConditionFalse, Reason: "ReasonB", Message: "MsgB"},
			},
			right: []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionTrue, Reason: "ReasonA", Message: "MsgA"},
				{Type: "Synced", Status: metav1.ConditionFalse, Reason: "ReasonB", Message: "MsgB"},
			},
			expectedResult: true,
		},
		{
			desc: "Same conditions, different order",
			left: []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionTrue},
				{Type: "Synced", Status: metav1.ConditionFalse},
			},
			right: []metav1.Condition{
				{Type: "Synced", Status: metav1.ConditionFalse},
				{Type: "Ready", Status: metav1.ConditionTrue},
			},
			expectedResult: true,
		},
		{
			desc: "Missing condition type in right",
			left: []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionTrue},
				{Type: "Synced", Status: metav1.ConditionTrue},
			},
			right: []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionTrue},
				{Type: "Other", Status: metav1.ConditionTrue},
			},
			expectedResult: false,
		},
		{
			desc: "Different Status",
			left: []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionTrue},
			},
			right: []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionFalse},
			},
			expectedResult: false,
		},
		{
			desc: "Different Reason",
			left: []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionTrue, Reason: "Foo"},
			},
			right: []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionTrue, Reason: "Bar"},
			},
			expectedResult: false,
		},
		{
			desc: "Different Message",
			left: []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionTrue, Message: "Foo"},
			},
			right: []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionTrue, Message: "Bar"},
			},
			expectedResult: false,
		},
		{
			desc: "Extra fields in left ignored (LastTransitionTime is not checked)",
			left: []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionTrue, LastTransitionTime: metav1.Now()},
			},
			right: []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionTrue},
			},
			expectedResult: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			gotResult := conditionsEqual(tc.left, tc.right)
			if gotResult != tc.expectedResult {
				t.Errorf("conditionsEqual() = %v, expected %v", gotResult, tc.expectedResult)
			}
		})
	}
}

func TestMergeConditions(t *testing.T) {
	testCases := []struct {
		desc          string
		existing      []metav1.Condition
		newConditions []metav1.Condition
		expected      []metav1.Condition
	}{
		{
			desc:          "Empty existing and new",
			existing:      nil,
			newConditions: nil,
			expected:      nil,
		},
		{
			desc:          "Add new conditions to empty existing",
			existing:      nil,
			newConditions: []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue}},
			expected:      []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue}},
		},
		{
			desc:          "Replace existing condition",
			existing:      []metav1.Condition{{Type: "Ready", Status: metav1.ConditionFalse, Reason: "Old"}},
			newConditions: []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue, Reason: "New"}},
			expected:      []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue, Reason: "New"}},
		},
		{
			desc:          "Add new condition to existing",
			existing:      []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue}},
			newConditions: []metav1.Condition{{Type: "Synced", Status: metav1.ConditionTrue}},
			expected:      []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue}, {Type: "Synced", Status: metav1.ConditionTrue}},
		},
		{
			desc: "Mixed update and add",
			existing: []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionFalse},
				{Type: "Old", Status: metav1.ConditionTrue},
			},
			newConditions: []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionTrue},
				{Type: "New", Status: metav1.ConditionTrue},
			},
			expected: []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionTrue},
				{Type: "Old", Status: metav1.ConditionTrue},
				{Type: "New", Status: metav1.ConditionTrue},
			},
		},
		{
			desc:          "Case sensitive types",
			existing:      []metav1.Condition{{Type: "ready", Status: metav1.ConditionFalse}},
			newConditions: []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue}},
			expected:      []metav1.Condition{{Type: "ready", Status: metav1.ConditionFalse}, {Type: "Ready", Status: metav1.ConditionTrue}},
		},
		{
			desc:          "Duplicate types in newConditions (last wins)",
			existing:      []metav1.Condition{{Type: "Ready", Status: metav1.ConditionFalse}},
			newConditions: []metav1.Condition{{Type: "Ready", Status: metav1.ConditionFalse}, {Type: "Ready", Status: metav1.ConditionTrue}},
			expected:      []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue}},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			got := mergeConditions(tc.existing, tc.newConditions, nil)
			if !conditionsEqual(got, tc.expected) {
				t.Errorf("mergeConditions() = %v, expected %v", got, tc.expected)
			}
		})
	}
}

func TestMergeConditions_Remove(t *testing.T) {
	testCases := []struct {
		desc               string
		existing           []metav1.Condition
		newConditions      []metav1.Condition
		conditionsToRemove []string
		expected           []metav1.Condition
	}{
		{
			desc:               "Remove existing condition",
			existing:           []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue}},
			newConditions:      nil,
			conditionsToRemove: []string{"Ready"},
			expected:           nil,
		},
		{
			desc: "Remove one, keep one, add one",
			existing: []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionTrue},
				{Type: "Synced", Status: metav1.ConditionFalse},
			},
			newConditions:      []metav1.Condition{{Type: "Programmed", Status: metav1.ConditionTrue}},
			conditionsToRemove: []string{"Ready"},
			expected: []metav1.Condition{
				{Type: "Synced", Status: metav1.ConditionFalse},
				{Type: "Programmed", Status: metav1.ConditionTrue},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			got := mergeConditions(tc.existing, tc.newConditions, tc.conditionsToRemove)
			if !conditionsEqual(got, tc.expected) {
				t.Errorf("mergeConditions() = %v, expected %v", got, tc.expected)
			}
		})
	}
}

func TestMergeConditions_PreserveLastTransitionTime(t *testing.T) {
	t1 := metav1.NewTime(time.Now().Add(-10 * time.Minute))
	t2 := metav1.NewTime(time.Now())

	testCases := []struct {
		desc          string
		existing      []metav1.Condition
		newConditions []metav1.Condition
		expected      []metav1.Condition
	}{
		{
			desc: "Preserve transition time when status is same",
			existing: []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionTrue, LastTransitionTime: t1},
			},
			newConditions: []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionTrue, LastTransitionTime: t2},
			},
			expected: []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionTrue, LastTransitionTime: t1},
			},
		},
		{
			desc: "Update transition time when status changes",
			existing: []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionFalse, LastTransitionTime: t1},
			},
			newConditions: []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionTrue, LastTransitionTime: t2},
			},
			expected: []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionTrue, LastTransitionTime: t2},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			got := mergeConditions(tc.existing, tc.newConditions, nil)
			if len(got) != len(tc.expected) {
				t.Fatalf("Got %d conditions, expected %d", len(got), len(tc.expected))
			}
			for i, gotCond := range got {
				expectedCond := tc.expected[i]
				if gotCond.Type != expectedCond.Type {
					t.Errorf("Condition %d type mismatch: got %s, expected %s", i, gotCond.Type, expectedCond.Type)
				}
				if gotCond.Status != expectedCond.Status {
					t.Errorf("Condition %d status mismatch: got %v, expected %v", i, gotCond.Status, expectedCond.Status)
				}
				if !gotCond.LastTransitionTime.Equal(&expectedCond.LastTransitionTime) {
					t.Errorf("Condition %d LastTransitionTime mismatch: got %v, expected %v", i, gotCond.LastTransitionTime, expectedCond.LastTransitionTime)
				}
			}
		})
	}
}

func TestMergeConditions_Sorting(t *testing.T) {
	newConditions := []metav1.Condition{
		{Type: "C", Status: metav1.ConditionTrue},
		{Type: "A", Status: metav1.ConditionTrue},
		{Type: "B", Status: metav1.ConditionTrue},
	}
	expectedOrder := []string{"A", "B", "C"}

	got := mergeConditions(nil, newConditions, nil)

	if len(got) != len(expectedOrder) {
		t.Fatalf("Got %d conditions, expected %d", len(got), len(expectedOrder))
	}

	for i, cond := range got {
		if cond.Type != expectedOrder[i] {
			t.Errorf("At index %d: got Type %q, expected %q", i, cond.Type, expectedOrder[i])
		}
	}
}
