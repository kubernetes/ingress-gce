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
	"reflect"
	"strings"
	"time"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/backends"
	"k8s.io/ingress-gce/pkg/context"
	"k8s.io/ingress-gce/pkg/controller/translator"
	"k8s.io/ingress-gce/pkg/forwardingrules"
	"k8s.io/ingress-gce/pkg/instancegroups"
	"k8s.io/ingress-gce/pkg/l4lb/metrics"
	"k8s.io/ingress-gce/pkg/loadbalancers"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/common"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/klog/v2"
)

const l4NetLBControllerName = "l4netlb-controller"

type L4NetLBController struct {
	ctx           *context.ControllerContext
	svcQueue      utils.TaskQueue
	serviceLister cache.Indexer
	nodeLister    listers.NodeLister
	stopCh        chan struct{}

	translator *translator.Translator
	namer      namer.L4ResourcesNamer
	// enqueueTracker tracks the latest time an update was enqueued
	enqueueTracker utils.TimeTracker
	// syncTracker tracks the latest time an enqueued service was synced
	syncTracker utils.TimeTracker

	backendPool     *backends.Backends
	instancePool    instancegroups.Manager
	igLinker        *backends.RegionalInstanceGroupLinker
	forwardingRules ForwardingRulesGetter
	enableDualStack bool
}

// NewL4NetLBController creates a controller for l4 external loadbalancer.
func NewL4NetLBController(
	ctx *context.ControllerContext,
	stopCh chan struct{}) *L4NetLBController {
	if ctx.NumL4NetLBWorkers <= 0 {
		klog.Infof("External L4 worker count has not been set, setting to 1")
		ctx.NumL4NetLBWorkers = 1
	}

	backendPool := backends.NewPool(ctx.Cloud, ctx.L4Namer)
	l4netLBc := &L4NetLBController{
		ctx:             ctx,
		serviceLister:   ctx.ServiceInformer.GetIndexer(),
		nodeLister:      listers.NewNodeLister(ctx.NodeInformer.GetIndexer()),
		stopCh:          stopCh,
		translator:      ctx.Translator,
		backendPool:     backendPool,
		namer:           ctx.L4Namer,
		instancePool:    ctx.InstancePool,
		igLinker:        backends.NewRegionalInstanceGroupLinker(ctx.InstancePool, backendPool),
		forwardingRules: forwardingrules.New(ctx.Cloud, meta.VersionGA, meta.Regional),
		enableDualStack: ctx.EnableL4NetLBDualStack,
	}
	l4netLBc.svcQueue = utils.NewPeriodicTaskQueueWithMultipleWorkers("l4netLB", "services", ctx.NumL4NetLBWorkers, l4netLBc.sync)

	ctx.ServiceInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			addSvc := obj.(*v1.Service)
			svcKey := utils.ServiceKeyFunc(addSvc.Namespace, addSvc.Name)
			if l4netLBc.shouldProcessService(addSvc, nil) {
				klog.V(3).Infof("L4 External LoadBalancer Service %s added, enqueuing", svcKey)
				l4netLBc.ctx.Recorder(addSvc.Namespace).Eventf(addSvc, v1.EventTypeNormal, "ADD", svcKey)
				l4netLBc.svcQueue.Enqueue(addSvc)
				l4netLBc.enqueueTracker.Track()
			} else {
				klog.V(4).Infof("Ignoring add for non external lb service %s", svcKey)
			}
		},
		// Deletes will be handled in the Update when the deletion timestamp is set.
		UpdateFunc: func(old, cur interface{}) {
			curSvc := cur.(*v1.Service)
			oldSvc := old.(*v1.Service)
			svcKey := utils.ServiceKeyFunc(curSvc.Namespace, curSvc.Name)
			if l4netLBc.shouldProcessService(curSvc, oldSvc) {
				klog.V(3).Infof("L4 External LoadBalancer Service %s updated, enqueuing", svcKey)
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
func (lc *L4NetLBController) needsDeletion(svc *v1.Service) bool {
	// Check if service has RBS related fields/resources
	// this check differs from isRBSBasedService() func by checking also non load balancer type services
	if !(lc.hasRBSAnnotation(svc) || utils.HasL4NetLBFinalizerV2(svc) || lc.hasRBSForwardingRule(svc)) {
		return false
	}
	if svc.ObjectMeta.DeletionTimestamp != nil {
		return true
	}
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

	if !portsEqualForLBService(oldSvc, newSvc) || oldSvc.Spec.SessionAffinity != newSvc.Spec.SessionAffinity {
		recorder.Eventf(newSvc, v1.EventTypeNormal, "Ports/SessionAffinity", "Ports %v, SessionAffinity %v -> Ports %v, SessionAffinity  %v",
			oldSvc.Spec.Ports, oldSvc.Spec.SessionAffinity, newSvc.Spec.Ports, newSvc.Spec.SessionAffinity)
		return true
	}
	if !reflect.DeepEqual(oldSvc.Spec.SessionAffinityConfig, newSvc.Spec.SessionAffinityConfig) {
		recorder.Eventf(newSvc, v1.EventTypeNormal, "SessionAffinityConfig", "%v -> %v",
			oldSvc.Spec.SessionAffinityConfig, newSvc.Spec.SessionAffinityConfig)
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
func (lc *L4NetLBController) shouldProcessService(newSvc, oldSvc *v1.Service) bool {
	if !(lc.isRBSBasedService(newSvc) || lc.isRBSBasedService(oldSvc)) {
		return false
	}
	if lc.needsAddition(newSvc, oldSvc) || lc.needsUpdate(newSvc, oldSvc) || lc.needsDeletion(newSvc) {
		return true
	}
	return lc.needsPeriodicEnqueue(newSvc, oldSvc)
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
func (lc *L4NetLBController) isRBSBasedService(svc *v1.Service) bool {
	// Check if the type=LoadBalancer, so we don't execute API calls o non-LB services
	// this call is nil-safe
	if !utils.IsLoadBalancerServiceType(svc) {
		return false
	}
	return lc.hasRBSAnnotation(svc) || utils.HasL4NetLBFinalizerV2(svc) || lc.hasRBSForwardingRule(svc)
}

func (lc *L4NetLBController) hasRBSAnnotation(service *v1.Service) bool {
	if service == nil {
		return false
	}

	if val, ok := service.Annotations[annotations.RBSAnnotationKey]; ok && val == annotations.RBSEnabled {
		return true
	}
	return false
}

func (lc *L4NetLBController) preventLegacyServiceHandling(service *v1.Service, key string) (bool, error) {
	if lc.hasRBSAnnotation(service) && lc.hasTargetPoolForwardingRule(service) {
		if utils.HasL4NetLBFinalizerV2(service) {
			// If we found that RBS finalizer was attached to service, it means that RBS controller
			// had a race condition on Service creation with Legacy Controller.
			// It should only happen during service creation, and we should clean up RBS resources
			return true, lc.preventTargetPoolRaceWithRBSOnCreation(service, key)
		} else {
			// Target Pool to RBS migration is NOT yet supported and causes service to break (for now).
			// If we detect RBS annotation on legacy service, we remove RBS annotation,
			// so service stays with Legacy Target Pool implementation
			return true, lc.preventExistingTargetPoolToRBSMigration(service)
		}
	}
	return false, nil
}

func (lc *L4NetLBController) hasTargetPoolForwardingRule(service *v1.Service) bool {
	frName := utils.LegacyForwardingRuleName(service)
	if lc.hasForwardingRuleAnnotation(service, frName) {
		return false
	}

	existingFR, err := lc.forwardingRules.Get(frName)
	if err != nil {
		klog.Errorf("Error getting forwarding rule %s", frName)
		return false
	}
	if existingFR != nil && existingFR.Target != "" {
		return true
	}
	return false
}

func (lc *L4NetLBController) preventTargetPoolRaceWithRBSOnCreation(service *v1.Service, key string) error {
	lc.ctx.Recorder(service.Namespace).Eventf(service, v1.EventTypeWarning, "TargetPoolRaceWithRBS",
		"Target Pool found on provisioned RBS service. Deleting RBS resources")

	metrics.IncreaseL4NetLBTargetPoolRaceWithRBS()
	result := lc.garbageCollectRBSNetLB(key, service)
	if result.Error != nil {
		lc.ctx.Recorder(service.Namespace).Eventf(service, v1.EventTypeWarning, "CleanRBSResourcesForLegacyService",
			"Failed to clean RBS resources for load balancer with target pool, err: %v", result.Error)
		return result.Error
	}
	return nil
}

func (lc *L4NetLBController) preventExistingTargetPoolToRBSMigration(service *v1.Service) error {
	lc.ctx.Recorder(service.Namespace).Eventf(service, v1.EventTypeWarning, "CanNotMigrateTargetPoolToRBS",
		"RBS annotation was attached to the Legacy Target Pool service. Migration is not supported. Removing annotation")
	metrics.IncreaseL4NetLBLegacyToRBSMigrationAttempts()

	return lc.deleteRBSAnnotation(service)
}

func (lc *L4NetLBController) deleteRBSAnnotation(service *v1.Service) error {
	err := deleteAnnotation(lc.ctx, service, annotations.RBSAnnotationKey)
	if err != nil {
		return fmt.Errorf("deleteAnnotation(_, %v, %s) returned error %v, want nil", service, annotations.RBSAnnotationKey, err)
	}
	// update current object annotations, so we do not treat it as RBS after
	delete(service.Annotations, annotations.RBSAnnotationKey)
	return nil
}

// hasRBSForwardingRule checks if services loadbalancer has forwarding rule pointing to backend service
func (lc *L4NetLBController) hasRBSForwardingRule(svc *v1.Service) bool {
	frName := utils.LegacyForwardingRuleName(svc)
	// to optimize number of api calls, at first, check if forwarding rule exists in annotation
	if lc.hasForwardingRuleAnnotation(svc, frName) {
		return true
	}
	existingFR, err := lc.forwardingRules.Get(frName)
	if err != nil {
		klog.Errorf("Error getting forwarding rule %s", frName)
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
	if lastSyncTime.After(syncTimeLatest) {
		msg := fmt.Sprintf("L4 External LoadBalancer Sync happened at time %v - %v after enqueue time, threshold is %v", lastSyncTime, lastSyncTime.Sub(lastEnqueueTime), enqueueToSyncDelayThreshold)
		// Log here, context/http handler do no log the error.
		klog.Error(msg)
		metrics.PublishL4FailedHealthCheckCount(l4NetLBControllerName)
	}
	return nil
}

// Run starts the loadbalancer controller.
func (lc *L4NetLBController) Run() {
	defer lc.shutdown()
	klog.Infof("Starting l4NetLBController")
	lc.svcQueue.Run()

	<-lc.stopCh
}

func (lc *L4NetLBController) shutdown() {
	klog.Infof("Shutting down l4NetLBController")
	lc.svcQueue.Shutdown()
}

func (lc *L4NetLBController) sync(key string) error {
	lc.syncTracker.Track()
	svc, exists, err := lc.ctx.Services().GetByKey(key)
	if err != nil {
		return fmt.Errorf("failed to lookup L4 External LoadBalancer service for key %s : %w", key, err)
	}
	if !exists || svc == nil {
		klog.V(3).Infof("Ignoring sync of non-existent service %s", key)
		return nil
	}

	isLegacyService, err := lc.preventLegacyServiceHandling(svc, key)
	if err != nil {
		klog.Warningf("lc.preventLegacyServiceHandling(%v, %s) returned error %v, want nil", svc, key, err)
		return err
	}
	if isLegacyService {
		klog.V(3).Infof("Ignoring sync of legacy target pool service %s", key)
		return nil
	}

	if lc.needsDeletion(svc) {
		klog.V(3).Infof("Deleting L4 External LoadBalancer resources for service %s", key)
		result := lc.garbageCollectRBSNetLB(key, svc)
		if result == nil {
			return nil
		}
		lc.publishMetrics(result, svc.Name, svc.Namespace)
		return result.Error
	}

	if wantsNetLB, _ := annotations.WantsL4NetLB(svc); wantsNetLB {
		result := lc.syncInternal(svc)
		if result == nil {
			// result will be nil if the service was ignored(due to presence of service controller finalizer).
			return nil
		}
		lc.publishMetrics(result, svc.Name, svc.Namespace)
		return result.Error
	}
	klog.V(3).Infof("Ignoring sync of service %s, neither delete nor ensure needed.", key)
	return nil
}

// syncInternal ensures load balancer resources for the given service, as needed.
// Returns an error if processing the service update failed.
func (lc *L4NetLBController) syncInternal(service *v1.Service) *loadbalancers.L4NetLBSyncResult {
	l4NetLBParams := &loadbalancers.L4NetLBParams{
		Service:          service,
		Cloud:            lc.ctx.Cloud,
		Namer:            lc.namer,
		Recorder:         lc.ctx.Recorder(service.Namespace),
		DualStackEnabled: lc.enableDualStack,
	}
	l4netlb := loadbalancers.NewL4NetLB(l4NetLBParams)
	// check again that rbs is enabled.
	if !lc.isRBSBasedService(service) {
		klog.Infof("Skipping syncInternal. Service %s does not have RBS enabled", service.Name)
		return nil
	}

	startTime := time.Now()
	klog.Infof("Syncing L4 NetLB RBS service %s/%s", service.Namespace, service.Name)
	defer func() {
		klog.Infof("Finished syncing L4 NetLB RBS service %s/%s, time taken: %v", service.Namespace, service.Name, time.Since(startTime))
	}()

	if err := common.EnsureServiceFinalizer(service, common.NetLBFinalizerV2, lc.ctx.KubeClient); err != nil {
		return &loadbalancers.L4NetLBSyncResult{Error: fmt.Errorf("Failed to attach L4 External LoadBalancer finalizer to service %s/%s, err %w", service.Namespace, service.Name, err)}
	}

	nodeNames, err := utils.GetReadyNodeNames(lc.nodeLister)
	if err != nil {
		return &loadbalancers.L4NetLBSyncResult{Error: err}
	}

	if err := lc.ensureInstanceGroups(service, nodeNames); err != nil {
		lc.ctx.Recorder(service.Namespace).Eventf(service, v1.EventTypeWarning, "SyncInstanceGroupsFailed",
			"Error syncing instance group, err: %v", err)
		return &loadbalancers.L4NetLBSyncResult{Error: err}
	}

	// Use the same function for both create and updates. If controller crashes and restarts,
	// all existing services will show up as Service Adds.
	syncResult := l4netlb.EnsureFrontend(nodeNames, service)
	if syncResult.Error != nil {
		lc.ctx.Recorder(service.Namespace).Eventf(service, v1.EventTypeWarning, "SyncExternalLoadBalancerFailed",
			"Error ensuring Resource for L4 External LoadBalancer, err: %v", syncResult.Error)
		return syncResult
	}

	if err = lc.ensureBackendLinking(service); err != nil {
		lc.ctx.Recorder(service.Namespace).Eventf(service, v1.EventTypeWarning, "SyncExternalLoadBalancerFailed",
			"Error linking instance groups to backend service, err: %v", err)
		syncResult.Error = err
		return syncResult
	}

	err = updateServiceStatus(lc.ctx, service, syncResult.Status)
	if err != nil {
		lc.ctx.Recorder(service.Namespace).Eventf(service, v1.EventTypeWarning, "SyncExternalLoadBalancerFailed",
			"Error updating L4 External LoadBalancer, err: %v", err)
		syncResult.Error = err
		return syncResult
	}
	if lc.enableDualStack {
		lc.emitEnsuredDualStackEvent(service)

		if err = updateL4DualStackResourcesAnnotations(lc.ctx, service, syncResult.Annotations); err != nil {
			lc.ctx.Recorder(service.Namespace).Eventf(service, v1.EventTypeWarning, "SyncExternalLoadBalancerFailed",
				"Failed to update annotations for load balancer, err: %v", err)
			syncResult.Error = fmt.Errorf("failed to set resource annotations, err: %w", err)
			return syncResult
		}
	} else {
		lc.ctx.Recorder(service.Namespace).Eventf(service, v1.EventTypeNormal, "SyncLoadBalancerSuccessful",
			"Successfully ensured L4 External LoadBalancer resources")

		if err = updateL4ResourcesAnnotations(lc.ctx, service, syncResult.Annotations); err != nil {
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

func (lc *L4NetLBController) ensureBackendLinking(service *v1.Service) error {
	start := time.Now()
	klog.V(2).Infof("Linking backend service with instance groups for service %s/%s", service.Namespace, service.Name)
	defer func() {
		klog.V(2).Infof("Finished linking backend service with instance groups for service %s/%s, time taken: %v", service.Namespace, service.Name, time.Since(start))
	}()

	zones, err := lc.translator.ListZones(utils.CandidateNodesPredicate)
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

	return lc.igLinker.Link(servicePort, lc.ctx.Cloud.ProjectID(), zones)
}

func (lc *L4NetLBController) ensureInstanceGroups(service *v1.Service, nodeNames []string) error {
	// TODO(kl52752) Move instance creation and deletion logic to NodeController
	// to avoid race condition between controllers
	start := time.Now()
	klog.V(2).Infof("Ensuring instance groups for L4 NetLB Service %s/%s, len(nodeNames): %v", service.Namespace, service.Name, len(nodeNames))
	defer func() {
		klog.V(2).Infof("Finished ensuring instance groups for L4 NetLB Service %s/%s, time taken: %v", service.Namespace, service.Name, time.Since(start))
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
func (lc *L4NetLBController) garbageCollectRBSNetLB(key string, svc *v1.Service) *loadbalancers.L4NetLBSyncResult {
	startTime := time.Now()
	klog.Infof("Deleting L4 NetLB RBS service %s/%s", svc.Namespace, svc.Name)
	defer func() {
		klog.Infof("Finished deleting L4 NetLB service %s/%s, time taken: %v", svc.Namespace, svc.Name, time.Since(startTime))
	}()

	l4NetLBParams := &loadbalancers.L4NetLBParams{
		Service:          svc,
		Cloud:            lc.ctx.Cloud,
		Namer:            lc.namer,
		Recorder:         lc.ctx.Recorder(svc.Namespace),
		DualStackEnabled: lc.enableDualStack,
	}
	l4netLB := loadbalancers.NewL4NetLB(l4NetLBParams)
	lc.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeNormal, "DeletingLoadBalancer",
		"Deleting L4 External LoadBalancer for %s", key)

	result := l4netLB.EnsureLoadBalancerDeleted(svc)
	if result.Error != nil {
		lc.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeWarning, "DeleteLoadBalancerFailed",
			"Error deleting L4 External LoadBalancer, err: %v", result.Error)
		return result
	}

	if err := updateServiceStatus(lc.ctx, svc, &v1.LoadBalancerStatus{}); err != nil {
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

	if err := common.EnsureDeleteServiceFinalizer(svc, common.NetLBFinalizerV2, lc.ctx.KubeClient); err != nil {
		lc.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeWarning, "DeleteLoadBalancerFailed",
			"Error removing finalizer from L4 External LoadBalancer, err: %v", err)
		result.Error = fmt.Errorf("Failed to remove L4 External LoadBalancer finalizer, err: %w", err)
		return result
	}

	// IMPORTANT: Remove LB annotations from the Service LAST, cause changing service fields are emitted as service update to other controllers
	if lc.enableDualStack {
		if err := deleteL4RBSDualStackAnnotations(lc.ctx, svc); err != nil {
			lc.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeWarning, "DeleteLoadBalancer",
				"Error removing Dual Stack resource annotations: %v: %v", err)
			result.Error = fmt.Errorf("failed to reset Dual Stack resource annotations, err: %w", err)
			return result
		}
	} else {
		if err := deleteL4RBSAnnotations(lc.ctx, svc); err != nil {
			lc.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeWarning, "DeleteLoadBalancer",
				"Error removing resource annotations: %v: %v", err)
			result.Error = fmt.Errorf("failed to reset resource annotations, err: %w", err)
			return result
		}
	}

	lc.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeNormal, "DeletedLoadBalancer", "Deleted L4 External LoadBalancer")
	return result
}

// publishMetrics sets controller metrics for NetLB services and pushes NetLB metrics based on sync type.
func (lc *L4NetLBController) publishMetrics(result *loadbalancers.L4NetLBSyncResult, name, namespace string) {
	namespacedName := types.NamespacedName{Name: name, Namespace: namespace}.String()
	switch result.SyncType {
	case loadbalancers.SyncTypeCreate, loadbalancers.SyncTypeUpdate:
		klog.V(4).Infof("External L4 Loadbalancer for Service %s ensured, updating its state %v in metrics cache", namespacedName, result.MetricsState)
		lc.ctx.ControllerMetrics.SetL4NetLBService(namespacedName, result.MetricsState)
		lc.publishSyncMetrics(result)
	case loadbalancers.SyncTypeDelete:
		// if service is successfully deleted, remove it from cache
		if result.Error == nil {
			klog.V(4).Infof("External L4 Loadbalancer for Service %s deleted, removing its state from metrics cache", namespacedName)
			lc.ctx.ControllerMetrics.DeleteL4NetLBService(namespacedName)
		}
		lc.publishSyncMetrics(result)
	default:
		klog.Warningf("Unknown sync type %q, skipping metrics", result.SyncType)
	}
}

func (lc *L4NetLBController) publishSyncMetrics(result *loadbalancers.L4NetLBSyncResult) {
	if result.Error == nil {
		metrics.PublishL4NetLBSyncSuccess(result.SyncType, result.StartTime)
		return
	}
	metrics.PublishL4NetLBSyncError(result.SyncType, result.GCEResourceInError, utils.GetErrorType(result.Error), result.StartTime)
}
