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

	"github.com/golang/glog"
	extensions "k8s.io/api/extensions/v1beta1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/utils"
)

type targetResourceManager struct {
	kubeClient kubernetes.Interface
}

func NewTargetResourceManager(kubeClient kubernetes.Interface) TargetResourceManager {
	return &targetResourceManager{kubeClient: kubeClient}
}

var _ TargetResourceManager = &targetResourceManager{}

func (m *targetResourceManager) EnsureTargetIngress(ing *extensions.Ingress) (*extensions.Ingress, error) {
	existingIng, getErr := m.kubeClient.ExtensionsV1beta1().Ingresses(ing.Namespace).Get(ing.Name, meta_v1.GetOptions{})
	if getErr != nil {
		if utils.IsNotFoundError(getErr) {
			// Add MCI annotation to "target" ingress.
			ing.Annotations[annotations.IngressClassKey] = annotations.GceMultiIngressClass
			actualIng, createErr := m.kubeClient.ExtensionsV1beta1().Ingresses(ing.Namespace).Create(ing)
			if createErr != nil {
				return nil, fmt.Errorf("Error creating target ingress %v/%v: %v", ing.Namespace, ing.Name, createErr)
			}
			return actualIng, nil
		}
	}
	// Ignore instance group annotation while comparing ingresses.
	ignoreAnnotations := map[string]string{instanceGroupAnnotationKey: ""}
	if !objectMetaAndSpecEquivalent(ing, existingIng, ignoreAnnotations) {
		glog.V(3).Infof("Target ingress %v in cluster %v differs. Overwriting... \n")
		updatedIng, updateErr := m.kubeClient.ExtensionsV1beta1().Ingresses(ing.Namespace).Update(ing)
		if updateErr != nil {
			return nil, fmt.Errorf("Error updating target ingress %v/%v: %v", ing.Namespace, ing.Name, updateErr)
		}
		return updatedIng, nil
	}
	return existingIng, nil

}

func (m *targetResourceManager) DeleteTargetIngress(ing *extensions.Ingress) error {
	deleteErr := m.kubeClient.ExtensionsV1beta1().Ingresses(ing.Namespace).Delete(ing.Name, &meta_v1.DeleteOptions{})
	if deleteErr != nil {
		return fmt.Errorf("Error deleting ingress %v/%v: %v", ing.Namespace, ing.Name, deleteErr)
	}
	return nil
}
