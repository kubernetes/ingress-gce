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
	"time"

	nodetopologyv1 "github.com/GoogleCloudPlatform/gke-networking-api/apis/nodetopology/v1"
	apiv1 "k8s.io/api/core/v1"
	discovery "k8s.io/api/discovery/v1"
	v1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	unversionedcore "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/cloud-provider/service/helpers"
	"k8s.io/ingress-gce/pkg/annotations"
	svcnegv1beta1 "k8s.io/ingress-gce/pkg/apis/svcneg/v1beta1"
	"k8s.io/ingress-gce/pkg/controller/translator"
	"k8s.io/ingress-gce/pkg/flags"
	metrics "k8s.io/ingress-gce/pkg/neg/metrics"
	"k8s.io/ingress-gce/pkg/neg/metrics/metricscollector"
	syncMetrics "k8s.io/ingress-gce/pkg/neg/metrics/metricscollector"
	"k8s.io/ingress-gce/pkg/neg/readiness"
	"k8s.io/ingress-gce/pkg/neg/syncers/labels"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/ingress-gce/pkg/network"
	svcnegclient "k8s.io/ingress-gce/pkg/svcneg/client/clientset/versioned"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/endpointslices"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/ingress-gce/pkg/utils/patch"
	"k8s.io/ingress-gce/pkg/utils/zonegetter"
	"k8s.io/klog/v2"
)

func init() {
	// register prometheus metrics
	metrics.RegisterMetrics()
	syncMetrics.RegisterMetrics()
}

// Controller is network endpoint group controller.
// It determines whether NEG for a service port is needed, then signals NegSyncerManager to sync it.
type Controller struct {
	manager         negtypes.NegSyncerManager
	gcPeriod        time.Duration
	recorder        record.EventRecorder
	namer           negtypes.NetworkEndpointGroupNamer
	l4Namer         namer.L4ResourcesNamer
	zoneGetter      *zonegetter.ZoneGetter
	networkResolver network.Resolver

	hasSynced             func() bool
	ingressLister         cache.Indexer
	serviceLister         cache.Indexer
	client                kubernetes.Interface
	defaultBackendService utils.ServicePort

	// serviceQueue takes service key as work item. Service key with format "namespace/name".
	serviceQueue workqueue.RateLimitingInterface
	// endpointQueue takes endpoint key as work item. Endpoint key with format "namespace/name".
	endpointQueue workqueue.RateLimitingInterface
	// nodeQueue takes node name as work item.
	nodeQueue workqueue.RateLimitingInterface
	// nodeTopologyQueue acts as an intermeidate queue to trigger sync on all
	// syncers on Node Topology resource updates.
	nodeTopologyQueue workqueue.RateLimitingInterface

	// syncTracker tracks the latest time that service and endpoint changes are processed
	syncTracker utils.TimeTracker
	// nodeSyncTracker tracks the latest time that node changes are processed
	nodeSyncTracker utils.TimeTracker

	// reflector handles NEG readiness gate and conditions for pods in NEG.
	reflector readiness.Reflector

	// syncerMetrics collects NEG controller metrics
	syncerMetrics *syncMetrics.SyncerMetrics

	// runL4ForILB indicates whether to run NEG controller that processes L4 ILB services
	runL4ForILB bool

	// enableIngressRegionalExternal indicates where NEG controller should process
	// gce-regional-external ingresses
	enableIngressRegionalExternal bool

	// enableMultiSubnetClusterPhase1 indicates whether NEG controller should create
	// additional NEGs in the non-default subnets.
	enableMultiSubnetClusterPhase1 bool

	// runL4ForNetLB indicates if the controller can create NEGs for L4 NetLB services.
	runL4ForNetLB bool

	stopCh <-chan struct{}
	logger klog.Logger

	// negMetrics is used to collect metrics for NEG
	negMetrics *metrics.NegMetrics
}

// NewController returns a network endpoint group controller.
func NewController(
	kubeClient kubernetes.Interface,
	svcNegClient svcnegclient.Interface,
	eventRecorderClient kubernetes.Interface,
	kubeSystemUID types.UID,
	ingressInformer cache.SharedIndexInformer,
	serviceInformer cache.SharedIndexInformer,
	podInformer cache.SharedIndexInformer,
	nodeInformer cache.SharedIndexInformer,
	endpointSliceInformer cache.SharedIndexInformer,
	svcNegInformer cache.SharedIndexInformer,
	networkInformer cache.SharedIndexInformer,
	gkeNetworkParamSetInformer cache.SharedIndexInformer,
	nodeTopologyInformer cache.SharedIndexInformer,
	hasSynced func() bool,
	l4Namer namer.L4ResourcesNamer,
	defaultBackendService utils.ServicePort,
	cloud negtypes.NetworkEndpointGroupCloud,
	zoneGetter *zonegetter.ZoneGetter,
	namer negtypes.NetworkEndpointGroupNamer,
	resyncPeriod time.Duration,
	gcPeriod time.Duration,
	numGCWorkers int,
	enableReadinessReflector bool,
	runL4Controller bool,
	enableNonGcpMode bool,
	enableDualStackNEG bool,
	lpConfig labels.PodLabelPropagationConfig,
	enableMultiNetworking bool,
	enableIngressRegionalExternal bool,
	runL4ForNetLB bool,
	stopCh <-chan struct{},
	logger klog.Logger,
	negMetrics *metrics.NegMetrics,
) (*Controller, error) {
	if svcNegClient == nil {
		return nil, fmt.Errorf("svcNegClient is nil")
	}
	// init event recorder
	// TODO: move event recorder initializer to main. Reuse it among controllers.
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartStructuredLogging(0)
	eventBroadcaster.StartRecordingToSink(&unversionedcore.EventSinkImpl{
		Interface: eventRecorderClient.CoreV1().Events(""),
	})
	negScheme := runtime.NewScheme()
	err := scheme.AddToScheme(negScheme)
	if err != nil {
		logger.Error(err, "Errored adding default scheme to event recorder")
		negMetrics.PublishNegControllerErrorCountMetrics(err, true)
	}
	err = svcnegv1beta1.AddToScheme(negScheme)
	if err != nil {
		logger.Error(err, "Errored adding NEG CRD scheme to event recorder")
		negMetrics.PublishNegControllerErrorCountMetrics(err, true)
	}
	recorder := eventBroadcaster.NewRecorder(negScheme,
		apiv1.EventSource{Component: "neg-controller"})

	syncerMetrics := syncMetrics.NewNegMetricsCollector(flags.F.NegMetricsExportInterval, logger, negMetrics.ProviderConfigID)

	manager := newSyncerManager(
		namer,
		l4Namer,
		recorder,
		cloud,
		zoneGetter,
		svcNegClient,
		kubeSystemUID,
		podInformer.GetIndexer(),
		serviceInformer.GetIndexer(),
		endpointSliceInformer.GetIndexer(),
		nodeInformer.GetIndexer(),
		svcNegInformer.GetIndexer(),
		syncerMetrics,
		enableNonGcpMode,
		enableDualStackNEG,
		numGCWorkers,
		lpConfig,
		logger,
		negMetrics,
	)

	var reflector readiness.Reflector
	if enableReadinessReflector {
		reflector = readiness.NewReadinessReflector(
			kubeClient,
			eventRecorderClient,
			podInformer.GetIndexer(),
			cloud,
			manager,
			zoneGetter,
			enableDualStackNEG,
			flags.F.EnableMultiSubnetCluster && !flags.F.EnableMultiSubnetClusterPhase1,
			logger,
			negMetrics,
		)
	} else {
		reflector = &readiness.NoopReflector{}
	}
	manager.reflector = reflector

	var networkIndexer cache.Indexer
	if networkInformer != nil {
		networkIndexer = networkInformer.GetIndexer()
	}
	var gkeNetworkParamSetIndexer cache.Indexer
	if gkeNetworkParamSetInformer != nil {
		gkeNetworkParamSetIndexer = gkeNetworkParamSetInformer.GetIndexer()
	}
	enableMultiSubnetClusterPhase1 := flags.F.EnableMultiSubnetClusterPhase1

	negController := &Controller{
		client:                         kubeClient,
		manager:                        manager,
		gcPeriod:                       gcPeriod,
		recorder:                       recorder,
		zoneGetter:                     zoneGetter,
		namer:                          namer,
		l4Namer:                        l4Namer,
		defaultBackendService:          defaultBackendService,
		hasSynced:                      hasSynced,
		ingressLister:                  ingressInformer.GetIndexer(),
		serviceLister:                  serviceInformer.GetIndexer(),
		networkResolver:                network.NewNetworksResolver(networkIndexer, gkeNetworkParamSetIndexer, cloud, enableMultiNetworking, logger),
		serviceQueue:                   workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "neg_service_queue"),
		endpointQueue:                  workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "neg_endpoint_queue"),
		nodeQueue:                      workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "neg_node_queue"),
		syncTracker:                    utils.NewTimeTracker(),
		reflector:                      reflector,
		syncerMetrics:                  syncerMetrics,
		runL4ForILB:                    runL4Controller,
		enableIngressRegionalExternal:  enableIngressRegionalExternal,
		enableMultiSubnetClusterPhase1: enableMultiSubnetClusterPhase1,
		runL4ForNetLB:                  runL4ForNetLB,
		stopCh:                         stopCh,
		logger:                         logger,
		negMetrics:                     negMetrics,
	}
	if enableMultiSubnetClusterPhase1 {
		negController.nodeTopologyQueue = workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "neg_node_topology_queue")
	}

	ingressInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			addIng := obj.(*v1.Ingress)
			if !utils.IsGLBCIngress(addIng) {
				logger.V(4).Info("Ignoring add for ingress based on annotation", "ingress", klog.KObj(addIng), "annotation", annotations.IngressClassKey)
				return
			}
			negController.enqueueIngressServices(addIng)
		},
		DeleteFunc: func(obj interface{}) {
			delIng := obj.(*v1.Ingress)
			if !utils.IsGLBCIngress(delIng) {
				logger.V(4).Info("Ignoring delete for ingress based on annotation", "ingress", klog.KObj(delIng), "annotation", annotations.IngressClassKey)
				return
			}
			negController.enqueueIngressServices(delIng)
		},
		UpdateFunc: func(old, cur interface{}) {
			oldIng := old.(*v1.Ingress)
			curIng := cur.(*v1.Ingress)
			// Check if ingress class changed and previous class was a GCE ingress
			// Ingress class change may require cleanup so enqueue related services
			if !utils.IsGLBCIngress(curIng) && !utils.IsGLBCIngress(oldIng) {
				logger.V(4).Info("Ignoring update for ingress based on annotation", "ingress", klog.KObj(curIng), "annotation", annotations.IngressClassKey)
				return
			}
			keys := gatherIngressServiceKeys(oldIng)
			keys = keys.Union(gatherIngressServiceKeys(curIng))
			for _, key := range keys.List() {
				negController.enqueueService(cache.ExplicitKey(key))
			}
		},
	})
	podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			pod := obj.(*apiv1.Pod)
			negController.reflector.SyncPod(pod)
		},
		UpdateFunc: func(old, cur interface{}) {
			pod := cur.(*apiv1.Pod)
			negController.reflector.SyncPod(pod)
		},
	})
	serviceInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    negController.enqueueService,
		DeleteFunc: negController.enqueueService,
		UpdateFunc: func(old, cur interface{}) {
			negController.enqueueService(cur)
		},
	})
	endpointSliceInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    negController.enqueueEndpointSlice,
		DeleteFunc: negController.enqueueEndpointSlice,
		UpdateFunc: func(old, cur interface{}) {
			negController.enqueueEndpointSlice(cur)
		},
	})
	nodeInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			node := obj.(*apiv1.Node)
			negController.enqueueNode(node)
		},
		DeleteFunc: func(obj interface{}) {
			node := obj.(*apiv1.Node)
			negController.enqueueNode(node)
		},
		UpdateFunc: func(old, cur interface{}) {
			oldNode := old.(*apiv1.Node)
			currentNode := cur.(*apiv1.Node)

			vmIpCandidateNodeCheck := zonegetter.CandidateAndUnreadyNodesFilter
			vmIpPortCandidateNodeCheck := zonegetter.CandidateNodesFilter

			if zoneGetter.IsNodeSelectedByFilter(oldNode, vmIpCandidateNodeCheck, logger) != zoneGetter.IsNodeSelectedByFilter(currentNode, vmIpCandidateNodeCheck, logger) ||
				zoneGetter.IsNodeSelectedByFilter(oldNode, vmIpPortCandidateNodeCheck, logger) != zoneGetter.IsNodeSelectedByFilter(currentNode, vmIpPortCandidateNodeCheck, logger) {
				logger.Info("Node has changed, enqueueing", "node", currentNode.Name)
				negController.enqueueNode(currentNode)
			}
			// Trigger a sync when node provider ID changed.
			if oldNode.Spec.ProviderID != currentNode.Spec.ProviderID {
				logger.Info("Node provider ID changed, enqueueing", "node", currentNode.Name, "old providerID", oldNode.Spec.ProviderID, "new providerID", currentNode.Spec.ProviderID)
				negController.enqueueNode(currentNode)
			}
		},
	})
	if enableMultiSubnetClusterPhase1 {
		nodeTopologyInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				crd := obj.(*nodetopologyv1.NodeTopology)
				negController.enqueueNodeTopology(crd)
			},
			UpdateFunc: func(old, cur interface{}) {
				oldCrd := old.(*nodetopologyv1.NodeTopology)
				currentCrd := cur.(*nodetopologyv1.NodeTopology)

				var zoneChanged, subnetChanged bool
				if isZoneChanged(oldCrd.Status.Zones, currentCrd.Status.Zones) {
					logger.Info("Zones in Node Topology CR have changed", "oldZones", oldCrd.Status.Zones, "currentZones", currentCrd.Status.Zones)
					zoneChanged = true
				}
				if isSubnetChanged(oldCrd.Status.Subnets, currentCrd.Status.Subnets) {
					logger.Info("Subnets in Node Topology CR have changed", "oldSubnets", oldCrd.Status.Subnets, "currentSubnets", currentCrd.Status.Subnets)
					subnetChanged = true
				}
				if zoneChanged || subnetChanged {
					negController.enqueueNodeTopology(currentCrd)
				}
			},
		})
	}

	return negController, nil
}

func (c *Controller) Run() {
	wait.PollUntil(5*time.Second, func() (bool, error) {
		c.logger.V(2).Info("Waiting for initial sync")
		return c.hasSynced(), nil
	}, c.stopCh)

	c.logger.V(2).Info("Starting network endpoint group controller")
	defer func() {
		c.logger.V(2).Info("Shutting down network endpoint group controller")
		c.stop()
		c.logger.Info("Network Endpoint Group Controller Shutdown")
	}()

	go wait.Until(c.serviceWorker, time.Second, c.stopCh)
	go wait.Until(c.endpointWorker, time.Second, c.stopCh)
	go wait.Until(c.nodeWorker, time.Second, c.stopCh)
	if c.enableMultiSubnetClusterPhase1 {
		go wait.Until(c.nodeTopologyWorker, time.Second, c.stopCh)
	}
	go func() {
		// Wait for gcPeriod to run the first GC
		// This is to make sure that all services are fully processed before running GC.
		time.Sleep(c.gcPeriod)
		wait.Until(c.gc, c.gcPeriod, c.stopCh)
	}()
	go c.reflector.Run(c.stopCh)
	go c.syncerMetrics.Run(c.stopCh)
	<-c.stopCh
}

func (c *Controller) IsHealthy() error {
	// log the last node sync
	c.logger.V(5).Info("Last node sync time", "time", c.nodeSyncTracker.Get())
	// check if last seen service and endpoint processing is more than an hour ago
	if c.syncTracker.Get().Before(time.Now().Add(-time.Hour)) {
		msg := fmt.Sprintf("NEG controller has not processed any service "+
			"and endpoint updates for more than an hour. Something went wrong. "+
			"Last sync was on %v", c.syncTracker.Get())
		c.logger.Error(nil, msg)
		return fmt.Errorf("%s", msg)
	}
	return nil
}

func (c *Controller) stop() {
	c.serviceQueue.ShutDown()
	c.endpointQueue.ShutDown()
	c.nodeQueue.ShutDown()
	if c.enableMultiSubnetClusterPhase1 {
		c.nodeTopologyQueue.ShutDown()
	}
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
		metrics.LastSyncTimestamp.Set(float64(now.UTC().UnixNano()))
	}()
	c.manager.SyncNodes()
}

// processEndpoint finds the related syncers and signal it to sync
func (c *Controller) processEndpoint(key string) {
	defer func() {
		now := c.syncTracker.Track()
		metrics.LastSyncTimestamp.Set(float64(now.UTC().UnixNano()))
	}()

	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		c.logger.Error(err, "Failed to split endpoint namespaced key", "key", key)
		c.negMetrics.PublishNegControllerErrorCountMetrics(err, true)
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
			c.negMetrics.PublishNegControllerErrorCountMetrics(err, false)
		}()
	}
}

// processService takes a service and determines whether it needs NEGs or not.
func (c *Controller) processService(key string) error {
	c.logger.V(3).Info("Processing service", "service", key)
	defer func() {
		now := c.syncTracker.Track()
		metrics.LastSyncTimestamp.Set(float64(now.UTC().UnixNano()))
		c.logger.V(3).Info("Finished processing service", "service", key)
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
		c.syncerMetrics.DeleteNegService(key)
		c.manager.StopSyncer(namespace, name)
		return nil
	}
	service := obj.(*apiv1.Service)
	if service == nil {
		return fmt.Errorf("cannot convert to Service (%T)", obj)
	}
	negUsage := metricscollector.NegServiceState{}
	svcPortInfoMap := make(negtypes.PortInfoMap)
	networkInfo, err := c.networkResolver.ServiceNetwork(service)
	if err != nil {
		return err
	}
	if err := c.mergeDefaultBackendServicePortInfoMap(key, service, svcPortInfoMap, networkInfo); err != nil {
		return err
	}
	negUsage.IngressNeg = len(svcPortInfoMap)
	if err := c.mergeIngressPortInfo(service, types.NamespacedName{Namespace: namespace, Name: name}, svcPortInfoMap, networkInfo); err != nil {
		return err
	}
	negUsage.IngressNeg = len(svcPortInfoMap)
	if err := c.mergeStandaloneNEGsPortInfo(service, types.NamespacedName{Namespace: namespace, Name: name}, svcPortInfoMap, &negUsage, networkInfo); err != nil {
		return err
	}
	negUsage.StandaloneNeg = len(svcPortInfoMap) - negUsage.IngressNeg

	// Create L4 PortInfo if ILB subsetting is enabled or a NetLB service needs NEG backends.
	if err := c.mergeVmIpNEGsPortInfo(service, types.NamespacedName{Namespace: namespace, Name: name}, svcPortInfoMap, &negUsage, networkInfo); err != nil {
		return err
	}
	if len(svcPortInfoMap) != 0 {
		c.logger.V(2).Info("Syncing service", "service", key)
		if !flags.F.EnableIPV6OnlyNEG {
			if service.Spec.Type != apiv1.ServiceTypeLoadBalancer && isSingleStackIPv6Service(service) {
				return fmt.Errorf("NEG is not supported for ipv6 only service (%T)", service)
			}
		}

		if err = c.syncNegStatusAnnotation(namespace, name, svcPortInfoMap); err != nil {
			return err
		}
		negUsage.SuccessfulNeg, negUsage.ErrorNeg, err = c.manager.EnsureSyncers(namespace, name, svcPortInfoMap)
		c.syncerMetrics.SetNegService(key, negUsage)
		return err
	}
	// do not need Neg
	c.logger.V(3).Info("Service does not need any NEG. Skipping", "service", key)
	c.syncerMetrics.DeleteNegService(key)
	// neg annotation is not found or NEG is not enabled
	c.manager.StopSyncer(namespace, name)

	// delete the annotation
	return c.syncNegStatusAnnotation(namespace, name, make(negtypes.PortInfoMap))
}

func (c *Controller) nodeTopologyWorker() {
	for {
		func() {
			key, quit := c.nodeTopologyQueue.Get()
			if quit {
				return
			}
			c.processNodeTopology()
			// Node Topology CR is a cluster-wide resource, so the key will
			// always be the same.
			// Done() ensures that if the item is updated while it is being
			// process, it will be re-added to the queue for re-processing,
			// so we won't miss any updates.
			c.nodeTopologyQueue.Done(key)
		}()
	}
}

// processNodeTopology signals all syncers to sync
func (c *Controller) processNodeTopology() {
	defer func() {
		now := c.syncTracker.Track()
		metrics.LastSyncTimestamp.Set(float64(now.UTC().UnixNano()))
	}()
	c.manager.SyncAllSyncers()
}

// mergeIngressPortInfo merges Ingress PortInfo into portInfoMap if the service has Enable Ingress annotation.
func (c *Controller) mergeIngressPortInfo(service *apiv1.Service, name types.NamespacedName, portInfoMap negtypes.PortInfoMap, networkInfo *network.NetworkInfo) error {
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
		ingressSvcPortTuples := gatherPortMappingUsedByIngress(ings, service, c.logger)
		ingressPortInfoMap := negtypes.NewPortInfoMap(name.Namespace, name.Name, ingressSvcPortTuples, c.namer, true, nil, networkInfo)
		if err := portInfoMap.Merge(ingressPortInfoMap); err != nil {
			return fmt.Errorf("failed to merge service ports referenced by ingress (%v): %w", ingressPortInfoMap, err)
		}
	}
	return nil
}

// mergeStandaloneNEGsPortInfo merge Standalone NEG PortInfo into portInfoMap
func (c *Controller) mergeStandaloneNEGsPortInfo(service *apiv1.Service, name types.NamespacedName, portInfoMap negtypes.PortInfoMap, negUsage *metricscollector.NegServiceState, networkInfo *network.NetworkInfo) error {
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

		exposedNegSvcPort, customNames, err := negServicePorts(negAnnotation, knowSvcPortSet)
		if err != nil {
			return err
		}

		if negAnnotation.NEGEnabledForIngress() && len(customNames) != 0 {
			return fmt.Errorf("configuration for negs in service (%s) is invalid, custom neg name cannot be used with ingress enabled", name.String())
		}
		negUsage.CustomNamedNeg = len(customNames)

		if err := portInfoMap.Merge(negtypes.NewPortInfoMap(name.Namespace, name.Name, exposedNegSvcPort, c.namer, true, customNames, networkInfo)); err != nil {
			return fmt.Errorf("failed to merge service ports exposed as standalone NEGs (%v) into ingress referenced service ports (%v): %w", exposedNegSvcPort, portInfoMap, err)
		}
	}

	return nil
}

// mergeVmIpNEGsPortInfo merges the PortInfo for ILB, multinet NetLB and NetLB V3 (variant with NEG default) services using GCE_VM_IP NEGs into portInfoMap
func (c *Controller) mergeVmIpNEGsPortInfo(service *apiv1.Service, name types.NamespacedName, portInfoMap negtypes.PortInfoMap, negUsage *metricscollector.NegServiceState, networkInfo *network.NetworkInfo) error {
	wantsILB, _ := annotations.WantsL4ILB(service)
	needsNEGForILB := c.runL4ForILB && wantsILB
	needsNEGForNetLB := c.netLBServiceNeedsNEG(service, networkInfo)
	if !needsNEGForILB && !needsNEGForNetLB {
		return nil
	}
	// Only process ILB services after L4 controller has marked it with v2 finalizer.
	if needsNEGForILB && !utils.HasL4ILBFinalizerV2(service) {
		msg := fmt.Sprintf("Ignoring ILB Service %s, namespace %s as it does not have the v2 finalizer", service.Name, service.Namespace)
		c.logger.Info(msg)
		c.recorder.Eventf(service, apiv1.EventTypeWarning, "ProcessServiceSkipped", msg)
		return nil
	}

	// Ignore services with LoadBalancerClass different than "networking.gke.io/l4-regional-external" or
	// "networking.gke.io/l4-regional-internal" used for L4 controllers that use GCE_VM_IP NEGs.
	if service.Spec.LoadBalancerClass != nil &&
		!annotations.HasLoadBalancerClass(service, annotations.RegionalExternalLoadBalancerClass) &&
		!annotations.HasLoadBalancerClass(service, annotations.RegionalInternalLoadBalancerClass) {
		msg := fmt.Sprintf("Ignoring Service %s, namespace %s as it uses a LoadBalancerClass %s", service.Name, service.Namespace, *service.Spec.LoadBalancerClass)
		c.logger.Info(msg)
		return nil
	}

	onlyLocal := helpers.RequestsOnlyLocalTraffic(service)
	// Update usage metrics.
	negUsage.VmIpNeg = metricscollector.NewVmIpNegType(onlyLocal)
	var l4LBType negtypes.L4LBType
	if needsNEGForILB {
		l4LBType = negtypes.L4InternalLB
	} else {
		l4LBType = negtypes.L4ExternalLB
	}

	return portInfoMap.Merge(negtypes.NewPortInfoMapForVMIPNEG(name.Namespace, name.Name, c.l4Namer, onlyLocal, networkInfo, l4LBType))
}

// netLBServiceNeedsNEG determines if NEGs need to be created for L4 NetLB.
// - service must be an L4 External Load Balancer service
// - service is a multinetwork service on a non default network OR NEGs are enabled and V3 finalizer is present.
// - service has the ExternalLoadBalancer class (these will always use NEGs).
// - service has the V3 finalizer
// otherwise the service does not need NEGs.
func (c *Controller) netLBServiceNeedsNEG(service *apiv1.Service, networkInfo *network.NetworkInfo) bool {
	wantsNetLB, _ := annotations.WantsL4NetLB(service)
	if !wantsNetLB {
		return false
	}
	if !networkInfo.IsDefault {
		return true
	}
	// The multinet check should be above because the runL4ForNetLB decides if NEGs
	// should be used for non multinet services.
	if !c.runL4ForNetLB {
		return false
	}
	if annotations.HasLoadBalancerClass(service, annotations.RegionalExternalLoadBalancerClass) {
		return true
	}
	if utils.HasL4NetLBFinalizerV3(service) {
		return true
	}
	return false
}

// mergeDefaultBackendServicePortInfoMap merge the PortInfoMap for the default backend service into portInfoMap
// The default backend service needs special handling since it is not explicitly referenced
// in the ingress spec.  It is either inferred and then managed by the controller, or
// it is passed to the controller via a command line flag.
// Additionally, supporting NEGs for default backends is only for L7-ILB
func (c *Controller) mergeDefaultBackendServicePortInfoMap(key string, service *apiv1.Service, portInfoMap negtypes.PortInfoMap, networkInfo *network.NetworkInfo) error {
	if c.defaultBackendService.ID.Service.String() != key {
		return nil
	}

	scanIngress := func(qualify func(*v1.Ingress) bool) error {
		for _, m := range c.ingressLister.List() {
			ing := *m.(*v1.Ingress)
			if qualify(&ing) && ing.Spec.DefaultBackend == nil {
				svcPortTupleSet := make(negtypes.SvcPortTupleSet)
				svcPortTupleSet.Insert(negtypes.SvcPortTuple{
					Name:       c.defaultBackendService.ID.Port.Name,
					Port:       c.defaultBackendService.Port,
					TargetPort: c.defaultBackendService.TargetPort.String(),
				})
				defaultServicePortInfoMap := negtypes.NewPortInfoMap(c.defaultBackendService.ID.Service.Namespace, c.defaultBackendService.ID.Service.Name, svcPortTupleSet, c.namer, false, nil, networkInfo)
				return portInfoMap.Merge(defaultServicePortInfoMap)
			}
		}
		return nil
	}

	// ILB always has neg enabled, regardless of neg annotation.
	if err := scanIngress(utils.IsGCEL7ILBIngress); err != nil {
		return err
	}
	if c.enableIngressRegionalExternal {
		// Regional XLB always has neg enabled, regardless of annotation.
		if err := scanIngress(utils.IsGCEL7XLBRegionalIngress); err != nil {
			return err
		}
	}

	// process default backend service for L7 XLB
	negAnnotation, foundNEGAnnotation, err := annotations.FromService(service).NEGAnnotation()
	if err != nil {
		return err
	}
	if !foundNEGAnnotation {
		return nil
	}
	if negAnnotation.Ingress == false {
		return nil
	}
	return scanIngress(utils.IsGCEIngress)
}

// syncNegStatusAnnotation syncs the neg status annotation
// it takes service namespace, name and the expected service ports for NEGs.
func (c *Controller) syncNegStatusAnnotation(namespace, name string, portMap negtypes.PortInfoMap) error {
	zones, err := c.zoneGetter.ListZones(negtypes.NodeFilterForEndpointCalculatorMode(portMap.EndpointsCalculatorMode()), c.logger)
	if err != nil {
		return err
	}
	obj, exists, err := c.serviceLister.GetByKey(getServiceKey(namespace, name).Key())
	if err != nil {
		return err
	}
	if !exists {
		// Service no longer exists so doesn't require any update.
		return nil
	}
	service, ok := obj.(*apiv1.Service)
	if !ok {
		return fmt.Errorf("cannot convert obj to Service; obj=%T", obj)
	}

	// Remove NEG Status Annotation when no NEG is needed
	if len(portMap) == 0 {
		if _, ok := service.Annotations[annotations.NEGStatusKey]; ok {
			newSvcObjectMeta := service.ObjectMeta.DeepCopy()
			delete(newSvcObjectMeta.Annotations, annotations.NEGStatusKey)
			c.logger.V(2).Info("Removing NEG status annotation from service", "service", klog.KRef(namespace, name))
			return patch.PatchServiceObjectMetadata(c.client.CoreV1(), service, *newSvcObjectMeta)
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
	newSvcObjectMeta := service.ObjectMeta.DeepCopy()
	// If enableCSM=true, it's possible a service having nil Annotations.
	if newSvcObjectMeta.Annotations == nil {
		newSvcObjectMeta.Annotations = make(map[string]string)
	}
	newSvcObjectMeta.Annotations[annotations.NEGStatusKey] = annotation
	c.logger.V(2).Info("Updating NEG visibility annotation on service", "annotation", annotation, "service", klog.KRef(namespace, name))
	return patch.PatchServiceObjectMetadata(c.client.CoreV1(), service, *newSvcObjectMeta)
}

func (c *Controller) handleErr(err error, key interface{}) {
	if err == nil {
		c.serviceQueue.Forget(key)
		return
	}

	msg := fmt.Sprintf("error processing service %q: %v", key, err)
	c.logger.Error(nil, msg)
	if service, exists, err := c.serviceLister.GetByKey(key.(string)); err != nil {
		c.logger.Error(err, "Failed to retrieve service from store", "service", key.(string))
		c.negMetrics.PublishNegControllerErrorCountMetrics(err, true)
	} else if exists {
		c.recorder.Eventf(service.(*apiv1.Service), apiv1.EventTypeWarning, "ProcessServiceFailed", msg)
	}
	c.serviceQueue.AddRateLimited(key)
}

func (c *Controller) enqueueEndpointSlice(obj interface{}) {
	endpointSlice, ok := obj.(*discovery.EndpointSlice)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			c.logger.Error(nil, "Unexpected object type, expected cache.DeletedFinalStateUnknown", "objectTypeFound", fmt.Sprintf("%T", obj))
			return
		}
		if endpointSlice, ok = tombstone.Obj.(*discovery.EndpointSlice); !ok {
			c.logger.Error(nil, "Unexpected tombstone object, expected *discovery.EndpointSlice", "objectTypeFound", fmt.Sprintf("%T", obj))
			return
		}
	}
	key, err := endpointslices.EndpointSlicesServiceKey(endpointSlice)
	if err != nil {
		c.logger.Error(err, "Failed to find a service label inside endpoint slice", "endpointSlice", klog.KObj(endpointSlice))
		c.negMetrics.PublishNegControllerErrorCountMetrics(err, true)
		return
	}
	c.logger.V(3).Info("Adding EndpointSlice to endpointQueue for processing", "endpointSlice", key)
	c.endpointQueue.Add(key)
}

func (c *Controller) enqueueNode(obj interface{}) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		c.logger.Error(err, "Failed to generate node key")
		c.negMetrics.PublishNegControllerErrorCountMetrics(err, true)
		return
	}
	c.logger.V(3).Info("Adding Node to nodeQueue for processing", "node", key)
	c.nodeQueue.Add(key)
}

func (c *Controller) enqueueService(obj interface{}) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		c.logger.Error(err, "Failed to generate service key")
		c.negMetrics.PublishNegControllerErrorCountMetrics(err, true)
		return
	}
	c.logger.V(3).Info("Adding Service to serviceQueue for processing", "service", key)
	c.serviceQueue.Add(key)
}

func (c *Controller) enqueueIngressServices(ing *v1.Ingress) {
	// enqueue services referenced by ingress
	keys := gatherIngressServiceKeys(ing)
	for key := range keys {
		c.enqueueService(cache.ExplicitKey(key))
	}

	// enqueue default backend service
	if ing.Spec.DefaultBackend == nil {
		c.enqueueService(cache.ExplicitKey(c.defaultBackendService.ID.Service.String()))
	}
}

func (c *Controller) enqueueNodeTopology(obj interface{}) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		c.logger.Error(err, "Failed to generate Node Topology key")
		c.negMetrics.PublishNegControllerErrorCountMetrics(err, true)
		return
	}
	c.logger.V(3).Info("Adding NodeTopology to nodeTopologyQueue for processing", "nodeTopology", key)
	c.nodeTopologyQueue.Add(key)
}

func (c *Controller) gc() {
	if err := c.manager.GC(); err != nil {
		c.logger.Error(err, "NEG controller garbage collection failed")
		c.negMetrics.PublishNegControllerErrorCountMetrics(err, true)
	}
}

// gatherPortMappingUsedByIngress returns a map containing port:targetport
// of all service ports of the service that are referenced by ingresses
func gatherPortMappingUsedByIngress(ings []v1.Ingress, svc *apiv1.Service, logger klog.Logger) negtypes.SvcPortTupleSet {
	ingressSvcPortTuples := make(negtypes.SvcPortTupleSet)
	for _, ing := range ings {
		if utils.IsGLBCIngress(&ing) {
			utils.TraverseIngressBackends(&ing, func(id utils.ServicePortID) bool {
				if id.Service.Name == svc.Name && id.Service.Namespace == svc.Namespace {
					servicePort := translator.ServicePort(*svc, id.Port)
					if servicePort == nil {
						logger.Error(nil, "Port not found in service", "port", fmt.Sprintf("%+v", id.Port), "service", id.Service.String())
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
func gatherIngressServiceKeys(ing *v1.Ingress) sets.String {
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

func getIngressServicesFromStore(store cache.Store, svc *apiv1.Service) (ings []v1.Ingress) {
	for _, m := range store.List() {
		ing := *m.(*v1.Ingress)
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

// isSingleStackIPv6Service returns true if the given service is a single stack ipv6 service
func isSingleStackIPv6Service(service *apiv1.Service) bool {
	if service.Spec.IPFamilyPolicy != nil && *service.Spec.IPFamilyPolicy != apiv1.IPFamilyPolicySingleStack {
		return false
	}
	if len(service.Spec.IPFamilies) == 1 && service.Spec.IPFamilies[0] == apiv1.IPv6Protocol {
		return true
	}
	return false
}
