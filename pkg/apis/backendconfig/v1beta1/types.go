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
	TrafficManagement    *TrafficManagementConfig    `json:"trafficManagement,omitempty"`
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
type SecurityPolicyConfig struct {
	// Name of the security policy that should be associated.
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

// TrafficManagementConfig holds all of the traffic director specific features
// +k8s:openapi-gen=true
type TrafficManagementConfig struct {
	LocalityLbPolicy string                              `json:"localityLbPolicy,omitempty"`
	ConsistentHash   *ConsistentHashLoadBalancerSettings `json:"ConsistentHash,omitempty"`
	OutlierDetection *OutlierDetection                   `json:"outlierDetection,omitempty,string"`
	CircuitBreakers  *CircuitBreakers                    `json:"circuitBreakers,omitempty,string"`
}

// Duration contains configuration for time intervals
// +k8s:openapi-gen=true
type Duration struct {
	// Span of time that's a fraction of a second at nanosecond resolution.
	// Durations less than one second are represented with a 0 `seconds`
	// field and a positive `nanos` field. Must be from 0 to 999,999,999
	// inclusive.
	Nanos int64 `json:"nanos,omitempty"`
	// Span of time at a resolution of a second. Must be from 0 to
	// 315,576,000,000 inclusive. Note: these bounds are computed from: 60
	// sec/min * 60 min/hr * 24 hr/day * 365.25 days/year * 10000 years
	Seconds int64 `json:"seconds,omitempty,string"`
}

// ConsistentHashLoadBalancerSettings contains configuration for consistent hash features on
// TrafficManagement based load balancers
// +k8s:openapi-gen=true
type ConsistentHashLoadBalancerSettings struct {
	// Hash is based on HTTP Cookie. This field describes a HTTP cookie that
	// will be used as the hash key for the consistent hash load balancer.
	// If the cookie is not present, it will be generated. This field is
	// applicable if the sessionAffinity is set to HTTP_COOKIE.
	HttpCookie *ConsistentHashLoadBalancerSettingsHttpCookie `json:"httpCookie,omitempty"`
	// The hash based on the value of the specified header field. This field
	// is applicable if the sessionAffinity is set to HEADER_FIELD.
	HttpHeaderName string `json:"httpHeaderName,omitempty"`
	// The minimum number of virtual nodes to use for the hash ring.
	// Defaults to 1024. Larger ring sizes result in more granular load
	// distributions. If the number of hosts in the load balancing pool is
	// larger than the ring size, each host will be assigned a single
	// virtual node.
	MinimumRingSize int64 `json:"minimumRingSize,omitempty,string"`
}

// ConsistentHashLoadBalancerSettingsHttpCookie contains configuration for the HttpCookie for the
// consistent hash feature
// +k8s:openapi-gen=true
type ConsistentHashLoadBalancerSettingsHttpCookie struct {
	// Name of the cookie.
	Name string `json:"name,omitempty"`
	// Path to set for the cookie.
	Path string `json:"path,omitempty"`
	// Lifetime of the cookie.
	Ttl *Duration `json:"ttl,omitempty"`
}

// OutlierDetection contains configuration for outlier detection on TrafficManagement based load balancers
// +k8s:openapi-gen=true
type OutlierDetection struct {
	// The base time that a host is ejected for. The real time is equal to
	// the base time multiplied by the number of times the host has been
	// ejected. Defaults to 30000ms or 30s.
	BaseEjectionTime *Duration `json:"baseEjectionTime,omitempty"`
	// Number of errors before a host is ejected from the connection pool.
	// When the backend host is accessed over HTTP, a 5xx return code
	// qualifies as an error. Defaults to 5.
	ConsecutiveErrors int64 `json:"consecutiveErrors,omitempty"`
	// The number of consecutive gateway failures (502, 503, 504 status or
	// connection errors that are mapped to one of those status codes)
	// before a consecutive gateway failure ejection occurs. Defaults to 5.
	ConsecutiveGatewayFailure int64 `json:"consecutiveGatewayFailure,omitempty"`
	// The percentage chance that a host will be actually ejected when an
	// outlier status is detected through consecutive 5xx. This setting can
	// be used to disable ejection or to ramp it up slowly. Defaults to 100.
	EnforcingConsecutiveErrors int64 `json:"enforcingConsecutiveErrors,omitempty"`
	// The percentage chance that a host will be actually ejected when an
	// outlier status is detected through consecutive gateway failures. This
	// setting can be used to disable ejection or to ramp it up slowly.
	// Defaults to 0.
	EnforcingConsecutiveGatewayFailure int64 `json:"enforcingConsecutiveGatewayFailure,omitempty"`
	// The percentage chance that a host will be actually ejected when an
	// outlier status is detected through success rate statistics. This
	// setting can be used to disable ejection or to ramp it up slowly.
	// Defaults to 100.
	EnforcingSuccessRate int64 `json:"enforcingSuccessRate,omitempty"`
	// Time interval between ejection sweep analysis. This can result in
	// both new ejections as well as hosts being returned to service.
	// Defaults to 10 seconds.
	Interval *Duration `json:"interval,omitempty"`
	// Maximum percentage of hosts in the load balancing pool for the
	// backend service that can be ejected. Defaults to 10%.
	MaxEjectionPercent int64 `json:"maxEjectionPercent,omitempty"`
	// The number of hosts in a cluster that must have enough request volume
	// to detect success rate outliers. If the number of hosts is less than
	// this setting, outlier detection via success rate statistics is not
	// performed for any host in the cluster. Defaults to 5.
	SuccessRateMinimumHosts int64 `json:"successRateMinimumHosts,omitempty"`
	// The minimum number of total requests that must be collected in one
	// interval (as defined by the interval duration above) to include this
	// host in success rate based outlier detection. If the volume is lower
	// than this setting, outlier detection via success rate statistics is
	// not performed for that host. Defaults to 100.
	SuccessRateRequestVolume int64 `json:"successRateRequestVolume,omitempty"`
	// This factor is used to determine the ejection threshold for success
	// rate outlier ejection. The ejection threshold is the difference
	// between the mean success rate, and the product of this factor and the
	// standard deviation of the mean success rate: mean - (stdev *
	// success_rate_stdev_factor). This factor is divided by a thousand to
	// get a double. That is, if the desired factor is 1.9, the runtime
	// value should be 1900. Defaults to 1900.
	SuccessRateStdevFactor int64 `json:"successRateStdevFactor,omitempty"`
}

// CircuitBreakers contains configuration for the circuit breakers feature on TrafficManagement based
// load balancers
// +k8s:openapi-gen=true
type CircuitBreakers struct {
	// The timeout for new network connections to hosts.
	ConnectTimeout *Duration `json:"connectTimeout,omitempty"`
	// The maximum number of connections to the backend cluster. If not
	// specified, the default is 1024.
	MaxConnections int64 `json:"maxConnections,omitempty"`
	// The maximum number of pending requests allowed to the backend
	// cluster. If not specified, the default is 1024.
	MaxPendingRequests int64 `json:"maxPendingRequests,omitempty"`
	// The maximum number of parallel requests that allowed to the backend
	// cluster. If not specified, the default is 1024.
	MaxRequests int64 `json:"maxRequests,omitempty"`
	// Maximum requests for a single backend connection. This parameter is
	// respected by both the HTTP/1.1 and HTTP/2 implementations. If not
	// specified, there is no limit. Setting this parameter to 1 will
	// effectively disable keep alive.
	MaxRequestsPerConnection int64 `json:"maxRequestsPerConnection,omitempty"`
	// The maximum number of parallel retries allowed to the backend
	// cluster. If not specified, the default is 3.
	MaxRetries int64 `json:"maxRetries,omitempty"`
}
