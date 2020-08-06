/*
Copyright 2018 The Kubernetes Authors.

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
// +genclient:noStatus
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
//
// +k8s:openapi-gen=true
type FrontendConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              FrontendConfigSpec   `json:"spec,omitempty"`
	Status            FrontendConfigStatus `json:"status,omitempty"`
}

// FrontendConfigSpec is the spec for a FrontendConfig resource
// +k8s:openapi-gen=true
type FrontendConfigSpec struct {
	SslPolicy       *string              `json:"sslPolicy,omitempty"`
	RedirectToHttps *HttpsRedirectConfig `json:"redirectToHttps,omitempty"`
}

// HttpsRedirectConfig representing the configuration of Https redirects
// +k8s:openapi-gen=true
type HttpsRedirectConfig struct {
	Enabled bool `json:"enabled"`
	// String representing the HTTP response code
	// Options are MOVED_PERMANENTLY_DEFAULT, FOUND, TEMPORARY_REDIRECT, or PERMANENT_REDIRECT
	ResponseCodeName string `json:"responseCodeName,omitempty"`
}

// FrontendConfigStatus is the status for a FrontendConfig resource
type FrontendConfigStatus struct{}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// FrontendConfigList is a list of FrontendConfig resources
type FrontendConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []FrontendConfig `json:"items"`
}
