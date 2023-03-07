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

// PodIPsAnnotation is the value of the pod IPs annotation.
// +kubebuilder:object:generate:=false
type PodIPsAnnotation []PodIP

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

// MarshalNodeNetworkAnnotation marshals a NodeNetworkAnnotation into string.
func MarshalNodeNetworkAnnotation(a NodeNetworkAnnotation) (string, error) {
	return MarshalAnnotation(a)
}

// MarshalNorthInterfacesAnnotation marshals a NorthInterfacesAnnotation into string.
func MarshalNorthInterfacesAnnotation(a NorthInterfacesAnnotation) (string, error) {
	return MarshalAnnotation(a)
}
