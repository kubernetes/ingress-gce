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
	"reflect"

	"k8s.io/ingress-gce/pkg/l4/resources"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/context"
	l4metrics "k8s.io/ingress-gce/pkg/l4/metrics"
	"k8s.io/ingress-gce/pkg/l4/serviceconsistency"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/common"
	"k8s.io/ingress-gce/pkg/utils/consistency"
	"k8s.io/klog/v2"
)

const (
	// ReasonL4LBConfigAnnotationRemoved is used when the annotation for L4LBConfig is removed from the service.
	ReasonL4LBConfigAnnotationRemoved = "L4LBConfigAnnotationRemoved"
)

// ensureServiceFinalizer adds the finalizer to svc if it is missing and
// records the write in the ConsistencyStore.
func ensureServiceFinalizer(ctx *context.ControllerContext, store consistency.ConsistencyStore, svc *v1.Service, key string, svcLogger klog.Logger) error {
	patchedSvc, err := common.EnsureServiceFinalizer(svc, key, ctx.KubeClient, svcLogger)
	if err != nil {
		return err
	}
	serviceconsistency.RecordWrite(store, patchedSvc)
	return nil
}

// ensureDeleteServiceFinalizer removes the finalizer from svc if it is
// present and records the write in the ConsistencyStore.
func ensureDeleteServiceFinalizer(ctx *context.ControllerContext, store consistency.ConsistencyStore, svc *v1.Service, key string, svcLogger klog.Logger) error {
	patchedSvc, err := common.EnsureDeleteServiceFinalizer(svc, key, ctx.KubeClient, svcLogger)
	if err != nil {
		return err
	}
	serviceconsistency.RecordWrite(store, patchedSvc)
	return nil
}

// ensureServiceDeleteFinalizers removes the finalizers from svc that are
// present and records the write in the ConsistencyStore.
func ensureServiceDeleteFinalizers(ctx *context.ControllerContext, store consistency.ConsistencyStore, svc *v1.Service, keys []string, svcLogger klog.Logger) error {
	patchedSvc, err := common.EnsureServiceDeleteFinalizers(svc, keys, ctx.KubeClient, svcLogger)
	if err != nil {
		return err
	}
	serviceconsistency.RecordWrite(store, patchedSvc)
	return nil
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
func updateL4ResourcesAnnotations(ctx *context.ControllerContext, store consistency.ConsistencyStore, svc *v1.Service, newL4LBAnnotations map[string]string, svcLogger klog.Logger) error {
	svcLogger.V(3).Info("Updating annotations of service")
	newObjectMeta := computeNewAnnotationsIfNeeded(svc, newL4LBAnnotations, resources.L4ResourceAnnotationKeys)
	if newObjectMeta == nil {
		svcLogger.V(3).Info("Service annotations not changed, skipping patch for service")
		return nil
	}
	svcLogger.V(3).Info("Patching annotations of service")
	return serviceconsistency.PatchMetadata(ctx.KubeClient.CoreV1(), store, svc, *newObjectMeta)
}

// updateL4DualStackResourcesAnnotations checks if new annotations should be added to dual-stack service and patch service metadata if needed.
func updateL4DualStackResourcesAnnotations(ctx *context.ControllerContext, store consistency.ConsistencyStore, svc *v1.Service, newL4LBAnnotations map[string]string, svcLogger klog.Logger) error {
	newObjectMeta := computeNewAnnotationsIfNeeded(svc, newL4LBAnnotations, resources.L4DualStackResourceAnnotationKeys)
	if newObjectMeta == nil {
		return nil
	}
	svcLogger.V(3).Info("Patching annotations of service")
	return serviceconsistency.PatchMetadata(ctx.KubeClient.CoreV1(), store, svc, *newObjectMeta)
}

func deleteAnnotation(ctx *context.ControllerContext, store consistency.ConsistencyStore, svc *v1.Service, annotationKey string, svcLogger klog.Logger) error {
	newObjectMeta := svc.ObjectMeta.DeepCopy()
	if _, ok := newObjectMeta.Annotations[annotationKey]; !ok {
		return nil
	}
	svcLogger.V(3).Info("Removing annotation from service", "annotationKey", annotationKey)
	delete(newObjectMeta.Annotations, annotationKey)
	return serviceconsistency.PatchMetadata(ctx.KubeClient.CoreV1(), store, svc, *newObjectMeta)
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
