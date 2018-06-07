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

package translator

import (
	api_v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// ServicePort is a helper function that retrieves a port of a Service.
func ServicePort(svc api_v1.Service, port intstr.IntOrString) *api_v1.ServicePort {
	var svcPort *api_v1.ServicePort
PortLoop:
	for _, p := range svc.Spec.Ports {
		np := p
		switch port.Type {
		case intstr.Int:
			if p.Port == port.IntVal {
				svcPort = &np
				break PortLoop
			}
		default:
			if p.Name == port.StrVal {
				svcPort = &np
				break PortLoop
			}
		}
	}
	return svcPort
}
