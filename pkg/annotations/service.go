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
	"errors"
	"fmt"

	v1 "k8s.io/api/core/v1"
)

const (
	// ServiceApplicationProtocolKey and GoogleServiceApplicationProtocolKey
	// is a stringified JSON map of port names to protocol strings.
	// Possible values are HTTP, HTTPS and HTTP2.
	// Example:
	// '{"my-https-port":"HTTPS","my-http-port":"HTTP"}'
	// Note: ServiceApplicationProtocolKey will be deprecated.
	ServiceApplicationProtocolKey       = "service.alpha.kubernetes.io/app-protocols"
	GoogleServiceApplicationProtocolKey = "cloud.google.com/app-protocols"

	// BetaBackendConfigKey is a stringified JSON with two fields:
	// - "ports": a map of port names or port numbers to backendConfig names
	// - "default": denotes the default backendConfig name for all ports except
	// those are explicitly referenced.
	// Examples:
	// - '{"ports":{"my-https-port":"config-https","my-http-port":"config-http"}}'
	// - '{"default":"config-default","ports":{"my-https-port":"config-https"}}'
	BetaBackendConfigKey = "beta.cloud.google.com/backend-config"

	// BackendConfigKey is GA version of backend config key.
	BackendConfigKey = "cloud.google.com/backend-config"

	// THCAnnotationKey is the boolean annotation key to enable Transparent Health Checks.
	THCAnnotationKey = "networking.gke.io/transparent-health-checker"

	// ProtocolHTTP protocol for a service
	ProtocolHTTP AppProtocol = "HTTP"
	// ProtocolHTTPS protocol for a service
	ProtocolHTTPS AppProtocol = "HTTPS"
	// ProtocolHTTP2 protocol for a service
	ProtocolHTTP2 AppProtocol = "HTTP2"
	// ProtocolGRPC protocol for a service
	ProtocolGRPC AppProtocol = "GRPC"
	// ProtocolGRPCWithTLS protocol for a service
	ProtocolGRPCWithTLS AppProtocol = "GRPC_WITH_TLS"
)

// THCAnnotation is the format of the annotation associated with the THCAnnotationKey key.
type THCAnnotation struct {
	// "enabled" indicates whether to enable the Transparent Health Checks feature.
	Enabled bool `json:"enabled,omitempty"`
}

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
func (svc *Service) ApplicationProtocols() (map[string]AppProtocol, error) {
	var val string
	var ok bool
	// First check the old annotation, then fall back to the new one.
	val, ok = svc.v[ServiceApplicationProtocolKey]
	if !ok {
		val, ok = svc.v[GoogleServiceApplicationProtocolKey]
		if !ok {
			return map[string]AppProtocol{}, nil
		}
	}

	var portToProtos map[string]AppProtocol
	err := json.Unmarshal([]byte(val), &portToProtos)

	// Verify protocol is an accepted value
	for _, proto := range portToProtos {
		switch proto {
		case ProtocolHTTP, ProtocolHTTPS, ProtocolHTTP2, ProtocolGRPC, ProtocolGRPCWithTLS:
			// accepted
		default:
			return nil, fmt.Errorf("invalid port application protocol: %v", proto)
		}
	}

	return portToProtos, err
}

var (
	ErrBackendConfigNoneFound         = errors.New("no BackendConfig's found in annotation")
	ErrBackendConfigInvalidJSON       = errors.New("BackendConfig annotation is invalid json")
	ErrBackendConfigAnnotationMissing = errors.New("BackendConfig annotation is missing")
	ErrTHCAnnotationInvalid           = errors.New("THC annotation is invalid")
)

// IsThcAnnotated returns true if a THC annotation is found and its value is true.
func (svc *Service) IsThcAnnotated() (bool, error) {
	var res THCAnnotation
	annotation, ok := svc.v[THCAnnotationKey]
	if !ok {
		return false, nil
	}

	if err := json.Unmarshal([]byte(annotation), &res); err != nil {
		return false, ErrTHCAnnotationInvalid
	}

	return res.Enabled, nil
}

type BackendConfigs struct {
	Default string            `json:"default,omitempty"`
	Ports   map[string]string `json:"ports,omitempty"`
}

// GetBackendConfigs returns BackendConfigs for the service.
func (svc *Service) GetBackendConfigs() (*BackendConfigs, error) {
	val, ok := svc.getBackendConfigAnnotation()
	if !ok {
		return nil, ErrBackendConfigAnnotationMissing
	}

	configs := BackendConfigs{}
	if err := json.Unmarshal([]byte(val), &configs); err != nil {
		return nil, ErrBackendConfigInvalidJSON
	}
	if configs.Default == "" && len(configs.Ports) == 0 {
		return nil, ErrBackendConfigNoneFound
	}
	return &configs, nil
}

// getBackendConfigAnnotation returns specified backendconfig annotation value.
// Returns false if both beta and ga annotations are not specified.
// Note that GA annotation is returned if both beta and ga annotations are specified.
func (svc *Service) getBackendConfigAnnotation() (string, bool) {
	for _, bcKey := range []string{BackendConfigKey, BetaBackendConfigKey} {
		val, ok := svc.v[bcKey]
		if ok {
			return val, ok
		}
	}
	return "", false
}
