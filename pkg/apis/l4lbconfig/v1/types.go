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

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// L4LBConfig is the Schema for the l4lbconfigs API
// +genclient
// +genclient:noStatus
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +k8s:openapi-gen=true
type L4LBConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   L4LBConfigSpec   `json:"spec,omitempty"`
	Status L4LBConfigStatus `json:"status,omitempty"`
}

// L4LBConfigSpec defines the desired state of L4LBConfig
// +k8s:openapi-gen=true
type L4LBConfigSpec struct {
	// Logging defines the telemetry and flow logging configuration for the L4 Load Balancer.
	// +optional
	Logging *LoggingConfig `json:"logging,omitempty"`
}

// L4LBConfigStatus defines the observed state of L4LBConfig
// +k8s:openapi-gen=true
type L4LBConfigStatus struct {
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// L4LBConfigList contains a list of L4LBConfig
type L4LBConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []L4LBConfig `json:"items"`
}

// LoggingConfig defines the parameters for LB logging.
// +k8s:openapi-gen=true
type LoggingConfig struct {
	// Enabled allows toggling of Cloud Logging.
	// +optional
	Enabled bool `json:"enabled"`

	// SampleRate is the percentage of flows to log, from 0 to 1000000.
	// 1000000 means 100% of packets are logged.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=1000000
	// +optional
	SampleRate *int32 `json:"sampleRate,omitempty"`

	// OptionalMode defines which metadata fields to include in the logs.
	// Options: INCLUDE_ALL_OPTIONAL, EXCLUDE_ALL_OPTIONAL, CUSTOM.
	// +kubebuilder:validation:Enum=INCLUDE_ALL_OPTIONAL;EXCLUDE_ALL_OPTIONAL;CUSTOM
	// +optional
	OptionalMode string `json:"optionalMode,omitempty"`

	// OptionalFields is a list of additional metadata fields to include.
	// Only valid when optionalMode is set to 'CUSTOM'.
	// +listType=set
	// +optional
	OptionalFields []string `json:"optionalFields,omitempty"`
}
