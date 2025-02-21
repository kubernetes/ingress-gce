/*
Copyright 2024 The Kubernetes Authors.
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
package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ProviderConfig is the Schema for the ProviderConfig resource in the Multi-Project cluster.
// +genclient
// +genclient:noStatus
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
//
// +k8s:openapi-gen=true
type ProviderConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ProviderConfigSpec   `json:"spec,omitempty"`
	Status ProviderConfigStatus `json:"status,omitempty"`
}

// ProviderConfigSpec is the spec for a  resource
// +k8s:openapi-gen=true
type ProviderConfigSpec struct {
	// The ID of the project where the provider config is to be created.
	ProjectID string `json:"projectID,omitempty"`
	// The project number where the provider config is to be created.
	ProjectNumber int64 `json:"projectNumber,omitempty"`
	// The network configuration for the provider config.
	NetworkConfig *NetworkConfig `json:"networkConfig,omitempty"`
}

// NetworkConfig specifies the network configuration for the provider config.
type NetworkConfig struct {
	// The network where the provider config is to be created.
	Network string `json:"network,omitempty"`
	// The default subnetwork where the provider config is to be created.
	DefaultSubnetwork string `json:"defaultSubnetwork,omitempty"`
}

// ProviderConfigStatus is the status for a ProviderConfig resource
type ProviderConfigStatus struct {
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// ProviderConfigList is a list of ProviderConfig resources
type ProviderConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	Items           []ProviderConfig `json:"items"`
}
