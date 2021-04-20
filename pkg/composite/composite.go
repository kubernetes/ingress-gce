/*
Copyright 2019 The Kubernetes Authors.

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

// This file includes all the handwritten functions from the composite library
package composite

import (
	"fmt"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	computealpha "google.golang.org/api/compute/v0.alpha"
	computebeta "google.golang.org/api/compute/v0.beta"
	"google.golang.org/api/compute/v1"
	"k8s.io/ingress-gce/pkg/composite/metrics"
	"k8s.io/klog"
	"k8s.io/legacy-cloud-providers/gce"
)

// SetUrlMapForTargetHttpsProxy() sets the UrlMap for a target https proxy
func SetUrlMapForTargetHttpsProxy(gceCloud *gce.Cloud, key *meta.Key, targetHttpsProxy *TargetHttpsProxy, urlMapLink string) error {
	ctx, cancel := cloud.ContextWithCallTimeout()
	defer cancel()
	mc := metrics.NewMetricContext("TargetHttpsProxy", "set_url_map", key.Region, key.Zone, string(targetHttpsProxy.Version))

	// Set name in case it is not present in the key
	key.Name = targetHttpsProxy.Name
	klog.V(3).Infof("setting URLMap for TargetHttpsProxy %v", key)

	switch targetHttpsProxy.Version {
	case meta.VersionAlpha:
		ref := &computealpha.UrlMapReference{UrlMap: urlMapLink}
		switch key.Type() {
		case meta.Regional:
			return mc.Observe(gceCloud.Compute().AlphaRegionTargetHttpsProxies().SetUrlMap(ctx, key, ref))
		default:
			return mc.Observe(gceCloud.Compute().AlphaTargetHttpsProxies().SetUrlMap(ctx, key, ref))
		}
	case meta.VersionBeta:
		ref := &computebeta.UrlMapReference{UrlMap: urlMapLink}
		switch key.Type() {
		case meta.Regional:
			return mc.Observe(gceCloud.Compute().BetaRegionTargetHttpsProxies().SetUrlMap(ctx, key, ref))
		default:
			return mc.Observe(gceCloud.Compute().BetaTargetHttpsProxies().SetUrlMap(ctx, key, ref))
		}
	default:
		ref := &compute.UrlMapReference{UrlMap: urlMapLink}
		switch key.Type() {
		case meta.Regional:
			return mc.Observe(gceCloud.Compute().RegionTargetHttpsProxies().SetUrlMap(ctx, key, ref))
		default:
			return mc.Observe(gceCloud.Compute().TargetHttpsProxies().SetUrlMap(ctx, key, ref))
		}
	}
}

// SetSslCertificateForTargetHttpsProxy() sets the SSL Certificate for a target https proxy
func SetSslCertificateForTargetHttpsProxy(gceCloud *gce.Cloud, key *meta.Key, targetHttpsProxy *TargetHttpsProxy, sslCertURLs []string) error {
	ctx, cancel := cloud.ContextWithCallTimeout()
	defer cancel()
	mc := metrics.NewMetricContext("TargetHttpsProxy", "set_ssl_certificate", key.Region, key.Zone, string(targetHttpsProxy.Version))

	// Set name in case it is not present in the key
	key.Name = targetHttpsProxy.Name
	klog.V(3).Infof("setting SslCertificate for TargetHttpsProxy %v", key)

	switch targetHttpsProxy.Version {
	case meta.VersionAlpha:
		switch key.Type() {
		case meta.Regional:
			req := &computealpha.RegionTargetHttpsProxiesSetSslCertificatesRequest{SslCertificates: sslCertURLs}
			return mc.Observe(gceCloud.Compute().AlphaRegionTargetHttpsProxies().SetSslCertificates(ctx, key, req))
		default:
			req := &computealpha.TargetHttpsProxiesSetSslCertificatesRequest{SslCertificates: sslCertURLs}
			return mc.Observe(gceCloud.Compute().AlphaTargetHttpsProxies().SetSslCertificates(ctx, key, req))
		}
	case meta.VersionBeta:
		switch key.Type() {
		case meta.Regional:
			req := &computebeta.RegionTargetHttpsProxiesSetSslCertificatesRequest{SslCertificates: sslCertURLs}
			return mc.Observe(gceCloud.Compute().BetaRegionTargetHttpsProxies().SetSslCertificates(ctx, key, req))
		default:
			req := &computebeta.TargetHttpsProxiesSetSslCertificatesRequest{SslCertificates: sslCertURLs}
			return mc.Observe(gceCloud.Compute().BetaTargetHttpsProxies().SetSslCertificates(ctx, key, req))
		}
	default:
		switch key.Type() {
		case meta.Regional:
			req := &compute.RegionTargetHttpsProxiesSetSslCertificatesRequest{SslCertificates: sslCertURLs}
			return mc.Observe(gceCloud.Compute().RegionTargetHttpsProxies().SetSslCertificates(ctx, key, req))
		default:
			req := &compute.TargetHttpsProxiesSetSslCertificatesRequest{SslCertificates: sslCertURLs}
			return mc.Observe(gceCloud.Compute().TargetHttpsProxies().SetSslCertificates(ctx, key, req))
		}
	}
}

// SetSslPolicyForTargetHttpsProxy() sets the url map for a target proxy
func SetSslPolicyForTargetHttpsProxy(gceCloud *gce.Cloud, key *meta.Key, targetHttpsProxy *TargetHttpsProxy, SslPolicyLink string) error {
	ctx, cancel := cloud.ContextWithCallTimeout()
	defer cancel()
	mc := metrics.NewMetricContext("TargetHttpProxy", "set_url_map", key.Region, key.Zone, string(targetHttpsProxy.Version))

	// Set name in case it is not present in the key
	key.Name = targetHttpsProxy.Name
	klog.V(3).Infof("Setting SslPolicy for TargetHttpProxy %v", key)

	switch targetHttpsProxy.Version {
	case meta.VersionAlpha:
		ref := &computealpha.SslPolicyReference{SslPolicy: SslPolicyLink}
		switch key.Type() {
		case meta.Regional:
			return fmt.Errorf("SetSslPolicy() is not supported for regional Target Https Proxies")
		default:
			return mc.Observe(gceCloud.Compute().AlphaTargetHttpsProxies().SetSslPolicy(ctx, key, ref))
		}
	case meta.VersionBeta:
		ref := &computebeta.SslPolicyReference{SslPolicy: SslPolicyLink}
		switch key.Type() {
		case meta.Regional:
			return fmt.Errorf("SetSslPolicy() is not supported for regional Target Https Proxies")
		default:
			return mc.Observe(gceCloud.Compute().BetaTargetHttpsProxies().SetSslPolicy(ctx, key, ref))
		}
	default:
		ref := &compute.SslPolicyReference{SslPolicy: SslPolicyLink}
		switch key.Type() {
		case meta.Regional:
			return fmt.Errorf("SetSslPolicy() is not supported for regional Target Https Proxies")
		default:
			return mc.Observe(gceCloud.Compute().TargetHttpsProxies().SetSslPolicy(ctx, key, ref))
		}
	}
}

// SetUrlMapForTargetHttpProxy() sets the url map for a target proxy
func SetUrlMapForTargetHttpProxy(gceCloud *gce.Cloud, key *meta.Key, targetHttpProxy *TargetHttpProxy, urlMapLink string) error {
	ctx, cancel := cloud.ContextWithCallTimeout()
	defer cancel()
	mc := metrics.NewMetricContext("TargetHttpProxy", "set_url_map", key.Region, key.Zone, string(targetHttpProxy.Version))

	// Set name in case it is not present in the key
	key.Name = targetHttpProxy.Name
	klog.V(3).Infof("setting URLMap for TargetHttpProxy %v", key)

	switch targetHttpProxy.Version {
	case meta.VersionAlpha:
		ref := &computealpha.UrlMapReference{UrlMap: urlMapLink}
		switch key.Type() {
		case meta.Regional:
			return mc.Observe(gceCloud.Compute().AlphaRegionTargetHttpProxies().SetUrlMap(ctx, key, ref))
		default:
			return mc.Observe(gceCloud.Compute().AlphaTargetHttpProxies().SetUrlMap(ctx, key, ref))
		}
	case meta.VersionBeta:
		ref := &computebeta.UrlMapReference{UrlMap: urlMapLink}
		switch key.Type() {
		case meta.Regional:
			return mc.Observe(gceCloud.Compute().BetaRegionTargetHttpProxies().SetUrlMap(ctx, key, ref))
		default:
			return mc.Observe(gceCloud.Compute().BetaTargetHttpProxies().SetUrlMap(ctx, key, ref))
		}
	default:
		ref := &compute.UrlMapReference{UrlMap: urlMapLink}
		switch key.Type() {
		case meta.Regional:
			return mc.Observe(gceCloud.Compute().RegionTargetHttpProxies().SetUrlMap(ctx, key, ref))
		default:
			return mc.Observe(gceCloud.Compute().TargetHttpProxies().SetUrlMap(ctx, key, ref))
		}
	}
}

// SetProxyForForwardingRule() sets the target proxy for a forwarding rule
func SetProxyForForwardingRule(gceCloud *gce.Cloud, key *meta.Key, forwardingRule *ForwardingRule, targetProxyLink string) error {
	ctx, cancel := cloud.ContextWithCallTimeout()
	defer cancel()
	mc := metrics.NewMetricContext("ForwardingRule", "set_proxy", key.Region, key.Zone, string(forwardingRule.Version))

	// Set name in case it is not present in the key
	key.Name = forwardingRule.Name
	klog.V(3).Infof("setting proxy for forwarding rule ForwardingRule %v", key)

	switch forwardingRule.Version {
	case meta.VersionAlpha:
		target := &computealpha.TargetReference{Target: targetProxyLink}
		switch key.Type() {
		case meta.Regional:
			return mc.Observe(gceCloud.Compute().AlphaForwardingRules().SetTarget(ctx, key, target))
		default:
			return mc.Observe(gceCloud.Compute().AlphaGlobalForwardingRules().SetTarget(ctx, key, target))
		}
	case meta.VersionBeta:
		target := &computebeta.TargetReference{Target: targetProxyLink}
		switch key.Type() {
		case meta.Regional:
			return mc.Observe(gceCloud.Compute().BetaForwardingRules().SetTarget(ctx, key, target))
		default:
			return mc.Observe(gceCloud.Compute().BetaGlobalForwardingRules().SetTarget(ctx, key, target))
		}
	default:
		target := &compute.TargetReference{Target: targetProxyLink}
		switch key.Type() {
		case meta.Regional:
			return mc.Observe(gceCloud.Compute().ForwardingRules().SetTarget(ctx, key, target))
		default:
			return mc.Observe(gceCloud.Compute().GlobalForwardingRules().SetTarget(ctx, key, target))
		}
	}
}

// SetSecurityPolicy sets the cloud armor security policy for a backend service.
func SetSecurityPolicy(gceCloud *gce.Cloud, backendService *BackendService, securityPolicy string) error {
	key := meta.GlobalKey(backendService.Name)
	if backendService.Scope != meta.Global {
		return fmt.Errorf("cloud armor security policies not supported for %s backend service %s", backendService.Scope, backendService.Name)
	}

	ctx, cancel := cloud.ContextWithCallTimeout()
	defer cancel()
	mc := metrics.NewMetricContext("BackendService", "set_security_policy", key.Region, key.Zone, string(backendService.Version))

	switch backendService.Version {
	case meta.VersionAlpha:
		var ref *computealpha.SecurityPolicyReference
		if securityPolicy != "" {
			securityPolicyLink := cloud.SelfLink(meta.VersionAlpha, gceCloud.ProjectID(), "securityPolicies", meta.GlobalKey(securityPolicy))
			ref = &computealpha.SecurityPolicyReference{SecurityPolicy: securityPolicyLink}
		}
		return mc.Observe(gceCloud.Compute().AlphaBackendServices().SetSecurityPolicy(ctx, key, ref))
	case meta.VersionBeta:
		var ref *computebeta.SecurityPolicyReference
		if securityPolicy != "" {
			securityPolicyLink := cloud.SelfLink(meta.VersionBeta, gceCloud.ProjectID(), "securityPolicies", meta.GlobalKey(securityPolicy))
			ref = &computebeta.SecurityPolicyReference{SecurityPolicy: securityPolicyLink}
		}
		return mc.Observe(gceCloud.Compute().BetaBackendServices().SetSecurityPolicy(ctx, key, ref))
	default:
		var ref *compute.SecurityPolicyReference
		if securityPolicy != "" {
			securityPolicyLink := cloud.SelfLink(meta.VersionGA, gceCloud.ProjectID(), "securityPolicies", meta.GlobalKey(securityPolicy))
			ref = &compute.SecurityPolicyReference{SecurityPolicy: securityPolicyLink}
		}
		return mc.Observe(gceCloud.Compute().BackendServices().SetSecurityPolicy(ctx, key, ref))
	}
}
