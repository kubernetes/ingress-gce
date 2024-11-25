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

// ClusterSlice is the Schema for the ClusterSlice resource in the Multi-Project cluster.
// +genclient
// +genclient:noStatus
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
//
// +k8s:openapi-gen=true
type ClusterSlice struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClusterSliceSpec   `json:"spec,omitempty"`
	Status ClusterSliceStatus `json:"status,omitempty"`
}

// ClusterSliceSpec is the spec for a  resource
// +k8s:openapi-gen=true
type ClusterSliceSpec struct {
	// The ID of the project where the cluster slice is to be created.
	ProjectID string `json:"projectID,omitempty"`
	// The project number where the cluster slice is to be created.
	ProjectNumber int64 `json:"projectNumber,omitempty"`
	// The network configuration for the cluster slice.
	NetworkConfig *NetworkConfig `json:"networkConfig,omitempty"`
}

// NetworkConfig specifies the network configuration for the cluster slice.
type NetworkConfig struct {
	// The network where the cluster slice is to be created.
	Network string `json:"network,omitempty"`
	// The default subnetwork where the cluster slice is to be created.
	DefaultSubnetwork string `json:"defaultSubnetwork,omitempty"`
}

// ClusterSliceStatus is the status for a ClusterSlice resource
type ClusterSliceStatus struct {
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// ClusterSliceList is a list of ClusterSlice resources
type ClusterSliceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	Items           []ClusterSlice `json:"items"`
}
