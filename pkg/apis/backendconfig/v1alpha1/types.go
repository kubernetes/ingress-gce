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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +genclient:noStatus
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// BackendConfig is a specification for a BackendConfig resource
type BackendConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   BackendConfigSpec   `json:"spec"`
	Status BackendConfigStatus `json:"status"`
}

// BackendConfigSpec is the spec for a BackendConfig resource
type BackendConfigSpec struct {
}

// BackendConfigStatus is the status for a BackendConfig resource
type BackendConfigStatus struct {
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// BackendConfigList is a list of BackendConfig resources
type BackendConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []BackendConfig `json:"items"`
}

// IAPConfig contains configuration for IAP-enabled backends.
type IAPConfig struct {
	Enabled           bool
	ClientCredentials *OAuthClientCredentials
}

// OAuthClientCredentials contains credentials for a single IAP-enabled backend.
type OAuthClientCredentials struct {
	// The name of a k8s secret which stores the OAuth client id & secret.
	Secret string
}

// CDNConfig contains configuration for CDN-enabled backends.
type CDNConfig struct {
	Enabled   bool
	CDNPolicy *CacheKeyPolicy
}

// CacheKeyPolicy contains configuration for how requests to a CDN-enabled backend are cached.
type CacheKeyPolicy struct {
	// If true, requests to different hosts will be cached separately.
	IncludeHost bool
	// If true, http and https requests will be cached separately.
	IncludeProtocol bool
	// If true, query string parameters are included in the cache key
	// according to QueryStringBlacklist and QueryStringWhitelist.
	// If neither is set, the entire query string is included and if false
	// the entire query string is excluded.
	IncludeQueryString bool
	// Names of query strint parameters to exclude from cache keys. All other
	// parameters are included. Either specify QueryStringBlacklist or
	// QueryStringWhitelist, but not both.
	QueryStringBlacklist []string
	// Names of query string parameters to include in cache keys. All other
	// parameters are excluded. Either specify QueryStringBlacklist or
	// QueryStringWhitelist, but not both.
	QueryStringWhitelist []string
}
