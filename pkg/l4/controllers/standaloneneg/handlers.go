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

package standaloneneg

import (
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/ingress-gce/pkg/l4/annotations"
)

// serviceEventHandler returns the handler of the service informer events.
// The handlers run in the informer goroutine, they only filter the services
// and enqueue them.
func (c *Controller) serviceEventHandler() cache.ResourceEventHandler {
	return cache.ResourceEventHandlerFuncs{
		AddFunc:    c.onServiceAdd,
		UpdateFunc: c.onServiceUpdate,
		DeleteFunc: c.onServiceDelete,
	}
}

func (c *Controller) onServiceAdd(obj any) {
	svc, ok := obj.(*v1.Service)
	if !ok {
		return
	}
	if wantsStandaloneNEGLB(svc) {
		c.enqueue(svc)
	}
}

func (c *Controller) onServiceUpdate(old, cur any) {
	curSvc, ok := cur.(*v1.Service)
	if !ok {
		return
	}
	// Services that stopped using the Standalone NEG LB are enqueued as well,
	// so that their status is cleared.
	oldSvc, okOld := old.(*v1.Service)
	if okOld && wantsStandaloneNEGLB(oldSvc) || wantsStandaloneNEGLB(curSvc) {
		c.enqueue(curSvc)
	}
}

func (c *Controller) onServiceDelete(obj any) {
	svc, ok := obj.(*v1.Service)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			c.logger.Error(nil, "Unexpected object type in DeleteFunc", "type", fmt.Sprintf("%T", obj))
			return
		}
		svc, ok = tombstone.Obj.(*v1.Service)
		if !ok {
			c.logger.Error(nil, "Unexpected object type in tombstone in DeleteFunc", "type", fmt.Sprintf("%T", tombstone.Obj))
			return
		}
	}
	if wantsStandaloneNEGLB(svc) {
		c.enqueue(svc)
	}
}

// wantsStandaloneNEGLB checks if the service is a LoadBalancer service using
// the Standalone NEG load balancer class.
func wantsStandaloneNEGLB(svc *v1.Service) bool {
	if svc == nil {
		return false
	}
	if svc.Spec.Type != v1.ServiceTypeLoadBalancer {
		return false
	}
	return svc.Spec.LoadBalancerClass != nil && *svc.Spec.LoadBalancerClass == annotations.StandalonePassthroughNegLoadBalancerClass
}

func (c *Controller) enqueue(svc *v1.Service) {
	c.svcQueue.Enqueue(svc)
}
