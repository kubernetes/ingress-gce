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

package features

import (
	"reflect"
	"sort"
	"strings"

	"github.com/kr/pretty"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog"
)

// This values are copied from GCP Load balancer
var defaultCdnPolicy = composite.BackendServiceCdnPolicy{
	CacheKeyPolicy:          &composite.CacheKeyPolicy{IncludeHost: true, IncludeProtocol: true, IncludeQueryString: true},
	CacheMode:               "CACHE_ALL_STATIC",
	ClientTtl:               3600,
	DefaultTtl:              3600,
	MaxTtl:                  86400,
	NegativeCaching:         true,
	RequestCoalescing:       true,
	ServeWhileStale:         86400,
	SignedUrlCacheMaxAgeSec: 0,
}

// EnsureCDN reads the CDN configuration specified in the ServicePort.BackendConfig
// and applies it to the BackendService. It returns true if there were existing
// settings on the BackendService that were overwritten.
func EnsureCDN(sp utils.ServicePort, be *composite.BackendService) bool {
	if sp.BackendConfig.Spec.Cdn == nil {
		return false
	}
	klog.V(3).Infof("BackendConfig.Spec.Cdn before merge = %s", prettyPrint(sp.BackendConfig.Spec.Cdn))
	klog.V(3).Infof("CdnPolicy before merge = %+v", prettyPrint(be.CdnPolicy))
	changed := applyCDNSettings(sp, be)
	klog.V(3).Infof("CdnPolicy after merge = %+v", prettyPrint(be.CdnPolicy))
	if changed {
		klog.V(2).Infof("Updated CDN settings for service %v/%v.", sp.ID.Service.Namespace, sp.ID.Service.Name)
	}
	return changed
}

func prettyPrint(a interface{}) string {
	value := pretty.Sprint(a)
	return strings.ReplaceAll(value, "\n", "")
}

func sliceEqual(a, b []string) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	return reflect.DeepEqual(a, b)
}

type negativeCachePolicySortInterface []*composite.BackendServiceCdnPolicyNegativeCachingPolicy

func (l negativeCachePolicySortInterface) Len() int           { return len(l) }
func (l negativeCachePolicySortInterface) Less(i, j int) bool { return l[i].Code < l[j].Code }
func (l negativeCachePolicySortInterface) Swap(i, j int)      { l[i], l[j] = l[j], l[i] }

func negativeCachingPolicyEqual(x, y []*composite.BackendServiceCdnPolicyNegativeCachingPolicy) bool {

	xSorted := negativeCachePolicySortInterface(append([]*composite.BackendServiceCdnPolicyNegativeCachingPolicy{}, x...))
	ySorted := negativeCachePolicySortInterface(append([]*composite.BackendServiceCdnPolicyNegativeCachingPolicy{}, y...))
	sort.Sort(xSorted)
	sort.Sort(ySorted)

	return reflect.DeepEqual(xSorted, ySorted)
}

// applyCDNSettings applies the CDN settings specified in the BackendConfig
// to the passed in compute.BackendService. A GCE API call still needs to be
// made to actually persist the changes.
func applyCDNSettings(sp utils.ServicePort, be *composite.BackendService) bool {
	cdnConfig := sp.BackendConfig.Spec.Cdn
	changed := be.EnableCDN != cdnConfig.Enabled

	if !cdnConfig.Enabled {
		be.EnableCDN = false
		return changed
	}

	be.EnableCDN = true
	if be.CdnPolicy == nil {
		be.CdnPolicy = &composite.BackendServiceCdnPolicy{}
		klog.V(3).Info("CdnPolicy not found, assign new BackendServiceCdnPolicy")
		changed = true
	}

	if cdnConfig.CachePolicy != nil {
		cacheKeyPolicy := cdnConfig.CachePolicy
		beCacheKeyPolicy := be.CdnPolicy.CacheKeyPolicy
		if beCacheKeyPolicy == nil {
			klog.V(3).Info("CdnPolicy.CacheKeyPolicy not found, assign new CacheKeyPolicy")
			beCacheKeyPolicy = &composite.CacheKeyPolicy{}
			be.CdnPolicy.CacheKeyPolicy = beCacheKeyPolicy
			changed = true
		}
		if beCacheKeyPolicy.IncludeHost != cacheKeyPolicy.IncludeHost {
			klog.V(3).Infof("CdnPolicy.CacheKeyPolicy.IncludeHost property will be updated from %v to %v", beCacheKeyPolicy.IncludeHost, cacheKeyPolicy.IncludeHost)
			beCacheKeyPolicy.IncludeHost = cacheKeyPolicy.IncludeHost
			changed = true
		}
		if beCacheKeyPolicy.IncludeProtocol != cacheKeyPolicy.IncludeProtocol {
			klog.V(3).Infof("CdnPolicy.CacheKeyPolicy.IncludeProtocol property will be updated from %v to %v", beCacheKeyPolicy.IncludeProtocol, cacheKeyPolicy.IncludeProtocol)
			beCacheKeyPolicy.IncludeProtocol = cacheKeyPolicy.IncludeProtocol
			changed = true
		}
		if beCacheKeyPolicy.IncludeQueryString != cacheKeyPolicy.IncludeQueryString {
			klog.V(3).Infof("CdnPolicy.CacheKeyPolicy.IncludeQueryString property will be updated from %v to %v", beCacheKeyPolicy.IncludeQueryString, cacheKeyPolicy.IncludeQueryString)
			beCacheKeyPolicy.IncludeQueryString = cacheKeyPolicy.IncludeQueryString
			changed = true
		}

		var queryStringBlacklist []string
		if beCacheKeyPolicy.IncludeQueryString {
			queryStringBlacklist = cacheKeyPolicy.QueryStringBlacklist
		}
		if !sliceEqual(queryStringBlacklist, beCacheKeyPolicy.QueryStringBlacklist) {
			klog.V(3).Infof("CdnPolicy.CacheKeyPolicy.QueryStringBlacklist property will be updated from %v to %v", beCacheKeyPolicy.QueryStringBlacklist, queryStringBlacklist)
			beCacheKeyPolicy.QueryStringBlacklist = queryStringBlacklist
			changed = true
		}

		var queryStringWhitelist []string
		if beCacheKeyPolicy.IncludeQueryString {
			queryStringWhitelist = cacheKeyPolicy.QueryStringWhitelist
		}
		if !sliceEqual(queryStringWhitelist, beCacheKeyPolicy.QueryStringWhitelist) {
			klog.V(3).Infof("CdnPolicy.CacheKeyPolicy.QueryStringWhitelist property will be updated from %v to %v", beCacheKeyPolicy.QueryStringWhitelist, queryStringWhitelist)
			beCacheKeyPolicy.QueryStringWhitelist = queryStringWhitelist
			changed = true
		}
	} else if !reflect.DeepEqual(be.CdnPolicy.CacheKeyPolicy, defaultCdnPolicy.CacheKeyPolicy) {
		klog.V(3).Infof("CdnPolicy.CacheKeyPolicy not found on BackendConfig, reset value to default  %+v", defaultCdnPolicy.CacheKeyPolicy)
		be.CdnPolicy.CacheKeyPolicy = defaultCdnPolicy.CacheKeyPolicy
		changed = true
	}

	if cdnConfig.CacheMode != nil {
		if be.CdnPolicy.CacheMode != *cdnConfig.CacheMode {
			klog.V(3).Infof("CdnPolicy.CacheMode property will be updated from %v to %v", be.CdnPolicy.CacheMode, *cdnConfig.CacheMode)
			be.CdnPolicy.CacheMode = *cdnConfig.CacheMode
			changed = true
		}
	} else if be.CdnPolicy.CacheMode != defaultCdnPolicy.CacheMode {
		klog.V(3).Infof("CdnPolicy.CacheMode not found on BackendConfig, reset value to default  %+v", defaultCdnPolicy.CacheMode)
		be.CdnPolicy.CacheMode = defaultCdnPolicy.CacheMode
		changed = true
	}

	if cdnConfig.RequestCoalescing != nil {
		if be.CdnPolicy.RequestCoalescing != *cdnConfig.RequestCoalescing {
			klog.V(3).Infof("CdnPolicy.RequestCoalescing property will be updated from %v to %v", be.CdnPolicy.RequestCoalescing, *cdnConfig.RequestCoalescing)
			be.CdnPolicy.RequestCoalescing = *cdnConfig.RequestCoalescing
			changed = true
		}
	} else if be.CdnPolicy.RequestCoalescing != defaultCdnPolicy.RequestCoalescing {
		klog.V(3).Infof("CdnPolicy.RequestCoalescing not found on BackendConfig, reset value to default  %+v", defaultCdnPolicy.RequestCoalescing)
		be.CdnPolicy.RequestCoalescing = defaultCdnPolicy.RequestCoalescing
		changed = true
	}

	if cdnConfig.ServeWhileStale != nil {
		if be.CdnPolicy.ServeWhileStale != *cdnConfig.ServeWhileStale {
			klog.V(3).Infof("CdnPolicy.ServeWhileStale property will be updated from %v to %v", be.CdnPolicy.ServeWhileStale, *cdnConfig.ServeWhileStale)
			be.CdnPolicy.ServeWhileStale = *cdnConfig.ServeWhileStale
			changed = true
		}
	} else if be.CdnPolicy.ServeWhileStale != defaultCdnPolicy.ServeWhileStale {
		klog.V(3).Infof("CdnPolicy.ServeWhileStale not found on BackendConfig, reset value to default  %+v", defaultCdnPolicy.ServeWhileStale)
		be.CdnPolicy.ServeWhileStale = defaultCdnPolicy.ServeWhileStale
		changed = true
	}

	// MaxTtl must be specified with CACHE_ALL_STATIC cache_mode only
	if be.CdnPolicy.CacheMode != "CACHE_ALL_STATIC" {
		be.CdnPolicy.MaxTtl = 0
	} else if cdnConfig.MaxTtl != nil {
		if be.CdnPolicy.MaxTtl != *cdnConfig.MaxTtl {
			klog.V(3).Infof("CdnPolicy.MaxTtl property will be updated from %v to %v", be.CdnPolicy.MaxTtl, *cdnConfig.MaxTtl)
			be.CdnPolicy.MaxTtl = *cdnConfig.MaxTtl
			if be.CdnPolicy.MaxTtl == 0 {
				be.CdnPolicy.ForceSendFields = append(be.CdnPolicy.ForceSendFields, "MaxTtl")
			}
			changed = true
		}
	} else if be.CdnPolicy.MaxTtl != defaultCdnPolicy.MaxTtl {
		klog.V(3).Infof("CdnPolicy.MaxTtl not found on BackendConfig, reset value to default  %+v", defaultCdnPolicy.MaxTtl)
		be.CdnPolicy.MaxTtl = defaultCdnPolicy.MaxTtl
		changed = true
	}

	// if USE_ORIGIN_HEADERS ClientTtl must be ignored
	if be.CdnPolicy.CacheMode == "USE_ORIGIN_HEADERS" {
		be.CdnPolicy.ClientTtl = 0
	} else if cdnConfig.ClientTtl != nil {
		if be.CdnPolicy.ClientTtl != *cdnConfig.ClientTtl {
			klog.V(3).Infof("CdnPolicy.ClientTtl property will be updated from %v to %v", be.CdnPolicy.ClientTtl, *cdnConfig.ClientTtl)
			be.CdnPolicy.ClientTtl = *cdnConfig.ClientTtl
			if be.CdnPolicy.ClientTtl == 0 {
				be.CdnPolicy.ForceSendFields = append(be.CdnPolicy.ForceSendFields, "ClientTtl")
			}
			changed = true
		}
	} else if be.CdnPolicy.ClientTtl != defaultCdnPolicy.ClientTtl {
		klog.V(3).Infof("CdnPolicy.ClientTtl not found on BackendConfig, reset value to default  %+v", defaultCdnPolicy.ClientTtl)
		be.CdnPolicy.ClientTtl = defaultCdnPolicy.ClientTtl
		changed = true
	}

	// if USE_ORIGIN_HEADERS DefaultTtl must be ignored
	if be.CdnPolicy.CacheMode == "USE_ORIGIN_HEADERS" {
		be.CdnPolicy.DefaultTtl = 0
	} else if cdnConfig.DefaultTtl != nil {
		if be.CdnPolicy.DefaultTtl != *cdnConfig.DefaultTtl {
			klog.V(3).Infof("CdnPolicy.DefaultTtl property will be updated from %v to %v", be.CdnPolicy.DefaultTtl, *cdnConfig.DefaultTtl)
			be.CdnPolicy.DefaultTtl = *cdnConfig.DefaultTtl
			if be.CdnPolicy.DefaultTtl == 0 {
				be.CdnPolicy.ForceSendFields = append(be.CdnPolicy.ForceSendFields, "DefaultTtl")
			}
			changed = true
		}
	} else if be.CdnPolicy.DefaultTtl != defaultCdnPolicy.DefaultTtl {
		klog.V(3).Infof("CdnPolicy.DefaultTtl not found on BackendConfig, reset value to default  %+v", defaultCdnPolicy.DefaultTtl)
		be.CdnPolicy.DefaultTtl = defaultCdnPolicy.DefaultTtl
		changed = true
	}

	if cdnConfig.NegativeCaching != nil {
		if be.CdnPolicy.NegativeCaching != *cdnConfig.NegativeCaching {
			klog.V(3).Infof("CdnPolicy.NegativeCaching property will be updated from %v to %v", be.CdnPolicy.NegativeCaching, *cdnConfig.NegativeCaching)
			be.CdnPolicy.NegativeCaching = *cdnConfig.NegativeCaching
			changed = true
		}
	} else if be.CdnPolicy.NegativeCaching != defaultCdnPolicy.NegativeCaching {
		klog.V(3).Infof("CdnPolicy.NegativeCaching not found on BackendConfig, reset value to default  %+v", defaultCdnPolicy.NegativeCaching)
		be.CdnPolicy.NegativeCaching = defaultCdnPolicy.NegativeCaching
		changed = true
	}

	negativeCachingPolicy := []*composite.BackendServiceCdnPolicyNegativeCachingPolicy{}
	if be.CdnPolicy.NegativeCaching {
		for _, policyRef := range cdnConfig.NegativeCachingPolicy {
			negativeCachingPolicy = append(
				negativeCachingPolicy,
				&composite.BackendServiceCdnPolicyNegativeCachingPolicy{
					Code: policyRef.Code,
					Ttl:  policyRef.Ttl,
				},
			)
		}
	}
	if (len(negativeCachingPolicy) == 0 &&
		len(be.CdnPolicy.NegativeCachingPolicy) > 0) ||
		(len(negativeCachingPolicy) > 0 &&
			!negativeCachingPolicyEqual(be.CdnPolicy.NegativeCachingPolicy, negativeCachingPolicy)) {
		klog.V(3).Infof("CdnPolicy.NegativeCachingPolicy property will be updated from %s to %s", prettyPrint(be.CdnPolicy.NegativeCachingPolicy), prettyPrint(negativeCachingPolicy))
		if len(negativeCachingPolicy) == 0 {
			be.CdnPolicy.NegativeCachingPolicy = nil
		} else {
			be.CdnPolicy.NegativeCachingPolicy = negativeCachingPolicy
		}
		changed = true
	}

	if cdnConfig.SignedUrlCacheMaxAgeSec != nil {
		if be.CdnPolicy.SignedUrlCacheMaxAgeSec != *cdnConfig.SignedUrlCacheMaxAgeSec {
			klog.V(3).Infof("CdnPolicy.SignedUrlCacheMaxAgeSec property will be updated from %v to %v", be.CdnPolicy.SignedUrlCacheMaxAgeSec, *cdnConfig.SignedUrlCacheMaxAgeSec)
			be.CdnPolicy.SignedUrlCacheMaxAgeSec = *cdnConfig.SignedUrlCacheMaxAgeSec
			changed = true
		}
	} else if be.CdnPolicy.SignedUrlCacheMaxAgeSec != defaultCdnPolicy.SignedUrlCacheMaxAgeSec {
		klog.V(3).Infof("CdnPolicy.SignedUrlCacheMaxAgeSec not found on BackendConfig, reset value to default  %+v", defaultCdnPolicy.SignedUrlCacheMaxAgeSec)
		be.CdnPolicy.SignedUrlCacheMaxAgeSec = defaultCdnPolicy.SignedUrlCacheMaxAgeSec
		changed = true
	}

	bypassCacheOnRequestHeaders := []*composite.BackendServiceCdnPolicyBypassCacheOnRequestHeader{}
	for _, policyRef := range cdnConfig.BypassCacheOnRequestHeaders {
		bypassCacheOnRequestHeaders = append(
			bypassCacheOnRequestHeaders,
			&composite.BackendServiceCdnPolicyBypassCacheOnRequestHeader{
				HeaderName: policyRef.HeaderName,
			},
		)
	}
	if (len(bypassCacheOnRequestHeaders) == 0 &&
		len(be.CdnPolicy.BypassCacheOnRequestHeaders) > 0) ||
		(len(bypassCacheOnRequestHeaders) > 0 &&
			!reflect.DeepEqual(be.CdnPolicy.BypassCacheOnRequestHeaders, bypassCacheOnRequestHeaders)) {
		klog.V(3).Infof("CdnPolicy.BypassCacheOnRequestHeaders property will be updated from %s to %s", prettyPrint(be.CdnPolicy.BypassCacheOnRequestHeaders), prettyPrint(bypassCacheOnRequestHeaders))
		if len(bypassCacheOnRequestHeaders) == 0 {
			be.CdnPolicy.BypassCacheOnRequestHeaders = nil
		} else {
			be.CdnPolicy.BypassCacheOnRequestHeaders = bypassCacheOnRequestHeaders
		}
		changed = true
	}

	// changes for SignedUrlKeys are handled outside this module
	return changed
}
