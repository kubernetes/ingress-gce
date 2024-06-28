/*
Copyright 2024 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package v1alpha1

import (
	"encoding/json"
	"fmt"
)

// Annotations on pods.
const (
	// DisableSourceValidationAnnotationKey is the annotation on pod to disable source IP validation on L2 interfaces.
	// Useful when you want to assign new IPs onto the interface.
	DisableSourceIPValidationAnnotationKey = "networking.gke.io/disable-source-ip-validation"
	// DisableSourceValidationAnnotationValTrue is the value to disable source IP validation for the pod.
	DisableSourceIPValidationAnnotationValTrue = "true"
	// DefaultInterfaceAnnotationKey specifies the default route interface with interface name in pod.
	// The IP of the gateway comes from network CRs.
	DefaultInterfaceAnnotationKey = "networking.gke.io/default-interface"
	// InterfaceAnnotationKey specifies interfaces for pod.
	InterfaceAnnotationKey = "networking.gke.io/interfaces"
	// NodeNetworkAnnotationKey is the key of the annotation which indicates the status of
	// networks on the node.
	NodeNetworkAnnotationKey = "networking.gke.io/network-status"
	// NetworkAnnotationKey is the network annotation on NetworkPolicy object.
	// Value for this key will be the network on which network policy should be enforced.
	NetworkAnnotationKey = "networking.gke.io/network"
	// NetworkInUseAnnotationKey is the annotation on Network object.
	// It's used to indicate if the Network object is referenced by NetworkInterface/pod objects.
	NetworkInUseAnnotationKey = "networking.gke.io/in-use"
	// NetworkInUseAnnotationValTrue is the value to be set for NetworkInUseAnnotationKey to indicate
	// the Network object is referenced by at least one NetworkInterface/pod object.
	NetworkInUseAnnotationValTrue = "true"
)

// InterfaceAnnotation is the value of the interface annotation.
// +kubebuilder:object:generate:=false
type InterfaceAnnotation []InterfaceRef

// InterfaceRef specifies the reference to network interface.
// All fields are mutual exclusive.
// Either Network or Interface field can be specified.
// +kubebuilder:object:generate:=false
type InterfaceRef struct {
	// InterfaceName is the name of the interface in pod network namespace.
	InterfaceName string `json:"interfaceName,omitempty"`
	// Network refers to a network object within the cluster.
	// When network is specified, NetworkInterface object is optionally generated with default configuration.
	Network *string `json:"network,omitempty"`
	// Interface reference the NetworkInterface object within the namespace.
	Interface *string `json:"interface,omitempty"`
}

// ParseInterfaceAnnotation parses the given annotation.
func ParseInterfaceAnnotation(annotation string) (InterfaceAnnotation, error) {
	ret := &InterfaceAnnotation{}
	err := json.Unmarshal([]byte(annotation), ret)
	return *ret, err
}

// MarshalAnnotation marshals any object into string using json.Marshal.
func MarshalAnnotation(a interface{}) (string, error) {
	ret, err := json.Marshal(a)
	if err != nil {
		return "", fmt.Errorf("failed to marshal (%+v): %v", a, err)
	}
	return string(ret), nil
}

// NodeNetworkAnnotation is the value of the network status annotation.
// +kubebuilder:object:generate:=false
type NodeNetworkAnnotation []NodeNetworkStatus

// NodeNetworkStatus specifies the status of a network.
// +kubebuilder:object:generate:=false
type NodeNetworkStatus struct {
	// Name specifies the name of the network.
	Name string `json:"name,omitempty"`
}

// ParseNodeNetworkAnnotation parses the given annotation to NodeNetworkAnnotation.
func ParseNodeNetworkAnnotation(annotation string) (NodeNetworkAnnotation, error) {
	ret := &NodeNetworkAnnotation{}
	err := json.Unmarshal([]byte(annotation), ret)
	return *ret, err
}

// MarshalNodeNetworkAnnotation marshals a NodeNetworkAnnotation into string.
func MarshalNodeNetworkAnnotation(a NodeNetworkAnnotation) (string, error) {
	return MarshalAnnotation(a)
}
