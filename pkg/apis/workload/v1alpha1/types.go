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
	InstanceName string `json:"instanceName"`
	HostName     string `json:"hostName"`
	Locality     string `json:"locality"`
	IP           string `json:"ip"`
}

// WorkloadStatus is the status for a Workload resource
// +k8s:openapi-gen=true
type WorkloadStatus struct {
	// Last time the workload updated its status.
	// +optional
	Heartbeat *metav1.Time `json:"heartbeat,omitempty"`

	// Last time the controller successfully pinged the workload.
	// +optional
	Ping *metav1.Time `json:"ping,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// WorkloadList is a list of Workload resources
type WorkloadList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []Workload `json:"items"`
}
