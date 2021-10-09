/*
Copyright 2015 The Kubernetes Authors.

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

package features

import (
	"bytes"
	"encoding/json"
	"flag"
	"reflect"
	"testing"

	bcnf "k8s.io/ingress-gce/pkg/apis/backendconfig/v1"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog"
)

var (
	cacheAllStatic   string = "CACHE_ALL_STATIC"
	useOriginHeaders string = "USE_ORIGIN_HEADERS"
	forceCacheAll    string = "FORCE_CACHE_ALL"
)

func init() {
	// Init klog flags so we can see the V logs.
	klog.InitFlags(nil)
	var logLevel string
	flag.StringVar(&logLevel, "logLevel", "0", "test")
	flag.Lookup("v").Value.Set(logLevel)
}

func createInt64(a int64) *int64 {
	return &a
}

func createBool(a bool) *bool {
	return &a
}

func copyViaJSON(dst, src interface{}) error {
	var err error
	bytes, err := json.Marshal(src)
	if err != nil {
		return err
	}
	return json.Unmarshal(bytes, dst)
}

func deepCopyCdnPolicy(src *composite.BackendServiceCdnPolicy) *composite.BackendServiceCdnPolicy {
	dst := &composite.BackendServiceCdnPolicy{}
	if err := copyViaJSON(dst, src); err != nil {
		panic(err)
	}
	return dst
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

type servicePortBuilder struct {
	CDNConfig *bcnf.CDNConfig
}

func newServicePort() *servicePortBuilder {
	return &servicePortBuilder{CDNConfig: &bcnf.CDNConfig{
		Enabled: true,
	}}
}

func (sb *servicePortBuilder) setProp(fn func(*bcnf.CDNConfig)) *servicePortBuilder {
	fn(sb.CDNConfig)
	return sb
}

func (sb *servicePortBuilder) build() utils.ServicePort {
	return utils.ServicePort{
		BackendConfig: &bcnf.BackendConfig{
			Spec: bcnf.BackendConfigSpec{
				Cdn: sb.CDNConfig,
			},
		}}
}

type backendServiceBuilder struct {
	CdnPolicy *composite.BackendServiceCdnPolicy
}

func newDefaultBackendService() *backendServiceBuilder {
	return &backendServiceBuilder{
		CdnPolicy: deepCopyCdnPolicy(&defaultCdnPolicy),
	}
}

func (bsb *backendServiceBuilder) setProp(fn func(*composite.BackendServiceCdnPolicy)) *backendServiceBuilder {
	fn(bsb.CdnPolicy)
	return bsb
}

func (bsb *backendServiceBuilder) build() *composite.BackendService {
	return &composite.BackendService{
		EnableCDN: true,
		CdnPolicy: bsb.CdnPolicy,
	}
}

func defaultCdnPolicyCopy() *composite.BackendServiceCdnPolicy {
	return deepCopyCdnPolicy(&defaultCdnPolicy)
}

type testCase struct {
	desc           string
	sp             utils.ServicePort
	be             *composite.BackendService
	updateExpected bool
	beAfter        *composite.BackendService
}

func evaluateTestCases(t *testing.T, testCases []testCase) {
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {

			// we must fail the test if the properties are equal before merge
			if tc.updateExpected && reflect.DeepEqual(tc.beAfter, tc.be) {
				t.Errorf("%v: be and beAfter are equal before merge %v = %v", tc.desc,
					prettyJSON(tc.beAfter),
					prettyJSON(tc.be))
			}

			result := EnsureCDN(tc.sp, tc.be)

			if result != tc.updateExpected {
				t.Errorf("%v: expected %v but got %v", tc.desc, tc.updateExpected, result)
			}

			if !reflect.DeepEqual(tc.beAfter, tc.be) {
				t.Errorf("%v: expected %v but got %v", tc.desc,
					prettyJSON(tc.beAfter),
					prettyJSON(tc.be))
			}
		})
	}
}

func TestEnsureCDN(t *testing.T) {

	testCases := []testCase{
		{
			desc: "cdn setting are missing from spec, no update needed",
			sp: utils.ServicePort{
				BackendConfig: &bcnf.BackendConfig{
					Spec: bcnf.BackendConfigSpec{
						Cdn: nil,
					},
				},
			},
			be: &composite.BackendService{
				EnableCDN: true,
				CdnPolicy: &composite.BackendServiceCdnPolicy{
					CacheKeyPolicy: &composite.CacheKeyPolicy{
						IncludeHost: true,
					},
				},
			},
			updateExpected: false,
			beAfter: &composite.BackendService{
				EnableCDN: true,
				CdnPolicy: &composite.BackendServiceCdnPolicy{
					CacheKeyPolicy: &composite.CacheKeyPolicy{
						IncludeHost: true,
					},
				},
			},
		},
		{
			desc: "cache policies are missing from spec, no update needed",
			sp: utils.ServicePort{
				BackendConfig: &bcnf.BackendConfig{
					Spec: bcnf.BackendConfigSpec{
						Cdn: &bcnf.CDNConfig{
							Enabled: true,
						},
					},
				},
			},
			be: &composite.BackendService{
				EnableCDN: true,
				CdnPolicy: deepCopyCdnPolicy(&defaultCdnPolicy),
			},
			updateExpected: false,
			beAfter: &composite.BackendService{
				EnableCDN: true,
				CdnPolicy: deepCopyCdnPolicy(&defaultCdnPolicy),
			},
		},
		{
			desc: "settings are identical, no update needed",
			sp: utils.ServicePort{
				BackendConfig: &bcnf.BackendConfig{
					Spec: bcnf.BackendConfigSpec{
						Cdn: &bcnf.CDNConfig{
							Enabled:   true,
							CacheMode: &cacheAllStatic,
							CachePolicy: &bcnf.CacheKeyPolicy{
								IncludeHost:          true,
								IncludeProtocol:      false,
								IncludeQueryString:   true,
								QueryStringWhitelist: []string{"testQueryString"},
								QueryStringBlacklist: []string{"testQueryString"},
							},
							MaxTtl:          createInt64(3600),
							ClientTtl:       createInt64(3600),
							DefaultTtl:      createInt64(3600),
							NegativeCaching: createBool(true),
							NegativeCachingPolicy: []*bcnf.NegativeCachingPolicy{
								{Code: 301, Ttl: 1800},
							},
							ServeWhileStale:   createInt64(86400),
							RequestCoalescing: createBool(true),
							BypassCacheOnRequestHeaders: []*bcnf.BypassCacheOnRequestHeader{
								{HeaderName: "header"},
							},
							SignedUrlCacheMaxAgeSec: createInt64(3600),
						},
					},
				},
			},
			be: &composite.BackendService{
				EnableCDN: true,
				CdnPolicy: &composite.BackendServiceCdnPolicy{
					CacheKeyPolicy: &composite.CacheKeyPolicy{
						IncludeHost:          true,
						IncludeProtocol:      false,
						IncludeQueryString:   true,
						QueryStringWhitelist: []string{"testQueryString"},
						QueryStringBlacklist: []string{"testQueryString"},
					},
					CacheMode:       cacheAllStatic,
					MaxTtl:          3600,
					ClientTtl:       3600,
					DefaultTtl:      3600,
					NegativeCaching: true,
					NegativeCachingPolicy: []*composite.BackendServiceCdnPolicyNegativeCachingPolicy{
						{Code: 301, Ttl: 1800},
					},
					ServeWhileStale:   86400,
					RequestCoalescing: true,
					BypassCacheOnRequestHeaders: []*composite.BackendServiceCdnPolicyBypassCacheOnRequestHeader{
						{HeaderName: "header"},
					},
					SignedUrlCacheMaxAgeSec: 3600,
					SignedUrlKeyNames:       []string{"key1"},
				},
			},
			updateExpected: false,
			beAfter: &composite.BackendService{
				EnableCDN: true,
				CdnPolicy: &composite.BackendServiceCdnPolicy{
					CacheKeyPolicy: &composite.CacheKeyPolicy{
						IncludeHost:          true,
						IncludeProtocol:      false,
						IncludeQueryString:   true,
						QueryStringWhitelist: []string{"testQueryString"},
						QueryStringBlacklist: []string{"testQueryString"},
					},
					CacheMode:       cacheAllStatic,
					MaxTtl:          3600,
					ClientTtl:       3600,
					DefaultTtl:      3600,
					NegativeCaching: true,
					NegativeCachingPolicy: []*composite.BackendServiceCdnPolicyNegativeCachingPolicy{
						{Code: 301, Ttl: 1800},
					},
					ServeWhileStale:   86400,
					RequestCoalescing: true,
					BypassCacheOnRequestHeaders: []*composite.BackendServiceCdnPolicyBypassCacheOnRequestHeader{
						{HeaderName: "header"},
					},
					SignedUrlCacheMaxAgeSec: 3600,
					SignedUrlKeyNames:       []string{"key1"},
				},
			},
		},
		{
			desc: "enabled setting is different, true, update needed",
			sp: utils.ServicePort{
				BackendConfig: &bcnf.BackendConfig{
					Spec: bcnf.BackendConfigSpec{
						Cdn: &bcnf.CDNConfig{
							Enabled: true,
						},
					},
				},
			},
			be: &composite.BackendService{
				EnableCDN: false,
			},
			updateExpected: true,
			beAfter: &composite.BackendService{
				EnableCDN: true,
				CdnPolicy: deepCopyCdnPolicy(&defaultCdnPolicy),
			},
		},
		{
			desc: "enabled setting is different, false, update needed",
			sp: utils.ServicePort{
				BackendConfig: &bcnf.BackendConfig{
					Spec: bcnf.BackendConfigSpec{
						Cdn: &bcnf.CDNConfig{
							Enabled: false,
						},
					},
				},
			},
			be: &composite.BackendService{
				EnableCDN: true,
				CdnPolicy: deepCopyCdnPolicy(&defaultCdnPolicy),
			},
			updateExpected: true,
			beAfter: &composite.BackendService{
				EnableCDN: false,
				CdnPolicy: deepCopyCdnPolicy(&defaultCdnPolicy),
			},
		},
		{
			desc: "settings are equal with defaults, no update needed",
			sp: newServicePort().setProp(func(cdn *bcnf.CDNConfig) {
				defCdn := defaultCdnPolicyCopy()
				cdn.CacheMode = &defaultCdnPolicy.CacheMode
				cdn.CachePolicy = &bcnf.CacheKeyPolicy{
					IncludeHost:          defCdn.CacheKeyPolicy.IncludeHost,
					IncludeProtocol:      defCdn.CacheKeyPolicy.IncludeProtocol,
					IncludeQueryString:   defCdn.CacheKeyPolicy.IncludeQueryString,
					QueryStringWhitelist: defCdn.CacheKeyPolicy.QueryStringWhitelist,
					QueryStringBlacklist: defCdn.CacheKeyPolicy.QueryStringBlacklist,
				}
				cdn.MaxTtl = createInt64(defCdn.MaxTtl)
				cdn.ClientTtl = createInt64(defCdn.ClientTtl)
				cdn.DefaultTtl = createInt64(defCdn.DefaultTtl)
				cdn.NegativeCaching = createBool(defCdn.NegativeCaching)
				cdn.ServeWhileStale = createInt64(defCdn.ServeWhileStale)
				cdn.RequestCoalescing = createBool(defCdn.RequestCoalescing)
				cdn.SignedUrlCacheMaxAgeSec = createInt64(defCdn.SignedUrlCacheMaxAgeSec)
			}).build(),
			be:             newDefaultBackendService().build(),
			updateExpected: false,
			beAfter:        newDefaultBackendService().build(),
		},
	}

	evaluateTestCases(t, testCases)
}

func TestEnsureCDNCacheMode(t *testing.T) {

	testCases := []testCase{
		{
			desc: "cacheMode from CACHE_ALL_STATIC to USE_ORIGIN_HEADERS",
			sp: newServicePort().setProp(func(cdn *bcnf.CDNConfig) {
				cdn.CacheMode = &useOriginHeaders
			}).build(),
			be: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.CacheMode = cacheAllStatic
			}).build(),
			updateExpected: true,
			beAfter: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.CacheMode = useOriginHeaders
				cdn.ClientTtl = 0
				cdn.DefaultTtl = 0
				cdn.MaxTtl = 0
			}).build(),
		},
		{
			desc: "cacheMode from USE_ORIGIN_HEADERS to CACHE_ALL_STATIC",
			sp: newServicePort().setProp(func(cdn *bcnf.CDNConfig) {
				cdn.CacheMode = &cacheAllStatic
			}).build(),
			be: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.CacheMode = useOriginHeaders
				cdn.ClientTtl = 0
				cdn.DefaultTtl = 0
				cdn.MaxTtl = 0
			}).build(),
			updateExpected: true,
			beAfter: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.CacheMode = cacheAllStatic
			}).build(),
		},
		{
			desc: "cacheMode from CACHE_ALL_STATIC to FORCE_CACHE_ALL",
			sp: newServicePort().setProp(func(cdn *bcnf.CDNConfig) {
				cdn.CacheMode = &forceCacheAll
			}).build(),
			be: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.CacheMode = cacheAllStatic
			}).build(),
			updateExpected: true,
			beAfter: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.CacheMode = forceCacheAll
				cdn.MaxTtl = 0
			}).build(),
		},
		{
			desc: "cacheMode from FORCE_CACHE_ALL to CACHE_ALL_STATIC",
			sp: newServicePort().setProp(func(cdn *bcnf.CDNConfig) {
				cdn.CacheMode = &cacheAllStatic
			}).build(),
			be: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.CacheMode = forceCacheAll
				cdn.MaxTtl = 0
			}).build(),
			updateExpected: true,
			beAfter: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.CacheMode = cacheAllStatic
			}).build(),
		},
		{
			desc: "cacheMode from USE_ORIGIN_HEADERS to FORCE_CACHE_ALL",
			sp: newServicePort().setProp(func(cdn *bcnf.CDNConfig) {
				cdn.CacheMode = &forceCacheAll
			}).build(),
			be: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.CacheMode = useOriginHeaders
				cdn.ClientTtl = 0
				cdn.DefaultTtl = 0
				cdn.MaxTtl = 0
			}).build(),
			updateExpected: true,
			beAfter: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.CacheMode = forceCacheAll
				cdn.MaxTtl = 0
			}).build(),
		},
		{
			desc: "cacheMode from FORCE_CACHE_ALL to USE_ORIGIN_HEADERS",
			sp: newServicePort().setProp(func(cdn *bcnf.CDNConfig) {
				cdn.CacheMode = &useOriginHeaders
			}).build(),
			be: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.CacheMode = forceCacheAll
				cdn.MaxTtl = 0
			}).build(),
			updateExpected: true,
			beAfter: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.CacheMode = useOriginHeaders
				cdn.MaxTtl = 0
				cdn.ClientTtl = 0
				cdn.DefaultTtl = 0
			}).build(),
		},
		{
			desc: "cacheMode when USE_ORIGIN_HEADERS, ignore ttl settings",
			sp: newServicePort().setProp(func(cdn *bcnf.CDNConfig) {
				cdn.CacheMode = &useOriginHeaders
				cdn.ClientTtl = createInt64(3600)
				cdn.DefaultTtl = createInt64(3600)
				cdn.MaxTtl = createInt64(3600)
			}).build(),
			be: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.CacheMode = cacheAllStatic
			}).build(),
			updateExpected: true,
			beAfter: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.CacheMode = useOriginHeaders
				cdn.ClientTtl = 0
				cdn.DefaultTtl = 0
				cdn.MaxTtl = 0
			}).build(),
		},
		{
			desc: "cacheMode when USE_ORIGIN_HEADERS, ignore ttl settings",
			sp: newServicePort().setProp(func(cdn *bcnf.CDNConfig) {
				cdn.CacheMode = &useOriginHeaders
				cdn.ClientTtl = createInt64(3600)
				cdn.DefaultTtl = createInt64(3600)
				cdn.MaxTtl = createInt64(3600)
			}).build(),
			be: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.CacheMode = cacheAllStatic
			}).build(),
			updateExpected: true,
			beAfter: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.CacheMode = useOriginHeaders
				cdn.ClientTtl = 0
				cdn.DefaultTtl = 0
				cdn.MaxTtl = 0
			}).build(),
		},
		{
			desc: "cacheMode when CACHE_ALL_STATIC, ttl equals zero",
			sp: newServicePort().setProp(func(cdn *bcnf.CDNConfig) {
				cdn.CacheMode = &cacheAllStatic
				cdn.ClientTtl = createInt64(0)
				cdn.DefaultTtl = createInt64(0)
				cdn.MaxTtl = createInt64(0)
			}).build(),
			be: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.CacheMode = useOriginHeaders
			}).build(),
			updateExpected: true,
			beAfter: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.CacheMode = cacheAllStatic
				cdn.ClientTtl = 0
				cdn.DefaultTtl = 0
				cdn.MaxTtl = 0
				cdn.ForceSendFields = []string{"MaxTtl", "ClientTtl", "DefaultTtl"}
			}).build(),
		},
		{
			desc: "cacheMode when FORCE_CACHE_ALL, ttl equals zero",
			sp: newServicePort().setProp(func(cdn *bcnf.CDNConfig) {
				cdn.CacheMode = &forceCacheAll
				cdn.ClientTtl = createInt64(0)
				cdn.DefaultTtl = createInt64(0)
			}).build(),
			be: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.CacheMode = cacheAllStatic
			}).build(),
			updateExpected: true,
			beAfter: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.CacheMode = forceCacheAll
				cdn.ClientTtl = 0
				cdn.DefaultTtl = 0
				cdn.MaxTtl = 0
				cdn.ForceSendFields = []string{"ClientTtl", "DefaultTtl"}
			}).build(),
		},
		{
			desc: "when not specified, restore to default",
			sp: newServicePort().setProp(func(cdn *bcnf.CDNConfig) {
			}).build(),
			be: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.CacheMode = useOriginHeaders
			}).build(),
			updateExpected: true,
			beAfter: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
			}).build(),
		},
	}

	evaluateTestCases(t, testCases)
}

func TestEnsureCDNCacheKeyPolicy(t *testing.T) {

	testCases := []testCase{
		{
			desc: "nil CacheKeyPolicy, update needed",
			sp: newServicePort().setProp(func(cdn *bcnf.CDNConfig) {
				cdn.CachePolicy = &bcnf.CacheKeyPolicy{IncludeHost: true, IncludeProtocol: true, IncludeQueryString: true}
			}).build(),
			be: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.CacheKeyPolicy = nil
			}).build(),
			updateExpected: true,
			beAfter:        newDefaultBackendService().build(),
		},
		{
			desc: "update IncludeHost value",
			sp: newServicePort().setProp(func(cdn *bcnf.CDNConfig) {
				cdn.CachePolicy = &bcnf.CacheKeyPolicy{IncludeHost: false, IncludeProtocol: true, IncludeQueryString: true}
			}).build(),
			be:             newDefaultBackendService().build(),
			updateExpected: true,
			beAfter: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.CacheKeyPolicy.IncludeHost = false
			}).build(),
		},
		{
			desc: "update IncludeProtocol value",
			sp: newServicePort().setProp(func(cdn *bcnf.CDNConfig) {
				cdn.CachePolicy = &bcnf.CacheKeyPolicy{IncludeHost: true, IncludeProtocol: false, IncludeQueryString: true}
			}).build(),
			be:             newDefaultBackendService().build(),
			updateExpected: true,
			beAfter: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.CacheKeyPolicy.IncludeProtocol = false
			}).build(),
		},
		{
			desc: "update IncludeQueryString value",
			sp: newServicePort().setProp(func(cdn *bcnf.CDNConfig) {
				cdn.CachePolicy = &bcnf.CacheKeyPolicy{IncludeHost: true, IncludeProtocol: true, IncludeQueryString: false}
			}).build(),
			be:             newDefaultBackendService().build(),
			updateExpected: true,
			beAfter: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.CacheKeyPolicy.IncludeQueryString = false
			}).build(),
		},
		{
			desc: "update QueryStringWhitelist empty slice", // queryStringWhitelist: []
			sp: newServicePort().setProp(func(cdn *bcnf.CDNConfig) {
				cdn.CachePolicy = &bcnf.CacheKeyPolicy{IncludeHost: true, IncludeProtocol: true, IncludeQueryString: true,
					QueryStringWhitelist: []string{}}
			}).build(),
			be:             newDefaultBackendService().build(),
			updateExpected: false,
			beAfter:        newDefaultBackendService().build(),
		},
		{
			desc: "update QueryStringWhitelist value",
			sp: newServicePort().setProp(func(cdn *bcnf.CDNConfig) {
				cdn.CachePolicy = &bcnf.CacheKeyPolicy{IncludeHost: true, IncludeProtocol: true, IncludeQueryString: true,
					QueryStringWhitelist: []string{"value1"}}
			}).build(),
			be:             newDefaultBackendService().build(),
			updateExpected: true,
			beAfter: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.CacheKeyPolicy.QueryStringWhitelist = []string{"value1"}
			}).build(),
		},
		{
			desc: "update QueryStringWhitelist,ignore for IncludeQueryString false",
			sp: newServicePort().setProp(func(cdn *bcnf.CDNConfig) {
				cdn.CachePolicy = &bcnf.CacheKeyPolicy{IncludeHost: true, IncludeProtocol: true,
					IncludeQueryString:   false,
					QueryStringWhitelist: []string{"value1"}}
			}).build(),
			be:             newDefaultBackendService().build(),
			updateExpected: true,
			beAfter: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.CacheKeyPolicy.IncludeQueryString = false
				cdn.CacheKeyPolicy.QueryStringWhitelist = nil
			}).build(),
		},
		{
			desc: "update QueryStringWhitelist,ignore for IncludeQueryString false, clear existing",
			sp: newServicePort().setProp(func(cdn *bcnf.CDNConfig) {
				cdn.CachePolicy = &bcnf.CacheKeyPolicy{IncludeHost: true, IncludeProtocol: true,
					IncludeQueryString:   false,
					QueryStringWhitelist: []string{"value1"}}
			}).build(),
			be: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.CacheKeyPolicy.IncludeQueryString = true
				cdn.CacheKeyPolicy.QueryStringWhitelist = []string{"value1"}
			}).build(),
			updateExpected: true,
			beAfter: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.CacheKeyPolicy.IncludeQueryString = false
				cdn.CacheKeyPolicy.QueryStringWhitelist = nil
			}).build(),
		},
		{
			desc: "update QueryStringBlacklist empty slice", // queryStringBlacklist: []
			sp: newServicePort().setProp(func(cdn *bcnf.CDNConfig) {
				cdn.CachePolicy = &bcnf.CacheKeyPolicy{IncludeHost: true, IncludeProtocol: true, IncludeQueryString: true,
					QueryStringBlacklist: []string{}}
			}).build(),
			be:             newDefaultBackendService().build(),
			updateExpected: false,
			beAfter:        newDefaultBackendService().build(),
		},
		{
			desc: "update QueryStringBlacklist value",
			sp: newServicePort().setProp(func(cdn *bcnf.CDNConfig) {
				cdn.CachePolicy = &bcnf.CacheKeyPolicy{IncludeHost: true, IncludeProtocol: true, IncludeQueryString: true,
					QueryStringBlacklist: []string{"value1"}}
			}).build(),
			be:             newDefaultBackendService().build(),
			updateExpected: true,
			beAfter: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.CacheKeyPolicy.QueryStringBlacklist = []string{"value1"}
			}).build(),
		},
		{
			desc: "update QueryStringBlacklist,ignore for IncludeQueryString false",
			sp: newServicePort().setProp(func(cdn *bcnf.CDNConfig) {
				cdn.CachePolicy = &bcnf.CacheKeyPolicy{IncludeHost: true, IncludeProtocol: true, IncludeQueryString: false,
					QueryStringBlacklist: []string{"value1"}}
			}).build(),
			be:             newDefaultBackendService().build(),
			updateExpected: true,
			beAfter: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.CacheKeyPolicy.IncludeQueryString = false
				cdn.CacheKeyPolicy.QueryStringBlacklist = nil
			}).build(),
		},
		{
			desc: "update QueryStringBlacklist,ignore for IncludeQueryString false, clear existing",
			sp: newServicePort().setProp(func(cdn *bcnf.CDNConfig) {
				cdn.CachePolicy = &bcnf.CacheKeyPolicy{IncludeHost: true, IncludeProtocol: true, IncludeQueryString: false,
					QueryStringBlacklist: []string{"value1"}}
			}).build(),
			be: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.CacheKeyPolicy.IncludeQueryString = true
				cdn.CacheKeyPolicy.QueryStringBlacklist = []string{"value1"}
			}).build(),
			updateExpected: true,
			beAfter: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.CacheKeyPolicy.IncludeQueryString = false
				cdn.CacheKeyPolicy.QueryStringBlacklist = nil
			}).build(),
		},
		{
			desc: "ignore for IncludeQueryString false",
			sp: newServicePort().setProp(func(cdn *bcnf.CDNConfig) {
				cdn.CachePolicy = &bcnf.CacheKeyPolicy{IncludeHost: true, IncludeProtocol: true, IncludeQueryString: false,
					QueryStringBlacklist: []string{"value1"},
					QueryStringWhitelist: []string{"value2"}}
			}).build(),
			be: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.CacheKeyPolicy.IncludeQueryString = false
			}).build(),
			updateExpected: false,
			beAfter: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.CacheKeyPolicy.IncludeQueryString = false
			}).build(),
		},
		{
			desc: "when not specified, restore to default",
			sp:   newServicePort().build(),
			be: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.CacheKeyPolicy.IncludeHost = false
				cdn.CacheKeyPolicy.IncludeProtocol = false
				cdn.CacheKeyPolicy.IncludeQueryString = false
				cdn.CacheKeyPolicy.QueryStringBlacklist = []string{"query1"}
				cdn.CacheKeyPolicy.QueryStringBlacklist = []string{"query1"}
			}).build(),
			updateExpected: true,
			beAfter:        newDefaultBackendService().build(),
		},
	}

	evaluateTestCases(t, testCases)
}

func TestEnsureCDNTtl(t *testing.T) {

	testCases := []testCase{
		{
			desc: "update ClientTtl value",
			sp: newServicePort().setProp(func(cdn *bcnf.CDNConfig) {
				cdn.ClientTtl = createInt64(1)
			}).build(),
			be: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.ClientTtl = 2
			}).build(),
			updateExpected: true,
			beAfter: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.ClientTtl = 1
			}).build(),
		},
		{
			desc: "update DefaultTtl value",
			sp: newServicePort().setProp(func(cdn *bcnf.CDNConfig) {
				cdn.DefaultTtl = createInt64(1)
			}).build(),
			be: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.DefaultTtl = 2
			}).build(),
			updateExpected: true,
			beAfter: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.DefaultTtl = 1
			}).build(),
		},
		{
			desc: "update MaxTtl value",
			sp: newServicePort().setProp(func(cdn *bcnf.CDNConfig) {
				cdn.MaxTtl = createInt64(1)
			}).build(),
			be: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.MaxTtl = 2
			}).build(),
			updateExpected: true,
			beAfter: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.MaxTtl = 1
			}).build(),
		},
		{
			desc: "when not specified, restore to default",
			sp:   newServicePort().build(),
			be: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.ClientTtl = 1
				cdn.DefaultTtl = 1
				cdn.MaxTtl = 1
			}).build(),
			updateExpected: true,
			beAfter:        newDefaultBackendService().build(),
		},
	}

	evaluateTestCases(t, testCases)
}

func TestEnsureCDNNegativeCaching(t *testing.T) {

	testCases := []testCase{
		{
			desc: "update NegativeCaching value false",
			sp: newServicePort().setProp(func(cdn *bcnf.CDNConfig) {
				cdn.NegativeCaching = createBool(false)
			}).build(),
			be:             newDefaultBackendService().build(),
			updateExpected: true,
			beAfter: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.NegativeCaching = false
			}).build(),
		},
		{
			desc: "update NegativeCaching value false,ignore NegativeCachingPolicy",
			sp: newServicePort().setProp(func(cdn *bcnf.CDNConfig) {
				cdn.NegativeCaching = createBool(false)
				cdn.NegativeCachingPolicy = []*bcnf.NegativeCachingPolicy{
					{Code: 302, Ttl: 1800},
				}
			}).build(),
			be:             newDefaultBackendService().build(),
			updateExpected: true,
			beAfter: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.NegativeCaching = false
			}).build(),
		},
		{
			desc: "update NegativeCaching value false,clear NegativeCachingPolicy",
			sp: newServicePort().setProp(func(cdn *bcnf.CDNConfig) {
				cdn.NegativeCaching = createBool(false)
				cdn.NegativeCachingPolicy = []*bcnf.NegativeCachingPolicy{
					{Code: 302, Ttl: 1800},
				}
			}).build(),
			be: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.NegativeCaching = true
				cdn.NegativeCachingPolicy = []*composite.BackendServiceCdnPolicyNegativeCachingPolicy{
					{Code: 301, Ttl: 0},
				}
			}).build(),
			updateExpected: true,
			beAfter: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.NegativeCaching = false
			}).build(),
		},
		{
			desc: "update NegativeCaching value true",
			sp: newServicePort().setProp(func(cdn *bcnf.CDNConfig) {
				cdn.NegativeCaching = createBool(true)
			}).build(),
			be: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.NegativeCaching = false
			}).build(),
			updateExpected: true,
			beAfter: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.NegativeCaching = true
			}).build(),
		},
		{
			desc: "update NegativeCachingPolicy value",
			sp: newServicePort().setProp(func(cdn *bcnf.CDNConfig) {
				cdn.NegativeCachingPolicy = []*bcnf.NegativeCachingPolicy{
					{Code: 302, Ttl: 1800},
				}
			}).build(),
			be: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.NegativeCachingPolicy = []*composite.BackendServiceCdnPolicyNegativeCachingPolicy{
					{Code: 301, Ttl: 0},
				}
			}).build(),
			updateExpected: true,
			beAfter: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.NegativeCachingPolicy = []*composite.BackendServiceCdnPolicyNegativeCachingPolicy{
					{Code: 302, Ttl: 1800},
				}
			}).build(),
		},
		{
			desc: "update NegativeCachingPolicy value empty array, update needed", //negativeCachingPolicy:[]
			sp: newServicePort().setProp(func(cdn *bcnf.CDNConfig) {
				cdn.NegativeCachingPolicy = []*bcnf.NegativeCachingPolicy{}
			}).build(),
			be: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.NegativeCachingPolicy = []*composite.BackendServiceCdnPolicyNegativeCachingPolicy{
					{Code: 301, Ttl: 1800},
				}
			}).build(),
			updateExpected: true,
			beAfter: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.NegativeCachingPolicy = nil
			}).build(),
		},
		{
			desc: "update NegativeCachingPolicy value empty array, update not needed", //negativeCachingPolicy:[]
			sp: newServicePort().setProp(func(cdn *bcnf.CDNConfig) {
				cdn.NegativeCachingPolicy = []*bcnf.NegativeCachingPolicy{}
			}).build(),
			be:             newDefaultBackendService().build(),
			updateExpected: false,
			beAfter:        newDefaultBackendService().build(),
		},
		{
			desc: "when not specified, restore to defaults, clear policy",
			sp:   newServicePort().build(),
			be: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.NegativeCachingPolicy = []*composite.BackendServiceCdnPolicyNegativeCachingPolicy{
					{Code: 301, Ttl: 1800},
				}
			}).build(),
			updateExpected: true,
			beAfter:        newDefaultBackendService().build(),
		},
		{
			desc: "when not specified, restore to defaults",
			sp:   newServicePort().build(),
			be: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.NegativeCaching = false
			}).build(),
			updateExpected: true,
			beAfter:        newDefaultBackendService().build(),
		},
		{
			desc: "same values different order, no update",
			sp: newServicePort().setProp(func(cdn *bcnf.CDNConfig) {
				cdn.NegativeCachingPolicy = []*bcnf.NegativeCachingPolicy{
					{Code: 404, Ttl: 600},
					{Code: 301, Ttl: 1800},
				}
			}).build(),
			be: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.NegativeCachingPolicy = []*composite.BackendServiceCdnPolicyNegativeCachingPolicy{
					{Code: 301, Ttl: 1800},
					{Code: 404, Ttl: 600},
				}
			}).build(),
			updateExpected: false,
			beAfter: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.NegativeCachingPolicy = []*composite.BackendServiceCdnPolicyNegativeCachingPolicy{
					{Code: 301, Ttl: 1800},
					{Code: 404, Ttl: 600},
				}
			}).build(),
		},
	}

	evaluateTestCases(t, testCases)
}

func TestEnsureCDNSignedUrl(t *testing.T) {

	testCases := []testCase{
		{
			desc: "update SignedUrlCacheMaxAgeSec value",
			sp: newServicePort().setProp(func(cdn *bcnf.CDNConfig) {
				cdn.SignedUrlCacheMaxAgeSec = createInt64(1800)
			}).build(),
			be: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.SignedUrlCacheMaxAgeSec = 3600
			}).build(),
			updateExpected: true,
			beAfter: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.SignedUrlCacheMaxAgeSec = 1800
			}).build(),
		},
		{
			desc: "when not specified, restore to default",
			sp:   newServicePort().build(),
			be: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.SignedUrlCacheMaxAgeSec = 3600
			}).build(),
			updateExpected: true,
			beAfter:        newDefaultBackendService().build(),
		},
	}

	evaluateTestCases(t, testCases)
}

func TestEnsureCDNServeWhileStale(t *testing.T) {

	testCases := []testCase{
		{
			desc: "update ServeWhileStale value",
			sp: newServicePort().setProp(func(cdn *bcnf.CDNConfig) {
				cdn.ServeWhileStale = createInt64(3600)
			}).build(),
			be: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.ServeWhileStale = 1800
			}).build(),
			updateExpected: true,
			beAfter: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.ServeWhileStale = 3600
			}).build(),
		},
		{
			desc: "when not specified, restore to default",
			sp:   newServicePort().build(),
			be: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.ServeWhileStale = 3600
			}).build(),
			updateExpected: true,
			beAfter:        newDefaultBackendService().build(),
		},
	}

	evaluateTestCases(t, testCases)
}

func TestEnsureCDNRequestCoalescing(t *testing.T) {

	testCases := []testCase{
		{
			desc: "update RequestCoalescing value, false",
			sp: newServicePort().setProp(func(cdn *bcnf.CDNConfig) {
				cdn.RequestCoalescing = createBool(false)
			}).build(),
			be: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.RequestCoalescing = true
			}).build(),
			updateExpected: true,
			beAfter: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.RequestCoalescing = false
			}).build(),
		},
		{
			desc: "update RequestCoalescing value, true",
			sp: newServicePort().setProp(func(cdn *bcnf.CDNConfig) {
				cdn.RequestCoalescing = createBool(true)
			}).build(),
			be: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.RequestCoalescing = false
			}).build(),
			updateExpected: true,
			beAfter: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.RequestCoalescing = true
			}).build(),
		},
		{
			desc: "when not specified, restore to default",
			sp:   newServicePort().build(),
			be: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.RequestCoalescing = false
			}).build(),
			updateExpected: true,
			beAfter:        newDefaultBackendService().build(),
		},
	}

	evaluateTestCases(t, testCases)
}

func TestEnsureCDNBypassCacheOnRequestHeaders(t *testing.T) {

	testCases := []testCase{
		{
			desc: "update NegativeCachingPolicy value",
			sp: newServicePort().setProp(func(cdn *bcnf.CDNConfig) {
				cdn.BypassCacheOnRequestHeaders = []*bcnf.BypassCacheOnRequestHeader{
					{HeaderName: "header1"},
				}
			}).build(),
			be: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.BypassCacheOnRequestHeaders = []*composite.BackendServiceCdnPolicyBypassCacheOnRequestHeader{
					{HeaderName: "header2"},
				}
			}).build(),
			updateExpected: true,
			beAfter: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.BypassCacheOnRequestHeaders = []*composite.BackendServiceCdnPolicyBypassCacheOnRequestHeader{
					{HeaderName: "header1"},
				}
			}).build(),
		},
		{
			desc: "update NegativeCachingPolicy value empty array, update needed", //negativeCachingPolicy:[]
			sp: newServicePort().setProp(func(cdn *bcnf.CDNConfig) {
				cdn.BypassCacheOnRequestHeaders = []*bcnf.BypassCacheOnRequestHeader{}
			}).build(),
			be: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.BypassCacheOnRequestHeaders = []*composite.BackendServiceCdnPolicyBypassCacheOnRequestHeader{
					{HeaderName: "header"},
				}
			}).build(),
			updateExpected: true,
			beAfter: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.BypassCacheOnRequestHeaders = nil
			}).build(),
		},
		{
			desc: "update BypassCacheOnRequestHeaders value empty array, update not needed", //negativeCachingPolicy:[]
			sp: newServicePort().setProp(func(cdn *bcnf.CDNConfig) {
				cdn.BypassCacheOnRequestHeaders = []*bcnf.BypassCacheOnRequestHeader{}
			}).build(),
			be:             newDefaultBackendService().build(),
			updateExpected: false,
			beAfter:        newDefaultBackendService().build(),
		},
		{
			desc: "when not specified, restore to default",
			sp:   newServicePort().build(),
			be: newDefaultBackendService().setProp(func(cdn *composite.BackendServiceCdnPolicy) {
				cdn.BypassCacheOnRequestHeaders = []*composite.BackendServiceCdnPolicyBypassCacheOnRequestHeader{
					{HeaderName: "header"},
				}
			}).build(),
			updateExpected: true,
			beAfter:        newDefaultBackendService().build(),
		},
	}

	evaluateTestCases(t, testCases)
}
