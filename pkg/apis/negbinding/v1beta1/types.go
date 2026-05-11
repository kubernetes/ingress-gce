/*
Copyright 2026 The Kubernetes Authors.

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
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
//
// +k8s:openapi-gen=true
type NetworkEndpointGroupBinding struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NetworkEndpointGroupBindingSpec   `json:"spec,omitempty"`
	Status NetworkEndpointGroupBindingStatus `json:"status,omitempty"`
}

// NetworkEndpointGroupBindingSpec is the spec for a NetworkEndpointGroupBinding resource
// +k8s:openapi-gen=true
type NetworkEndpointGroupBindingSpec struct {
	// BackendRef references backend to which NEGs should be attached
	// +k8s:validation:cel[0]:rule="oldSelf == self"
	// +k8s:validation:cel[0]:message="BackendRef can't be changed"
	// +k8s:validation:cel[1]:rule="has(self.kind)"
	// +k8s:validation:cel[1]:message="Field 'kind' must be set"
	// +k8s:validation:cel[2]:rule="has(self.name)"
	// +k8s:validation:cel[2]:message="Field 'name' must be set"
	// +k8s:validation:cel[3]:rule="has(self.port)"
	// +k8s:validation:cel[3]:message="Field 'port' must be set"
	BackendRef *BackendRefConfig `json:"backendRef"`
	// NetworkEndpointGroups references NEGs which should be managed by controller
	// +listType=map
	// +listMapKey=name
	// +k8s:validation:maxItems=50
	// +k8s:validation:cel[0]:rule="self.all(i, self.filter(j, j.subnet == i.subnet).size() <= 1)"
	// +k8s:validation:cel[0]:message="NegBinding can contain only one NEG name per subnet"
	NetworkEndpointGroups []SpecNegRef `json:"networkEndpointGroups"`
}

// BackendRefConfig specifies backendRef for the NetworkEndpointGroupBinding resource
// +k8s:openapi-gen=true
type BackendRefConfig struct {
	Group string         `json:"group"`
	Kind  BackendRefKind `json:"kind,omitempty"`
	// +k8s:validation:minLength=1
	Name string `json:"name,omitempty"`
	// +k8s:validation:minimum=1
	// +k8s:validation:maximum=65535
	Port int32 `json:"port,omitempty"`
}

// +k8s:openapi-gen=true
// +enum
type BackendRefKind string

const (
	ServiceKind = BackendRefKind("Service")
)

// +k8s:openapi-gen=true
// SpecNegRef references NetworkEndpointGroups which should be managed by controller
type SpecNegRef struct {
	// Name of NetworkEndpointGroups
	Name string `json:"name"`
	// Subnet of NetworkEndpointGroups
	// +k8s:validation:maxLength=63
	Subnet string `json:"subnet"`
	// Zones of NetworkEndpointGroups to manage
	// +listType=set
	// +k8s:validation:maxItems=10
	Zones []string `json:"zones"`
}

// +k8s:openapi-gen=true
// NetworkEndpointGroupBindingStatus is the status for a NetworkEndpointGroupBinding resource
type NetworkEndpointGroupBindingStatus struct {
	// Last time the NEG syncer synced endpoints of associated NEGs.
	// +optional
	LastSyncTime metav1.Time `json:"lastSyncTime,omitempty"`
	// Conditions describe the current conditions of the NetworkEndpointGroupBinding.
	// +listType=map
	// +listMapKey=type
	Conditions []Condition `json:"conditions,omitempty"`
	// Negs contains currently managed NEGs referenced by NetworkEndpointGroupsBinding
	// +listType=map
	// +listMapKey=resourceURL
	NetworkEndpointGroups []StatusNegRef `json:"networkEndpointGroups,omitempty"`
}

// +k8s:openapi-gen=true
type Condition metav1.Condition

// +k8s:openapi-gen=true
// StatusNegRef references NetworkEndpointGroup which is actively managed by controller
type StatusNegRef struct {
	// ResourceURL is the GCE Server-defined fully-qualified URL for the GCE NEG resource
	ResourceURL string `json:"resourceURL"`

	// URL of the subnetwork to which all network endpoints in the NEG belong.
	SubnetURL string `json:"subnetURL"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// NetworkEndpointGroupBindingList is a list of NetworkEndpointGroupBinding resources
type NetworkEndpointGroupBindingList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []NetworkEndpointGroupBinding `json:"items"`
}
