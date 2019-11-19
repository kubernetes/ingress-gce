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

	"k8s.io/api/networking/v1beta1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	client "k8s.io/client-go/kubernetes/typed/networking/v1beta1"
	"k8s.io/klog"
	"k8s.io/kubernetes/pkg/util/slice"
)

const (
	// FinalizerKey is the string representing the Ingress finalizer.
	FinalizerKey = "networking.gke.io/ingress-finalizer"
	// FinalizerKeyV2 is the string representing the Ingress finalizer version.
	// Ingress with V2 finalizer uses V2 frontend naming scheme.
	FinalizerKeyV2 = "networking.gke.io/ingress-finalizer-V2"
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
	return slice.ContainsString(m.Finalizers, key, nil)
}

// EnsureFinalizer ensures that the specified finalizer exists on given Ingress.
func EnsureFinalizer(ing *v1beta1.Ingress, ingClient client.IngressInterface, finalizerKey string) (*v1beta1.Ingress, error) {
	updated := ing.DeepCopy()
	if needToAddFinalizer(ing.ObjectMeta, finalizerKey) {
		updated.ObjectMeta.Finalizers = append(updated.ObjectMeta.Finalizers, finalizerKey)
		// TODO(smatti): Make this optimistic concurrency control compliant on write.
		// Refer: https://github.com/eBay/Kubernetes/blob/master/docs/devel/api-conventions.md#concurrency-control-and-consistency
		if _, err := ingClient.Update(updated); err != nil {
			return nil, fmt.Errorf("error updating Ingress %s/%s: %v", ing.Namespace, ing.Name, err)
		}
		klog.V(2).Infof("Added finalizer %q for Ingress %s/%s", finalizerKey, ing.Namespace, ing.Name)
	}
	return updated, nil
}

// needToAddFinalizer is true if the passed in meta does not contain the specified finalizer.
func needToAddFinalizer(m meta_v1.ObjectMeta, key string) bool {
	return m.DeletionTimestamp == nil && !HasGivenFinalizer(m, key)
}

// EnsureDeleteFinalizer ensures that the specified finalizer is deleted from given Ingress.
func EnsureDeleteFinalizer(ing *v1beta1.Ingress, ingClient client.IngressInterface, finalizerKey string) error {
	if HasGivenFinalizer(ing.ObjectMeta, finalizerKey) {
		updated := ing.DeepCopy()
		updated.ObjectMeta.Finalizers = slice.RemoveString(updated.ObjectMeta.Finalizers, finalizerKey, nil)
		if _, err := ingClient.Update(updated); err != nil {
			return fmt.Errorf("error updating Ingress %s/%s: %v", ing.Namespace, ing.Name, err)
		}
		klog.V(2).Infof("Removed finalizer %q for Ingress %s/%s", finalizerKey, ing.Namespace, ing.Name)
	}
	return nil
}
