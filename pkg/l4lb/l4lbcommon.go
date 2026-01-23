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
	ctx "context"
	"fmt"
	"reflect"

	api_errors "k8s.io/apimachinery/pkg/api/errors"

	"k8s.io/ingress-gce/pkg/l4annotations"
	"k8s.io/ingress-gce/pkg/l4lbconfig"
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

// updateServiceStatus this faction checks if LoadBalancer status changed and patch service if needed.
func updateServiceStatus(ctx *context.ControllerContext, svc *v1.Service, newStatus *v1.LoadBalancerStatus, svcLogger klog.Logger) error {
	svcLogger.V(2).Info("Updating service status", "newStatus", fmt.Sprintf("%+v", newStatus))
	if helpers.LoadBalancerStatusEqual(&svc.Status.LoadBalancer, newStatus) {
		svcLogger.V(2).Info("New and old statuses are equal, skipping patch")
		return nil
	}
	return patch.PatchServiceLoadBalancerStatus(ctx.KubeClient.CoreV1(), svc, *newStatus)
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

// updateL4LBConfig syncs the Backend Service logging state back to the L4LBConfig.
// This is used for back-syncing manual GCE changes to the source-of-truth L4LBConfig.
func updateL4LBConfig(controllerContext *context.ControllerContext, svc *v1.Service, bsLogConfig *composite.BackendServiceLogConfig, svcLogger klog.Logger) error {
	l4lbConfigName, configReferenced := l4annotations.FromService(svc).GetL4LBConfigAnnotation()

	// If no L4LBConfig is referenced, use the service name as the default L4LBConfig name.
	if !configReferenced {
		svcLogger.Info("No L4LBConfig annotation found on service, using service name as default L4LBConfig name")
		l4lbConfigName = svc.Name
	}

	existingSvcL4LBConfig, err := controllerContext.L4LBConfigClient.NetworkingV1().L4LBConfigs(svc.Namespace).Get(ctx.Background(), l4lbConfigName, metav1.GetOptions{})
	if err != nil {
		if api_errors.IsNotFound(err) {
			svcLogger.Info("Referenced L4LBConfig not found", "configName", l4lbConfigName)
			existingSvcL4LBConfig = nil
			// L4LBConfig not found, will proceed with back-sync creating a new one.
		} else {
			return fmt.Errorf("failed to fetch L4LBConfig %s: %w", l4lbConfigName, err)
		}
	}

	// Map GCE state to L4LBConfig Spec using existing utility
	updatedSvcL4LBConfig := l4lbconfig.UpdateL4LBConfigWithBackendServiceLogConfig(existingSvcL4LBConfig, bsLogConfig)

	// Update only if there is a semantic difference to maintain idempotency
	if l4lbconfig.IsL4LBConfigEqual(existingSvcL4LBConfig, updatedSvcL4LBConfig) {
		svcLogger.Info("L4LBConfig is already in sync with GCE state", "configName", l4lbConfigName)
		return nil
	}

	if existingSvcL4LBConfig == nil || updatedSvcL4LBConfig.Name == "" || updatedSvcL4LBConfig.Namespace == "" {
		updatedSvcL4LBConfig.Name = l4lbConfigName
		updatedSvcL4LBConfig.Namespace = svc.Namespace
	}

	svcLogger.Info("Back-syncing GCE logging state to L4LBConfig", "configName", l4lbConfigName)

	if !configReferenced {
		// Add annotation to service
		newSvc := svc.DeepCopy()
		l4annotations.SetL4LBConfigAnnotation(newSvc, l4lbConfigName)
		err = patch.PatchServiceObjectMetadata(controllerContext.KubeClient.CoreV1(), svc, newSvc.ObjectMeta)
		if err != nil {
			return fmt.Errorf("failed to patch L4LBConfig annotation to service %s/%s: %w", svc.Namespace, svc.Name, err)
		}
		svcLogger.Info("Patched L4LBConfig annotation to service", "configName", l4lbConfigName)
		svc.ObjectMeta.Annotations = newSvc.ObjectMeta.Annotations
	}

	if existingSvcL4LBConfig == nil {
		// Create L4LBConfig if it did not exist
		_, err = controllerContext.L4LBConfigClient.NetworkingV1().L4LBConfigs(svc.Namespace).Create(ctx.Background(), updatedSvcL4LBConfig, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("failed to create L4LBConfig %s: %w", l4lbConfigName, err)
		}

		controllerContext.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeNormal, "CreatedL4LBConfig",
			"Created L4LBConfig %s with logging settings from Backend Service", l4lbConfigName)
		svcLogger.Info("Created L4LBConfig", "configName", l4lbConfigName)
		return nil
	}

	_, err = controllerContext.L4LBConfigClient.NetworkingV1().L4LBConfigs(svc.Namespace).Update(ctx.Background(), updatedSvcL4LBConfig, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update L4LBConfig %s: %w", l4lbConfigName, err)
	}

	controllerContext.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeNormal, "UpdatedL4LBConfig",
		"Updated L4LBConfig %s logging settings to match Backend Service", l4lbConfigName)
	svcLogger.Info("Updated L4LBConfig", "configName", l4lbConfigName)
	return nil
}
