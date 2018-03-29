/*
Copyright 2017 The Kubernetes Authors.

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

package annotations

import (
	"encoding/json"
	"fmt"

	"k8s.io/api/core/v1"
)

const (
	// ServiceApplicationProtocolKey is a stringified JSON map of port names to
	// protocol strings. Possible values are HTTP, HTTPS
	// Example:
	// '{"my-https-port":"HTTPS","my-http-port":"HTTP"}'
	ServiceApplicationProtocolKey = "service.alpha.kubernetes.io/app-protocols"

	// NetworkEndpointGroupAlphaAnnotation is the annotation key to enable GCE NEG feature for ingress backend services.
	// To enable this feature, the value of the annotation must be "true".
	// This annotation should be specified on services that are backing ingresses.
	// WARNING: The feature will NOT be effective in the following circumstances:
	// 1. NEG feature is not enabled in feature gate.
	// 2. Service is not referenced in any ingress.
	// 3. Adding this annotation on ingress.
	NetworkEndpointGroupAlphaAnnotation = "alpha.cloud.google.com/load-balancer-neg"

	// ProtocolHTTP protocol for a service
	ProtocolHTTP AppProtocol = "HTTP"
	// ProtocolHTTPS protocol for a service
	ProtocolHTTPS AppProtocol = "HTTPS"

	// ServiceExtensionReferenceKey is the annotation key used for referencing
	// a ServiceExtension from Service.
	ServiceExtensionReferenceKey = "alpha.cloud.google.com/service-extension"
)

// AppProtocol describes the service protocol.
type AppProtocol string

// Service represents Service annotations.
type Service struct {
	v map[string]string
}

// FromService extracts the annotations from an Service definition.
func FromService(obj *v1.Service) *Service {
	return &Service{obj.Annotations}
}

// ApplicationProtocols returns a map of port (name or number) to the protocol
// on the port.
func (svc Service) ApplicationProtocols() (map[string]AppProtocol, error) {
	val, ok := svc.v[ServiceApplicationProtocolKey]
	if !ok {
		return map[string]AppProtocol{}, nil
	}

	var portToProtos map[string]AppProtocol
	err := json.Unmarshal([]byte(val), &portToProtos)

	// Verify protocol is an accepted value
	for _, proto := range portToProtos {
		switch proto {
		case ProtocolHTTP, ProtocolHTTPS:
		default:
			return nil, fmt.Errorf("invalid port application protocol: %v", proto)
		}
	}

	return portToProtos, err
}

// NEGEnabled is true if the service uses NEGs.
func (svc Service) NEGEnabled() bool {
	v, ok := svc.v[NetworkEndpointGroupAlphaAnnotation]
	return ok && v == "true"
}

// ServiceExtensionName returns the name of ServiceExtension if exsits.
func (svc Service) ServiceExtensionName() string {
	svcExtName, ok := svc.v[ServiceExtensionReferenceKey]
	if !ok {
		return ""
	}
	return svcExtName
}
