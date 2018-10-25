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
	"strings"

	"github.com/golang/glog"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/utils"
)

// EnsureAffinity reads the sessionAffinity and AffinityCookieTtlSec configuration
// specified in the ServicePort.BackendConfig and applies it to the BackendService.
// It returns true if there were existing settings on the BackendService
// that were overwritten.
func EnsureAffinity(sp utils.ServicePort, be *composite.BackendService) bool {
	changed := false

	// Should we check if specified SessionAffinity is among the currently GCP supported values?
	// For now let's forward as is to GCP, to inherit API error message (and evolutions).
	// Same for TTL (should be in 0-86400 range).
	if sp.BackendConfig.Spec.SessionAffinity != "" && strings.Compare(sp.BackendConfig.Spec.SessionAffinity, be.SessionAffinity) != 0 {
		be.SessionAffinity = sp.BackendConfig.Spec.SessionAffinity
		glog.V(2).Infof("Updated SessionAffinity settings for service %v/%v.", sp.ID.Service.Namespace, sp.ID.Service.Name)
		changed = true
	}

	if sp.BackendConfig.Spec.AffinityCookieTtlSec != nil && *sp.BackendConfig.Spec.AffinityCookieTtlSec != be.AffinityCookieTtlSec {
		be.AffinityCookieTtlSec = *sp.BackendConfig.Spec.AffinityCookieTtlSec
		glog.V(2).Infof("Updated AffinityCookieTtlSec settings for service %v/%v.", sp.ID.Service.Namespace, sp.ID.Service.Name)
		changed = true
	}

	return changed
}
