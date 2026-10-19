package controllers

import (
	"fmt"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/ingress-gce/pkg/test"
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

func TestPeriodicEnqueuerOffsetIsSeeded(t *testing.T) {
	const keys = 100
	a := newPeriodicEnqueuer(10*time.Minute, 1)
	b := newPeriodicEnqueuer(10*time.Minute, 1)
	for i := 0; i < keys; i++ {
		key := fmt.Sprintf("namespace/service-%d", i)
		if a.offset(key) != b.offset(key) {
			return
		}
	}
	t.Errorf("offset() returned the same values for %d keys in two periodicEnqueuers, want them to differ", keys)
}

// fakeTimers stands in for time.AfterFunc: it records the scheduled callbacks
// instead of running them, so that the test decides when a delay elapses.
type fakeTimers struct {
	delays []time.Duration
	funcs  []func()
}

func (ft *fakeTimers) afterFunc(d time.Duration, f func()) *time.Timer {
	ft.delays = append(ft.delays, d)
	ft.funcs = append(ft.funcs, f)
	return nil
}

func TestPeriodicEnqueuerEnqueue(t *testing.T) {
	const (
		period = 10 * time.Minute
		key    = "namespace/service"
	)

	t.Run("without spreading", func(t *testing.T) {
		p := newPeriodicEnqueuer(period, 0)
		timers := &fakeTimers{}
		p.afterFunc = timers.afterFunc
		calls := 0

		p.enqueue(key, func() { calls++ })

		if calls != 1 {
			t.Errorf("enqueue(%q, f) called f %d times, want 1", key, calls)
		}
		if got := len(timers.funcs); got != 0 {
			t.Errorf("enqueue(%q, f) scheduled %d delayed calls, want 0", key, got)
		}
	})

	t.Run("with spreading", func(t *testing.T) {
		// The offset of key is random in [0, period), so it is 0, which would
		// fail this test, with a probability of 1 in 6e11.
		p := newPeriodicEnqueuer(period, 1)
		timers := &fakeTimers{}
		p.afterFunc = timers.afterFunc
		calls := 0

		p.enqueue(key, func() { calls++ })

		if calls != 0 {
			t.Errorf("enqueue(%q, f) called f %d times before the delay elapsed, want 0", key, calls)
		}
		if got := len(timers.funcs); got != 1 {
			t.Fatalf("enqueue(%q, f) scheduled %d delayed calls, want 1", key, got)
		}
		if got, want := timers.delays[0], p.offset(key); got != want {
			t.Errorf("enqueue(%q, f) scheduled f after %v, want offset(%q) = %v", key, got, key, want)
		}

		timers.funcs[0]()

		if calls != 1 {
			t.Errorf("f was called %d times after the delay elapsed, want 1", calls)
		}
	})
}

// handlerCapturingInformer records the event handlers registered on it, so that tests can
// call the Service event handlers of the L4 controllers directly.
type handlerCapturingInformer struct {
	cache.SharedIndexInformer
	handlers []cache.ResourceEventHandler
}

func (i *handlerCapturingInformer) AddEventHandler(h cache.ResourceEventHandler) (cache.ResourceEventHandlerRegistration, error) {
	i.handlers = append(i.handlers, h)
	return i.SharedIndexInformer.AddEventHandler(h)
}

// spreadTestController is an L4 controller under test, with its Service event handler, its
// queue (with no workers running), and the delayed enqueues scheduled by its periodicEnqueuer.
type spreadTestController struct {
	handler cache.ResourceEventHandler
	queue   utils.TaskQueue
	timers  *fakeTimers
}

func newSpreadTestController(t *testing.T, informer *handlerCapturingInformer, queue utils.TaskQueue, enqueuer *periodicEnqueuer) spreadTestController {
	t.Helper()
	if len(informer.handlers) != 1 {
		t.Fatalf("controller registered %d Service event handlers, want 1", len(informer.handlers))
	}
	timers := &fakeTimers{}
	enqueuer.afterFunc = timers.afterFunc
	return spreadTestController{handler: informer.handlers[0], queue: queue, timers: timers}
}

func newILBSpreadTestController(t *testing.T, spreadFactor float64) spreadTestController {
	t.Helper()
	ctx, stopCh := newServiceControllerContext(t, newFakeGCE(), false)
	t.Cleanup(func() { close(stopCh) })
	informer := &handlerCapturingInformer{SharedIndexInformer: ctx.ServiceInformer}
	ctx.ServiceInformer = informer
	ctx.L4ResyncSpreadFactor = spreadFactor
	l4c := NewILBController(ctx, stopCh, klog.TODO())
	return newSpreadTestController(t, informer, l4c.svcQueue, l4c.periodicEnqueuer)
}

func newNetLBSpreadTestController(t *testing.T, spreadFactor float64) spreadTestController {
	t.Helper()
	ctx, err := buildContext(test.DefaultTestClusterValues(), false, false)
	if err != nil {
		t.Fatalf("buildContext() err=%v", err)
	}
	stopCh := make(chan struct{})
	t.Cleanup(func() { close(stopCh) })
	informer := &handlerCapturingInformer{SharedIndexInformer: ctx.ServiceInformer}
	ctx.ServiceInformer = informer
	ctx.L4ResyncSpreadFactor = spreadFactor
	lc := NewL4NetLBController(ctx, stopCh, klog.TODO())
	return newSpreadTestController(t, informer, lc.svcQueue, lc.periodicEnqueuer)
}

// TestL4ControllersSpreadPeriodicResyncs verifies, for the ILB and NetLB controllers, that
// with --l4-resync-spread-factor > 0 only informer resyncs of unchanged Services are delayed,
// while real changes are enqueued immediately.
func TestL4ControllersSpreadPeriodicResyncs(t *testing.T) {
	t.Parallel()
	controllers := []struct {
		name          string
		newController func(t *testing.T, spreadFactor float64) spreadTestController
		newService    func(port int) *v1.Service
	}{
		{
			name:          "ILB",
			newController: newILBSpreadTestController,
			newService:    func(port int) *v1.Service { return test.NewL4ILBService(false, port) },
		},
		{
			name:          "NetLB",
			newController: newNetLBSpreadTestController,
			newService:    test.NewL4NetLBRBSService,
		},
	}

	for _, ctrl := range controllers {
		t.Run(ctrl.name, func(t *testing.T) {
			newService := func() *v1.Service {
				svc := ctrl.newService(8080)
				svc.ResourceVersion = "1"
				return svc
			}
			// updatedService returns the Service with a changed port, which requires a load balancer update.
			updatedService := func() *v1.Service {
				updated := ctrl.newService(8081)
				updated.ResourceVersion = "2"
				return updated
			}

			t.Run("resync is enqueued immediately without spreading", func(t *testing.T) {
				t.Parallel()
				c := ctrl.newController(t, 0)
				svc := newService()

				c.handler.OnUpdate(svc, svc)

				if got := c.queue.Len(); got != 1 {
					t.Errorf("queue.Len() = %d after resync, want 1", got)
				}
				if got := len(c.timers.funcs); got != 0 {
					t.Errorf("resync scheduled %d delayed enqueues, want 0", got)
				}
			})

			t.Run("resync is delayed with spreading", func(t *testing.T) {
				t.Parallel()
				c := ctrl.newController(t, 1)
				svc := newService()

				c.handler.OnUpdate(svc, svc)

				if got := c.queue.Len(); got != 0 {
					t.Errorf("queue.Len() = %d right after resync, want 0", got)
				}
				if got := len(c.timers.funcs); got != 1 {
					t.Fatalf("resync scheduled %d delayed enqueues, want 1", got)
				}
				c.timers.funcs[0]()
				if got := c.queue.Len(); got != 1 {
					t.Errorf("queue.Len() = %d after the delay, want 1", got)
				}
			})

			t.Run("real update is enqueued immediately with spreading", func(t *testing.T) {
				t.Parallel()
				c := ctrl.newController(t, 1)

				c.handler.OnUpdate(newService(), updatedService())

				if got := c.queue.Len(); got != 1 {
					t.Errorf("queue.Len() = %d right after real update, want 1", got)
				}
				if got := len(c.timers.funcs); got != 0 {
					t.Errorf("real update scheduled %d delayed enqueues, want 0", got)
				}
			})
		})
	}
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
