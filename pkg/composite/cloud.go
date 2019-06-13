package composite

import (
	"fmt"
	cloudprovider "github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	computealpha "google.golang.org/api/compute/v0.alpha"
	computebeta "google.golang.org/api/compute/v0.beta"
	"google.golang.org/api/compute/v1"
	"k8s.io/ingress-gce/pkg/composite/metrics"
	"k8s.io/klog"
	"k8s.io/kubernetes/pkg/cloudprovider/providers/gce"
)

type Cloud struct {
	// TODO: (shance) remove this dependency
	// Keep a copy of gce.Cloud around to make it easier to implement the loadbalancer interface
	// This will aid in the transition of removing gceCloud entirely
	gceCloud *gce.Cloud
}

func NewCloud(c interface{}) *Cloud {
	gce, ok := c.(*gce.Cloud)
	if ok && gce != nil {
		return &Cloud{gce}
	}

	return &Cloud{}
}

func (c *Cloud) GceCloud() *gce.Cloud {
	return c.gceCloud
}

func (c *Cloud) CreateKey(name string, regional bool) *meta.Key {
	region := c.gceCloud.Region()
	if regional && region != "" {
		return meta.RegionalKey(name, region)
	}
	return meta.GlobalKey(name)
}

// TODO: (shance) generate this
// ListAllUrlMaps merges all possible List() calls into one list of composite UrlMaps
func (c *Cloud) ListAllUrlMaps() ([]*UrlMap, error) {
	resultMap := map[string]*UrlMap{}
	keys := []*meta.Key{c.CreateKey("", false), c.CreateKey("", true)}

	for _, version := range meta.AllVersions {
		for _, key := range keys {
			list, err := c.ListUrlMaps(version, key)
			if err != nil {
				return nil, fmt.Errorf("error listing all urlmaps: %v", err)
			}
			for _, um := range list {
				resultMap[um.Name] = um
			}
		}
	}

	// Convert map to slice
	result := []*UrlMap{}
	for _, um := range resultMap {
		result = append(result, um)
	}

	return result, nil
}

// TODO: (shance) generate this
// ListAllUrlMaps merges all possible List() calls into one list of composite UrlMaps
func (c *Cloud) ListAllBackendServices() ([]*BackendService, error) {
	resultMap := map[string]*BackendService{}
	keys := []*meta.Key{c.CreateKey("", false), c.CreateKey("", true)}

	for _, version := range meta.AllVersions {
		for _, key := range keys {
			list, err := c.ListBackendServices(version, key)
			if err != nil {
				return nil, fmt.Errorf("error listing all urlmaps: %v", err)
			}
			for _, bs := range list {
				resultMap[bs.Name] = bs
			}
		}
	}

	// Convert map to slice
	result := []*BackendService{}
	for _, bs := range resultMap {
		result = append(result, bs)
	}

	return result, nil
}

func IsRegionalUrlMap(um *UrlMap) (bool, error) {
	if um != nil {
		return IsRegionalResource(um.SelfLink)
	}
	return false, nil
}

func IsRegionalResource(selfLink string) (bool, error) {
	// Figure out if cluster is regional
	// Update L7 if its regional so we can delete the right resources
	resourceID, err := cloudprovider.ParseResourceURL(selfLink)
	if err != nil {
		return false, fmt.Errorf("error parsing self-link for url-map %s: %v", selfLink, err)
	}

	if resourceID.Key.Region != "" {
		return true, nil
	}

	return false, nil
}

// Below functions are simply wrapped from gceCloud
// TODO: (shance) find a way to remove these
func (c *Cloud) ReserveGlobalAddress(addr *compute.Address) error {
	return c.gceCloud.ReserveGlobalAddress(addr)
}

func (c *Cloud) GetGlobalAddress(name string) (*compute.Address, error) {
	return c.gceCloud.GetGlobalAddress(name)
}

func (c *Cloud) DeleteGlobalAddress(name string) error {
	return c.gceCloud.DeleteGlobalAddress(name)
}

// TODO: (shance) below functions should be generated
func (c *Cloud) SetUrlMapForTargetHttpsProxy(targetHttpsProxy *TargetHttpsProxy, urlMapLink string, key *meta.Key) error {
	ctx, cancel := cloudprovider.ContextWithCallTimeout()
	defer cancel()
	mc := metrics.NewMetricContext("TargetHttpsProxy", "set_url_map", key.Region, key.Zone, string(targetHttpsProxy.Version))

	// Set name in case it is not present in the key
	key.Name = targetHttpsProxy.Name
	klog.V(3).Infof("setting URLMap for TargetHttpsProxy %v", targetHttpsProxy.Name)

	switch targetHttpsProxy.Version {
	case meta.VersionAlpha:
		ref := &computealpha.UrlMapReference{UrlMap: urlMapLink}
		switch key.Type() {
		case meta.Regional:
			return mc.Observe(c.gceCloud.Compute().AlphaRegionTargetHttpsProxies().SetUrlMap(ctx, key, ref))
		default:
			return mc.Observe(c.gceCloud.Compute().AlphaTargetHttpsProxies().SetUrlMap(ctx, key, ref))
		}
	case meta.VersionBeta:
		ref := &computebeta.UrlMapReference{UrlMap: urlMapLink}
		return mc.Observe(c.gceCloud.Compute().BetaTargetHttpsProxies().SetUrlMap(ctx, key, ref))
	default:
		ref := &compute.UrlMapReference{UrlMap: urlMapLink}
		return mc.Observe(c.gceCloud.Compute().TargetHttpsProxies().SetUrlMap(ctx, key, ref))
	}
}

func (c *Cloud) SetSslCertificateForTargetHttpsProxy(targetHttpsProxy *TargetHttpsProxy, sslCertURLs []string, key *meta.Key) error {
	ctx, cancel := cloudprovider.ContextWithCallTimeout()
	defer cancel()
	mc := metrics.NewMetricContext("TargetHttpsProxy", "set_ssl_certificate", key.Region, key.Zone, string(targetHttpsProxy.Version))

	// Set name in case it is not present in the key
	key.Name = targetHttpsProxy.Name
	klog.V(3).Infof("setting URLMap for TargetHttpsProxy %v", targetHttpsProxy.Name)

	switch targetHttpsProxy.Version {
	case meta.VersionAlpha:
		switch key.Type() {
		case meta.Regional:
			req := &computealpha.RegionTargetHttpsProxiesSetSslCertificatesRequest{
				SslCertificates: sslCertURLs,
			}
			return mc.Observe(c.gceCloud.Compute().AlphaRegionTargetHttpsProxies().SetSslCertificates(ctx, key, req))
		default:
			req := &computealpha.TargetHttpsProxiesSetSslCertificatesRequest{
				SslCertificates: sslCertURLs,
			}
			return mc.Observe(c.gceCloud.Compute().AlphaTargetHttpsProxies().SetSslCertificates(ctx, key, req))
		}
	case meta.VersionBeta:
		req := &computebeta.TargetHttpsProxiesSetSslCertificatesRequest{
			SslCertificates: sslCertURLs,
		}
		return mc.Observe(c.gceCloud.Compute().BetaTargetHttpsProxies().SetSslCertificates(ctx, key, req))
	default:

		req := &compute.TargetHttpsProxiesSetSslCertificatesRequest{
			SslCertificates: sslCertURLs,
		}
		return mc.Observe(c.gceCloud.Compute().TargetHttpsProxies().SetSslCertificates(ctx, key, req))
	}
}

func (c *Cloud) SetUrlMapForTargetHttpProxy(targetHttpProxy *TargetHttpProxy, urlMapLink string, key *meta.Key) error {
	ctx, cancel := cloudprovider.ContextWithCallTimeout()
	defer cancel()
	mc := metrics.NewMetricContext("TargetHttpProxy", "set_url_map", key.Region, key.Zone, string(targetHttpProxy.Version))

	// Set name in case it is not present in the key
	key.Name = targetHttpProxy.Name
	klog.V(3).Infof("setting URLMap for TargetHttpProxy %v", targetHttpProxy.Name)

	switch targetHttpProxy.Version {
	case meta.VersionAlpha:
		ref := &computealpha.UrlMapReference{UrlMap: urlMapLink}
		switch key.Type() {
		case meta.Regional:
			return mc.Observe(c.gceCloud.Compute().AlphaRegionTargetHttpProxies().SetUrlMap(ctx, key, ref))
		default:
			return mc.Observe(c.gceCloud.Compute().AlphaTargetHttpProxies().SetUrlMap(ctx, key, ref))
		}
	case meta.VersionBeta:
		ref := &computebeta.UrlMapReference{UrlMap: urlMapLink}
		return mc.Observe(c.gceCloud.Compute().BetaTargetHttpProxies().SetUrlMap(ctx, key, ref))
	default:
		ref := &compute.UrlMapReference{UrlMap: urlMapLink}
		return mc.Observe(c.gceCloud.Compute().TargetHttpProxies().SetUrlMap(ctx, key, ref))
	}
}

func (c *Cloud) SetProxyForForwardingRule(forwardingRule *ForwardingRule, key *meta.Key, targetProxyLink string) error {
	ctx, cancel := cloudprovider.ContextWithCallTimeout()
	defer cancel()
	mc := metrics.NewMetricContext("ForwardingRule", "set_proxy", key.Region, key.Zone, string(forwardingRule.Version))

	// Set name in case it is not present in the key
	key.Name = forwardingRule.Name
	klog.V(3).Infof("setting proxy for forwarding rule ForwardingRule %v", forwardingRule.Name)

	switch forwardingRule.Version {
	case meta.VersionAlpha:
		target := &computealpha.TargetReference{Target: targetProxyLink}
		switch key.Type() {
		case meta.Regional:
			return mc.Observe(c.gceCloud.Compute().AlphaForwardingRules().SetTarget(ctx, key, target))
		default:
			return mc.Observe(c.gceCloud.Compute().AlphaGlobalForwardingRules().SetTarget(ctx, key, target))
		}
	case meta.VersionBeta:
		target := &computebeta.TargetReference{Target: targetProxyLink}
		switch key.Type() {
		case meta.Regional:
			return mc.Observe(c.gceCloud.Compute().BetaForwardingRules().SetTarget(ctx, key, target))
		default:
			return mc.Observe(c.gceCloud.Compute().BetaGlobalForwardingRules().SetTarget(ctx, key, target))
		}
	default:
		target := &compute.TargetReference{Target: targetProxyLink}
		switch key.Type() {
		case meta.Regional:
			return mc.Observe(c.gceCloud.Compute().ForwardingRules().SetTarget(ctx, key, target))
		default:
			return mc.Observe(c.gceCloud.Compute().GlobalForwardingRules().SetTarget(ctx, key, target))
		}
	}
}
