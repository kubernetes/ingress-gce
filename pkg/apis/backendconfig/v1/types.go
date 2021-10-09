/*
Copyright 2019 The Kubernetes Authors.

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
	// Logging specifies the configuration for access logs.
	Logging *LogConfig `json:"logging,omitempty"`
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
	Enabled                     bool                          `json:"enabled"`
	BypassCacheOnRequestHeaders []*BypassCacheOnRequestHeader `json:"bypassCacheOnRequestHeaders,omitempty"`
	CachePolicy                 *CacheKeyPolicy               `json:"cachePolicy,omitempty"`
	CacheMode                   *string                       `json:"cacheMode,omitempty"`
	ClientTtl                   *int64                        `json:"clientTtl,omitempty"`
	DefaultTtl                  *int64                        `json:"defaultTtl,omitempty"`
	MaxTtl                      *int64                        `json:"maxTtl,omitempty"`
	NegativeCaching             *bool                         `json:"negativeCaching,omitempty"`
	NegativeCachingPolicy       []*NegativeCachingPolicy      `json:"negativeCachingPolicy,omitempty"`
	RequestCoalescing           *bool                         `json:"requestCoalescing,omitempty"`
	ServeWhileStale             *int64                        `json:"serveWhileStale,omitempty"`
	SignedUrlCacheMaxAgeSec     *int64                        `json:"signedUrlCacheMaxAgeSec,omitempty"`
	SignedUrlKeys               []*SignedUrlKey               `json:"signedUrlKeys,omitempty"`
}

// BypassCacheOnRequestHeader contains configuration for how requests containing specific request
// headers bypass the cache, even if the content was previously cached.
// +k8s:openapi-gen=true
type BypassCacheOnRequestHeader struct {
	// The header field name to match on when bypassing cache. Values are
	// case-insensitive.
	HeaderName string `json:"headerName,omitempty"`
}

// NegativeCachingPolicy contains configuration for how negative caching is applied.
// +k8s:openapi-gen=true
type NegativeCachingPolicy struct {
	// The HTTP status code to define a TTL against. Only HTTP status codes
	// 300, 301, 308, 404, 405, 410, 421, 451 and 501 are can be specified
	// as values, and you cannot specify a status code more than once.
	Code int64 `json:"code,omitempty"`
	// The TTL (in seconds) for which to cache responses with the
	// corresponding status code. The maximum allowed value is 1800s (30
	// minutes), noting that infrequently accessed objects may be evicted
	// from the cache before the defined TTL.
	Ttl int64 `json:"ttl,omitempty"`
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

// SignedUrlKey represents a customer-supplied Signing Key used by
// Cloud CDN Signed URLs
// +k8s:openapi-gen=true
type SignedUrlKey struct {
	// KeyName: Name of the key. The name must be 1-63 characters long, and
	// comply with RFC1035. Specifically, the name must be 1-63 characters
	// long and match the regular expression `[a-z]([-a-z0-9]*[a-z0-9])?`
	// which means the first character must be a lowercase letter, and all
	// following characters must be a dash, lowercase letter, or digit,
	// except the last character, which cannot be a dash.
	KeyName string `json:"keyName,omitempty"`

	// KeyValue: 128-bit key value used for signing the URL. The key value
	// must be a valid RFC 4648 Section 5 base64url encoded string.
	KeyValue string `json:"keyValue,omitempty"`

	// The name of a k8s secret which stores the 128-bit key value
	// used for signing the URL. The key value must be a valid RFC 4648 Section 5
	// base64url encoded string
	SecretName string `json:"secretName,omitempty"`
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
	// Port is a health check parameter. See
	// https://cloud.google.com/compute/docs/reference/rest/v1/healthChecks.
	// If Port is used, the controller updates portSpecification as well
	Port *int64 `json:"port,omitempty"`
	// RequestPath is a health check parameter. See
	// https://cloud.google.com/compute/docs/reference/rest/v1/healthChecks.
	RequestPath *string `json:"requestPath,omitempty"`
}

// LogConfig contains configuration for logging.
// +k8s:openapi-gen=true
type LogConfig struct {
	// This field denotes whether to enable logging for the load balancer
	// traffic served by this backend service.
	Enable bool `json:"enable,omitempty"`
	// This field can only be specified if logging is enabled for this
	// backend service. The value of the field must be in [0, 1]. This
	// configures the sampling rate of requests to the load balancer where
	// 1.0 means all logged requests are reported and 0.0 means no logged
	// requests are reported. The default value is 1.0.
	SampleRate *float64 `json:"sampleRate,omitempty"`
}
