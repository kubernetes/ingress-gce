/*
Copyright 2025 The Kubernetes Authors.
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
	v1 "k8s.io/api/core/v1"
	"k8s.io/ingress-gce/pkg/utils/slice"
)

const (
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
)

// HasLoadBalancerClass checks if the given service has a specific loadBalancerClass set.
func HasLoadBalancerClass(service *v1.Service, key string) bool {
	if service.Spec.LoadBalancerClass != nil {
		if *service.Spec.LoadBalancerClass == key {
			return true
		}
	}
	return false
}

// IsLegacyL4ILBService returns true if the given LoadBalancer service is managed by service controller.
func IsLegacyL4ILBService(svc *v1.Service) bool {
	if svc.Spec.LoadBalancerClass != nil {
		return HasLoadBalancerClass(svc, LegacyRegionalInternalLoadBalancerClass)
	}
	return HasLegacyL4ILBFinalizerV1(svc)
}

// HasLegacyL4ILBFinalizerV1 returns true if the given Service has ILBFinalizerV1
func HasLegacyL4ILBFinalizerV1(svc *v1.Service) bool {
	return slice.ContainsString(svc.ObjectMeta.Finalizers, LegacyILBFinalizer, nil)
}

// IsSubsettingL4ILBService returns true if the given LoadBalancer service is managed by NEG and L4 controller.
func IsSubsettingL4ILBService(svc *v1.Service) bool {
	if svc.Spec.LoadBalancerClass != nil {
		return HasLoadBalancerClass(svc, RegionalInternalLoadBalancerClass)
	}
	return HasL4ILBFinalizerV2(svc)
}

// HasL4ILBFinalizerV2 returns true if the given Service has ILBFinalizerV2
func HasL4ILBFinalizerV2(svc *v1.Service) bool {
	return slice.ContainsString(svc.ObjectMeta.Finalizers, ILBFinalizerV2, nil)
}

// HasLegacyL4NetLBFinalizerV1 returns true if the given Service has NetLBFinalizerV1
func HasLegacyL4NetLBFinalizerV1(svc *v1.Service) bool {
	return slice.ContainsString(svc.ObjectMeta.Finalizers, LegacyNetLBFinalizerV1, nil)
}

// HasL4NetLBFinalizerV2 returns true if the given Service has NetLBFinalizerV2
func HasL4NetLBFinalizerV2(svc *v1.Service) bool {
	return slice.ContainsString(svc.ObjectMeta.Finalizers, NetLBFinalizerV2, nil)
}

// HasL4NetLBFinalizerV3 returns true if the given Service has NetLBFinalizerV3
func HasL4NetLBFinalizerV3(svc *v1.Service) bool {
	return slice.ContainsString(svc.ObjectMeta.Finalizers, NetLBFinalizerV3, nil)
}

// IsRBSL4NetLB returns true if the given service is managed by RBS L4NetLB controller.
func IsRBSL4NetLB(svc *v1.Service) bool {
	return HasL4NetLBFinalizerV3(svc) || HasL4NetLBFinalizerV2(svc) || HasLoadBalancerClass(svc, RegionalExternalLoadBalancerClass)
}
