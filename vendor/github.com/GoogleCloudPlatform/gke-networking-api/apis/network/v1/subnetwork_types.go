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

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// +genclient
// +genclient:nonNamespaced
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:storageversion
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Subnetwork defines configuration for a subnetwork of a network.
type Subnetwork struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SubnetworkSpec   `json:"spec,omitempty"`
	Status SubnetworkStatus `json:"status,omitempty"`
}

// SubnetworkSpec is the desired configuration for the Subnetwork resource.
type SubnetworkSpec struct {
	// NetworkName refers to a network object that this Subnetwork is connected.
	// +required
	// +kubebuilder:validation:MinLength=1
	NetworkName string `json:"networkName"`
	// Gateway4 defines the gateway IPv4 address for the Subnetwork.
	// If specified, it will override the `Gateway4` field in the Network API.
	// If not specfied, the Gateway IP will either be derived from the Network API
	// or assigned automatically.
	// +optional
	// +kubebuilder:validation:Format=ipv4
	Gateway4 *string `json:"gateway4,omitempty"`
}

// SubnetworkStatus is the status for the Subnetwork resource.
type SubnetworkStatus struct {
	// Gateway4 defines the gateway IPv4 address for the Subnetwork.
	// +optional
	// +kubebuilder:validation:Format=ipv4
	Gateway4 *string `json:"gateway4,omitempty"`
}

// +kubebuilder:object:root=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// SubnetworkList contains a list of Subnetwork resources.
type SubnetworkList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	// Items is a slice of Subnetwork resources.
	Items []Subnetwork `json:"items"`
}
