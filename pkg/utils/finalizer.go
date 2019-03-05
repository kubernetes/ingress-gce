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

package utils

import (
	"fmt"

	extensions "k8s.io/api/extensions/v1beta1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	client "k8s.io/client-go/kubernetes/typed/extensions/v1beta1"
	"k8s.io/klog"
	"k8s.io/kubernetes/pkg/util/slice"
)

// FinalizerKey is the string representing the Ingress finalizer.
const FinalizerKey = "networking.gke.io/ingress-finalizer"

// IsDeletionCandidate is true if the passed in meta contains the specified finalizer.
func IsDeletionCandidate(m meta_v1.ObjectMeta, key string) bool {
	return m.DeletionTimestamp != nil && HasFinalizer(m, key)
}

// NeedToAddFinalizer is true if the passed in meta does not contain the specified finalizer.
func NeedToAddFinalizer(m meta_v1.ObjectMeta, key string) bool {
	return m.DeletionTimestamp == nil && !HasFinalizer(m, key)
}

// HasFinalizer is true if the passed in meta has the specified finalizer.
func HasFinalizer(m meta_v1.ObjectMeta, key string) bool {
	return slice.ContainsString(m.Finalizers, key, nil)
}

// AddFinalizer tries to add a finalizer to an Ingress. If a finalizer
// already exists, it does nothing.
func AddFinalizer(ing *extensions.Ingress, ingClient client.IngressInterface) error {
	ingKey := FinalizerKey
	if NeedToAddFinalizer(ing.ObjectMeta, ingKey) {
		updated := ing.DeepCopy()
		updated.ObjectMeta.Finalizers = append(updated.ObjectMeta.Finalizers, ingKey)
		if _, err := ingClient.Update(updated); err != nil {
			return fmt.Errorf("error updating Ingress %s/%s: %v", ing.Namespace, ing.Name, err)
		}
		klog.V(3).Infof("Added finalizer %q for Ingress %s/%s", ingKey, ing.Namespace, ing.Name)
	}

	return nil
}

// RemoveFinalizer tries to remove a Finalizer from an Ingress. If a
// finalizer is not on the Ingress, it does nothing.
func RemoveFinalizer(ing *extensions.Ingress, ingClient client.IngressInterface) error {
	ingKey := FinalizerKey
	if HasFinalizer(ing.ObjectMeta, ingKey) {
		updated := ing.DeepCopy()
		updated.ObjectMeta.Finalizers = slice.RemoveString(updated.ObjectMeta.Finalizers, ingKey, nil)
		if _, err := ingClient.Update(updated); err != nil {
			return fmt.Errorf("error updating Ingress %s/%s: %v", ing.Namespace, ing.Name, err)
		}
		klog.V(3).Infof("Removed finalizer %q for Ingress %s/%s", ingKey, ing.Namespace, ing.Name)
	}

	return nil
}
