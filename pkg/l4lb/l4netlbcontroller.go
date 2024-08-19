/*
Copyright 2021 The Kubernetes Authors.

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
	"github.com/google/go-cmp/cmp"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/backends"
	"k8s.io/ingress-gce/pkg/context"
	"k8s.io/ingress-gce/pkg/forwardingrules"
	"k8s.io/ingress-gce/pkg/instancegroups"
	l4metrics "k8s.io/ingress-gce/pkg/l4lb/metrics"
	"k8s.io/ingress-gce/pkg/loadbalancers"
	"k8s.io/ingress-gce/pkg/metrics"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/ingress-gce/pkg/network"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/common"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/ingress-gce/pkg/utils/zonegetter"
	"k8s.io/klog/v2"
)

const (
	l4NetLBControllerName          = "l4netlb-controller"
	l4NetLBDualStackControllerName = "l4netlb-dualstack-controller"

	instanceGroupLink backendLinkType = 0
	negLink           backendLinkType = 1
)

type backendLinkType int64

type L4NetLBController struct {
	ctx             *context.ControllerContext
	svcQueue        utils.TaskQueue
	serviceLister   cache.Indexer
	networkResolver network.Resolver
	stopCh          <-chan struct{}

	zoneGetter *zonegetter.ZoneGetter
	namer      namer.L4ResourcesNamer
	// enqueueTracker tracks the latest time an update was enqueued
	enqueueTracker utils.TimeTracker
	// syncTracker tracks the latest time an enqueued service was synced
	syncTracker utils.TimeTracker

	backendPool                 *backends.Backends
	instancePool                instancegroups.Manager
	igLinker                    *backends.RegionalInstanceGroupLinker
	negLinker                   backends.Linker
	forwardingRules             ForwardingRulesGetter
	enableDualStack             bool
	enableStrongSessionAffinity bool
	serviceVersions             *serviceVersionsTracker

	hasSynced func() bool

	logger klog.Logger
}

// NewL4NetLBController creates a controller for l4 external loadbalancer.
func NewL4NetLBController(
	ctx *context.ControllerContext,
	stopCh <-chan struct{},
	logger klog.Logger) *L4NetLBController {
	logger = logger.WithName("L4NetLBController")
	if ctx.NumL4NetLBWorkers <= 0 {
		logger.Info("External L4 worker count has not been set, setting to 1")
		ctx.NumL4NetLBWorkers = 1
	}

	backendPool := backends.NewPoolWithConnectionTrackingPolicy(ctx.Cloud, ctx.L4Namer, ctx.EnableL4StrongSessionAffinity)
	l4netLBc := &L4NetLBController{
		ctx:                         ctx,
		serviceLister:               ctx.ServiceInformer.GetIndexer(),
		stopCh:                      stopCh,
		zoneGetter:                  ctx.ZoneGetter,
		backendPool:                 backendPool,
		namer:                       ctx.L4Namer,
		instancePool:                ctx.InstancePool,
		igLinker:                    backends.NewRegionalInstanceGroupLinker(ctx.InstancePool, backendPool, logger),
		forwardingRules:             forwardingrules.New(ctx.Cloud, meta.VersionGA, meta.Regional, logger),
		enableDualStack:             ctx.EnableL4NetLBDualStack,
		enableStrongSessionAffinity: ctx.EnableL4StrongSessionAffinity,
		serviceVersions:             NewServiceVersionsTracker(),
		logger:                      logger,
		hasSynced:                   ctx.HasSynced,
	}
	var networkLister cache.Indexer
	if ctx.NetworkInformer != nil {
		networkLister = ctx.NetworkInformer.GetIndexer()
	}
	var gkeNetworkParamSetLister cache.Indexer
	if ctx.GKENetworkParamsInformer != nil {
		gkeNetworkParamSetLister = ctx.GKENetworkParamsInformer.GetIndexer()
	}
	l4netLBc.networkResolver = network.NewNetworksResolver(networkLister, gkeNetworkParamSetLister, ctx.Cloud, ctx.EnableMultinetworking, logger)
	l4netLBc.negLinker = backends.NewNEGLinker(l4netLBc.backendPool, negtypes.NewAdapter(ctx.Cloud), ctx.Cloud, ctx.SvcNegInformer.GetIndexer(), logger)
	l4netLBc.svcQueue = utils.NewPeriodicTaskQueueWithMultipleWorkers("l4netLB", "services", ctx.NumL4NetLBWorkers, l4netLBc.syncWrapper, logger)

	ctx.ServiceInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			addSvc := obj.(*v1.Service)
			svcKey := utils.ServiceKeyFunc(addSvc.Namespace, addSvc.Name)
			svcLogger := logger.WithValues("serviceKey", svcKey)
			if shouldProcess, _ := l4netLBc.shouldProcessService(addSvc, nil, svcLogger); shouldProcess {
				svcLogger.V(3).Info("L4 External LoadBalancer Service added, enqueuing")
				l4netLBc.ctx.Recorder(addSvc.Namespace).Eventf(addSvc, v1.EventTypeNormal, "ADD", svcKey)
				l4netLBc.serviceVersions.SetLastUpdateSeen(svcKey, addSvc.ResourceVersion, svcLogger)
				l4netLBc.svcQueue.Enqueue(addSvc)
				l4netLBc.enqueueTracker.Track()
			} else {
				svcLogger.V(4).Info("Ignoring add for non external lb service")
			}
		},
		// Deletes will be handled in the Update when the deletion timestamp is set.
		UpdateFunc: func(old, cur interface{}) {
			curSvc := cur.(*v1.Service)
			oldSvc := old.(*v1.Service)
			svcKey := utils.ServiceKeyFunc(curSvc.Namespace, curSvc.Name)
			svcLogger := logger.WithValues("serviceKey", svcKey)
			if shouldProcess, isResync := l4netLBc.shouldProcessService(curSvc, oldSvc, svcLogger); shouldProcess {
				svcLogger.V(3).Info("L4 External LoadBalancer Service updated, enqueuing")
				if !isResync {
					l4netLBc.serviceVersions.SetLastUpdateSeen(svcKey, curSvc.ResourceVersion, svcLogger)
				}
				l4netLBc.svcQueue.Enqueue(curSvc)
				l4netLBc.enqueueTracker.Track()
				return
			}
		},
	})
	ctx.AddHealthCheck(l4NetLBControllerName, l4netLBc.checkHealth)
	return l4netLBc
}

// needsAddition checks if given service should be added by controller
func (lc *L4NetLBController) needsAddition(newSvc, oldSvc *v1.Service) bool {
	if oldSvc != nil {
		return false
	}
	needsNetLB, _ := annotations.WantsL4NetLB(newSvc)
	return needsNetLB
}

// needsDeletion return true if svc required deleting RBS based NetLB
func (lc *L4NetLBController) needsDeletion(svc *v1.Service, svcLogger klog.Logger) bool {
	// Check if service was provisioned by RBS controller before -- if it has rbs finalizer or rbs forwarding rule
	if !utils.HasL4NetLBFinalizerV2(svc) && !lc.hasRBSForwardingRule(svc, svcLogger) {
		return false
	}
	// handles service deletion
	if svc.ObjectMeta.DeletionTimestamp != nil {
		return true
	}
	// handles NetLB to ILB migration
	needsNetLB, _ := annotations.WantsL4NetLB(svc)
	return !needsNetLB
}

// needsPeriodicEnqueue return true if svc required periodic enqueue
func (lc *L4NetLBController) needsPeriodicEnqueue(newSvc, oldSvc *v1.Service) bool {
	if oldSvc == nil {
		return false
	}
	needsNetLb, _ := annotations.WantsL4NetLB(newSvc)
	return needsNetLb && reflect.DeepEqual(oldSvc, newSvc)
}

// needsUpdate checks if load balancer needs to be updated due to change in attributes.
func (lc *L4NetLBController) needsUpdate(newSvc, oldSvc *v1.Service) bool {
	if oldSvc == nil {
		return false
	}
	oldSvcWantsNetLB, oldType := annotations.WantsL4NetLB(oldSvc)
	newSvcWantsNetLB, newType := annotations.WantsL4NetLB(newSvc)
	recorder := lc.ctx.Recorder(oldSvc.Namespace)
	if oldSvcWantsNetLB != newSvcWantsNetLB {
		recorder.Eventf(newSvc, v1.EventTypeNormal, "Type", "%v -> %v", oldType, newType)
		return true
	}
	if !newSvcWantsNetLB && !oldSvcWantsNetLB {
		// Ignore any other changes if both the previous and new service do not need L4 External LB.
		return false
	}

	if !reflect.DeepEqual(oldSvc.Spec.LoadBalancerSourceRanges, newSvc.Spec.LoadBalancerSourceRanges) {
		recorder.Eventf(newSvc, v1.EventTypeNormal, "LoadBalancerSourceRanges", "%v -> %v",
			oldSvc.Spec.LoadBalancerSourceRanges, newSvc.Spec.LoadBalancerSourceRanges)
		return true
	}

	if !portsEqualForLBService(oldSvc, newSvc) {
		recorder.Eventf(newSvc, v1.EventTypeNormal, "Ports", "%v -> %v", oldSvc.Spec.Ports, newSvc.Spec.Ports)
		return true
	}

	if diff := cmp.Diff(oldSvc.Spec.SessionAffinity, newSvc.Spec.SessionAffinity); diff != "" {
		recorder.Eventf(newSvc, v1.EventTypeNormal, "SessionAffinity", "%v -> %v", oldSvc.Spec.SessionAffinity, newSvc.Spec.SessionAffinity)
		return true
	}

	if diff := cmp.Diff(oldSvc.Spec.SessionAffinityConfig, newSvc.Spec.SessionAffinityConfig); diff != "" {
		recorder.Eventf(newSvc, v1.EventTypeNormal, "SessionAffinityConfig", "old -> new %s", diff)
		return true
	}

	if oldSvc.Spec.LoadBalancerIP != newSvc.Spec.LoadBalancerIP {
		recorder.Eventf(newSvc, v1.EventTypeNormal, "LoadbalancerIP", "%v -> %v",
			oldSvc.Spec.LoadBalancerIP, newSvc.Spec.LoadBalancerIP)
		return true
	}
	if len(oldSvc.Spec.ExternalIPs) != len(newSvc.Spec.ExternalIPs) {
		recorder.Eventf(newSvc, v1.EventTypeNormal, "ExternalIP", "Count: %v -> %v",
			len(oldSvc.Spec.ExternalIPs), len(newSvc.Spec.ExternalIPs))
		return true
	}
	for i := range oldSvc.Spec.ExternalIPs {
		if oldSvc.Spec.ExternalIPs[i] != newSvc.Spec.ExternalIPs[i] {
			recorder.Eventf(newSvc, v1.EventTypeNormal, "ExternalIP", "Added: %v",
				newSvc.Spec.ExternalIPs[i])
			return true
		}
	}
	if !reflect.DeepEqual(oldSvc.Annotations, newSvc.Annotations) {
		recorder.Eventf(newSvc, v1.EventTypeNormal, "Annotations", "%v -> %v",
			oldSvc.Annotations, newSvc.Annotations)
		return true
	}
	if oldSvc.Spec.ExternalTrafficPolicy != newSvc.Spec.ExternalTrafficPolicy {
		recorder.Eventf(newSvc, v1.EventTypeNormal, "ExternalTrafficPolicy", "%v -> %v",
			oldSvc.Spec.ExternalTrafficPolicy, newSvc.Spec.ExternalTrafficPolicy)
		return true
	}
	if oldSvc.Spec.HealthCheckNodePort != newSvc.Spec.HealthCheckNodePort {
		recorder.Eventf(newSvc, v1.EventTypeNormal, "HealthCheckNodePort", "%v -> %v",
			oldSvc.Spec.HealthCheckNodePort, newSvc.Spec.HealthCheckNodePort)
		return true
	}
	if lc.enableDualStack && !reflect.DeepEqual(oldSvc.Spec.IPFamilies, newSvc.Spec.IPFamilies) {
		recorder.Eventf(newSvc, v1.EventTypeNormal, "IPFamilies", "%v -> %v",
			oldSvc.Spec.IPFamilies, newSvc.Spec.IPFamilies)
		return true
	}
	return false
}

// shouldProcessService checks if given service should be process by controller
func (lc *L4NetLBController) shouldProcessService(newSvc, oldSvc *v1.Service, svcLogger klog.Logger) (shouldProcess bool, isResync bool) {
	// Ignore services with LoadBalancerClass set. LoadBalancerClass can't be updated (see the field API doc) so we don't need to worry about cleaning up services that changed the class.
	if newSvc.Spec.LoadBalancerClass != nil {
		svcLogger.Info("Ignoring service managed by another controller", "serviceLoadBalancerClass", *newSvc.Spec.LoadBalancerClass)
		return false, false
	}

	warnL4FinalizerRemoved(lc.ctx, oldSvc, newSvc)

	if !lc.isRBSBasedService(newSvc, svcLogger) && !lc.isRBSBasedService(oldSvc, svcLogger) {
		return false, false
	}
	if lc.needsAddition(newSvc, oldSvc) || lc.needsUpdate(newSvc, oldSvc) || lc.needsDeletion(newSvc, svcLogger) {
		return true, false
	}
	needsResync := lc.needsPeriodicEnqueue(newSvc, oldSvc)
	return needsResync, needsResync
}

// hasForwardingRuleAnnotation checks if service has forwarding rule with matching name
func (lc *L4NetLBController) hasForwardingRuleAnnotation(svc *v1.Service, frName string) bool {
	if val, ok := svc.Annotations[annotations.TCPForwardingRuleKey]; ok && val == frName {
		return true
	}
	if val, ok := svc.Annotations[annotations.UDPForwardingRuleKey]; ok && val == frName {
		return true
	}
	return false
}

// isRBSBasedService checks if service has either RBS annotation, finalizer or RBSForwardingRule
func (lc *L4NetLBController) isRBSBasedService(svc *v1.Service, svcLogger klog.Logger) bool {
	// Check if the type=LoadBalancer, so we don't execute API calls o non-LB services
	// this call is nil-safe
	if !utils.IsLoadBalancerServiceType(svc) {
		return false
	}
	return annotations.HasRBSAnnotation(svc) || utils.HasL4NetLBFinalizerV2(svc) || lc.hasRBSForwardingRule(svc, svcLogger)
}

func (lc *L4NetLBController) preventLegacyServiceHandling(service *v1.Service, key string, svcLogger klog.Logger) (bool, error) {
	if annotations.HasRBSAnnotation(service) && lc.hasTargetPoolForwardingRule(service, svcLogger) {
		if utils.HasL4NetLBFinalizerV2(service) {
			// If we found that RBS finalizer was attached to service, it means that RBS controller
			// had a race condition on Service creation with Legacy Controller.
			// It should only happen during service creation, and we should clean up RBS resources
			return true, lc.preventTargetPoolRaceWithRBSOnCreation(service, key, svcLogger)
		} else {
			// Target Pool to RBS migration is NOT yet supported and causes service to break (for now).
			// If we detect RBS annotation on legacy service, we remove RBS annotation,
			// so service stays with Legacy Target Pool implementation
			return true, lc.preventExistingTargetPoolToRBSMigration(service, svcLogger)
		}
	}
	return false, nil
}

func (lc *L4NetLBController) hasTargetPoolForwardingRule(service *v1.Service, svcLogger klog.Logger) bool {
	frName := utils.LegacyForwardingRuleName(service)
	if lc.hasForwardingRuleAnnotation(service, frName) {
		return false
	}

	existingFR, err := lc.forwardingRules.Get(frName)
	if err != nil {
		svcLogger.Error(err, "Error getting forwarding rule", "forwardingRule", frName)
		return false
	}
	if existingFR != nil && existingFR.Target != "" {
		return true
	}
	return false
}

func (lc *L4NetLBController) preventTargetPoolRaceWithRBSOnCreation(service *v1.Service, key string, svcLogger klog.Logger) error {
	lc.ctx.Recorder(service.Namespace).Eventf(service, v1.EventTypeWarning, "TargetPoolRaceWithRBS",
		"Target Pool found on provisioned RBS service. Deleting RBS resources")

	l4metrics.IncreaseL4NetLBTargetPoolRaceWithRBS()
	result := lc.garbageCollectRBSNetLB(key, service, svcLogger)
	if result.Error != nil {
		lc.ctx.Recorder(service.Namespace).Eventf(service, v1.EventTypeWarning, "CleanRBSResourcesForLegacyService",
			"Failed to clean RBS resources for load balancer with target pool, err: %v", result.Error)
		return result.Error
	}
	return lc.deleteRBSAnnotation(service, svcLogger)
}

func (lc *L4NetLBController) preventExistingTargetPoolToRBSMigration(service *v1.Service, svcLogger klog.Logger) error {
	lc.ctx.Recorder(service.Namespace).Eventf(service, v1.EventTypeWarning, "CanNotMigrateTargetPoolToRBS",
		"RBS annotation was attached to the Legacy Target Pool service. Migration is not supported. Removing annotation")
	l4metrics.IncreaseL4NetLBLegacyToRBSMigrationAttempts()

	return lc.deleteRBSAnnotation(service, svcLogger)
}

func (lc *L4NetLBController) deleteRBSAnnotation(service *v1.Service, svcLogger klog.Logger) error {
	err := deleteAnnotation(lc.ctx, service, annotations.RBSAnnotationKey, svcLogger)
	if err != nil {
		return fmt.Errorf("deleteAnnotation(_, %v, %s) returned error %v, want nil", service, annotations.RBSAnnotationKey, err)
	}
	// update current object annotations, so we do not treat it as RBS after
	delete(service.Annotations, annotations.RBSAnnotationKey)
	return nil
}

// hasRBSForwardingRule checks if services loadbalancer has forwarding rule pointing to backend service
func (lc *L4NetLBController) hasRBSForwardingRule(svc *v1.Service, svcLogger klog.Logger) bool {
	frName := utils.LegacyForwardingRuleName(svc)
	// to optimize number of api calls, at first, check if forwarding rule exists in annotation
	if lc.hasForwardingRuleAnnotation(svc, frName) {
		return true
	}
	existingFR, err := lc.forwardingRules.Get(frName)
	if err != nil {
		svcLogger.Error(err, "Error getting forwarding rule", "forwardingRule", frName)
		return false
	}
	return existingFR != nil && existingFR.LoadBalancingScheme == string(cloud.SchemeExternal) && existingFR.BackendService != ""
}

func (lc *L4NetLBController) checkHealth() error {
	lastEnqueueTime := lc.enqueueTracker.Get()
	lastSyncTime := lc.syncTracker.Get()
	// if lastEnqueue time is more than 15 minutes before the last sync time, the controller is falling behind.
	// This indicates that the controller was stuck handling a previous update, or sync function did not get invoked.
	syncTimeLatest := lastEnqueueTime.Add(enqueueToSyncDelayThreshold)
	controllerHealth := l4metrics.ControllerHealthyStatus
	if lastSyncTime.After(syncTimeLatest) {
		msg := fmt.Sprintf("L4 NetLB Sync happened at time %v, %v after enqueue time, last enqueue time %v, threshold is %v", lastSyncTime, lastSyncTime.Sub(lastEnqueueTime), lastEnqueueTime, enqueueToSyncDelayThreshold)
		// Log here, context/http handler do no log the error.
		lc.logger.Error(nil, msg)
		l4metrics.PublishL4FailedHealthCheckCount(l4NetLBControllerName)
		controllerHealth = l4metrics.ControllerUnhealthyStatus
		// Reset trackers. Otherwise, if there is nothing in the queue then it will report the FailedHealthCheckCount every time the checkHealth is called
		// If checkHealth returned error (as it is meant to) then container would be restarted and trackers would be reset either
		lc.enqueueTracker.Track()
		lc.syncTracker.Track()
	}
	if lc.enableDualStack {
		l4metrics.PublishL4ControllerHealthCheckStatus(l4NetLBDualStackControllerName, controllerHealth)
	}
	return nil
}

// Run starts the loadbalancer controller.
func (lc *L4NetLBController) Run() {
	defer lc.shutdown()

	wait.PollUntil(5*time.Second, func() (bool, error) {
		lc.logger.V(2).Info("Waiting for initial cache sync before starting L4 Net LB Controller")
		return lc.hasSynced(), nil
	}, lc.stopCh)

	lc.logger.Info("Running L4 Net Controller", "numWorkers", lc.ctx.NumL4NetLBWorkers)
	lc.svcQueue.Run()

	<-lc.stopCh
}

func (lc *L4NetLBController) shutdown() {
	lc.logger.Info("Shutting down l4NetLBController")
	lc.svcQueue.Shutdown()
}
func (lc *L4NetLBController) syncWrapper(key string) (err error) {
	syncTrackingId := rand.Int31()
	svcLogger := lc.logger.WithValues("serviceKey", key, "syncId", syncTrackingId)

	defer func() {
		if r := recover(); r != nil {
			errMessage := fmt.Sprintf("Panic in L4 NetLB sync worker goroutine: %v", r)
			svcLogger.Error(nil, errMessage)
			l4metrics.PublishL4ControllerPanicCount(l4NetLBControllerName)
			err = fmt.Errorf(errMessage)
		}
	}()
	syncErr := lc.sync(key, svcLogger)
	return skipUserError(syncErr, svcLogger)
}

func (lc *L4NetLBController) sync(key string, svcLogger klog.Logger) error {
	lc.syncTracker.Track()
	l4metrics.PublishL4controllerLastSyncTime(l4NetLBControllerName)

	svc, exists, err := lc.ctx.Services().GetByKey(key)
	if err != nil {
		return fmt.Errorf("failed to lookup L4 External LoadBalancer service for key %s : %w", key, err)
	}
	if !exists || svc == nil {
		svcLogger.V(3).Info("Ignoring sync of non-existent service")
		return nil
	}
	isLegacyService, err := lc.preventLegacyServiceHandling(svc, key, svcLogger)
	if err != nil {
		svcLogger.Info("lc.preventLegacyServiceHandling returned error, want nil", "service", svc, "err", err)
		return err
	}
	if isLegacyService {
		svcLogger.V(3).Info("Ignoring sync of legacy target pool service")
		return nil
	}
	isResync := lc.serviceVersions.IsResync(key, svc.ResourceVersion, svcLogger)
	svcLogger.Info("Processing update operation for service", "resync", isResync, "resourceVersion", svc.ResourceVersion)
	if lc.needsDeletion(svc, svcLogger) {
		svcLogger.V(3).Info("Deleting L4 External LoadBalancer resources for service")
		result := lc.garbageCollectRBSNetLB(key, svc, svcLogger)
		if result == nil {
			return nil
		}
		lc.serviceVersions.Delete(key)
		lc.publishMetrics(result, svc.Name, svc.Namespace, false, svcLogger)
		return result.Error
	}

	if wantsNetLB, _ := annotations.WantsL4NetLB(svc); wantsNetLB {
		result := lc.syncInternal(svc, svcLogger)
		if result == nil {
			// result will be nil if the service was ignored(due to presence of service controller finalizer).
			return nil
		}
		lc.serviceVersions.SetProcessed(key, svc.ResourceVersion, result.Error == nil, isResync, svcLogger)
		lc.publishMetrics(result, svc.Name, svc.Namespace, isResync, svcLogger)
		return result.Error
	}
	svcLogger.V(3).Info("Ignoring sync of service, neither delete nor ensure needed.")
	return nil
}

// syncInternal ensures load balancer resources for the given service, as needed.
// Returns an error if processing the service update failed.
func (lc *L4NetLBController) syncInternal(service *v1.Service, svcLogger klog.Logger) *loadbalancers.L4NetLBSyncResult {
	// check again that rbs is enabled.
	if !lc.isRBSBasedService(service, svcLogger) {
		svcLogger.Info("Skipping syncInternal. Service does not have RBS enabled")
		return nil
	}

	startTime := time.Now()
	svcLogger.Info("Syncing L4 NetLB RBS service")
	defer func() {
		svcLogger.Info("Finished syncing L4 NetLB RBS service", "timeTaken", time.Since(startTime))
	}()

	l4NetLBParams := &loadbalancers.L4NetLBParams{
		Service:                      service,
		Cloud:                        lc.ctx.Cloud,
		Namer:                        lc.namer,
		Recorder:                     lc.ctx.Recorder(service.Namespace),
		DualStackEnabled:             lc.enableDualStack,
		StrongSessionAffinityEnabled: lc.enableStrongSessionAffinity,
		NetworkResolver:              lc.networkResolver,
		EnableWeightedLB:             lc.ctx.EnableWeightedL4NetLB,
	}
	l4netlb := loadbalancers.NewL4NetLB(l4NetLBParams, svcLogger)

	if err := common.EnsureServiceFinalizer(service, common.NetLBFinalizerV2, lc.ctx.KubeClient, svcLogger); err != nil {
		return &loadbalancers.L4NetLBSyncResult{Error: fmt.Errorf("Failed to attach L4 External LoadBalancer finalizer to service %s/%s, err %w", service.Namespace, service.Name, err)}
	}

	nodes, err := lc.zoneGetter.ListNodes(zonegetter.CandidateNodesFilter, svcLogger)
	if err != nil {
		return &loadbalancers.L4NetLBSyncResult{Error: err}
	}
	nodeNames := utils.GetNodeNames(nodes)
	isMultinet := lc.networkResolver.IsMultinetService(service)
	if !isMultinet {
		if err := lc.ensureInstanceGroups(service, nodeNames, svcLogger); err != nil {
			lc.ctx.Recorder(service.Namespace).Eventf(service, v1.EventTypeWarning, "SyncInstanceGroupsFailed",
				"Error syncing instance group, err: %v", err)
			return &loadbalancers.L4NetLBSyncResult{Error: err}
		}
	}

	// Use the same function for both create and updates. If controller crashes and restarts,
	// all existing services will show up as Service Adds.
	syncResult := l4netlb.EnsureFrontend(nodeNames, service)
	if syncResult.Error != nil {
		lc.ctx.Recorder(service.Namespace).Eventf(service, v1.EventTypeWarning, "SyncExternalLoadBalancerFailed",
			"Error ensuring Resource for L4 External LoadBalancer, err: %v", syncResult.Error)
		if utils.IsUserError(syncResult.Error) {
			syncResult.MetricsLegacyState.IsUserError = true
			syncResult.MetricsState.Status = metrics.StatusUserError
		}
		return syncResult
	}

	linkType := instanceGroupLink
	if isMultinet {
		linkType = negLink
	}

	if err = lc.ensureBackendLinking(service, linkType, svcLogger); err != nil {
		lc.ctx.Recorder(service.Namespace).Eventf(service, v1.EventTypeWarning, "SyncExternalLoadBalancerFailed",
			"Error linking instance groups to backend service, err: %v", err)
		syncResult.Error = err
		return syncResult
	}

	err = updateServiceStatus(lc.ctx, service, syncResult.Status, svcLogger)
	if err != nil {
		lc.ctx.Recorder(service.Namespace).Eventf(service, v1.EventTypeWarning, "SyncExternalLoadBalancerFailed",
			"Error updating L4 External LoadBalancer, err: %v", err)
		syncResult.Error = err
		return syncResult
	}
	if lc.enableDualStack {
		lc.emitEnsuredDualStackEvent(service)

		if err = updateL4DualStackResourcesAnnotations(lc.ctx, service, syncResult.Annotations, svcLogger); err != nil {
			lc.ctx.Recorder(service.Namespace).Eventf(service, v1.EventTypeWarning, "SyncExternalLoadBalancerFailed",
				"Failed to update annotations for load balancer, err: %v", err)
			syncResult.Error = fmt.Errorf("failed to set resource annotations, err: %w", err)
			return syncResult
		}
	} else {
		lc.ctx.Recorder(service.Namespace).Eventf(service, v1.EventTypeNormal, "SyncLoadBalancerSuccessful",
			"Successfully ensured L4 External LoadBalancer resources")

		if err = updateL4ResourcesAnnotations(lc.ctx, service, syncResult.Annotations, svcLogger); err != nil {
			lc.ctx.Recorder(service.Namespace).Eventf(service, v1.EventTypeWarning, "SyncExternalLoadBalancerFailed",
				"Failed to update annotations for load balancer, err: %v", err)
			syncResult.Error = fmt.Errorf("failed to set resource annotations, err: %w", err)
			return syncResult
		}
	}
	syncResult.SetMetricsForSuccessfulServiceSync()
	return syncResult
}

func (lc *L4NetLBController) emitEnsuredDualStackEvent(service *v1.Service) {
	var ipFamilies []string
	for _, ipFamily := range service.Spec.IPFamilies {
		ipFamilies = append(ipFamilies, string(ipFamily))
	}
	lc.ctx.Recorder(service.Namespace).Eventf(service, v1.EventTypeNormal, "SyncLoadBalancerSuccessful",
		"Successfully ensured %v External LoadBalancer resources", strings.Join(ipFamilies, " "))
}

func (lc *L4NetLBController) ensureBackendLinking(service *v1.Service, linkType backendLinkType, svcLogger klog.Logger) error {
	start := time.Now()

	svcLogger.V(2).Info("Linking backends to backend service for k8s service")
	defer func() {
		svcLogger.V(2).Info("Finished linking backends to backend service for k8s service", "timeTaken", time.Since(start))
	}()

	zones, err := lc.zoneGetter.ListZones(zonegetter.CandidateNodesFilter, svcLogger)
	if err != nil {
		return err
	}

	namespacedName := types.NamespacedName{Name: service.Name, Namespace: service.Namespace}
	portId := utils.ServicePortID{Service: namespacedName}
	servicePort := utils.ServicePort{
		ID:           portId,
		BackendNamer: lc.namer,
		L4RBSEnabled: true,
	}
	// NEG backends should only be used for multinetwork services on the non default network.
	if linkType == negLink {
		svcLogger.V(2).Info("Linking backend service with NEGs for service")
		servicePort.VMIPNEGEnabled = true
		var groupKeys []backends.GroupKey
		for _, zone := range zones {
			groupKeys = append(groupKeys, backends.GroupKey{Zone: zone})
		}
		return lc.negLinker.Link(servicePort, groupKeys)
	} else if linkType == instanceGroupLink {
		svcLogger.V(2).Info("Linking backend service with Instance Groups for service (uses default network)")
		return lc.igLinker.Link(servicePort, lc.ctx.Cloud.ProjectID(), zones)
	} else {
		return fmt.Errorf("cannot link backend service - invalid backend link type %d", linkType)
	}
}

func (lc *L4NetLBController) ensureInstanceGroups(service *v1.Service, nodeNames []string, svcLogger klog.Logger) error {
	// TODO(kl52752) Move instance creation and deletion logic to NodeController
	// to avoid race condition between controllers
	start := time.Now()
	svcLogger.V(2).Info("Ensuring instance groups for L4 NetLB Service", "len(nodeNames)", len(nodeNames))
	defer func() {
		svcLogger.V(2).Info("Finished ensuring instance groups for L4 NetLB Service", "timeTaken", time.Since(start))
	}()

	// L4 NetLB does not use node ports, so we provide empty slice
	var nodePorts []int64
	igName := lc.ctx.ClusterNamer.InstanceGroup()
	_, err := lc.instancePool.EnsureInstanceGroupsAndPorts(igName, nodePorts)
	if err != nil {
		return fmt.Errorf("lc.instancePool.EnsureInstanceGroupsAndPorts(%s, %v) returned error %w", igName, nodePorts, err)
	}

	return lc.instancePool.Sync(nodeNames)
}

// garbageCollectRBSNetLB cleans-up all gce resources related to service and removes NetLB finalizer
func (lc *L4NetLBController) garbageCollectRBSNetLB(key string, svc *v1.Service, svcLogger klog.Logger) *loadbalancers.L4NetLBSyncResult {
	startTime := time.Now()
	svcLogger.Info("Deleting L4 NetLB RBS service")
	defer func() {
		svcLogger.Info("Finished deleting L4 NetLB service", "timeTaken", time.Since(startTime))
	}()

	l4NetLBParams := &loadbalancers.L4NetLBParams{
		Service:                      svc,
		Cloud:                        lc.ctx.Cloud,
		Namer:                        lc.namer,
		Recorder:                     lc.ctx.Recorder(svc.Namespace),
		DualStackEnabled:             lc.enableDualStack,
		StrongSessionAffinityEnabled: lc.enableStrongSessionAffinity,
		NetworkResolver:              lc.networkResolver,
		EnableWeightedLB:             lc.ctx.EnableWeightedL4NetLB,
	}
	l4netLB := loadbalancers.NewL4NetLB(l4NetLBParams, svcLogger)
	lc.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeNormal, "DeletingLoadBalancer",
		"Deleting L4 External LoadBalancer for %s", key)

	result := l4netLB.EnsureLoadBalancerDeleted(svc)
	if result.Error != nil {
		lc.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeWarning, "DeleteLoadBalancerFailed",
			"Error deleting L4 External LoadBalancer, err: %v", result.Error)
		return result
	}

	if err := updateServiceStatus(lc.ctx, svc, &v1.LoadBalancerStatus{}, svcLogger); err != nil {
		lc.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeWarning, "DeleteLoadBalancer",
			"Error resetting L4 External LoadBalancer status to empty, err: %v", err)
		result.Error = fmt.Errorf("Failed to reset L4 External LoadBalancer status, err: %w", err)
		return result
	}

	// Try to delete instance group, instancePool.DeleteInstanceGroup ignores errors if resource is in use or not found.
	// TODO(cezarygerard) replace with multi-IG management
	if err := lc.instancePool.DeleteInstanceGroup(lc.namer.InstanceGroup()); err != nil {
		lc.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeWarning, "DeleteInstanceGroupFailed",
			"Error deleting delete Instance Group from L4 External LoadBalancer, err: %v", err)
		result.Error = fmt.Errorf("Failed to delete Instance Group, err: %w", err)
		return result
	}

	if lc.enableDualStack {
		if err := updateL4DualStackResourcesAnnotations(lc.ctx, svc, nil, svcLogger); err != nil {
			lc.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeWarning, "DeleteLoadBalancer",
				"Error removing Dual Stack resource annotations: %v", err)
			result.Error = fmt.Errorf("failed to reset Dual Stack resource annotations, err: %w", err)
			return result
		}
	} else {
		if err := updateL4ResourcesAnnotations(lc.ctx, svc, nil, svcLogger); err != nil {
			lc.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeWarning, "DeleteLoadBalancer",
				"Error removing resource annotations: %v", err)
			result.Error = fmt.Errorf("failed to reset resource annotations, err: %w", err)
			return result
		}
	}

	// Finalizer needs to be removed last, because after deleting finalizer service can be deleted and
	// updating annotations or other manipulations will fail
	if err := common.EnsureDeleteServiceFinalizer(svc, common.NetLBFinalizerV2, lc.ctx.KubeClient, svcLogger); err != nil {
		lc.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeWarning, "DeleteLoadBalancerFailed",
			"Error removing finalizer from L4 External LoadBalancer, err: %v", err)
		result.Error = fmt.Errorf("Failed to remove L4 External LoadBalancer finalizer, err: %w", err)
		return result
	}

	lc.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeNormal, "DeletedLoadBalancer", "Deleted L4 External LoadBalancer")
	return result
}

// publishMetrics sets controller metrics for NetLB services and pushes NetLB metrics based on sync type.
func (lc *L4NetLBController) publishMetrics(result *loadbalancers.L4NetLBSyncResult, name, namespace string, isResync bool, svcLogger klog.Logger) {
	namespacedName := types.NamespacedName{Name: name, Namespace: namespace}.String()
	switch result.SyncType {
	case loadbalancers.SyncTypeCreate, loadbalancers.SyncTypeUpdate:
		svcLogger.V(2).Info("External L4 Loadbalancer for Service ensured, updating its state in metrics cache", "serviceState", result.MetricsState, "serviceLegacyState", result.MetricsLegacyState)
		lc.ctx.ControllerMetrics.SetL4NetLBServiceForLegacyMetric(namespacedName, result.MetricsLegacyState)
		lc.ctx.ControllerMetrics.SetL4NetLBService(namespacedName, result.MetricsState)
		lc.publishSyncMetrics(result, isResync)
	case loadbalancers.SyncTypeDelete:
		// if service is successfully deleted, remove it from cache
		if result.Error == nil {
			svcLogger.V(2).Info("External L4 Loadbalancer for Service deleted, removing its state from metrics cache")
			lc.ctx.ControllerMetrics.DeleteL4NetLBServiceForLegacyMetric(namespacedName)
			lc.ctx.ControllerMetrics.DeleteL4NetLBService(namespacedName)
		}
		lc.publishSyncMetrics(result, false)
	default:
		svcLogger.Info("Unknown sync type, skipping metrics for service", "syncType", result.SyncType)
	}
}

func (lc *L4NetLBController) publishSyncMetrics(result *loadbalancers.L4NetLBSyncResult, isResync bool) {
	if lc.enableDualStack {
		l4metrics.PublishL4NetLBDualStackSyncLatency(result.Error == nil, result.SyncType, result.MetricsState.IPFamilies, result.StartTime, isResync)
	}
	if result.MetricsState.Multinetwork {
		l4metrics.PublishL4NetLBMultiNetSyncLatency(result.Error == nil, result.SyncType, result.StartTime, isResync)
	}
	if result.Error == nil {
		l4metrics.PublishL4NetLBSyncSuccess(result.SyncType, result.StartTime, isResync)
		return
	}
	l4metrics.PublishL4NetLBSyncError(result.SyncType, result.GCEResourceInError, utils.GetErrorType(result.Error), result.StartTime, isResync)
}
