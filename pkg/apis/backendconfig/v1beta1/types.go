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

// +k8s:openapi-gen=true
type BackendConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   BackendConfigSpec   `json:"spec,omitempty"`
	Status BackendConfigStatus `json:"status,omitempty"`
}

// BackendConfigSpec is the spec for a BackendConfig resource
// +k8s:openapi-gen=true
type BackendConfigSpec struct {
	Iap                  *IAPConfig                  `json:"iap,omitempty"`
	Cdn                  *CDNConfig                  `json:"cdn,omitempty"`
	SecurityPolicy       *SecurityPolicyConfig       `json:"securityPolicy,omitempty"`
	TimeoutSec           *int64                      `json:"timeoutSec,omitempty"`
	ConnectionDraining   *ConnectionDrainingConfig   `json:"connectionDraining,omitempty"`
	SessionAffinity      *SessionAffinityConfig      `json:"sessionAffinity,omitempty"`
	CustomRequestHeaders *CustomRequestHeadersConfig `json:"customRequestHeaders,omitempty"`
	HealthCheck          *HealthCheckConfig          `json:"healthCheck,omitempty"`
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
// +k8s:openapi-gen=true
type IAPConfig struct {
	Enabled                bool                    `json:"enabled"`
	OAuthClientCredentials *OAuthClientCredentials `json:"oauthclientCredentials"`
}

// OAuthClientCredentials contains credentials for a single IAP-enabled backend.
// +k8s:openapi-gen=true
type OAuthClientCredentials struct {
	// The name of a k8s secret which stores the OAuth client id & secret.
	SecretName string `json:"secretName"`
	// Direct reference to OAuth client id.
	ClientID string `json:"clientID,omitempty"`
	// Direct reference to OAuth client secret.
	ClientSecret string `json:"clientSecret,omitempty"`
}

// CDNConfig contains configuration for CDN-enabled backends.
// +k8s:openapi-gen=true
type CDNConfig struct {
	Enabled     bool            `json:"enabled"`
	CachePolicy *CacheKeyPolicy `json:"cachePolicy,omitempty"`
}

// CacheKeyPolicy contains configuration for how requests to a CDN-enabled backend are cached.
// +k8s:openapi-gen=true
type CacheKeyPolicy struct {
	// If true, requests to different hosts will be cached separately.
	IncludeHost bool `json:"includeHost,omitempty"`
	// If true, http and https requests will be cached separately.
	IncludeProtocol bool `json:"includeProtocol,omitempty"`
	// If true, query string parameters are included in the cache key
	// according to QueryStringBlacklist and QueryStringWhitelist.
	// If neither is set, the entire query string is included and if false
	// the entire query string is excluded.
	IncludeQueryString bool `json:"includeQueryString,omitempty"`
	// Names of query strint parameters to exclude from cache keys. All other
	// parameters are included. Either specify QueryStringBlacklist or
	// QueryStringWhitelist, but not both.
	QueryStringBlacklist []string `json:"queryStringBlacklist,omitempty"`
	// Names of query string parameters to include in cache keys. All other
	// parameters are excluded. Either specify QueryStringBlacklist or
	// QueryStringWhitelist, but not both.
	QueryStringWhitelist []string `json:"queryStringWhitelist,omitempty"`
}

// SecurityPolicyConfig contains configuration for CloudArmor-enabled backends.
// If not specified, the controller will not reconcile the security policy
// configuration. In other words, users can make changes in GCE without the
// controller overwriting them.
// +k8s:openapi-gen=true
type SecurityPolicyConfig struct {
	// Name of the security policy that should be associated. If set to empty, the
	// existing security policy on the backend will be removed.
	Name string `json:"name"`
}

// ConnectionDrainingConfig contains configuration for connection draining.
// For now the draining timeout. May manage more settings in the future.
// +k8s:openapi-gen=true
type ConnectionDrainingConfig struct {
	// Draining timeout in seconds.
	DrainingTimeoutSec int64 `json:"drainingTimeoutSec,omitempty"`
}

// SessionAffinityConfig contains configuration for stickyness parameters.
// +k8s:openapi-gen=true
type SessionAffinityConfig struct {
	AffinityType         string `json:"affinityType,omitempty"`
	AffinityCookieTtlSec *int64 `json:"affinityCookieTtlSec,omitempty"`
}

// CustomRequestHeadersConfig contains configuration for custom request headers
// +k8s:openapi-gen=true
type CustomRequestHeadersConfig struct {
	Headers []string `json:"headers,omitempty"`
}

// HealthCheckConfig contains configuration for the health check.
// +k8s:openapi-gen=true
type HealthCheckConfig struct {
	// CheckIntervalSec is a health check parameter. See
	// https://cloud.google.com/compute/docs/reference/rest/v1/healthChecks.
	CheckIntervalSec *int64 `json:"checkIntervalSec,omitempty"`
	// TimeoutSec is a health check parameter. See
	// https://cloud.google.com/compute/docs/reference/rest/v1/healthChecks.
	TimeoutSec *int64 `json:"timeoutSec,omitempty"`
	// HealthyThreshold is a health check parameter. See
	// https://cloud.google.com/compute/docs/reference/rest/v1/healthChecks.
	HealthyThreshold *int64 `json:"healthyThreshold,omitempty"`
	// UnhealthyThreshold is a health check parameter. See
	// https://cloud.google.com/compute/docs/reference/rest/v1/healthChecks.
	UnhealthyThreshold *int64 `json:"unhealthyThreshold,omitempty"`
	// Type is a health check parameter. See
	// https://cloud.google.com/compute/docs/reference/rest/v1/healthChecks.
	Type *string `json:"type,omitempty"`
	Port *int64  `json:"port,omitempty"`
	// RequestPath is a health check parameter. See
	// https://cloud.google.com/compute/docs/reference/rest/v1/healthChecks.
	RequestPath *string `json:"requestPath,omitempty"`
}
