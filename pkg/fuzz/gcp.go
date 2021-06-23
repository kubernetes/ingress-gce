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
	NegResourceType          = "networkEndpointGroup"
	IgResourceType           = "instanceGroup"
	HttpProtocol             = Protocol("HTTP")
	HttpsProtocol            = Protocol("HTTPS")
	targetHTTPProxyResource  = "targetHttpProxies"
	targetHTTPSProxyResource = "targetHttpsProxies"
	kubeSystemNS             = "kube-system"
	defaultHTTPBackend       = "default-http-backend"
)

// Protocol specifies GCE loadbalancer protocol.
type Protocol string

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

// HealthCheck is a union of the API version types.
type HealthCheck struct {
	GA *compute.HealthCheck
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

// ServiceAttachment is a union of the API version types.
type ServiceAttachment struct {
	Beta *computebeta.ServiceAttachment
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
	HealthCheck          map[meta.Key]*HealthCheck
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
		HealthCheck:          map[meta.Key]*HealthCheck{},
	}
}

// GCLBDeleteOptions may be provided when cleaning up GCLB resource.
type GCLBDeleteOptions struct {
	// SkipDefaultBackend indicates whether to skip checking for the
	// system default backend.
	SkipDefaultBackend bool
	// SkipBackends indicates whether to skip checking for the backends.
	// This is enabled only when we know that backends are shared among multiple ingresses
	// in which case shared backends are not cleaned up on ingress deletion.
	SkipBackends bool
	// CheckHttpFrontendResources indicates whether to check just the http
	// frontend resources.
	CheckHttpFrontendResources bool
	// CheckHttpsFrontendResources indicates whether to check just the https
	// frontend resources.
	CheckHttpsFrontendResources bool
}

// CheckResourceDeletion checks the existence of the resources. Returns nil if
// all of the associated resources no longer exist.
func (g *GCLB) CheckResourceDeletion(ctx context.Context, c cloud.Cloud, options *GCLBDeleteOptions) error {
	var resources []meta.Key

	for k := range g.ForwardingRule {
		var err error
		if k.Region != "" {
			_, err = c.ForwardingRules().Get(ctx, &k)
		} else {
			_, err = c.GlobalForwardingRules().Get(ctx, &k)
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
		var err error
		if k.Region != "" {
			// Use beta since GA isn't available yet
			_, err = c.BetaRegionTargetHttpProxies().Get(ctx, &k)
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
		var err error
		if k.Region != "" {
			// Use beta since GA isn't available yet
			_, err = c.BetaRegionTargetHttpsProxies().Get(ctx, &k)
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
		var err error
		if k.Region != "" {
			_, err = c.BetaRegionUrlMaps().Get(ctx, &k)
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
	if options == nil || !options.SkipBackends {
		for k := range g.BackendService {
			var err error
			var bs *compute.BackendService
			if k.Region != "" {
				bs, err = c.RegionBackendServices().Get(ctx, &k)
			} else {
				bs, err = c.BackendServices().Get(ctx, &k)
			}
			if err != nil {
				if err.(*googleapi.Error) == nil || err.(*googleapi.Error).Code != http.StatusNotFound {
					return fmt.Errorf("BackendService %s is not deleted/error to get: %s", k.Name, err)
				}
			} else {
				if options != nil && options.SkipDefaultBackend {
					desc := utils.DescriptionFromString(bs.Description)
					if desc.ServiceName == fmt.Sprintf("%s/%s", kubeSystemNS, defaultHTTPBackend) {
						continue
					}
				}
				resources = append(resources, k)
			}
		}

		for k := range g.NetworkEndpointGroup {
			ns, err := c.BetaNetworkEndpointGroups().Get(ctx, &k)
			if err != nil {
				if err.(*googleapi.Error) == nil || err.(*googleapi.Error).Code != http.StatusNotFound {
					return fmt.Errorf("NetworkEndpointGroup %s is not deleted/error to get: %s", k.Name, err)
				}
			} else {
				// TODO(smatti): Add NEG description to make this less error prone.
				// This is to ensure that ILB tests that use NEGs are not blocked on default NEG deletion.
				// Also, the default NEG may not get recognized here if default http backend name is changed
				// to cause truncation.
				if options != nil && options.SkipDefaultBackend &&
					strings.Contains(ns.Name, fmt.Sprintf("%s-%s", kubeSystemNS, defaultHTTPBackend)) {
					continue
				}
				resources = append(resources, k)
			}
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

// CheckResourceDeletionByProtocol checks the existence of the resources for given protocol.
// Returns nil if all of the associated frontend resources no longer exist.
func (g *GCLB) CheckResourceDeletionByProtocol(ctx context.Context, c cloud.Cloud, options *GCLBDeleteOptions, protocol Protocol) error {
	var resources []meta.Key

	for k, gfr := range g.ForwardingRule {
		// Check if forwarding rule matches given protocol.
		if gfrProtocol, err := getForwardingRuleProtocol(gfr.GA); err != nil {
			return err
		} else if gfrProtocol != protocol {
			continue
		}

		var err error
		if k.Region != "" {
			_, err = c.ForwardingRules().Get(ctx, &k)
		} else {
			_, err = c.GlobalForwardingRules().Get(ctx, &k)
		}
		if err != nil {
			if err.(*googleapi.Error) == nil || err.(*googleapi.Error).Code != http.StatusNotFound {
				return fmt.Errorf("ForwardingRule %s is not deleted/error to get: %s", k.Name, err)
			}
		} else {
			resources = append(resources, k)
		}
	}

	switch protocol {
	case HttpProtocol:
		for k := range g.TargetHTTPProxy {
			_, err := c.TargetHttpProxies().Get(ctx, &k)
			if err != nil {
				if err.(*googleapi.Error) == nil || err.(*googleapi.Error).Code != http.StatusNotFound {
					return fmt.Errorf("TargetHTTPProxy %s is not deleted/error to get: %s", k.Name, err)
				}
			} else {
				resources = append(resources, k)
			}
		}
	case HttpsProtocol:
		for k := range g.TargetHTTPSProxy {
			_, err := c.TargetHttpsProxies().Get(ctx, &k)
			if err != nil {
				if err.(*googleapi.Error) == nil || err.(*googleapi.Error).Code != http.StatusNotFound {
					return fmt.Errorf("TargetHTTPSProxy %s is not deleted/error to get: %s", k.Name, err)
				}
			} else {
				resources = append(resources, k)
			}
		}
	default:
		return fmt.Errorf("invalid protocol %q", protocol)
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

// getForwardingRuleProtocol returns the protocol for given forwarding rule.
func getForwardingRuleProtocol(forwardingRule *compute.ForwardingRule) (Protocol, error) {
	resID, err := cloud.ParseResourceURL(forwardingRule.Target)
	if err != nil {
		return "", fmt.Errorf("error parsing Target (%q): %v", forwardingRule.Target, err)
	}
	switch resID.Resource {
	case targetHTTPProxyResource:
		return HttpProtocol, nil
	case targetHTTPSProxyResource:
		return HttpsProtocol, nil
	default:
		return "", fmt.Errorf("unhandled resource %q", resID.Resource)
	}
}

// CheckNEGDeletion checks that all NEGs associated with the GCLB have been deleted
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

// CheckRedirectURLMapDeletion checks that the Redirect URL map associated with the GCLB is deleted
// This assumes that there is only one redirect url map
func (g *GCLB) CheckRedirectUrlMapDeletion(ctx context.Context, c cloud.Cloud) error {
	for k := range g.URLMap {
		if strings.Contains(k.Name, "-rm") {
			_, err := c.UrlMaps().Get(ctx, &k)
			if err != nil {
				if err.(*googleapi.Error) == nil || err.(*googleapi.Error).Code != http.StatusNotFound {
					return err
				}
			} else {
				return fmt.Errorf("Redirect URL Map still exists: %v", k)
			}
		}
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

type GCLBForVIPParams struct {
	VIP        string
	Region     string
	Network    string
	Validators []FeatureValidator
}

// GCLBForVIP retrieves all of the resources associated with the GCLB for a given VIP.
func GCLBForVIP(ctx context.Context, c cloud.Cloud, params *GCLBForVIPParams) (*GCLB, error) {
	gclb := NewGCLB(params.VIP)

	if params.Region != "" {
		err := RegionalGCLBForVIP(ctx, c, gclb, params)
		return gclb, err
	}

	allGFRs, err := c.GlobalForwardingRules().List(ctx, filter.None)
	if err != nil {
		klog.Warningf("Error listing forwarding rules: %v", err)
		return nil, err
	}

	var gfrs []*compute.ForwardingRule
	for _, gfr := range allGFRs {
		if gfr.IPAddress == params.VIP {
			gfrs = append(gfrs, gfr)
		}
	}

	// Return immediately if there are no forwarding rules exist.
	if len(gfrs) == 0 {
		klog.Warningf("No global forwarding rules found, can't get all GCLB resources")
		return gclb, nil
	}

	var urlMapKey *meta.Key
	for _, gfr := range gfrs {
		frKey := meta.GlobalKey(gfr.Name)
		gclb.ForwardingRule[*frKey] = &ForwardingRule{GA: gfr}
		if hasAlphaResource("forwardingRule", params.Validators) {
			fr, err := c.AlphaForwardingRules().Get(ctx, frKey)
			if err != nil {
				klog.Warningf("Error getting alpha forwarding rules: %v", err)
				return nil, err
			}
			gclb.ForwardingRule[*frKey].Alpha = fr
		}
		if hasBetaResource("forwardingRule", params.Validators) {
			return nil, errors.New("unsupported forwardingRule version")
		}

		// ForwardingRule => TargetProxy
		resID, err := cloud.ParseResourceURL(gfr.Target)
		if err != nil {
			klog.Warningf("Error parsing Target (%q): %v", gfr.Target, err)
			return nil, err
		}
		switch resID.Resource {
		case targetHTTPProxyResource:
			p, err := c.TargetHttpProxies().Get(ctx, resID.Key)
			if err != nil {
				klog.Warningf("Error getting TargetHttpProxy %s: %v", resID.Key, err)
				return nil, err
			}
			gclb.TargetHTTPProxy[*resID.Key] = &TargetHTTPProxy{GA: p}
			if hasAlphaResource("targetHttpProxy", params.Validators) || hasBetaResource("targetHttpProxy", params.Validators) {
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
		case targetHTTPSProxyResource:
			p, err := c.TargetHttpsProxies().Get(ctx, resID.Key)
			if err != nil {
				klog.Warningf("Error getting targetHttpsProxy (%s): %v", resID.Key, err)
				return nil, err
			}
			gclb.TargetHTTPSProxy[*resID.Key] = &TargetHTTPSProxy{GA: p}
			if hasAlphaResource("targetHttpsProxy", params.Validators) || hasBetaResource("targetHttpsProxy", params.Validators) {
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
			// Ignore redirect urlmaps since they will not have backends, but add them to the gclb map
			if strings.Contains(urlMapKey.Name, "-rm-") {
				urlMap, err := c.UrlMaps().Get(ctx, urlMapKey)
				if err != nil {
					return nil, err
				}
				gclb.URLMap[*urlMapKey] = &URLMap{GA: urlMap}
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
	if hasAlphaResource("urlMap", params.Validators) || hasBetaResource("urlMap", params.Validators) {
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

		if hasAlphaResource("backendService", params.Validators) {
			bs, err := c.AlphaBackendServices().Get(ctx, bsKey)
			if err != nil {
				return nil, err
			}
			gclb.BackendService[*bsKey].Alpha = bs
		}
		if hasBetaResource("backendService", params.Validators) {
			bs, err := c.BetaBackendServices().Get(ctx, bsKey)
			if err != nil {
				return nil, err
			}
			gclb.BackendService[*bsKey].Beta = bs
		}

		for _, hcURL := range bs.HealthChecks {
			rID, err := cloud.ParseResourceURL(hcURL)
			if err != nil {
				return nil, err
			}
			hc, err := c.HealthChecks().Get(ctx, rID.Key)
			if err != nil {
				return nil, err
			}
			gclb.HealthCheck[*rID.Key] = &HealthCheck{
				GA: hc,
			}
		}
	}

	var negKeys []*meta.Key
	var igKeys []*meta.Key
	// Fetch NEG Backends
	for _, bsKey := range bsKeys {
		var beGroups []string
		if hasAlphaResource("backendService", params.Validators) {
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
		if hasAlphaResource(NegResourceType, params.Validators) {
			neg, err := c.AlphaNetworkEndpointGroups().Get(ctx, negKey)
			if err != nil {
				return nil, err
			}
			gclb.NetworkEndpointGroup[*negKey].Alpha = neg
		}
		if hasBetaResource(NegResourceType, params.Validators) {
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

// RegionalGCLBForVIP retrieves all of the resources associated with the GCLB for a given VIP.
func RegionalGCLBForVIP(ctx context.Context, c cloud.Cloud, gclb *GCLB, params *GCLBForVIPParams) error {
	allRFRs, err := c.ForwardingRules().List(ctx, params.Region, filter.None)
	if err != nil {
		klog.Warningf("Error listing forwarding rules: %v", err)
		return err
	}

	var rfrs []*compute.ForwardingRule
	for _, rfr := range allRFRs {
		netResID, err := cloud.ParseResourceURL(rfr.Network)
		if err != nil {
			klog.Warningf("Error parsing Network (%q): %v", rfr.Network, err)
			return err
		}
		if rfr.IPAddress == params.VIP && netResID.Key.Name == params.Network {
			rfrs = append(rfrs, rfr)
		}
	}

	if len(rfrs) == 0 {
		klog.Warningf("No regional forwarding rules found, can't get all GCLB resources")
		return nil
	}

	var urlMapKey *meta.Key
	for _, rfr := range rfrs {
		frKey := meta.RegionalKey(rfr.Name, params.Region)
		gclb.ForwardingRule[*frKey] = &ForwardingRule{GA: rfr}
		if hasAlphaResource("forwardingRule", params.Validators) {
			fr, err := c.AlphaForwardingRules().Get(ctx, frKey)
			if err != nil {
				klog.Warningf("Error getting alpha forwarding rules: %v", err)
				return err
			}
			gclb.ForwardingRule[*frKey].Alpha = fr
		}
		if hasBetaResource("forwardingRule", params.Validators) {
			fr, err := c.BetaForwardingRules().Get(ctx, frKey)
			if err != nil {
				klog.Warningf("Error getting alpha forwarding rules: %v", err)
				return err
			}
			gclb.ForwardingRule[*frKey].Beta = fr
		}

		// ForwardingRule => TargetProxy
		resID, err := cloud.ParseResourceURL(rfr.Target)
		if err != nil {
			klog.Warningf("Error parsing Target (%q): %v", rfr.Target, err)
			return err
		}
		switch resID.Resource {
		case "targetHttpProxies":
			// Use beta by default since not GA yet
			p, err := c.BetaRegionTargetHttpProxies().Get(ctx, resID.Key)
			if err != nil {
				klog.Warningf("Error getting TargetHttpProxy %s: %v", resID.Key, err)
				return err
			}
			gclb.TargetHTTPProxy[*resID.Key] = &TargetHTTPProxy{Beta: p}
			if hasAlphaResource("targetHttpProxy", params.Validators) || hasBetaResource("targetHttpProxy", params.Validators) {
				return errors.New("unsupported targetHttpProxy version")
			}

			urlMapResID, err := cloud.ParseResourceURL(p.UrlMap)
			if err != nil {
				klog.Warningf("Error parsing urlmap URL (%q): %v", p.UrlMap, err)
				return err
			}
			if urlMapKey == nil {
				urlMapKey = urlMapResID.Key
			}
			if *urlMapKey != *urlMapResID.Key {
				klog.Warningf("Error targetHttpProxy references are not the same (%s != %s)", *urlMapKey, *urlMapResID.Key)
				return fmt.Errorf("targetHttpProxy references are not the same: %+v != %+v", *urlMapKey, *urlMapResID.Key)
			}
		case "targetHttpsProxies":
			// Use Beta by default since not GA yet
			p, err := c.BetaRegionTargetHttpsProxies().Get(ctx, resID.Key)
			if err != nil {
				klog.Warningf("Error getting targetHttpsProxy (%s): %v", resID.Key, err)
				return err
			}
			gclb.TargetHTTPSProxy[*resID.Key] = &TargetHTTPSProxy{Beta: p}
			if hasAlphaResource("targetHttpsProxy", params.Validators) || hasBetaResource("targetHttpsProxy", params.Validators) {
				return errors.New("unsupported targetHttpsProxy version")
			}

			urlMapResID, err := cloud.ParseResourceURL(p.UrlMap)
			if err != nil {
				klog.Warningf("Error parsing urlmap URL (%q): %v", p.UrlMap, err)
				return err
			}
			if urlMapKey == nil {
				urlMapKey = urlMapResID.Key
			}
			if *urlMapKey != *urlMapResID.Key {
				klog.Warningf("Error targetHttpsProxy references are not the same (%s != %s)", *urlMapKey, *urlMapResID.Key)
				return fmt.Errorf("targetHttpsProxy references are not the same: %+v != %+v", *urlMapKey, *urlMapResID.Key)
			}
		default:
			klog.Errorf("Unhandled resource: %q, grf = %+v", resID.Resource, rfr)
			return fmt.Errorf("unhandled resource %q", resID.Resource)
		}
	}

	// TargetProxy => URLMap
	// Use beta since params.Region is not GA yet
	urlMap, err := c.BetaRegionUrlMaps().Get(ctx, urlMapKey)
	if err != nil {
		return err
	}
	gclb.URLMap[*urlMapKey] = &URLMap{Beta: urlMap}
	if hasAlphaResource("urlMap", params.Validators) || hasBetaResource("urlMap", params.Validators) {
		return errors.New("unsupported urlMap version")
	}

	// URLMap => BackendService(s)
	var bsKeys []*meta.Key
	resID, err := cloud.ParseResourceURL(urlMap.DefaultService)
	if err != nil {
		return err
	}
	bsKeys = append(bsKeys, resID.Key)

	for _, pm := range urlMap.PathMatchers {
		resID, err := cloud.ParseResourceURL(pm.DefaultService)
		if err != nil {
			return err
		}
		bsKeys = append(bsKeys, resID.Key)

		for _, pr := range pm.PathRules {
			resID, err := cloud.ParseResourceURL(pr.Service)
			if err != nil {
				return err
			}
			bsKeys = append(bsKeys, resID.Key)
		}
	}

	for _, bsKey := range bsKeys {
		bs, err := c.RegionBackendServices().Get(ctx, bsKey)
		if err != nil {
			return err
		}
		gclb.BackendService[*bsKey] = &BackendService{GA: bs}

		if hasAlphaResource("backendService", params.Validators) {
			bs, err := c.AlphaRegionBackendServices().Get(ctx, bsKey)
			if err != nil {
				return err
			}
			gclb.BackendService[*bsKey].Alpha = bs
		}
		if hasBetaResource("backendService", params.Validators) {
			bs, err := c.BetaRegionBackendServices().Get(ctx, bsKey)
			if err != nil {
				return err
			}
			gclb.BackendService[*bsKey].Beta = bs
		}
		for _, hcURL := range bs.HealthChecks {
			rID, err := cloud.ParseResourceURL(hcURL)
			if err != nil {
				return err
			}
			hc, err := c.RegionHealthChecks().Get(ctx, rID.Key)
			if err != nil {
				return err
			}
			gclb.HealthCheck[*rID.Key] = &HealthCheck{
				GA: hc,
			}
		}
	}

	var negKeys []*meta.Key
	var igKeys []*meta.Key
	// Fetch NEG Backends
	for _, bsKey := range bsKeys {
		var beGroups []string
		if hasAlphaResource("backendService", params.Validators) {
			bs, err := c.AlphaRegionBackendServices().Get(ctx, bsKey)
			if err != nil {
				return err
			}
			for _, be := range bs.Backends {
				beGroups = append(beGroups, be.Group)
			}
		} else {
			bs, err := c.BetaRegionBackendServices().Get(ctx, bsKey)
			if err != nil {
				return err
			}
			for _, be := range bs.Backends {
				beGroups = append(beGroups, be.Group)
			}
		}
		for _, group := range beGroups {
			if strings.Contains(group, NegResourceType) {
				resourceId, err := cloud.ParseResourceURL(group)
				if err != nil {
					return err
				}
				negKeys = append(negKeys, resourceId.Key)
			}

			if strings.Contains(group, IgResourceType) {
				resourceId, err := cloud.ParseResourceURL(group)
				if err != nil {
					return err
				}
				igKeys = append(igKeys, resourceId.Key)
			}

		}
	}

	for _, negKey := range negKeys {
		neg, err := c.NetworkEndpointGroups().Get(ctx, negKey)
		if err != nil {
			return err
		}
		gclb.NetworkEndpointGroup[*negKey] = &NetworkEndpointGroup{GA: neg}
		if hasAlphaResource(NegResourceType, params.Validators) {
			neg, err := c.AlphaNetworkEndpointGroups().Get(ctx, negKey)
			if err != nil {
				return err
			}
			gclb.NetworkEndpointGroup[*negKey].Alpha = neg
		}
		if hasBetaResource(NegResourceType, params.Validators) {
			neg, err := c.BetaNetworkEndpointGroups().Get(ctx, negKey)
			if err != nil {
				return err
			}
			gclb.NetworkEndpointGroup[*negKey].Beta = neg
		}
	}

	for _, igKey := range igKeys {
		ig, err := c.InstanceGroups().Get(ctx, igKey)
		if err != nil {
			return err
		}
		gclb.InstanceGroup[*igKey] = &InstanceGroup{GA: ig}
	}

	return err
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

// CheckStandaloneNEGDeletion checks that specified NEG has been deleted
func CheckStandaloneNEGDeletion(ctx context.Context, c cloud.Cloud, negName, port string, zones []string) (bool, error) {
	var foundNegs []string

	for _, zone := range zones {
		key := meta.ZonalKey(negName, zone)
		neg, err := c.NetworkEndpointGroups().Get(ctx, key)
		if err != nil {
			if e, ok := err.(*googleapi.Error); ok && e.Code == http.StatusNotFound {
				continue
			}
			return false, err
		}

		if neg.Description != "" {
			desc, err := utils.NegDescriptionFromString(neg.Description)
			if err == nil && desc.Port != port {
				continue
			}

			if err != nil {
				return false, err
			}
		}
		foundNegs = append(foundNegs, negName)
	}

	if len(foundNegs) != 0 {
		klog.Infof("CheckStandaloneNEGDeletion(), expected neg %s not to exist", negName)
		return false, nil
	}

	return true, nil
}

// GetServiceAttachment gets the GCE service attachment
func GetServiceAttachment(ctx context.Context, c cloud.Cloud, saURL string) (*ServiceAttachment, error) {
	resID, err := cloud.ParseResourceURL(saURL)
	if err != nil {
		return nil, err
	}

	sa, err := c.BetaServiceAttachments().Get(ctx, resID.Key)
	if err != nil {
		return nil, err
	}
	return &ServiceAttachment{Beta: sa}, nil
}

// CheckServiceAttachmentDeletion verfies that the Service Attachment does not exist
func CheckServiceAttachmentDeletion(ctx context.Context, c cloud.Cloud, saURL string) (bool, error) {
	resID, err := cloud.ParseResourceURL(saURL)
	if err != nil {
		klog.Infof("CheckServiceAttachmentDeletion(), failed")
		return false, err
	}
	_, err = c.BetaServiceAttachments().Get(ctx, resID.Key)
	if e, ok := err.(*googleapi.Error); ok && e.Code == http.StatusNotFound {
		klog.Infof("CheckServiceAttachmnetDeletion(), service attachment was successfully deleted")
		return true, nil
	}
	return false, err
}

// GetForwardingRule returns the GCE Forwarding Rule
func GetForwardingRule(ctx context.Context, c cloud.Cloud, frURL string) (*ForwardingRule, error) {

	resID, err := cloud.ParseResourceURL(frURL)
	if err != nil {
		return nil, err
	}

	fr, err := c.ForwardingRules().Get(ctx, resID.Key)
	if err != nil {
		return nil, err
	}
	return &ForwardingRule{GA: fr}, nil
}
