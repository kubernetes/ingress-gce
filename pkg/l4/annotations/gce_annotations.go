/*
Copyright 2017 The Kubernetes Authors.

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

package annotations

import (
	"fmt"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/ingress-gce/pkg/utils/namer"
)

// LoadBalancerType defines a specific type for holding load balancer types (eg. Internal)
type LoadBalancerType string

const (
	// ServiceAnnotationLoadBalancerType is annotated on a service with type LoadBalancer
	// dictates what specific kind of GCP LB should be assembled.
	// Currently, only "Internal" is supported.
	ServiceAnnotationLoadBalancerType = "networking.gke.io/load-balancer-type"

	// Deprecating the old-style naming of LoadBalancerType annotation
	deprecatedServiceAnnotationLoadBalancerType = "cloud.google.com/load-balancer-type"

	// LBTypeInternal is the constant for the official internal type.
	LBTypeInternal LoadBalancerType = "Internal"

	// LBTypeExternal is the constant to represent the default type.
	LBTypeExternal LoadBalancerType = "External"

	// Deprecating the lowercase spelling of Internal.
	deprecatedTypeInternalLowerCase LoadBalancerType = "internal"

	// RegionalInternalLoadBalancerClass is the loadBalancerClass name used to select the
	// GKE subsetting LB implementation.
	RegionalInternalLoadBalancerClass = "networking.gke.io/l4-regional-internal"

	// RegionalExternalLoadBalancerClass is the loadBalancerClass name used to select the
	// RBS LB implementation.
	RegionalExternalLoadBalancerClass = "networking.gke.io/l4-regional-external"

	// LegacyRegionalInternalLoadBalancerClass is the loadBalancerClass name used to select the
	// GKE CCM ILB implementation.
	LegacyRegionalInternalLoadBalancerClass = "networking.gke.io/l4-regional-internal-legacy"

	// LegacyRegionalExternalLoadBalancerClass is the loadBalancerClass name used to select the
	// GKE CCM NetLB implementation.
	LegacyRegionalExternalLoadBalancerClass = "networking.gke.io/l4-regional-external-legacy"

	// StandalonePassthroughNegLoadBalancerClass is the loadBalancerClass name used for services that
	// should use GCE_VM_IP NEGs for L4.
	StandalonePassthroughNegLoadBalancerClass = "networking.gke.io/standalone-passthrough-lb-neg"

	// StandalonePassthroughLBNegNameKey is the optional annotation used to choose the name of the
	// GCE_VM_IP NEG created for services using the StandalonePassthroughNegLoadBalancerClass.
	//
	// If a NEG with the given name already exists, it is only used if its description matches
	// {"cluster-uid":"<kube-system UID>","namespace":"<service namespace>","service-name":"<service name>"}.
	// Such NEGs are then managed (and garbage collected) as if they were created by the controller.
	// NEGs with an empty or non-matching description are never used nor deleted.
	//
	// In Multi Subnet Clusters, NEGs for the non-default subnets are named "<name>-<subnet hash>".
	//
	// Changing the annotation value creates the NEGs with the new name, while the NEGs with the
	// previous name are no longer synced and are garbage collected once they are no longer
	// referenced by a backend service. The load balancer IPs stay programmed while only the
	// previous NEGs are attached (with a DrainingNEGsAttached warning event), so the backend
	// services can be switched without disruption: attach the new NEGs, then detach the old ones.
	// The endpoints of the previous NEGs are not updated anymore, so the switch should be done
	// promptly.
	//
	// An invalid value is reported with an InvalidNEGName event on the service and blocks the
	// update of its NEG configuration: the NEGs created before keep being synced and the load
	// balancer IPs stay programmed, while a new service gets no NEGs at all.
	StandalonePassthroughLBNegNameKey = "networking.gke.io/standalone-passthrough-lb-neg-name"
)

// GetLoadBalancerAnnotationType returns the type of GCP load balancer which should be assembled.
func GetLoadBalancerAnnotationType(svc *v1.Service) LoadBalancerType {
	var lbType LoadBalancerType
	// Check LoadBalancerClass before load balancer type annotation since it has precedence.
	if HasLoadBalancerClass(svc, RegionalInternalLoadBalancerClass) {
		return LBTypeInternal
	} else if HasLoadBalancerClass(svc, RegionalExternalLoadBalancerClass) {
		return LBTypeExternal
	}
	for _, ann := range []string{
		ServiceAnnotationLoadBalancerType,
		deprecatedServiceAnnotationLoadBalancerType,
	} {
		if v, ok := svc.Annotations[ann]; ok {
			lbType = LoadBalancerType(v)
			break
		}
	}

	switch lbType {
	case LBTypeInternal, deprecatedTypeInternalLowerCase:
		return LBTypeInternal
	default:
		return LBTypeExternal
	}
}

// StandaloneNEGName returns the user requested name for the L4 standalone NEG,
// or an empty string if the annotation is not set, and a validation error if the value is unusable.
// The returned name is also used as the ServiceNetworkEndpointGroup CR name.
func StandaloneNEGName(service *v1.Service) (string, error) {
	if service == nil {
		return "", nil
	}
	raw, ok := service.Annotations[StandalonePassthroughLBNegNameKey]
	if !ok {
		return "", nil
	}
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", fmt.Errorf("annotation %q must not be empty", StandalonePassthroughLBNegNameKey)
	}
	if len(name) > namer.MaxDefaultSubnetNegNameLength {
		return "", fmt.Errorf("annotation %q: %w", StandalonePassthroughLBNegNameKey, namer.ErrCustomNEGNameTooLong)
	}
	if errs := validation.IsDNS1035Label(name); len(errs) > 0 {
		return "", fmt.Errorf("annotation %q value %q is not a valid NEG name: %s", StandalonePassthroughLBNegNameKey, name, strings.Join(errs, ", "))
	}
	return name, nil
}
