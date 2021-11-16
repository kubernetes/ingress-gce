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
	newConfig := renderConfig(sp)
	if hasDiff(newConfig, be) {
		klog.V(2).Infof("Updated CDN settings for service %v/%v.", sp.ID.Service.Namespace, sp.ID.Service.Name)
		applyConfig(newConfig, be)
		return true
	}
	return false
}

func prettyPrint(a interface{}) string {
	value := pretty.Sprint(a)
	return strings.ReplaceAll(value, "\n", "")
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

func renderConfig(sp utils.ServicePort) *composite.BackendService {
	cdnConfig := sp.BackendConfig.Spec.Cdn
	be := &composite.BackendService{}
	be.EnableCDN = cdnConfig.Enabled

	if !be.EnableCDN {
		return be
	}

	be.CdnPolicy = &composite.BackendServiceCdnPolicy{}

	if cdnConfig.CachePolicy != nil {
		beCacheKeyPolicy := &composite.CacheKeyPolicy{}
		beCacheKeyPolicy.IncludeHost = cdnConfig.CachePolicy.IncludeHost
		beCacheKeyPolicy.IncludeProtocol = cdnConfig.CachePolicy.IncludeProtocol
		beCacheKeyPolicy.IncludeQueryString = cdnConfig.CachePolicy.IncludeQueryString
		if cdnConfig.CachePolicy.IncludeQueryString {
			if cdnConfig.CachePolicy.QueryStringBlacklist != nil && len(cdnConfig.CachePolicy.QueryStringBlacklist) > 0 {
				beCacheKeyPolicy.QueryStringBlacklist = cdnConfig.CachePolicy.QueryStringBlacklist
			}
			if cdnConfig.CachePolicy.QueryStringWhitelist != nil && len(cdnConfig.CachePolicy.QueryStringWhitelist) > 0 {
				beCacheKeyPolicy.QueryStringWhitelist = cdnConfig.CachePolicy.QueryStringWhitelist
			}
		}
		be.CdnPolicy.CacheKeyPolicy = beCacheKeyPolicy
	} else {
		be.CdnPolicy.CacheKeyPolicy = defaultCdnPolicy.CacheKeyPolicy
	}

	if cdnConfig.CacheMode != nil {
		be.CdnPolicy.CacheMode = *cdnConfig.CacheMode
	} else {
		be.CdnPolicy.CacheMode = defaultCdnPolicy.CacheMode
	}

	if cdnConfig.RequestCoalescing != nil {
		be.CdnPolicy.RequestCoalescing = *cdnConfig.RequestCoalescing
	} else {
		be.CdnPolicy.RequestCoalescing = defaultCdnPolicy.RequestCoalescing
	}

	if cdnConfig.ServeWhileStale != nil {
		be.CdnPolicy.ServeWhileStale = *cdnConfig.ServeWhileStale
	} else {
		be.CdnPolicy.ServeWhileStale = defaultCdnPolicy.ServeWhileStale
	}

	// MaxTtl must be specified with CACHE_ALL_STATIC cache_mode only
	if be.CdnPolicy.CacheMode != "CACHE_ALL_STATIC" {
		be.CdnPolicy.MaxTtl = 0
	} else if cdnConfig.MaxTtl != nil {
		be.CdnPolicy.MaxTtl = *cdnConfig.MaxTtl
		if be.CdnPolicy.MaxTtl == 0 {
			be.CdnPolicy.ForceSendFields = append(be.CdnPolicy.ForceSendFields, "MaxTtl")
		}
	} else {
		be.CdnPolicy.MaxTtl = defaultCdnPolicy.MaxTtl
	}

	// if USE_ORIGIN_HEADERS ClientTtl must be ignored
	if be.CdnPolicy.CacheMode == "USE_ORIGIN_HEADERS" {
		be.CdnPolicy.ClientTtl = 0
	} else if cdnConfig.ClientTtl != nil {
		be.CdnPolicy.ClientTtl = *cdnConfig.ClientTtl
		if be.CdnPolicy.ClientTtl == 0 {
			be.CdnPolicy.ForceSendFields = append(be.CdnPolicy.ForceSendFields, "ClientTtl")
		}
	} else {
		be.CdnPolicy.ClientTtl = defaultCdnPolicy.ClientTtl
	}

	// if USE_ORIGIN_HEADERS DefaultTtl must be ignored
	if be.CdnPolicy.CacheMode == "USE_ORIGIN_HEADERS" {
		be.CdnPolicy.DefaultTtl = 0
	} else if cdnConfig.DefaultTtl != nil {
		be.CdnPolicy.DefaultTtl = *cdnConfig.DefaultTtl
		if be.CdnPolicy.DefaultTtl == 0 {
			be.CdnPolicy.ForceSendFields = append(be.CdnPolicy.ForceSendFields, "DefaultTtl")
		}
	} else {
		be.CdnPolicy.DefaultTtl = defaultCdnPolicy.DefaultTtl
	}

	if cdnConfig.NegativeCaching != nil {
		be.CdnPolicy.NegativeCaching = *cdnConfig.NegativeCaching
	} else {
		be.CdnPolicy.NegativeCaching = defaultCdnPolicy.NegativeCaching
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

	if len(negativeCachingPolicy) > 0 {
		be.CdnPolicy.NegativeCachingPolicy = negativeCachingPolicy
	}

	if cdnConfig.SignedUrlCacheMaxAgeSec != nil {
		be.CdnPolicy.SignedUrlCacheMaxAgeSec = *cdnConfig.SignedUrlCacheMaxAgeSec
	} else {
		be.CdnPolicy.SignedUrlCacheMaxAgeSec = defaultCdnPolicy.SignedUrlCacheMaxAgeSec
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
	if len(bypassCacheOnRequestHeaders) > 0 {
		be.CdnPolicy.BypassCacheOnRequestHeaders = bypassCacheOnRequestHeaders
	}

	return be
}

func hasDiff(new *composite.BackendService, current *composite.BackendService) bool {

	if new.EnableCDN != current.EnableCDN {
		return true
	}

	if !new.EnableCDN {
		return false
	}

	if current.CdnPolicy == nil {
		return true
	}

	// In order to use reflect.DeepEqual we need to handle the special cases

	// SignedUrlKeyNames are not handled by this function and must be preserved as they are
	if current.CdnPolicy != nil && current.CdnPolicy.SignedUrlKeyNames != nil {
		new.CdnPolicy.SignedUrlKeyNames = current.CdnPolicy.SignedUrlKeyNames
	}

	// for NegativeCachingPolicy order does not matter, so if there are equal regardless of order
	// we take the current one
	if negativeCachingPolicyEqual(new.CdnPolicy.NegativeCachingPolicy, current.CdnPolicy.NegativeCachingPolicy) {
		new.CdnPolicy.NegativeCachingPolicy = current.CdnPolicy.NegativeCachingPolicy
	}

	// for all other fields we can use reflect.DeepEqual
	return !reflect.DeepEqual(new.CdnPolicy, current.CdnPolicy)
}

func applyConfig(new *composite.BackendService, current *composite.BackendService) {
	current.EnableCDN = new.EnableCDN
	current.CdnPolicy = new.CdnPolicy
}
