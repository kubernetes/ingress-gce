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

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/cloud-provider/service/helpers"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/context"
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
func computeNewAnnotationsIfNeeded(svc *v1.Service, newAnnotations map[string]string, enableDualStack bool) *metav1.ObjectMeta {
	var keysToRemove []string
	if enableDualStack {
		keysToRemove = loadbalancers.L4DualStackResourceAnnotationKeys
	} else {
		keysToRemove = loadbalancers.L4ResourceAnnotationKeys
	}
	newObjectMeta := svc.ObjectMeta.DeepCopy()
	newObjectMeta.Annotations = mergeAnnotations(newObjectMeta.Annotations, newAnnotations, keysToRemove)
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

func deleteAnnotation(ctx *context.ControllerContext, svc *v1.Service, annotationKey string, svcLogger klog.Logger) error {
	newObjectMeta := svc.ObjectMeta.DeepCopy()
	if _, ok := newObjectMeta.Annotations[annotationKey]; !ok {
		return nil
	}
	svcLogger.V(3).Info("Removing annotation from service", "annotationKey", annotationKey)
	delete(newObjectMeta.Annotations, annotationKey)
	return patch.PatchServiceObjectMetadata(ctx.KubeClient.CoreV1(), svc, newObjectMeta)
}

// updateServiceStatus this faction checks if LoadBalancer status changed and patch service if needed.
func updateServiceInformation(ctx *context.ControllerContext, enableDualStack bool, svc *v1.Service, newStatus *v1.LoadBalancerStatus, newL4LBAnnotations map[string]string, svcLogger klog.Logger) error {
	emitIpFamiliesStackEvent(ctx, svc)
	newObjectMeta := computeNewAnnotationsIfNeeded(svc, newL4LBAnnotations, enableDualStack)

	svcLogger.V(2).Info("Updating service information", "newStatus", fmt.Sprintf("%+v", newStatus), "newObjectMeta", newObjectMeta)
	if helpers.LoadBalancerStatusEqual(&svc.Status.LoadBalancer, newStatus) && reflect.DeepEqual(svc.ObjectMeta.Annotations, newObjectMeta.Annotations) {
		svcLogger.V(2).Info("New and old service Status and Annotations are equal, skipping patch")
		return nil
	}
	return patch.PatchServiceLoadBalancerInformation(ctx.KubeClient.CoreV1(), svc, newStatus, newObjectMeta)
}

func emitIpFamiliesStackEvent(ctx *context.ControllerContext, service *v1.Service) {
	var ipFamilies []string
	for _, ipFamily := range service.Spec.IPFamilies {
		ipFamilies = append(ipFamilies, string(ipFamily))
	}
	ctx.Recorder(service.Namespace).Eventf(service, v1.EventTypeNormal, "SyncLoadBalancerSuccessful",
		"Successfully ensured %v load balancer resources", func() string {
			if len(ipFamilies) == 0 {
				return "L4"
			}
			return strings.Join(ipFamilies, " ")
		}())

}

// isHealthCheckDeleted checks if given health check exists in GCE
func isHealthCheckDeleted(cloud *gce.Cloud, hcName string, logger klog.Logger) bool {
	_, err := composite.GetHealthCheck(cloud, meta.GlobalKey(hcName), meta.VersionGA, logger)
	return utils.IsNotFoundError(err)
}

func skipUserError(err error, logger klog.Logger) error {
	if utils.IsUserError(err) {
		logger.Info("Sync failed with user-caused error", "err", err)
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
