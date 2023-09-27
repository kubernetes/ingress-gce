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
package features

import (
	"reflect"

	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog/v2"
)

// EnsureCustomResponseHeaders reads the CustomResponseHeaders configuration specified in the ServicePort.BackendConfig
// and applies it to the BackendService. It returns true if there was a difference between
// current settings on the BackendService and ServicePort.BackendConfig.

func EnsureCustomResponseHeaders(sp utils.ServicePort, be *composite.BackendService) bool {
	desiredHeaders := []string{}
	if sp.BackendConfig.Spec.CustomResponseHeaders != nil {
		desiredHeaders = sp.BackendConfig.Spec.CustomResponseHeaders.Headers
	}
	currentHeaders := []string{}
	if be.CustomResponseHeaders != nil {
		currentHeaders = be.CustomResponseHeaders
	}

	if !reflect.DeepEqual(desiredHeaders, currentHeaders) {
		be.CustomResponseHeaders = desiredHeaders
		klog.V(2).Infof("Updated Custom Response Headers for service %v/%v.", sp.ID.Service.Namespace, sp.ID.Service.Name)
		return true
	}

	return false
}
