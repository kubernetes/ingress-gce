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
	// ServiceApplicationProtocolKey is a stringified JSON map of port names to
	// protocol strings. Possible values are HTTP, HTTPS
	// Example:
	// '{"my-https-port":"HTTPS","my-http-port":"HTTP"}'
	ServiceApplicationProtocolKey = "service.alpha.kubernetes.io/app-protocols"

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

// NEGEnabledForIngress returns true if the annotation is to be applied on
// Ingress-referenced ports
func (svc *Service) NEGEnabledForIngress() bool {
	annotation, err := svc.NegAnnotation()
	if err != nil {
		return false
	}
	return annotation.Ingress
}

var (
	ErrBackendConfigNoneFound         = errors.New("no BackendConfig's found in annotation")
	ErrBackendConfigInvalidJSON       = errors.New("BackendConfig annotation is invalid json")
	ErrBackendConfigAnnotationMissing = errors.New("BackendConfig annotation is missing")
)

// NEGExposed is true if the service exposes NEGs
func (svc *Service) NEGExposed() bool {
	if !flags.F.Features.NEGExposed {
		return false
	}

	annotation, err := svc.NegAnnotation()
	if err != nil {
		return false
	}
	return len(annotation.ExposedPorts) > 0
}

var (
	ErrExposeNegAnnotationMissing = errors.New("No NEG ServicePorts specified")
	ErrExposeNegAnnotationInvalid = errors.New("Expose NEG annotation is invalid")
)

// NegAnnotation returns the value of the  NEG annotation key
func (svc *Service) NegAnnotation() (NegAnnotation, error) {
	var res NegAnnotation
	annotation, ok := svc.v[NEGAnnotationKey]
	if !ok {
		return res, ErrExposeNegAnnotationMissing
	}

	// TODO: add link to Expose NEG documentation when complete
	if err := json.Unmarshal([]byte(annotation), &res); err != nil {
		return res, ErrExposeNegAnnotationInvalid
	}

	return res, nil
}

// NEGEnabled is true if the service uses NEGs.
func (svc *Service) NEGEnabled() bool {
	return svc.NEGEnabledForIngress() || svc.NEGExposed()
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
