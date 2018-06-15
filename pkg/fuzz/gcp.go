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

	"github.com/golang/glog"
	computealpha "google.golang.org/api/compute/v0.alpha"
	computebeta "google.golang.org/api/compute/v0.beta"
	compute "google.golang.org/api/compute/v1"
	"k8s.io/kubernetes/pkg/cloudprovider/providers/gce/cloud"
	"k8s.io/kubernetes/pkg/cloudprovider/providers/gce/cloud/filter"
	"k8s.io/kubernetes/pkg/cloudprovider/providers/gce/cloud/meta"
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

// GCLB contains the resources for a load balancer.
type GCLB struct {
	ForwardingRule   map[meta.Key]*ForwardingRule
	TargetHTTPProxy  map[meta.Key]*TargetHTTPProxy
	TargetHTTPSProxy map[meta.Key]*TargetHTTPSProxy
	URLMap           map[meta.Key]*URLMap
	BackendService   map[meta.Key]*BackendService
}

// NewGCLB returns an empty GCLB.
func NewGCLB() *GCLB {
	return &GCLB{
		ForwardingRule:   map[meta.Key]*ForwardingRule{},
		TargetHTTPProxy:  map[meta.Key]*TargetHTTPProxy{},
		TargetHTTPSProxy: map[meta.Key]*TargetHTTPSProxy{},
		URLMap:           map[meta.Key]*URLMap{},
		BackendService:   map[meta.Key]*BackendService{},
	}
}

func hasAlphaResource(resourceType string, validators []FeatureValidator) bool {
	for _, val := range validators {
		if val.HasAlphaResource("forwardingRule") {
			return true
		}
	}
	return false
}

func hasBetaResource(resourceType string, validators []FeatureValidator) bool {
	for _, val := range validators {
		if val.HasBetaResource("forwardingRule") {
			return true
		}
	}
	return false
}

// GCLBForVIP retrieves all of the resources associated with the GCLB for a
// given VIP.
func GCLBForVIP(ctx context.Context, c cloud.Cloud, vip string, validators []FeatureValidator) (*GCLB, error) {
	gclb := NewGCLB()
	allGFRs, err := c.GlobalForwardingRules().List(ctx, filter.None)
	if err != nil {
		glog.Warningf("Error listing forwarding rules: %v", err)
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
				glog.Warningf("Error getting alpha forwarding rules: %v", err)
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
			glog.Warningf("Error parsing Target (%q): %v", gfr.Target, err)
			return nil, err
		}
		switch resID.Resource {
		case "targetHttpProxies":
			p, err := c.TargetHttpProxies().Get(ctx, resID.Key)
			if err != nil {
				glog.Warningf("Error getting TargetHttpProxy %s: %v", resID.Key, err)
				return nil, err
			}
			gclb.TargetHTTPProxy[*resID.Key] = &TargetHTTPProxy{GA: p}
			if hasAlphaResource("targetHttpProxy", validators) || hasBetaResource("targetHttpProxy", validators) {
				return nil, errors.New("unsupported targetHttpProxy version")
			}

			urlMapResID, err := cloud.ParseResourceURL(p.UrlMap)
			if err != nil {
				glog.Warningf("Error parsing urlmap URL (%q): %v", p.UrlMap, err)
				return nil, err
			}
			if urlMapKey == nil {
				urlMapKey = urlMapResID.Key
			}
			if *urlMapKey != *urlMapResID.Key {
				glog.Warningf("Error targetHttpProxy references are not the same (%s != %s)", *urlMapKey, *urlMapResID.Key)
				return nil, fmt.Errorf("targetHttpProxy references are not the same: %+v != %+v", *urlMapKey, *urlMapResID.Key)
			}
		case "targetHttpsProxies":
			p, err := c.TargetHttpsProxies().Get(ctx, resID.Key)
			if err != nil {
				glog.Warningf("Error getting targetHttpsProxy (%s): %v", resID.Key, err)
				return nil, err
			}
			gclb.TargetHTTPSProxy[*resID.Key] = &TargetHTTPSProxy{GA: p}
			if hasAlphaResource("targetHttpsProxy", validators) || hasBetaResource("targetHttpsProxy", validators) {
				return nil, errors.New("unsupported targetHttpsProxy version")
			}

			urlMapResID, err := cloud.ParseResourceURL(p.UrlMap)
			if err != nil {
				glog.Warningf("Error parsing urlmap URL (%q): %v", p.UrlMap, err)
				return nil, err
			}
			if urlMapKey == nil {
				urlMapKey = urlMapResID.Key
			}
			if *urlMapKey != *urlMapResID.Key {
				glog.Warningf("Error targetHttpsProxy references are not the same (%s != %s)", *urlMapKey, *urlMapResID.Key)
				return nil, fmt.Errorf("targetHttpsProxy references are not the same: %+v != %+v", *urlMapKey, *urlMapResID.Key)
			}
		default:
			glog.Errorf("Unhandled resource: %q, grf = %+v", resID.Resource, gfr)
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

	// TODO: fetch Backends

	return gclb, err
}
