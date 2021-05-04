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
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/legacy-cloud-providers/gce"
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

	// ProtocolHTTP protocol for a service
	ProtocolHTTP AppProtocol = "HTTP"
	// ProtocolHTTPS protocol for a service
	ProtocolHTTPS AppProtocol = "HTTPS"
	// ProtocolHTTP2 protocol for a service
	ProtocolHTTP2 AppProtocol = "HTTP2"

	// ServiceStatusPrefix is the prefix used in annotations used to record
	// debug information in the Service annotations. This is applicable to L4 ILB services.
	ServiceStatusPrefix = "service.kubernetes.io"
	// TCPForwardingRuleKey is the annotation key used by l4 controller to record
	// GCP TCP forwarding rule name.
	TCPForwardingRuleKey = ServiceStatusPrefix + "/tcp-forwarding-rule"
	// UDPForwardingRuleKey is the annotation key used by l4 controller to record
	// GCP UDP forwarding rule name.
	UDPForwardingRuleKey = ServiceStatusPrefix + "/udp-forwarding-rule"
	// BackendServiceKey is the annotation key used by l4 controller to record
	// GCP Backend service name.
	BackendServiceKey = ServiceStatusPrefix + "/backend-service"
	// FirewallRuleKey is the annotation key used by l4 controller to record
	// GCP Firewall rule name.
	FirewallRuleKey = ServiceStatusPrefix + "/firewall-rule"
	// HealthcheckKey is the annotation key used by l4 controller to record
	// GCP Healthcheck name.
	HealthcheckKey = ServiceStatusPrefix + "/healthcheck"
	// FirewallRuleForHealthcheckKey is the annotation key used by l4 controller to record
	// the firewall rule name that allows healthcheck traffic.
	FirewallRuleForHealthcheckKey = ServiceStatusPrefix + "/firewall-rule-for-hc"
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

// PortNegMap is the mapping between service port to NEG name
type PortNegMap map[string]string

// NegStatus contains name and zone of the Network Endpoint Group
// resources associated with this service
type NegStatus struct {
	// NetworkEndpointGroups returns the mapping between service port and NEG
	// resource. key is service port, value is the name of the NEG resource.
	NetworkEndpointGroups PortNegMap `json:"network_endpoint_groups,omitempty"`
	// Zones is a list of zones where the NEGs exist.
	Zones []string `json:"zones,omitempty"`
}

func (ns NegStatus) Marshal() (string, error) {
	ret := ""
	bytes, err := json.Marshal(ns)
	if err != nil {
		return ret, err
	}
	return string(bytes), err
}

// NewNegStatus generates a NegStatus denoting the current NEGs
// associated with the given ports.
func NewNegStatus(zones []string, portToNegs PortNegMap) NegStatus {
	res := NegStatus{}
	res.Zones = zones
	res.NetworkEndpointGroups = portToNegs
	return res
}

// ParseNegStatus parses the given annotation into NEG status struct
func ParseNegStatus(annotation string) (NegStatus, error) {
	ret := &NegStatus{}
	err := json.Unmarshal([]byte(annotation), ret)
	return *ret, err
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

// WantsL4ILB checks if the given service requires L4 ILB.
// the function returns a boolean as well as the loadbalancer type(string).
func WantsL4ILB(service *v1.Service) (bool, string) {
	if service == nil {
		return false, ""
	}
	if service.Spec.Type != v1.ServiceTypeLoadBalancer {
		return false, fmt.Sprintf("Type : %s", service.Spec.Type)
	}
	ltype := gce.GetLoadBalancerAnnotationType(service)
	if ltype == gce.LBTypeInternal {
		return true, fmt.Sprintf("Type : %s, LBType : %s", service.Spec.Type, ltype)
	}
	return false, fmt.Sprintf("Type : %s, LBType : %s", service.Spec.Type, ltype)
}

// OnlyStatusAnnotationsChanged returns true if the only annotation change between the 2 services is the NEG or ILB
// resources annotations.
// Note : This assumes that the annotations in old and new service are different. If they are identical, this will
// return true.
func OnlyStatusAnnotationsChanged(oldService, newService *v1.Service) bool {
	return onlyStatusAnnotationsChanged(oldService, newService) && onlyStatusAnnotationsChanged(newService, oldService)
}

// onlyStatusAnnotationsChanged returns true if the NEG Status or ILB resources annotations are the only extra
// annotations present in the new service but not in the old service.
// Note : This assumes that the annotations in old and new service are different. If they are identical, this will
// return true.
func onlyStatusAnnotationsChanged(oldService, newService *v1.Service) bool {
	for key, val := range newService.ObjectMeta.Annotations {
		if oldVal, ok := oldService.ObjectMeta.Annotations[key]; !ok || oldVal != val {
			if key == NEGStatusKey || strings.HasPrefix(key, ServiceStatusPrefix) {
				continue
			}
			return false
		}
	}
	return true
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
func (svc *Service) NEGAnnotation() (*NegAnnotation, bool, error) {
	var res NegAnnotation
	annotation, ok := svc.v[NEGAnnotationKey]
	if !ok {
		return nil, false, nil
	}

	// TODO: add link to Expose NEG documentation when complete
	if err := json.Unmarshal([]byte(annotation), &res); err != nil {
		return nil, true, ErrNEGAnnotationInvalid
	}

	return &res, true, nil
}

func (svc *Service) NEGStatus() (*NegStatus, bool, error) {
	var res NegStatus
	var err error

	annotation, ok := svc.v[NEGStatusKey]
	if !ok {
		return nil, false, nil
	}

	if res, err = ParseNegStatus(annotation); err != nil {
		return nil, true, fmt.Errorf("Error parsing neg status: %v", err)
	}

	return &res, true, nil
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
