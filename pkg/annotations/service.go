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
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
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

	// ExposeNEGAnnotationKey is the annotation key to specify standalone NEGs associated
	// with the service. This should be a valid JSON string, as defined in
	// ExposeNegAnnotation.
	// example: {"80":{},"443":{}}
	ExposeNEGAnnotationKey = "cloud.google.com/expose-load-balancer-neg"

	// NEGStatusKey is the annotation key whose value is the status of the NEGs
	// on the Service, and is applied by the NEG Controller.
	NEGStatusKey = "cloud.google.com/neg"

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

// ExposeNegAnnotation is the format of the annotation associated with the
// ExposeNEGAnnotationKey key, and maps ServicePort to attributes of the NEG that should be
// associated with the ServicePort. ServicePorts in this map will be NEG-enabled.
type ExposeNegAnnotation map[int32]NegAttributes

// NegAttributes houses the attributes of the NEGs that are associated with the
// service. Future extensions to the Expose NEGs annotation should be added here.
type NegAttributes struct {
	kind string
}

// AppProtocol describes the service protocol.
type AppProtocol string

// Service represents Service annotations.
type Service struct {
	v map[string]string
}

// PortNameMap is a map of ServicePort:TargetPort.
type PortNameMap map[int32]string

// FromService extracts the annotations from an Service definition.
func FromService(obj *v1.Service) *Service {
	return &Service{obj.Annotations}
}

// Union returns the union of the Port:TargetPort mappings
func (p1 PortNameMap) Union(p2 PortNameMap) PortNameMap {
	result := make(PortNameMap)
	for svcPort, targetPort := range p1 {
		result[svcPort] = targetPort
	}

	for svcPort, targetPort := range p2 {
		result[svcPort] = targetPort
	}

	return result
}

// Difference returns the set of Port:TargetPorts in p2 that aren't present in p1
func (p1 PortNameMap) Difference(p2 PortNameMap) PortNameMap {
	result := make(PortNameMap)
	for svcPort, targetPort := range p1 {
		if _, ok := p2[svcPort]; !ok {
			result[svcPort] = targetPort
		}
	}

	return result
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
	v, ok := svc.v[NetworkEndpointGroupAlphaAnnotation]
	return ok && v == "true"
}

var (
	ErrBackendConfigNoneFound         = errors.New("no BackendConfig's found in annotation")
	ErrBackendConfigInvalidJSON       = errors.New("annotation is invalid json")
	ErrBackendConfigAnnotationMissing = errors.New("annotation is missing")
)

// NEGExposed is true if the service exposes NEGs
func (svc *Service) NEGExposed() bool {
	v, ok := svc.v[ExposeNEGAnnotationKey]
	return ok && len(v) > 0
}

// NEGEnabled is true if the service uses NEGs.
func (svc *Service) NEGEnabled() bool {
	return svc.NEGExposed() || svc.NEGEnabledForIngress()
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

// NEGServicePorts returns the parsed ServicePorts from the annotation.
// knownPorts represents the known Port:TargetPort attributes of servicePorts
// that already exist on the service. This function returns an error if
// any of the parsed ServicePorts from the annotation is not in knownPorts.
func (svc *Service) NEGServicePorts(knownPorts PortNameMap) (PortNameMap, error) {
	v, ok := svc.v[ExposeNEGAnnotationKey]
	if !ok {
		return nil, fmt.Errorf("No NEG ServicePorts specified")
	}

	// TODO: add link to Expose NEG documentation when complete
	var exposedNegPortMap ExposeNegAnnotation
	if err := json.Unmarshal([]byte(v), &exposedNegPortMap); err != nil {
		return nil, fmt.Errorf("NEG annotation %s is not well-formed: %v", v, err)
	}

	portSet := make(PortNameMap)
	var errList []error
	for port, _ := range exposedNegPortMap {
		// TODO: also validate ServicePorts in the exposed NEG annotation via webhook
		if _, ok := knownPorts[port]; !ok {
			errList = append(errList, fmt.Errorf("ServicePort %v doesn't exist on Service", port))
		}
		portSet[port] = knownPorts[port]
	}

	return portSet, utilerrors.NewAggregate(errList)
}

// NegServiceState contains name and zone of the Network Endpoint Group
// resources associated with this service
type NegServiceState struct {
	// NetworkEndpointGroups returns the mapping between service port and NEG
	// resource. key is service port, value is the name of the NEG resource.
	NetworkEndpointGroups PortNameMap `json:"network_endpoint_groups,omitempty"`
	Zones                 []string    `json:"zones,omitempty"`
}

// GenNegServiceState generates a NegServiceState denoting the current NEGs
// associated with the given ports.
// NetworkEndpointGroups is a mapping between ServicePort and NEG name
// Zones is a list of zones where the NEGs exist.
func GenNegServiceState(zones []string, portToNegs PortNameMap) NegServiceState {
	res := NegServiceState{}
	res.NetworkEndpointGroups = make(PortNameMap)
	res.Zones = zones
	res.NetworkEndpointGroups = portToNegs
	return res
}
