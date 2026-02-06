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

	"k8s.io/ingress-gce/pkg/l4resources"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/cloud-provider/service/helpers"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/context"
	l4metrics "k8s.io/ingress-gce/pkg/l4lb/metrics"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/common"
	"k8s.io/ingress-gce/pkg/utils/patch"
	"k8s.io/klog/v2"
)

const (
	// ReasonL4LBConfigAnnotationRemoved is used when the annotation for L4LBConfig is removed from the service.
	ReasonL4LBConfigAnnotationRemoved = "L4LBConfigAnnotationRemoved"
)

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
	newObjectMeta := computeNewAnnotationsIfNeeded(svc, newL4LBAnnotations, l4resources.L4ResourceAnnotationKeys)
	if newObjectMeta == nil {
		svcLogger.V(3).Info("Service annotations not changed, skipping patch for service")
		return nil
	}
	svcLogger.V(3).Info("Patching annotations of service")
	return patch.PatchServiceObjectMetadata(ctx.KubeClient.CoreV1(), svc, *newObjectMeta)
}

// updateL4DualStackResourcesAnnotations checks if new annotations should be added to dual-stack service and patch service metadata if needed.
func updateL4DualStackResourcesAnnotations(ctx *context.ControllerContext, svc *v1.Service, newL4LBAnnotations map[string]string, svcLogger klog.Logger) error {
	newObjectMeta := computeNewAnnotationsIfNeeded(svc, newL4LBAnnotations, l4resources.L4DualStackResourceAnnotationKeys)
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
func mergeConditions(existing, newConditions []metav1.Condition) []metav1.Condition {
	if existing == nil {
		return newConditions
	}

	existingMap := make(map[string]metav1.Condition)
	for _, cond := range existing {
		existingMap[cond.Type] = cond
	}

	for _, newCond := range newConditions {
		existingMap[newCond.Type] = newCond
	}

	mergedConditions := make([]metav1.Condition, 0, len(existingMap))
	for _, cond := range existingMap {
		mergedConditions = append(mergedConditions, cond)
	}
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
func updateServiceStatus(ctx *context.ControllerContext, svc *v1.Service, newStatus *v1.LoadBalancerStatus, newConditions []metav1.Condition, svcLogger klog.Logger) error {
	svcLogger.V(2).Info("Updating service status and conditions", "newStatus", fmt.Sprintf("%+v", newStatus), "newConditions", fmt.Sprintf("%+v", newConditions))

	mergedConditions := mergeConditions(svc.Status.Conditions, newConditions)
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
	if l4resources.IsUserError(err) {
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
