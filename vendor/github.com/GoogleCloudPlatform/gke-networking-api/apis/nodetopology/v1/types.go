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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +genclient:nonNamespaced
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=tp,scope=Cluster
// +kubebuilder:storageversion
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// NodeTopology describes the VPC network configuration for the cluster.
//
// This resource is a singleton.
type NodeTopology struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NodeTopologySpec   `json:"spec,omitempty"`
	Status NodeTopologyStatus `json:"status,omitempty"`
}

// NodeTopologySpec is the spec for a NodeTopology resource
type NodeTopologySpec struct{}

// NodeTopologyStatus is the status for a NodeTopology resource
// +k8s:openapi-gen=true
type NodeTopologyStatus struct {
	// Zones specifies the current node zones of the GKE cluster that corresponds
	// to this NodeTopology Resource.
	// +required
	Zones []string `json:"zones"`

	// Subnets contains the list of subnets used by the GKE cluster that
	// corresponds to this Node Topology Resource.
	// +required
	Subnets []SubnetConfig `json:"subnets"`

	// Conditions contains the latest conditions observed of this Node Tolology
	// resource.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []Condition `json:"conditions,omitempty"`
}

// SubnetConfig describes the configuration of a GKE subnetwork.
type SubnetConfig struct {
	// Name is the short name of the subnetwork.
	// More info: https://cloud.google.com/vpc/docs/subnets
	// +required
	Name string `json:"name"`

	// SubnetPath is the fully qualified resource path of this subnet.
	// Examples: projects/my-project/regions/us-central1/subnetworks/my-subnet
	// +required
	SubnetPath string `json:"subnetPath"`
}

// PodCondition contains details for the current condition of this Node
// Topology resource.
type Condition struct {
	// Type is the type of the condition.
	// +required
	Type string `json:"type" protobuf:"bytes,1,opt,name=type"`
	// Status of the condition, one of True, False, Unknown.
	// +required
	Status corev1.ConditionStatus `json:"status" protobuf:"bytes,2,opt,name=status"`
	// Last time the condition transitioned from one status to another.
	// +optional
	LastTransitionTime metav1.Time `json:"lastTransitionTime" protobuf:"bytes,4,opt,name=lastTransitionTime"`
	// The reason for the condition's last transition
	// +optional
	Reason string `json:"reason" protobuf:"bytes,5,opt,name=reason"`
	// A human readable message indicating details about the transition.
	// +optional
	Message string `json:"message" protobuf:"bytes,6,opt,name=message"`
}

// These are valid conditions of NodeTopology resource.
const (
	// Synced means the NodeTopology resource has been synced.
	// Use Condition.Status=True to indicate the sync happened successfully,
	// and Condition.Status=False to indicate an error has been encountered.
	Synced ConditionType = "Synced"
)

// ConditionType is a valid value for Condition.Type
type ConditionType string

// +kubebuilder:object:root=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// NodeTopologyList contains a list of NodeTopology resources.
type NodeTopologyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	// Items is a list of NodeTopology.
	Items []NodeTopology `json:"items"`
}
