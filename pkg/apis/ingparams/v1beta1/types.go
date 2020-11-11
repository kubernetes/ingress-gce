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
)

// GCPIngressParams contains the parameters for GCP Ingress Classes
// +genclient
// +genclient:noStatus
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +k8s:openapi-gen=true
type GCPIngressParams struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GCPIngressParamsSpec   `json:"spec,omitempty"`
	Status GCPIngressParamsStatus `json:"status,omitempty"`
}

// GCPIngressParamsSpec is the spec for a GCPIngressParams resource
// +k8s:openapi-gen=true
type GCPIngressParamsSpec struct {
	// Internal specifies whether internal or external load balancing is desired.
	// The default is external load balancing, so Internal will default to false.
	// +required
	Internal bool `json:"internal"`
}

// GCPIngressParamsStatus is the status for a GCPIngressParams resource
type GCPIngressParamsStatus struct{}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// GCPIngressParamsList is a list of GCPIngressParams resources
type GCPIngressParamsList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []GCPIngressParams `json:"items"`
}
