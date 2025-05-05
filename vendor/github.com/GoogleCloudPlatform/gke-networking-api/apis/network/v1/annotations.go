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
package v1

import (
	"encoding/json"
	"fmt"
)

// Annotation definitions.
const (
	// DisableSourceValidationAnnotationKey is the annotation on pod to disable source IP validation on L2 interfaces.
	// Useful when you want to assign new IPs onto the interface.
	DisableSourceIPValidationAnnotationKey = "networking.gke.io/disable-source-ip-validation"
	// DisableSourceValidationAnnotationValTrue is the value to disable source IP validation for the pod.
	DisableSourceIPValidationAnnotationValTrue = "true"
	// DisableSourceMACValidationAnnotationKey is the annotation on pod to disable source MAC validation on L2 interfaces.
	DisableSourceMACValidationAnnotationKey = "networking.gke.io/disable-source-mac-validation"
	// DisableSourceMACValidationAnnotationValTrue is the value to disable source MAC validation for the pod.
	DisableSourceMACValidationAnnotationValTrue = "true"
	// EnableMulticastAnnotationKey is the annotation on pod to enable multicast on L2 interfaces.
	// It's also used to enable IGMP protocol for L2 interfaces.
	EnableMulticastAnnotationKey = "networking.gke.io/enable-multicast"
	// EnableMulticastAnnotationValTrue is the value to enable multicast for the pod.
	EnableMulticastAnnotationValTrue = "true"
	// DefaultInterfaceAnnotationKey specifies the default route interface with interface name in pod.
	// The IP of the gateway comes from network CRs.
	DefaultInterfaceAnnotationKey = "networking.gke.io/default-interface"
	// InterfaceAnnotationKey specifies interfaces for pod.
	InterfaceAnnotationKey = "networking.gke.io/interfaces"
	// NodeNetworkAnnotationKey is the key of the annotation which indicates the status of
	// networks on the node.
	NodeNetworkAnnotationKey = "networking.gke.io/network-status"
	// PodIPsAnnotationKey is the key of the annotation which indicates additional pod IPs assigned to the pod.
	PodIPsAnnotationKey = "networking.gke.io/pod-ips"
	// NetworkAnnotationKey is the network annotation on NetworkPolicy object.
	// Value for this key will be the network on which network policy should be enforced.
	NetworkAnnotationKey = "networking.gke.io/network"
	// NetworkInUseAnnotationKey is the annotation on Network object.
	// It's used to indicate if the Network object is referenced by NetworkInterface/pod objects.
	NetworkInUseAnnotationKey = "networking.gke.io/in-use"
	// NetworkInUseAnnotationValTrue is the value to be set for NetworkInUseAnnotationKey to indicate
	// the Network object is referenced by at least one NetworkInterface/pod object.
	NetworkInUseAnnotationValTrue = "true"
	// MultiNetworkAnnotationKey is the network annotation key used to hold network data per node, eg: PodCIDRs.
	MultiNetworkAnnotationKey = "networking.gke.io/networks"
	// AutoGenAnnotationKey is to indicate if the object is auto-generated.
	AutoGenAnnotationKey = "networking.gke.io/auto-generated"
	// AutoGenAnnotationValTrue is the value to be set for auto-generated objects.
	AutoGenAnnotationValTrue = "true"
	// NorthInterfacesAnnotationKey is the annotation key used to hold interfaces data per node.
	NorthInterfacesAnnotationKey = "networking.gke.io/north-interfaces"
	// NICInfoAnnotationKey specifies the mapping between the fist IP address and the PCI BDF number on the node.
	NICInfoAnnotationKey = "networking.gke.io/nic-info"
	// InterfaceStatusAnnotationKey is the key of the annotation which shows information of each interface of a pod.
	InterfaceStatusAnnotationKey = "networking.gke.io/interface-status"
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

// NICInfoAnnotation is the value of the nic-info annotation
// +kubebuilder:object:generate:=false
type NICInfoAnnotation []NICInfoRef

// NICInfoRef specifies the mapping between a NIC's first IP and its
// PCI address on the node.
// +kubebuilder:object:generate:=false
type NICInfoRef struct {
	// First IP address of the interface.
	BirthIP string `json:"birthIP,omitempty"`
	// PCI address of this device on the node.
	PCIAddress string `json:"pciAddress,omitempty"`
	// Name is the birth name of this interface at node boot time.
	BirthName string `json:"birthName,omitempty"`
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

// PodIPsAnnotation is the value of the pod IPs annotation.
// +kubebuilder:object:generate:=false
type PodIPsAnnotation []PodIP

// InterfaceStatusAnnotation is the value of the network interface status annotation.
// +kubebuilder:object:generate:=false
type InterfaceStatusAnnotation []InterfaceStatus

// MultiNetworkAnnotation is the value of networks annotation.
// +kubebuilder:object:generate:=false
type MultiNetworkAnnotation []NodeNetwork

// NorthInterfacesAnnotation is the value of north-interfaces annotation.
// +kubebuilder:object:generate:=false
type NorthInterfacesAnnotation []NorthInterface

// NodeNetworkStatus specifies the status of a network.
// +kubebuilder:object:generate:=false
type NodeNetworkStatus struct {
	// Name specifies the name of the network.
	Name string `json:"name,omitempty"`

	// IPv4Subnet is the Node internal IPv4 subnet for the network.
	IPv4Subnet string `json:"ipv4-subnet,omitempty"`

	// IPv6Subnet is the Node internal IPv6 subnet for the network.
	IPv6Subnet string `json:"ipv6-subnet,omitempty"`
}

// PodIP specifies the additional pod IPs assigned to the pod.
// This will eventually be merged into the `podIPs` field in PodStatus, so the fields must remain compatible.
// +kubebuilder:object:generate:=false
type PodIP struct {
	// NetworkName refers to the network object associated with this IP.
	NetworkName string `json:"networkName"`

	// IP is an IP address (IPv4 or IPv6) assigned to the pod.
	IP string `json:"ip"`
}

// NodeNetwork specifies network data on a node.
// +kubebuilder:object:generate:=false
type NodeNetwork struct {
	// Name specifies the name of the network.
	Name string `json:"name"`
	// Cidrs denotes the IPv4/IPv6 ranges of the network.
	Cidrs []string `json:"cidrs"`
	// Scope specifies if the network is local to a node or global across a node pool.
	Scope string `json:"scope"`
}

// NorthInterface specifies interface data on a node.
// +kubebuilder:object:generate:=false
type NorthInterface struct {
	// Name of the network an interface on node is connected to.
	Network string `json:"network"`
	// IP address of the interface.
	IpAddress string `json:"ipAddress"`
}

// InterfaceStatus holds information of a NIC of a pod.
// +kubebuilder:object:generate:=false
type InterfaceStatus struct {
	// NetworkName refers to the network object associated with this NIC.
	NetworkName string `json:"networkName"`

	// IPAddresses are the IP addresses assigned to this NIC.
	// Can be either IPv4 or IPv6.
	IPAddresses []string `json:"ipAddresses"`

	// MACAddress is the MAC address assigned to the NIC.
	MACAddress string `json:"macAddress"`

	// Routes contains a list of routes for the network this interface connects to.
	Routes []Route `json:"routes,omitempty"`

	// Gateway4 defines the gateway IPv4 address for the network this interface connects to.
	Gateway4 *string `json:"gateway4,omitempty"`

	// DNSConfig specifies the DNS configuration of the network this interface connects to.
	DNSConfig *DNSConfig `json:"dnsConfig,omitempty"`

	// DHCPServerIP is the IP of the DHCP server.
	DHCPServerIP *string `json:"dhcpServerIP,omitempty"`
}

// ParseNodeNetworkAnnotation parses the given annotation to NodeNetworkAnnotation.
func ParseNodeNetworkAnnotation(annotation string) (NodeNetworkAnnotation, error) {
	ret := &NodeNetworkAnnotation{}
	err := json.Unmarshal([]byte(annotation), ret)
	return *ret, err
}

// ParsePodIPsAnnotation parses the given annotation to PodIPsAnnotation.
func ParsePodIPsAnnotation(annotation string) (PodIPsAnnotation, error) {
	ret := &PodIPsAnnotation{}
	err := json.Unmarshal([]byte(annotation), ret)
	return *ret, err
}

// ParseMultiNetworkAnnotation parses given annotation to MultiNetworkAnnotation.
func ParseMultiNetworkAnnotation(annotation string) (MultiNetworkAnnotation, error) {
	ret := &MultiNetworkAnnotation{}
	err := json.Unmarshal([]byte(annotation), ret)
	return *ret, err
}

// ParseNorthInterfacesAnnotation parses given annotation to NorthInterfacesAnnotation.
func ParseNorthInterfacesAnnotation(annotation string) (NorthInterfacesAnnotation, error) {
	ret := &NorthInterfacesAnnotation{}
	err := json.Unmarshal([]byte(annotation), ret)
	return *ret, err
}

// ParseNICInfoAnnotation parses given annotation to NicInfoAnnotation
func ParseNICInfoAnnotation(annotation string) (NICInfoAnnotation, error) {
	ret := &NICInfoAnnotation{}
	err := json.Unmarshal([]byte(annotation), ret)
	return *ret, err
}

// ParseInterfaceStatusAnnotation parses the given annotation to InterfaceStatusAnnotation
func ParseInterfaceStatusAnnotation(annotation string) (InterfaceStatusAnnotation, error) {
	ret := &InterfaceStatusAnnotation{}
	err := json.Unmarshal([]byte(annotation), ret)
	return *ret, err
}

// MarshalNodeNetworkAnnotation marshals a NodeNetworkAnnotation into string.
func MarshalNodeNetworkAnnotation(a NodeNetworkAnnotation) (string, error) {
	return MarshalAnnotation(a)
}

// MarshalNorthInterfacesAnnotation marshals a NorthInterfacesAnnotation into string.
func MarshalNorthInterfacesAnnotation(a NorthInterfacesAnnotation) (string, error) {
	return MarshalAnnotation(a)
}

// MarshalNICInfoAnnotation marshals a NICInfoAnnotation into string.
func MarshalNICInfoAnnotation(a NICInfoAnnotation) (string, error) {
	return MarshalAnnotation(a)
}
