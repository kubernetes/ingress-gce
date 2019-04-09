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

package fuzz

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	computealpha "google.golang.org/api/compute/v0.alpha"
	computebeta "google.golang.org/api/compute/v0.beta"
	compute "google.golang.org/api/compute/v1"
	"google.golang.org/api/googleapi"
	"k8s.io/klog"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/filter"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"

	"k8s.io/ingress-gce/pkg/utils"
)

const (
	NegResourceType = "networkEndpointGroup"
)

// ForwardingRule is a union of the API version types.
type ForwardingRule struct {
	GA    *compute.ForwardingRule
	Alpha *computealpha.ForwardingRule
	Beta  *computebeta.ForwardingRule
}

// TargetHTTPProxy is a union of the API version types.
type TargetHTTPProxy struct {
	GA    *compute.TargetHttpProxy
	Alpha *computealpha.TargetHttpProxy
	Beta  *computebeta.TargetHttpProxy
}

// TargetHTTPSProxy is a union of the API version types.
type TargetHTTPSProxy struct {
	GA    *compute.TargetHttpsProxy
	Alpha *computealpha.TargetHttpsProxy
	Beta  *computebeta.TargetHttpsProxy
}

// URLMap is a union of the API version types.
type URLMap struct {
	GA    *compute.UrlMap
	Alpha *computealpha.UrlMap
	Beta  *computebeta.UrlMap
}

// BackendService is a union of the API version types.
type BackendService struct {
	GA    *compute.BackendService
	Alpha *computealpha.BackendService
	Beta  *computebeta.BackendService
}

// NetworkEndpointGroup is a union of the API version types.
type NetworkEndpointGroup struct {
	Alpha *computealpha.NetworkEndpointGroup
	Beta  *computebeta.NetworkEndpointGroup
}

// GCLB contains the resources for a load balancer.
type GCLB struct {
	VIP string

	ForwardingRule       map[meta.Key]*ForwardingRule
	TargetHTTPProxy      map[meta.Key]*TargetHTTPProxy
	TargetHTTPSProxy     map[meta.Key]*TargetHTTPSProxy
	URLMap               map[meta.Key]*URLMap
	BackendService       map[meta.Key]*BackendService
	NetworkEndpointGroup map[meta.Key]*NetworkEndpointGroup
}

// NewGCLB returns an empty GCLB.
func NewGCLB(vip string) *GCLB {
	return &GCLB{
		VIP:                  vip,
		ForwardingRule:       map[meta.Key]*ForwardingRule{},
		TargetHTTPProxy:      map[meta.Key]*TargetHTTPProxy{},
		TargetHTTPSProxy:     map[meta.Key]*TargetHTTPSProxy{},
		URLMap:               map[meta.Key]*URLMap{},
		BackendService:       map[meta.Key]*BackendService{},
		NetworkEndpointGroup: map[meta.Key]*NetworkEndpointGroup{},
	}
}

// GCLBDeleteOptions may be provided when cleaning up GCLB resource.
type GCLBDeleteOptions struct {
	// SkipDefaultBackend indicates whether to skip checking for the
	// system default backend.
	SkipDefaultBackend bool
}

// CheckResourceDeletion checks the existence of the resources. Returns nil if
// all of the associated resources no longer exist.
func (g *GCLB) CheckResourceDeletion(ctx context.Context, c cloud.Cloud, options *GCLBDeleteOptions) error {
	var resources []meta.Key

	for k := range g.ForwardingRule {
		_, err := c.ForwardingRules().Get(ctx, &k)
		if err != nil {
			if err.(*googleapi.Error) == nil || err.(*googleapi.Error).Code != http.StatusNotFound {
				return err
			}
		} else {
			resources = append(resources, k)
		}
	}
	for k := range g.TargetHTTPProxy {
		_, err := c.TargetHttpProxies().Get(ctx, &k)
		if err != nil {
			if err.(*googleapi.Error) == nil || err.(*googleapi.Error).Code != http.StatusNotFound {
				return err
			}
		} else {
			resources = append(resources, k)
		}
	}
	for k := range g.TargetHTTPSProxy {
		_, err := c.TargetHttpsProxies().Get(ctx, &k)
		if err != nil {
			if err.(*googleapi.Error) == nil || err.(*googleapi.Error).Code != http.StatusNotFound {
				return err
			}
		} else {
			resources = append(resources, k)
		}
	}
	for k := range g.URLMap {
		_, err := c.UrlMaps().Get(ctx, &k)
		if err != nil {
			if err.(*googleapi.Error) == nil || err.(*googleapi.Error).Code != http.StatusNotFound {
				return err
			}
		} else {
			resources = append(resources, k)
		}
	}
	for k := range g.BackendService {
		bs, err := c.BackendServices().Get(ctx, &k)
		if err != nil {
			if err.(*googleapi.Error) == nil || err.(*googleapi.Error).Code != http.StatusNotFound {
				return err
			}
		} else {
			if options != nil && options.SkipDefaultBackend {
				desc := utils.DescriptionFromString(bs.Description)
				if desc.ServiceName == "kube-system/default-http-backend" {
					continue
				}
			}
			resources = append(resources, k)
		}
	}
	for k := range g.NetworkEndpointGroup {
		_, err := c.BetaNetworkEndpointGroups().Get(ctx, &k)
		if err != nil {
			if err.(*googleapi.Error) == nil || err.(*googleapi.Error).Code != http.StatusNotFound {
				return err
			}
		} else {
			resources = append(resources, k)
		}
	}

	if len(resources) != 0 {
		var s []string
		for _, r := range resources {
			s = append(s, r.String())
		}
		return fmt.Errorf("resources still exist (%s)", strings.Join(s, ", "))
	}

	return nil
}

// Check that all NEGs associated with the GCLB have been deleted
func (g *GCLB) CheckNEGDeletion(ctx context.Context, c cloud.Cloud, options *GCLBDeleteOptions) error {
	var resources []meta.Key

	for k := range g.NetworkEndpointGroup {
		_, err := c.BetaNetworkEndpointGroups().Get(ctx, &k)
		if err != nil {
			if err.(*googleapi.Error) == nil || err.(*googleapi.Error).Code != http.StatusNotFound {
				return err
			}
		} else {
			resources = append(resources, k)
		}
	}

	if len(resources) != 0 {
		var s []string
		for _, r := range resources {
			s = append(s, r.String())
		}
		return fmt.Errorf("NEGs still exist (%s)", strings.Join(s, ", "))
	}

	return nil
}

func hasAlphaResource(resourceType string, validators []FeatureValidator) bool {
	for _, val := range validators {
		if val.HasAlphaResource(resourceType) {
			return true
		}
	}
	return false
}

func hasBetaResource(resourceType string, validators []FeatureValidator) bool {
	for _, val := range validators {
		if val.HasBetaResource(resourceType) {
			return true
		}
	}
	return false
}

// GCLBForVIP retrieves all of the resources associated with the GCLB for a
// given VIP.
func GCLBForVIP(ctx context.Context, c cloud.Cloud, vip string, validators []FeatureValidator) (*GCLB, error) {
	gclb := NewGCLB(vip)

	allGFRs, err := c.GlobalForwardingRules().List(ctx, filter.None)
	if err != nil {
		klog.Warningf("Error listing forwarding rules: %v", err)
		return nil, err
	}

	var gfrs []*compute.ForwardingRule
	for _, gfr := range allGFRs {
		if gfr.IPAddress == vip {
			gfrs = append(gfrs, gfr)
		}
	}

	var urlMapKey *meta.Key
	for _, gfr := range gfrs {
		frKey := meta.GlobalKey(gfr.Name)
		gclb.ForwardingRule[*frKey] = &ForwardingRule{GA: gfr}
		if hasAlphaResource("forwardingRule", validators) {
			fr, err := c.AlphaForwardingRules().Get(ctx, frKey)
			if err != nil {
				klog.Warningf("Error getting alpha forwarding rules: %v", err)
				return nil, err
			}
			gclb.ForwardingRule[*frKey].Alpha = fr
		}
		if hasBetaResource("forwardingRule", validators) {
			return nil, errors.New("unsupported forwardingRule version")
		}

		// ForwardingRule => TargetProxy
		resID, err := cloud.ParseResourceURL(gfr.Target)
		if err != nil {
			klog.Warningf("Error parsing Target (%q): %v", gfr.Target, err)
			return nil, err
		}
		switch resID.Resource {
		case "targetHttpProxies":
			p, err := c.TargetHttpProxies().Get(ctx, resID.Key)
			if err != nil {
				klog.Warningf("Error getting TargetHttpProxy %s: %v", resID.Key, err)
				return nil, err
			}
			gclb.TargetHTTPProxy[*resID.Key] = &TargetHTTPProxy{GA: p}
			if hasAlphaResource("targetHttpProxy", validators) || hasBetaResource("targetHttpProxy", validators) {
				return nil, errors.New("unsupported targetHttpProxy version")
			}

			urlMapResID, err := cloud.ParseResourceURL(p.UrlMap)
			if err != nil {
				klog.Warningf("Error parsing urlmap URL (%q): %v", p.UrlMap, err)
				return nil, err
			}
			if urlMapKey == nil {
				urlMapKey = urlMapResID.Key
			}
			if *urlMapKey != *urlMapResID.Key {
				klog.Warningf("Error targetHttpProxy references are not the same (%s != %s)", *urlMapKey, *urlMapResID.Key)
				return nil, fmt.Errorf("targetHttpProxy references are not the same: %+v != %+v", *urlMapKey, *urlMapResID.Key)
			}
		case "targetHttpsProxies":
			p, err := c.TargetHttpsProxies().Get(ctx, resID.Key)
			if err != nil {
				klog.Warningf("Error getting targetHttpsProxy (%s): %v", resID.Key, err)
				return nil, err
			}
			gclb.TargetHTTPSProxy[*resID.Key] = &TargetHTTPSProxy{GA: p}
			if hasAlphaResource("targetHttpsProxy", validators) || hasBetaResource("targetHttpsProxy", validators) {
				return nil, errors.New("unsupported targetHttpsProxy version")
			}

			urlMapResID, err := cloud.ParseResourceURL(p.UrlMap)
			if err != nil {
				klog.Warningf("Error parsing urlmap URL (%q): %v", p.UrlMap, err)
				return nil, err
			}
			if urlMapKey == nil {
				urlMapKey = urlMapResID.Key
			}
			if *urlMapKey != *urlMapResID.Key {
				klog.Warningf("Error targetHttpsProxy references are not the same (%s != %s)", *urlMapKey, *urlMapResID.Key)
				return nil, fmt.Errorf("targetHttpsProxy references are not the same: %+v != %+v", *urlMapKey, *urlMapResID.Key)
			}
		default:
			klog.Errorf("Unhandled resource: %q, grf = %+v", resID.Resource, gfr)
			return nil, fmt.Errorf("unhandled resource %q", resID.Resource)
		}
	}

	// TargetProxy => URLMap
	urlMap, err := c.UrlMaps().Get(ctx, urlMapKey)
	if err != nil {
		return nil, err
	}
	gclb.URLMap[*urlMapKey] = &URLMap{GA: urlMap}
	if hasAlphaResource("urlMap", validators) || hasBetaResource("urlMap", validators) {
		return nil, errors.New("unsupported urlMap version")
	}

	// URLMap => BackendService(s)
	var bsKeys []*meta.Key
	resID, err := cloud.ParseResourceURL(urlMap.DefaultService)
	if err != nil {
		return nil, err
	}
	bsKeys = append(bsKeys, resID.Key)

	for _, pm := range urlMap.PathMatchers {
		resID, err := cloud.ParseResourceURL(pm.DefaultService)
		if err != nil {
			return nil, err
		}
		bsKeys = append(bsKeys, resID.Key)

		for _, pr := range pm.PathRules {
			resID, err := cloud.ParseResourceURL(pr.Service)
			if err != nil {
				return nil, err
			}
			bsKeys = append(bsKeys, resID.Key)
		}
	}

	for _, bsKey := range bsKeys {
		bs, err := c.BackendServices().Get(ctx, bsKey)
		if err != nil {
			return nil, err
		}
		gclb.BackendService[*bsKey] = &BackendService{GA: bs}

		if hasAlphaResource("backendService", validators) {
			bs, err := c.AlphaBackendServices().Get(ctx, bsKey)
			if err != nil {
				return nil, err
			}
			gclb.BackendService[*bsKey].Alpha = bs
		}
		if hasBetaResource("backendService", validators) {
			bs, err := c.BetaBackendServices().Get(ctx, bsKey)
			if err != nil {
				return nil, err
			}
			gclb.BackendService[*bsKey].Beta = bs
		}
	}

	negKeys := []*meta.Key{}
	// Fetch NEG Backends
	for _, bsKey := range bsKeys {
		beGroups := []string{}
		if hasAlphaResource("backendService", validators) {
			bs, err := c.AlphaBackendServices().Get(ctx, bsKey)
			if err != nil {
				return nil, err
			}
			for _, be := range bs.Backends {
				beGroups = append(beGroups, be.Group)
			}
		} else {
			bs, err := c.BetaBackendServices().Get(ctx, bsKey)
			if err != nil {
				return nil, err
			}
			for _, be := range bs.Backends {
				beGroups = append(beGroups, be.Group)
			}
		}
		for _, group := range beGroups {
			// Only fetch NEG backends
			if !strings.Contains(group, NegResourceType) {
				continue
			}
			resourceId, err := cloud.ParseResourceURL(group)
			if err != nil {
				return nil, err
			}
			negKeys = append(negKeys, resourceId.Key)
		}
	}

	for _, negKey := range negKeys {
		neg, err := c.BetaNetworkEndpointGroups().Get(ctx, negKey)
		if err != nil {
			return nil, err
		}
		gclb.NetworkEndpointGroup[*negKey] = &NetworkEndpointGroup{Beta: neg}
		if hasAlphaResource(NegResourceType, validators) {
			neg, err := c.AlphaNetworkEndpointGroups().Get(ctx, negKey)
			if err != nil {
				return nil, err
			}
			gclb.NetworkEndpointGroup[*negKey].Alpha = neg
		}
	}

	return gclb, err
}
