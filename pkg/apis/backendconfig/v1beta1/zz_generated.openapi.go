//go:build !ignore_autogenerated
// +build !ignore_autogenerated

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

// Code generated by openapi-gen. DO NOT EDIT.

// This file was autogenerated by openapi-gen. Do not edit it manually!

package v1beta1

import (
	common "k8s.io/kube-openapi/pkg/common"
	spec "k8s.io/kube-openapi/pkg/validation/spec"
)

func GetOpenAPIDefinitions(ref common.ReferenceCallback) map[string]common.OpenAPIDefinition {
	return map[string]common.OpenAPIDefinition{
		"k8s.io/ingress-gce/pkg/apis/backendconfig/v1beta1.BackendConfig":              schema_pkg_apis_backendconfig_v1beta1_BackendConfig(ref),
		"k8s.io/ingress-gce/pkg/apis/backendconfig/v1beta1.BackendConfigSpec":          schema_pkg_apis_backendconfig_v1beta1_BackendConfigSpec(ref),
		"k8s.io/ingress-gce/pkg/apis/backendconfig/v1beta1.CDNConfig":                  schema_pkg_apis_backendconfig_v1beta1_CDNConfig(ref),
		"k8s.io/ingress-gce/pkg/apis/backendconfig/v1beta1.CacheKeyPolicy":             schema_pkg_apis_backendconfig_v1beta1_CacheKeyPolicy(ref),
		"k8s.io/ingress-gce/pkg/apis/backendconfig/v1beta1.ConnectionDrainingConfig":   schema_pkg_apis_backendconfig_v1beta1_ConnectionDrainingConfig(ref),
		"k8s.io/ingress-gce/pkg/apis/backendconfig/v1beta1.CustomRequestHeadersConfig": schema_pkg_apis_backendconfig_v1beta1_CustomRequestHeadersConfig(ref),
		"k8s.io/ingress-gce/pkg/apis/backendconfig/v1beta1.HealthCheckConfig":          schema_pkg_apis_backendconfig_v1beta1_HealthCheckConfig(ref),
		"k8s.io/ingress-gce/pkg/apis/backendconfig/v1beta1.IAPConfig":                  schema_pkg_apis_backendconfig_v1beta1_IAPConfig(ref),
		"k8s.io/ingress-gce/pkg/apis/backendconfig/v1beta1.OAuthClientCredentials":     schema_pkg_apis_backendconfig_v1beta1_OAuthClientCredentials(ref),
		"k8s.io/ingress-gce/pkg/apis/backendconfig/v1beta1.SecurityPolicyConfig":       schema_pkg_apis_backendconfig_v1beta1_SecurityPolicyConfig(ref),
		"k8s.io/ingress-gce/pkg/apis/backendconfig/v1beta1.SessionAffinityConfig":      schema_pkg_apis_backendconfig_v1beta1_SessionAffinityConfig(ref),
	}
}

func schema_pkg_apis_backendconfig_v1beta1_BackendConfig(ref common.ReferenceCallback) common.OpenAPIDefinition {
	return common.OpenAPIDefinition{
		Schema: spec.Schema{
			SchemaProps: spec.SchemaProps{
				Type: []string{"object"},
				Properties: map[string]spec.Schema{
					"kind": {
						SchemaProps: spec.SchemaProps{
							Description: "Kind is a string value representing the REST resource this object represents. Servers may infer this from the endpoint the client submits requests to. Cannot be updated. In CamelCase. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds",
							Type:        []string{"string"},
							Format:      "",
						},
					},
					"apiVersion": {
						SchemaProps: spec.SchemaProps{
							Description: "APIVersion defines the versioned schema of this representation of an object. Servers should convert recognized schemas to the latest internal value, and may reject unrecognized values. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources",
							Type:        []string{"string"},
							Format:      "",
						},
					},
					"metadata": {
						SchemaProps: spec.SchemaProps{
							Default: map[string]interface{}{},
							Ref:     ref("k8s.io/apimachinery/pkg/apis/meta/v1.ObjectMeta"),
						},
					},
					"spec": {
						SchemaProps: spec.SchemaProps{
							Default: map[string]interface{}{},
							Ref:     ref("k8s.io/ingress-gce/pkg/apis/backendconfig/v1beta1.BackendConfigSpec"),
						},
					},
					"status": {
						SchemaProps: spec.SchemaProps{
							Default: map[string]interface{}{},
							Ref:     ref("k8s.io/ingress-gce/pkg/apis/backendconfig/v1beta1.BackendConfigStatus"),
						},
					},
				},
			},
		},
		Dependencies: []string{
			"k8s.io/apimachinery/pkg/apis/meta/v1.ObjectMeta", "k8s.io/ingress-gce/pkg/apis/backendconfig/v1beta1.BackendConfigSpec", "k8s.io/ingress-gce/pkg/apis/backendconfig/v1beta1.BackendConfigStatus"},
	}
}

func schema_pkg_apis_backendconfig_v1beta1_BackendConfigSpec(ref common.ReferenceCallback) common.OpenAPIDefinition {
	return common.OpenAPIDefinition{
		Schema: spec.Schema{
			SchemaProps: spec.SchemaProps{
				Description: "BackendConfigSpec is the spec for a BackendConfig resource",
				Type:        []string{"object"},
				Properties: map[string]spec.Schema{
					"iap": {
						SchemaProps: spec.SchemaProps{
							Ref: ref("k8s.io/ingress-gce/pkg/apis/backendconfig/v1beta1.IAPConfig"),
						},
					},
					"cdn": {
						SchemaProps: spec.SchemaProps{
							Ref: ref("k8s.io/ingress-gce/pkg/apis/backendconfig/v1beta1.CDNConfig"),
						},
					},
					"securityPolicy": {
						SchemaProps: spec.SchemaProps{
							Ref: ref("k8s.io/ingress-gce/pkg/apis/backendconfig/v1beta1.SecurityPolicyConfig"),
						},
					},
					"timeoutSec": {
						SchemaProps: spec.SchemaProps{
							Type:   []string{"integer"},
							Format: "int64",
						},
					},
					"connectionDraining": {
						SchemaProps: spec.SchemaProps{
							Ref: ref("k8s.io/ingress-gce/pkg/apis/backendconfig/v1beta1.ConnectionDrainingConfig"),
						},
					},
					"sessionAffinity": {
						SchemaProps: spec.SchemaProps{
							Ref: ref("k8s.io/ingress-gce/pkg/apis/backendconfig/v1beta1.SessionAffinityConfig"),
						},
					},
					"customRequestHeaders": {
						SchemaProps: spec.SchemaProps{
							Ref: ref("k8s.io/ingress-gce/pkg/apis/backendconfig/v1beta1.CustomRequestHeadersConfig"),
						},
					},
					"healthCheck": {
						SchemaProps: spec.SchemaProps{
							Ref: ref("k8s.io/ingress-gce/pkg/apis/backendconfig/v1beta1.HealthCheckConfig"),
						},
					},
				},
			},
		},
		Dependencies: []string{
			"k8s.io/ingress-gce/pkg/apis/backendconfig/v1beta1.CDNConfig", "k8s.io/ingress-gce/pkg/apis/backendconfig/v1beta1.ConnectionDrainingConfig", "k8s.io/ingress-gce/pkg/apis/backendconfig/v1beta1.CustomRequestHeadersConfig", "k8s.io/ingress-gce/pkg/apis/backendconfig/v1beta1.HealthCheckConfig", "k8s.io/ingress-gce/pkg/apis/backendconfig/v1beta1.IAPConfig", "k8s.io/ingress-gce/pkg/apis/backendconfig/v1beta1.SecurityPolicyConfig", "k8s.io/ingress-gce/pkg/apis/backendconfig/v1beta1.SessionAffinityConfig"},
	}
}

func schema_pkg_apis_backendconfig_v1beta1_CDNConfig(ref common.ReferenceCallback) common.OpenAPIDefinition {
	return common.OpenAPIDefinition{
		Schema: spec.Schema{
			SchemaProps: spec.SchemaProps{
				Description: "CDNConfig contains configuration for CDN-enabled backends.",
				Type:        []string{"object"},
				Properties: map[string]spec.Schema{
					"enabled": {
						SchemaProps: spec.SchemaProps{
							Default: false,
							Type:    []string{"boolean"},
							Format:  "",
						},
					},
					"cachePolicy": {
						SchemaProps: spec.SchemaProps{
							Ref: ref("k8s.io/ingress-gce/pkg/apis/backendconfig/v1beta1.CacheKeyPolicy"),
						},
					},
				},
				Required: []string{"enabled"},
			},
		},
		Dependencies: []string{
			"k8s.io/ingress-gce/pkg/apis/backendconfig/v1beta1.CacheKeyPolicy"},
	}
}

func schema_pkg_apis_backendconfig_v1beta1_CacheKeyPolicy(ref common.ReferenceCallback) common.OpenAPIDefinition {
	return common.OpenAPIDefinition{
		Schema: spec.Schema{
			SchemaProps: spec.SchemaProps{
				Description: "CacheKeyPolicy contains configuration for how requests to a CDN-enabled backend are cached.",
				Type:        []string{"object"},
				Properties: map[string]spec.Schema{
					"includeHost": {
						SchemaProps: spec.SchemaProps{
							Description: "If true, requests to different hosts will be cached separately.",
							Type:        []string{"boolean"},
							Format:      "",
						},
					},
					"includeProtocol": {
						SchemaProps: spec.SchemaProps{
							Description: "If true, http and https requests will be cached separately.",
							Type:        []string{"boolean"},
							Format:      "",
						},
					},
					"includeQueryString": {
						SchemaProps: spec.SchemaProps{
							Description: "If true, query string parameters are included in the cache key according to QueryStringBlacklist and QueryStringWhitelist. If neither is set, the entire query string is included and if false the entire query string is excluded.",
							Type:        []string{"boolean"},
							Format:      "",
						},
					},
					"queryStringBlacklist": {
						SchemaProps: spec.SchemaProps{
							Description: "Names of query strint parameters to exclude from cache keys. All other parameters are included. Either specify QueryStringBlacklist or QueryStringWhitelist, but not both.",
							Type:        []string{"array"},
							Items: &spec.SchemaOrArray{
								Schema: &spec.Schema{
									SchemaProps: spec.SchemaProps{
										Default: "",
										Type:    []string{"string"},
										Format:  "",
									},
								},
							},
						},
					},
					"queryStringWhitelist": {
						SchemaProps: spec.SchemaProps{
							Description: "Names of query string parameters to include in cache keys. All other parameters are excluded. Either specify QueryStringBlacklist or QueryStringWhitelist, but not both.",
							Type:        []string{"array"},
							Items: &spec.SchemaOrArray{
								Schema: &spec.Schema{
									SchemaProps: spec.SchemaProps{
										Default: "",
										Type:    []string{"string"},
										Format:  "",
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func schema_pkg_apis_backendconfig_v1beta1_ConnectionDrainingConfig(ref common.ReferenceCallback) common.OpenAPIDefinition {
	return common.OpenAPIDefinition{
		Schema: spec.Schema{
			SchemaProps: spec.SchemaProps{
				Description: "ConnectionDrainingConfig contains configuration for connection draining. For now the draining timeout. May manage more settings in the future.",
				Type:        []string{"object"},
				Properties: map[string]spec.Schema{
					"drainingTimeoutSec": {
						SchemaProps: spec.SchemaProps{
							Description: "Draining timeout in seconds.",
							Type:        []string{"integer"},
							Format:      "int64",
						},
					},
				},
			},
		},
	}
}

func schema_pkg_apis_backendconfig_v1beta1_CustomRequestHeadersConfig(ref common.ReferenceCallback) common.OpenAPIDefinition {
	return common.OpenAPIDefinition{
		Schema: spec.Schema{
			SchemaProps: spec.SchemaProps{
				Description: "CustomRequestHeadersConfig contains configuration for custom request headers",
				Type:        []string{"object"},
				Properties: map[string]spec.Schema{
					"headers": {
						SchemaProps: spec.SchemaProps{
							Type: []string{"array"},
							Items: &spec.SchemaOrArray{
								Schema: &spec.Schema{
									SchemaProps: spec.SchemaProps{
										Default: "",
										Type:    []string{"string"},
										Format:  "",
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func schema_pkg_apis_backendconfig_v1beta1_HealthCheckConfig(ref common.ReferenceCallback) common.OpenAPIDefinition {
	return common.OpenAPIDefinition{
		Schema: spec.Schema{
			SchemaProps: spec.SchemaProps{
				Description: "HealthCheckConfig contains configuration for the health check.",
				Type:        []string{"object"},
				Properties: map[string]spec.Schema{
					"checkIntervalSec": {
						SchemaProps: spec.SchemaProps{
							Description: "CheckIntervalSec is a health check parameter. See https://cloud.google.com/compute/docs/reference/rest/v1/healthChecks.",
							Type:        []string{"integer"},
							Format:      "int64",
						},
					},
					"timeoutSec": {
						SchemaProps: spec.SchemaProps{
							Description: "TimeoutSec is a health check parameter. See https://cloud.google.com/compute/docs/reference/rest/v1/healthChecks.",
							Type:        []string{"integer"},
							Format:      "int64",
						},
					},
					"healthyThreshold": {
						SchemaProps: spec.SchemaProps{
							Description: "HealthyThreshold is a health check parameter. See https://cloud.google.com/compute/docs/reference/rest/v1/healthChecks.",
							Type:        []string{"integer"},
							Format:      "int64",
						},
					},
					"unhealthyThreshold": {
						SchemaProps: spec.SchemaProps{
							Description: "UnhealthyThreshold is a health check parameter. See https://cloud.google.com/compute/docs/reference/rest/v1/healthChecks.",
							Type:        []string{"integer"},
							Format:      "int64",
						},
					},
					"type": {
						SchemaProps: spec.SchemaProps{
							Description: "Type is a health check parameter. See https://cloud.google.com/compute/docs/reference/rest/v1/healthChecks.",
							Type:        []string{"string"},
							Format:      "",
						},
					},
					"port": {
						SchemaProps: spec.SchemaProps{
							Type:   []string{"integer"},
							Format: "int64",
						},
					},
					"requestPath": {
						SchemaProps: spec.SchemaProps{
							Description: "RequestPath is a health check parameter. See https://cloud.google.com/compute/docs/reference/rest/v1/healthChecks.",
							Type:        []string{"string"},
							Format:      "",
						},
					},
				},
			},
		},
	}
}

func schema_pkg_apis_backendconfig_v1beta1_IAPConfig(ref common.ReferenceCallback) common.OpenAPIDefinition {
	return common.OpenAPIDefinition{
		Schema: spec.Schema{
			SchemaProps: spec.SchemaProps{
				Description: "IAPConfig contains configuration for IAP-enabled backends.",
				Type:        []string{"object"},
				Properties: map[string]spec.Schema{
					"enabled": {
						SchemaProps: spec.SchemaProps{
							Default: false,
							Type:    []string{"boolean"},
							Format:  "",
						},
					},
					"oauthclientCredentials": {
						SchemaProps: spec.SchemaProps{
							Ref: ref("k8s.io/ingress-gce/pkg/apis/backendconfig/v1beta1.OAuthClientCredentials"),
						},
					},
				},
				Required: []string{"enabled"},
			},
		},
		Dependencies: []string{
			"k8s.io/ingress-gce/pkg/apis/backendconfig/v1beta1.OAuthClientCredentials"},
	}
}

func schema_pkg_apis_backendconfig_v1beta1_OAuthClientCredentials(ref common.ReferenceCallback) common.OpenAPIDefinition {
	return common.OpenAPIDefinition{
		Schema: spec.Schema{
			SchemaProps: spec.SchemaProps{
				Description: "OAuthClientCredentials contains credentials for a single IAP-enabled backend.",
				Type:        []string{"object"},
				Properties: map[string]spec.Schema{
					"secretName": {
						SchemaProps: spec.SchemaProps{
							Description: "The name of a k8s secret which stores the OAuth client id & secret.",
							Default:     "",
							Type:        []string{"string"},
							Format:      "",
						},
					},
					"clientID": {
						SchemaProps: spec.SchemaProps{
							Description: "Direct reference to OAuth client id.",
							Type:        []string{"string"},
							Format:      "",
						},
					},
					"clientSecret": {
						SchemaProps: spec.SchemaProps{
							Description: "Direct reference to OAuth client secret.",
							Type:        []string{"string"},
							Format:      "",
						},
					},
				},
				Required: []string{"secretName"},
			},
		},
	}
}

func schema_pkg_apis_backendconfig_v1beta1_SecurityPolicyConfig(ref common.ReferenceCallback) common.OpenAPIDefinition {
	return common.OpenAPIDefinition{
		Schema: spec.Schema{
			SchemaProps: spec.SchemaProps{
				Description: "SecurityPolicyConfig contains configuration for CloudArmor-enabled backends. If not specified, the controller will not reconcile the security policy configuration. In other words, users can make changes in GCE without the controller overwriting them.",
				Type:        []string{"object"},
				Properties: map[string]spec.Schema{
					"name": {
						SchemaProps: spec.SchemaProps{
							Description: "Name of the security policy that should be associated. If set to empty, the existing security policy on the backend will be removed.",
							Default:     "",
							Type:        []string{"string"},
							Format:      "",
						},
					},
				},
				Required: []string{"name"},
			},
		},
	}
}

func schema_pkg_apis_backendconfig_v1beta1_SessionAffinityConfig(ref common.ReferenceCallback) common.OpenAPIDefinition {
	return common.OpenAPIDefinition{
		Schema: spec.Schema{
			SchemaProps: spec.SchemaProps{
				Description: "SessionAffinityConfig contains configuration for stickiness parameters.",
				Type:        []string{"object"},
				Properties: map[string]spec.Schema{
					"affinityType": {
						SchemaProps: spec.SchemaProps{
							Type:   []string{"string"},
							Format: "",
						},
					},
					"affinityCookieTtlSec": {
						SchemaProps: spec.SchemaProps{
							Type:   []string{"integer"},
							Format: "int64",
						},
					},
				},
			},
		},
	}
}
