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
