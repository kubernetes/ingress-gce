/*
Copyright 2026 The Kubernetes Authors.

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

package controllers

import (
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/ingress-gce/pkg/test"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog/v2"
)

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
	handler   cache.ResourceEventHandler
	queue     utils.TaskQueue
	enqueuer  *periodicEnqueuer
	scheduled *[]func()
}

func newSpreadTestController(t *testing.T, informer *handlerCapturingInformer, queue utils.TaskQueue, enqueuer *periodicEnqueuer) spreadTestController {
	t.Helper()
	if len(informer.handlers) != 1 {
		t.Fatalf("controller registered %d Service event handlers, want 1", len(informer.handlers))
	}
	var scheduled []func()
	enqueuer.afterFunc = func(_ time.Duration, f func()) *time.Timer {
		scheduled = append(scheduled, f)
		return time.NewTimer(time.Hour)
	}
	return spreadTestController{handler: informer.handlers[0], queue: queue, enqueuer: enqueuer, scheduled: &scheduled}
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
// while real changes are enqueued immediately and cancel the delayed resync, and that
// Service deletion cancels the delayed resync.
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
		ctrl := ctrl
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

		t.Run(ctrl.name+"/resync is enqueued immediately without spreading", func(t *testing.T) {
			t.Parallel()
			c := ctrl.newController(t, 0)
			svc := newService()

			c.handler.OnUpdate(svc, svc)

			if got := c.queue.Len(); got != 1 {
				t.Errorf("queue.Len() = %d after resync, want 1", got)
			}
			if got := len(*c.scheduled); got != 0 {
				t.Errorf("scheduled %d delayed enqueues, want 0", got)
			}
		})

		t.Run(ctrl.name+"/resync is delayed with spreading", func(t *testing.T) {
			t.Parallel()
			c := ctrl.newController(t, 1)
			svc := newService()

			c.handler.OnUpdate(svc, svc)

			if got := c.queue.Len(); got != 0 {
				t.Errorf("queue.Len() = %d right after resync, want 0", got)
			}
			if got := len(*c.scheduled); got != 1 {
				t.Fatalf("scheduled %d delayed enqueues, want 1", got)
			}
			(*c.scheduled)[0]()
			if got := c.queue.Len(); got != 1 {
				t.Errorf("queue.Len() = %d after the delay, want 1", got)
			}
		})

		t.Run(ctrl.name+"/real update is enqueued immediately and cancels the delayed resync", func(t *testing.T) {
			t.Parallel()
			c := ctrl.newController(t, 1)
			svc := newService()
			c.handler.OnUpdate(svc, svc)
			if got := len(*c.scheduled); got != 1 {
				t.Fatalf("scheduled %d delayed enqueues, want 1", got)
			}

			c.handler.OnUpdate(svc, updatedService())

			if got := c.queue.Len(); got != 1 {
				t.Errorf("queue.Len() = %d right after real update, want 1", got)
			}
			if got := len(*c.scheduled); got != 1 {
				t.Errorf("scheduled %d delayed enqueues after real update, want 1 (only the resync)", got)
			}
			if got := len(c.enqueuer.timers); got != 0 {
				t.Errorf("%d delayed enqueues pending after real update, want 0", got)
			}
		})

		for _, deleted := range []struct {
			name string
			obj  func(svc *v1.Service) interface{}
		}{
			{name: "Service", obj: func(svc *v1.Service) interface{} { return svc }},
			{name: "tombstone", obj: func(svc *v1.Service) interface{} {
				return cache.DeletedFinalStateUnknown{Key: utils.ServiceKeyFunc(svc.Namespace, svc.Name), Obj: svc}
			}},
		} {
			t.Run(ctrl.name+"/delete of "+deleted.name+" cancels the delayed resync", func(t *testing.T) {
				t.Parallel()
				c := ctrl.newController(t, 1)
				svc := newService()
				c.handler.OnUpdate(svc, svc)
				if got := len(*c.scheduled); got != 1 {
					t.Fatalf("scheduled %d delayed enqueues, want 1", got)
				}

				c.handler.OnDelete(deleted.obj(svc))
				(*c.scheduled)[0]()

				if got := c.queue.Len(); got != 0 {
					t.Errorf("queue.Len() = %d after delete and the delay, want 0", got)
				}
			})
		}
	}
}
