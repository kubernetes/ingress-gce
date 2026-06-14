/*
Copyright 2022 The Kubernetes Authors.

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

import "k8s.io/api/core/v1"

func NeedsIPv6(service *v1.Service) bool {
	return supportsIPFamily(service, v1.IPv6Protocol)
}

func NeedsIPv4(service *v1.Service) bool {
	if service == nil {
		return false
	}
	// Should never happen, defensive coding if kube-api did not populate IPFamilies
	if len(service.Spec.IPFamilies) == 0 {
		return true
	}

	return supportsIPFamily(service, v1.IPv4Protocol)
}

func supportsIPFamily(service *v1.Service, ipFamily v1.IPFamily) bool {
	if service == nil {
		return false
	}

	ipFamilies := service.Spec.IPFamilies
	for _, ipf := range ipFamilies {
		if ipf == ipFamily {
			return true
		}
	}
	return false
}
