// l4lb/l4controller.go
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

package l4lb

import (
	"fmt"
	"math/rand"
	"reflect"
	"strings"
	"time"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/backends"
	"k8s.io/ingress-gce/pkg/common/operator"
	"k8s.io/ingress-gce/pkg/context"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/forwardingrules"
	"k8s.io/ingress-gce/pkg/l4lb/metrics"
	"k8s.io/ingress-gce/pkg/loadbalancers"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/ingress-gce/pkg/network"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/common"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/ingress-gce/pkg/utils/zonegetter"
	"k8s.io/klog/v2"
)

const (
	// The max tolerated delay between update being enqueued and sync being invoked.
	enqueueToSyncDelayThreshold  = 15 * time.Minute
	L4ILBControllerName          = "l4-ilb-subsetting-controller"
	l4ILBDualStackControllerName = "l4-ilb-dualstack-controller"
)

// L4Controller manages the create/update delete of all L4 Internal LoadBalancer services.
type L4Controller struct {
	ctx *context.ControllerContext
	// kubeClient, needed for attaching finalizer
	client                   kubernetes.Interface
	svcQueue                 utils.TaskQueue
	numWorkers               int
	networkLister            cache.Indexer
	gkeNetworkParamSetLister cache.Indexer
	networkResolver          network.Resolver
	stopCh                   <-chan struct{}
	// needed for listing the zones in the cluster.
	zoneGetter *zonegetter.ZoneGetter
	// needed for linking the NEG with the backend service for each ILB service.
	NegLinker   backends.Linker
	backendPool *backends.Pool
	namer       namer.L4ResourcesNamer
	// enqueueTracker tracks the latest time an update was enqueued
	enqueueTracker utils.TimeTracker
	// syncTracker tracks the latest time an enqueued service was synced
	syncTracker     utils.TimeTracker
	forwardingRules ForwardingRulesGetter
	enableDualStack bool

	hasSynced func() bool

	serviceVersions *serviceVersionsTracker

	logger klog.Logger
}

// NewILBController creates a new instance of the L4 ILB controller.
func NewILBController(ctx *context.ControllerContext, stopCh <-chan struct{}, logger klog.Logger) *L4Controller {
	logger = logger.WithName("L4Controller")
	if ctx.NumL4Workers <= 0 {
		logger.Info("L4 Internal LB Service worker count has not been set, setting to 1")
		ctx.NumL4Workers = 1
	}
	l4c := &L4Controller{
		ctx:             ctx,
		client:          ctx.KubeClient,
		stopCh:          stopCh,
		numWorkers:      ctx.NumL4Workers,
		namer:           ctx.L4Namer,
		zoneGetter:      ctx.ZoneGetter,
		forwardingRules: forwardingrules.New(ctx.Cloud, meta.VersionGA, meta.Regional, logger),
		enableDualStack: ctx.EnableL4ILBDualStack,
		serviceVersions: NewServiceVersionsTracker(),
		logger:          logger,
		hasSynced:       ctx.HasSynced,
	}
	l4c.backendPool = backends.NewPool(ctx.Cloud, l4c.namer)
	l4c.NegLinker = backends.NewNEGLinker(l4c.backendPool, negtypes.NewAdapter(ctx.Cloud), ctx.Cloud, ctx.SvcNegInformer.GetIndexer(), logger)

	l4c.svcQueue = utils.NewPeriodicTaskQueueWithMultipleWorkers("l4", "services", l4c.numWorkers, l4c.syncWrapper, logger)

	if ctx.NetworkInformer != nil {
		l4c.networkLister = ctx.NetworkInformer.GetIndexer()
	}
	if ctx.GKENetworkParamsInformer != nil {
		l4c.gkeNetworkParamSetLister = ctx.GKENetworkParamsInformer.GetIndexer()
	}

	// The following adapter will use Network Selflink as Network Url instead of the NetworkUrl itself.
	// Network Selflink is always composed by the network name even if the cluster was initialized with Network Id.
	// All the components created from it will be consistent and always use the Url with network name and not the url with netowork Id
	adapter, err := network.NewAdapterNetworkSelfLink(ctx.Cloud)
	if err != nil {
		logger.Error(err, "Failed to create network adapter with SelfLink")
		// if it was not possible to retrieve network information use standard context as cloud network provider
		adapter = ctx.Cloud
	}

	l4c.networkResolver = network.NewNetworksResolver(l4c.networkLister, l4c.gkeNetworkParamSetLister, adapter, ctx.EnableMultinetworking, logger)
	ctx.ServiceInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			addSvc := obj.(*v1.Service)
			svcKey := utils.ServiceKeyFunc(addSvc.Namespace, addSvc.Name)
			needsILB, svcType := annotations.WantsL4ILB(addSvc)
			svcLogger := logger.WithValues("serviceKey", svcKey)
			// Check for deletion since updates or deletes show up as Add when controller restarts.
			if needsILB || l4c.needsDeletion(addSvc) {
				svcLogger.V(3).Info("ILB Service added, enqueuing")
				l4c.ctx.Recorder(addSvc.Namespace).Eventf(addSvc, v1.EventTypeNormal, "ADD", svcKey)
				l4c.serviceVersions.SetLastUpdateSeen(svcKey, addSvc.ResourceVersion, svcLogger)
				l4c.svcQueue.Enqueue(addSvc)
				l4c.enqueueTracker.Track()
			} else {
				// We already know that LoadBalancerClass is different than "networking.gke.io/l4-regional-internal"
				if addSvc.Spec.LoadBalancerClass != nil {
					svcLogger.V(4).Info("Ignoring service managed by another controller", "serviceLoadBalancerClass", *addSvc.Spec.LoadBalancerClass)
				} else {
					svcLogger.V(4).Info("Ignoring add for non-lb service", "serviceType", svcType)
				}
			}
		},
		// Deletes will be handled in the Update when the deletion timestamp is set.
		UpdateFunc: func(old, cur interface{}) {
			curSvc := cur.(*v1.Service)
			svcKey := utils.ServiceKeyFunc(curSvc.Namespace, curSvc.Name)
			oldSvc := old.(*v1.Service)
			svcLogger := logger.WithValues("serviceKey", svcKey)
			needsUpdate := l4c.needsUpdate(oldSvc, curSvc)
			needsDeletion := l4c.needsDeletion(curSvc)
			if needsUpdate || needsDeletion {
				svcLogger.V(3).Info("Service changed, enqueuing", "needsUpdate", needsUpdate, "needsDeletion", needsDeletion)
				l4c.serviceVersions.SetLastUpdateSeen(svcKey, curSvc.ResourceVersion, svcLogger)
				l4c.svcQueue.Enqueue(curSvc)
				l4c.enqueueTracker.Track()
				return
			}
			// Enqueue ILB services periodically for reasserting that resources exist.
			needsILB, _ := annotations.WantsL4ILB(curSvc)
			if needsILB && reflect.DeepEqual(old, cur) {
				// this will happen when informers run a resync on all the existing services even when the object is
				// not modified.
				svcLogger.V(3).Info("Periodic enqueueing of service")
				l4c.svcQueue.Enqueue(curSvc)
				l4c.enqueueTracker.Track()
			} else if needsILB {
				l4c.serviceVersions.SetLastIgnored(svcKey, curSvc.ResourceVersion, svcLogger)
			}
		},
	})

	enableMultiSubnetClusterPhase1 := flags.F.EnableMultiSubnetClusterPhase1
	if enableMultiSubnetClusterPhase1 {
		ctx.SvcNegInformer.AddEventHandler(&svcNEGEventHandler{
			ServiceInformer: ctx.ServiceInformer,
			svcQueue:        l4c.svcQueue,
			svcFilterFunc:   isSubsettingILBService,
			logger:          logger,
		})
		logger.V(3).Info("set up SvcNegInformer event handlers")
	}

	if flags.F.ManageL4LBLogging {
		ctx.ConfigMapInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				configMap, ok := obj.(*v1.ConfigMap)
				if ok {
					l4c.enqueueServicesReferencingConfigMap(configMap)
				}
			},
			UpdateFunc: func(_, obj interface{}) {
				configMap, ok := obj.(*v1.ConfigMap)
				if ok {
					l4c.enqueueServicesReferencingConfigMap(configMap)
				}
			},
			DeleteFunc: func(obj interface{}) {
				configMap, ok := obj.(*v1.ConfigMap)
				if ok {
					l4c.enqueueServicesReferencingConfigMap(configMap)
				}
			},
		})
	}

	return l4c
}

func (l4c *L4Controller) enqueueServicesReferencingConfigMap(configMap *v1.ConfigMap) {
	services := operator.Services(l4c.ctx.Services().List(), l4c.logger).ReferencesL4LoggingConfigMap(configMap).AsList()
	for _, svc := range services {
		svcKey := utils.ServiceKeyFunc(svc.Namespace, svc.Name)
		svcLogger := l4c.logger.WithValues("serviceKey", svcKey)
		if l4c.shouldProcessService(svc, svcLogger) {
			l4c.serviceVersions.SetLastUpdateSeen(svcKey, svc.ResourceVersion, svcLogger)
			l4c.svcQueue.Enqueue(svc)
		}
	}
	l4c.enqueueTracker.Track()
}

func (l4c *L4Controller) SystemHealth() error {
	lastEnqueueTime := l4c.enqueueTracker.Get()
	lastSyncTime := l4c.syncTracker.Get()
	// if lastEnqueue time is more than 30 minutes before the last sync time, the controller is falling behind.
	// This indicates that the controller was stuck handling a previous update, or sync function did not get invoked.
	syncTimeLatest := lastEnqueueTime.Add(enqueueToSyncDelayThreshold)
	controllerHealth := metrics.ControllerHealthyStatus
	if lastSyncTime.After(syncTimeLatest) {
		msg := fmt.Sprintf("L4 ILB Sync happened at time %v, %v after enqueue time, last enqueue time %v, threshold is %v", lastSyncTime, lastSyncTime.Sub(lastEnqueueTime), lastEnqueueTime, enqueueToSyncDelayThreshold)
		// Log here, context/http handler do no log the error.
		l4c.logger.Error(nil, msg)
		metrics.PublishL4FailedHealthCheckCount(L4ILBControllerName)
		controllerHealth = metrics.ControllerUnhealthyStatus
		// Reset trackers. Otherwise, if there is nothing in the queue then it will report the FailedHealthCheckCount every time the checkHealth is called
		// If checkHealth returned error (as it is meant to) then container would be restarted and trackers would be reset either
		l4c.enqueueTracker.Track()
		l4c.syncTracker.Track()
	}
	if l4c.enableDualStack {
		metrics.PublishL4ControllerHealthCheckStatus(l4ILBDualStackControllerName, controllerHealth)
	}
	return nil
}

func (l4c *L4Controller) Run() {
	defer l4c.shutdown()

	wait.PollUntil(5*time.Second, func() (bool, error) {
		l4c.logger.V(2).Info("Waiting for initial cache sync before starting L4 Controller")
		return l4c.hasSynced(), nil
	}, l4c.stopCh)

	l4c.logger.Info("Running L4 Controller", "numWorkers", l4c.numWorkers)
	l4c.svcQueue.Run()
	<-l4c.stopCh
}

// This should only be called when the process is being terminated.
func (l4c *L4Controller) shutdown() {
	l4c.logger.Info("Shutting down L4 Service Controller")
	l4c.svcQueue.Shutdown()
}

// shouldProcessService returns if the given LoadBalancer service should be processed by this controller.
// If the service has either the v1 finalizer or the forwarding rule created by v1 implementation(service controller),
// the subsetting controller will not process it. Processing it will fail forwarding rule creation with the same IP anyway.
// This check prevents processing of v1-implemented services whose finalizer field got wiped out.
func (l4c *L4Controller) shouldProcessService(service *v1.Service, svcLogger klog.Logger) bool {
	// Ignore services with LoadBalancerClass different than "networking.gke.io/l4-regional-internal" used for this controller.
	// LoadBalancerClass can't be updated (see the field API doc) so we don't need to worry about cleaning up services that changed the class.
	// Services with a different loadBalancerClass shouldn't even be added to the queue
	if service.Spec.LoadBalancerClass != nil {
		if annotations.HasLoadBalancerClass(service, annotations.RegionalInternalLoadBalancerClass) {
			return true
		} else {
			svcLogger.Info("Ignoring service managed by another controller", "serviceLoadBalancerClass", *service.Spec.LoadBalancerClass)
			return false
		}
	}

	// Prevent race condition with legacy controller
	service = l4c.handleCreationRace(service, svcLogger)
	if service == nil {
		return false
	}

	// skip services that are being handled by the legacy service controller.
	if utils.IsLegacyL4ILBService(service) {
		svcLogger.Info("Ignoring update for service managed by service controller, has finalizer v1")
		return false
	}
	frName := utils.LegacyForwardingRuleName(service)
	frLogger := svcLogger.WithValues("forwardingRule", frName)
	// Processing should continue if an external forwarding rule exists. This can happen if the service is transitioning from External to Internal.
	// The external forwarding rule might not be deleted by the time this controller starts processing the service.
	fr, err := l4c.forwardingRules.Get(frName)
	if utils.IsNotFoundError(err) {
		frLogger.Info("Legacy ForwardingRule not found, start processing service")
		return true
	}
	if err != nil {
		frLogger.Error(err, "Error getting l4 forwarding rule. Ignore update until forwarding rule can be read.")
		return false
	}
	if fr != nil && fr.LoadBalancingScheme == string(cloud.SchemeInternal) {
		frLogger.Info("Ignoring update for service as it contains legacy forwarding rule")
		return false
	}
	return true
}

// processServiceCreateOrUpdate ensures load balancer resources for the given service, as needed.
// Returns an error if processing the service update failed.
func (l4c *L4Controller) processServiceCreateOrUpdate(service *v1.Service, svcLogger klog.Logger) *loadbalancers.L4ILBSyncResult {
	if !l4c.shouldProcessService(service, svcLogger) {
		return nil
	}

	startTime := time.Now()
	svcLogger.Info("Syncing L4 ILB service")
	defer func() {
		svcLogger.Info("Finished syncing L4 ILB service", "timeTaken", time.Since(startTime))
	}()

	// Ensure v2 finalizer
	if err := common.EnsureServiceFinalizer(service, common.ILBFinalizerV2, l4c.ctx.KubeClient, svcLogger); err != nil {
		return &loadbalancers.L4ILBSyncResult{Error: fmt.Errorf("Failed to attach finalizer to service %s/%s, err %v", service.Namespace, service.Name, err)}
	}
	nodes, err := l4c.zoneGetter.ListNodes(zonegetter.CandidateNodesFilter, svcLogger)
	if err != nil {
		return &loadbalancers.L4ILBSyncResult{Error: err}
	}
	// Use the same function for both create and updates. If controller crashes and restarts,
	// all existing services will show up as Service Adds.
	l4ilbParams := &loadbalancers.L4ILBParams{
		Service:                          service,
		Cloud:                            l4c.ctx.Cloud,
		Namer:                            l4c.namer,
		Recorder:                         l4c.ctx.Recorder(service.Namespace),
		DualStackEnabled:                 l4c.enableDualStack,
		NetworkResolver:                  l4c.networkResolver,
		EnableWeightedLB:                 l4c.ctx.EnableWeightedL4ILB,
		DisableNodesFirewallProvisioning: l4c.ctx.DisableL4LBFirewall,
		EnableMixedProtocol:              l4c.ctx.EnableL4ILBMixedProtocol,
		EnableZonalAffinity:              l4c.ctx.EnableL4ILBZonalAffinity,
	}
	if l4c.ctx.ConfigMapInformer != nil {
		l4ilbParams.ConfigMapLister = l4c.ctx.ConfigMapInformer.GetIndexer()
	}

	l4 := loadbalancers.NewL4Handler(l4ilbParams, svcLogger)
	syncResult := l4.EnsureInternalLoadBalancer(utils.GetNodeNames(nodes), service)
	// syncResult will not be nil
	if syncResult.Error != nil {
		l4c.ctx.Recorder(service.Namespace).Eventf(service, v1.EventTypeWarning, "SyncLoadBalancerFailed",
			"Error syncing load balancer: %v", syncResult.Error)
		if loadbalancers.IsUserError(syncResult.Error) {
			syncResult.MetricsLegacyState.IsUserError = true
			if l4c.enableDualStack {
				syncResult.MetricsState.Status = metrics.StatusUserError
			}
		}
		return syncResult
	}
	if syncResult.Status == nil {
		l4c.ctx.Recorder(service.Namespace).Eventf(service, v1.EventTypeWarning, "SyncLoadBalancerFailed",
			"Empty status returned, even though there were no errors")
		syncResult.Error = fmt.Errorf("service status returned by EnsureInternalLoadBalancer for %s is nil",
			l4.NamespacedName.String())
		return syncResult
	}
	if err = l4c.linkNEG(l4, svcLogger); err != nil {
		l4c.ctx.Recorder(service.Namespace).Eventf(service, v1.EventTypeWarning, "SyncLoadBalancerFailed",
			"Failed to link NEG with Backend Service for load balancer, err: %v", err)
		syncResult.Error = err
		return syncResult
	}
	err = updateServiceStatus(l4c.ctx, service, syncResult.Status, svcLogger)
	if err != nil {
		l4c.ctx.Recorder(service.Namespace).Eventf(service, v1.EventTypeWarning, "SyncLoadBalancerFailed",
			"Error updating load balancer status: %v", err)
		syncResult.Error = err
		return syncResult
	}
	if l4c.enableDualStack {
		l4c.emitEnsuredDualStackEvent(service)
		if err = updateL4DualStackResourcesAnnotations(l4c.ctx, service, syncResult.Annotations, svcLogger); err != nil {
			l4c.ctx.Recorder(service.Namespace).Eventf(service, v1.EventTypeWarning, "SyncLoadBalancerFailed",
				"Failed to update Dual Stack annotations for load balancer, err: %v", err)
			syncResult.Error = fmt.Errorf("failed to set Dual Stack resource annotations, err: %w", err)
			return syncResult
		}
	} else {
		l4c.ctx.Recorder(service.Namespace).Eventf(service, v1.EventTypeNormal, "SyncLoadBalancerSuccessful",
			"Successfully ensured load balancer resources")
		if err = updateL4ResourcesAnnotations(l4c.ctx, service, syncResult.Annotations, svcLogger); err != nil {
			l4c.ctx.Recorder(service.Namespace).Eventf(service, v1.EventTypeWarning, "SyncLoadBalancerFailed",
				"Failed to update annotations for load balancer, err: %v", err)
			syncResult.Error = fmt.Errorf("failed to set resource annotations, err: %w", err)
			return syncResult
		}
	}
	err = ensureServiceLoadBalancerStatusCR(l4c.ctx, service, syncResult.GCEResourceURLs, svcLogger)
	if err != nil {
		l4c.ctx.Recorder(service.Namespace).Eventf(service, v1.EventTypeWarning, "SyncLoadBalancerFailed",
			"Failed to update ServiceLoadBalancerStatus CR, err: %v", err)
		syncResult.Error = fmt.Errorf("failed to ensure ServiceLoadBalancerStatus CR, err: %w", err)
		return syncResult
	}

	return syncResult
}

func (l4c *L4Controller) emitEnsuredDualStackEvent(service *v1.Service) {
	var ipFamilies []string
	for _, ipFamily := range service.Spec.IPFamilies {
		ipFamilies = append(ipFamilies, string(ipFamily))
	}
	l4c.ctx.Recorder(service.Namespace).Eventf(service, v1.EventTypeNormal, "SyncLoadBalancerSuccessful",
		"Successfully ensured %v load balancer resources", strings.Join(ipFamilies, " "))
}

func (l4c *L4Controller) processServiceDeletion(key string, svc *v1.Service, svcLogger klog.Logger) *loadbalancers.L4ILBSyncResult {
	startTime := time.Now()
	svcLogger.Info("Deleting L4 ILB service")
	defer func() {
		svcLogger.Info("Finished deleting L4 ILB service", "timeTaken", time.Since(startTime))
	}()

	l4ilbParams := &loadbalancers.L4ILBParams{
		Service:                          svc,
		Cloud:                            l4c.ctx.Cloud,
		Namer:                            l4c.namer,
		Recorder:                         l4c.ctx.Recorder(svc.Namespace),
		DualStackEnabled:                 l4c.enableDualStack,
		NetworkResolver:                  l4c.networkResolver,
		EnableWeightedLB:                 l4c.ctx.EnableWeightedL4ILB,
		DisableNodesFirewallProvisioning: l4c.ctx.DisableL4LBFirewall,
		EnableMixedProtocol:              l4c.ctx.EnableL4ILBMixedProtocol,
		EnableZonalAffinity:              l4c.ctx.EnableL4ILBZonalAffinity,
	}
	if l4c.ctx.ConfigMapInformer != nil {
		l4ilbParams.ConfigMapLister = l4c.ctx.ConfigMapInformer.GetIndexer()
	}

	l4 := loadbalancers.NewL4Handler(l4ilbParams, svcLogger)
	l4c.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeNormal, "DeletingLoadBalancer", "Deleting load balancer for %s", key)
	result := l4.EnsureInternalLoadBalancerDeleted(svc)
	if result.Error != nil {
		l4c.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeWarning, "DeleteLoadBalancerFailed", "Error deleting load balancer: %v", result.Error)
		return result
	}
	// Reset the loadbalancer status first, before resetting annotations.
	// Other controllers(like service-controller) will process the service update if annotations change, but will ignore a service status change.
	// Following this order avoids a race condition when a service is changed from LoadBalancer type Internal to External.
	if err := updateServiceStatus(l4c.ctx, svc, &v1.LoadBalancerStatus{}, svcLogger); err != nil {
		l4c.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeWarning, "DeleteLoadBalancer",
			"Error resetting load balancer status to empty: %v", err)
		result.Error = fmt.Errorf("failed to reset ILB status, err: %w", err)
		return result
	}
	// Also remove any ILB annotations from the service metadata
	if l4c.enableDualStack {
		if err := updateL4DualStackResourcesAnnotations(l4c.ctx, svc, nil, svcLogger); err != nil {
			l4c.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeWarning, "DeleteLoadBalancer",
				"Error resetting DualStack resource annotations for load balancer: %v", err)
			result.Error = fmt.Errorf("failed to reset DualStack resource annotations, err: %w", err)
			return result
		}
	} else {
		if err := updateL4ResourcesAnnotations(l4c.ctx, svc, nil, svcLogger); err != nil {
			l4c.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeWarning, "DeleteLoadBalancer",
				"Error resetting resource annotations for load balancer: %v", err)
			result.Error = fmt.Errorf("failed to reset resource annotations, err: %w", err)
			return result
		}
	}

	if err := common.EnsureDeleteServiceFinalizer(svc, common.ILBFinalizerV2, l4c.ctx.KubeClient, svcLogger); err != nil {
		l4c.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeWarning, "DeleteLoadBalancerFailed",
			"Error removing finalizer from load balancer: %v", err)
		result.Error = fmt.Errorf("failed to remove ILB finalizer, err: %w", err)
		return result
	}
	l4c.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeNormal, "DeletedLoadBalancer", "Deleted load balancer")
	return result
}

// linkNEG associates the NEG to the backendService for the given L4 ILB service.
func (l4c *L4Controller) linkNEG(l4 *loadbalancers.L4, svcLogger klog.Logger) error {
	// link neg to backend service
	zones, err := l4c.zoneGetter.ListZones(zonegetter.CandidateAndUnreadyNodesFilter, svcLogger)
	if err != nil {
		return nil
	}
	var groupKeys []backends.GroupKey
	for _, zone := range zones {
		groupKeys = append(groupKeys, backends.GroupKey{Zone: zone})
	}
	return l4c.NegLinker.Link(l4.ServicePort, groupKeys)
}

func (l4c *L4Controller) syncWrapper(key string) (err error) {
	syncTrackingId := rand.Int31()
	svcLogger := l4c.logger.WithValues("serviceKey", key, "syncId", syncTrackingId)

	defer func() {
		if r := recover(); r != nil {
			errMessage := fmt.Sprintf("Panic in L4 ILB sync worker goroutine: %v", r)
			svcLogger.Error(nil, errMessage)
			metrics.PublishL4ControllerPanicCount(L4ILBControllerName)
			err = fmt.Errorf("%s", errMessage)
		}
	}()
	syncErr := l4c.sync(key, svcLogger)
	return skipUserError(syncErr, svcLogger)
}

func (l4c *L4Controller) sync(key string, svcLogger klog.Logger) error {
	l4c.syncTracker.Track()
	metrics.PublishL4controllerLastSyncTime(L4ILBControllerName)

	svc, exists, err := l4c.ctx.Services().GetByKey(key)
	if err != nil {
		return fmt.Errorf("Failed to lookup service for key %s : %w", key, err)
	}
	if !exists || svc == nil {
		// The service will not exist if its resources and finalizer are handled by the legacy service controller and
		// it has been deleted. As long as the V2 finalizer is present, the service will not be deleted by apiserver.
		svcLogger.V(3).Info("Ignoring delete of service not managed by L4 controller")
		return nil
	}

	if l4c.ctx.ReadOnlyMode {
		l4c.serviceVersions.SetProcessed(key, svc.ResourceVersion, true, false, svcLogger)
		svcLogger.Info("Skipping syncing L4 ILB service since the controller is in read-only mode", "service", svc.Name)
		return nil
	}

	isResync := l4c.serviceVersions.IsResync(key, svc.ResourceVersion, svcLogger)
	svcLogger.V(2).Info("Processing update operation for service", "resync", isResync, "resourceVersion", svc.ResourceVersion)
	namespacedName := types.NamespacedName{Name: svc.Name, Namespace: svc.Namespace}.String()
	var result *loadbalancers.L4ILBSyncResult
	if l4c.needsDeletion(svc) {
		svcLogger.V(2).Info("Deleting ILB resources for service managed by L4 controller")
		result = l4c.processServiceDeletion(key, svc, svcLogger)
		if result == nil {
			return nil
		}
		l4c.serviceVersions.Delete(key)
		l4c.publishMetrics(result, namespacedName, false, svcLogger)
		return skipUserError(result.Error, svcLogger)
	}
	// Check again here, to avoid time-of check, time-of-use race. A service queued by informer could have changed, no
	// longer needing an ILB.
	if wantsILB, _ := annotations.WantsL4ILB(svc); wantsILB {
		svcLogger.V(2).Info("Ensuring ILB resources for service managed by L4 controller")
		result = l4c.processServiceCreateOrUpdate(svc, svcLogger)
		if result == nil {
			// result will be nil if the service was ignored(due to presence of service controller finalizer).
			return nil
		}
		svcLogger.V(3).Info("Resources modified in the sync", "modifiedResources", result.ResourceUpdates.String(), "wasResync", isResync)
		if isResync {
			if result.ResourceUpdates.WereAnyResourcesModified() {
				svcLogger.V(3).Error(nil, "Resources were modified but this was not expected for a resync.", "modifiedResources", result.ResourceUpdates.String())
			}
		}
		l4c.publishMetrics(result, namespacedName, isResync, svcLogger)
		l4c.serviceVersions.SetProcessed(key, svc.ResourceVersion, result.Error == nil, isResync, svcLogger)
		return skipUserError(result.Error, svcLogger)
	}
	svcLogger.V(3).Info("Ignoring sync of service, neither delete nor ensure needed.")
	return nil
}

func (l4c *L4Controller) needsDeletion(svc *v1.Service) bool {
	if !utils.IsSubsettingL4ILBService(svc) {
		return false
	}
	if common.IsDeletionCandidateForGivenFinalizer(svc.ObjectMeta, common.ILBFinalizerV2) {
		return true
	}
	needsILB, _ := annotations.WantsL4ILB(svc)
	return !needsILB
}

// needsUpdate checks if load balancer needs to be updated due to change in attributes.
func (l4c *L4Controller) needsUpdate(oldService *v1.Service, newService *v1.Service) bool {
	// Ignore services not handled by this controller.
	// LoadBalancerClass can't be updated so we know if this controller should not process the ILB.
	// We don't need to clean any resources if service is controlled by another controller.
	if newService.Spec.LoadBalancerClass != nil && !annotations.HasLoadBalancerClass(newService, annotations.RegionalInternalLoadBalancerClass) {
		return false
	}

	warnL4FinalizerRemoved(l4c.ctx, oldService, newService)

	oldSvcWantsILB, oldType := annotations.WantsL4ILB(oldService)
	newSvcWantsILB, newType := annotations.WantsL4ILB(newService)
	recorder := l4c.ctx.Recorder(oldService.Namespace)
	if oldSvcWantsILB != newSvcWantsILB {
		recorder.Eventf(newService, v1.EventTypeNormal, "Type", "%v -> %v", oldType, newType)
		return true
	}

	if !newSvcWantsILB && !oldSvcWantsILB {
		// Ignore any other changes if both the previous and new service do not need ILB.
		return false
	}

	if !reflect.DeepEqual(oldService.Spec.LoadBalancerSourceRanges, newService.Spec.LoadBalancerSourceRanges) {
		recorder.Eventf(newService, v1.EventTypeNormal, "LoadBalancerSourceRanges", "%v -> %v",
			oldService.Spec.LoadBalancerSourceRanges, newService.Spec.LoadBalancerSourceRanges)
		return true
	}

	if !portsEqualForLBService(oldService, newService) || oldService.Spec.SessionAffinity != newService.Spec.SessionAffinity {
		recorder.Eventf(newService, v1.EventTypeNormal, "Ports/SessionAffinity", "Ports %v, SessionAffinity %v -> Ports %v, SessionAffinity  %v",
			oldService.Spec.Ports, oldService.Spec.SessionAffinity, newService.Spec.Ports, newService.Spec.SessionAffinity)
		return true
	}

	if !reflect.DeepEqual(oldService.Spec.SessionAffinityConfig, newService.Spec.SessionAffinityConfig) {
		recorder.Eventf(newService, v1.EventTypeNormal, "SessionAffinityConfig", "%v -> %v",
			oldService.Spec.SessionAffinityConfig, newService.Spec.SessionAffinityConfig)
		return true
	}
	if oldService.Spec.LoadBalancerIP != newService.Spec.LoadBalancerIP {
		recorder.Eventf(newService, v1.EventTypeNormal, "LoadbalancerIP", "%v -> %v",
			oldService.Spec.LoadBalancerIP, newService.Spec.LoadBalancerIP)
		return true
	}
	if len(oldService.Spec.ExternalIPs) != len(newService.Spec.ExternalIPs) {
		recorder.Eventf(newService, v1.EventTypeNormal, "ExternalIP", "Count: %v -> %v",
			len(oldService.Spec.ExternalIPs), len(newService.Spec.ExternalIPs))
		return true
	}
	for i := range oldService.Spec.ExternalIPs {
		if oldService.Spec.ExternalIPs[i] != newService.Spec.ExternalIPs[i] {
			recorder.Eventf(newService, v1.EventTypeNormal, "ExternalIP", "Added: %v",
				newService.Spec.ExternalIPs[i])
			return true
		}
	}
	if !reflect.DeepEqual(oldService.Annotations, newService.Annotations) {
		// Ignore update if only neg or ilb resources annotations changed, these are added by the neg/l4 controller.
		if !annotations.OnlyStatusAnnotationsChanged(oldService, newService) {
			recorder.Eventf(newService, v1.EventTypeNormal, "Annotations", "%v -> %v",
				oldService.Annotations, newService.Annotations)
			return true
		}
	}
	if oldService.UID != newService.UID {
		recorder.Eventf(newService, v1.EventTypeNormal, "UID", "%v -> %v",
			oldService.UID, newService.UID)
		return true
	}
	if oldService.Spec.ExternalTrafficPolicy != newService.Spec.ExternalTrafficPolicy {
		recorder.Eventf(newService, v1.EventTypeNormal, "ExternalTrafficPolicy", "%v -> %v",
			oldService.Spec.ExternalTrafficPolicy, newService.Spec.ExternalTrafficPolicy)
		return true
	}
	if oldService.Spec.HealthCheckNodePort != newService.Spec.HealthCheckNodePort {
		recorder.Eventf(newService, v1.EventTypeNormal, "HealthCheckNodePort", "%v -> %v",
			oldService.Spec.HealthCheckNodePort, newService.Spec.HealthCheckNodePort)
		return true
	}
	if oldService.Spec.TrafficDistribution != newService.Spec.TrafficDistribution {
		recorder.Eventf(newService, v1.EventTypeNormal, "TrafficDistribution", "%v -> %v",
			oldService.Spec.TrafficDistribution, newService.Spec.TrafficDistribution)
		return true
	}
	if l4c.enableDualStack && !reflect.DeepEqual(oldService.Spec.IPFamilies, newService.Spec.IPFamilies) {
		recorder.Eventf(newService, v1.EventTypeNormal, "IPFamilies", "%v -> %v",
			oldService.Spec.IPFamilies, newService.Spec.IPFamilies)
		return true
	}
	return false
}

// publishMetrics this function sets controller metrics for ILB services and pushed ILB metrics based on sync type.
func (l4c *L4Controller) publishMetrics(result *loadbalancers.L4ILBSyncResult, namespacedName string, isResync bool, svcLogger klog.Logger) {
	if result == nil {
		return
	}
	switch result.SyncType {
	case loadbalancers.SyncTypeCreate, loadbalancers.SyncTypeUpdate:
		svcLogger.V(2).Info("Internal L4 Loadbalancer for Service ensured, updating its state in metrics cache", "serviceState", result.MetricsLegacyState)
		l4c.ctx.L4Metrics.SetL4ILBServiceForLegacyMetric(namespacedName, result.MetricsLegacyState)
		l4c.ctx.L4Metrics.SetL4ILBService(namespacedName, result.MetricsState)
		isWeightedLB := result.MetricsState.WeightedLBPodsPerNode
		isZonalAffinityLB := result.MetricsState.ZonalAffinity
		metrics.PublishILBSyncMetrics(result.Error == nil, result.SyncType, result.GCEResourceInError, utils.GetErrorType(result.Error), result.StartTime, isResync, isWeightedLB, result.MetricsState.Protocol, isZonalAffinityLB)
		if l4c.enableDualStack {
			svcLogger.V(2).Info("Internal L4 DualStack Loadbalancer for Service ensured, updating its state in metrics cache", "serviceState", result.MetricsState)
			metrics.PublishL4ILBDualStackSyncLatency(result.Error == nil, result.SyncType, result.MetricsState.IPFamilies, result.StartTime, isResync)
		}
		if result.MetricsState.Multinetwork {
			metrics.PublishL4ILBMultiNetSyncLatency(result.Error == nil, result.SyncType, result.StartTime, isResync)
		}
		metrics.PublishL4SyncDetails(L4ILBControllerName, result.Error == nil, isResync, result.ResourceUpdates.WereAnyResourcesModified())

	case loadbalancers.SyncTypeDelete:
		// if service is successfully deleted, remove it from cache
		if result.Error == nil {
			svcLogger.V(2).Info("Internal L4 Loadbalancer for Service deleted, removing its state from metrics cache")
			l4c.ctx.L4Metrics.DeleteL4ILBServiceForLegacyMetric(namespacedName)
			l4c.ctx.L4Metrics.DeleteL4ILBService(namespacedName)
		}
		isWeightedLB := result.MetricsState.WeightedLBPodsPerNode
		isZonalAffinityLB := result.MetricsState.ZonalAffinity
		metrics.PublishILBSyncMetrics(result.Error == nil, result.SyncType, result.GCEResourceInError, utils.GetErrorType(result.Error), result.StartTime, false, isWeightedLB, result.MetricsState.Protocol, isZonalAffinityLB)
		if l4c.enableDualStack {
			metrics.PublishL4ILBDualStackSyncLatency(result.Error == nil, result.SyncType, result.MetricsState.IPFamilies, result.StartTime, false)
		}
		if result.MetricsState.Multinetwork {
			metrics.PublishL4ILBMultiNetSyncLatency(result.Error == nil, result.SyncType, result.StartTime, false)
		}
	default:
		svcLogger.Info("Unknown sync type, skipping metrics for service", "syncType", result.SyncType)
	}
}

// handleCreationRace prevents a race condition between the legacy and new L4 ILB controllers
// when a service is created, ensuring the L4 controller processes the most recent service
// state and avoids conflicting operations..
func (l4c *L4Controller) handleCreationRace(service *v1.Service, svcLogger klog.Logger) *v1.Service {
	hasLegacyILBFinalizer := utils.HasLegacyL4ILBFinalizerV1(service)
	hasILBFinalizerV2 := utils.HasL4ILBFinalizerV2(service)
	l4ILBLegacyHeadStartTime := flags.F.L4ILBLegacyHeadStartTime

	// Prevent controllers race on creation
	if !hasLegacyILBFinalizer && !hasILBFinalizerV2 && l4ILBLegacyHeadStartTime > 0*time.Second {
		svcLogger.Info("Service has no finalizers, waiting %d seconds to prevent controllers race on creation.", l4ILBLegacyHeadStartTime/time.Second)
		time.Sleep(l4ILBLegacyHeadStartTime)

		// Get current service from store, so we can verify most recent state.
		svcKey := utils.ServiceKeyFunc(service.Namespace, service.Name)
		svc, exists, err := l4c.ctx.Services().GetByKey(svcKey)
		if err != nil {
			svcLogger.Info("Could not get service from store, using existing one, error: ", err)
		} else if exists {
			service = svc
			svcLogger.Info("finalizer Found service in informer store after wait, using it.")
		} else {
			// Service might have been deleted during the wait, return to avoid processing a non-existing service.
			svcLogger.Info("Service not found in informer store after wait, ignoring processing.")
			return nil
		}
	}
	return service
}
