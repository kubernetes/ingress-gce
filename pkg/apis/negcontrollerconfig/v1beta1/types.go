/*
Copyright 2023 The Kubernetes Authors.

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

// NEGControllerConfig represents the configuration of GKE NEG Controller.

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
//
// +k8s:openapi-gen=true
type NEGControllerConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec NEGControllerConfigSpec `json:"spec,omitempty"`
}

// NEGControllerConfigSpec is the spec for NEGControllerConfig resource.
// +k8s:openapi-gen=true
type NEGControllerConfigSpec struct {
	PodLabelPropagation *PodLabelPropagationConfig `json:"podLabelPropagation,omitempty"`
}

// PodLabelPropagationConfig contains a list of configurations for labels to be propagated to GCE network endpoints.
// +k8s:openapi-gen=true
type PodLabelPropagationConfig struct {
	Labels []Label `json:"labels"`
}

// Label contains configuration for a label to be propagated to GCE network endpoints.
// +k8s:openapi-gen=true
type Label struct {
	Key               string `json:"key"`
	ShortKey          string `json:"shortKey"`
	MaxLabelSizeBytes int    `json:"maxValueSizeBytes"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// NEGControllerConfigList is a list of NEGControllerConfig resources
type NEGControllerConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []NEGControllerConfig `json:"items"`
}
