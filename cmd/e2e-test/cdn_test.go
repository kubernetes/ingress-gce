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

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"google.golang.org/api/compute/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/ingress-gce/pkg/annotations"
	backendconfig "k8s.io/ingress-gce/pkg/apis/backendconfig/v1"
	"k8s.io/ingress-gce/pkg/e2e"
	"k8s.io/ingress-gce/pkg/e2e/adapter"
	"k8s.io/ingress-gce/pkg/fuzz"
	"k8s.io/ingress-gce/pkg/fuzz/features"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog"
)

func TestCDNEnable(t *testing.T) {
	t.Parallel()

	const (
		serviceName1   = "cdn-service-1"
		backendconfig1 = "cdn-backendconfig-1"
		ingressName    = "cdn-ingress-1"
	)

	Framework.RunWithSandbox("CDN enabled/disabled tests", t, func(t *testing.T, s *e2e.Sandbox) {

		ing, err := setupIngress(s, ingressName, serviceName1, backendconfig1)
		if err != nil {
			t.Fatalf("%v", err)
		}

		for _, tc := range []struct {
			desc     string
			beConfig *backendconfig.BackendConfig
			expected *compute.BackendServiceCdnPolicy
		}{
			{
				desc: "cdn disabled",
				beConfig: fuzz.NewBackendConfigBuilder(s.Namespace, backendconfig1).
					SetCDNConfig(&backendconfig.CDNConfig{
						Enabled: false,
					}).Build(),
				expected: nil,
			},
			{
				desc: "cdn re-enabled",
				beConfig: fuzz.NewBackendConfigBuilder(s.Namespace, backendconfig1).
					SetCDNConfig(&backendconfig.CDNConfig{
						Enabled: true,
					}).Build(),
				expected: newBSWithDefaults().build(),
			},
			// disable cdn when backend config is removed is not supported for now
		} {
			t.Run(tc.desc, func(t *testing.T) {

				if err := updateConfigAndValidate(ing, s.Namespace, serviceName1, tc.beConfig, tc.expected); err != nil {
					t.Errorf("%v: %v", tc.desc, err)
				}

			})
		}
	})
}

func TestCDNCacheMode(t *testing.T) {
	t.Parallel()

	const (
		serviceName1   = "cdn-service-1"
		backendconfig1 = "cdn-backendconfig-1"
		ingressName    = "cdn-ingress-1"
	)

	var (
		cacheAllStatic   string = "CACHE_ALL_STATIC"
		useOriginHeaders string = "USE_ORIGIN_HEADERS"
		forceCacheAll    string = "FORCE_CACHE_ALL"
	)

	Framework.RunWithSandbox("CDN cache mode tests", t, func(t *testing.T, s *e2e.Sandbox) {

		ing, err := setupIngress(s, ingressName, serviceName1, backendconfig1)
		if err != nil {
			t.Fatalf("%v", err)
		}

		for _, tc := range []struct {
			desc     string
			beConfig *backendconfig.BackendConfig
			expected *compute.BackendServiceCdnPolicy
		}{
			{
				desc: "update TTLs",
				beConfig: fuzz.NewBackendConfigBuilder(s.Namespace, backendconfig1).
					SetCDNConfig(&backendconfig.CDNConfig{
						Enabled:    true,
						ClientTtl:  createInt64(1234),
						DefaultTtl: createInt64(1234),
						MaxTtl:     createInt64(1234),
					}).Build(),
				expected: newBSWithDefaults().setProp(func(cdn *compute.BackendServiceCdnPolicy) {
					cdn.CacheMode = cacheAllStatic
					cdn.ClientTtl = 1234
					cdn.DefaultTtl = 1234
					cdn.MaxTtl = 1234
				}).build(),
			},
			{
				desc: "update TTLs to zero",
				beConfig: fuzz.NewBackendConfigBuilder(s.Namespace, backendconfig1).
					SetCDNConfig(&backendconfig.CDNConfig{
						Enabled:    true,
						ClientTtl:  createInt64(0),
						DefaultTtl: createInt64(0),
						MaxTtl:     createInt64(0),
					}).Build(),
				expected: newBSWithDefaults().
					setProp(func(cdn *compute.BackendServiceCdnPolicy) {
						cdn.CacheMode = cacheAllStatic
						cdn.ClientTtl = 0
						cdn.DefaultTtl = 0
						cdn.MaxTtl = 0
					}).build(),
			},
			{
				desc: "Update cache mode to,from CACHE_ALL_STATIC to USE_ORIGIN_HEADERS",
				beConfig: fuzz.NewBackendConfigBuilder(s.Namespace, backendconfig1).
					SetCDNConfig(&backendconfig.CDNConfig{
						Enabled:   true,
						CacheMode: &useOriginHeaders,
					}).Build(),
				expected: newBSWithDefaults().setProp(func(cdn *compute.BackendServiceCdnPolicy) {
					cdn.CacheMode = useOriginHeaders
					cdn.ClientTtl = 0
					cdn.DefaultTtl = 0
					cdn.MaxTtl = 0
				}).build(),
			},
			{
				desc: "Update cache mode to,from USE_ORIGIN_HEADERS to FORCE_CACHE_ALL",
				beConfig: fuzz.NewBackendConfigBuilder(s.Namespace, backendconfig1).
					SetCDNConfig(&backendconfig.CDNConfig{
						Enabled:   true,
						CacheMode: &forceCacheAll,
					}).Build(),
				expected: newBSWithDefaults().setProp(func(cdn *compute.BackendServiceCdnPolicy) {
					cdn.CacheMode = forceCacheAll
					cdn.MaxTtl = 0
				}).build(),
			},
			{
				desc: "Update cache mode to,from FORCE_CACHE_ALL to USE_ORIGIN_HEADERS",
				beConfig: fuzz.NewBackendConfigBuilder(s.Namespace, backendconfig1).
					SetCDNConfig(&backendconfig.CDNConfig{
						Enabled:   true,
						CacheMode: &useOriginHeaders,
					}).Build(),
				expected: newBSWithDefaults().setProp(func(cdn *compute.BackendServiceCdnPolicy) {
					cdn.CacheMode = useOriginHeaders
					cdn.ClientTtl = 0
					cdn.DefaultTtl = 0
					cdn.MaxTtl = 0
				}).build(),
			},
			{
				desc: "Update cache mode to,from USE_ORIGIN_HEADERS to CACHE_ALL_STATIC",
				beConfig: fuzz.NewBackendConfigBuilder(s.Namespace, backendconfig1).
					SetCDNConfig(&backendconfig.CDNConfig{
						Enabled:   true,
						CacheMode: &cacheAllStatic,
					}).Build(),
				expected: newBSWithDefaults().
					setProp(func(cdn *compute.BackendServiceCdnPolicy) {
						cdn.CacheMode = cacheAllStatic
					}).build(),
			},
			{
				desc: "Update cache mode to, from CACHE_ALL_STATIC to FORCE_CACHE_ALL",
				beConfig: fuzz.NewBackendConfigBuilder(s.Namespace, backendconfig1).
					SetCDNConfig(&backendconfig.CDNConfig{
						Enabled:   true,
						CacheMode: &forceCacheAll,
					}).Build(),
				expected: newBSWithDefaults().setProp(func(cdn *compute.BackendServiceCdnPolicy) {
					cdn.CacheMode = forceCacheAll
					cdn.MaxTtl = 0
				}).build(),
			},
			{
				desc: "FORCE_CACHE_ALL update TTLs, set to zero",
				beConfig: fuzz.NewBackendConfigBuilder(s.Namespace, backendconfig1).
					SetCDNConfig(&backendconfig.CDNConfig{
						Enabled:    true,
						CacheMode:  &forceCacheAll,
						ClientTtl:  createInt64(0),
						DefaultTtl: createInt64(0),
						MaxTtl:     createInt64(0),
					}).Build(),
				expected: newBSWithDefaults().setProp(func(cdn *compute.BackendServiceCdnPolicy) {
					cdn.CacheMode = forceCacheAll
					cdn.ClientTtl = 0
					cdn.DefaultTtl = 0
					cdn.MaxTtl = 0
				}).build(),
			},
			{
				desc: "Update cache mode to, from FORCE_CACHE_ALL to CACHE_ALL_STATIC",
				beConfig: fuzz.NewBackendConfigBuilder(s.Namespace, backendconfig1).
					SetCDNConfig(&backendconfig.CDNConfig{
						Enabled:   true,
						CacheMode: &cacheAllStatic,
					}).Build(),
				expected: newBSWithDefaults().setProp(func(cdn *compute.BackendServiceCdnPolicy) {
					cdn.CacheMode = cacheAllStatic
				}).build(),
			},
		} {
			t.Run(tc.desc, func(t *testing.T) {

				if err := updateConfigAndValidate(ing, s.Namespace, serviceName1, tc.beConfig, tc.expected); err != nil {
					t.Errorf("%v: %v", tc.desc, err)
				}

			})
		}
	})
}

func TestCDNCacheKeyPolicy(t *testing.T) {
	t.Parallel()

	const (
		serviceName1   = "cdn-service-1"
		backendconfig1 = "cdn-backendconfig-1"
		ingressName    = "cdn-ingress-1"
	)

	Framework.RunWithSandbox("CDN cache key policy tests", t, func(t *testing.T, s *e2e.Sandbox) {

		ing, err := setupIngress(s, ingressName, serviceName1, backendconfig1)
		if err != nil {
			t.Fatalf("%v", err)
		}

		for _, tc := range []struct {
			desc     string
			beConfig *backendconfig.BackendConfig
			expected *compute.BackendServiceCdnPolicy
		}{
			{
				desc: "custom cache key",
				beConfig: fuzz.NewBackendConfigBuilder(s.Namespace, backendconfig1).
					SetCDNConfig(&backendconfig.CDNConfig{
						Enabled: true,
						CachePolicy: &backendconfig.CacheKeyPolicy{
							IncludeHost:        false,
							IncludeProtocol:    false,
							IncludeQueryString: false,
						},
					}).Build(),
				expected: newBSWithDefaults().setProp(func(cdn *compute.BackendServiceCdnPolicy) {
					cdn.CacheKeyPolicy.IncludeHost = false
					cdn.CacheKeyPolicy.IncludeProtocol = false
					cdn.CacheKeyPolicy.IncludeQueryString = false
				}).build(),
			},
			{
				desc: "set white list",
				beConfig: fuzz.NewBackendConfigBuilder(s.Namespace, backendconfig1).
					SetCDNConfig(&backendconfig.CDNConfig{
						Enabled: true,
						CachePolicy: &backendconfig.CacheKeyPolicy{
							IncludeHost:          true,
							IncludeProtocol:      false,
							IncludeQueryString:   true,
							QueryStringWhitelist: []string{"query1", "query2"},
						},
					}).Build(),
				expected: newBSWithDefaults().setProp(func(cdn *compute.BackendServiceCdnPolicy) {
					cdn.CacheKeyPolicy.IncludeHost = true
					cdn.CacheKeyPolicy.IncludeProtocol = false
					cdn.CacheKeyPolicy.IncludeQueryString = true
					cdn.CacheKeyPolicy.QueryStringWhitelist = []string{"query1", "query2"}
				}).build(),
			},
			{
				desc: "set black list",
				beConfig: fuzz.NewBackendConfigBuilder(s.Namespace, backendconfig1).
					SetCDNConfig(&backendconfig.CDNConfig{
						Enabled: true,
						CachePolicy: &backendconfig.CacheKeyPolicy{
							IncludeHost:          true,
							IncludeProtocol:      false,
							IncludeQueryString:   true,
							QueryStringBlacklist: []string{"query3", "query4"},
						},
					}).Build(),
				expected: newBSWithDefaults().setProp(func(cdn *compute.BackendServiceCdnPolicy) {
					cdn.CacheKeyPolicy.IncludeHost = true
					cdn.CacheKeyPolicy.IncludeProtocol = false
					cdn.CacheKeyPolicy.IncludeQueryString = true
					cdn.CacheKeyPolicy.QueryStringBlacklist = []string{"query3", "query4"}
				}).build(),
			},
			{
				desc: "set IncludeQueryString to false, white list",
				beConfig: fuzz.NewBackendConfigBuilder(s.Namespace, backendconfig1).
					SetCDNConfig(&backendconfig.CDNConfig{
						Enabled: true,
						CachePolicy: &backendconfig.CacheKeyPolicy{
							IncludeHost:          true,
							IncludeProtocol:      false,
							IncludeQueryString:   false,
							QueryStringWhitelist: []string{"query1", "query2"},
						},
					}).Build(),
				expected: newBSWithDefaults().setProp(func(cdn *compute.BackendServiceCdnPolicy) {
					cdn.CacheKeyPolicy.IncludeHost = true
					cdn.CacheKeyPolicy.IncludeProtocol = false
					cdn.CacheKeyPolicy.IncludeQueryString = false
				}).build(),
			},
			{
				desc: "set IncludeQueryString to false, black list",
				beConfig: fuzz.NewBackendConfigBuilder(s.Namespace, backendconfig1).
					SetCDNConfig(&backendconfig.CDNConfig{
						Enabled: true,
						CachePolicy: &backendconfig.CacheKeyPolicy{
							IncludeHost:          true,
							IncludeProtocol:      false,
							IncludeQueryString:   false,
							QueryStringBlacklist: []string{"query3", "query4"},
						},
					}).Build(),
				expected: newBSWithDefaults().setProp(func(cdn *compute.BackendServiceCdnPolicy) {
					cdn.CacheKeyPolicy.IncludeHost = true
					cdn.CacheKeyPolicy.IncludeProtocol = false
					cdn.CacheKeyPolicy.IncludeQueryString = false
				}).build(),
			},
			{
				desc: "restore defaults",
				beConfig: fuzz.NewBackendConfigBuilder(s.Namespace, backendconfig1).
					SetCDNConfig(&backendconfig.CDNConfig{
						Enabled: true,
					}).Build(),
				expected: newBSWithDefaults().build(),
			},
		} {
			t.Run(tc.desc, func(t *testing.T) {

				if err := updateConfigAndValidate(ing, s.Namespace, serviceName1, tc.beConfig, tc.expected); err != nil {
					t.Errorf("%v: %v", tc.desc, err)
				}

			})
		}
	})
}

func TestCDNNegativeCaching(t *testing.T) {
	t.Parallel()

	const (
		serviceName1   = "cdn-service-1"
		backendconfig1 = "cdn-backendconfig-1"
		ingressName    = "cdn-ingress-1"
	)

	Framework.RunWithSandbox("CDN negative caching tests", t, func(t *testing.T, s *e2e.Sandbox) {
		ing, err := setupIngress(s, ingressName, serviceName1, backendconfig1)
		if err != nil {
			t.Fatalf("%v", err)
		}

		for _, tc := range []struct {
			desc     string
			beConfig *backendconfig.BackendConfig
			expected *compute.BackendServiceCdnPolicy
		}{
			{
				desc: "negative caching,defaults",
				beConfig: fuzz.NewBackendConfigBuilder(s.Namespace, backendconfig1).
					SetCDNConfig(&backendconfig.CDNConfig{
						Enabled:         true,
						NegativeCaching: createBool(true),
						NegativeCachingPolicy: []*backendconfig.NegativeCachingPolicy{
							{Code: 301, Ttl: 600},
							{Code: 404, Ttl: 1800},
						},
					}).Build(),
				expected: newBSWithDefaults().setProp(func(cdn *compute.BackendServiceCdnPolicy) {
					cdn.NegativeCaching = true
					cdn.NegativeCachingPolicy = []*compute.BackendServiceCdnPolicyNegativeCachingPolicy{
						{Code: 301, Ttl: 600},
						{Code: 404, Ttl: 1800},
					}
				}).build(),
			},
			{
				desc: "negative caching,disable",
				beConfig: fuzz.NewBackendConfigBuilder(s.Namespace, backendconfig1).
					SetCDNConfig(&backendconfig.CDNConfig{
						Enabled:         true,
						NegativeCaching: createBool(false),
						NegativeCachingPolicy: []*backendconfig.NegativeCachingPolicy{
							{Code: 301, Ttl: 600},
							{Code: 404, Ttl: 1800},
						},
					}).Build(),
				expected: newBSWithDefaults().setProp(func(cdn *compute.BackendServiceCdnPolicy) {
					cdn.NegativeCaching = false
				}).build(),
			},
			{
				desc: "negative caching, restore to defaults",
				beConfig: fuzz.NewBackendConfigBuilder(s.Namespace, backendconfig1).
					SetCDNConfig(&backendconfig.CDNConfig{
						Enabled: true,
					}).Build(),
				expected: newBSWithDefaults().build(),
			},
		} {
			t.Run(tc.desc, func(t *testing.T) {

				if err := updateConfigAndValidate(ing, s.Namespace, serviceName1, tc.beConfig, tc.expected); err != nil {
					t.Errorf("%v: %v", tc.desc, err)
				}
			})
		}
	})
}

func TestCDNBypassCache(t *testing.T) {
	t.Parallel()

	const (
		serviceName1   = "cdn-service-1"
		backendconfig1 = "cdn-backendconfig-1"
		ingressName    = "cdn-ingress-1"
	)

	Framework.RunWithSandbox("CDN bypass cache on request headers tests", t, func(t *testing.T, s *e2e.Sandbox) {

		ing, err := setupIngress(s, ingressName, serviceName1, backendconfig1)
		if err != nil {
			t.Fatalf("%v", err)
		}

		for _, tc := range []struct {
			desc     string
			beConfig *backendconfig.BackendConfig
			expected *compute.BackendServiceCdnPolicy
		}{
			{
				desc: "bypass cache, set headers",
				beConfig: fuzz.NewBackendConfigBuilder(s.Namespace, backendconfig1).
					SetCDNConfig(&backendconfig.CDNConfig{
						Enabled: true,
						BypassCacheOnRequestHeaders: []*backendconfig.BypassCacheOnRequestHeader{
							{HeaderName: "X-Bypass-Cache-1"},
							{HeaderName: "X-Bypass-Cache-2"},
						},
					}).Build(),
				expected: newBSWithDefaults().setProp(func(cdn *compute.BackendServiceCdnPolicy) {
					cdn.BypassCacheOnRequestHeaders = []*compute.BackendServiceCdnPolicyBypassCacheOnRequestHeader{
						{HeaderName: "X-Bypass-Cache-1"},
						{HeaderName: "X-Bypass-Cache-2"},
					}
				}).build(),
			},
			{
				desc: "bypass cache, update headers",
				beConfig: fuzz.NewBackendConfigBuilder(s.Namespace, backendconfig1).
					SetCDNConfig(&backendconfig.CDNConfig{
						Enabled: true,
						BypassCacheOnRequestHeaders: []*backendconfig.BypassCacheOnRequestHeader{
							{HeaderName: "X-Bypass-Cache-3"},
							{HeaderName: "X-Bypass-Cache-4"},
						},
					}).Build(),
				expected: newBSWithDefaults().setProp(func(cdn *compute.BackendServiceCdnPolicy) {
					cdn.BypassCacheOnRequestHeaders = []*compute.BackendServiceCdnPolicyBypassCacheOnRequestHeader{
						{HeaderName: "X-Bypass-Cache-3"},
						{HeaderName: "X-Bypass-Cache-4"},
					}
				}).build(),
			},
			{
				desc: "bypass cache, reset to defaults",
				beConfig: fuzz.NewBackendConfigBuilder(s.Namespace, backendconfig1).
					SetCDNConfig(&backendconfig.CDNConfig{
						Enabled: true,
					}).Build(),
				expected: newBSWithDefaults().build(),
			},
		} {
			t.Run(tc.desc, func(t *testing.T) {

				if err := updateConfigAndValidate(ing, s.Namespace, serviceName1, tc.beConfig, tc.expected); err != nil {
					t.Errorf("%v: %v", tc.desc, err)
				}

			})
		}
	})
}

func TestCDNServeWhileStale(t *testing.T) {
	t.Parallel()

	const (
		serviceName1   = "cdn-service-1"
		backendconfig1 = "cdn-backendconfig-1"
		ingressName    = "cdn-ingress-1"
	)

	Framework.RunWithSandbox("CDN serve while stale tests", t, func(t *testing.T, s *e2e.Sandbox) {

		ing, err := setupIngress(s, ingressName, serviceName1, backendconfig1)
		if err != nil {
			t.Fatalf("%v", err)
		}

		for _, tc := range []struct {
			desc     string
			beConfig *backendconfig.BackendConfig
			expected *compute.BackendServiceCdnPolicy
		}{
			{
				desc: "serve while stale, set",
				beConfig: fuzz.NewBackendConfigBuilder(s.Namespace, backendconfig1).
					SetCDNConfig(&backendconfig.CDNConfig{
						Enabled:         true,
						ServeWhileStale: createInt64(1234),
					}).Build(),
				expected: newBSWithDefaults().setProp(func(cdn *compute.BackendServiceCdnPolicy) {
					cdn.ServeWhileStale = 1234
				}).build(),
			},
			{
				desc: "serve while stale, update",
				beConfig: fuzz.NewBackendConfigBuilder(s.Namespace, backendconfig1).
					SetCDNConfig(&backendconfig.CDNConfig{
						Enabled:         true,
						ServeWhileStale: createInt64(4321),
					}).Build(),
				expected: newBSWithDefaults().setProp(func(cdn *compute.BackendServiceCdnPolicy) {
					cdn.ServeWhileStale = 4321
				}).build(),
			},
			{
				desc: "serve while stale, reset to defaults",
				beConfig: fuzz.NewBackendConfigBuilder(s.Namespace, backendconfig1).
					SetCDNConfig(&backendconfig.CDNConfig{
						Enabled: true,
					}).Build(),
				expected: newBSWithDefaults().build(),
			},
		} {
			t.Run(tc.desc, func(t *testing.T) {

				if err := updateConfigAndValidate(ing, s.Namespace, serviceName1, tc.beConfig, tc.expected); err != nil {
					t.Errorf("%v: %v", tc.desc, err)
				}

			})
		}
	})
}

func TestCDNRequestCoalescing(t *testing.T) {
	t.Parallel()

	const (
		serviceName1   = "cdn-service-1"
		backendconfig1 = "cdn-backendconfig-1"
		ingressName    = "cdn-ingress-1"
	)

	Framework.RunWithSandbox("CDN request coalescing tests", t, func(t *testing.T, s *e2e.Sandbox) {

		ing, err := setupIngress(s, ingressName, serviceName1, backendconfig1)
		if err != nil {
			t.Fatalf("%v", err)
		}

		for _, tc := range []struct {
			desc     string
			beConfig *backendconfig.BackendConfig
			expected *compute.BackendServiceCdnPolicy
		}{
			{
				desc: "request coalescing, disable",
				beConfig: fuzz.NewBackendConfigBuilder(s.Namespace, backendconfig1).
					SetCDNConfig(&backendconfig.CDNConfig{
						Enabled:           true,
						RequestCoalescing: createBool(false),
					}).Build(),
				expected: newBSWithDefaults().setProp(func(cdn *compute.BackendServiceCdnPolicy) {
					cdn.RequestCoalescing = false
				}).build(),
			},
			{
				desc: "request coalescing, enable",
				beConfig: fuzz.NewBackendConfigBuilder(s.Namespace, backendconfig1).
					SetCDNConfig(&backendconfig.CDNConfig{
						Enabled:           true,
						RequestCoalescing: createBool(true),
					}).Build(),
				expected: newBSWithDefaults().setProp(func(cdn *compute.BackendServiceCdnPolicy) {
					cdn.RequestCoalescing = true
				}).build(),
			},
			{
				desc: "request coalescing, reset to defaults",
				beConfig: fuzz.NewBackendConfigBuilder(s.Namespace, backendconfig1).
					SetCDNConfig(&backendconfig.CDNConfig{
						Enabled: true,
					}).Build(),
				expected: newBSWithDefaults().build(),
			},
		} {
			t.Run(tc.desc, func(t *testing.T) {

				if err := updateConfigAndValidate(ing, s.Namespace, serviceName1, tc.beConfig, tc.expected); err != nil {
					t.Errorf("%v: %v", tc.desc, err)
				}

			})
		}
	})
}

func TestCDNSignedUrls(t *testing.T) {
	t.Parallel()

	const (
		serviceName1   = "cdn-service-1"
		backendconfig1 = "cdn-backendconfig-1"
		ingressName    = "cdn-ingress-1"
	)

	Framework.RunWithSandbox("CDN signed urls tests", t, func(t *testing.T, s *e2e.Sandbox) {

		ing, err := setupIngress(s, ingressName, serviceName1, backendconfig1)
		if err != nil {
			t.Fatalf("%v", err)
		}

		for _, tc := range []struct {
			desc     string
			beConfig *backendconfig.BackendConfig
			expected *compute.BackendServiceCdnPolicy
		}{
			{
				desc: "signed url cache max age, set",
				beConfig: fuzz.NewBackendConfigBuilder(s.Namespace, backendconfig1).
					SetCDNConfig(&backendconfig.CDNConfig{
						Enabled:                 true,
						SignedUrlCacheMaxAgeSec: createInt64(1234),
						SignedUrlKeys: []*backendconfig.SignedUrlKey{
							{KeyName: "key1", KeyValue: "MH5PnJa2HCKM232GxJ3z0g=="},
							{KeyName: "key2", KeyValue: "MH5PnJa2HCKM232GxJ3z0g=="},
							{KeyName: "key3", KeyValue: "MH5PnJa2HCKM232GxJ3z0g=="},
						},
					}).Build(),
				expected: newBSWithDefaults().setProp(func(cdn *compute.BackendServiceCdnPolicy) {
					cdn.SignedUrlCacheMaxAgeSec = 1234
					cdn.SignedUrlKeyNames = []string{"key1", "key2", "key3"}
				}).build(),
			},
			{
				desc: "signed url cache max age, update",
				beConfig: fuzz.NewBackendConfigBuilder(s.Namespace, backendconfig1).
					SetCDNConfig(&backendconfig.CDNConfig{
						Enabled:                 true,
						SignedUrlCacheMaxAgeSec: createInt64(3421),
						SignedUrlKeys: []*backendconfig.SignedUrlKey{
							{KeyName: "key4", KeyValue: "MH5PnJa2HCKM232GxJ3z0g=="},
							{KeyName: "key5", KeyValue: "MH5PnJa2HCKM232GxJ3z0g=="},
							{KeyName: "key6", KeyValue: "MH5PnJa2HCKM232GxJ3z0g=="},
						},
					}).Build(),
				expected: newBSWithDefaults().setProp(func(cdn *compute.BackendServiceCdnPolicy) {
					cdn.SignedUrlCacheMaxAgeSec = 3421
					cdn.SignedUrlKeyNames = []string{"key4", "key5", "key6"}
				}).build(),
			},
			{
				desc: "signed url cache max age, reset to defaults",
				beConfig: fuzz.NewBackendConfigBuilder(s.Namespace, backendconfig1).
					SetCDNConfig(&backendconfig.CDNConfig{
						Enabled: true,
					}).Build(),
				expected: newBSWithDefaults().build(),
			},
		} {
			t.Run(tc.desc, func(t *testing.T) {

				if err := updateConfigAndValidate(ing, s.Namespace, serviceName1, tc.beConfig, tc.expected); err != nil {
					t.Errorf("%v: %v", tc.desc, err)
				}

			})
		}
	})
}

// helper functions

func createInt64(a int64) *int64 {
	return &a
}

func createBool(a bool) *bool {
	return &a
}

func prettyJSON(data interface{}) string {
	buffer := new(bytes.Buffer)
	encoder := json.NewEncoder(buffer)
	encoder.SetIndent("", "\t")

	err := encoder.Encode(data)
	if err != nil {
		panic(err)
	}
	return buffer.String()
}

type backendServiceBuilder struct {
	CdnPolicy *compute.BackendServiceCdnPolicy
}

func newBSWithDefaults() *backendServiceBuilder {
	return &backendServiceBuilder{
		CdnPolicy: &compute.BackendServiceCdnPolicy{
			CacheKeyPolicy: &compute.CacheKeyPolicy{
				IncludeHost:        true,
				IncludeProtocol:    true,
				IncludeQueryString: true,
			},
			CacheMode:         "CACHE_ALL_STATIC",
			ClientTtl:         3600,
			DefaultTtl:        3600,
			MaxTtl:            86400,
			NegativeCaching:   true,
			RequestCoalescing: true,
			ServeWhileStale:   86400,
		},
	}
}

func (bsb *backendServiceBuilder) setProp(fn func(*compute.BackendServiceCdnPolicy)) *backendServiceBuilder {
	fn(bsb.CdnPolicy)
	return bsb
}

func (bsb *backendServiceBuilder) build() *compute.BackendServiceCdnPolicy {
	return bsb.CdnPolicy
}

func setupIngress(s *e2e.Sandbox, ingressName, serviceName1, backendconfig1 string) (*networkingv1.Ingress, error) {

	ingress := fuzz.NewIngressBuilder(s.Namespace, ingressName, "").
		AddPath("", "/", serviceName1, networkingv1.ServiceBackendPort{Number: 80}).
		Build()

	serviceAnnotations := map[string]string{
		annotations.BackendConfigKey: fmt.Sprintf(`{"%s":"%s"}`, "default", backendconfig1),
		annotations.NEGAnnotationKey: `{"ingress": true}`,
	}

	// create the backend config with defaults
	bcCRUD := adapter.BackendConfigCRUD{C: Framework.BackendConfigClient}
	_, err := bcCRUD.Ensure(fuzz.NewBackendConfigBuilder(s.Namespace, backendconfig1).
		SetCDNConfig(&backendconfig.CDNConfig{
			Enabled: true,
		}).Build())
	if err != nil {
		return nil, fmt.Errorf("error creating BackendConfig: %v", err)
	}
	// create the service and deployment
	if _, err := e2e.CreateEchoService(s, serviceName1, serviceAnnotations); err != nil {
		return nil, fmt.Errorf("error creating echo service: %v", err)
	}
	// create the ingress
	ing, err := e2e.EnsureIngress(s, ingress)
	if err != nil {
		return nil, fmt.Errorf("error ensuring Ingress spec: %v", err)
	}
	// wait for ingress to stabilize
	ing, err = e2e.WaitForIngress(s, ing, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("error waiting for Ingress to stabilize: %v", err)
	}
	// validate the default cdn setup
	err = validateBackend(ing, s.Namespace, serviceName1, newBSWithDefaults().build())
	if err != nil {
		return nil, fmt.Errorf("error validate backend: %v", err)
	}

	return ing, nil
}

func updateConfigAndValidate(ing *networkingv1.Ingress, namespace, serviceName string,
	beConfig *backendconfig.BackendConfig, expected *compute.BackendServiceCdnPolicy) error {

	// update the backend configuration
	bcCRUD := adapter.BackendConfigCRUD{C: Framework.BackendConfigClient}
	_, err := bcCRUD.Ensure(beConfig)
	if err != nil {
		return fmt.Errorf("error creating BackendConfig: %v", err)
	}

	// wait and validate the changes
	var lastError error
	waitErr := wait.Poll(transitionPollInterval, transitionPollTimeout, func() (bool, error) {
		lastError = validateBackend(ing, namespace, serviceName, expected)
		if lastError == nil {
			return true, nil
		}
		klog.V(2).Infof("Retry backend validation for %s/%s", namespace, serviceName)
		return false, nil
	})
	if waitErr != nil {
		return lastError
	}
	return nil
}

func validateBackend(ing *networkingv1.Ingress, namespace, serviceName string, expected *compute.BackendServiceCdnPolicy) error {

	klog.V(2).Infof("Begin backend validation for %s/%s", namespace, serviceName)

	vip := ing.Status.LoadBalancer.Ingress[0].IP
	params := &fuzz.GCLBForVIPParams{
		VIP:        vip,
		Validators: fuzz.FeatureValidators(features.All),
	}
	gclb, err := fuzz.GCLBForVIP(context.Background(), Framework.Cloud, params)
	if err != nil {
		return fmt.Errorf("error getting GCP resources for LB with IP = %q: %v", vip, err)
	}

	numBsWithPolicy := 0
	for _, bs := range gclb.BackendService {
		// find our backend service
		bcdesc := utils.DescriptionFromString(bs.GA.Description)
		if bcdesc.ServiceName != fmt.Sprintf("%s/%s", namespace, serviceName) {
			continue
		}
		numBsWithPolicy++

		if expected == nil {
			if bs.GA.EnableCDN {
				return fmt.Errorf("CDN enabled on service %s/%s expected not", namespace, serviceName)
			}
			return nil
		}

		if !bs.GA.EnableCDN {
			return fmt.Errorf("CDN not enabled on service %s/%s", namespace, serviceName)
		}

		if bs.GA.CdnPolicy == nil {
			return fmt.Errorf("backend service %q has no CdnPolicy", bs.GA.Name)
		}

		// we need deep copy here because we will alter the objects
		expCdnPolicy := deepCopyCdnPolicy(expected)
		bsCdnPolicy := deepCopyCdnPolicy(bs.GA.CdnPolicy)

		if sliceEqual(expCdnPolicy.NegativeCachingPolicy, bsCdnPolicy.NegativeCachingPolicy) {
			expCdnPolicy.NegativeCachingPolicy = nil
			bsCdnPolicy.NegativeCachingPolicy = nil
		}

		if !reflect.DeepEqual(expCdnPolicy, bsCdnPolicy) {
			return fmt.Errorf("expected %s, but got %s",
				prettyJSON(expected), // use the original values for error reporting
				prettyJSON(bs.GA.CdnPolicy))
		}
	}

	if numBsWithPolicy != 1 {
		return fmt.Errorf("unexpected number of backend service has cache policy attached: got %d, want 1", numBsWithPolicy)
	}
	return nil
}

func sliceEqual(x, y []*compute.BackendServiceCdnPolicyNegativeCachingPolicy) bool {
	if len(x) != len(y) {
		return false
	}
	diff := make(map[string]int, len(x))
	for _, v := range x {
		diff[fmt.Sprintf("%d-%d", v.Code, v.Ttl)]++
	}
	for _, v := range y {
		key := fmt.Sprintf("%d-%d", v.Code, v.Ttl)
		if _, ok := diff[key]; !ok {
			return false
		}
		diff[key]--
		if diff[key] == 0 {
			delete(diff, key)
		}
	}
	return len(diff) == 0
}

func copyViaJSON(dst, src interface{}) error {
	var err error
	bytes, err := json.Marshal(src)
	if err != nil {
		return err
	}
	return json.Unmarshal(bytes, dst)
}

func deepCopyCdnPolicy(src *compute.BackendServiceCdnPolicy) *compute.BackendServiceCdnPolicy {
	dst := &compute.BackendServiceCdnPolicy{}
	if err := copyViaJSON(dst, src); err != nil {
		panic(err)
	}
	return dst
}
