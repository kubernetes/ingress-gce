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
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/utils"
	"net/http"
	"strings"

	computealpha "google.golang.org/api/compute/v0.alpha"
	computebeta "google.golang.org/api/compute/v0.beta"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/googleapi"
	"k8s.io/klog"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/filter"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
)

const (
	NegResourceType = "networkEndpointGroup"
	defaultRegion   = "us-central1" // TODO(shance): plumb this from GCE
	IgResourceType  = "instanceGroup"
)

// TODO(shance): convert below to composite types and resourceVersions

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
	GA    *compute.NetworkEndpointGroup
	Alpha *computealpha.NetworkEndpointGroup
	Beta  *computebeta.NetworkEndpointGroup
}

// InstanceGroup is a union of the API version types.
type InstanceGroup struct {
	GA *compute.InstanceGroup
}

// NetworkEndpoints contains the NEG definition and the network Endpoints in NEG
type NetworkEndpoints struct {
	NEG       *compute.NetworkEndpointGroup
	Endpoints []*compute.NetworkEndpointWithHealthStatus
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
	InstanceGroup        map[meta.Key]*InstanceGroup
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
		InstanceGroup:        map[meta.Key]*InstanceGroup{},
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
	var err error

	for k := range g.ForwardingRule {
		if k.Type() == meta.Regional {
			_, err = c.AlphaForwardingRules().Get(ctx, &k)
		} else {
			_, err = c.ForwardingRules().Get(ctx, &k)
		}
		if err != nil {
			if err.(*googleapi.Error) == nil || err.(*googleapi.Error).Code != http.StatusNotFound {
				return fmt.Errorf("ForwardingRule %s is not deleted/error to get: %s", k.Name, err)
			}
		} else {
			resources = append(resources, k)
		}
	}
	for k := range g.TargetHTTPProxy {
		if k.Type() == meta.Regional {
			_, err = c.AlphaRegionTargetHttpProxies().Get(ctx, &k)
		} else {
			_, err = c.TargetHttpProxies().Get(ctx, &k)
		}
		if err != nil {
			if err.(*googleapi.Error) == nil || err.(*googleapi.Error).Code != http.StatusNotFound {
				return fmt.Errorf("TargetHTTPProxy %s is not deleted/error to get: %s", k.Name, err)
			}
		} else {
			resources = append(resources, k)
		}
	}
	for k := range g.TargetHTTPSProxy {
		if k.Type() == meta.Regional {
			_, err = c.AlphaRegionTargetHttpsProxies().Get(ctx, &k)
		} else {
			_, err = c.TargetHttpsProxies().Get(ctx, &k)
		}
		if err != nil {
			if err.(*googleapi.Error) == nil || err.(*googleapi.Error).Code != http.StatusNotFound {
				return fmt.Errorf("TargetHTTPSProxy %s is not deleted/error to get: %s", k.Name, err)
			}
		} else {
			resources = append(resources, k)
		}
	}
	for k := range g.URLMap {
		if k.Type() == meta.Regional {
			_, err = c.AlphaRegionUrlMaps().Get(ctx, &k)
		} else {
			_, err = c.UrlMaps().Get(ctx, &k)
		}
		if err != nil {
			if err.(*googleapi.Error) == nil || err.(*googleapi.Error).Code != http.StatusNotFound {
				return fmt.Errorf("URLMap %s is not deleted/error to get: %s", k.Name, err)
			}
		} else {
			resources = append(resources, k)
		}
	}
	for k := range g.BackendService {
		if k.Type() == meta.Regional {
			bs, err := c.AlphaRegionBackendServices().Get(ctx, &k)
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
		} else {
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
	}
	for k := range g.NetworkEndpointGroup {
		_, err := c.BetaNetworkEndpointGroups().Get(ctx, &k)
		if err != nil {
			if err.(*googleapi.Error) == nil || err.(*googleapi.Error).Code != http.StatusNotFound {
				return fmt.Errorf("NetworkEndpointGroup %s is not deleted/error to get: %s", k.Name, err)
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

func hasAlphaRegionResource(resourceType string, validators []FeatureValidator) bool {
	for _, val := range validators {
		if val.HasAlphaRegionResource(resourceType) {
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

	var forwardingRules []*composite.ForwardingRule
	for _, gfr := range allGFRs {
		if gfr.IPAddress == vip {
			compositeGfr, err := composite.ToForwardingRule(gfr)
			if err != nil {
				return nil, fmt.Errorf("Error converting forwarding rule to composite")
			}
			forwardingRules = append(forwardingRules, compositeGfr)
		}
	}

	if hasAlphaRegionResource("forwardingRule", validators) {
		allRFRs, err := c.AlphaForwardingRules().List(ctx, defaultRegion, filter.None)
		if err != nil {
			klog.Warningf("Error listing forwarding rules: %v", err)
			return nil, err
		}

		for _, rfr := range allRFRs {
			if rfr.IPAddress == vip {
				compositeRfr, err := composite.ToForwardingRule(rfr)
				if err != nil {
					return nil, fmt.Errorf("Error converting forwarding rule to composite")
				}
				compositeRfr.Scope = meta.Regional
				forwardingRules = append(forwardingRules, compositeRfr)
			}
		}
	}

	var urlMapKey *meta.Key
	for _, fr := range forwardingRules {
		frKey := meta.GlobalKey(fr.Name)
		ga, err := fr.ToGA()
		gclb.ForwardingRule[*frKey] = &ForwardingRule{GA: ga}
		if hasAlphaResource("forwardingRule", validators) {
			fr, err := c.AlphaGlobalForwardingRules().Get(ctx, frKey)
			if err != nil {
				klog.Warningf("Error getting alpha forwarding rules: %v", err)
				return nil, err
			}
			gclb.ForwardingRule[*frKey].Alpha = fr
		}
		if hasAlphaRegionResource("forwardingRule", validators) && fr.Scope == meta.Regional {
			regionKey := meta.RegionalKey(fr.Name, defaultRegion)
			fr, err := c.AlphaForwardingRules().Get(ctx, regionKey)
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
		resID, err := cloud.ParseResourceURL(fr.Target)
		if err != nil {
			klog.Warningf("Error parsing Target (%q): %v", fr.Target, err)
			return nil, err
		}

		var urlMapResID *cloud.ResourceID
		switch resID.Resource {
		case "targetHttpProxies":
			if hasAlphaRegionResource("targetHttpProxy", validators) && resID.Key.Type() == meta.Regional {
				p, err := c.AlphaRegionTargetHttpProxies().Get(ctx, resID.Key)
				if err != nil {
					klog.Warningf("Error getting TargetHttpProxy %s: %v", resID.Key, err)
					return nil, err
				}
				gclb.TargetHTTPProxy[*resID.Key] = &TargetHTTPProxy{Alpha: p}

				urlMapResID, err = cloud.ParseResourceURL(p.UrlMap)
				if err != nil {
					klog.Warningf("Error parsing urlmap URL (%q): %v", p.UrlMap, err)
					return nil, err
				}
			} else if hasAlphaResource("targetHttpProxy", validators) || hasBetaResource("targetHttpProxy", validators) {
				return nil, errors.New("unsupported targetHttpProxy version")
			} else {
				p, err := c.TargetHttpProxies().Get(ctx, resID.Key)
				if err != nil {
					klog.Warningf("Error getting TargetHttpProxy %s: %v", resID.Key, err)
					return nil, err
				}
				gclb.TargetHTTPProxy[*resID.Key] = &TargetHTTPProxy{GA: p}

				urlMapResID, err = cloud.ParseResourceURL(p.UrlMap)
				if err != nil {
					klog.Warningf("Error parsing urlmap URL (%q): %v", p.UrlMap, err)
					return nil, err
				}
			}
			if urlMapKey == nil {
				urlMapKey = urlMapResID.Key
			}
			if *urlMapKey != *urlMapResID.Key {
				klog.Warningf("Error targetHttpProxy references are not the same (%s != %s)", *urlMapKey, *urlMapResID.Key)
				return nil, fmt.Errorf("targetHttpProxy references are not the same: %+v != %+v", *urlMapKey, *urlMapResID.Key)
			}
		case "targetHttpsProxies":
			if hasAlphaRegionResource("targetHttpsProxy", validators) && resID.Key.Type() == meta.Regional {
				p, err := c.AlphaRegionTargetHttpsProxies().Get(ctx, resID.Key)
				if err != nil {
					klog.Warningf("Error getting targetHttpsProxy (%s): %v", resID.Key, err)
					return nil, err
				}
				gclb.TargetHTTPSProxy[*resID.Key] = &TargetHTTPSProxy{Alpha: p}
				urlMapResID, err = cloud.ParseResourceURL(p.UrlMap)
				if err != nil {
					klog.Warningf("Error parsing urlmap URL (%q): %v", p.UrlMap, err)
					return nil, err
				}
			} else if hasAlphaResource("targetHttpsProxy", validators) || hasBetaResource("targetHttpsProxy", validators) {
				return nil, errors.New("unsupported targetHttpsProxy version")
			} else {
				p, err := c.TargetHttpsProxies().Get(ctx, resID.Key)
				if err != nil {
					klog.Warningf("Error getting targetHttpsProxy (%s): %v", resID.Key, err)
					return nil, err
				}
				gclb.TargetHTTPSProxy[*resID.Key] = &TargetHTTPSProxy{GA: p}

				urlMapResID, err = cloud.ParseResourceURL(p.UrlMap)
				if err != nil {
					klog.Warningf("Error parsing urlmap URL (%q): %v", p.UrlMap, err)
					return nil, err
				}
			}
			if urlMapKey == nil {
				urlMapKey = urlMapResID.Key
			}
			if *urlMapKey != *urlMapResID.Key {
				klog.Warningf("Error targetHttpsProxy references are not the same (%s != %s)", *urlMapKey, *urlMapResID.Key)
				return nil, fmt.Errorf("targetHttpsProxy references are not the same: %+v != %+v", *urlMapKey, *urlMapResID.Key)
			}
		default:
			klog.Errorf("Unhandled resource: %q, grf = %+v", resID.Resource, fr)
			return nil, fmt.Errorf("unhandled resource %q", resID.Resource)
		}
	}

	// TargetProxy => URLMap
	var bsKeys []*meta.Key
	if hasAlphaResource("urlMap", validators) || hasBetaResource("urlMap", validators) {
		return nil, errors.New("unsupported urlMap version")
	} else if hasAlphaRegionResource("urlMap", validators) && urlMapKey.Type() == meta.Regional {
		urlMap, err := c.AlphaRegionUrlMaps().Get(ctx, urlMapKey)
		if err != nil {
			klog.Warningf("Error getting alpha region Url Maps: %v", err)
			return nil, err
		}
		gclb.URLMap[*urlMapKey] = &URLMap{Alpha: urlMap}
		resID, err := cloud.ParseResourceURL(urlMap.DefaultService)
		if err != nil {
			klog.Warningf("Error parsing resource URL: %v", err)
			return nil, err
		}
		bsKeys = append(bsKeys, resID.Key)

		// URLMap => BackendService(s)
		for _, pm := range urlMap.PathMatchers {
			resID, err := cloud.ParseResourceURL(pm.DefaultService)
			if err != nil {
				klog.Warningf("Error parsing resource URL: %v", err)
				return nil, err
			}
			bsKeys = append(bsKeys, resID.Key)

			for _, pr := range pm.PathRules {
				resID, err := cloud.ParseResourceURL(pr.Service)
				if err != nil {
					klog.Warningf("Error parsing resource URL: %v", err)
					return nil, err
				}
				bsKeys = append(bsKeys, resID.Key)
			}
		}
	} else {
		urlMap, err := c.UrlMaps().Get(ctx, urlMapKey)
		if err != nil {
			klog.Warningf("Error getting URL map: %v", err)
			return nil, err
		}
		gclb.URLMap[*urlMapKey] = &URLMap{GA: urlMap}
		resID, err := cloud.ParseResourceURL(urlMap.DefaultService)
		if err != nil {
			klog.Warningf("Error parsing resource URL: %v", err)
			return nil, err
		}
		bsKeys = append(bsKeys, resID.Key)

		// URLMap => BackendService(s)
		for _, pm := range urlMap.PathMatchers {
			resID, err := cloud.ParseResourceURL(pm.DefaultService)
			if err != nil {
				klog.Warningf("Error parsing resource URL: %v", err)
				return nil, err
			}
			bsKeys = append(bsKeys, resID.Key)

			for _, pr := range pm.PathRules {
				resID, err := cloud.ParseResourceURL(pr.Service)
				if err != nil {
					klog.Warningf("Error parsing resource URL: %v", err)
					return nil, err
				}
				bsKeys = append(bsKeys, resID.Key)
			}
		}
	}

	for _, bsKey := range bsKeys {
		if hasAlphaRegionResource("backendService", validators) && bsKey.Type() == meta.Regional {
			bs, err := c.AlphaRegionBackendServices().Get(ctx, bsKey)
			if err != nil {
				klog.Warningf("Error getting alpha region backend service: %v", err)
				return nil, err
			}
			gclb.BackendService[*bsKey] = &BackendService{Alpha: bs}
		} else if hasBetaResource("backendService", validators) {
			bs, err := c.BetaBackendServices().Get(ctx, bsKey)
			if err != nil {
				klog.Warningf("Error getting beta backend service: %v", err)
				return nil, err
			}
			gclb.BackendService[*bsKey] = &BackendService{Beta: bs}
		} else {
			bs, err := c.BackendServices().Get(ctx, bsKey)
			if err != nil {
				return nil, err
			}
			gclb.BackendService[*bsKey] = &BackendService{GA: bs}
		}
	}

	negKeys := []*meta.Key{}
	igKeys := []*meta.Key{}
	// Fetch NEG Backends
	for _, bsKey := range bsKeys {
		beGroups := []string{}
		if hasAlphaRegionResource("backendService", validators) && bsKey.Type() == meta.Regional {
			bs, err := c.AlphaRegionBackendServices().Get(ctx, bsKey)
			if err != nil {
				klog.Warningf("Error getting alpha region backend service: %v", err)
				return nil, err
			}
			for _, be := range bs.Backends {
				beGroups = append(beGroups, be.Group)
			}
		} else if hasAlphaResource("backendService", validators) {
			bs, err := c.AlphaBackendServices().Get(ctx, bsKey)
			if err != nil {
				klog.Warningf("Error getting alpha backend service: %v", err)
				return nil, err
			}
			for _, be := range bs.Backends {
				beGroups = append(beGroups, be.Group)
			}
		} else {
			bs, err := c.BetaBackendServices().Get(ctx, bsKey)
			if err != nil {
				klog.Warningf("Error getting backend service: %v", err)
				return nil, err
			}
			for _, be := range bs.Backends {
				beGroups = append(beGroups, be.Group)
			}
		}
		for _, group := range beGroups {
			if strings.Contains(group, NegResourceType) {
				resourceId, err := cloud.ParseResourceURL(group)
				if err != nil {
					return nil, err
				}
				negKeys = append(negKeys, resourceId.Key)
			}

			if strings.Contains(group, IgResourceType) {
				resourceId, err := cloud.ParseResourceURL(group)
				if err != nil {
					return nil, err
				}
				igKeys = append(igKeys, resourceId.Key)
			}

		}
	}

	for _, negKey := range negKeys {
		neg, err := c.NetworkEndpointGroups().Get(ctx, negKey)
		if err != nil {
			return nil, err
		}
		gclb.NetworkEndpointGroup[*negKey] = &NetworkEndpointGroup{GA: neg}
		if hasAlphaResource(NegResourceType, validators) {
			neg, err := c.AlphaNetworkEndpointGroups().Get(ctx, negKey)
			if err != nil {
				klog.Warningf("Error getting alpha network endpoint groups: %v", err)
				return nil, err
			}
			gclb.NetworkEndpointGroup[*negKey].Alpha = neg
		}
		if hasBetaResource(NegResourceType, validators) {
			neg, err := c.BetaNetworkEndpointGroups().Get(ctx, negKey)
			if err != nil {
				return nil, err
			}
			gclb.NetworkEndpointGroup[*negKey].Beta = neg
		}
	}

	for _, igKey := range igKeys {
		ig, err := c.InstanceGroups().Get(ctx, igKey)
		if err != nil {
			return nil, err
		}
		gclb.InstanceGroup[*igKey] = &InstanceGroup{GA: ig}
	}

	return gclb, err
}

// NetworkEndpointsInNegs retrieves the network Endpoints from NEGs with one name in multiple zones
func NetworkEndpointsInNegs(ctx context.Context, c cloud.Cloud, name string, zones []string) (map[meta.Key]*NetworkEndpoints, error) {
	ret := map[meta.Key]*NetworkEndpoints{}
	for _, zone := range zones {
		key := meta.ZonalKey(name, zone)
		neg, err := c.NetworkEndpointGroups().Get(ctx, key)
		if err != nil {
			return nil, err
		}
		networkEndpoints := &NetworkEndpoints{
			NEG: neg,
		}
		nes, err := c.NetworkEndpointGroups().ListNetworkEndpoints(ctx, key, &compute.NetworkEndpointGroupsListEndpointsRequest{HealthStatus: "SHOW"}, nil)
		if err != nil {
			return nil, err
		}
		networkEndpoints.Endpoints = nes
		ret[*key] = networkEndpoints
	}
	return ret, nil
}
