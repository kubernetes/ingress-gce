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

// EnsureAffinity reads the sessionAffinity and AffinityCookieTtlSec configuration
// specified in the ServicePort.BackendConfig and applies it to the BackendService.
// It returns true if there were existing settings on the BackendService
// that were overwritten.
func EnsureAffinity(sp utils.ServicePort, be *composite.BackendService) bool {
	if sp.BackendConfig.Spec.SessionAffinity == nil {
		return false
	}
	beTemp := &composite.BackendService{}
	applyAffinitySettings(sp, beTemp)
	if !reflect.DeepEqual(beTemp.AffinityCookieTtlSec, be.AffinityCookieTtlSec) || beTemp.SessionAffinity != be.SessionAffinity {
		applyAffinitySettings(sp, be)
		klog.V(2).Infof("Updated SessionAffinity settings for service %v/%v.", sp.ID.Service.Namespace, sp.ID.Service.Name)
		return true
	}
	return false
}

// applyAffinitySettings applies the session affinity settings specified in the
// BackendConfig to the passed in composite.BackendService. A GCE API call still
// needs to be made to actually persist the changes.
func applyAffinitySettings(sp utils.ServicePort, be *composite.BackendService) {
	be.SessionAffinity = sp.BackendConfig.Spec.SessionAffinity.AffinityType
	if sp.BackendConfig.Spec.SessionAffinity.AffinityCookieTtlSec != nil {
		be.AffinityCookieTtlSec = *sp.BackendConfig.Spec.SessionAffinity.AffinityCookieTtlSec
	}
}
