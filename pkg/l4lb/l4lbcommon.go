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
	"reflect"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/cloud-provider/service/helpers"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/context"
	"k8s.io/ingress-gce/pkg/loadbalancers"
	"k8s.io/ingress-gce/pkg/utils"
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

// updateL4ResourcesAnnotations this function checks if new annotations should be added to service and patch service metadata if needed.
func updateL4ResourcesAnnotations(ctx *context.ControllerContext, svc *v1.Service, newL4LBAnnotations map[string]string) error {
	klog.V(3).Infof("Updating annotations of service %s/%s", svc.Namespace, svc.Name)
	newObjectMeta := computeNewAnnotationsIfNeeded(svc, newL4LBAnnotations, loadbalancers.L4LBResourceAnnotationKeys)
	if newObjectMeta == nil {
		klog.V(3).Infof("Service annotations not changed, skipping patch for service %s/%s", svc.Namespace, svc.Name)
		return nil
	}
	klog.V(3).Infof("Patching annotations of service %v/%v", svc.Namespace, svc.Name)
	return patch.PatchServiceObjectMetadata(ctx.KubeClient.CoreV1(), svc, *newObjectMeta)
}

// updateL4DualStackResourcesAnnotations this function checks if new annotations should be added to dual-stack service and patch service metadata if needed.
func updateL4DualStackResourcesAnnotations(ctx *context.ControllerContext, svc *v1.Service, newL4LBAnnotations map[string]string) error {
	newObjectMeta := computeNewAnnotationsIfNeeded(svc, newL4LBAnnotations, loadbalancers.L4DualStackResourceAnnotationKeys)
	if newObjectMeta == nil {
		return nil
	}
	klog.V(3).Infof("Patching annotations of service %v/%v", svc.Namespace, svc.Name)
	return patch.PatchServiceObjectMetadata(ctx.KubeClient.CoreV1(), svc, *newObjectMeta)
}

// deleteL4RBSAnnotations deletes all annotations which could be added by L4 ELB RBS controller
func deleteL4RBSAnnotations(ctx *context.ControllerContext, svc *v1.Service) error {
	newObjectMeta := computeNewAnnotationsIfNeeded(svc, nil, loadbalancers.L4RBSAnnotations)
	if newObjectMeta == nil {
		return nil
	}
	klog.V(3).Infof("Deleting all annotations for L4 ELB RBS service %v/%v", svc.Namespace, svc.Name)
	return patch.PatchServiceObjectMetadata(ctx.KubeClient.CoreV1(), svc, *newObjectMeta)
}

// deleteL4RBSDualStackAnnotations deletes all annotations which could be added by L4 ELB RBS controller
func deleteL4RBSDualStackAnnotations(ctx *context.ControllerContext, svc *v1.Service) error {
	newObjectMeta := computeNewAnnotationsIfNeeded(svc, nil, loadbalancers.L4DualStackResourceRBSAnnotationKeys)
	if newObjectMeta == nil {
		return nil
	}
	klog.V(3).Infof("Deleting all DualStack annotations for L4 ELB RBS service %v/%v", svc.Namespace, svc.Name)
	return patch.PatchServiceObjectMetadata(ctx.KubeClient.CoreV1(), svc, *newObjectMeta)
}

func deleteAnnotation(ctx *context.ControllerContext, svc *v1.Service, annotationKey string) error {
	newObjectMeta := svc.ObjectMeta.DeepCopy()
	if _, ok := newObjectMeta.Annotations[annotationKey]; !ok {
		return nil
	}

	klog.V(3).Infof("Removing annotation %s from service %v/%v", annotationKey, svc.Namespace, svc.Name)
	delete(newObjectMeta.Annotations, annotationKey)
	return patch.PatchServiceObjectMetadata(ctx.KubeClient.CoreV1(), svc, *newObjectMeta)
}

// updateServiceStatus this faction checks if LoadBalancer status changed and patch service if needed.
func updateServiceStatus(ctx *context.ControllerContext, svc *v1.Service, newStatus *v1.LoadBalancerStatus) error {
	klog.V(2).Infof("Updating service status, service: %s/%s, new status: %+v", svc.Namespace, svc.Name, newStatus)
	if helpers.LoadBalancerStatusEqual(&svc.Status.LoadBalancer, newStatus) {
		klog.V(2).Infof("New and old statuses are equal, skipping patch. Service: %s/%s", svc.Namespace, svc.Name)
		return nil
	}
	return patch.PatchServiceLoadBalancerStatus(ctx.KubeClient.CoreV1(), svc, *newStatus)
}

// isHealthCheckDeleted checks if given health check exists in GCE
func isHealthCheckDeleted(cloud *gce.Cloud, hcName string) bool {
	_, err := composite.GetHealthCheck(cloud, meta.GlobalKey(hcName), meta.VersionGA)
	return utils.IsNotFoundError(err)
}
