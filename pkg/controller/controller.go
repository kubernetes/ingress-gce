/*
Copyright 2015 The Kubernetes Authors.

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

package controller

import (
	"fmt"
	"math/rand"
	"net/http"
	"reflect"
	"sync"
	"time"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	apiv1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"
	unversionedcore "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/ingress-gce/pkg/annotations"
	backendconfigv1 "k8s.io/ingress-gce/pkg/apis/backendconfig/v1"
	frontendconfigv1beta1 "k8s.io/ingress-gce/pkg/apis/frontendconfig/v1beta1"
	negv1beta1 "k8s.io/ingress-gce/pkg/apis/svcneg/v1beta1"
	"k8s.io/ingress-gce/pkg/backends"
	"k8s.io/ingress-gce/pkg/common/operator"
	"k8s.io/ingress-gce/pkg/context"
	legacytranslator "k8s.io/ingress-gce/pkg/controller/translator"
	"k8s.io/ingress-gce/pkg/events"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/frontendconfig"
	"k8s.io/ingress-gce/pkg/healthchecks"
	"k8s.io/ingress-gce/pkg/instancegroups"
	"k8s.io/ingress-gce/pkg/loadbalancers"
	"k8s.io/ingress-gce/pkg/loadbalancers/features"
	"k8s.io/ingress-gce/pkg/metrics"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	ingsync "k8s.io/ingress-gce/pkg/sync"
	"k8s.io/ingress-gce/pkg/translator"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/common"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/ingress-gce/pkg/utils/zonegetter"
	"k8s.io/klog/v2"
)

// LoadBalancerController watches the kubernetes api and adds/removes services
// from the loadbalancer, via loadBalancerConfig.
type LoadBalancerController struct {
	ctx *context.ControllerContext

	// TODO: Watch secrets
	ingQueue   utils.TaskQueue
	Translator *legacytranslator.Translator
	stopCh     <-chan struct{}
	// stopLock is used to enforce only a single call to Stop is active.
	// Needed because we allow stopping through an http endpoint and
	// allowing concurrent stoppers leads to stack traces.
	stopLock sync.Mutex
	shutdown bool
	// hasSynced returns true if all associated sub-controllers have synced.
	// Abstracted into a func for testing.
	hasSynced func() bool

	// Resource pools.
	instancePool instancegroups.Manager
	l7Pool       loadbalancers.LoadBalancerPool

	// syncer implementation for backends
	backendSyncer *backends.Syncer
	// backendLock locks the SyncBackend function to avoid conflicts between
	// multiple ingress workers.
	backendLock sync.Mutex

	// gcLock locks the GC logics to avoid conflicts between multiple ingress workers.
	gcLock sync.Mutex

	// linker implementations for backends
	negLinker backends.Linker
	igLinker  backends.Linker

	// Ingress sync + GC implementation
	ingSyncer ingsync.Syncer

	// Ingress usage metrics.
	metrics metrics.IngressMetricsCollector

	ZoneGetter *zonegetter.ZoneGetter

	enableMultiSubnetClusterPhase1 bool

	backendPool *backends.Pool

	logger klog.Logger
}

// NewLoadBalancerController creates a controller for gce loadbalancers.
func NewLoadBalancerController(
	ctx *context.ControllerContext,
	stopCh <-chan struct{},
	logger klog.Logger,
) *LoadBalancerController {
	logger = logger.WithName("IngressController")

	broadcaster := record.NewBroadcaster()
	broadcaster.StartLogging(klog.Infof)
	broadcaster.StartRecordingToSink(&unversionedcore.EventSinkImpl{
		Interface: ctx.EventRecorderClient.CoreV1().Events(""),
	})

	healthChecker := healthchecks.NewHealthChecker(ctx.Cloud, ctx.HealthCheckPath, ctx.DefaultBackendSvcPort.ID.Service, ctx, ctx.Translator, healthchecks.HealthcheckFlags{
		EnableTHC: flags.F.EnableTransparentHealthChecks,
		EnableRecalculationOnBackendConfigRemoval: flags.F.EnableRecalculateUHCOnBCRemoval,
		THCPort: int64(flags.F.THCPort),
	})
	backendPool := backends.NewPool(ctx.Cloud, ctx.ClusterNamer)

	enableMultiSubnetClusterPhase1 := flags.F.EnableMultiSubnetClusterPhase1

	lbc := LoadBalancerController{
		ctx:                            ctx,
		Translator:                     ctx.Translator,
		stopCh:                         stopCh,
		hasSynced:                      ctx.HasSynced,
		instancePool:                   ctx.InstancePool,
		l7Pool:                         loadbalancers.NewLoadBalancerPool(ctx.Cloud, ctx.ClusterNamer, ctx, namer.NewFrontendNamerFactory(ctx.ClusterNamer, ctx.KubeSystemUID, logger), logger),
		backendSyncer:                  backends.NewBackendSyncer(backendPool, healthChecker, ctx.Cloud, ctx.Translator),
		negLinker:                      backends.NewNEGLinker(backendPool, negtypes.NewAdapter(ctx.Cloud), ctx.Cloud, ctx.SvcNegInformer.GetIndexer(), logger),
		igLinker:                       backends.NewInstanceGroupLinker(ctx.InstancePool, backendPool, logger),
		metrics:                        ctx.ControllerMetrics,
		ZoneGetter:                     ctx.ZoneGetter,
		enableMultiSubnetClusterPhase1: enableMultiSubnetClusterPhase1,
		backendPool:                    backendPool,
		logger:                         logger,
	}

	lbc.ingSyncer = ingsync.NewIngressSyncer(&lbc, logger)
	lbc.ingQueue = utils.NewPeriodicTaskQueueWithMultipleWorkers("ingress", "ingresses", flags.F.NumIngressWorkers, lbc.sync, logger)

	// Ingress event handlers.
	ctx.IngressInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			addIng := obj.(*v1.Ingress)
			ingLogger := logger.WithValues("ingressKey", common.NamespacedName(addIng))
			if !utils.IsGLBCIngress(addIng) {
				if !flags.F.EnableIngressGlobalExternal && annotations.FromIngress(addIng).IngressClass() == annotations.GceIngressClass {
					lbc.ctx.Recorder(addIng.Namespace).Eventf(addIng, apiv1.EventTypeWarning, events.SyncIngress, "Ingress class \"gce\" is not supported in this environment. Please use \"gce-regional-external\".")
				}
				ingLogger.Info("Ignoring add for ingress based on annotation", "annotation", annotations.IngressClassKey)
				return
			}

			ingLogger.Info("Ingress added, enqueuing")
			lbc.ctx.Recorder(addIng.Namespace).Eventf(addIng, apiv1.EventTypeNormal, events.SyncIngress, "Scheduled for sync")
			lbc.ingQueue.Enqueue(obj)
		},
		DeleteFunc: func(obj interface{}) {
			delIng := obj.(*v1.Ingress)
			if delIng == nil {
				logger.Error(nil, "Invalid object type", "type", fmt.Sprintf("%T", obj))
				return
			}
			ingLogger := logger.WithValues("ingressKey", common.NamespacedName(delIng))
			if delIng.ObjectMeta.DeletionTimestamp != nil {
				ingLogger.Info("Ignoring delete event for ingress, deletion will be handled via the finalizer")
				return
			}

			if !utils.IsGLBCIngress(delIng) {
				ingLogger.Info("Ignoring delete for ingress based on annotation", "annotation", annotations.IngressClassKey)
				return
			}

			ingLogger.Info("Ingress deleted, enqueueing")
			lbc.ingQueue.Enqueue(obj)
		},
		UpdateFunc: func(old, cur interface{}) {
			curIng := cur.(*v1.Ingress)
			ingLogger := logger.WithValues("ingressKey", common.NamespacedName(curIng))
			if !utils.IsGLBCIngress(curIng) {
				// Ingress needs to be enqueued if a ingress finalizer exists.
				// An existing finalizer means that
				// 1. Ingress update for class change.
				// 2. Ingress cleanup failed and re-queued.
				// 3. Finalizer remove failed and re-queued.
				if common.HasFinalizer(curIng.ObjectMeta) {
					ingLogger.Info("Ingress class was changed but has a glbc finalizer, enqueuing")
					lbc.ingQueue.Enqueue(cur)
					return
				}
				if !flags.F.EnableIngressGlobalExternal && annotations.FromIngress(curIng).IngressClass() == annotations.GceIngressClass {
					lbc.ctx.Recorder(curIng.Namespace).Eventf(curIng, apiv1.EventTypeWarning, events.SyncIngress, "Ingress class \"gce\" is not supported in this environment. Please use \"gce-regional-external\".")
				}
				return
			}
			if reflect.DeepEqual(old, cur) {
				ingLogger.Info("Periodic enqueueing of ingress")
			} else {
				ingLogger.Info("Ingress changed, enqueuing")
			}
			lbc.ctx.Recorder(curIng.Namespace).Eventf(curIng, apiv1.EventTypeNormal, events.SyncIngress, "Scheduled for sync")
			lbc.ingQueue.Enqueue(cur)
		},
	})

	// Service event handlers.
	ctx.ServiceInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			svc := obj.(*apiv1.Service)
			ings := operator.Ingresses(ctx.Ingresses().List()).ReferencesService(svc).AsList()
			lbc.ingQueue.Enqueue(convert(ings)...)
		},
		UpdateFunc: func(old, cur interface{}) {
			if !reflect.DeepEqual(old, cur) {
				svc := cur.(*apiv1.Service)
				ings := operator.Ingresses(ctx.Ingresses().List()).ReferencesService(svc).AsList()
				lbc.ingQueue.Enqueue(convert(ings)...)
			}
		},
		// Ingress deletes matter, service deletes don't.
	})

	// BackendConfig event handlers.
	ctx.BackendConfigInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			logger.Info("obj added", "type", fmt.Sprintf("%T", obj))
			beConfig := obj.(*backendconfigv1.BackendConfig)
			ings := operator.Ingresses(ctx.Ingresses().List()).ReferencesBackendConfig(beConfig, operator.Services(ctx.Services().List(), logger)).AsList()
			lbc.ingQueue.Enqueue(convert(ings)...)
		},
		UpdateFunc: func(old, cur interface{}) {
			if !reflect.DeepEqual(old, cur) {
				logger.Info("obj updated", "type", fmt.Sprintf("%T", cur))
				beConfig := cur.(*backendconfigv1.BackendConfig)
				ings := operator.Ingresses(ctx.Ingresses().List()).ReferencesBackendConfig(beConfig, operator.Services(ctx.Services().List(), logger)).AsList()
				lbc.ingQueue.Enqueue(convert(ings)...)
			}
		},
		DeleteFunc: func(obj interface{}) {
			logger.Info("obj deleted", "type", fmt.Sprintf("%T", obj))
			var beConfig *backendconfigv1.BackendConfig
			var ok, beOk bool
			beConfig, ok = obj.(*backendconfigv1.BackendConfig)
			if !ok {
				// This can happen if the watch is closed and misses the delete event
				state, stateOk := obj.(cache.DeletedFinalStateUnknown)
				if !stateOk {
					logger.Error(nil, "Wanted cache.DeleteFinalStateUnknown of backendconfig obj", "got", obj)
					return
				}

				beConfig, beOk = state.Obj.(*backendconfigv1.BackendConfig)
				if !beOk {
					logger.Error(nil, "Wanted backendconfig obj", "got", fmt.Sprintf("%+v", state.Obj))
					return
				}
			}

			ings := operator.Ingresses(ctx.Ingresses().List()).ReferencesBackendConfig(beConfig, operator.Services(ctx.Services().List(), logger)).AsList()
			lbc.ingQueue.Enqueue(convert(ings)...)
		},
	})

	// FrontendConfig event handlers.
	if ctx.FrontendConfigEnabled {
		ctx.FrontendConfigInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				feConfig := obj.(*frontendconfigv1beta1.FrontendConfig)
				ings := operator.Ingresses(ctx.Ingresses().List()).ReferencesFrontendConfig(feConfig).AsList()
				lbc.ingQueue.Enqueue(convert(ings)...)
			},
			UpdateFunc: func(old, cur interface{}) {
				if !reflect.DeepEqual(old, cur) {
					feConfig := cur.(*frontendconfigv1beta1.FrontendConfig)
					logger.Info("FrontendConfig updated", "feConfigName", klog.KRef(feConfig.Namespace, feConfig.Name))
					ings := operator.Ingresses(ctx.Ingresses().List()).ReferencesFrontendConfig(feConfig).AsList()
					lbc.ingQueue.Enqueue(convert(ings)...)
				}
			},
			DeleteFunc: func(obj interface{}) {
				var feConfig *frontendconfigv1beta1.FrontendConfig
				var ok, feOk bool
				feConfig, ok = obj.(*frontendconfigv1beta1.FrontendConfig)
				if !ok {
					// This can happen if the watch is closed and misses the delete event
					state, stateOk := obj.(cache.DeletedFinalStateUnknown)
					if !stateOk {
						logger.Error(nil, "Wanted cache.DeleteFinalStateUnknown of frontendconfig obj", "got", fmt.Sprintf("%+v", obj), "gotType", fmt.Sprintf("%T", obj))
						return
					}

					feConfig, feOk = state.Obj.(*frontendconfigv1beta1.FrontendConfig)
					if !feOk {
						logger.Error(nil, "Wanted frontendconfig obj", "got", fmt.Sprintf("%+v", state.Obj), "gotType", fmt.Sprintf("%T", state.Obj))
						return
					}
				}

				ings := operator.Ingresses(ctx.Ingresses().List()).ReferencesFrontendConfig(feConfig).AsList()
				lbc.ingQueue.Enqueue(convert(ings)...)
			},
		})
	}

	if enableMultiSubnetClusterPhase1 {
		// SvcNeg event handlers.
		ctx.SvcNegInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
			UpdateFunc: func(old, cur interface{}) {
				oldSvcNeg := old.(*negv1beta1.ServiceNetworkEndpointGroup)
				newSvcNeg := cur.(*negv1beta1.ServiceNetworkEndpointGroup)

				if !reflect.DeepEqual(oldSvcNeg.Status.NetworkEndpointGroups, newSvcNeg.Status.NetworkEndpointGroups) {
					logger.Info("svcneg updated", "namespace", newSvcNeg.Namespace, "name", newSvcNeg.Name)
					ings := operator.Ingresses(ctx.Ingresses().List()).ReferencesSvcNeg(newSvcNeg, ctx.Services()).AsList()
					lbc.ingQueue.Enqueue(convert(ings)...)
				}
			},
			// Similar to service handler, SvcNeg deletions do not matter.
		})
	}

	logger.Info("Created new loadbalancer controller")

	return &lbc
}

// SystemHealth performs a health check showing if ingress controller is healthy.
func (lbc *LoadBalancerController) SystemHealth() error {
	name := "k8s-ingress-svc-acct-permission-check-probe"
	version := meta.VersionGA
	var scope meta.KeyType = meta.Global
	beLogger := lbc.logger.WithValues("backendServiceName", name, "backendVersion", version, "backendScope", scope)
	_, err := lbc.backendPool.Get(name, meta.VersionGA, meta.Global, beLogger)

	// If this container is scheduled on a node without compute/rw it is
	// effectively useless, but it is healthy. Reporting it as unhealthy
	// will lead to container crashlooping.
	if utils.IsHTTPErrorCode(err, http.StatusForbidden) {
		lbc.logger.Info("Reporting cluster as healthy, but unable to list backends", "err", err)
		return nil
	}
	return utils.IgnoreHTTPNotFound(err)
}

// Run starts the loadbalancer controller.
func (lbc *LoadBalancerController) Run() {
	defer func() {
		lbc.Stop()
	}()
	lbc.logger.Info("Starting loadbalancer controller")
	go lbc.ingQueue.Run()

	<-lbc.stopCh
	lbc.logger.Info("Shutting down Loadbalancer Controller")
}

// Stop stops the loadbalancer controller. It also deletes cluster resources
// if deleteAll is true.
func (lbc *LoadBalancerController) Stop() {
	// Stop is invoked from the http endpoint.
	lbc.stopLock.Lock()
	defer lbc.stopLock.Unlock()

	// Only try draining the workqueue if we haven't already.
	if !lbc.shutdown {
		lbc.logger.Info("Shutting down controller queues.")
		lbc.ingQueue.Shutdown()
		lbc.shutdown = true
	}
}

// SyncBackends implements Controller.
func (lbc *LoadBalancerController) SyncBackends(state interface{}, ingLogger klog.Logger) error {
	// TODO: Only lock per resource
	// It is incredibly tricky to get an efficient synchronization method here.
	// For now, we are effectively making backend syncing single-threaded to avoid
	// any potential bugs such as deadlocks/memory leaks.
	// This lock is necessary because multiple ingresses may share backend resources
	// If two threads try to do the same operation, one will fail and mark the Ingress
	// as being in an error state in the UI
	lbc.backendLock.Lock()
	defer lbc.backendLock.Unlock()
	ingLogger = ingLogger.WithName("SyncBackends")

	// We expect state to be a syncState
	syncState, ok := state.(*syncState)
	if !ok {
		return fmt.Errorf("expected state type to be syncState, type was %T", state)
	}
	ingSvcPorts := syncState.urlMap.AllServicePorts()

	// Only sync instance group when IG is used for this ingress
	if len(nodePorts(ingSvcPorts)) > 0 {
		if err := lbc.syncInstanceGroup(syncState.ing, ingSvcPorts, ingLogger); err != nil {
			ingLogger.Error(err, "Failed to sync instance group", "ingress", syncState.ing)
			return err
		}
	} else {
		ingLogger.Info("Skip syncing instance groups")
	}

	// Sync the backends
	if err := lbc.backendSyncer.Sync(ingSvcPorts, ingLogger); err != nil {
		return err
	}

	// Get the zones our groups live in.
	zones, err := lbc.ZoneGetter.ListZones(zonegetter.CandidateNodesFilter, lbc.logger)
	if err != nil {
		ingLogger.Error(err, "lbc.ZoneGetter.List(zonegetter.CandidateNodesFilter)")
		return err
	}
	var groupKeys []backends.GroupKey
	for _, zone := range zones {
		groupKeys = append(groupKeys, backends.GroupKey{Zone: zone})
	}

	// Link backends to groups.
	for _, sp := range ingSvcPorts {
		var linkErr error
		if sp.NEGEnabled {
			// Link backend to NEG's if the backend has NEG enabled.
			linkErr = lbc.negLinker.Link(sp, groupKeys)
		} else {
			// Otherwise, link backend to IG's.
			linkErr = lbc.igLinker.Link(sp, groupKeys)
		}
		if linkErr != nil {
			return linkErr
		}
	}

	return nil
}

// syncInstanceGroup creates instance groups, syncs instances, sets named ports and updates instance group annotation
func (lbc *LoadBalancerController) syncInstanceGroup(ing *v1.Ingress, ingSvcPorts []utils.ServicePort, ingLogger klog.Logger) error {
	nodePorts := nodePorts(ingSvcPorts)
	ingLogger.Info("Syncing Instance Group", "nodePorts", nodePorts)
	igs, err := lbc.instancePool.EnsureInstanceGroupsAndPorts(lbc.ctx.ClusterNamer.InstanceGroup(), nodePorts, ingLogger)
	if err != nil {
		return err
	}

	nodes, err := lbc.ZoneGetter.ListNodes(zonegetter.CandidateNodesFilter, lbc.logger)
	if err != nil {
		return err
	}
	// Add/remove instances to the instance groups.
	if err = lbc.instancePool.Sync(utils.GetNodeNames(nodes), ingLogger); err != nil {
		return err
	}

	// TODO: Remove this after deprecation
	if utils.IsGCEMultiClusterIngress(ing) {
		ingLogger.Info("kubemci is used", "ingress", ing)
		// Add instance group names as annotation on the ingress and return.
		newAnnotations := ing.ObjectMeta.DeepCopy().Annotations
		if newAnnotations == nil {
			newAnnotations = make(map[string]string)
		}
		if err = setInstanceGroupsAnnotation(newAnnotations, igs); err != nil {
			return err
		}
		if err = updateAnnotations(lbc.ctx.KubeClient, ing, newAnnotations, ingLogger); err != nil {
			return err
		}
		// This short-circuit will stop the syncer from moving to next step.
		return ingsync.ErrSkipBackendsSync
	}
	return nil
}

// GCBackends implements Controller.
func (lbc *LoadBalancerController) GCBackends(toKeep []*v1.Ingress, ingLogger klog.Logger) error {
	// Only GCE ingress associated resources are managed by this controller.
	GCEIngresses := operator.Ingresses(toKeep).Filter(utils.IsGCEIngress).AsList()
	svcPortsToKeep := lbc.ToSvcPorts(GCEIngresses)
	if err := lbc.backendSyncer.GC(svcPortsToKeep, ingLogger); err != nil {
		return err
	}
	// TODO(ingress#120): Move this to the backend pool so it mirrors creation
	// Do not delete instance group if there exists a GLBC ingress.
	if len(toKeep) == 0 {
		igName := lbc.ctx.ClusterNamer.InstanceGroup()
		ingLogger.Info("Deleting instance group", "instanceGroup", igName)
		if err := lbc.instancePool.DeleteInstanceGroup(igName, ingLogger); err != err {
			return err
		}
	}
	return nil
}

// SyncLoadBalancer implements Controller.
func (lbc *LoadBalancerController) SyncLoadBalancer(state interface{}, ingLogger klog.Logger) error {
	ingLogger = ingLogger.WithName("SyncLoadBalancer")
	// We expect state to be a syncState
	syncState, ok := state.(*syncState)
	if !ok {
		return fmt.Errorf("expected state type to be syncState, type was %T", state)
	}

	lb, err := lbc.toRuntimeInfo(syncState.ing, syncState.urlMap, ingLogger)
	if err != nil {
		return err
	}

	// Create higher-level LB resources.
	l7, err := lbc.l7Pool.Ensure(lb)
	if err != nil {
		return err
	}

	syncState.l7 = l7
	return nil
}

// GCv1LoadBalancers implements Controller.
func (lbc *LoadBalancerController) GCv1LoadBalancers(toKeep []*v1.Ingress) error {
	return lbc.l7Pool.GCv1(common.ToIngressKeys(toKeep, lbc.logger))
}

// GCv2LoadBalancer implements Controller.
func (lbc *LoadBalancerController) GCv2LoadBalancer(ing *v1.Ingress, scope meta.KeyType) error {
	return lbc.l7Pool.GCv2(ing, scope)
}

// EnsureDeleteV1Finalizers implements Controller.
func (lbc *LoadBalancerController) EnsureDeleteV1Finalizers(toCleanup []*v1.Ingress, ingLogger klog.Logger) error {
	if !flags.F.FinalizerRemove {
		ingLogger.Info("Removing finalizers not enabled")
		return nil
	}
	for _, ing := range toCleanup {
		ingClient := lbc.ctx.KubeClient.NetworkingV1().Ingresses(ing.Namespace)
		if err := common.EnsureDeleteFinalizer(ing, ingClient, common.FinalizerKey, ingLogger); err != nil {
			ingLogger.Error(err, "Failed to ensure delete finalizer", "finalizer", common.FinalizerKey)
			return err
		}
	}
	return nil
}

// EnsureDeleteV2Finalizer implements Controller.
func (lbc *LoadBalancerController) EnsureDeleteV2Finalizer(ing *v1.Ingress, ingLogger klog.Logger) error {
	if !flags.F.FinalizerRemove {
		ingLogger.Info("Removing finalizers not enabled")
		return nil
	}
	ingClient := lbc.ctx.KubeClient.NetworkingV1().Ingresses(ing.Namespace)
	if err := common.EnsureDeleteFinalizer(ing, ingClient, common.FinalizerKeyV2, ingLogger); err != nil {
		ingLogger.Error(err, "Failed to ensure delete finalizer", "finalizer", common.FinalizerKeyV2)
		return err
	}
	return nil
}

// PostProcess implements Controller.
func (lbc *LoadBalancerController) PostProcess(state interface{}, ingLogger klog.Logger) error {
	ingLogger = ingLogger.WithName("PostProcess")
	// We expect state to be a syncState
	syncState, ok := state.(*syncState)
	if !ok {
		return fmt.Errorf("expected state type to be syncState, type was %T", state)
	}

	// Update the ingress status.
	return lbc.updateIngressStatus(syncState.l7, syncState.ing, ingLogger)
}

// preSyncGC is intended to execute GC logic before sync if necessary. e.g. Ingress ing has deletion timestamp.
// preSyncGC returns if the sync needs to take place or not.
func (lbc *LoadBalancerController) preSyncGC(key string, scope meta.KeyType, ingExists bool, ing *v1.Ingress, ingLogger klog.Logger) (bool, error) {
	lbc.gcLock.Lock()
	defer lbc.gcLock.Unlock()
	ingLogger = ingLogger.WithName("preSyncGC")

	ingLogger.Info("Running preSyncGC")
	defer ingLogger.Info("Finish preSyncGC")

	allIngresses := lbc.ctx.Ingresses().List()
	// Determine if the ingress needs to be GCed.
	if !ingExists || utils.NeedsCleanup(ing) {
		frontendGCAlgorithm := frontendGCAlgorithm(ingExists, false, ing, ingLogger)
		// GC will find GCE resources that were used for this ingress and delete them.
		err := lbc.ingSyncer.GC(allIngresses, ing, frontendGCAlgorithm, scope, ingLogger)
		// Skip emitting an event if ingress does not exist as we cannot retrieve ingress namespace.
		if err != nil && ingExists {
			ingLogger.Error(err, "Error in ingress GC")
			lbc.ctx.Recorder(ing.Namespace).Eventf(ing, apiv1.EventTypeWarning, events.GarbageCollection, "Error: %v", err)
		}
		// Delete the ingress state for metrics after GC is successful.
		if err == nil && ingExists {
			lbc.metrics.DeleteIngress(key)
		}
		return false, err
	}
	return true, nil
}

func (lbc *LoadBalancerController) gcRegionalIngressResources(ing *v1.Ingress, ingLogger klog.Logger) error {
	lbc.gcLock.Lock()
	defer lbc.gcLock.Unlock()
	ingLogger = ingLogger.WithName("gcRegionalIngressResources")

	ingLogger.Info("Running gcRegionalIngressResources")
	defer ingLogger.Info("Finish gcRegionalIngressResources")

	allIngresses := lbc.ctx.Ingresses().List()
	// Keep all ingresses, besides current one, that needs to be cleaned up.
	filteredIngresses := operator.Ingresses(allIngresses).Filter(func(curIng *v1.Ingress) bool {
		return curIng.Namespace != ing.Namespace && curIng.Name != ing.Name
	}).AsList()
	// Use strategy CleanupV2FrontendResourcesScopeChange which will only delete
	// GCP resources, but will leave Ingress (by not removing the finalizer).
	if gcErr := lbc.ingSyncer.GC(filteredIngresses, ing, utils.CleanupV2FrontendResourcesScopeChange, meta.Regional, ingLogger); gcErr != nil {
		lbc.ctx.Recorder(ing.Namespace).Eventf(ing, apiv1.EventTypeWarning, events.GarbageCollection, "Error during garbage collection: %v", gcErr)
		return fmt.Errorf("error during GC %v", gcErr)
	}
	return nil
}

// postSyncGC cleans up the unnecessary resources (backend-services, frontend resources in wrong scope) after sync.
func (lbc *LoadBalancerController) postSyncGC(key string, syncErr error, oldScope *meta.KeyType, newScope meta.KeyType, ingExists bool, ing *v1.Ingress, ingLogger klog.Logger) error {
	lbc.gcLock.Lock()
	defer lbc.gcLock.Unlock()
	ingLogger = ingLogger.WithName("postSyncGC")

	ingLogger.Info("Running postSyncGC")
	defer ingLogger.Info("Finish postSyncGC")

	// Garbage collection will occur regardless of an error occurring. If an error occurred,
	// it could have been caused by quota issues; therefore, garbage collecting now may
	// free up enough quota for the next sync to pass.
	allIngresses := lbc.ctx.Ingresses().List()
	frontendGCAlgorithm := frontendGCAlgorithm(ingExists, oldScope != nil, ing, ingLogger)
	if gcErr := lbc.ingSyncer.GC(allIngresses, ing, frontendGCAlgorithm, newScope, ingLogger); gcErr != nil {
		lbc.ctx.Recorder(ing.Namespace).Eventf(ing, apiv1.EventTypeWarning, events.GarbageCollection, "Error during garbage collection: %v", gcErr)
		return fmt.Errorf("error during sync %v, error during GC %v", syncErr, gcErr)
	}
	return syncErr
}

// sync manages Ingress create/updates/deletes events from queue.
func (lbc *LoadBalancerController) sync(key string) error {
	syncTrackingId := rand.Int31()
	ingLogger := lbc.logger.WithValues("ingressKey", key, "syncId", syncTrackingId)
	if !lbc.hasSynced() {
		time.Sleep(context.StoreSyncPollPeriod)
		return fmt.Errorf("waiting for stores to sync")
	}
	ingLogger.Info("Syncing ingress")

	ing, ingExists, err := lbc.ctx.Ingresses().GetByKey(key)
	if err != nil {
		return fmt.Errorf("error getting Ingress for key %s: %v", key, err)
	}

	// Capture GC state for ingress.
	scope := features.ScopeFromIngress(ing)
	needSync, err := lbc.preSyncGC(key, scope, ingExists, ing, ingLogger)
	if err != nil {
		return err
	}
	if !needSync {
		ingLogger.Info("Ingress does not need to be synced. Skipping sync")
		return nil
	}

	// Ensure that a finalizer is attached.
	if flags.F.FinalizerAdd {
		if ing, err = lbc.ensureFinalizer(ing, ingLogger); err != nil {
			return err
		}
	}

	if lbc.ctx.EnableIngressRegionalExternal {
		classNameChanged, err := lbc.l7Pool.DidRegionalClassChange(ing, ingLogger)
		if err != nil {
			return fmt.Errorf("failed checking regional class name change for ing %v, err: %w", ing, err)
		}
		if classNameChanged {
			ingLogger.Info("Detected Ingress class change, cleaning up old resources")
			err := lbc.gcRegionalIngressResources(ing, ingLogger)
			if err != nil {
				return fmt.Errorf("failed while handling ingress class name change. Ingress: %v, err: %w", ing, err)
			}
		}
	}

	// Bootstrap state for GCP sync.
	urlMap, errs, warnings := lbc.Translator.TranslateIngress(ing, lbc.ctx.DefaultBackendSvcPort.ID, lbc.ctx.ClusterNamer)

	if errs != nil {
		msg := fmt.Errorf("invalid ingress spec: %v", utils.JoinErrs(errs))
		lbc.ctx.Recorder(ing.Namespace).Eventf(ing, apiv1.EventTypeWarning, events.TranslateIngress, "Translation failed: %v", msg)
		return msg
	}

	if warnings {
		msg := "THC annotation is present for at least one Service, but the Transparent Health Checks feature is not enabled."
		lbc.ctx.Recorder(ing.Namespace).Event(ing, apiv1.EventTypeWarning, "THCAnnotationWithoutFlag", msg)
	}

	// Sync GCP resources.
	syncState := &syncState{urlMap, ing, nil}
	syncErr := lbc.ingSyncer.Sync(syncState, ingLogger)
	if syncErr != nil {
		lbc.ctx.Recorder(ing.Namespace).Eventf(ing, apiv1.EventTypeWarning, events.SyncIngress, "Error syncing to GCP: %v", syncErr.Error())
	} else {
		// Insert/update the ingress state for metrics after successful sync.
		var fc *frontendconfigv1beta1.FrontendConfig
		if flags.F.EnableFrontendConfig {
			fc, err = frontendconfig.FrontendConfigForIngress(lbc.ctx.FrontendConfigs().List(), ing)
			if err != nil {
				return err
			}
		}
		lbc.metrics.SetIngress(key, metrics.NewIngressState(ing, fc, urlMap.AllServicePorts()))
	}

	// Check for scope change GC
	var oldScope *meta.KeyType
	oldScope, err = lbc.l7Pool.FrontendScopeChangeGC(ing, ingLogger)
	if err != nil {
		return err
	}
	if oldScope != nil {
		scope = *oldScope
	}

	return lbc.postSyncGC(key, syncErr, oldScope, scope, ingExists, ing, ingLogger)
}

// updateIngressStatus updates the IP and annotations of a loadbalancer.
// The annotations are parsed by kubectl describe.
func (lbc *LoadBalancerController) updateIngressStatus(l7 *loadbalancers.L7, ing *v1.Ingress, ingLogger klog.Logger) error {
	ingClient := lbc.ctx.KubeClient.NetworkingV1().Ingresses(ing.Namespace)

	// Update IP through update/status endpoint
	ip := l7.GetIP()
	updatedIngStatus := v1.IngressStatus{
		LoadBalancer: v1.IngressLoadBalancerStatus{
			Ingress: []v1.IngressLoadBalancerIngress{
				{IP: ip},
			},
		},
	}
	if ip != "" {
		lbIPs := ing.Status.LoadBalancer.Ingress
		if len(lbIPs) == 0 || lbIPs[0].IP != ip {
			ingLogger.Info("Updating loadbalancer", "IP", ip)
			if _, err := common.PatchIngressStatus(ingClient, ing, updatedIngStatus, ingLogger); err != nil {
				ingLogger.Error(err, "PatchIngressStatus failed")
				return err
			}
			lbc.ctx.Recorder(ing.Namespace).Eventf(ing, apiv1.EventTypeNormal, events.IPChanged, "IP is now %v", ip)
		}
	}

	newAnnotations, err := loadbalancers.GetLBAnnotations(l7, ing.ObjectMeta.DeepCopy().Annotations, lbc.backendSyncer, ingLogger)
	if err != nil {
		return err
	}

	if err := updateAnnotations(lbc.ctx.KubeClient, ing, newAnnotations, ingLogger); err != nil {
		return err
	}
	return nil
}

// toRuntimeInfo returns L7RuntimeInfo for the given ingress.
func (lbc *LoadBalancerController) toRuntimeInfo(ing *v1.Ingress, urlMap *utils.GCEURLMap, ingLogger klog.Logger) (*loadbalancers.L7RuntimeInfo, error) {
	annotations := annotations.FromIngress(ing)
	env, err := translator.NewEnv(ing, lbc.ctx.KubeClient, "", "", "")
	if err != nil {
		return nil, fmt.Errorf("error initializing translator env: %v", err)
	}

	tls, errors := translator.ToTLSCerts(env)
	for _, err := range errors {
		if apierrors.IsNotFound(err) {
			msg := fmt.Sprintf("Could not find TLS certificate: %v", err)
			lbc.ctx.Recorder(ing.Namespace).Eventf(ing, apiv1.EventTypeWarning, events.SyncIngress, msg)
		} else {
			ingLogger.Error(err, "Could not get certificates")
			return nil, err
		}
	}

	// Setup HTTP-only if no valid TLS certs
	// The errors are assumed to be 404s since we short-circuit otherwise
	if len(tls) == 0 && len(errors) > 0 {
		// TODO: this path should be removed when external certificate managers migrate to a better solution.
		const msg = "Could not find any TLS certificates. Continuing setup for the load balancer to serve HTTP only. Note: this behavior is deprecated and will be removed in a future version of ingress-gce"
		lbc.ctx.Recorder(ing.Namespace).Eventf(ing, apiv1.EventTypeWarning, events.SyncIngress, msg)
	}

	var feConfig *frontendconfigv1beta1.FrontendConfig
	if lbc.ctx.FrontendConfigEnabled {
		feConfig, err = frontendconfig.FrontendConfigForIngress(lbc.ctx.FrontendConfigs().List(), ing)
		if err != nil {
			lbc.ctx.Recorder(ing.Namespace).Eventf(ing, apiv1.EventTypeWarning, events.SyncIngress, "Error: %v", err)
		}
		// Object in cache could be changed in-flight. Deepcopy to
		// reduce race conditions.
		feConfig = feConfig.DeepCopy()
	}

	staticIPName, err := annotations.StaticIPName()
	if err != nil {
		return nil, err
	}

	return &loadbalancers.L7RuntimeInfo{
		TLS:            tls,
		TLSName:        annotations.UseNamedTLS(),
		Ingress:        ing,
		AllowHTTP:      annotations.AllowHTTP(),
		StaticIPName:   staticIPName,
		UrlMap:         urlMap,
		FrontendConfig: feConfig,
	}, nil
}

func updateAnnotations(client kubernetes.Interface, ing *v1.Ingress, newAnnotations map[string]string, ingLogger klog.Logger) error {
	if reflect.DeepEqual(ing.Annotations, newAnnotations) {
		return nil
	}
	ingClient := client.NetworkingV1().Ingresses(ing.Namespace)
	newObjectMeta := ing.ObjectMeta.DeepCopy()
	newObjectMeta.Annotations = newAnnotations
	if _, err := common.PatchIngressObjectMetadata(ingClient, ing, *newObjectMeta, ingLogger); err != nil {
		ingLogger.Error(err, "PatchIngressObjectMetadata failed")
		return err
	}
	return nil
}

// ToSvcPorts returns a list of SVC ports given a list of ingresses.
// Note: This method is used for GC.
func (lbc *LoadBalancerController) ToSvcPorts(ings []*v1.Ingress) []utils.ServicePort {
	var knownPorts []utils.ServicePort
	for _, ing := range ings {
		urlMap, _, _ := lbc.Translator.TranslateIngress(ing, lbc.ctx.DefaultBackendSvcPort.ID, lbc.ctx.ClusterNamer)
		knownPorts = append(knownPorts, urlMap.AllServicePorts()...)
	}
	return knownPorts
}

// defaultFrontendNamingScheme returns frontend naming scheme for an ingress without finalizer.
// This is used for adding an appropriate finalizer on the ingress.
func (lbc *LoadBalancerController) defaultFrontendNamingScheme(ing *v1.Ingress) (namer.Scheme, error) {
	// Ingress frontend naming scheme is determined based on the following logic,
	// V2 frontend namer is disabled         : v1 frontend naming scheme
	// V2 frontend namer is enabled
	//     - VIP does not exists             : v2 frontend naming scheme
	//     - VIP exists
	//         - GCE URL Map exists          : v1 frontend naming scheme
	//         - GCE URL Map does not exists : v2 frontend naming scheme
	if !flags.F.EnableV2FrontendNamer {
		return namer.V1NamingScheme, nil
	}
	if !utils.HasVIP(ing) {
		return namer.V2NamingScheme, nil
	}
	urlMapExists, err := lbc.l7Pool.HasUrlMap(ing)
	if err != nil {
		return "", err
	}
	if urlMapExists {
		return namer.V1NamingScheme, nil
	}
	return namer.V2NamingScheme, nil
}

// ensureFinalizer ensures that a finalizer is attached.
func (lbc *LoadBalancerController) ensureFinalizer(ing *v1.Ingress, ingLogger klog.Logger) (*v1.Ingress, error) {
	ingLogger = ingLogger.WithName("ensureFinalizer")
	if common.HasFinalizer(ing.ObjectMeta) {
		ingLogger.Info("Finalizer exists")
		return ing, nil
	}
	namingScheme, err := lbc.defaultFrontendNamingScheme(ing)
	if err != nil {
		return nil, err
	}
	finalizerKey, err := namer.FinalizerForNamingScheme(namingScheme)
	if err != nil {
		return nil, err
	}
	ingClient := lbc.ctx.KubeClient.NetworkingV1().Ingresses(ing.Namespace)
	// Update ingress with finalizer so that load-balancer pool uses correct naming scheme
	// while ensuring frontend resources. Note that this updates only the finalizer annotation
	// which may be inconsistent with ingress store for a short period.
	updatedIng, err := common.EnsureFinalizer(ing, ingClient, finalizerKey, ingLogger)
	if err != nil {
		ingLogger.Error(err, "Failed to ensure finalizer", "finalizer", finalizerKey)
		return nil, err
	}
	return updatedIng, nil
}

// frontendGCAlgorithm returns the naming scheme using which frontend resources needs to be cleaned-up.
// This also returns a boolean to specify if we need to delete frontend resources.
// GC path is
// If ingress does not exist :   v1 frontends and all backends
// If ingress exists
//   - Needs cleanup
//   - If v1 naming scheme  :    v1 frontends and all backends
//   - If v2 naming scheme  :    v2 frontends and all backends
//   - Does not need cleanup
//   - Finalizer enabled    :    all backends
//   - Finalizer disabled   :    v1 frontends and all backends
//   - Scope changed        :    v2 frontends for all scope
func frontendGCAlgorithm(ingExists bool, scopeChange bool, ing *v1.Ingress, ingLogger klog.Logger) utils.FrontendGCAlgorithm {
	// If ingress does not exist, that means its pre-finalizer era.
	// Run GC via v1 naming scheme.
	if !ingExists {
		return utils.CleanupV1FrontendResources
	}
	// Determine if we do not need to delete current ingress.
	if !utils.NeedsCleanup(ing) {
		// GC backends only if current ingress does not need cleanup and finalizers is enabled.
		if flags.F.FinalizerAdd {
			if scopeChange {
				return utils.CleanupV2FrontendResourcesScopeChange
			}
			return utils.NoCleanUpNeeded
		}
		return utils.CleanupV1FrontendResources
	}
	namingScheme := namer.FrontendNamingScheme(ing, ingLogger)
	switch namingScheme {
	case namer.V2NamingScheme:
		return utils.CleanupV2FrontendResources
	case namer.V1NamingScheme:
		return utils.CleanupV1FrontendResources
	default:
		ingLogger.Error(nil, "Unexpected naming scheme", "scheme", namingScheme)
		return utils.NoCleanUpNeeded
	}
}
