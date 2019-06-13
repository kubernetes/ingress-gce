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

package loadbalancers

import (
	"fmt"
	"k8s.io/ingress-gce/pkg/composite"

	"k8s.io/klog"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"google.golang.org/api/compute/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	"k8s.io/ingress-gce/pkg/utils"
)

const FakeCertQuota = 15
const FakeRegion = "us-fake1"

var testIPManager = testIP{}

type testIP struct {
	start int
}

func (t *testIP) ip() string {
	t.start++
	return fmt.Sprintf("0.0.0.%v", t.start)
}

// Loadbalancer fakes

// FakeLoadBalancers is a type that fakes out the loadbalancer interface.
type FakeLoadBalancers struct {
	Fw    []*composite.ForwardingRule
	Um    []*composite.UrlMap
	Tp    []*composite.TargetHttpProxy
	Tps   []*composite.TargetHttpsProxy
	IP    []*compute.Address
	Certs []*composite.SslCertificate
	name  string
	calls []string // list of calls that were made

	namer *utils.Namer

	Version      meta.Version
	ResourceType meta.KeyType
}

// CreateKey implements LoadBalancer
func (f *FakeLoadBalancers) CreateKey(name string, regional bool) *meta.Key {
	if regional {
		return meta.RegionalKey(name, FakeRegion)
	}
	return meta.GlobalKey(name)
}

// FWName returns the name of the firewall given the protocol.
//
// TODO: There is some duplication between these functions and the name mungers in
// loadbalancer file.
func (f *FakeLoadBalancers) FWName(https bool) string {
	proto := utils.HTTPProtocol
	if https {
		proto = utils.HTTPSProtocol
	}
	return f.namer.ForwardingRule(f.name, proto)
}

func (f *FakeLoadBalancers) UMName() string {
	return f.namer.UrlMap(f.name)
}

func (f *FakeLoadBalancers) TPName(https bool) string {
	protocol := utils.HTTPProtocol
	if https {
		protocol = utils.HTTPSProtocol
	}
	return f.namer.TargetProxy(f.name, protocol)
}

// String is the string method for FakeLoadBalancers.
func (f *FakeLoadBalancers) String() string {
	msg := fmt.Sprintf(
		"Loadbalancer %v,\nforwarding rules:\n", f.name)
	for _, fw := range f.Fw {
		msg += fmt.Sprintf("\t%v\n", fw.Name)
	}
	msg += fmt.Sprintf("Target proxies\n")
	for _, tp := range f.Tp {
		msg += fmt.Sprintf("\t%v\n", tp.Name)
	}
	msg += fmt.Sprintf("UrlMaps\n")
	for _, um := range f.Um {
		msg += fmt.Sprintf("%v\n", um.Name)
		msg += fmt.Sprintf("\tHost Rules:\n")
		for _, hostRule := range um.HostRules {
			msg += fmt.Sprintf("\t\t%v\n", hostRule)
		}
		msg += fmt.Sprintf("\tPath Matcher:\n")
		for _, pathMatcher := range um.PathMatchers {
			msg += fmt.Sprintf("\t\t%v\n", pathMatcher.Name)
			for _, pathRule := range pathMatcher.PathRules {
				msg += fmt.Sprintf("\t\t\t%+v\n", pathRule)
			}
		}
	}
	msg += "Certificates:\n"
	for _, cert := range f.Certs {
		msg += fmt.Sprintf("\t%+v\n", cert)
	}
	return msg
}

// Forwarding Rule fakes

// GetGlobalForwardingRule returns a fake forwarding rule.
func (f *FakeLoadBalancers) GetForwardingRule(version meta.Version, key *meta.Key) (*composite.ForwardingRule, error) {
	name := key.Name
	f.calls = append(f.calls, "GetForwardingRule")
	for i, fw := range f.Fw {
		if fw.Name == name && fw.Version == version && fw.ResourceType == key.Type() {
			return f.Fw[i], nil
		}
	}
	return nil, utils.FakeGoogleAPINotFoundErr()
}

func (f *FakeLoadBalancers) ListForwardingRules(version meta.Version, key *meta.Key) ([]*composite.ForwardingRule, error) {
	f.calls = append(f.calls, "ListForwardingRules")
	result := []*composite.ForwardingRule{}
	for _, fw := range f.Fw {
		if fw.Version == version && fw.ResourceType == key.Type() {
			result = append(result, fw)
		}
	}
	return result, nil
}

// CreateGlobalForwardingRule fakes forwarding rule creation.
func (f *FakeLoadBalancers) CreateForwardingRule(rule *composite.ForwardingRule, key *meta.Key) error {
	f.calls = append(f.calls, "CreateForwardingRule")
	if rule.IPAddress == "" {
		rule.IPAddress = fmt.Sprintf(testIPManager.ip())
	}
	rule.SelfLink = cloud.NewGlobalForwardingRulesResourceID("mock-project", rule.Name).SelfLink(rule.Version)
	rule.ResourceType = key.Type()
	if rule.Version == "" {
		rule.Version = meta.VersionGA
	}
	f.Fw = append(f.Fw, rule)
	return nil
}

// SetProxyForGlobalForwardingRule fakes setting a global forwarding rule.
func (f *FakeLoadBalancers) SetProxyForForwardingRule(forwardingRule *composite.ForwardingRule, key *meta.Key, proxyLink string) error {
	forwardingRuleName := forwardingRule.Name
	version := forwardingRule.Version
	f.calls = append(f.calls, "SetProxyForForwardingRule")
	for i := range f.Fw {
		if f.Fw[i].Name == forwardingRuleName && f.Fw[i].Version == version && f.Fw[i].ResourceType == key.Type() {
			f.Fw[i].Target = proxyLink
		}
	}
	return nil
}

// DeleteGlobalForwardingRule fakes deleting a global forwarding rule.
func (f *FakeLoadBalancers) DeleteForwardingRule(version meta.Version, key *meta.Key) error {
	name := key.Name
	f.calls = append(f.calls, "DeleteForwardingRule")
	fw := []*composite.ForwardingRule{}
	for i := range f.Fw {
		if f.Fw[i].Name != name || f.Fw[i].Version != version || f.Fw[i].ResourceType != key.Type() {
			fw = append(fw, f.Fw[i])
		}
	}
	if len(f.Fw) == len(fw) {
		// Nothing was deleted.
		return utils.FakeGoogleAPINotFoundErr()
	}
	f.Fw = fw
	return nil
}

// GetForwardingRulesWithIPs returns all forwarding rules that match the given ips.
func (f *FakeLoadBalancers) GetForwardingRulesWithIPs(ip []string) (fwRules []*composite.ForwardingRule) {
	f.calls = append(f.calls, "GetForwardingRulesWithIPs")
	ipSet := sets.NewString(ip...)
	for i := range f.Fw {
		if ipSet.Has(f.Fw[i].IPAddress) {
			fwRules = append(fwRules, f.Fw[i])
		}
	}
	return fwRules
}

// UrlMaps fakes

// GetURLMap fakes getting url maps from the cloud.
func (f *FakeLoadBalancers) GetUrlMap(version meta.Version, key *meta.Key) (*composite.UrlMap, error) {
	name := key.Name
	f.calls = append(f.calls, "GetURLMap")
	for _, um := range f.Um {
		if um.Name == name && um.Version == version && um.ResourceType == key.Type() {
			return um, nil
		}
	}
	return nil, utils.FakeGoogleAPINotFoundErr()
}

// CreateURLMap fakes url-map creation.
func (f *FakeLoadBalancers) CreateUrlMap(urlMap *composite.UrlMap, key *meta.Key) error {
	klog.V(4).Infof("CreateURLMap %+v", urlMap)
	f.calls = append(f.calls, "CreateURLMap")
	if key.Type() == meta.Regional {
		urlMap.SelfLink = cloud.NewRegionUrlMapsResourceID("mock-project", FakeRegion, urlMap.Name).SelfLink(urlMap.Version)
	} else {
		urlMap.SelfLink = cloud.NewUrlMapsResourceID("mock-project", urlMap.Name).SelfLink(urlMap.Version)
	}
	urlMap.ResourceType = key.Type()
	if urlMap.Version == "" {
		urlMap.Version = meta.VersionGA
	}
	f.Um = append(f.Um, urlMap)
	return nil
}

// UpdateURLMap fakes updating url-maps.
func (f *FakeLoadBalancers) UpdateUrlMap(urlMap *composite.UrlMap, key *meta.Key) error {
	f.calls = append(f.calls, "UpdateURLMap")
	if urlMap.Version == "" {
		urlMap.Version = meta.VersionGA
	}

	for i, um := range f.Um {
		if um.Name == urlMap.Name && um.Version == urlMap.Version && um.ResourceType == key.Type() {
			f.Um[i] = urlMap
			return nil
		}
	}
	return utils.FakeGoogleAPINotFoundErr()
}

// DeleteURLMap fakes url-map deletion.
func (f *FakeLoadBalancers) DeleteUrlMap(version meta.Version, key *meta.Key) error {
	name := key.Name
	f.calls = append(f.calls, "DeleteURLMap")
	um := []*composite.UrlMap{}
	for i := range f.Um {
		if f.Um[i].Name != name {
			um = append(um, f.Um[i])
		}
	}
	if len(f.Um) == len(um) {
		// Nothing was deleted.
		return utils.FakeGoogleAPINotFoundErr()
	}
	f.Um = um
	return nil
}

// ListURLMaps fakes getting url maps from the cloud.
func (f *FakeLoadBalancers) ListUrlMaps(version meta.Version, key *meta.Key) ([]*composite.UrlMap, error) {
	f.calls = append(f.calls, "ListURLMaps")
	result := []*composite.UrlMap{}
	for _, um := range f.Um {
		if um.Version == version && um.ResourceType == key.Type() {
			result = append(result, um)
		}
	}

	return f.Um, nil
}

func (f *FakeLoadBalancers) ListAllUrlMaps() ([]*composite.UrlMap, error) {
	f.calls = append(f.calls, "ListURLMaps")
	return f.ListUrlMaps(f.Version, f.CreateKey("", false))
}

// TargetProxies fakes

// GetTargetHTTPProxy fakes getting target http proxies from the cloud.
func (f *FakeLoadBalancers) GetTargetHttpProxy(version meta.Version, key *meta.Key) (*composite.TargetHttpProxy, error) {
	name := key.Name
	f.calls = append(f.calls, "GetTargetHTTPProxy")
	for i, tp := range f.Tp {
		if tp.Name == name && tp.Version == version && tp.ResourceType == key.Type() {
			return f.Tp[i], nil
		}
	}
	return nil, utils.FakeGoogleAPINotFoundErr()
}

// CreateTargetHTTPProxy fakes creating a target http proxy.
func (f *FakeLoadBalancers) CreateTargetHttpProxy(proxy *composite.TargetHttpProxy, key *meta.Key) error {
	f.calls = append(f.calls, "CreateTargetHTTPProxy")
	proxy.SelfLink = cloud.NewTargetHttpProxiesResourceID("mock-project", proxy.Name).SelfLink(proxy.Version)
	proxy.ResourceType = key.Type()
	if proxy.Version == "" {
		proxy.Version = meta.VersionGA
	}
	f.Tp = append(f.Tp, proxy)
	return nil
}

// DeleteTargetHTTPProxy fakes deleting a target http proxy.
func (f *FakeLoadBalancers) DeleteTargetHttpProxy(version meta.Version, key *meta.Key) error {
	name := key.Name
	f.calls = append(f.calls, "DeleteTargetHTTPProxy")
	proxies := []*composite.TargetHttpProxy{}
	for i, tp := range f.Tp {
		if tp.Name != name || tp.Version != version || tp.ResourceType != key.Type() {
			proxies = append(proxies, f.Tp[i])
		}
	}
	if len(f.Tp) == len(proxies) {
		// Nothing was deleted.
		return utils.FakeGoogleAPINotFoundErr()
	}
	f.Tp = proxies
	return nil
}

// SetURLMapForTargetHTTPProxy fakes setting an url-map for a target http proxy.
func (f *FakeLoadBalancers) SetUrlMapForTargetHttpProxy(proxy *composite.TargetHttpProxy, urlMapLink string, key *meta.Key) error {
	f.calls = append(f.calls, "SetURLMapForTargetHTTPProxy")
	for i, tp := range f.Tp {
		if tp.Name == proxy.Name && tp.Version == proxy.Version && tp.ResourceType == key.Type() {
			f.Tp[i].UrlMap = urlMapLink
		}
	}
	return nil
}

// TargetHttpsProxy fakes

// GetTargetHTTPSProxy fakes getting target http proxies from the cloud.
func (f *FakeLoadBalancers) GetTargetHttpsProxy(version meta.Version, key *meta.Key) (*composite.TargetHttpsProxy, error) {
	name := key.Name
	f.calls = append(f.calls, "GetTargetHTTPSProxy")
	for i, tps := range f.Tps {
		if tps.Name == name && tps.Version == version && tps.ResourceType == key.Type() {
			return f.Tps[i], nil
		}
	}
	return nil, utils.FakeGoogleAPINotFoundErr()
}

// CreateTargetHTTPSProxy fakes creating a target http proxy.
func (f *FakeLoadBalancers) CreateTargetHttpsProxy(proxy *composite.TargetHttpsProxy, key *meta.Key) error {
	f.calls = append(f.calls, "CreateTargetHTTPSProxy")
	proxy.SelfLink = cloud.NewTargetHttpProxiesResourceID("mock-project", proxy.Name).SelfLink(proxy.Version)
	proxy.ResourceType = key.Type()
	if proxy.Version == "" {
		proxy.Version = meta.VersionGA
	}
	f.Tps = append(f.Tps, proxy)
	return nil
}

// DeleteTargetHTTPSProxy fakes deleting a target http proxy.
func (f *FakeLoadBalancers) DeleteTargetHttpsProxy(version meta.Version, key *meta.Key) error {
	name := key.Name
	f.calls = append(f.calls, "DeleteTargetHTTPSProxy")
	tp := []*composite.TargetHttpsProxy{}
	for i, tps := range f.Tps {
		if tps.Name != name || tps.Version != version || tps.ResourceType != key.Type() {
			tp = append(tp, f.Tps[i])
		}
	}
	if len(f.Tps) == len(tp) {
		// Nothing was deleted.
		return utils.FakeGoogleAPINotFoundErr()
	}
	f.Tps = tp
	return nil
}

// SetURLMapForTargetHTTPSProxy fakes setting an url-map for a target http proxy.
func (f *FakeLoadBalancers) SetUrlMapForTargetHttpsProxy(proxy *composite.TargetHttpsProxy, urlMapLink string, key *meta.Key) error {
	f.calls = append(f.calls, "SetURLMapForTargetHTTPSProxy")
	for i, tps := range f.Tps {
		if tps.Name == proxy.Name && tps.Version == proxy.Version && tps.ResourceType == key.Type() {
			f.Tps[i].UrlMap = urlMapLink
		}
	}
	return nil
}

// SetSslCertificateForTargetHTTPProxy fakes out setting certificates.
func (f *FakeLoadBalancers) SetSslCertificateForTargetHttpsProxy(proxy *composite.TargetHttpsProxy, sslCertURLs []string, key *meta.Key) error {
	f.calls = append(f.calls, "SetSslCertificateForTargetHTTPSProxy")
	found := false
	for i, tps := range f.Tps {
		if tps.Name == proxy.Name && tps.ResourceType == key.Type() && tps.Version == proxy.Version {
			if len(sslCertURLs) > TargetProxyCertLimit {
				return utils.FakeGoogleAPIForbiddenErr()
			}
			f.Tps[i].SslCertificates = sslCertURLs
			found = true
			break
		}
	}
	if !found {
		return utils.FakeGoogleAPINotFoundErr()
	}
	return nil
}

// Static IP fakes

// ReserveGlobalAddress fakes out static IP reservation.
func (f *FakeLoadBalancers) ReserveGlobalAddress(addr *compute.Address) error {
	f.calls = append(f.calls, "ReserveGlobalAddress")
	f.IP = append(f.IP, addr)
	return nil
}

// GetGlobalAddress fakes out static IP retrieval.
func (f *FakeLoadBalancers) GetGlobalAddress(name string) (*compute.Address, error) {
	f.calls = append(f.calls, "GetGlobalAddress")
	for i := range f.IP {
		if f.IP[i].Name == name {
			return f.IP[i], nil
		}
	}
	return nil, utils.FakeGoogleAPINotFoundErr()
}

// DeleteGlobalAddress fakes out static IP deletion.
func (f *FakeLoadBalancers) DeleteGlobalAddress(name string) error {
	f.calls = append(f.calls, "DeleteGlobalAddress")
	ip := []*compute.Address{}
	for i := range f.IP {
		if f.IP[i].Name != name {
			ip = append(ip, f.IP[i])
		}
	}
	if len(f.IP) == len(ip) {
		// Nothing was deleted.
		return utils.FakeGoogleAPINotFoundErr()
	}
	f.IP = ip
	return nil
}

// SslCertificate fakes

// GetSslCertificate fakes out getting ssl certs.
func (f *FakeLoadBalancers) GetSslCertificate(version meta.Version, key *meta.Key) (*composite.SslCertificate, error) {
	name := key.Name
	f.calls = append(f.calls, "GetSslCertificate")
	for _, cert := range f.Certs {
		if cert.Name == name && cert.Version == version && cert.ResourceType == key.Type() {
			return cert, nil
		}
	}
	return nil, utils.FakeGoogleAPINotFoundErr()
}

func (f *FakeLoadBalancers) ListSslCertificates(version meta.Version, key *meta.Key) ([]*composite.SslCertificate, error) {
	f.calls = append(f.calls, "ListSslCertificates")
	result := []*composite.SslCertificate{}
	for _, cert := range f.Certs {
		if cert.Version == version && cert.ResourceType == key.Type() {
			result = append(result, cert)
		}
	}

	return result, nil
}

// CreateSslCertificate fakes out certificate creation.
func (f *FakeLoadBalancers) CreateSslCertificate(cert *composite.SslCertificate, key *meta.Key) error {
	f.calls = append(f.calls, "CreateSslCertificate")
	cert.SelfLink = cloud.NewSslCertificatesResourceID("mock-project", cert.Name).SelfLink(cert.Version)
	if len(f.Certs) == FakeCertQuota {
		// Simulate cert creation failure
		return fmt.Errorf("unable to create cert, Exceeded cert limit of %d.", FakeCertQuota)
	}
	cert.ResourceType = key.Type()
	if cert.Version == "" {
		cert.Version = meta.VersionGA
	}
	f.Certs = append(f.Certs, cert)
	return nil
}

// DeleteSslCertificate fakes out certificate deletion.
func (f *FakeLoadBalancers) DeleteSslCertificate(version meta.Version, key *meta.Key) error {
	name := key.Name
	f.calls = append(f.calls, "DeleteSslCertificate")
	certs := []*composite.SslCertificate{}
	for _, cert := range f.Certs {
		if cert.Name != name || cert.Version != version || cert.ResourceType != key.Type() {
			certs = append(certs, cert)
		}
	}
	if len(f.Certs) == len(certs) {
		// Nothing was deleted.
		return utils.FakeGoogleAPINotFoundErr()
	}
	f.Certs = certs
	return nil
}

// NewFakeLoadBalancers creates a fake cloud client. Name is the name
// inserted into the selfLink of the associated resources for testing.
// eg: forwardingRule.SelfLink == k8-fw-name.
func NewFakeLoadBalancers(name string, namer *utils.Namer, version meta.Version, resourceType meta.KeyType) *FakeLoadBalancers {
	return &FakeLoadBalancers{
		Fw:           []*composite.ForwardingRule{},
		name:         name,
		namer:        namer,
		Version:      version,
		ResourceType: resourceType,
	}
}
