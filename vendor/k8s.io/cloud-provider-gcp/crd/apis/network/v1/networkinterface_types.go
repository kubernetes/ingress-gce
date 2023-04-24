package v1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp",description="The age of this resource"
// +kubebuilder:printcolumn:name="IP",type="string",JSONPath=".status.ipAddresses[0]",description="IP address assigned to this interface"
// +kubebuilder:printcolumn:name="MAC",type="string",JSONPath=".status.macAddress",description="MAC address assigned to this interface"
// +kubebuilder:printcolumn:name="NETWORK",type="string",JSONPath=".spec.networkName",description="The Network this interface connects to"
// +kubebuilder:storageversion
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// NetworkInterface defines the network interface for a pod to connect to a network.
type NetworkInterface struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NetworkInterfaceSpec   `json:"spec,omitempty"`
	Status NetworkInterfaceStatus `json:"status,omitempty"`
}

// NetworkInterfaceSpec is the specification for the NetworkInterface resource.
type NetworkInterfaceSpec struct {
	// NetworkName refers to a network object that this NetworkInterface is connected.
	// +required
	// +kubebuilder:validation:MinLength=1
	NetworkName string `json:"networkName"`

	// IpAddresses specifies the static IP addresses on this NetworkInterface.
	// Each IPAddress may contain subnet mask. If subnet mask is not included, /32 is taken as default.
	// For example, IPAddress input 1.2.3.4 will be taken as 1.2.3.4/32. Alternatively, the input can be 1.2.3.4/24
	// with subnet mask of /24.
	// +optional
	IpAddresses []string `json:"ipAddresses,omitempty"`

	// Macddress specifies the static MAC address on this NetworkInterface.
	// +optional
	MacAddress *string `json:"macAddress,omitempty"`
}

// NetworkInterfaceStatus is the status for the NetworkInterface resource.
type NetworkInterfaceStatus struct {
	// IpAddresses are the IP addresses assigned to the NetworkInterface.
	IpAddresses []string `json:"ipAddresses,omitempty"`
	// MacAddress is the MAC address assigned to the NetworkInterface.
	MacAddress string `json:"macAddress,omitempty"`

	// Routes contains a list of routes for the network this interface connects to.
	Routes []Route `json:"routes,omitempty"`

	// Gateway4 defines the gateway IPv4 address for the network this interface connects to.
	Gateway4 *string `json:"gateway4,omitempty"`

	// Specifies the DNS configuration of the network this interface connects to.
	// +optional
	DNSConfig *DNSConfig `json:"dnsConfig,omitempty"`

	// PodName specifies the current pod name this interface is connected to
	// +optional
	PodName *string `json:"podName,omitempty"`

	//// Conditions include the the conditions associated with this Interface
	// Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +genclient
// +genclient:onlyVerbs=get
// +kubebuilder:object:root=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// NetworkInterfaceList contains a list of NetworkInterface resources.
type NetworkInterfaceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	// Items is a slice of NetworkInterface resources.
	Items []NetworkInterface `json:"items"`
}
