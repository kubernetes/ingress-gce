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
	"k8s.io/api/core/v1"
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

	// Deprecating the lowercase spelling of Internal.
	deprecatedTypeInternalLowerCase LoadBalancerType = "internal"
)

// GetLoadBalancerAnnotationType returns the type of GCP load balancer which should be assembled.
func GetLoadBalancerAnnotationType(service *v1.Service) LoadBalancerType {
	var lbType LoadBalancerType
	for _, ann := range []string{
		ServiceAnnotationLoadBalancerType,
		deprecatedServiceAnnotationLoadBalancerType,
	} {
		if v, ok := service.Annotations[ann]; ok {
			lbType = LoadBalancerType(v)
			break
		}
	}

	switch lbType {
	case LBTypeInternal, deprecatedTypeInternalLowerCase:
		return LBTypeInternal
	default:
		return lbType
	}
}
