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

package namer

import (
	"errors"
	"fmt"

	"k8s.io/client-go/tools/cache"
	negbindingv1beta1 "k8s.io/ingress-gce/pkg/apis/negbinding/v1beta1"
)

var (
	ErrNegBindingNotFound       = errors.New("negbinding is not in store")
	ErrNegNameNotFound          = errors.New("NEG name for subnet not found in NegBinding Spec")
	ErrNBNamerNotImplemented    = errors.New("not implemented for NegBindingNamer")
	ErrNBNamerInvalidBackendRef = errors.New("NEGBinding's backendRef doesn't match")
)

// NegBindingNamer resolves NEG names based on NetworkEndpointGroupBinding CR.
// It implements NonDefaultSubnetNEGNamer.
type NegBindingNamer struct {
	namespace        string
	name             string
	negBindingLister cache.Indexer
}

// NewNegBindingNamer constructs a new NegBindingNamer
func NewNegBindingNamer(namespace, name string, negBindingLister cache.Indexer) *NegBindingNamer {
	return &NegBindingNamer{
		namespace:        namespace,
		name:             name,
		negBindingLister: negBindingLister,
	}
}

func (n *NegBindingNamer) getBinding() (*negbindingv1beta1.NetworkEndpointGroupBinding, error) {
	key := fmt.Sprintf("%s/%s", n.namespace, n.name)
	obj, exists, err := n.negBindingLister.GetByKey(key)
	if err != nil {
		return nil, fmt.Errorf("error getting negbinding from cache: %w", err)
	}
	if !exists {
		return nil, fmt.Errorf("%w: key %s", ErrNegBindingNotFound, key)
	}

	binding, ok := obj.(*negbindingv1beta1.NetworkEndpointGroupBinding)
	if !ok {
		return nil, fmt.Errorf("unexpected object type %T in negbinding cache for key %s", obj, key)
	}
	return binding, nil
}

// negNameForSubnet returns the NEG name defined in the NegBinding CR Spec for the given subnet.
func (n *NegBindingNamer) negNameForSubnet(subnet, namespace, svcName string, port int32) (string, error) {
	binding, err := n.getBinding()
	if err != nil {
		return "", err
	}

	if binding.Namespace != namespace ||
		binding.Spec.BackendRef.Name != svcName ||
		binding.Spec.BackendRef.Port != port {
		gotRef := fmt.Sprintf("%s/%s:%v", namespace, svcName, port)
		expectedRef := fmt.Sprintf("%s/%s:%v", binding.Namespace, binding.Spec.BackendRef.Name, port)
		return "", fmt.Errorf("%w: backendRef %s, negBinding.Spec.BackendRef %s", ErrNBNamerInvalidBackendRef, gotRef, expectedRef)
	}

	for _, ref := range binding.Spec.NetworkEndpointGroups {
		if ref.Subnet == subnet {
			return ref.Name, nil
		}
	}
	return "", fmt.Errorf("%w: subnet %s, negBinding %s/%s", ErrNegNameNotFound, subnet, n.namespace, n.name)
}

// NonDefaultSubnetNEG returns the NEG name for the given subnet based on NegBinding's Spec.
func (n *NegBindingNamer) NonDefaultSubnetNEG(namespace, svcName, subnetName string, port int32) (string, error) {
	return n.negNameForSubnet(subnetName, namespace, svcName, port)
}

// NonDefaultSubnetCustomNEG implements NonDefaultSubnetNEGNamer.NonDefaultSubnetCustomNEG.
// Returns a not implemented error as custom names should not be set for NegBinding-based syncers.
func (n *NegBindingNamer) NonDefaultSubnetCustomNEG(customNEGName, subnetName string) (string, error) {
	return "", ErrNBNamerNotImplemented
}
