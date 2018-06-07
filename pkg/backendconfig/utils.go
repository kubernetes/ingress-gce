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

package backendconfig

import (
	"strconv"

	apiv1 "k8s.io/api/core/v1"

	"k8s.io/ingress-gce/pkg/annotations"
)

// BackendConfigName returns the name of the BackendConfig which is associated
// with the given ServicePort.
func BackendConfigName(backendConfigs annotations.BackendConfigs, svcPort apiv1.ServicePort) string {
	configName := ""
	// Both port name and port number are allowed.
	if name, ok := backendConfigs.Ports[svcPort.Name]; ok {
		configName = name
	} else if name, ok := backendConfigs.Ports[strconv.Itoa(int(svcPort.Port))]; ok {
		configName = name
	} else if len(backendConfigs.Default) != 0 {
		configName = backendConfigs.Default
	}
	return configName
}
