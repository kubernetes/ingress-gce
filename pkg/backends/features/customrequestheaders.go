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
package features

import (
	"reflect"

	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog"
)

// EnsureCustomRequestHeaders reads the CustomRequestHeaders configuration specified in the ServicePort.BackendConfig
// and applies it to the BackendService. It returns true if there were existing
// settings on the BackendService that were overwritten.
func EnsureCustomRequestHeaders(sp utils.ServicePort, be *composite.BackendService) bool {
	if sp.BackendConfig.Spec.CustomRequestHeaders == nil {
		return false
	}
	beTemp := &composite.BackendService{}
	applyCustomRequestHeaders(sp, beTemp)
	if !reflect.DeepEqual(beTemp.CustomRequestHeaders, be.CustomRequestHeaders) {
		applyCustomRequestHeaders(sp, be)
		klog.V(2).Infof("Updated Custom Request Headers for service %v/%v.", sp.ID.Service.Namespace, sp.ID.Service.Name)
		return true
	}

	return false
}

// applyCustomRequestHeaders applies the CustomRequestHeaders settings specified in the BackendConfig
// to the passed in composite.BackendService. A GCE API call still needs to be made
// to actually persist the changes.
func applyCustomRequestHeaders(sp utils.ServicePort, be *composite.BackendService) {
	be.CustomRequestHeaders = sp.BackendConfig.Spec.CustomRequestHeaders.Headers
}
