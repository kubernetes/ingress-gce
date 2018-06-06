/*
Copyright 2017 The Kubernetes Authors.

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

package neg

import (
	"fmt"
	"reflect"
	"strconv"
	"time"

	"github.com/golang/glog"
	apiv1 "k8s.io/api/core/v1"
	extensions "k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes/scheme"
	unversionedcore "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/context"
)

const (
	// For each service, only retries 15 times to process it.
	// This is a convention in kube-controller-manager.
	maxRetries = 15
)

// Controller is network endpoint group controller.
// It determines whether NEG for a service port is needed, then signals negSyncerManager to sync it.
type Controller struct {
	manager      negSyncerManager
	resyncPeriod time.Duration
	recorder     record.EventRecorder

	ingressSynced  cache.InformerSynced
	serviceSynced  cache.InformerSynced
	endpointSynced cache.InformerSynced
	ingressLister  cache.Indexer
	serviceLister  cache.Indexer

	// serviceQueue takes service key as work item. Service key with format "namespace/name".
	serviceQueue workqueue.RateLimitingInterface
}

// NewController returns a network endpoint group controller.
func NewController(
	cloud networkEndpointGroupCloud,
	ctx *context.ControllerContext,
	zoneGetter zoneGetter,
	namer networkEndpointGroupNamer,
	resyncPeriod time.Duration,
) (*Controller, error) {
	// init event recorder
	// TODO: move event recorder initializer to main. Reuse it among controllers.
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(glog.Infof)
	eventBroadcaster.StartRecordingToSink(&unversionedcore.EventSinkImpl{
		Interface: ctx.KubeClient.Core().Events(""),
	})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme,
		apiv1.EventSource{Component: "neg-controller"})

	manager := newSyncerManager(namer,
		recorder,
		cloud,
		zoneGetter,
		ctx.ServiceInformer.GetIndexer(),
		ctx.EndpointInformer.GetIndexer())

	negController := &Controller{
		manager:        manager,
		resyncPeriod:   resyncPeriod,
		recorder:       recorder,
		ingressSynced:  ctx.IngressInformer.HasSynced,
		serviceSynced:  ctx.ServiceInformer.HasSynced,
		endpointSynced: ctx.EndpointInformer.HasSynced,
		ingressLister:  ctx.IngressInformer.GetIndexer(),
		serviceLister:  ctx.ServiceInformer.GetIndexer(),
		serviceQueue:   workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
	}

	ctx.ServiceInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    negController.enqueueService,
		DeleteFunc: negController.enqueueService,
		UpdateFunc: func(old, cur interface{}) {
			negController.enqueueService(cur)
		},
	})

	ctx.IngressInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    negController.enqueueIngressServices,
		DeleteFunc: negController.enqueueIngressServices,
		UpdateFunc: func(old, cur interface{}) {
			keys := gatherIngressServiceKeys(old)
			keys = keys.Union(gatherIngressServiceKeys(cur))
			for _, key := range keys.List() {
				negController.enqueueService(cache.ExplicitKey(key))
			}
		},
	})

	ctx.EndpointInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    negController.processEndpoint,
		DeleteFunc: negController.processEndpoint,
		UpdateFunc: func(old, cur interface{}) {
			negController.processEndpoint(cur)
		},
	})
	return negController, nil
}

func (c *Controller) Run(stopCh <-chan struct{}) {
	wait.PollUntil(5*time.Second, func() (bool, error) {
		glog.V(2).Infof("Waiting for initial sync")
		return c.synced(), nil
	}, stopCh)

	glog.V(2).Infof("Starting network endpoint group controller")
	defer func() {
		glog.V(2).Infof("Shutting down network endpoint group controller")
		c.stop()
	}()

	go wait.Until(c.serviceWorker, time.Second, stopCh)
	go wait.Until(c.gc, c.resyncPeriod, stopCh)

	<-stopCh
}

func (c *Controller) stop() {
	glog.V(2).Infof("Shutting down network endpoint group controller")
	c.serviceQueue.ShutDown()
	c.manager.ShutDown()
}

// processEndpoint finds the related syncers and signal it to sync
func (c *Controller) processEndpoint(obj interface{}) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		glog.Errorf("Failed to generate endpoint key: %v", err)
		return
	}
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return
	}
	c.manager.Sync(namespace, name)
}

func (c *Controller) serviceWorker() {
	for {
		func() {
			key, quit := c.serviceQueue.Get()
			if quit {
				return
			}
			defer c.serviceQueue.Done(key)
			err := c.processService(key.(string))
			c.handleErr(err, key)
		}()
	}
}

// processService takes a service and determines whether it needs NEGs or not.
func (c *Controller) processService(key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}

	svc, exists, err := c.serviceLister.GetByKey(key)
	if err != nil {
		return err
	}
	if !exists {
		c.manager.StopSyncer(namespace, name)
		return nil
	}

	var service *apiv1.Service
	var enabled bool
	if exists {
		service = svc.(*apiv1.Service)
		enabled = annotations.FromService(service).NEGEnabled()
	}

	if !enabled {
		c.manager.StopSyncer(namespace, name)
		return nil
	}

	glog.V(2).Infof("Syncing service %q", key)
	// Only service ports referenced by ingress are synced for NEG
	ings := getIngressServicesFromStore(c.ingressLister, service)
	return c.manager.EnsureSyncers(namespace, name, gatherSerivceTargetPortUsedByIngress(ings, service))
}

func (c *Controller) handleErr(err error, key interface{}) {
	if err == nil {
		c.serviceQueue.Forget(key)
		return
	}

	glog.Errorf("Error processing service %q: %v", key, err)
	if c.serviceQueue.NumRequeues(key) < maxRetries {
		c.serviceQueue.AddRateLimited(key)
		return
	}

	defer c.serviceQueue.Forget(key)
	service, exists, err := c.serviceLister.GetByKey(key.(string))
	if err != nil {
		glog.Warning("Failed to retrieve service %q from store: %v", key.(string), err)
		return
	}
	if exists {
		c.recorder.Eventf(service.(*apiv1.Service), apiv1.EventTypeWarning, "ProcessServiceFailed", "Service %q dropped from queue (requeued %v times)", key, c.serviceQueue.NumRequeues(key))
	}
}

func (c *Controller) enqueueService(obj interface{}) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		glog.Errorf("Failed to generate service key: %v", err)
		return
	}
	c.serviceQueue.Add(key)
}

func (c *Controller) enqueueIngressServices(obj interface{}) {
	keys := gatherIngressServiceKeys(obj)
	for key := range keys {
		c.enqueueService(cache.ExplicitKey(key))
	}
}

func (c *Controller) gc() {
	if err := c.manager.GC(); err != nil {
		glog.Errorf("NEG controller garbage collection failed: %v", err)
	}
}

func (c *Controller) synced() bool {
	return c.endpointSynced() &&
		c.serviceSynced() &&
		c.ingressSynced()
}

// gatherSerivceTargetPortUsedByIngress returns all target ports of the service that are referenced by ingresses
func gatherSerivceTargetPortUsedByIngress(ings []extensions.Ingress, svc *apiv1.Service) sets.String {
	servicePorts := sets.NewString()
	targetPorts := sets.NewString()
	for _, ing := range ings {
		if ing.Spec.Backend != nil && ing.Spec.Backend.ServiceName == svc.Name {
			servicePorts.Insert(ing.Spec.Backend.ServicePort.String())
		}
	}
	for _, ing := range ings {
		for _, rule := range ing.Spec.Rules {
			for _, path := range rule.IngressRuleValue.HTTP.Paths {
				if path.Backend.ServiceName == svc.Name {
					servicePorts.Insert(path.Backend.ServicePort.String())
				}
			}
		}
	}

	for _, svcPort := range svc.Spec.Ports {
		found := false
		for _, refSvcPort := range servicePorts.List() {
			port, err := strconv.Atoi(refSvcPort)
			if err != nil {
				// port name matches
				if refSvcPort == svcPort.Name {
					found = true
					break
				}
			} else {
				// service port matches
				if port == int(svcPort.Port) {
					found = true
					break
				}
			}
		}
		if found {
			targetPorts.Insert(svcPort.TargetPort.String())
		}
	}

	return targetPorts
}

// gatherIngressServiceKeys returns all service key (formatted as namespace/name) referenced in the ingress
func gatherIngressServiceKeys(obj interface{}) sets.String {
	set := sets.NewString()
	ing, ok := obj.(*extensions.Ingress)
	if !ok {
		glog.Errorf("Expecting ingress type but got: %T", reflect.TypeOf(ing))
		return set
	}

	if ing.Spec.Backend != nil {
		set.Insert(serviceKeyFunc(ing.Namespace, ing.Spec.Backend.ServiceName))
	}

	for _, rule := range ing.Spec.Rules {
		for _, path := range rule.IngressRuleValue.HTTP.Paths {
			set.Insert(serviceKeyFunc(ing.Namespace, path.Backend.ServiceName))
		}
	}
	return set
}

func serviceKeyFunc(namespace, name string) string {
	return fmt.Sprintf("%s/%s", namespace, name)
}

func getIngressServicesFromStore(store cache.Store, svc *apiv1.Service) (ings []extensions.Ingress) {
IngressLoop:
	for _, m := range store.List() {
		ing := *m.(*extensions.Ingress)
		if ing.Namespace != svc.Namespace {
			continue
		}

		// Check service of default backend
		if ing.Spec.Backend != nil && ing.Spec.Backend.ServiceName == svc.Name {
			ings = append(ings, ing)
			continue
		}

		// Check the target service for each path rule
		for _, rule := range ing.Spec.Rules {
			if rule.IngressRuleValue.HTTP == nil {
				continue
			}
			for _, p := range rule.IngressRuleValue.HTTP.Paths {
				if p.Backend.ServiceName == svc.Name {
					ings = append(ings, ing)
					// Skip the rest of the rules to avoid duplicate ingresses in list.
					// Check next ingress
					continue IngressLoop
				}
			}
		}
	}
	return
}
