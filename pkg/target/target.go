// Copyright 2018 Google Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package target

import (
	"fmt"

	extensions "k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/ingress-gce/pkg/annotations"
)

type targetResourceManager struct {
	kubeClient kubernetes.Interface
}

func NewTargetResourceManager(kubeClient kubernetes.Interface) TargetResourceManager {
	return &targetResourceManager{kubeClient: kubeClient}
}

var _ TargetResourceManager = &targetResourceManager{}

func (m *targetResourceManager) EnsureTargetIngress(ing *extensions.Ingress) (*extensions.Ingress, error) {
	existingTargetIng, getErr := m.kubeClient.ExtensionsV1beta1().Ingresses(ing.Namespace).Get(ing.Name, meta_v1.GetOptions{})
	if getErr != nil {
		if errors.IsNotFound(getErr) {
			targetIng := createTargetIngress(ing)
			createdIng, createErr := m.kubeClient.ExtensionsV1beta1().Ingresses(targetIng.Namespace).Create(targetIng)
			if createErr != nil {
				return nil, fmt.Errorf("Error creating target ingress %v/%v: %v", ing.Namespace, ing.Name, createErr)
			}
			return createdIng, nil
		}
		return nil, getErr
	}
	// Ignore all GCP resource annotations and MCI annotation when comparing ingresses.
	ignoreAnnotations := map[string]string{annotations.InstanceGroupsAnnotationKey: "", annotations.IngressClassKey: ""}
	if !objectMetaAndSpecEquivalent(ing, existingTargetIng, ignoreAnnotations) {
		updateTargetIng(ing, existingTargetIng)
		updatedIng, updateErr := m.kubeClient.ExtensionsV1beta1().Ingresses(ing.Namespace).Update(existingTargetIng)
		if updateErr != nil {
			return nil, fmt.Errorf("Error updating target ingress %v/%v: %v", ing.Namespace, ing.Name, updateErr)
		}
		return updatedIng, nil
	}
	return existingTargetIng, nil

}

func (m *targetResourceManager) DeleteTargetIngress(name, namespace string) error {
	deleteErr := m.kubeClient.ExtensionsV1beta1().Ingresses(namespace).Delete(name, &meta_v1.DeleteOptions{})
	if deleteErr != nil {
		return fmt.Errorf("Error deleting ingress %v/%v: %v", namespace, name, deleteErr)
	}
	return nil
}
