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

package l4netlb

import (
	"fmt"
	"sync"
	"time"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	v1 "k8s.io/api/core/v1"
	listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/cloud-provider/service/helpers"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/backends"
	"k8s.io/ingress-gce/pkg/context"
	"k8s.io/ingress-gce/pkg/controller/translator"
	"k8s.io/ingress-gce/pkg/instances"
	"k8s.io/ingress-gce/pkg/loadbalancers"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/ingress-gce/pkg/utils/patch"
	"k8s.io/klog"
)

const (
	// The max tolerated delay between update being enqueued and sync being invoked.
	enqueueToSyncDelayThreshold = 15 * time.Minute
)

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
	syncTracker         utils.TimeTracker
	sharedResourcesLock sync.Mutex

	backendPool  *backends.Backends
	instancePool instances.NodePool
	igLinker     *backends.RegionalInstanceGroupLinker
}

// NewL4NetLBController creates a controller for l4 external loadbalancer.
func NewL4NetLBController(
	ctx *context.ControllerContext,
	stopCh chan struct{}) *L4NetLBController {
	if ctx.NumL4Workers <= 0 {
		klog.Infof("L4 Worker count has not been set, setting to 1")
		ctx.NumL4Workers = 1
	}

	backendPool := backends.NewPool(ctx.Cloud, ctx.L4Namer)
	instancePool := instances.NewNodePool(ctx.Cloud, ctx.ClusterNamer, ctx, utils.GetBasePath(ctx.Cloud))
	l4netLBc := &L4NetLBController{
		ctx:           ctx,
		serviceLister: ctx.ServiceInformer.GetIndexer(),
		nodeLister:    listers.NewNodeLister(ctx.NodeInformer.GetIndexer()),
		stopCh:        stopCh,
		translator:    translator.NewTranslator(ctx),
		backendPool:   backendPool,
		namer:         ctx.L4Namer,
		instancePool:  instancePool,
		igLinker:      backends.NewRegionalInstanceGroupLinker(instancePool, backendPool),
	}
	l4netLBc.svcQueue = utils.NewPeriodicTaskQueueWithMultipleWorkers("l4netLB", "services", ctx.NumL4Workers, l4netLBc.sync)

	ctx.ServiceInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			addSvc := obj.(*v1.Service)
			svcKey := utils.ServiceKeyFunc(addSvc.Namespace, addSvc.Name)
			needsNetLB, svcType := annotations.WantsL4NetLB(addSvc)
			//TODO (kl52752) Add check for deletion
			if needsNetLB {
				klog.V(3).Infof("L4 External LoadBalancer Service %s added, enqueuing", svcKey)
				l4netLBc.ctx.Recorder(addSvc.Namespace).Eventf(addSvc, v1.EventTypeNormal, "ADD", svcKey)
				l4netLBc.svcQueue.Enqueue(addSvc)
				l4netLBc.enqueueTracker.Track()
			} else {
				klog.V(4).Infof("Ignoring add for non external lb service %s based on %v", svcKey, svcType)
			}
		},
		// Deletes will be handled in the Update when the deletion timestamp is set.
		UpdateFunc: func(old, cur interface{}) {
			//TODO(kl52752) add implementation and check for deletion
		},
	})
	ctx.AddHealthCheck("service-controller health", l4netLBc.checkHealth)
	return l4netLBc
}

func (lc *L4NetLBController) checkHealth() error {
	lastEnqueueTime := lc.enqueueTracker.Get()
	lastSyncTime := lc.syncTracker.Get()
	// if lastEnqueue time is more than 15 minutes before the last sync time, the controller is falling behind.
	// This indicates that the controller was stuck handling a previous update, or sync function did not get invoked.
	syncTimeLatest := lastEnqueueTime.Add(enqueueToSyncDelayThreshold)
	if lastSyncTime.After(syncTimeLatest) {
		msg := fmt.Sprintf("L4 External LoadBalancer Sync happened at time %v - %v after enqueue time, threshold is %v", lastSyncTime, lastSyncTime.Sub(lastEnqueueTime), enqueueToSyncDelayThreshold)
		klog.Error(msg)
	}
	return nil
}

//Init inits instance Pool
func (lc *L4NetLBController) Init() {
	lc.instancePool.Init(lc.translator)
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
		return fmt.Errorf("Failed to lookup L4 External LoadBalancer service for key %s : %w", key, err)
	}
	if !exists || svc == nil {
		klog.V(3).Infof("Ignoring sync of non-existent service %s", key)
		return nil
	}
	var result *loadbalancers.SyncResultNetLB
	if wantsNetLB, _ := annotations.WantsL4NetLB(svc); wantsNetLB {
		result = lc.syncInternal(svc)
		if result == nil {
			// result will be nil if the service was ignored(due to presence of service controller finalizer).
			return nil
		}
		return result.Error
	}
	klog.V(3).Infof("Ignoring sync of service %s, neither delete nor ensure needed.", key)
	return nil
}

// syncInternal ensures load balancer resources for the given service, as needed.
// Returns an error if processing the service update failed.
func (lc *L4NetLBController) syncInternal(service *v1.Service) *loadbalancers.SyncResultNetLB {
	l4netlb := loadbalancers.NewL4NetLB(service, lc.ctx.Cloud, meta.Regional, lc.namer, lc.ctx.Recorder(service.Namespace), &lc.sharedResourcesLock)
	if !lc.shouldProcessService(service, l4netlb) {
		return nil
	}

	// #TODO(kl52752) Add ensure finalizer for NetLB
	nodeNames, err := utils.GetReadyNodeNames(lc.nodeLister)
	if err != nil {
		return &loadbalancers.SyncResultNetLB{Error: err}
	}

	if err := lc.ensureInstanceGroups(service, nodeNames); err != nil {
		lc.ctx.Recorder(service.Namespace).Eventf(service, v1.EventTypeWarning, "SyncInstanceGroupsFailed",
			"Error syncing instance group: %v", err)
		return &loadbalancers.SyncResultNetLB{Error: err}
	}

	// Use the same function for both create and updates. If controller crashes and restarts,
	// all existing services will show up as Service Adds.
	syncResult := l4netlb.EnsureFrontend(nodeNames, service)
	if syncResult.Error != nil {
		lc.ctx.Recorder(service.Namespace).Eventf(service, v1.EventTypeWarning, "SyncExternalLoadBalancerFailed",
			"Error ensuring Resource for L4 External LoadBalancer: %v", syncResult.Error)
		return syncResult
	}

	if err = lc.ensureBackendLinking(l4netlb.ServicePort); err != nil {
		lc.ctx.Recorder(service.Namespace).Eventf(service, v1.EventTypeWarning, "SyncExternalLoadBalancerFailed",
			"Error linking instance groups to backend service: %v", err)
		syncResult.Error = err
		return syncResult
	}

	err = lc.ensureServiceStatus(service, syncResult.Status)
	if err != nil {
		lc.ctx.Recorder(service.Namespace).Eventf(service, v1.EventTypeWarning, "SyncExternalLoadBalancerFailed",
			"Error updating L4 External LoadBalancer: %v", err)
		syncResult.Error = err
		return syncResult
	}
	lc.ctx.Recorder(service.Namespace).Eventf(service, v1.EventTypeNormal, "SyncLoadBalancerSuccessful",
		"Successfully ensured L4 External LoadBalancer resources")
	return nil
}

func (lc *L4NetLBController) ensureBackendLinking(port utils.ServicePort) error {
	zones, err := lc.translator.ListZones(utils.CandidateNodesPredicate)
	if err != nil {
		return err
	}
	return lc.igLinker.Link(port, lc.ctx.Cloud.ProjectID(), zones)
}

// shouldProcessService returns if the given LoadBalancer service should be processed by this controller.
func (lc *L4NetLBController) shouldProcessService(service *v1.Service, l4 *loadbalancers.L4NetLB) bool {
	//TODO(kl52752) add implementation
	return true
}

func (lc *L4NetLBController) ensureInstanceGroups(service *v1.Service, nodeNames []string) error {
	// TODO(kl52752) implement limit for 1000 nodes in instance group
	// TODO(kl52752) Move instance creation and deletion logic to NodeController
	// to avoid race condition between controllers
	_, _, nodePorts, _ := utils.GetPortsAndProtocol(service.Spec.Ports)
	_, err := lc.instancePool.EnsureInstanceGroupsAndPorts(lc.ctx.ClusterNamer.InstanceGroup(), nodePorts)
	if err != nil {
		return err
	}
	return lc.instancePool.Sync(nodeNames)
}

func (lc *L4NetLBController) ensureServiceStatus(svc *v1.Service, newStatus *v1.LoadBalancerStatus) error {
	if helpers.LoadBalancerStatusEqual(&svc.Status.LoadBalancer, newStatus) {
		return nil
	}
	return patch.PatchServiceLoadBalancerStatus(lc.ctx.KubeClient.CoreV1(), svc, *newStatus)
}
