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

package controllers

import (
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"math/rand"
	"reflect"
	"sort"
	"sync"
	"time"

	"k8s.io/ingress-gce/pkg/l4/resources"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/cloud-provider/service/helpers"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/context"
	l4metrics "k8s.io/ingress-gce/pkg/l4/metrics"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/common"
	"k8s.io/ingress-gce/pkg/utils/patch"
	"k8s.io/klog/v2"
)

const (
	// ReasonL4LBConfigAnnotationRemoved is used when the annotation for L4LBConfig is removed from the service.
	ReasonL4LBConfigAnnotationRemoved = "L4LBConfigAnnotationRemoved"
)

// periodicEnqueuer re-enqueues L4 LB Services on informer resyncs, optionally
// spreading them over a window instead of enqueueing all of them on the tick.
//
// Without spreading, every resync re-enqueues every LoadBalancer Service of the
// cluster at once, and every controller instance whose resync tick is aligned
// with the ticks of other instances (e.g. after all of them recovered from the
// same cloud API outage) does so at the same moment, so the resync-driven cloud
// API calls arrive in bursts. With spreading, each Service gets a stable
// pseudo-random offset in [0, window): it is still re-reconciled exactly once
// per resync period, just phase-shifted, so the load of one instance is spread
// over the window and the aggregate load of many instances is uniform in time
// regardless of how their ticks are aligned. The offsets are salted with a
// per-process random value, so that equally named Services of different
// clusters do not share a phase.
//
// Pending delayed enqueues are tracked per Service key so that a real Add,
// Update, or Delete of a Service (or a subsequent resync, or controller
// shutdown) cancels any pending timer for that Service instead of letting a
// stale timer trigger a redundant reconciliation or short-circuit workqueue
// exponential backoff.
//
// This function is used by External and Internal L4 LB controllers.
type periodicEnqueuer struct {
	window time.Duration
	salt   uint64
	// afterFunc is time.AfterFunc, replaceable in tests.
	afterFunc func(time.Duration, func()) *time.Timer

	mu      sync.Mutex
	stopped bool
	timers  map[string]*periodicTimer
}

type periodicTimer struct {
	timer *time.Timer
}

// newPeriodicEnqueuer returns a periodicEnqueuer spreading periodic enqueues
// over spreadFactor*resyncPeriod. A spreadFactor <= 0 disables spreading.
func newPeriodicEnqueuer(resyncPeriod time.Duration, spreadFactor float64) *periodicEnqueuer {
	var window time.Duration
	if spreadFactor > 0 && resyncPeriod > 0 {
		window = time.Duration(spreadFactor * float64(resyncPeriod))
	}
	return &periodicEnqueuer{
		window:    window,
		salt:      rand.Uint64(),
		afterFunc: time.AfterFunc,
		timers:    make(map[string]*periodicTimer),
	}
}

// offset returns the stable offset of key within the spread window,
// or 0 if spreading is disabled.
func (p *periodicEnqueuer) offset(key string) time.Duration {
	if p.window <= 0 {
		return 0
	}
	h := fnv.New64a()
	var salt [8]byte
	binary.LittleEndian.PutUint64(salt[:], p.salt)
	h.Write(salt[:])
	h.Write([]byte(key))
	return time.Duration(h.Sum64() % uint64(p.window))
}

// enqueue adds svc to queue after its offset, or immediately if spreading is
// disabled. tracker is updated when the Service is actually enqueued, not when
// its enqueue is scheduled, so that the enqueue-to-sync health check of the
// controllers keeps measuring the time Services spend waiting in the queue.
func (p *periodicEnqueuer) enqueue(queue utils.TaskQueue, tracker *utils.TimeTracker, svc *v1.Service, logger klog.Logger) {
	key := utils.ServiceKeyFunc(svc.Namespace, svc.Name)
	delay := p.offset(key)
	if delay <= 0 {
		p.cancel(key)
		logger.V(3).Info("Periodic enqueueing of service")
		queue.Enqueue(svc)
		tracker.Track()
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopped {
		return
	}
	p.cancelLocked(key)
	pt := &periodicTimer{}
	p.timers[key] = pt
	logger.V(3).Info("Scheduling periodic enqueueing of service", "delay", delay)
	pt.timer = p.afterFunc(delay, func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.stopped || p.timers[key] != pt {
			return
		}
		delete(p.timers, key)
		if queue.NumRequeues(svc) > 0 {
			// The Service is already scheduled for a retry with backoff, which also
			// reconciles it. Enqueueing it now would short-circuit the backoff.
			logger.V(4).Info("Skipping periodic enqueueing of service currently in rate-limited retry", "delay", delay)
			return
		}
		logger.V(3).Info("Periodic enqueueing of service", "delay", delay)
		queue.Enqueue(svc)
		tracker.Track()
	})
}

// cancel stops and removes any pending periodic enqueue timer for key.
func (p *periodicEnqueuer) cancel(key string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cancelLocked(key)
}

// cancelLocked stops and removes any pending periodic enqueue timer for key.
// p.mu must be held.
func (p *periodicEnqueuer) cancelLocked(key string) {
	prev, ok := p.timers[key]
	if !ok {
		return
	}
	if prev.timer != nil {
		prev.timer.Stop()
	}
	delete(p.timers, key)
}

// cancelObj stops and removes any pending periodic enqueue timer for obj,
// which may be a *v1.Service or a cache.DeletedFinalStateUnknown tombstone.
func (p *periodicEnqueuer) cancelObj(obj interface{}) {
	if p == nil {
		return
	}
	if key, err := utils.KeyFunc(obj); err == nil {
		p.cancel(key)
	}
}

// stop cancels all pending periodic enqueue timers and prevents any new ones
// from being scheduled.
func (p *periodicEnqueuer) stop() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stopped = true
	for key := range p.timers {
		p.cancelLocked(key)
	}
}

// computeNewAnnotationsIfNeeded checks if new annotations should be added to service.
// If needed creates new service meta object.
// This function is used by External and Internal L4 LB controllers.
func computeNewAnnotationsIfNeeded(svc *v1.Service, newAnnotations map[string]string, keysToRemove []string) *metav1.ObjectMeta {
	newObjectMeta := svc.ObjectMeta.DeepCopy()
	newObjectMeta.Annotations = mergeAnnotations(newObjectMeta.Annotations, newAnnotations, keysToRemove)
	if reflect.DeepEqual(svc.Annotations, newObjectMeta.Annotations) {
		return nil
	}
	return newObjectMeta
}

// mergeAnnotations merges the new set of l4lb resource annotations with the preexisting service annotations.
// Existing L4 resource annotation values will be replaced with the values in the new map.
// This function is used by External and Internal L4 LB controllers.
func mergeAnnotations(existing, lbAnnotations map[string]string, keysToRemove []string) map[string]string {
	if existing == nil {
		existing = make(map[string]string)
	} else {
		// Delete existing annotations.
		for _, key := range keysToRemove {
			delete(existing, key)
		}
	}
	// merge existing annotations with the newly added annotations
	for key, val := range lbAnnotations {
		existing[key] = val
	}
	return existing
}

// updateL4ResourcesAnnotations checks if new annotations should be added to service and patch service metadata if needed.
func updateL4ResourcesAnnotations(ctx *context.ControllerContext, svc *v1.Service, newL4LBAnnotations map[string]string, svcLogger klog.Logger) error {
	svcLogger.V(3).Info("Updating annotations of service")
	newObjectMeta := computeNewAnnotationsIfNeeded(svc, newL4LBAnnotations, resources.L4ResourceAnnotationKeys)
	if newObjectMeta == nil {
		svcLogger.V(3).Info("Service annotations not changed, skipping patch for service")
		return nil
	}
	svcLogger.V(3).Info("Patching annotations of service")
	return patch.PatchServiceObjectMetadata(ctx.KubeClient.CoreV1(), svc, *newObjectMeta)
}

// updateL4DualStackResourcesAnnotations checks if new annotations should be added to dual-stack service and patch service metadata if needed.
func updateL4DualStackResourcesAnnotations(ctx *context.ControllerContext, svc *v1.Service, newL4LBAnnotations map[string]string, svcLogger klog.Logger) error {
	newObjectMeta := computeNewAnnotationsIfNeeded(svc, newL4LBAnnotations, resources.L4DualStackResourceAnnotationKeys)
	if newObjectMeta == nil {
		return nil
	}
	svcLogger.V(3).Info("Patching annotations of service")
	return patch.PatchServiceObjectMetadata(ctx.KubeClient.CoreV1(), svc, *newObjectMeta)
}

func deleteAnnotation(ctx *context.ControllerContext, svc *v1.Service, annotationKey string, svcLogger klog.Logger) error {
	newObjectMeta := svc.ObjectMeta.DeepCopy()
	if _, ok := newObjectMeta.Annotations[annotationKey]; !ok {
		return nil
	}
	svcLogger.V(3).Info("Removing annotation from service", "annotationKey", annotationKey)
	delete(newObjectMeta.Annotations, annotationKey)
	return patch.PatchServiceObjectMetadata(ctx.KubeClient.CoreV1(), svc, *newObjectMeta)
}

// mergeConditions merges the new set of l4lb resource conditions with the preexisting service conditions.
// Existing L4 resource condition values will be replaced with the values in the new map.
// Conditions listed in conditionsToRemove will be deleted.
// Listing a condition both in newConditions and conditionsToRemove will remove
// the condition first and then add a new one. In particular, if a condition of
// this type existed it LastTransitionTime will be replaced.
func mergeConditions(existing, newConditions []metav1.Condition, conditionsToRemove []string) []metav1.Condition {
	existingMap := make(map[string]metav1.Condition)
	for _, cond := range existing {
		existingMap[cond.Type] = cond
	}

	for _, condType := range conditionsToRemove {
		delete(existingMap, condType)
	}

	for _, newCond := range newConditions {
		oldCond, exists := existingMap[newCond.Type]
		if exists && oldCond.Status == newCond.Status {
			newCond.LastTransitionTime = oldCond.LastTransitionTime
		}
		existingMap[newCond.Type] = newCond
	}

	if len(existingMap) == 0 {
		return nil
	}

	mergedConditions := make([]metav1.Condition, 0, len(existingMap))
	for _, cond := range existingMap {
		mergedConditions = append(mergedConditions, cond)
	}
	sort.Slice(mergedConditions, func(i, j int) bool {
		return mergedConditions[i].Type < mergedConditions[j].Type
	})
	return mergedConditions
}

// conditionsEqual checks if load balancer conditions are equal
func conditionsEqual(l, r []metav1.Condition) bool {
	if len(l) != len(r) {
		return false
	}
	lMap := make(map[string]metav1.Condition)
	for _, cond := range l {
		lMap[cond.Type] = cond
	}
	for _, condR := range r {
		condL, found := lMap[condR.Type]
		if !found {
			return false
		}
		if condL.Status != condR.Status || condL.Reason != condR.Reason || condL.Message != condR.Message {
			return false
		}
	}
	return true
}

// updateServiceStatus this faction checks if LoadBalancer status changed and patch service if needed.
func updateServiceStatus(ctx *context.ControllerContext, svc *v1.Service, newStatus *v1.LoadBalancerStatus, newConditions []metav1.Condition, conditionsToRemove []string, svcLogger klog.Logger) error {
	svcLogger.V(2).Info("Updating service status and conditions", "newStatus", fmt.Sprintf("%+v", newStatus), "newConditions", fmt.Sprintf("%+v", newConditions), "conditionsToRemove", conditionsToRemove)

	mergedConditions := mergeConditions(svc.Status.Conditions, newConditions, conditionsToRemove)
	svcLogger.V(2).Info("Merged conditions", "mergedConditions", fmt.Sprintf("%+v", mergedConditions))

	lbStatusEqual := helpers.LoadBalancerStatusEqual(&svc.Status.LoadBalancer, newStatus)
	lbConditionsEqual := conditionsEqual(svc.Status.Conditions, mergedConditions)

	if !lbStatusEqual || !lbConditionsEqual {
		svcLogger.V(2).Info("Patching LoadBalancer status and Conditions", "newStatus", fmt.Sprintf("%+v", newStatus), "newConditions", fmt.Sprintf("%+v", newConditions))
		return patch.PatchServiceStatus(ctx.KubeClient.CoreV1(), svc, v1.ServiceStatus{
			LoadBalancer: *newStatus,
			Conditions:   mergedConditions,
		})
	}
	svcLogger.V(3).Info("Service status not changed, skipping patch for service")
	return nil
}

// isHealthCheckDeleted checks if given health check exists in GCE
func isHealthCheckDeleted(cloud *gce.Cloud, hcName string, logger klog.Logger) bool {
	_, err := composite.GetHealthCheck(cloud, meta.GlobalKey(hcName), meta.VersionGA, logger)
	return utils.IsNotFoundError(err)
}

func skipUserError(err error, svcLogger klog.Logger) error {
	if resources.IsUserError(err) {
		svcLogger.Info("Sync failed with user-caused error", "err", err)
		return nil
	}
	return err
}

// warnL4FinalizerRemoved iterates across L4 specific finalizers and:
// * emits a warning event,
// * increases metric counter
// for finalizers that were removed.
func warnL4FinalizerRemoved(ctx *context.ControllerContext, oldService, newService *v1.Service) {

	l4FinalizersWithMetrics := map[string]func(){
		common.LegacyILBFinalizer:           l4metrics.PublishL4RemovedILBLegacyFinalizer,
		common.ILBFinalizerV2:               l4metrics.PublishL4RemovedILBFinalizer,
		common.NetLBFinalizerV2:             l4metrics.PublishL4RemovedNetLBRBSFinalizer,
		common.LoadBalancerCleanupFinalizer: l4metrics.PublishL4ServiceCleanupFinalizer,
	}
	for finalizer, metricFunction := range l4FinalizersWithMetrics {
		if finalizerWasRemovedUnexpectedly(oldService, newService, finalizer) {
			ctx.Recorder(newService.Namespace).Eventf(newService, v1.EventTypeWarning, "UnexpectedlyRemovedFinalizer",
				"Finalizer %v was unexpectedly removed from the service.", finalizer)
			metricFunction()
		}
	}
}

// finalizerWasRemoved returns true if old service had a given finalizer and new doesn't.
func finalizerWasRemovedUnexpectedly(oldService, newService *v1.Service, finalizer string) bool {
	if oldService == nil || newService == nil {
		return false
	}
	oldSvcHasLegacyFinalizer := common.HasGivenFinalizer(oldService.ObjectMeta, finalizer)
	newSvcHasLegacyFinalizer := common.HasGivenFinalizer(newService.ObjectMeta, finalizer)
	// If the service was added for deletion, we don't need finalizers
	svcToBeDeleted := newService.ObjectMeta.DeletionTimestamp != nil
	return oldSvcHasLegacyFinalizer && !newSvcHasLegacyFinalizer && !svcToBeDeleted
}
