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
	"strconv"
	"time"

	apiv1 "k8s.io/api/core/v1"
	extensions "k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	unversionedcore "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/context"
	"k8s.io/ingress-gce/pkg/neg/metrics"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog"
)

func init() {
	// register prometheus metrics
	metrics.RegisterMetrics()
}

// Controller is network endpoint group controller.
// It determines whether NEG for a service port is needed, then signals NegSyncerManager to sync it.
type Controller struct {
	manager      negtypes.NegSyncerManager
	resyncPeriod time.Duration
	gcPeriod     time.Duration
	recorder     record.EventRecorder
	namer        negtypes.NetworkEndpointGroupNamer
	zoneGetter   negtypes.ZoneGetter

	ingressSynced  cache.InformerSynced
	serviceSynced  cache.InformerSynced
	endpointSynced cache.InformerSynced
	ingressLister  cache.Indexer
	serviceLister  cache.Indexer
	client         kubernetes.Interface

	// serviceQueue takes service key as work item. Service key with format "namespace/name".
	serviceQueue workqueue.RateLimitingInterface
	// endpointQueue takes endpoint key as work item. Endpoint key with format "namespace/name".
	endpointQueue workqueue.RateLimitingInterface

	// syncTracker tracks the latest time that service and endpoint changes are processed
	syncTracker utils.TimeTracker
}

// NewController returns a network endpoint group controller.
func NewController(
	cloud negtypes.NetworkEndpointGroupCloud,
	ctx *context.ControllerContext,
	zoneGetter negtypes.ZoneGetter,
	namer negtypes.NetworkEndpointGroupNamer,
	resyncPeriod time.Duration,
	gcPeriod time.Duration,
	negSyncerType NegSyncerType) *Controller {
	// init event recorder
	// TODO: move event recorder initializer to main. Reuse it among controllers.
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(klog.Infof)
	eventBroadcaster.StartRecordingToSink(&unversionedcore.EventSinkImpl{
		Interface: ctx.KubeClient.Core().Events(""),
	})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme,
		apiv1.EventSource{Component: "neg-controller"})

	manager := newSyncerManager(namer, recorder, cloud, zoneGetter, ctx.ServiceInformer.GetIndexer(), ctx.EndpointInformer.GetIndexer(), negSyncerType)

	negController := &Controller{
		client:         ctx.KubeClient,
		manager:        manager,
		resyncPeriod:   resyncPeriod,
		gcPeriod:       gcPeriod,
		recorder:       recorder,
		zoneGetter:     zoneGetter,
		namer:          namer,
		ingressSynced:  ctx.IngressInformer.HasSynced,
		serviceSynced:  ctx.ServiceInformer.HasSynced,
		endpointSynced: ctx.EndpointInformer.HasSynced,
		ingressLister:  ctx.IngressInformer.GetIndexer(),
		serviceLister:  ctx.ServiceInformer.GetIndexer(),
		serviceQueue:   workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		endpointQueue:  workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		syncTracker:    utils.NewTimeTracker(),
	}

	ctx.IngressInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			addIng := obj.(*extensions.Ingress)
			if !utils.IsGLBCIngress(addIng) {
				klog.V(4).Infof("Ignoring add for ingress %v based on annotation %v", utils.IngressKeyFunc(addIng), annotations.IngressClassKey)
				return
			}
			negController.enqueueIngressServices(addIng)
		},
		DeleteFunc: func(obj interface{}) {
			delIng := obj.(*extensions.Ingress)
			if !utils.IsGLBCIngress(delIng) {
				klog.V(4).Infof("Ignoring delete for ingress %v based on annotation %v", utils.IngressKeyFunc(delIng), annotations.IngressClassKey)
				return
			}
			negController.enqueueIngressServices(delIng)
		},
		UpdateFunc: func(old, cur interface{}) {
			oldIng := cur.(*extensions.Ingress)
			curIng := cur.(*extensions.Ingress)
			if !utils.IsGLBCIngress(curIng) {
				klog.V(4).Infof("Ignoring update for ingress %v based on annotation %v", utils.IngressKeyFunc(curIng), annotations.IngressClassKey)
				return
			}
			keys := gatherIngressServiceKeys(oldIng)
			keys = keys.Union(gatherIngressServiceKeys(curIng))
			for _, key := range keys.List() {
				negController.enqueueService(cache.ExplicitKey(key))
			}
		},
	})

	ctx.ServiceInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    negController.enqueueService,
		DeleteFunc: negController.enqueueService,
		UpdateFunc: func(old, cur interface{}) {
			negController.enqueueService(cur)
		},
	})

	ctx.EndpointInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    negController.enqueueEndpoint,
		DeleteFunc: negController.enqueueEndpoint,
		UpdateFunc: func(old, cur interface{}) {
			negController.enqueueEndpoint(cur)
		},
	})
	ctx.AddHealthCheck("neg-controller", negController.IsHealthy)
	return negController
}

func (c *Controller) Run(stopCh <-chan struct{}) {
	wait.PollUntil(5*time.Second, func() (bool, error) {
		klog.V(2).Infof("Waiting for initial sync")
		return c.synced(), nil
	}, stopCh)

	klog.V(2).Infof("Starting network endpoint group controller")
	defer func() {
		klog.V(2).Infof("Shutting down network endpoint group controller")
		c.stop()
	}()

	go wait.Until(c.serviceWorker, time.Second, stopCh)
	go wait.Until(c.endpointWorker, time.Second, stopCh)
	go func() {
		// Wait for gcPeriod to run the first GC
		// This is to make sure that all services are fully processed before running GC.
		time.Sleep(c.gcPeriod)
		wait.Until(c.gc, c.gcPeriod, stopCh)
	}()

	<-stopCh
}

func (c *Controller) IsHealthy() error {
	// check if last seen service and endpoint processing is more than an hour ago
	if c.syncTracker.Get().Before(time.Now().Add(-time.Hour)) {
		msg := fmt.Sprintf("NEG controller has not processed any service "+
			"and endpoint updates for more than an hour. Something went wrong. "+
			"Last sync was on %v", c.syncTracker.Get())
		klog.Error(msg)
		return fmt.Errorf(msg)
	}
	return nil
}

func (c *Controller) stop() {
	klog.V(2).Infof("Shutting down network endpoint group controller")
	c.serviceQueue.ShutDown()
	c.endpointQueue.ShutDown()
	c.manager.ShutDown()
}

func (c *Controller) endpointWorker() {
	for {
		func() {
			key, quit := c.endpointQueue.Get()
			if quit {
				return
			}
			c.processEndpoint(key.(string))
			c.endpointQueue.Done(key)
		}()
	}
}

// processEndpoint finds the related syncers and signal it to sync
func (c *Controller) processEndpoint(key string) {
	defer func() {
		now := c.syncTracker.Track()
		metrics.LastSyncTimestamp.WithLabelValues().Set(float64(now.UTC().UnixNano()))
	}()

	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		klog.Errorf("Failed to split endpoint namespaced key %q: %v", key, err)
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
	defer func() {
		now := c.syncTracker.Track()
		metrics.LastSyncTimestamp.WithLabelValues().Set(float64(now.UTC().UnixNano()))
	}()

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
	var foundNEGAnnotation bool
	var negAnnotation *annotations.NegAnnotation
	if exists {
		service = svc.(*apiv1.Service)
		negAnnotation, foundNEGAnnotation, err = annotations.FromService(service).NEGAnnotation()
		if err != nil {
			return err
		}
	}

	// If neg annotation is not found or NEG is not enabled
	if !foundNEGAnnotation || negAnnotation == nil || !negAnnotation.NEGEnabled() {
		c.manager.StopSyncer(namespace, name)
		// delete the annotation
		return c.syncNegStatusAnnotation(namespace, name, make(negtypes.PortInfoMap))
	}

	klog.V(2).Infof("Syncing service %q", key)
	portInfoMap := make(negtypes.PortInfoMap)
	// handle NEGs used by ingress
	if negAnnotation.NEGEnabledForIngress() {
		// Only service ports referenced by ingress are synced for NEG
		ings := getIngressServicesFromStore(c.ingressLister, service)
		ingressSvcPorts := gatherPortMappingUsedByIngress(ings, service)
		ingressPortInfoMap := negtypes.NewPortInfoMap(namespace, name, ingressSvcPorts, c.namer)
		if err := portInfoMap.Merge(ingressPortInfoMap); err != nil {
			return fmt.Errorf("failed to merge service ports referenced by ingress (%v): %v", ingressPortInfoMap, err)
		}
	}

	// handle Exposed Standalone NEGs
	if negAnnotation.NEGExposed() {
		knownPorts := make(negtypes.SvcPortMap)
		for _, sp := range service.Spec.Ports {
			knownPorts[sp.Port] = sp.TargetPort.String()
		}

		exposedNegSvcPort, err := negServicePorts(negAnnotation, knownPorts)
		if err != nil {
			return err
		}

		if err := portInfoMap.Merge(negtypes.NewPortInfoMap(namespace, name, exposedNegSvcPort, c.namer)); err != nil {
			return fmt.Errorf("failed to merge service ports exposed as standalone NEGs (%v) into ingress referenced service ports (%v): %v", exposedNegSvcPort, portInfoMap, err)
		}
	}

	err = c.syncNegStatusAnnotation(namespace, name, portInfoMap)
	if err != nil {
		return err
	}
	return c.manager.EnsureSyncers(namespace, name, portInfoMap)
}

func (c *Controller) syncNegStatusAnnotation(namespace, name string, portMap negtypes.PortInfoMap) error {
	zones, err := c.zoneGetter.ListZones()
	if err != nil {
		return err
	}
	svcClient := c.client.CoreV1().Services(namespace)
	service, err := svcClient.Get(name, metav1.GetOptions{})
	if err != nil {
		return err
	}

	// Remove NEG Status Annotation when no NEG is needed
	if len(portMap) == 0 {
		if _, ok := service.Annotations[annotations.NEGStatusKey]; ok {
			// TODO: use PATCH to remove annotation
			delete(service.Annotations, annotations.NEGStatusKey)
			klog.V(2).Infof("Removing NEG status annotation from service: %s/%s", namespace, name)
			_, err = svcClient.Update(service)
			return err
		}
		// service doesn't have the expose NEG annotation and doesn't need update
		return nil
	}

	negStatus := annotations.NewNegStatus(zones, portMap.ToPortNegMap())
	annotation, err := negStatus.Marshal()
	if err != nil {
		return err
	}
	existingAnnotation, ok := service.Annotations[annotations.NEGStatusKey]
	if ok && existingAnnotation == annotation {
		return nil
	}

	service.Annotations[annotations.NEGStatusKey] = annotation
	klog.V(2).Infof("Updating NEG visibility annotation %q on service %s/%s.", annotation, namespace, name)
	_, err = svcClient.Update(service)
	return err
}

func (c *Controller) handleErr(err error, key interface{}) {
	if err == nil {
		c.serviceQueue.Forget(key)
		return
	}

	msg := fmt.Sprintf("error processing service %q: %v", key, err)
	klog.Errorf(msg)
	if service, exists, err := c.serviceLister.GetByKey(key.(string)); err != nil {
		klog.Warning("Failed to retrieve service %q from store: %v", key.(string), err)
	} else if exists {
		c.recorder.Eventf(service.(*apiv1.Service), apiv1.EventTypeWarning, "ProcessServiceFailed", msg)
	}
	c.serviceQueue.AddRateLimited(key)
}

func (c *Controller) enqueueEndpoint(obj interface{}) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		klog.Errorf("Failed to generate endpoint key: %v", err)
		return
	}
	c.endpointQueue.Add(key)
}

func (c *Controller) enqueueService(obj interface{}) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		klog.Errorf("Failed to generate service key: %v", err)
		return
	}
	c.serviceQueue.Add(key)
}

func (c *Controller) enqueueIngressServices(ing *extensions.Ingress) {
	keys := gatherIngressServiceKeys(ing)
	for key := range keys {
		c.enqueueService(cache.ExplicitKey(key))
	}
}

func (c *Controller) gc() {
	if err := c.manager.GC(); err != nil {
		klog.Errorf("NEG controller garbage collection failed: %v", err)
	}
}

func (c *Controller) synced() bool {
	return c.endpointSynced() &&
		c.serviceSynced() &&
		c.ingressSynced()
}

// gatherPortMappingUsedByIngress returns a map containing port:targetport
// of all service ports of the service that are referenced by ingresses
func gatherPortMappingUsedByIngress(ings []extensions.Ingress, svc *apiv1.Service) negtypes.SvcPortMap {
	servicePorts := sets.NewString()
	ingressSvcPorts := make(negtypes.SvcPortMap)
	for _, ing := range ings {
		if utils.IsGLBCIngress(&ing) {
			utils.TraverseIngressBackends(&ing, func(id utils.ServicePortID) bool {
				if id.Service.Name == svc.Name {
					servicePorts.Insert(id.Port.String())
				}
				return false
			})
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
			ingressSvcPorts[svcPort.Port] = svcPort.TargetPort.String()
		}
	}

	return ingressSvcPorts
}

// gatherIngressServiceKeys returns all service key (formatted as namespace/name) referenced in the ingress
func gatherIngressServiceKeys(ing *extensions.Ingress) sets.String {
	set := sets.NewString()
	if ing == nil {
		return set
	}
	utils.TraverseIngressBackends(ing, func(id utils.ServicePortID) bool {
		set.Insert(utils.ServiceKeyFunc(id.Service.Namespace, id.Service.Name))
		return false
	})
	return set
}

func getIngressServicesFromStore(store cache.Store, svc *apiv1.Service) (ings []extensions.Ingress) {
	for _, m := range store.List() {
		ing := *m.(*extensions.Ingress)
		if ing.Namespace != svc.Namespace {
			continue
		}

		if utils.IsGLBCIngress(&ing) {
			utils.TraverseIngressBackends(&ing, func(id utils.ServicePortID) bool {
				if id.Service.Name == svc.Name {
					ings = append(ings, ing)
					return true
				}
				return false
			})
		}

	}
	return
}
