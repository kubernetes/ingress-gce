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

	"github.com/golang/glog"
	backendconfigv1beta1 "k8s.io/ingress-gce/pkg/apis/backendconfig/v1beta1"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/utils"
)

// EnsureCDN reads the CDN configuration specified in the ServicePort.BackendConfig
// and applies it to the BackendService. It returns true if there were existing
// settings on the BackendService that were overwritten.
func EnsureCDN(sp utils.ServicePort, be *composite.BackendService) bool {
	beTemp := &composite.BackendService{}
	applyCDNSettings(sp, beTemp)
	if !reflect.DeepEqual(beTemp.CdnPolicy, be.CdnPolicy) || beTemp.EnableCDN != be.EnableCDN {
		applyCDNSettings(sp, be)
		glog.V(2).Infof("Updated CDN settings for service %v/%v.", sp.ID.Service.Namespace, sp.ID.Service.Name)
		return true
	}
	return false
}

// applyCDNSettings applies the CDN settings specified in the BackendConfig
// to the passed in compute.BackendService. A GCE API call still needs to be
// made to actually persist the changes.
func applyCDNSettings(sp utils.ServicePort, be *composite.BackendService) {
	beConfig := sp.BackendConfig
	setCDNDefaults(beConfig)
	// Apply the boolean switch
	be.EnableCDN = beConfig.Spec.Cdn.Enabled
	// Apply the cache key policies
	cacheKeyPolicy := beConfig.Spec.Cdn.CachePolicy
	be.CdnPolicy = &composite.BackendServiceCdnPolicy{CacheKeyPolicy: &composite.CacheKeyPolicy{}}
	be.CdnPolicy.CacheKeyPolicy.IncludeHost = cacheKeyPolicy.IncludeHost
	be.CdnPolicy.CacheKeyPolicy.IncludeProtocol = cacheKeyPolicy.IncludeProtocol
	be.CdnPolicy.CacheKeyPolicy.IncludeQueryString = cacheKeyPolicy.IncludeQueryString
	be.CdnPolicy.CacheKeyPolicy.QueryStringBlacklist = cacheKeyPolicy.QueryStringBlacklist
	be.CdnPolicy.CacheKeyPolicy.QueryStringWhitelist = cacheKeyPolicy.QueryStringWhitelist
}

// setCDNDefaults initializes any nil pointers in CDN configuration which ensures that there are defaults for missing sub-types.
func setCDNDefaults(beConfig *backendconfigv1beta1.BackendConfig) {
	if beConfig.Spec.Cdn == nil {
		beConfig.Spec.Cdn = &backendconfigv1beta1.CDNConfig{}
	}
	if beConfig.Spec.Cdn.CachePolicy == nil {
		beConfig.Spec.Cdn.CachePolicy = &backendconfigv1beta1.CacheKeyPolicy{}
	}
}
