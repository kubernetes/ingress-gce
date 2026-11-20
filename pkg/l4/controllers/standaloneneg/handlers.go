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
	"reflect"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	negv1beta1 "k8s.io/ingress-gce/pkg/apis/svcneg/v1beta1"
	"k8s.io/ingress-gce/pkg/l4/annotations"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog/v2"
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

// svcNegEventHandler returns the handler of the ServiceNetworkEndpointGroup
// informer events. The service controlling a GCE_VM_IP
// ServiceNetworkEndpointGroup is enqueued when its NEGs change, so that the
// NEG attachment of the load balancer is validated again.
func (c *Controller) svcNegEventHandler() cache.ResourceEventHandler {
	return cache.ResourceEventHandlerFuncs{
		AddFunc:    c.onSvcNegAdd,
		UpdateFunc: c.onSvcNegUpdate,
		DeleteFunc: c.onSvcNegDelete,
	}
}

func (c *Controller) onSvcNegAdd(obj any) {
	svcNeg, ok := obj.(*negv1beta1.ServiceNetworkEndpointGroup)
	if !ok {
		return
	}
	c.enqueueSvcNegOwner(svcNeg)
}

func (c *Controller) onSvcNegUpdate(old, cur any) {
	oldSvcNeg, okOld := old.(*negv1beta1.ServiceNetworkEndpointGroup)
	curSvcNeg, okCur := cur.(*negv1beta1.ServiceNetworkEndpointGroup)
	if !okOld || !okCur {
		return
	}
	// The NEGs of a ServiceNetworkEndpointGroup being deleted can be draining, so
	// the deletion timestamp is compared as well. The controller owner and the
	// labels determine which service the ServiceNetworkEndpointGroup belongs to,
	// e.g. the NEG controller moves it to a service recreated with the same name.
	if reflect.DeepEqual(oldSvcNeg.Status.NetworkEndpointGroups, curSvcNeg.Status.NetworkEndpointGroups) &&
		oldSvcNeg.DeletionTimestamp.Equal(curSvcNeg.DeletionTimestamp) &&
		reflect.DeepEqual(metav1.GetControllerOf(oldSvcNeg), metav1.GetControllerOf(curSvcNeg)) &&
		reflect.DeepEqual(oldSvcNeg.Labels, curSvcNeg.Labels) {
		return
	}
	c.enqueueSvcNegOwner(curSvcNeg)
}

func (c *Controller) onSvcNegDelete(obj any) {
	svcNeg, ok := obj.(*negv1beta1.ServiceNetworkEndpointGroup)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			c.logger.Error(nil, "Unexpected object type in ServiceNetworkEndpointGroup DeleteFunc", "type", fmt.Sprintf("%T", obj))
			return
		}
		svcNeg, ok = tombstone.Obj.(*negv1beta1.ServiceNetworkEndpointGroup)
		if !ok {
			c.logger.Error(nil, "Unexpected object type in tombstone in ServiceNetworkEndpointGroup DeleteFunc", "type", fmt.Sprintf("%T", tombstone.Obj))
			return
		}
	}
	c.enqueueSvcNegOwner(svcNeg)
}

// enqueueSvcNegOwner enqueues the service controlling the GCE_VM_IP
// ServiceNetworkEndpointGroup, if it uses the Standalone NEG LB.
func (c *Controller) enqueueSvcNegOwner(svcNeg *negv1beta1.ServiceNetworkEndpointGroup) {
	if svcNeg.Labels[negtypes.NegCRServicePortKey] != negtypes.NegCRServicePortVmIp {
		return
	}
	owner := negtypes.ControllingService(svcNeg)
	if owner == nil {
		return
	}
	svcKey := utils.ServiceKeyFunc(svcNeg.Namespace, owner.Name)
	obj, exists, err := c.ctx.ServiceInformer.GetIndexer().GetByKey(svcKey)
	if err != nil {
		c.logger.Error(err, "Failed to get the owner service of ServiceNetworkEndpointGroup", "svcKey", svcKey, "svcNeg", klog.KObj(svcNeg))
		return
	}
	if !exists {
		// Expected when the ServiceNetworkEndpointGroup is deleted after the service.
		c.logger.V(3).Info("Owner service of ServiceNetworkEndpointGroup not found", "svcKey", svcKey, "svcNeg", klog.KObj(svcNeg))
		return
	}
	svc, ok := obj.(*v1.Service)
	if !ok || svc.UID != owner.UID {
		return
	}
	if wantsStandaloneNEGLB(svc) {
		c.enqueue(svc)
	}
}
