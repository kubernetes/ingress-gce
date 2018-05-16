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
	"k8s.io/ingress-gce/pkg/flags"
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

	// BackendConfigKey is a stringified JSON with two fields:
	// - "ports": a map of port names or port numbers to backendConfig names
	// - "default": denotes the default backendConfig name for all ports except
	// those are explicitly referenced.
	// Examples:
	// - '{"ports":{"my-https-port":"config-https","my-http-port":"config-http"}}'
	// - '{"default":"config-default","ports":{"my-https-port":"config-https"}}'
	BackendConfigKey = "beta.cloud.google.com/backend-config"

	// ProtocolHTTP protocol for a service
	ProtocolHTTP AppProtocol = "HTTP"
	// ProtocolHTTPS protocol for a service
	ProtocolHTTPS AppProtocol = "HTTPS"
	// ProtocolHTTP2 protocol for a service
	ProtocolHTTP2 AppProtocol = "HTTP2"
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
		case ProtocolHTTP2:
			if !flags.F.Features.Http2 {
				return nil, fmt.Errorf("http2 not enabled as port application protocol")
			}
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

type BackendConfigs struct {
	Default string            `json:"default,omitempty"`
	Ports   map[string]string `json:"ports,omitempty"`
}

// GetBackendConfigs returns BackendConfigs for the service.
func (svc Service) GetBackendConfigs() (*BackendConfigs, error) {
	val, ok := svc.v[BackendConfigKey]
	if !ok {
		return nil, nil
	}

	configs := BackendConfigs{}
	if err := json.Unmarshal([]byte(val), &configs); err != nil {
		return nil, err
	}
	if configs.Default == "" && len(configs.Ports) == 0 {
		return nil, fmt.Errorf("no backendConfigs found in annotation: %v", val)
	}
	return &configs, nil
}
