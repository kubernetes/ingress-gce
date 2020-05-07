/*
Copyright 2020 The Kubernetes Authors.

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

package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	core "k8s.io/kubernetes/pkg/apis/core"
)

// ServiceNetworkEndpointGroup represents a group of Network Endpoint Groups associated with a service.

// +genclient
// +genclient:noStatus
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
//
// +k8s:openapi-gen=true
type ServiceNetworkEndpointGroup struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ServiceNetworkEndpointGroupSpec   `json:"spec,omitempty"`
	Status ServiceNetworkEndpointGroupStatus `json:"status,omitempty"`
}

// ServiceNetworkEndpointGroupSpec is the spec for a ServiceNetworkEndpointGroup resource
// +k8s:openapi-gen=true
type ServiceNetworkEndpointGroupSpec struct{}

// ServiceNetworkEndpointGroupStatus is the status for a ServiceNetworkEndpointGroup resource
type ServiceNetworkEndpointGroupStatus struct {
	NetworkEndpointGroups []NegObjectReference `json:"networkEndpointGroups,omitempty"`

	// Last time the NEG syncer syncs associated NEGs.
	// +optional
	Conditions []Condition `json:"conditions,omitempty"`

	// Last time the NEG syncer syncs associated NEGs.
	// +optional
	LastSyncTime metav1.Time `json:"lastTransitionTime,omitempty"`
}

// NegObjectReference is the object reference to the NEG resource in GCE
type NegObjectReference struct {
	// The unique identifier for the NEG resource in GCE API.
	Id uint64 `json:"id,omitempty,string"`

	// SelfLink is the GCE Server-defined fully-qualified URL for the GCE NEG resource
	SelfLink string `json:"selfLink,omitempty"`

	// NetworkEndpointType: Type of network endpoints in this network
	// endpoint group.
	NetworkEndpointType NetworkEndpointType `json:"networkEndpointType,omitempty"`
}

type NetworkEndpointType string

const (
	VmIpPortEndpointType      = NetworkEndpointType("GCE_VM_IP_PORT")
	VmIpEndpointType          = NetworkEndpointType("GCE_VM_IP")
	NonGCPPrivateEndpointType = NetworkEndpointType("NON_GCP_PRIVATE_IP_PORT")
)

// TODO: Replace Condition with standard Condition
// NegCondition contains details for the current condition of this NEG.
type Condition struct {
	// Type is the type of the condition.
	// +required
	Type string `json:"type" protobuf:"bytes,1,opt,name=type"`
	// Status of the condition, one of True, False, Unknown.
	// +required
	Status core.ConditionStatus `json:"status" protobuf:"bytes,2,opt,name=status"`
	// ObservedGeneration will not be set for ServiceNetworkEndpointGroup as the spec is empty.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty" protobuf:"varint,3,opt,name=observedGeneration"`
	// Last time the condition transitioned from one status to another.
	// +required
	LastTransitionTime metav1.Time `json:"lastTransitionTime" protobuf:"bytes,4,opt,name=lastTransitionTime"`
	// The reason for the condition's last transition
	// +required
	Reason string `json:"reason" protobuf:"bytes,5,opt,name=reason"`
	// A human readable message indicating details about the transition.
	// This field may be empty.
	// +required
	Message string `json:"message" protobuf:"bytes,6,opt,name=message"`
}

// These are valid conditions of NEG.
const (
	// Initialized means all NEGs have been created and initialized.
	Initialized = "Initialized"
	// Synced means all NEGs are being synced.
	// The LastSyncTime represents the time when the last sync took place.
	Synced = "Synced"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ServiceNetworkEndpointGroupList is a list of ServiceNetworkEndpointGroup resources
type ServiceNetworkEndpointGroupList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []ServiceNetworkEndpointGroup `json:"items"`
}
