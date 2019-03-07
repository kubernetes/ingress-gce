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

	"k8s.io/api/core/v1"
	"k8s.io/ingress-gce/pkg/flags"
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

	// NEGAnnotationKey is the annotation key to enable GCE NEG.
	// The value of the annotation must be a valid JSON string in the format
	// specified by type NegAnnotation. To enable, must have either Ingress: true
	// or a non-empty ExposedPorts map referencing valid ServicePorts.
	// examples:
	// - `{"exposed_ports":{"80":{},"443":{}}}`
	// - `{"ingress":true}`
	// - `{"ingress": true,"exposed_ports":{"3000":{},"4000":{}}}`
	NEGAnnotationKey = "cloud.google.com/neg"

	// NEGStatusKey is the annotation key whose value is the status of the NEGs
	// on the Service, and is applied by the NEG Controller.
	NEGStatusKey = "cloud.google.com/neg-status"

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

// NegAnnotation is the format of the annotation associated with the
// NEGAnnotationKey key.
type NegAnnotation struct {
	// "Ingress" indicates whether to enable NEG feature for Ingress referencing
	// the service. Each NEG correspond to a service port.
	// NEGs will be created and managed under the following conditions:
	// 1. Service is referenced by ingress
	// 2. "ingress" is set to "true". Default to "false"
	// When the above conditions are satisfied, Ingress will create a load balancer
	//  and target corresponding NEGs as backends. Service Nodeport is not required.
	Ingress bool `json:"ingress,omitempty"`
	// ExposedPorts specifies the service ports to be exposed as stand-alone NEG.
	// The exposed NEGs will be created and managed by NEG controller.
	// ExposedPorts maps ServicePort to attributes of the NEG that should be
	// associated with the ServicePort.
	ExposedPorts map[int32]NegAttributes `json:"exposed_ports,omitempty"`
}

// NegAttributes houses the attributes of the NEGs that are associated with the
// service. Future extensions to the Expose NEGs annotation should be added here.
type NegAttributes struct {
	// Note - in the future, this will be used for custom naming of NEGs.
	// Currently has no effect.
	Name string `json:"name,omitempty"`
}

// NEGEnabledForIngress returns true if the annotation is to be applied on
// Ingress-referenced ports
func (n *NegAnnotation) NEGEnabledForIngress() bool {
	return n.Ingress
}

// NEGExposed is true if the service exposes NEGs
func (n *NegAnnotation) NEGExposed() bool {
	return len(n.ExposedPorts) > 0
}

// NEGExposed is true if the service uses NEG
func (n *NegAnnotation) NEGEnabled() bool {
	return n.NEGEnabledForIngress() || n.NEGExposed()
}

func (n *NegAnnotation) String() string {
	bytes, _ := json.Marshal(n)
	return string(bytes)
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

var (
	ErrBackendConfigNoneFound         = errors.New("no BackendConfig's found in annotation")
	ErrBackendConfigInvalidJSON       = errors.New("BackendConfig annotation is invalid json")
	ErrBackendConfigAnnotationMissing = errors.New("BackendConfig annotation is missing")
	ErrNEGAnnotationInvalid           = errors.New("NEG annotation is invalid.")
)

// NEGAnnotation returns true if NEG annotation is found.
// If found, it also returns NEG annotation struct.
func (svc *Service) NEGAnnotation() (bool, *NegAnnotation, error) {
	var res NegAnnotation
	annotation, ok := svc.v[NEGAnnotationKey]
	if !ok {
		return false, nil, nil
	}

	// TODO: add link to Expose NEG documentation when complete
	if err := json.Unmarshal([]byte(annotation), &res); err != nil {
		return true, nil, ErrNEGAnnotationInvalid
	}

	return true, &res, nil
}

type BackendConfigs struct {
	Default string            `json:"default,omitempty"`
	Ports   map[string]string `json:"ports,omitempty"`
}

// GetBackendConfigs returns BackendConfigs for the service.
func (svc *Service) GetBackendConfigs() (*BackendConfigs, error) {
	val, ok := svc.v[BackendConfigKey]
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
