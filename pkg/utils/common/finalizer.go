/*
Copyright 2018 The Kubernetes Authors.
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

package common

import (
	"fmt"
	"slices"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/networking/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	client "k8s.io/client-go/kubernetes/typed/networking/v1"
	"k8s.io/ingress-gce/pkg/utils/patch"
	"k8s.io/ingress-gce/pkg/utils/slice"
	"k8s.io/klog/v2"
)

const (
	// FinalizerKey is the string representing the Ingress finalizer.
	FinalizerKey = "networking.gke.io/ingress-finalizer"
	// FinalizerKeyV2 is the string representing the Ingress finalizer version.
	// Ingress with V2 finalizer uses V2 frontend naming scheme.
	FinalizerKeyV2 = "networking.gke.io/ingress-finalizer-V2"
	// TODO remove the 2 definitions once they are added in legacy-cloud-providers/gce
	// LegacyILBFinalizer key is used to identify ILB services whose resources are managed by service controller.
	LegacyILBFinalizer = "gke.networking.io/l4-ilb-v1"
	// ILBFinalizerV2 is the finalizer used by newer controllers that implement Internal LoadBalancer services.
	ILBFinalizerV2 = "gke.networking.io/l4-ilb-v2"
	// NegFinalizerKey is the finalizer used by neg controller to ensure NEG CRs are deleted after corresponding negs are deleted
	NegFinalizerKey = "networking.gke.io/neg-finalizer"
	// NetLBFinalizerV2 is the finalizer used by newer controllers that manage L4 External LoadBalancer services.
	NetLBFinalizerV2 = "gke.networking.io/l4-netlb-v2"
	// NetLBFinalizerV3 is the finalizer used by the NEG backed variant of the L4 External LoadBalancer services.
	NetLBFinalizerV3 = "gke.networking.io/l4-netlb-v3"
	// LoadBalancerCleanupFinalizer added by original kubernetes service controller. This is not required in L4 RBS/ILB-subsetting services.
	LoadBalancerCleanupFinalizer = "service.kubernetes.io/load-balancer-cleanup"
)

// IsDeletionCandidate is true if the passed in meta contains an ingress finalizer.
func IsDeletionCandidate(m meta_v1.ObjectMeta) bool {
	return IsDeletionCandidateForGivenFinalizer(m, FinalizerKey) || IsDeletionCandidateForGivenFinalizer(m, FinalizerKeyV2)
}

// IsDeletionCandidateForGivenFinalizer is true if the passed in meta contains the specified finalizer.
func IsDeletionCandidateForGivenFinalizer(m meta_v1.ObjectMeta, key string) bool {
	return m.DeletionTimestamp != nil && HasGivenFinalizer(m, key)
}

// HasFinalizer is true if the passed in meta has an ingress finalizer.
func HasFinalizer(m meta_v1.ObjectMeta) bool {
	return HasGivenFinalizer(m, FinalizerKey) || HasGivenFinalizer(m, FinalizerKeyV2)
}

// HasGivenFinalizer is true if the passed in meta has the specified finalizer.
func HasGivenFinalizer(m meta_v1.ObjectMeta, key string) bool {
	return slices.Contains(m.Finalizers, key)
}

// EnsureFinalizer ensures that the specified finalizer exists on given Ingress.
func EnsureFinalizer(ing *v1.Ingress, ingClient client.IngressInterface, finalizerKey string, ingLogger klog.Logger) (*v1.Ingress, error) {
	updated := ing.DeepCopy()
	if needToAddFinalizer(ing.ObjectMeta, finalizerKey) {
		updated.ObjectMeta.Finalizers = append(updated.ObjectMeta.Finalizers, finalizerKey)
		if _, err := PatchIngressObjectMetadata(ingClient, ing, updated.ObjectMeta, ingLogger); err != nil {
			return nil, fmt.Errorf("error patching Ingress %s/%s: %v", ing.Namespace, ing.Name, err)
		}
		ingLogger.Info("Added finalizer", "finalizerKey", finalizerKey)
	}
	return updated, nil
}

// needToAddFinalizer is true if the passed in meta does not contain the specified finalizer.
func needToAddFinalizer(m meta_v1.ObjectMeta, key string) bool {
	return m.DeletionTimestamp == nil && !HasGivenFinalizer(m, key)
}

// EnsureDeleteFinalizer ensures that the specified finalizer is deleted from given Ingress.
func EnsureDeleteFinalizer(ing *v1.Ingress, ingClient client.IngressInterface, finalizerKey string, ingLogger klog.Logger) error {
	if HasGivenFinalizer(ing.ObjectMeta, finalizerKey) {
		updatedObjectMeta := ing.ObjectMeta.DeepCopy()
		updatedObjectMeta.Finalizers = slice.RemoveString(updatedObjectMeta.Finalizers, finalizerKey, nil)
		if _, err := PatchIngressObjectMetadata(ingClient, ing, *updatedObjectMeta, ingLogger); err != nil {
			return fmt.Errorf("error patching Ingress %s/%s: %v", ing.Namespace, ing.Name, err)
		}
		ingLogger.Info("Removed finalizer", "finalizer", finalizerKey)
	}
	return nil
}

// EnsureServiceFinalizer patches the service to add finalizer.
// This function will not modify the service object passed as the argument. Instead, a deep copy will be used to do a patch.
func EnsureServiceFinalizer(service *corev1.Service, key string, kubeClient kubernetes.Interface, svcLogger klog.Logger) error {
	if HasGivenFinalizer(service.ObjectMeta, key) {
		return nil
	}

	// Make a copy of object metadata so we don't mutate the shared informer cache.
	updatedObjectMeta := service.ObjectMeta.DeepCopy()
	updatedObjectMeta.Finalizers = append(updatedObjectMeta.Finalizers, key)

	svcLogger.V(2).Info("Adding finalizer to service", "finalizerKey", key)
	return patch.PatchServiceObjectMetadata(kubeClient.CoreV1(), service, *updatedObjectMeta)
}

// EnsureDeleteServiceFinalizer patches the service to remove finalizer.
// This function will not modify the service object passed as the argument. Instead, a deep copy will be used to do a patch.
func EnsureDeleteServiceFinalizer(service *corev1.Service, key string, kubeClient kubernetes.Interface, svcLogger klog.Logger) error {
	if !HasGivenFinalizer(service.ObjectMeta, key) {
		return nil
	}

	// Make a copy of object metadata so we don't mutate the shared informer cache.
	updatedObjectMeta := service.ObjectMeta.DeepCopy()
	updatedObjectMeta.Finalizers = slice.RemoveString(updatedObjectMeta.Finalizers, key, nil)

	svcLogger.V(2).Info("Removing finalizer from service", "finalizerKey", key)
	return patch.PatchServiceObjectMetadata(kubeClient.CoreV1(), service, *updatedObjectMeta)
}

// EnsureServiceDeleteFinalizers patches the service to ensure the specified finalizers are not present in the service finalizers list.
// This function is needed if more than one finalizer has to be removed since you can't invoke the 1 param version multiple times.
// This function will not modify the service object passed as the argument. Instead, a deep copy will be used to do a patch.
func EnsureServiceDeleteFinalizers(service *corev1.Service, ensureRemoveKeys []string, kubeClient kubernetes.Interface, svcLogger klog.Logger) error {
	var needToRemove []string
	for _, key := range ensureRemoveKeys {
		if HasGivenFinalizer(service.ObjectMeta, key) {
			needToRemove = append(needToRemove, key)
		}
	}
	if len(needToRemove) == 0 {
		return nil
	}

	// Make a copy of object metadata so we don't mutate the shared informer cache.
	updatedObjectMeta := service.ObjectMeta.DeepCopy()
	for _, key := range needToRemove {
		updatedObjectMeta.Finalizers = slice.RemoveString(updatedObjectMeta.Finalizers, key, nil)
	}

	svcLogger.V(2).Info("Removing finalizers from service", "finalizerKeys", ensureRemoveKeys)
	return patch.PatchServiceObjectMetadata(kubeClient.CoreV1(), service, *updatedObjectMeta)
}
