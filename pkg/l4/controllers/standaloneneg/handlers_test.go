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
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	negv1beta1 "k8s.io/ingress-gce/pkg/apis/svcneg/v1beta1"
	"k8s.io/ingress-gce/pkg/l4/annotations"
	l4metrics "k8s.io/ingress-gce/pkg/l4/metrics"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
)

func TestStandaloneNEGLBControllerEventHandlers_Add(t *testing.T) {
	kubeClient, _, lc, stopCh := setupControllerContext(t)
	defer close(stopCh)

	// Start the informer
	go lc.ctx.ServiceInformer.Run(stopCh)
	if !cache.WaitForCacheSync(stopCh, lc.ctx.ServiceInformer.HasSynced) {
		t.Fatalf("Failed to sync cache")
	}

	// Start the queue workers
	go lc.svcQueue.Run()
	defer lc.svcQueue.Shutdown()

	lbClass := annotations.StandalonePassthroughNegLoadBalancerClass
	svc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc1",
			Namespace: "default",
		},
		Spec: v1.ServiceSpec{
			Type:              v1.ServiceTypeLoadBalancer,
			LoadBalancerClass: &lbClass,
		},
	}
	svcKey := svc.Namespace + "/" + svc.Name

	// Act
	_, err := kubeClient.CoreV1().Services(svc.Namespace).Create(context.TODO(), svc, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	// Assert - Wait for metrics to be updated (should be UserError because no annotation)
	err = wait.PollImmediate(100*time.Millisecond, 5*time.Second, func() (bool, error) {
		state, ok := lc.ctx.L4Metrics.StandaloneNEGServiceState(svcKey)
		return ok && state.Status == l4metrics.StatusUserError, nil
	})
	if err != nil {
		t.Errorf("Failed to verify AddFunc via metrics: %v", err)
	}
}

func TestStandaloneNEGLBControllerEventHandlers_Update(t *testing.T) {
	kubeClient, _, lc, stopCh := setupControllerContext(t)
	defer close(stopCh)

	lbClass := annotations.StandalonePassthroughNegLoadBalancerClass
	svc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc1",
			Namespace: "default",
		},
		Spec: v1.ServiceSpec{
			Type:              v1.ServiceTypeLoadBalancer,
			LoadBalancerClass: &lbClass,
		},
	}
	svcKey := svc.Namespace + "/" + svc.Name

	// Setup initial state (service exists and is enqueued/processed once)
	_, err := kubeClient.CoreV1().Services(svc.Namespace).Create(context.TODO(), svc, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	// Start the informer
	go lc.ctx.ServiceInformer.Run(stopCh)
	if !cache.WaitForCacheSync(stopCh, lc.ctx.ServiceInformer.HasSynced) {
		t.Fatalf("Failed to sync cache")
	}

	// Start the queue workers
	go lc.svcQueue.Run()
	defer lc.svcQueue.Shutdown()

	// Wait for initial metrics to be created
	err = wait.PollImmediate(100*time.Millisecond, 5*time.Second, func() (bool, error) {
		_, ok := lc.ctx.L4Metrics.StandaloneNEGServiceState(svcKey)
		return ok, nil
	})
	if err != nil {
		t.Fatalf("Failed to establish initial metrics state: %v", err)
	}

	// Act - Modify to not match shouldProcess (remove load balancer class)
	currentSvc, err := kubeClient.CoreV1().Services(svc.Namespace).Get(context.TODO(), svc.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get service: %v", err)
	}
	currentSvc.Spec.LoadBalancerClass = nil
	_, err = kubeClient.CoreV1().Services(svc.Namespace).Update(context.TODO(), currentSvc, metav1.UpdateOptions{})
	if err != nil {
		t.Fatalf("Failed to update service: %v", err)
	}

	// Assert - Wait for metrics to be deleted
	err = wait.PollImmediate(100*time.Millisecond, 5*time.Second, func() (bool, error) {
		_, ok := lc.ctx.L4Metrics.StandaloneNEGServiceState(svcKey)
		return !ok, nil
	})
	if err != nil {
		t.Errorf("Failed to verify UpdateFunc (non-matching) via metrics: %v", err)
	}
}

func TestStandaloneNEGLBControllerEventHandlers_Delete(t *testing.T) {
	lbClass := annotations.StandalonePassthroughNegLoadBalancerClass

	kubeClient, _, lc, stopCh := setupControllerContext(t)
	defer close(stopCh)

	svc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc1",
			Namespace: "default",
		},
		Spec: v1.ServiceSpec{
			Type:              v1.ServiceTypeLoadBalancer,
			LoadBalancerClass: &lbClass,
		},
	}
	svcKey := svc.Namespace + "/" + svc.Name

	// Setup initial state
	_, err := kubeClient.CoreV1().Services(svc.Namespace).Create(context.TODO(), svc, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	// Start the informer
	go lc.ctx.ServiceInformer.Run(stopCh)
	if !cache.WaitForCacheSync(stopCh, lc.ctx.ServiceInformer.HasSynced) {
		t.Fatalf("Failed to sync cache")
	}

	// Start the queue workers
	go lc.svcQueue.Run()
	defer lc.svcQueue.Shutdown()

	// Wait for initial metrics to be created
	err = wait.PollImmediate(100*time.Millisecond, 5*time.Second, func() (bool, error) {
		_, ok := lc.ctx.L4Metrics.StandaloneNEGServiceState(svcKey)
		return ok, nil
	})
	if err != nil {
		t.Fatalf("Failed to establish initial metrics state: %v", err)
	}

	// Act - Delete service
	err = kubeClient.CoreV1().Services(svc.Namespace).Delete(context.TODO(), svc.Name, metav1.DeleteOptions{})
	if err != nil {
		t.Fatalf("Failed to delete service: %v", err)
	}

	// Assert - Wait for metrics to be deleted
	err = wait.PollImmediate(100*time.Millisecond, 5*time.Second, func() (bool, error) {
		_, ok := lc.ctx.L4Metrics.StandaloneNEGServiceState(svcKey)
		return !ok, nil
	})
	if err != nil {
		t.Errorf("Failed to verify DeleteFunc via metrics: %v", err)
	}
}

func TestServiceEventHandlers(t *testing.T) {
	lbClass := annotations.StandalonePassthroughNegLoadBalancerClass
	otherClass := "other-class"
	standalone := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "svc", Namespace: "default"},
		Spec:       v1.ServiceSpec{Type: v1.ServiceTypeLoadBalancer, LoadBalancerClass: &lbClass},
	}
	otherLB := standalone.DeepCopy()
	otherLB.Spec.LoadBalancerClass = &otherClass
	clusterIP := standalone.DeepCopy()
	clusterIP.Spec.Type = v1.ServiceTypeClusterIP

	testCases := []struct {
		desc        string
		handle      func(c *Controller)
		wantEnqueue bool
	}{
		{
			desc:        "add standalone NEG LB service",
			handle:      func(c *Controller) { c.onServiceAdd(standalone) },
			wantEnqueue: true,
		},
		{
			desc:   "add service with another load balancer class",
			handle: func(c *Controller) { c.onServiceAdd(otherLB) },
		},
		{
			desc:   "add ClusterIP service",
			handle: func(c *Controller) { c.onServiceAdd(clusterIP) },
		},
		{
			desc:   "add unexpected object",
			handle: func(c *Controller) { c.onServiceAdd("not a service") },
		},
		{
			desc:        "update standalone NEG LB service",
			handle:      func(c *Controller) { c.onServiceUpdate(standalone, standalone) },
			wantEnqueue: true,
		},
		{
			desc:        "update service to standalone NEG LB",
			handle:      func(c *Controller) { c.onServiceUpdate(clusterIP, standalone) },
			wantEnqueue: true,
		},
		{
			desc:        "update service from standalone NEG LB, so that its status is cleared",
			handle:      func(c *Controller) { c.onServiceUpdate(standalone, otherLB) },
			wantEnqueue: true,
		},
		{
			desc:   "update service with another load balancer class",
			handle: func(c *Controller) { c.onServiceUpdate(otherLB, otherLB) },
		},
		{
			desc:        "delete standalone NEG LB service",
			handle:      func(c *Controller) { c.onServiceDelete(standalone) },
			wantEnqueue: true,
		},
		{
			desc: "delete standalone NEG LB service tombstone",
			handle: func(c *Controller) {
				c.onServiceDelete(cache.DeletedFinalStateUnknown{Key: "default/svc", Obj: standalone})
			},
			wantEnqueue: true,
		},
		{
			desc:   "delete service with another load balancer class",
			handle: func(c *Controller) { c.onServiceDelete(otherLB) },
		},
		{
			desc: "delete unexpected tombstone",
			handle: func(c *Controller) {
				c.onServiceDelete(cache.DeletedFinalStateUnknown{Key: "default/svc", Obj: "not a service"})
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			_, _, c, stopCh := setupControllerContext(t)
			defer close(stopCh)

			tc.handle(c)

			wantLen := 0
			if tc.wantEnqueue {
				wantLen = 1
			}
			if got := c.svcQueue.Len(); got != wantLen {
				t.Errorf("queue length = %d, want %d", got, wantLen)
			}
		})
	}
}

// recordingQueue is a TaskQueue recording the keys of the enqueued objects.
type recordingQueue struct {
	keys []string
}

func (q *recordingQueue) Run()      {}
func (q *recordingQueue) Shutdown() {}
func (q *recordingQueue) Len() int  { return len(q.keys) }
func (q *recordingQueue) NumRequeues(obj interface{}) int {
	return 0
}
func (q *recordingQueue) Enqueue(objs ...interface{}) {
	for _, obj := range objs {
		key, err := cache.MetaNamespaceKeyFunc(obj)
		if err != nil {
			key = fmt.Sprintf("invalid object %T", obj)
		}
		q.keys = append(q.keys, key)
	}
}

func TestSvcNegEventHandlers(t *testing.T) {
	lbClass := annotations.StandalonePassthroughNegLoadBalancerClass
	otherClass := "other-class"
	standalone := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "svc", Namespace: "default", UID: "svc-uid"},
		Spec:       v1.ServiceSpec{Type: v1.ServiceTypeLoadBalancer, LoadBalancerClass: &lbClass},
	}
	otherLB := standalone.DeepCopy()
	otherLB.Name = "other-lb"
	otherLB.UID = "other-lb-uid"
	otherLB.Spec.LoadBalancerClass = &otherClass
	// A previous service with the same name as the standalone service.
	previous := standalone.DeepCopy()
	previous.UID = "previous-uid"

	neg := negSelfLink(snegTestProject, snegTestZone, "neg")
	newSvcNeg := func(owner *v1.Service, negRefs ...negv1beta1.NegObjectReference) *negv1beta1.ServiceNetworkEndpointGroup {
		return vmIPSvcNegOf(testSvcNeg("neg", negRefs...), owner)
	}
	svcNeg := newSvcNeg(standalone)
	svcNegWithNEG := newSvcNeg(standalone, negv1beta1.NegObjectReference{SelfLink: neg})
	deleting := deletingSvcNeg(newSvcNeg(standalone, negv1beta1.NegObjectReference{SelfLink: neg}))
	l7SvcNeg := testSvcNeg("l7-neg")
	l7SvcNeg.Labels = map[string]string{negtypes.NegCRServicePortKey: "80"}
	l7SvcNeg = vmIPSvcNegOf(l7SvcNeg, standalone)
	noOwner := newSvcNeg(standalone)
	noOwner.OwnerReferences = nil
	// The ServiceNetworkEndpointGroup of the previous service, moved by the NEG controller to the service
	// recreated with the same name.
	previousOwner := newSvcNeg(previous, negv1beta1.NegObjectReference{SelfLink: neg})
	movedToNewOwner := newSvcNeg(standalone, negv1beta1.NegObjectReference{SelfLink: neg})
	missing := standalone.DeepCopy()
	missing.Name = "missing"
	missing.UID = "missing-uid"

	testCases := []struct {
		desc        string
		handle      func(c *Controller)
		wantEnqueue bool
	}{
		{
			desc:        "add ServiceNetworkEndpointGroup of a standalone NEG LB service",
			handle:      func(c *Controller) { c.onSvcNegAdd(svcNegWithNEG) },
			wantEnqueue: true,
		},
		{
			desc:   "add ServiceNetworkEndpointGroup of a service with another load balancer class",
			handle: func(c *Controller) { c.onSvcNegAdd(newSvcNeg(otherLB)) },
		},
		{
			desc:   "add ServiceNetworkEndpointGroup of a previous service with the same name",
			handle: func(c *Controller) { c.onSvcNegAdd(newSvcNeg(previous)) },
		},
		{
			desc:   "add GCE_VM_IP_PORT ServiceNetworkEndpointGroup",
			handle: func(c *Controller) { c.onSvcNegAdd(l7SvcNeg) },
		},
		{
			desc:   "add ServiceNetworkEndpointGroup without a controller owner",
			handle: func(c *Controller) { c.onSvcNegAdd(noOwner) },
		},
		{
			desc:   "add ServiceNetworkEndpointGroup of a service not in the informer",
			handle: func(c *Controller) { c.onSvcNegAdd(newSvcNeg(missing)) },
		},
		{
			desc:   "add unexpected object",
			handle: func(c *Controller) { c.onSvcNegAdd("not a ServiceNetworkEndpointGroup") },
		},
		{
			desc:        "update NEGs of the ServiceNetworkEndpointGroup",
			handle:      func(c *Controller) { c.onSvcNegUpdate(svcNeg, svcNegWithNEG) },
			wantEnqueue: true,
		},
		{
			desc:        "ServiceNetworkEndpointGroup being deleted, its NEGs become draining",
			handle:      func(c *Controller) { c.onSvcNegUpdate(svcNegWithNEG, deleting) },
			wantEnqueue: true,
		},
		{
			desc:        "controller owner changed to the service recreated with the same name",
			handle:      func(c *Controller) { c.onSvcNegUpdate(previousOwner, movedToNewOwner) },
			wantEnqueue: true,
		},
		{
			desc: "labels changed",
			handle: func(c *Controller) {
				withoutLabels := svcNegWithNEG.DeepCopy()
				withoutLabels.Labels = nil
				c.onSvcNegUpdate(withoutLabels, svcNegWithNEG)
			},
			wantEnqueue: true,
		},
		{
			desc:   "update without changes of the NEGs",
			handle: func(c *Controller) { c.onSvcNegUpdate(svcNegWithNEG, svcNegWithNEG.DeepCopy()) },
		},
		{
			desc:        "delete ServiceNetworkEndpointGroup",
			handle:      func(c *Controller) { c.onSvcNegDelete(deleting) },
			wantEnqueue: true,
		},
		{
			desc: "delete ServiceNetworkEndpointGroup tombstone",
			handle: func(c *Controller) {
				c.onSvcNegDelete(cache.DeletedFinalStateUnknown{Key: "default/neg", Obj: deleting})
			},
			wantEnqueue: true,
		},
		{
			desc: "delete unexpected tombstone",
			handle: func(c *Controller) {
				c.onSvcNegDelete(cache.DeletedFinalStateUnknown{Key: "default/neg", Obj: "not a ServiceNetworkEndpointGroup"})
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			_, _, c, stopCh := setupControllerContext(t)
			defer close(stopCh)
			for _, svc := range []*v1.Service{standalone, otherLB} {
				if err := c.ctx.ServiceInformer.GetIndexer().Add(svc); err != nil {
					t.Fatalf("Failed to add service %s: %v", svc.Name, err)
				}
			}

			queue := &recordingQueue{}
			c.svcQueue = queue

			tc.handle(c)

			var wantKeys []string
			if tc.wantEnqueue {
				wantKeys = []string{"default/svc"}
			}
			if diff := cmp.Diff(wantKeys, queue.keys); diff != "" {
				t.Errorf("enqueued keys mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
