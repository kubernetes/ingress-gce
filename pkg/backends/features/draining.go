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

	"github.com/golang/glog"
	backendconfigv1beta1 "k8s.io/ingress-gce/pkg/apis/backendconfig/v1beta1"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/utils"
)

// EnsureDraining reads the ConnectionDraining configuration specified in
// the ServicePort.BackendConfig and applies it to the BackendService.
// It returns true if there were existing settings on the BackendService
// that were overwritten.
func EnsureDraining(sp utils.ServicePort, be *composite.BackendService) bool {
	if sp.BackendConfig.Spec.ConnectionDraining == nil {
		return false
	}
	beTemp := &composite.BackendService{}
	applyDrainingSettings(sp, beTemp)
	if !reflect.DeepEqual(beTemp.ConnectionDraining, be.ConnectionDraining) {
		applyDrainingSettings(sp, be)
		glog.V(2).Infof("Updated ConnectionDraining settings for service %v/%v.", sp.ID.Service.Namespace, sp.ID.Service.Name)
		return true
	}
	return false
}

// applyDrainingSettings applies the ConnectionDraining settings specified in the
// BackendConfig to the passed in compute.BackendService. A GCE API call still needs
// to be made to actually persist the changes.
func applyDrainingSettings(sp utils.ServicePort, be *composite.BackendService) {
	beConfig := sp.BackendConfig
	setDrainingDefaults(beConfig)
	be.ConnectionDraining = &composite.ConnectionDraining{}
	if beConfig.Spec.ConnectionDraining.DrainingTimeoutSec != nil {
		be.ConnectionDraining.DrainingTimeoutSec = *beConfig.Spec.ConnectionDraining.DrainingTimeoutSec
	}
}

// setDrainingDefaults initializes any nil pointers in Drainning configuration which ensures that
// there are defaults for missing sub-types. Will be more useful once we support more than just
// timeouts.
func setDrainingDefaults(beConfig *backendconfigv1beta1.BackendConfig) {
	if beConfig.Spec.ConnectionDraining == nil {
		beConfig.Spec.ConnectionDraining = &backendconfigv1beta1.ConnectionDrainingConfig{}
	}
}
