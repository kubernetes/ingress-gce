/*
Copyright 2025 The Kubernetes Authors.

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

// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:storageversion
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ServiceLoadBalancerStatus holds the mapping between a GKE Service and the GCE resources it configures.
type ServiceLoadBalancerStatus struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec defines the desired state of ServiceLoadBalancerStatus.
	Spec ServiceLoadBalancerStatusSpec `json:"spec,omitempty"`
	// Status defines the observed state of ServiceLoadBalancerStatus.
	Status ServiceLoadBalancerStatusStatus `json:"status,omitempty"`
}

// ServiceLoadBalancerStatusSpec defines the desired state of ServiceLoadBalancerStatus.
type ServiceLoadBalancerStatusSpec struct {
	// ServiceRef is a reference to the Kubernetes Service that this
	// ServiceLoadBalancerStatus is associated with.
	// +required
	ServiceRef ServiceReference `json:"serviceRef"`

	// GceResources is a list of URLs for GCE resources provisioned and managed
	// by the GKE controller for the referenced Service.
	// +required
	// +listType=set
	GceResources []string `json:"gceResources"`
}

// ServiceReference contains enough information to let you locate the
// referenced object.
type ServiceReference struct {
	// API version of the referent.
	// +optional
	APIVersion string `json:"apiVersion,omitempty"`
	// Kind of the referent.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds
	// +optional
	Kind string `json:"kind,omitempty"`
	// Name of the referent.
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#names
	// +required
	Name string `json:"name"`
}

// ServiceLoadBalancerStatusStatus defines the observed state of ServiceLoadBalancerStatus.
type ServiceLoadBalancerStatusStatus struct {
	// This field is intentionally empty.
}

// +kubebuilder:object:root=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ServiceLoadBalancerStatusList contains a list of ServiceLoadBalancerStatus.
type ServiceLoadBalancerStatusList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	// Items is the list of ServiceLoadBalancerStatus.
	Items []ServiceLoadBalancerStatus `json:"items"`
}
