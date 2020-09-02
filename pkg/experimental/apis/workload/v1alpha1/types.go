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

package v1alpha1

import (
	api "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Workload represents an external workload such as VM

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
//
// +k8s:openapi-gen=true
// +kubebuilder:subresource:status
type Workload struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   WorkloadSpec   `json:"spec,omitempty"`
	Status WorkloadStatus `json:"status,omitempty"`
}

// WorkloadSpec is the spec for a Workload resource
// +k8s:openapi-gen=true
type WorkloadSpec struct {
	// EnableHeartbeat indicates whether Heartbeat condition is enabled on this ExternalWorkload.
	EnableHeartbeat bool `json:"enableHeartbeat"`
	// EnablePing indicates whether Ping condition is enabled on this ExternalWorkload.
	EnablePing bool `json:"enablePing"`
	// Hostname is the hostname of this workload.
	Hostname *string `json:"hostname,omitempty"`
	// Addresses specifies the addresses that can be used to access the workload from the cluster.
	// +listType=atomic
	Addresses []ExternalWorkloadAddress `json:"addresses"`
}

// ExternalWorkloadAddress represents an address used by an ExternalWorkload
// +k8s:openapi-gen=true
type ExternalWorkloadAddress struct {
	// Address is the address of the workload exposed to the cluster.
	Address string `json:"address"`
	// AddressType specifies the address type of the external workload.
	AddressType AddressType `json:"addressType"`
}

// AddressType represents the type of an external workload address.
// +k8s:openapi-gen=true
type AddressType string

const (
	AddressTypeIPv4 = AddressType(api.IPv4Protocol)
	AddressTypeIPv6 = AddressType(api.IPv6Protocol)
)

// WorkloadStatus is the status for a Workload resource
// +k8s:openapi-gen=true
type WorkloadStatus struct {
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []Condition `json:"conditions"`
}

// The following constants define the types of conditions of an external workload
const (
	// WorkloadConditionHeartbeat is the heartbeat from the external workload.
	// Heartbeat provides a simple liveness check.
	WorkloadConditionHeartbeat = "externalworkloads.discovery.k8s.io/Heartbeat"
	// WorkloadConditionPing is the ping to the workload from the cluster.
	// Ping provides a simple reachability check.
	WorkloadConditionPing = "externalworkloads.discovery.k8s.io/Ping"
	// WorkloadConditionReady indicates the workload is able to handle requests.
	WorkloadConditionReady = "externalworkloads.discovery.k8s.io/Ready"
)

// +k8s:openapi-gen=true
type Condition struct {
	// Type of condition in CamelCase or in foo.example.com/CamelCase.
	// Many .condition.type values are consistent across resources like Available, but because arbitrary conditions can be
	// useful (see .node.status.conditions), the ability to deconflict is important.
	// +required
	Type string `json:"type" protobuf:"bytes,1,opt,name=type"`
	// Status of the condition, one of True, False, Unknown.
	// +required
	Status ConditionStatus `json:"status" protobuf:"bytes,2,opt,name=status"`
	// If set, this represents the .metadata.generation that the condition was set based upon.
	// For instance, if .metadata.generation is currently 12, but the .status.condition[x].observedGeneration is 9, the condition is out of date
	// with respect to the current state of the instance.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty" protobuf:"varint,3,opt,name=observedGeneration"`
	// Last time the condition transitioned from one status to another.
	// This should be when the underlying condition changed.  If that is not known, then using the time when the API field changed is acceptable.
	// +required
	LastTransitionTime metav1.Time `json:"lastTransitionTime" protobuf:"bytes,4,opt,name=lastTransitionTime"`
	// The reason for the condition's last transition in CamelCase.
	// The specific API may choose whether or not this field is considered a guaranteed API.
	// This field may not be empty.
	// +required
	Reason string `json:"reason" protobuf:"bytes,5,opt,name=reason"`
	// A human readable message indicating details about the transition.
	// This field may be empty.
	// +required
	Message string `json:"message" protobuf:"bytes,6,opt,name=message"`
}

// +k8s:openapi-gen=true
type ConditionStatus string

const (
	ConditionStatusTrue    = ConditionStatus("True")
	ConditionStatusFalse   = ConditionStatus("False")
	ConditionStatusUnknown = ConditionStatus("Unknown")
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// WorkloadList is a list of Workload resources
type WorkloadList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []Workload `json:"items"`
}
