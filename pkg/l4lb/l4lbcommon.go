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
	"context"
	"fmt"
	"reflect"

	svcLBStatus "github.com/GoogleCloudPlatform/gke-networking-api/apis/serviceloadbalancerstatus/v1"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/cloud-provider/service/helpers"
	"k8s.io/ingress-gce/pkg/composite"
	ctx "k8s.io/ingress-gce/pkg/context"
	l4metrics "k8s.io/ingress-gce/pkg/l4lb/metrics"
	"k8s.io/ingress-gce/pkg/loadbalancers"
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
func updateL4ResourcesAnnotations(ctx *ctx.ControllerContext, svc *v1.Service, newL4LBAnnotations map[string]string, svcLogger klog.Logger) error {
	svcLogger.V(3).Info("Updating annotations of service")
	newObjectMeta := computeNewAnnotationsIfNeeded(svc, newL4LBAnnotations, loadbalancers.L4ResourceAnnotationKeys)
	if newObjectMeta == nil {
		svcLogger.V(3).Info("Service annotations not changed, skipping patch for service")
		return nil
	}
	svcLogger.V(3).Info("Patching annotations of service")
	return patch.PatchServiceObjectMetadata(ctx.KubeClient.CoreV1(), svc, *newObjectMeta)
}

// updateL4DualStackResourcesAnnotations checks if new annotations should be added to dual-stack service and patch service metadata if needed.
func updateL4DualStackResourcesAnnotations(ctx *ctx.ControllerContext, svc *v1.Service, newL4LBAnnotations map[string]string, svcLogger klog.Logger) error {
	newObjectMeta := computeNewAnnotationsIfNeeded(svc, newL4LBAnnotations, loadbalancers.L4DualStackResourceAnnotationKeys)
	if newObjectMeta == nil {
		return nil
	}
	svcLogger.V(3).Info("Patching annotations of service")
	return patch.PatchServiceObjectMetadata(ctx.KubeClient.CoreV1(), svc, *newObjectMeta)
}

func deleteAnnotation(ctx *ctx.ControllerContext, svc *v1.Service, annotationKey string, svcLogger klog.Logger) error {
	newObjectMeta := svc.ObjectMeta.DeepCopy()
	if _, ok := newObjectMeta.Annotations[annotationKey]; !ok {
		return nil
	}
	svcLogger.V(3).Info("Removing annotation from service", "annotationKey", annotationKey)
	delete(newObjectMeta.Annotations, annotationKey)
	return patch.PatchServiceObjectMetadata(ctx.KubeClient.CoreV1(), svc, *newObjectMeta)
}

// updateServiceStatus this faction checks if LoadBalancer status changed and patch service if needed.
func updateServiceStatus(ctx *ctx.ControllerContext, svc *v1.Service, newStatus *v1.LoadBalancerStatus, svcLogger klog.Logger) error {
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
	if loadbalancers.IsUserError(err) {
		svcLogger.Info("Sync failed with user-caused error", "err", err)
		return nil
	}
	return err
}

// warnL4FinalizerRemoved iterates across L4 specific finalizers and:
// * emits a warning event,
// * increases metric counter
// for finalizers that were removed.
func warnL4FinalizerRemoved(ctx *ctx.ControllerContext, oldService, newService *v1.Service) {

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

// serviceLoadBalancerStatusSpecEqual checks if two ServiceLoadBalancerStatusSpec are equal.
// It compares GceResources as a multiset (bag), ignoring order.
func serviceLoadBalancerStatusSpecEqual(a, b svcLBStatus.ServiceLoadBalancerStatusSpec) bool {
	if !reflect.DeepEqual(a.ServiceRef, b.ServiceRef) {
		return false
	}

	if len(a.GceResources) != len(b.GceResources) {
		return false
	}

	resourceCounts := make(map[string]int, len(a.GceResources))
	for _, res := range a.GceResources {
		resourceCounts[res]++
	}

	for _, res := range b.GceResources {
		if count, ok := resourceCounts[res]; !ok || count == 0 {
			return false
		}
		resourceCounts[res]--
	}

	return true
}

// GenerateServiceLoadBalancerStatus creates a ServiceLoadBalancerStatus CR from a Service and GCEResources.
func GenerateServiceLoadBalancerStatus(service *v1.Service, gceResources []string) *svcLBStatus.ServiceLoadBalancerStatus {
	if service == nil {
		return nil
	}

	statusCR := &svcLBStatus.ServiceLoadBalancerStatus{
		ObjectMeta: metav1.ObjectMeta{
			Name:      service.Name + "-status",
			Namespace: service.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(service, v1.SchemeGroupVersion.WithKind("Service")),
			},
		},
		Spec: svcLBStatus.ServiceLoadBalancerStatusSpec{
			ServiceRef: svcLBStatus.ServiceReference{
				Kind:       "Service",
				Name:       service.Name,
				APIVersion: v1.SchemeGroupVersion.String(),
			},
			GceResources: gceResources,
		},
		Status: svcLBStatus.ServiceLoadBalancerStatusStatus{},
	}

	return statusCR
}

// ensureServiceLoadBalancerStatusCR ensures the ServiceLoadBalancerStatus CR
// exists for the given Service and that its Spec is up-to-date.
// If the list of GCE resources is empty, it ensures the CR is deleted.
func ensureServiceLoadBalancerStatusCR(ctx *ctx.ControllerContext, service *v1.Service, gceResourceURLs []string, logger klog.Logger) error {
	// Generate the desired state of the CR.
	desiredCR := GenerateServiceLoadBalancerStatus(service, gceResourceURLs)
	if desiredCR == nil {
		logger.V(4).Info("Generated ServiceLoadBalancerStatus CR is nil, skipping")
		return nil
	}
	crClient := ctx.SvcLBStatusClient.NetworkingV1().ServiceLoadBalancerStatuses(service.Namespace)

	// Try to get the existing CR from the cluster.
	existingCR, err := crClient.Get(context.TODO(), desiredCR.Name, metav1.GetOptions{})
	if err != nil {
		// If the CR does not exist, create it.
		if errors.IsNotFound(err) {
			logger.V(2).Info("ServiceLoadBalancerStatus CR not found, creating it", "crName", desiredCR.Name)
			logger.V(2).Info("Creating ServiceLoadBalancerStatus CR with spec", "spec", desiredCR.Spec)
			_, createErr := crClient.Create(context.TODO(), desiredCR, metav1.CreateOptions{})
			if createErr != nil {
				logger.Error(createErr, "Failed to create ServiceLoadBalancerStatus CR", "crName", desiredCR.Name)
				return createErr
			}
			logger.V(2).Info("Successfully created ServiceLoadBalancerStatus CR", "crName", desiredCR.Name)
			return nil
		}
		// For any other error, log and return it.
		logger.Error(err, "Failed to get ServiceLoadBalancerStatus CR", "crName", desiredCR.Name)
		return err
	}

	// If the CR already exists, check if an update is needed.
	if serviceLoadBalancerStatusSpecEqual(existingCR.Spec, desiredCR.Spec) {
		logger.V(3).Info("ServiceLoadBalancerStatus CR is already up-to-date", "crName", desiredCR.Name)
		return nil
	}

	// The Spec has changed, so update the CR.
	logger.V(2).Info("ServiceLoadBalancerStatus CR spec has changed, updating it", "crName", desiredCR.Name)
	logger.V(2).Info("Updating ServiceLoadBalancerStatus CR with new spec", "newSpec", desiredCR.Spec)
	// To update, we modify the existing object's Spec and send it back.
	existingCR.Spec = desiredCR.Spec
	_, updateErr := crClient.Update(context.TODO(), existingCR, metav1.UpdateOptions{})
	if updateErr != nil {
		logger.Error(updateErr, "Failed to update ServiceLoadBalancerStatus CR", "crName", desiredCR.Name)
		return updateErr
	}

	logger.V(2).Info("Successfully updated ServiceLoadBalancerStatus CR", "crName", desiredCR.Name)
	return nil
}
