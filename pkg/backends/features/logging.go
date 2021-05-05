/*
Copyright 2020 The Kubernetes Authors.

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
	"fmt"

	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog"
)

// EnsureLogging reads the log configurations specified in the ServicePort.BackendConfig
// and applies it to the BackendService. It returns true if there were existing settings
// on the BackendService that were overwritten.
func EnsureLogging(sp utils.ServicePort, be *composite.BackendService) bool {
	if sp.BackendConfig.Spec.Logging == nil {
		return false
	}
	svcKey := fmt.Sprintf("%s/%s", sp.ID.Service.Namespace, sp.ID.Service.Name)
	if be.LogConfig != nil && !be.LogConfig.Enable && !sp.BackendConfig.Spec.Logging.Enable {
		klog.V(3).Infof("Logging continues to stay disabled for service %s (port %d), skipping update", svcKey, sp.Port)
		return false
	}
	var existingLogConfig *composite.BackendServiceLogConfig
	if be.LogConfig != nil {
		existingLogConfig = &composite.BackendServiceLogConfig{
			Enable:     be.LogConfig.Enable,
			SampleRate: be.LogConfig.SampleRate,
		}
	}
	ensureBackendServiceLogConfig(sp, be)
	if existingLogConfig == nil || existingLogConfig.Enable != be.LogConfig.Enable ||
		existingLogConfig.SampleRate != be.LogConfig.SampleRate {
		klog.V(2).Infof("Updated Logging settings for service %s (port %d) to (Enable: %t, SampleRate: %f)", svcKey, sp.Port, be.LogConfig.Enable, be.LogConfig.SampleRate)
		return true
	}
	return false
}

// ensureBackendServiceLogConfig updates the BackendService LogConfig to
// the access log settings specified in the BackendConfig for the passed in
// service port.
// This method assumes that sp.BackendConfig.Spec.Logging is not nil.
func ensureBackendServiceLogConfig(sp utils.ServicePort, be *composite.BackendService) {
	if be.LogConfig == nil {
		be.LogConfig = &composite.BackendServiceLogConfig{}
	}
	be.LogConfig.Enable = sp.BackendConfig.Spec.Logging.Enable
	// Ignore sample rate if logging is not enabled.
	if !sp.BackendConfig.Spec.Logging.Enable {
		return
	}
	// Existing sample rate is retained if not specified.
	if sp.BackendConfig.Spec.Logging.SampleRate == nil {
		// Update sample rate to 1.0 if no existing sample rate or is 0.0
		if be.LogConfig.SampleRate == 0.0 {
			svcKey := fmt.Sprintf("%s/%s", sp.ID.Service.Namespace, sp.ID.Service.Name)
			klog.V(2).Infof("Sample rate neither specified nor preexists for service %s (port %d), defaulting to 1.0", svcKey, sp.Port)
			be.LogConfig.SampleRate = 1.0
		}
		return
	}
	be.LogConfig.SampleRate = *sp.BackendConfig.Spec.Logging.SampleRate
}
