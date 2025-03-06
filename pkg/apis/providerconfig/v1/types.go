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
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
//
// +k8s:openapi-gen=true
type ProviderConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ProviderConfigSpec   `json:"spec,omitempty"`
	Status ProviderConfigStatus `json:"status,omitempty"`
}

// ProviderConfigSpec defines the desired state of ProviderConfig.
// +k8s:openapi-gen=true
type ProviderConfigSpec struct {
	// ProjectNumber is the GCP project number.
	//
	// +kubebuilder:validation:Minimum=0
	// +(Validation done in accordance with go/elysium/project_ids#project-number)
	ProjectNumber int64 `json:"projectNumber"`
	// ProjectID is the GCP Project ID.
	//
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=30
	// +(Validation done in accordance with https://cloud.google.com/resource-manager/docs/creating-managing-projects#before_you_begin)
	ProjectID string `json:"projectID"`
	// PSC connection ID of the PSC endpoint.
	//
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Optional
	PSCConnectionID int64                 `json:"pscConnectionID"`
	NetworkConfig   ProviderNetworkConfig `json:"networkConfig"`
}

// ProviderNetworkConfig specifies the network configuration for the provider config.
type ProviderNetworkConfig struct {
	// The network where the provider config is to be created.
	Network string `json:"network,omitempty"`
	// The default subnetwork where the provider config is to be created.
	SubnetInfo ProviderConfigSubnetInfo `json:"subnetInfo"`
}

// ProviderConfigSubnetInfo defines the subnet configuration.
type ProviderConfigSubnetInfo struct {
	// Subnetwork is the name of the subnetwork in the format projects/{project}/regions/{region}/subnetworks/{subnet}.
	Subnetwork string `json:"subnetwork"`
	// The primary IP range of the subnet in CIDR notation (e.g.,`10.0.0.0/16`).
	CIDR string `json:"cidr"`
	// PodRanges contains the Pod CIDR ranges that are part of this Subnet.
	PodRanges []ProviderConfigSecondaryRange `json:"podRanges"`
}

// ProviderConfigSecondaryRange describes the configuration of a SecondaryRange.
type ProviderConfigSecondaryRange struct {
	// The name of the secondary range.
	Name string `json:"name"`
	// The secondary IP range in CIDR notation (e.g.,`10.0.0.0/16`).
	CIDR string `json:"cidr"`
}

// ProviderConfigStatus defines the current state of ProviderConfig.
type ProviderConfigStatus struct {
	// Conditions describe the current conditions of the ProviderConfig.
	//
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// ProviderConfigList is a list of ProviderConfig resources
type ProviderConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	Items           []ProviderConfig `json:"items"`
}
