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
	expectedLogConfig := expectedBackendServiceLogConfig(sp)
	if !expectedLogConfig.Enable && !be.LogConfig.Enable {
		klog.V(3).Infof("Logging continues to stay disabled for service %s, skipping update", svcKey)
		return false
	}
	if be.LogConfig == nil || expectedLogConfig.Enable != be.LogConfig.Enable ||
		expectedLogConfig.SampleRate != be.LogConfig.SampleRate {
		be.LogConfig = expectedLogConfig
		klog.V(2).Infof("Updated Logging settings for service %s and port %d (Enable: %t, SampleRate: %f)", svcKey, sp.Port, be.LogConfig.Enable, be.LogConfig.SampleRate)
		return true
	}
	return false
}

// expectedBackendServiceLogConfig returns composite.BackendServiceLogConfig for
// the access log settings specified in the BackendConfig to the passed in
// service port.
// This method assumes that sp.BackendConfig.Spec.Logging is not nil.
func expectedBackendServiceLogConfig(sp utils.ServicePort) *composite.BackendServiceLogConfig {
	logConfig := &composite.BackendServiceLogConfig{
		Enable: sp.BackendConfig.Spec.Logging.Enable,
	}
	// Ignore sample rate if logging is not enabled.
	if !sp.BackendConfig.Spec.Logging.Enable || sp.BackendConfig.Spec.Logging.SampleRate == nil {
		return logConfig
	}
	logConfig.SampleRate = *sp.BackendConfig.Spec.Logging.SampleRate
	return logConfig
}
