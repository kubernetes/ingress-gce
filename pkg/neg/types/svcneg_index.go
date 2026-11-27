/*
Copyright 2026 The Kubernetes Authors.

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

package types

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	negv1beta1 "k8s.io/ingress-gce/pkg/apis/svcneg/v1beta1"
	"k8s.io/ingress-gce/pkg/utils"
)

const (
	// SvcNegServiceIndex is the name of the ServiceNetworkEndpointGroup informer
	// index that maps the key (namespace/name) of the service controlling a
	// ServiceNetworkEndpointGroup to the ServiceNetworkEndpointGroup.
	SvcNegServiceIndex = "svcNegService"
)

// SvcNegIndexers returns the indexers of the ServiceNetworkEndpointGroup
// informer: by namespace and by the service controlling the
// ServiceNetworkEndpointGroup.
func SvcNegIndexers() cache.Indexers {
	indexers := utils.NewNamespaceIndexer()
	indexers[SvcNegServiceIndex] = SvcNegServiceIndexFunc
	return indexers
}

// SvcNegServiceIndexFunc indexes ServiceNetworkEndpointGroups by the key
// (namespace/name) of the service set as their controller owner, as done by
// the NEG controller. ServiceNetworkEndpointGroups without a service as their
// controller are not indexed.
func SvcNegServiceIndexFunc(obj interface{}) ([]string, error) {
	svcNeg, ok := obj.(*negv1beta1.ServiceNetworkEndpointGroup)
	if !ok {
		return nil, fmt.Errorf("unexpected object type %T", obj)
	}
	owner := ControllingService(svcNeg)
	if owner == nil {
		return nil, nil
	}
	return []string{SvcNegServiceIndexKey(svcNeg.Namespace, owner.Name)}, nil
}

// ControllingService returns the owner reference of the service controlling
// the ServiceNetworkEndpointGroup, or nil if it is not controlled by a
// service. The NEG controller sets the service it creates the
// ServiceNetworkEndpointGroup for as its controller.
func ControllingService(svcNeg *negv1beta1.ServiceNetworkEndpointGroup) *metav1.OwnerReference {
	owner := metav1.GetControllerOf(svcNeg)
	if owner == nil || owner.Kind != "Service" {
		return nil
	}
	return owner
}

// SvcNegServiceIndexKey returns the SvcNegServiceIndex key of the service,
// which is the key of the service in the service informer.
func SvcNegServiceIndexKey(namespace, name string) string {
	return utils.ServiceKeyFunc(namespace, name)
}
