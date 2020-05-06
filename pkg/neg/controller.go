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
	context2 "context"
	"fmt"
	"time"

	istioV1alpha3 "istio.io/api/networking/v1alpha3"
	apiv1 "k8s.io/api/core/v1"
	"k8s.io/api/networking/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	apimachinerytypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	unversionedcore "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/cloud-provider/service/helpers"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/context"
	"k8s.io/ingress-gce/pkg/controller/translator"
	"k8s.io/ingress-gce/pkg/flags"
	usage "k8s.io/ingress-gce/pkg/metrics"
	"k8s.io/ingress-gce/pkg/neg/metrics"
	"k8s.io/ingress-gce/pkg/neg/readiness"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/common"
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

	hasSynced                   func() bool
	ingressLister               cache.Indexer
	serviceLister               cache.Indexer
	client                      kubernetes.Interface
	defaultBackendService       utils.ServicePort
	destinationRuleLister       cache.Indexer
	destinationRuleClient       dynamic.NamespaceableResourceInterface
	enableASM                   bool
	asmServiceNEGSkipNamespaces []string

	// serviceQueue takes service key as work item. Service key with format "namespace/name".
	serviceQueue workqueue.RateLimitingInterface
	// endpointQueue takes endpoint key as work item. Endpoint key with format "namespace/name".
	endpointQueue workqueue.RateLimitingInterface
	// nodeQueue takes node name as work item.
	nodeQueue workqueue.RateLimitingInterface

	// destinationRuleQueue takes Istio DestinationRule key as work item. DestinationRule key with format "namespace/name"
	destinationRuleQueue workqueue.RateLimitingInterface

	// syncTracker tracks the latest time that service and endpoint changes are processed
	syncTracker utils.TimeTracker
	// nodeSyncTracker tracks the latest time that node changes are processed
	nodeSyncTracker utils.TimeTracker

	// reflector handles NEG readiness gate and conditions for pods in NEG.
	reflector readiness.Reflector

	// collector collects NEG usage metrics
	collector usage.NegMetricsCollector

	// runL4 indicates whether to run NEG controller that processes L4 ILB services
	runL4 bool
}

// NewController returns a network endpoint group controller.
func NewController(
	cloud negtypes.NetworkEndpointGroupCloud,
	ctx *context.ControllerContext,
	zoneGetter negtypes.ZoneGetter,
	namer negtypes.NetworkEndpointGroupNamer,
	resyncPeriod time.Duration,
	gcPeriod time.Duration,
	enableReadinessReflector bool,
	runIngress bool,
	runL4Controller bool,
) *Controller {
	// init event recorder
	// TODO: move event recorder initializer to main. Reuse it among controllers.
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(klog.Infof)
	eventBroadcaster.StartRecordingToSink(&unversionedcore.EventSinkImpl{
		Interface: ctx.KubeClient.CoreV1().Events(""),
	})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme,
		apiv1.EventSource{Component: "neg-controller"})

	manager := newSyncerManager(namer, recorder, cloud, zoneGetter, ctx.PodInformer.GetIndexer(), ctx.ServiceInformer.GetIndexer(), ctx.EndpointInformer.GetIndexer(), ctx.NodeInformer.GetIndexer())
	var reflector readiness.Reflector
	if enableReadinessReflector {
		reflector = readiness.NewReadinessReflector(ctx, manager)
	} else {
		reflector = &readiness.NoopReflector{}
	}
	manager.reflector = reflector

	negController := &Controller{
		client:                ctx.KubeClient,
		manager:               manager,
		resyncPeriod:          resyncPeriod,
		gcPeriod:              gcPeriod,
		recorder:              recorder,
		zoneGetter:            zoneGetter,
		namer:                 namer,
		defaultBackendService: ctx.DefaultBackendSvcPort,
		hasSynced:             ctx.HasSynced,
		ingressLister:         ctx.IngressInformer.GetIndexer(),
		serviceLister:         ctx.ServiceInformer.GetIndexer(),
		serviceQueue:          workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		endpointQueue:         workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		nodeQueue:             workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		syncTracker:           utils.NewTimeTracker(),
		reflector:             reflector,
		collector:             ctx.ControllerMetrics,
		runL4:                 runL4Controller,
	}

	if runIngress {
		ctx.IngressInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				addIng := obj.(*v1beta1.Ingress)
				if !utils.IsGLBCIngress(addIng) {
					klog.V(4).Infof("Ignoring add for ingress %v based on annotation %v", common.NamespacedName(addIng), annotations.IngressClassKey)
					return
				}
				negController.enqueueIngressServices(addIng)
			},
			DeleteFunc: func(obj interface{}) {
				delIng := obj.(*v1beta1.Ingress)
				if !utils.IsGLBCIngress(delIng) {
					klog.V(4).Infof("Ignoring delete for ingress %v based on annotation %v", common.NamespacedName(delIng), annotations.IngressClassKey)
					return
				}
				negController.enqueueIngressServices(delIng)
			},
			UpdateFunc: func(old, cur interface{}) {
				oldIng := cur.(*v1beta1.Ingress)
				curIng := cur.(*v1beta1.Ingress)
				if !utils.IsGLBCIngress(curIng) {
					klog.V(4).Infof("Ignoring update for ingress %v based on annotation %v", common.NamespacedName(curIng), annotations.IngressClassKey)
					return
				}
				keys := gatherIngressServiceKeys(oldIng)
				keys = keys.Union(gatherIngressServiceKeys(curIng))
				for _, key := range keys.List() {
					negController.enqueueService(cache.ExplicitKey(key))
				}
			},
		})

		ctx.PodInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				pod := obj.(*apiv1.Pod)
				negController.reflector.SyncPod(pod)
			},
			UpdateFunc: func(old, cur interface{}) {
				pod := cur.(*apiv1.Pod)
				negController.reflector.SyncPod(pod)
			},
		})
	}
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

	if negController.runL4 {
		ctx.NodeInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				node := obj.(*apiv1.Node)
				negController.enqueueNode(node)
			},
			DeleteFunc: func(obj interface{}) {
				node := obj.(*apiv1.Node)
				negController.enqueueNode(node)
			},
		})
	}

	if ctx.EnableASMConfigMap {
		cmconfig := ctx.ASMConfigController.GetConfig()
		if cmconfig.EnableASM {
			negController.enableASM = cmconfig.EnableASM
			negController.asmServiceNEGSkipNamespaces = cmconfig.ASMServiceNEGSkipNamespaces
			negController.destinationRuleLister = ctx.DestinationRuleInformer.GetIndexer()
			ctx.DestinationRuleInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
				AddFunc:    negController.enqueueDestinationRule,
				DeleteFunc: negController.enqueueDestinationRule,
				UpdateFunc: func(old, cur interface{}) {
					negController.enqueueDestinationRule(cur)
				},
			})
			negController.destinationRuleClient = ctx.DestinationRuleClient
		}
	}

	ctx.AddHealthCheck("neg-controller", negController.IsHealthy)
	return negController
}

func (c *Controller) Run(stopCh <-chan struct{}) {
	wait.PollUntil(5*time.Second, func() (bool, error) {
		klog.V(2).Infof("Waiting for initial sync")
		return c.hasSynced(), nil
	}, stopCh)

	klog.V(2).Infof("Starting network endpoint group controller")
	defer func() {
		klog.V(2).Infof("Shutting down network endpoint group controller")
		c.stop()
	}()

	go wait.Until(c.serviceWorker, time.Second, stopCh)
	go wait.Until(c.endpointWorker, time.Second, stopCh)
	go wait.Until(c.nodeWorker, time.Second, stopCh)
	go func() {
		// Wait for gcPeriod to run the first GC
		// This is to make sure that all services are fully processed before running GC.
		time.Sleep(c.gcPeriod)
		wait.Until(c.gc, c.gcPeriod, stopCh)
	}()
	go c.reflector.Run(stopCh)
	<-stopCh
}

func (c *Controller) IsHealthy() error {
	// log the last node sync
	klog.V(5).Infof("Last node sync was at %v", c.nodeSyncTracker.Get())
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

func (c *Controller) nodeWorker() {
	for {
		func() {
			key, quit := c.nodeQueue.Get()
			if quit {
				return
			}
			c.processNode()
			c.nodeQueue.Done(key)
		}()
	}
}

// processNode finds the related syncers and signal it to sync
// use a semaphore approach where all vm_ip syncers can wake up.
func (c *Controller) processNode() {
	defer func() {
		now := c.nodeSyncTracker.Track()
		metrics.LastSyncTimestamp.WithLabelValues().Set(float64(now.UTC().UnixNano()))
	}()
	c.manager.SyncNodes()
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

	obj, exists, err := c.serviceLister.GetByKey(key)
	if err != nil {
		return err
	}
	if !exists {
		c.collector.DeleteNegService(key)
		c.manager.StopSyncer(namespace, name)
		return nil
	}

	service := obj.(*apiv1.Service)
	if service == nil {
		return fmt.Errorf("cannot convert to Service (%T)", obj)
	}
	negUsage := usage.NegServiceState{}
	svcPortInfoMap := make(negtypes.PortInfoMap)
	if err := c.mergeDefaultBackendServicePortInfoMap(key, svcPortInfoMap); err != nil {
		return err
	}
	negUsage.IngressNeg = len(svcPortInfoMap)
	if err := c.mergeIngressPortInfo(service, types.NamespacedName{Namespace: namespace, Name: name}, svcPortInfoMap); err != nil {
		return err
	}
	negUsage.IngressNeg = len(svcPortInfoMap)
	if err := c.mergeStandaloneNEGsPortInfo(service, types.NamespacedName{Namespace: namespace, Name: name}, svcPortInfoMap); err != nil {
		return err
	}
	negUsage.StandaloneNeg = len(svcPortInfoMap) - negUsage.IngressNeg
	csmSVCPortInfoMap, destinationRulesPortInfoMap, err := c.getCSMPortInfoMap(namespace, name, service)
	if err != nil {
		return err
	}
	negUsage.AsmNeg = len(csmSVCPortInfoMap) + len(destinationRulesPortInfoMap)

	// merges csmSVCPortInfoMap, because eventually those NEG will sync with the service annotation.
	// merges destinationRulesPortInfoMap later, because we only want them sync with the DestinationRule annotation.
	if err := svcPortInfoMap.Merge(csmSVCPortInfoMap); err != nil {
		return fmt.Errorf("failed to merge CSM service PortInfoMap: %v, error: %v", csmSVCPortInfoMap, err)
	}
	if c.runL4 {
		if err := c.mergeVmIpNEGsPortInfo(service, types.NamespacedName{Namespace: namespace, Name: name}, svcPortInfoMap, negUsage); err != nil {
			return err
		}
	}
	if len(svcPortInfoMap) != 0 || len(destinationRulesPortInfoMap) != 0 {
		klog.V(2).Infof("Syncing service %q", key)
		if err = c.syncNegStatusAnnotation(namespace, name, svcPortInfoMap); err != nil {
			return err
		}
		// Merge destinationRule related NEG after the Service NEGStatus Sync, we don't want DR related NEG status go into service.
		if err := svcPortInfoMap.Merge(destinationRulesPortInfoMap); err != nil {
			return fmt.Errorf("failed to merge service ports referenced by Istio:DestinationRule (%v): %v", destinationRulesPortInfoMap, err)
		}
		c.collector.SetNegService(key, negUsage)
		return c.manager.EnsureSyncers(namespace, name, svcPortInfoMap)
	}

	// do not need Neg
	klog.V(4).Infof("Service %q does not need any NEG. Skipping", key)
	c.collector.DeleteNegService(key)
	// neg annotation is not found or NEG is not enabled
	c.manager.StopSyncer(namespace, name)
	// delete the annotation
	return c.syncNegStatusAnnotation(namespace, name, make(negtypes.PortInfoMap))
}

// mergeIngressPortInfo merges Ingress PortInfo into portInfoMap if the service has Enable Ingress annotation.
func (c *Controller) mergeIngressPortInfo(service *apiv1.Service, name types.NamespacedName, portInfoMap negtypes.PortInfoMap) error {
	negAnnotation, foundNEGAnnotation, err := annotations.FromService(service).NEGAnnotation()
	if err != nil {
		return err
	}
	if !foundNEGAnnotation {
		return nil
	}

	// handle NEGs used by ingress
	if negAnnotation != nil && negAnnotation.NEGEnabledForIngress() {
		// Only service ports referenced by ingress are synced for NEG
		ings := getIngressServicesFromStore(c.ingressLister, service)
		ingressSvcPortTuples := gatherPortMappingUsedByIngress(ings, service)
		ingressPortInfoMap := negtypes.NewPortInfoMap(name.Namespace, name.Name, ingressSvcPortTuples, c.namer, true)
		if err := portInfoMap.Merge(ingressPortInfoMap); err != nil {
			return fmt.Errorf("failed to merge service ports referenced by ingress (%v): %v", ingressPortInfoMap, err)
		}
	}
	return nil
}

// mergeStandaloneNEGsPortInfo merge Standalone NEG PortInfo into portInfoMap
func (c *Controller) mergeStandaloneNEGsPortInfo(service *apiv1.Service, name types.NamespacedName, portInfoMap negtypes.PortInfoMap) error {
	negAnnotation, foundNEGAnnotation, err := annotations.FromService(service).NEGAnnotation()
	if err != nil {
		return err
	}
	if !foundNEGAnnotation {
		return nil
	}
	// handle Exposed Standalone NEGs
	if negAnnotation != nil && negAnnotation.NEGExposed() {
		knowSvcPortSet := make(negtypes.SvcPortTupleSet)
		for _, sp := range service.Spec.Ports {
			knowSvcPortSet.Insert(
				negtypes.SvcPortTuple{
					Port:       sp.Port,
					Name:       sp.Name,
					TargetPort: sp.TargetPort.String(),
				},
			)
		}

		exposedNegSvcPort, err := negServicePorts(negAnnotation, knowSvcPortSet)
		if err != nil {
			return err
		}

		if err := portInfoMap.Merge(negtypes.NewPortInfoMap(name.Namespace, name.Name, exposedNegSvcPort, c.namer /*readinessGate*/, true)); err != nil {
			return fmt.Errorf("failed to merge service ports exposed as standalone NEGs (%v) into ingress referenced service ports (%v): %v", exposedNegSvcPort, portInfoMap, err)
		}
	}

	return nil
}

// mergeVmIpNEGsPortInfo merges the PortInfo for ILB services using GCE_VM_IP NEGs into portInfoMap
func (c *Controller) mergeVmIpNEGsPortInfo(service *apiv1.Service, name types.NamespacedName, portInfoMap negtypes.PortInfoMap, negUsage usage.NegServiceState) error {
	if wantsILB, _ := annotations.WantsL4ILB(service); !wantsILB {
		return nil
	}
	if utils.IsLegacyL4ILBService(service) {
		msg := fmt.Sprintf("Ignoring ILB Service %s, namespace %s as it contains legacy resources created by service controller", service.Name, service.Namespace)
		klog.Warning(msg)
		c.recorder.Eventf(service, apiv1.EventTypeWarning, "ProcessServiceFailed", msg)
		return nil
	}

	onlyLocal := helpers.RequestsOnlyLocalTraffic(service)
	// Update usage metrics.
	negUsage.VmIpNeg = usage.NewVmIpNegType(onlyLocal)

	return portInfoMap.Merge(negtypes.NewPortInfoMapForVMIPNEG(name.Namespace, name.Name, c.namer, !onlyLocal))
}

// mergeDefaultBackendServicePortInfoMap merge the PortInfoMap for the default backend service into portInfoMap
// The default backend service needs special handling since it is not explicitly referenced
// in the ingress spec.  It is either inferred and then managed by the controller, or
// it is passed to the controller via a command line flag.
// Additionally, supporting NEGs for default backends is only for L7-ILB
func (c *Controller) mergeDefaultBackendServicePortInfoMap(key string, portInfoMap negtypes.PortInfoMap) error {
	// process default backend service
	// Only enable for L7-ILB for now to limit possible issues
	// TODO(shance): investigate enabling this for all ingresses

	if flags.F.EnableL7Ilb && c.defaultBackendService.ID.Service.String() == key {
		for _, m := range c.ingressLister.List() {
			ing := *m.(*v1beta1.Ingress)
			if utils.IsGCEL7ILBIngress(&ing) && ing.Spec.Backend == nil {
				svcPortTupleSet := make(negtypes.SvcPortTupleSet)
				svcPortTupleSet.Insert(negtypes.SvcPortTuple{
					Name:       c.defaultBackendService.ID.Port.String(),
					Port:       c.defaultBackendService.Port,
					TargetPort: c.defaultBackendService.TargetPort,
				})
				defaultServicePortInfoMap := negtypes.NewPortInfoMap(c.defaultBackendService.ID.Service.Namespace, c.defaultBackendService.ID.Service.Name, svcPortTupleSet, c.namer, false)
				return portInfoMap.Merge(defaultServicePortInfoMap)
			}
		}
	}
	return nil
}

// getCSMPortInfoMap gets the PortInfoMap for service and DestinationRules.
// If enableCSM = true, the controller will create NEGs for every port/subsets combinations for the DestinaitonRules.
// It will also create NEGs for all the ports of the service that referred by the DestinationRules.
func (c *Controller) getCSMPortInfoMap(namespace, name string, service *apiv1.Service) (negtypes.PortInfoMap, negtypes.PortInfoMap, error) {
	destinationRulesPortInfoMap := make(negtypes.PortInfoMap)
	servicePortInfoMap := make(negtypes.PortInfoMap)
	if c.enableASM {
		// Find all destination rules that using this service.
		destinationRules := getDestinationRulesFromStore(c.destinationRuleLister, service)
		// Fill all service ports into portinfomap
		servicePorts := gatherPortMappingFromService(service)
		for namespacedName, destinationRule := range destinationRules {
			destinationRulePortInfoMap, err := negtypes.NewPortInfoMapWithDestinationRule(namespace, name, servicePorts, c.namer, false, destinationRule)
			if err != nil {
				klog.Warningf("DestinationRule(%s) contains duplicated subset, creating NEGs for the newer ones. %s", namespacedName.Name, err)
			}
			if err := destinationRulesPortInfoMap.Merge(destinationRulePortInfoMap); err != nil {
				return servicePortInfoMap, destinationRulesPortInfoMap, fmt.Errorf("failed to merge service ports referenced by Istio:DestinationRule (%v): %v", destinationRulePortInfoMap, err)
			}
			if err = c.syncDestinationRuleNegStatusAnnotation(namespacedName.Namespace, namespacedName.Name, destinationRulePortInfoMap); err != nil {
				return servicePortInfoMap, destinationRulesPortInfoMap, err
			}
		}
		// Create NEGs for every ports of the services.
		if service.Spec.Selector == nil || len(service.Spec.Selector) == 0 {
			klog.Infof("Skip NEG creation for services that with no selector: %s:%s", namespace, name)
		} else if contains(c.asmServiceNEGSkipNamespaces, namespace) {
			klog.Infof("Skip NEG creation for services in namespace: %s", namespace)
		} else {
			servicePortInfoMap = negtypes.NewPortInfoMap(namespace, name, servicePorts, c.namer, false)
		}
		return servicePortInfoMap, destinationRulesPortInfoMap, nil
	}
	return servicePortInfoMap, destinationRulesPortInfoMap, nil
}

// syncNegStatusAnnotation syncs the neg status annotation
// it takes service namespace, name and the expected service ports for NEGs.
func (c *Controller) syncNegStatusAnnotation(namespace, name string, portMap negtypes.PortInfoMap) error {
	zones, err := c.zoneGetter.ListZones()
	if err != nil {
		return err
	}
	svcClient := c.client.CoreV1().Services(namespace)
	service, err := svcClient.Get(context2.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return err
	}

	// Remove NEG Status Annotation when no NEG is needed
	if len(portMap) == 0 {
		if _, ok := service.Annotations[annotations.NEGStatusKey]; ok {
			// TODO: use PATCH to remove annotation
			delete(service.Annotations, annotations.NEGStatusKey)
			klog.V(2).Infof("Removing NEG status annotation from service: %s/%s", namespace, name)
			_, err = svcClient.Update(context2.TODO(), service, metav1.UpdateOptions{})
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
	// If enableCSM=true, it's possible a service having nil Annotations.
	if service.Annotations == nil {
		service.Annotations = make(map[string]string)
	}
	service.Annotations[annotations.NEGStatusKey] = annotation
	klog.V(2).Infof("Updating NEG visibility annotation %q on service %s/%s.", annotation, namespace, name)
	_, err = svcClient.Update(context2.TODO(), service, metav1.UpdateOptions{})
	return err
}

// syncDestinationRuleNegStatusAnnotation syncs the destinationrule related neg status annotation
func (c *Controller) syncDestinationRuleNegStatusAnnotation(namespace, destinationRuleName string, portmap negtypes.PortInfoMap) error {
	zones, err := c.zoneGetter.ListZones()
	if err != nil {
		return err
	}
	dsClient := c.destinationRuleClient.Namespace(namespace)
	destinationRule, err := dsClient.Get(context2.TODO(), destinationRuleName, metav1.GetOptions{})
	drAnnotations := destinationRule.GetAnnotations()
	if drAnnotations == nil {
		drAnnotations = make(map[string]string)
	}
	if len(portmap) == 0 {
		delete(drAnnotations, annotations.NEGStatusKey)
		klog.V(2).Infof("Removing NEG status annotation from DestinationRule: %s/%s", namespace, destinationRule)
	} else {
		negStatus := annotations.NewDestinationRuleNegStatus(zones, portmap.ToPortSubsetNegMap())
		negStatuAnnotation, err := negStatus.Marshal()
		if err != nil {
			return err
		}
		drAnnotations[annotations.NEGStatusKey] = negStatuAnnotation

		existingAnnotation, ok := destinationRule.GetAnnotations()[annotations.NEGStatusKey]
		if ok && existingAnnotation == drAnnotations[annotations.NEGStatusKey] {
			return nil
		}
	}
	newDestinationRule := destinationRule.DeepCopy()
	newDestinationRule.SetAnnotations(drAnnotations)
	// Get the diff, we only need the Object meta diff.
	patchBytes, err := utils.StrategicMergePatchBytes(destinationRule, newDestinationRule, struct {
		metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`
	}{})
	if err != nil {
		return err
	}
	klog.V(2).Infof("Updating NEG visibility annotation %q on Istio:DestinationRule %s/%s.", string(patchBytes), namespace, destinationRuleName)
	_, err = dsClient.Patch(context2.TODO(), destinationRuleName, apimachinerytypes.MergePatchType, patchBytes, metav1.PatchOptions{})
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
		klog.Warningf("Failed to retrieve service %q from store: %v", key.(string), err)
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

func (c *Controller) enqueueNode(obj interface{}) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		klog.Errorf("Failed to generate endpoint key: %v", err)
		return
	}
	c.nodeQueue.Add(key)
}

func (c *Controller) enqueueService(obj interface{}) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		klog.Errorf("Failed to generate service key: %v", err)
		return
	}
	c.serviceQueue.Add(key)
}

func (c *Controller) enqueueIngressServices(ing *v1beta1.Ingress) {
	// enqueue services referenced by ingress
	keys := gatherIngressServiceKeys(ing)
	for key := range keys {
		c.enqueueService(cache.ExplicitKey(key))
	}

	// enqueue default backend service
	if flags.F.EnableL7Ilb && ing.Spec.Backend == nil {
		c.enqueueService(cache.ExplicitKey(c.defaultBackendService.ID.Service.String()))
	}
}

// enqueueDestinationRule will enqueue the service used by obj.
func (c *Controller) enqueueDestinationRule(obj interface{}) {
	drus, ok := obj.(*unstructured.Unstructured)
	if !ok {
		klog.Errorf("Failed to convert informer object to Unstructured object")
		return
	}
	targetServiceNamespace, drHost, _, err := castToDestinationRule(drus)
	if err != nil {
		klog.Errorf("Failed to convert informer object to DestinationRule")
		return
	}
	svcKey := utils.ServiceKeyFunc(targetServiceNamespace, drHost)
	c.enqueueService(cache.ExplicitKey(svcKey))
}

func (c *Controller) gc() {
	if err := c.manager.GC(); err != nil {
		klog.Errorf("NEG controller garbage collection failed: %v", err)
	}
}

// gatherPortMappingUsedByIngress returns a map containing port:targetport
// of all service ports of the service that are referenced by ingresses
func gatherPortMappingUsedByIngress(ings []v1beta1.Ingress, svc *apiv1.Service) negtypes.SvcPortTupleSet {
	ingressSvcPortTuples := make(negtypes.SvcPortTupleSet)
	for _, ing := range ings {
		if utils.IsGLBCIngress(&ing) {
			utils.TraverseIngressBackends(&ing, func(id utils.ServicePortID) bool {
				if id.Service.Name == svc.Name && id.Service.Namespace == svc.Namespace {
					servicePort := translator.ServicePort(*svc, id.Port)
					if servicePort == nil {
						klog.Warningf("Port %+v in Service %q not found", id.Port, id.Service.String())
						return false
					}
					ingressSvcPortTuples.Insert(negtypes.SvcPortTuple{
						Port:       servicePort.Port,
						Name:       servicePort.Name,
						TargetPort: servicePort.TargetPort.String(),
					})
				}
				return false
			})
		}
	}
	return ingressSvcPortTuples
}

// gatherIngressServiceKeys returns all service key (formatted as namespace/name) referenced in the ingress
func gatherIngressServiceKeys(ing *v1beta1.Ingress) sets.String {
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

func getIngressServicesFromStore(store cache.Store, svc *apiv1.Service) (ings []v1beta1.Ingress) {
	for _, m := range store.List() {
		ing := *m.(*v1beta1.Ingress)
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

// gatherPortMappingFromService returns PortMapping for all ports of the service.
// Mapping all ports since Istio DestinationRule is using all ports.
func gatherPortMappingFromService(svc *apiv1.Service) negtypes.SvcPortTupleSet {
	svcPortTupleSet := make(negtypes.SvcPortTupleSet)
	for _, svcPort := range svc.Spec.Ports {
		svcPortTupleSet.Insert(negtypes.SvcPortTuple{
			Port:       svcPort.Port,
			Name:       svcPort.Name,
			TargetPort: svcPort.TargetPort.String(),
		})
	}
	return svcPortTupleSet
}

// getDestinationRulesFromStore returns all DestinationRules that referring service svc.
// Please notice that a DestionationRule can point to a service in a different namespace.
func getDestinationRulesFromStore(store cache.Store, svc *apiv1.Service) (drs map[apimachinerytypes.NamespacedName]*istioV1alpha3.DestinationRule) {
	drs = make(map[apimachinerytypes.NamespacedName]*istioV1alpha3.DestinationRule)
	for _, obj := range store.List() {
		drUnstructed := obj.(*unstructured.Unstructured)
		targetServiceNamespace, drHost, dr, err := castToDestinationRule(drUnstructed)
		if err != nil {
			klog.Errorf("Failed to cast Unstructured DestinationRule to DestinationRule.")
			continue
		}

		if targetServiceNamespace == svc.Namespace && drHost == svc.Name {
			// We want to return DestinationRule namespace but not the target service namespace.
			drs[apimachinerytypes.NamespacedName{Namespace: drUnstructed.GetNamespace(), Name: drUnstructed.GetName()}] = dr
		}
	}
	return
}
