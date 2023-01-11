/*
Copyright 2020 The Kubernetes Authors.

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

package workload

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	discovery "k8s.io/api/discovery/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	informerv1 "k8s.io/client-go/informers/core/v1"
	disinformer "k8s.io/client-go/informers/discovery/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	workloadv1a1 "k8s.io/ingress-gce/pkg/experimental/apis/workload/v1alpha1"
	workloadclient "k8s.io/ingress-gce/pkg/experimental/workload/client/clientset/versioned"
	informerworkload "k8s.io/ingress-gce/pkg/experimental/workload/client/informers/externalversions/workload/v1alpha1"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog/v2"
)

const controllerName = "workload-controller.k8s.io"

// ControllerContext holds the state needed for the execution of the workload controller.
type ControllerContext struct {
	KubeClient     kubernetes.Interface
	WorkloadClient workloadclient.Interface

	WorkloadInformer      cache.SharedIndexInformer
	ServiceInformer       cache.SharedIndexInformer
	EndpointSliceInformer cache.SharedIndexInformer
}

// NewControllerContext returns a new shared set of informers.
func NewControllerContext(
	kubeClient kubernetes.Interface,
	workloadClient workloadclient.Interface,
	namespace string,
	resyncPeriod time.Duration,
) *ControllerContext {
	return &ControllerContext{
		KubeClient:            kubeClient,
		WorkloadClient:        workloadClient,
		WorkloadInformer:      informerworkload.NewWorkloadInformer(workloadClient, namespace, resyncPeriod, utils.NewNamespaceIndexer()),
		ServiceInformer:       informerv1.NewServiceInformer(kubeClient, namespace, resyncPeriod, utils.NewNamespaceIndexer()),
		EndpointSliceInformer: disinformer.NewEndpointSliceInformer(kubeClient, namespace, resyncPeriod, utils.NewNamespaceIndexer()),
	}
}

// Controller represents a workload controller.
type Controller struct {
	hasSynced           func() bool
	queue               workqueue.RateLimitingInterface
	workloadLister      cache.Indexer
	serviceLister       cache.Indexer
	endpointSliceLister cache.Indexer
	kubeClient          kubernetes.Interface
	ctx                 *ControllerContext
}

// HasSynced returns true if all relevant informers has been synced.
func (ctx *ControllerContext) HasSynced() bool {
	return ctx.WorkloadInformer.HasSynced() && ctx.ServiceInformer.HasSynced() && ctx.EndpointSliceInformer.HasSynced()
}

// Start all of the informers.
func (ctx *ControllerContext) Start(stopCh chan struct{}) {
	go ctx.WorkloadInformer.Run(stopCh)
	go ctx.ServiceInformer.Run(stopCh)
	go ctx.EndpointSliceInformer.Run(stopCh)
}

// TODO: Implement ping for controller
// Steps:
//    1. Add a pingQueue in the controller struct.
//    2. Start ping workers when the controller starts.
//       A ping worker pings the workload in pingQueue, and patches the condition.
//    3. Start a goroutine to periodically push all workloads into pingQueue.
//    4. Write code logic for Ready condition based on Ping and Heartbeat.

// NewController returns a workload controller.
func NewController(ctx *ControllerContext) *Controller {
	c := &Controller{
		hasSynced:           ctx.HasSynced,
		queue:               workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		workloadLister:      ctx.WorkloadInformer.GetIndexer(),
		serviceLister:       ctx.ServiceInformer.GetIndexer(),
		endpointSliceLister: ctx.EndpointSliceInformer.GetIndexer(),
		kubeClient:          ctx.KubeClient,
		ctx:                 ctx,
	}

	ctx.WorkloadInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.addWorkload,
		UpdateFunc: c.updateWorkload,
		DeleteFunc: c.deleteWorkload,
	})

	ctx.ServiceInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: c.onServiceUpdate,
		UpdateFunc: func(old, cur interface{}) {
			c.onServiceUpdate(cur)
		},
		// EndpointsController handles DeleteFunc and deletes corresponding Endpoints object
		// But EndpointSliceController only releases resources used by tracker
		// It seems that the deletion is automatically done
	})

	return c
}

func (c *Controller) Run(stopCh <-chan struct{}) {
	wait.PollUntil(5*time.Second, func() (bool, error) {
		klog.V(2).Infof("Waiting for initial sync")
		return c.hasSynced(), nil
	}, stopCh)

	klog.V(2).Infof("Starting workload controller")
	defer func() {
		klog.V(2).Infof("Shutting down workload controller")
		c.stop()
	}()

	go wait.Until(c.serviceWorker, time.Second, stopCh)

	<-stopCh
}

func (c *Controller) stop() {
	klog.V(2).Infof("Shutting down workload controller")
	c.queue.ShutDown()
}

func (c *Controller) serviceWorker() {
	for {
		func() {
			key, quit := c.queue.Get()
			if quit {
				return
			}
			defer c.queue.Done(key)
			err := c.processService(key.(string))
			c.handleErr(err, key)
		}()
	}
}

func (c *Controller) handleErr(err error, key interface{}) {
	if err == nil {
		c.queue.Forget(key)
		return
	}

	klog.Errorf("error processing Service %q: %v", key, err)
	if _, exists, err := c.workloadLister.GetByKey(key.(string)); err != nil {
		klog.Warningf("failed to retrieve Service %q from store: %v", key.(string), err)
	} else if exists {
		klog.Warningf("process Service %q failed: %v", key.(string), err)
	}
	c.queue.AddRateLimited(key)
}

func (c *Controller) onServiceUpdate(obj interface{}) {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("Couldn't get key for object %+v: %v", obj, err))
		return
	}
	c.queue.Add(key)
}

func getWorkloadFromDeleteAction(obj interface{}) *workloadv1a1.Workload {
	if workload, ok := obj.(*workloadv1a1.Workload); ok {
		// Enqueue all the services that the workload used to be a member of.
		// This is the same thing we do when we add a workload.
		return workload
	}
	// If we reached here it means the workload was deleted but its final state is unrecorded.
	tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
	if !ok {
		utilruntime.HandleError(fmt.Errorf("Couldn't get object from tombstone %#v", obj))
		return nil
	}
	workload, ok := tombstone.Obj.(*workloadv1a1.Workload)
	if !ok {
		utilruntime.HandleError(fmt.Errorf("Tombstone contained object that is not a Pod: %#v", obj))
		return nil
	}
	return workload
}

// getWorkloadServiceMemberships returns the services that select the specified workload
func getWorkloadServiceMemberships(serviceLister cache.Indexer, workload *workloadv1a1.Workload) []string {
	ret := make([]string, 0)
	for _, obj := range serviceLister.List() {
		service := obj.(*corev1.Service)
		if service.Namespace != workload.Namespace {
			continue
		}

		selector := labels.Set(service.Spec.Selector).AsSelectorPreValidated()
		if !selector.Matches(labels.Set(workload.Labels)) {
			continue
		}

		key, err := cache.MetaNamespaceKeyFunc(service)
		if err != nil {
			klog.Errorf("failed to get the key of Service %q: %v", service.GetName(), err)
			continue
		}

		ret = append(ret, key)
	}
	return ret
}

func (c *Controller) addWorkload(obj interface{}) {
	workload := obj.(*workloadv1a1.Workload)
	services := getWorkloadServiceMemberships(c.serviceLister, workload)
	for _, key := range services {
		c.queue.Add(key)
	}
}

func (c *Controller) updateWorkload(old, cur interface{}) {
	oldWorkload := old.(*workloadv1a1.Workload)
	newWorkload := cur.(*workloadv1a1.Workload)

	// TODO: Need to skip processing if only Heartbeat/Ping timestamp changes, but the status does not.
	if newWorkload.ResourceVersion == oldWorkload.ResourceVersion {
		return
	}

	// To provide a quick and dirty solution, here I redo all services.
	// TODO: fix this
	// SA: https://github.com/kubernetes/kubernetes/blob/3f579d8971fcce96d6b01b968a46c720f10940b8/pkg/controller/util/endpoint/controller_utils.go#L198
	c.addWorkload(cur)
}

func (c *Controller) deleteWorkload(obj interface{}) {
	workload := getWorkloadFromDeleteAction(obj)
	if workload != nil {
		c.addWorkload(workload)
	}
}

func (c *Controller) processService(key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}

	obj, exists, err := c.serviceLister.GetByKey(key)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}

	service := obj.(*corev1.Service)
	if service.Spec.Selector == nil {
		return nil
	}
	wlSelector := labels.Set(service.Spec.Selector).AsSelectorPreValidated()
	// TODO: Use selector instead of name to select endpointslices
	// esSelector := labels.Set(map[string]string{
	// 	discovery.LabelServiceName: service.Name,
	// 	discovery.LabelManagedBy:   controllerName,
	// })

	// To simplify the complexity, assume there is only one slice managed by this controller.
	// TODO: fix this
	// SA: https://github.com/kubernetes/kubernetes/blob/bdb99c8e0954c6b2d4c40233ded94455a343af73/pkg/controller/endpointslice/reconciler.go#L58:22
	subsets := []discovery.Endpoint{}
	listMatchedWorkload(c.workloadLister, namespace, wlSelector, func(workload *workloadv1a1.Workload) {
		subsets = append(subsets, workloadToEndpoint(workload, service))
	})
	// TODO: Consider Ready conditions of the workloads before putting them into the EndpointSlice.

	// Create or update EndpointSlice
	sliceName := endpointsliceName(name)
	obj, exists, err = c.endpointSliceLister.GetByKey(namespace + "/" + sliceName)
	var curEs *discovery.EndpointSlice
	if exists {
		if len(subsets) == 0 {
			err := c.kubeClient.DiscoveryV1().EndpointSlices(namespace).Delete(context.Background(), sliceName, metav1.DeleteOptions{})
			if err != nil {
				klog.Errorf("error deleting EndpointSlice %q: %+v", sliceName, err)
			} else {
				klog.V(2).Infof("Deleted EndpointSlice %q", sliceName)
			}
			return err
		}
		curEs = obj.(*discovery.EndpointSlice)
		if equalEndpoints(curEs.Endpoints, subsets) {
			klog.V(2).Infof("Unchanged EndpointSlice %q", sliceName)
			return nil
		}
	} else {
		if len(subsets) == 0 {
			return nil
		}
		curEs = newEndpointSlice(sliceName, service)
	}

	newEs := curEs.DeepCopy()
	newEs.Endpoints = subsets

	if !exists {
		_, err = c.kubeClient.DiscoveryV1().EndpointSlices(namespace).Create(context.Background(), newEs, metav1.CreateOptions{})
	} else {
		_, err = c.kubeClient.DiscoveryV1().EndpointSlices(namespace).Update(context.Background(), newEs, metav1.UpdateOptions{})
	}
	if err != nil {
		klog.Errorf("error updating EndpointSlice %q: %+v", sliceName, err)
	} else {
		klog.V(2).Infof("Updated EndpointSlice %q", sliceName)
	}

	return err
}

func workloadToEndpoint(workload *workloadv1a1.Workload, service *corev1.Service) discovery.Endpoint {
	ready := true
	ep := discovery.Endpoint{
		Addresses: []string{},
		Conditions: discovery.EndpointConditions{
			Ready: &ready,
		},
		TargetRef: &corev1.ObjectReference{
			Kind:            "Pod",
			Namespace:       workload.Namespace,
			Name:            workload.Name,
			UID:             workload.UID,
			ResourceVersion: workload.ResourceVersion,
		},
	}
	for _, addr := range workload.Spec.Addresses {
		if addr.AddressType == workloadv1a1.AddressTypeIPv4 {
			ep.Addresses = append(ep.Addresses, addr.Address)
		}
	}
	return ep
}

func equalEndpoints(es1, es2 []discovery.Endpoint) bool {
	// TODO: Find a better way to compare these
	return apiequality.Semantic.DeepEqual(es1, es2)
}

func newEndpointSlice(name string, service *corev1.Service) *discovery.EndpointSlice {
	gvk := schema.GroupVersionKind{Version: "v1", Kind: "Service"}
	ownerRef := metav1.NewControllerRef(service, gvk)
	return &discovery.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				discovery.LabelServiceName: service.Name,
				discovery.LabelManagedBy:   controllerName,
			},
			Name:            name,
			OwnerReferences: []metav1.OwnerReference{*ownerRef},
			Namespace:       service.Namespace,
		},
		Ports:       getEndpointPortsFromServicePorts(service.Spec.Ports),
		AddressType: discovery.AddressTypeIPv4,
		Endpoints:   []discovery.Endpoint{},
	}
}

func getEndpointPortsFromServicePorts(svcPorts []corev1.ServicePort) []discovery.EndpointPort {
	ret := []discovery.EndpointPort{}
	for _, port := range svcPorts {
		ret = append(ret, discovery.EndpointPort{
			Name:        &port.Name,
			Port:        &port.Port,
			Protocol:    &port.Protocol,
			AppProtocol: port.AppProtocol,
		})
	}
	return ret
}

func listMatchedWorkload(
	lister cache.Indexer,
	namespace string,
	selector labels.Selector,
	callback func(*workloadv1a1.Workload),
) {
	wls := lister.List()
	for _, obj := range wls {
		workload := obj.(*workloadv1a1.Workload)
		if workload.Namespace != namespace {
			continue
		}
		if !selector.Matches(labels.Set(workload.Labels)) {
			continue
		}

		callback(workload)
	}
}

func endpointsliceName(svcName string) string {
	// WARNING: Change this name scheme may break endpointslices created by old versions
	return svcName + "-" + controllerName
}
