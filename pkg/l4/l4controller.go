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

package l4

import (
	"fmt"
	"reflect"
	"sync"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/backends"
	"k8s.io/ingress-gce/pkg/context"
	"k8s.io/ingress-gce/pkg/controller/translator"
	"k8s.io/ingress-gce/pkg/loadbalancers"
	"k8s.io/ingress-gce/pkg/metrics"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/common"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/ingress-gce/pkg/utils/patch"
	"k8s.io/klog"
	"k8s.io/kubernetes/pkg/apis/core/v1/helper"
	"time"
)

const (
	syncTypeCreate = "create"
	syncTypeUpdate = "update"
	syncTypeDelete = "delete"
	// The max tolerated delay between update being enqueued and sync being invoked.
	enqueueToSyncDelayThreshold = 15 * time.Minute
)

// L4Controller manages the create/update delete of all L4 Internal LoadBalancer services.
type L4Controller struct {
	ctx *context.ControllerContext
	// kubeClient, needed for attaching finalizer
	client        kubernetes.Interface
	svcQueue      utils.TaskQueue
	numWorkers    int
	serviceLister cache.Indexer
	nodeLister    listers.NodeLister
	stopCh        chan struct{}
	// needed for listing the zones in the cluster.
	translator *translator.Translator
	// needed for linking the NEG with the backend service for each ILB service.
	NegLinker   backends.Linker
	backendPool *backends.Backends
	namer       namer.L4ResourcesNamer
	// enqueueTracker tracks the latest time an update was enqueued
	enqueueTracker utils.TimeTracker
	// syncTracker tracks the latest time an enqueued service was synced
	syncTracker         utils.TimeTracker
	sharedResourcesLock sync.Mutex
}

// NewController creates a new instance of the L4 ILB controller.
func NewController(ctx *context.ControllerContext, stopCh chan struct{}) *L4Controller {
	if ctx.NumL4Workers <= 0 {
		klog.Infof("L4 Worker count has not been set, setting to 1")
		ctx.NumL4Workers = 1
	}
	l4c := &L4Controller{
		ctx:           ctx,
		client:        ctx.KubeClient,
		serviceLister: ctx.ServiceInformer.GetIndexer(),
		nodeLister:    listers.NewNodeLister(ctx.NodeInformer.GetIndexer()),
		stopCh:        stopCh,
		numWorkers:    ctx.NumL4Workers,
	}
	l4c.namer = ctx.L4Namer
	l4c.translator = translator.NewTranslator(ctx)
	l4c.backendPool = backends.NewPool(ctx.Cloud, l4c.namer)
	l4c.NegLinker = backends.NewNEGLinker(l4c.backendPool, negtypes.NewAdapter(ctx.Cloud), ctx.Cloud)

	l4c.svcQueue = utils.NewPeriodicTaskQueueWithMultipleWorkers("l4", "services", l4c.numWorkers, l4c.sync)

	ctx.ServiceInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			addSvc := obj.(*v1.Service)
			svcKey := utils.ServiceKeyFunc(addSvc.Namespace, addSvc.Name)
			needsILB, svcType := annotations.WantsL4ILB(addSvc)
			// Check for deletion since updates or deletes show up as Add when controller restarts.
			if needsILB || needsDeletion(addSvc) {
				klog.V(3).Infof("ILB Service %s added, enqueuing", svcKey)
				l4c.ctx.Recorder(addSvc.Namespace).Eventf(addSvc, v1.EventTypeNormal, "ADD", svcKey)
				l4c.svcQueue.Enqueue(addSvc)
				l4c.enqueueTracker.Track()
			} else {
				klog.V(4).Infof("Ignoring add for non-lb service %s based on %v", svcKey, svcType)
			}
		},
		// Deletes will be handled in the Update when the deletion timestamp is set.
		UpdateFunc: func(old, cur interface{}) {
			curSvc := cur.(*v1.Service)
			svcKey := utils.ServiceKeyFunc(curSvc.Namespace, curSvc.Name)
			oldSvc := old.(*v1.Service)
			needsUpdate := l4c.needsUpdate(oldSvc, curSvc)
			needsDeletion := needsDeletion(curSvc)
			if needsUpdate || needsDeletion {
				klog.V(3).Infof("Service %v changed, needsUpdate %v, needsDeletion %v, enqueuing", svcKey, needsUpdate, needsDeletion)
				l4c.svcQueue.Enqueue(curSvc)
				l4c.enqueueTracker.Track()
				return
			}
			// Enqueue ILB services periodically for reasserting that resources exist.
			needsILB, _ := annotations.WantsL4ILB(curSvc)
			if needsILB && reflect.DeepEqual(old, cur) {
				// this will happen when informers run a resync on all the existing services even when the object is
				// not modified.
				klog.V(3).Infof("Periodic enqueueing of %v", svcKey)
				l4c.svcQueue.Enqueue(curSvc)
				l4c.enqueueTracker.Track()
			}
		},
	})
	// TODO enhance this by looking at some metric from service controller to ensure it is up.
	// We cannot use existence of a backend service or other resource, since those are on a per-service basis.
	ctx.AddHealthCheck("service-controller health", l4c.checkHealth)
	return l4c
}

func (l4c *L4Controller) checkHealth() error {
	lastEnqueueTime := l4c.enqueueTracker.Get()
	lastSyncTime := l4c.syncTracker.Get()
	// if lastEnqueue time is more than 30 minutes before the last sync time, the controller is falling behind.
	// This indicates that the controller was stuck handling a previous update, or sync function did not get invoked.
	syncTimeLatest := lastEnqueueTime.Add(enqueueToSyncDelayThreshold)
	if lastSyncTime.After(syncTimeLatest) {
		msg := fmt.Sprintf("L4 ILB Sync happened at time %v - %v after enqueue time, threshold is %v", lastSyncTime, lastSyncTime.Sub(lastEnqueueTime), enqueueToSyncDelayThreshold)
		klog.Error(msg)
		// TODO return error here
	}
	return nil
}

func (l4c *L4Controller) Run() {
	defer l4c.shutdown()
	klog.Infof("Running L4 Controller with %d worker goroutines", l4c.numWorkers)
	l4c.svcQueue.Run()
	<-l4c.stopCh
}

// This should only be called when the process is being terminated.
func (l4c *L4Controller) shutdown() {
	klog.Infof("Shutting down L4 Service Controller")
	l4c.svcQueue.Shutdown()
}

// processServiceCreateOrUpdate ensures load balancer resources for the given service, as needed.
// Returns an error if processing the service update failed.
func (l4c *L4Controller) processServiceCreateOrUpdate(key string, service *v1.Service) error {
	// skip services that are being handled by the legacy service controller.
	if utils.IsLegacyL4ILBService(service) {
		klog.Warningf("Ignoring update for service %s:%s managed by service controller", service.Namespace, service.Name)
		l4c.ctx.Recorder(service.Namespace).Eventf(service, v1.EventTypeWarning, "SyncLoadBalancerSkipped",
			fmt.Sprintf("skipping l4 load balancer sync as service contains '%s' finalizer", common.LegacyILBFinalizer))
		return nil
	}

	var serviceMetricsState metrics.L4ILBServiceState
	// Mark the service InSuccess state as false to begin with.
	// This will be updated to true if the VIP is configured successfully.
	serviceMetricsState.InSuccess = false

	// If service already has an IP assigned, treat it as an update instead of a new Loadbalancer.
	// This will also cover cases where an external LB is updated to an ILB, which is technically a create for ILB.
	// But this is still the easiest way to identify create vs update in the common case.
	syncType := syncTypeCreate
	if len(service.Status.LoadBalancer.Ingress) > 0 {
		syncType = syncTypeUpdate
	}
	startTime := time.Now()
	defer func() {
		l4c.ctx.ControllerMetrics.SetL4ILBService(types.NamespacedName{Name: service.Name, Namespace: service.Namespace}.String(), serviceMetricsState)
		metrics.PublishL4ILBSyncLatency(serviceMetricsState.InSuccess, syncType, startTime)
	}()

	// Ensure v2 finalizer
	if err := common.EnsureServiceFinalizer(service, common.ILBFinalizerV2, l4c.ctx.KubeClient); err != nil {
		return fmt.Errorf("Failed to attach finalizer to service %s/%s, err %w", service.Namespace, service.Name, err)
	}
	l4 := loadbalancers.NewL4Handler(service, l4c.ctx.Cloud, meta.Regional, l4c.namer, l4c.ctx.Recorder(service.Namespace), &l4c.sharedResourcesLock)
	nodeNames, err := utils.GetReadyNodeNames(l4c.nodeLister)
	if err != nil {
		return err
	}
	// Use the same function for both create and updates. If controller crashes and restarts,
	// all existing services will show up as Service Adds.
	status, annotationsMap, err := l4.EnsureInternalLoadBalancer(nodeNames, service, &serviceMetricsState)
	if err != nil {
		l4c.ctx.Recorder(service.Namespace).Eventf(service, v1.EventTypeWarning, "SyncLoadBalancerFailed",
			"Error syncing load balancer: %v", err)
		return err
	}
	if status == nil {
		l4c.ctx.Recorder(service.Namespace).Eventf(service, v1.EventTypeWarning, "SyncLoadBalancerFailed",
			"Empty status returned, even though there were no errors")
		return fmt.Errorf("service status returned by EnsureInternalLoadBalancer for %s is nil",
			l4.NamespacedName.String())
	}
	if err = l4c.linkNEG(l4); err != nil {
		l4c.ctx.Recorder(service.Namespace).Eventf(service, v1.EventTypeWarning, "SyncLoadBalancerFailed",
			"Failed to link NEG with Backend Service for load balancer, err: %v", err)
		return err
	}
	err = l4c.updateServiceStatus(service, status)
	if err != nil {
		l4c.ctx.Recorder(service.Namespace).Eventf(service, v1.EventTypeWarning, "SyncLoadBalancerFailed",
			"Error updating load balancer status: %v", err)
		return err
	}
	l4c.ctx.Recorder(service.Namespace).Eventf(service, v1.EventTypeNormal, "SyncLoadBalancerSuccessful",
		"Successfully ensured load balancer resources")
	if err = l4c.updateAnnotations(service, annotationsMap); err != nil {
		l4c.ctx.Recorder(service.Namespace).Eventf(service, v1.EventTypeWarning, "SyncLoadBalancerFailed",
			"Failed to update annotations for load balancer, err: %v", err)
		return fmt.Errorf("failed to set resource annotations, err: %w", err)
	}
	return nil
}

func (l4c *L4Controller) processServiceDeletion(key string, svc *v1.Service) error {
	l4 := loadbalancers.NewL4Handler(svc, l4c.ctx.Cloud, meta.Regional, l4c.namer, l4c.ctx.Recorder(svc.Namespace), &l4c.sharedResourcesLock)
	l4c.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeNormal, "DeletingLoadBalancer", "Deleting load balancer for %s", key)
	startTime := time.Now()
	if err := l4.EnsureInternalLoadBalancerDeleted(svc); err != nil {
		l4c.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeWarning, "DeleteLoadBalancerFailed", "Error deleting load balancer: %v", err)
		metrics.PublishL4ILBSyncLatency(false, syncTypeDelete, startTime)
		return err
	}
	// Also remove any ILB annotations from the service metadata
	if err := l4c.updateAnnotations(svc, nil); err != nil {
		l4c.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeWarning, "DeleteLoadBalancer",
			"Error resetting resource annotations for load balancer: %v", err)
		return fmt.Errorf("failed to reset resource annotations, err: %w", err)
	}
	if err := common.EnsureDeleteServiceFinalizer(svc, common.ILBFinalizerV2, l4c.ctx.KubeClient); err != nil {
		l4c.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeWarning, "DeleteLoadBalancerFailed",
			"Error removing finalizer from load balancer: %v", err)
		return fmt.Errorf("failed to remove ILB finalizer, err: %w", err)
	}

	namespacedName := types.NamespacedName{Name: svc.Name, Namespace: svc.Namespace}
	klog.V(6).Infof("Internal L4 Loadbalancer for Service %s deleted, removing its state from metrics cache", namespacedName)
	l4c.ctx.ControllerMetrics.DeleteL4ILBService(namespacedName.String())
	metrics.PublishL4ILBSyncLatency(true, syncTypeDelete, startTime)

	// Reset the loadbalancer status, Ignore NotFound error since the service can already be deleted at this point.
	if err := l4c.updateServiceStatus(svc, &v1.LoadBalancerStatus{}); err != nil && !errors.IsNotFound(err) {
		l4c.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeWarning, "DeleteLoadBalancer",
			"Error reseting load balancer status to empty: %v", err)
		return fmt.Errorf("failed to reset ILB status, err: %w", err)
	}
	l4c.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeNormal, "DeletedLoadBalancer", "Deleted load balancer")
	return nil
}

// linkNEG associates the NEG to the backendService for the given L4 ILB service.
func (l4c *L4Controller) linkNEG(l4 *loadbalancers.L4) error {
	// link neg to backend service
	zones, err := l4c.translator.ListZones()
	if err != nil {
		return nil
	}
	var groupKeys []backends.GroupKey
	for _, zone := range zones {
		groupKeys = append(groupKeys, backends.GroupKey{Zone: zone})
	}
	return l4c.NegLinker.Link(l4.ServicePort, groupKeys)
}

func (l4c *L4Controller) sync(key string) error {
	l4c.syncTracker.Track()
	svc, exists, err := l4c.ctx.Services().GetByKey(key)
	if err != nil {
		return fmt.Errorf("Failed to lookup service for key %s : %w", key, err)
	}
	if !exists || svc == nil {
		// The service will not exist if its resources and finalizer are handled by the legacy service controller and
		// it has been deleted. As long as the V2 finalizer is present, the service will not be deleted by apiserver.
		klog.V(3).Infof("Ignoring delete of service %s not managed by L4 controller", key)
		return nil
	}
	if needsDeletion(svc) {
		klog.V(2).Infof("Deleting ILB resources for service %s managed by L4 controller", key)
		return l4c.processServiceDeletion(key, svc)
	}
	// Check again here, to avoid time-of check, time-of-use race. A service deletion can get queued multiple times
	// as annotations change and a service to be deleted can incorrectly get requeued here. This can happen if svc had
	// finalizer when enqueuing, but when listing it here, the finalizer already got removed. It will skip needsDeletion
	// and queue-up here.
	if wantsILB, _ := annotations.WantsL4ILB(svc); wantsILB {
		klog.V(2).Infof("Ensuring ILB resources for service %s managed by L4 controller", key)
		return l4c.processServiceCreateOrUpdate(key, svc)
	}
	klog.V(3).Infof("Ignoring sync of service %s, neither delete nor ensure needed.", key)
	return nil
}

func (l4c *L4Controller) updateServiceStatus(svc *v1.Service, newStatus *v1.LoadBalancerStatus) error {
	if helper.LoadBalancerStatusEqual(&svc.Status.LoadBalancer, newStatus) {
		return nil
	}
	return patch.PatchServiceLoadBalancerStatus(l4c.ctx.KubeClient.CoreV1(), svc, *newStatus)
}

func (l4c *L4Controller) updateAnnotations(svc *v1.Service, newILBAnnotations map[string]string) error {
	newObjectMeta := svc.ObjectMeta.DeepCopy()
	newObjectMeta.Annotations = mergeAnnotations(newObjectMeta.Annotations, newILBAnnotations)
	if reflect.DeepEqual(svc.Annotations, newObjectMeta.Annotations) {
		return nil
	}
	klog.V(3).Infof("Patching annotations of service %v/%v", svc.Namespace, svc.Name)
	return patch.PatchServiceObjectMetadata(l4c.ctx.KubeClient.CoreV1(), svc, *newObjectMeta)
}

// mergeAnnotations merges the new set of ilb resource annotations with the pre-existing service annotations.
// Existing ILB resource annotation values will be replaced with the values in the new map.
func mergeAnnotations(existing, ilbAnnotations map[string]string) map[string]string {
	if existing == nil {
		existing = make(map[string]string)
	}
	// Delete existing ILB annotations.
	for _, key := range loadbalancers.ILBResourceAnnotationKeys {
		delete(existing, key)
	}
	// merge existing annotations with the newly added annotations
	for key, val := range ilbAnnotations {
		existing[key] = val
	}
	return existing
}

func needsDeletion(svc *v1.Service) bool {
	if !common.HasGivenFinalizer(svc.ObjectMeta, common.ILBFinalizerV2) {
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
	return false
}

func getPortsForLB(service *v1.Service) []*v1.ServicePort {
	ports := []*v1.ServicePort{}
	for i := range service.Spec.Ports {
		sp := &service.Spec.Ports[i]
		ports = append(ports, sp)
	}
	return ports
}

func portsEqualForLBService(x, y *v1.Service) bool {
	xPorts := getPortsForLB(x)
	yPorts := getPortsForLB(y)
	return portSlicesEqualForLB(xPorts, yPorts)
}

func portSlicesEqualForLB(x, y []*v1.ServicePort) bool {
	if len(x) != len(y) {
		return false
	}

	for i := range x {
		if !portEqualForLB(x[i], y[i]) {
			return false
		}
	}
	return true
}

func portEqualForLB(x, y *v1.ServicePort) bool {
	// TODO: Should we check name?  (In theory, an LB could expose it)
	if x.Name != y.Name {
		return false
	}

	if x.Protocol != y.Protocol {
		return false
	}

	if x.Port != y.Port {
		return false
	}

	if x.NodePort != y.NodePort {
		return false
	}

	if x.TargetPort != y.TargetPort {
		return false
	}

	return true
}
