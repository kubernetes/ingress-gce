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

	"github.com/golang/glog"

	compute "google.golang.org/api/compute/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/kubernetes/pkg/cloudprovider/providers/gce/cloud"
	"k8s.io/kubernetes/pkg/cloudprovider/providers/gce/cloud/meta"

	"k8s.io/ingress-gce/pkg/utils"
)

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
	Fw    []*compute.ForwardingRule
	Um    []*compute.UrlMap
	Tp    []*compute.TargetHttpProxy
	Tps   []*compute.TargetHttpsProxy
	IP    []*compute.Address
	Certs []*compute.SslCertificate
	name  string
	calls []string // list of calls that were made

	namer *utils.Namer
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
func (f *FakeLoadBalancers) GetGlobalForwardingRule(name string) (*compute.ForwardingRule, error) {
	f.calls = append(f.calls, "GetGlobalForwardingRule")
	for i := range f.Fw {
		if f.Fw[i].Name == name {
			return f.Fw[i], nil
		}
	}
	return nil, utils.FakeGoogleAPINotFoundErr()
}

func (f *FakeLoadBalancers) ListGlobalForwardingRules() ([]*compute.ForwardingRule, error) {
	return f.Fw, nil
}

// CreateGlobalForwardingRule fakes forwarding rule creation.
func (f *FakeLoadBalancers) CreateGlobalForwardingRule(rule *compute.ForwardingRule) error {
	f.calls = append(f.calls, "CreateGlobalForwardingRule")
	if rule.IPAddress == "" {
		rule.IPAddress = fmt.Sprintf(testIPManager.ip())
	}
	rule.SelfLink = cloud.NewGlobalForwardingRulesResourceID("mock-project", rule.Name).SelfLink(meta.VersionGA)
	f.Fw = append(f.Fw, rule)
	return nil
}

// SetProxyForGlobalForwardingRule fakes setting a global forwarding rule.
func (f *FakeLoadBalancers) SetProxyForGlobalForwardingRule(forwardingRuleName, proxyLink string) error {
	f.calls = append(f.calls, "SetProxyForGlobalForwardingRule")
	for i := range f.Fw {
		if f.Fw[i].Name == forwardingRuleName {
			f.Fw[i].Target = proxyLink
		}
	}
	return nil
}

// DeleteGlobalForwardingRule fakes deleting a global forwarding rule.
func (f *FakeLoadBalancers) DeleteGlobalForwardingRule(name string) error {
	f.calls = append(f.calls, "DeleteGlobalForwardingRule")
	fw := []*compute.ForwardingRule{}
	for i := range f.Fw {
		if f.Fw[i].Name != name {
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
func (f *FakeLoadBalancers) GetForwardingRulesWithIPs(ip []string) (fwRules []*compute.ForwardingRule) {
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

// GetUrlMap fakes getting url maps from the cloud.
func (f *FakeLoadBalancers) GetUrlMap(name string) (*compute.UrlMap, error) {
	f.calls = append(f.calls, "GetUrlMap")
	for i := range f.Um {
		if f.Um[i].Name == name {
			return f.Um[i], nil
		}
	}
	return nil, utils.FakeGoogleAPINotFoundErr()
}

// CreateUrlMap fakes url-map creation.
func (f *FakeLoadBalancers) CreateUrlMap(urlMap *compute.UrlMap) error {
	glog.V(4).Infof("CreateUrlMap %+v", urlMap)
	f.calls = append(f.calls, "CreateUrlMap")
	urlMap.SelfLink = cloud.NewUrlMapsResourceID("mock-project", urlMap.Name).SelfLink(meta.VersionGA)
	f.Um = append(f.Um, urlMap)
	return nil
}

// UpdateUrlMap fakes updating url-maps.
func (f *FakeLoadBalancers) UpdateUrlMap(urlMap *compute.UrlMap) error {
	f.calls = append(f.calls, "UpdateUrlMap")
	for i := range f.Um {
		if f.Um[i].Name == urlMap.Name {
			f.Um[i] = urlMap
			return nil
		}
	}
	return utils.FakeGoogleAPINotFoundErr()
}

// DeleteUrlMap fakes url-map deletion.
func (f *FakeLoadBalancers) DeleteUrlMap(name string) error {
	f.calls = append(f.calls, "DeleteUrlMap")
	um := []*compute.UrlMap{}
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

// ListUrlMaps fakes getting url maps from the cloud.
func (f *FakeLoadBalancers) ListUrlMaps() ([]*compute.UrlMap, error) {
	f.calls = append(f.calls, "ListUrlMaps")
	return f.Um, nil
}

// TargetProxies fakes

// GetTargetHttpProxy fakes getting target http proxies from the cloud.
func (f *FakeLoadBalancers) GetTargetHttpProxy(name string) (*compute.TargetHttpProxy, error) {
	f.calls = append(f.calls, "GetTargetHttpProxy")
	for i := range f.Tp {
		if f.Tp[i].Name == name {
			return f.Tp[i], nil
		}
	}
	return nil, utils.FakeGoogleAPINotFoundErr()
}

// CreateTargetHttpProxy fakes creating a target http proxy.
func (f *FakeLoadBalancers) CreateTargetHttpProxy(proxy *compute.TargetHttpProxy) error {
	f.calls = append(f.calls, "CreateTargetHttpProxy")
	proxy.SelfLink = cloud.NewTargetHttpProxiesResourceID("mock-project", proxy.Name).SelfLink(meta.VersionGA)
	f.Tp = append(f.Tp, proxy)
	return nil
}

// DeleteTargetHttpProxy fakes deleting a target http proxy.
func (f *FakeLoadBalancers) DeleteTargetHttpProxy(name string) error {
	f.calls = append(f.calls, "DeleteTargetHttpProxy")
	tp := []*compute.TargetHttpProxy{}
	for i := range f.Tp {
		if f.Tp[i].Name != name {
			tp = append(tp, f.Tp[i])
		}
	}
	if len(f.Tp) == len(tp) {
		// Nothing was deleted.
		return utils.FakeGoogleAPINotFoundErr()
	}
	f.Tp = tp
	return nil
}

// SetUrlMapForTargetHttpProxy fakes setting an url-map for a target http proxy.
func (f *FakeLoadBalancers) SetUrlMapForTargetHttpProxy(proxy *compute.TargetHttpProxy, urlMap *compute.UrlMap) error {
	f.calls = append(f.calls, "SetUrlMapForTargetHttpProxy")
	for i := range f.Tp {
		if f.Tp[i].Name == proxy.Name {
			f.Tp[i].UrlMap = urlMap.SelfLink
		}
	}
	return nil
}

// TargetHttpsProxy fakes

// GetTargetHttpsProxy fakes getting target http proxies from the cloud.
func (f *FakeLoadBalancers) GetTargetHttpsProxy(name string) (*compute.TargetHttpsProxy, error) {
	f.calls = append(f.calls, "GetTargetHttpsProxy")
	for i := range f.Tps {
		if f.Tps[i].Name == name {
			return f.Tps[i], nil
		}
	}
	return nil, utils.FakeGoogleAPINotFoundErr()
}

// CreateTargetHttpsProxy fakes creating a target http proxy.
func (f *FakeLoadBalancers) CreateTargetHttpsProxy(proxy *compute.TargetHttpsProxy) error {
	f.calls = append(f.calls, "CreateTargetHttpsProxy")
	proxy.SelfLink = cloud.NewTargetHttpProxiesResourceID("mock-project", proxy.Name).SelfLink(meta.VersionGA)
	f.Tps = append(f.Tps, proxy)
	return nil
}

// DeleteTargetHttpsProxy fakes deleting a target http proxy.
func (f *FakeLoadBalancers) DeleteTargetHttpsProxy(name string) error {
	f.calls = append(f.calls, "DeleteTargetHttpsProxy")
	tp := []*compute.TargetHttpsProxy{}
	for i := range f.Tps {
		if f.Tps[i].Name != name {
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

// SetUrlMapForTargetHttpsProxy fakes setting an url-map for a target http proxy.
func (f *FakeLoadBalancers) SetUrlMapForTargetHttpsProxy(proxy *compute.TargetHttpsProxy, urlMap *compute.UrlMap) error {
	f.calls = append(f.calls, "SetUrlMapForTargetHttpsProxy")
	for i := range f.Tps {
		if f.Tps[i].Name == proxy.Name {
			f.Tps[i].UrlMap = urlMap.SelfLink
		}
	}
	return nil
}

// SetSslCertificateForTargetHttpsProxy fakes out setting certificates.
func (f *FakeLoadBalancers) SetSslCertificateForTargetHttpsProxy(proxy *compute.TargetHttpsProxy, sslCertURLs []string) error {
	f.calls = append(f.calls, "SetSslCertificateForTargetHttpsProxy")
	found := false
	for i := range f.Tps {
		if f.Tps[i].Name == proxy.Name {
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

// UrlMap fakes

// CheckURLMap checks the compute.UrlMap maintained by the load balancer.
// We check against our internal representation.
func (f *FakeLoadBalancers) CheckURLMap(l7 *L7, expectedUrlMap *utils.GCEURLMap) error {
	f.calls = append(f.calls, "CheckURLMap")
	um, err := f.GetUrlMap(l7.UrlMap().Name)
	if err != nil || um == nil {
		return fmt.Errorf("f.GetUrlMap(%q) = %v, %v; want _, nil", l7.UrlMap().Name, um, err)
	}
	defaultBackendName := expectedUrlMap.DefaultBackend.BackendName(f.namer)
	defaultBackendLink := utils.BackendServiceRelativeResourcePath(defaultBackendName)
	// The urlmap should have a default backend, and each path matcher.
	if defaultBackendName != "" && l7.UrlMap().DefaultService != defaultBackendLink {
		return fmt.Errorf("default backend = %v, want %v", l7.UrlMap().DefaultService, defaultBackendLink)
	}

	for _, matcher := range l7.UrlMap().PathMatchers {
		var hostname string
		// There's a 1:1 mapping between pathmatchers and hosts
		for _, hostRule := range l7.UrlMap().HostRules {
			if matcher.Name == hostRule.PathMatcher {
				if len(hostRule.Hosts) != 1 {
					return fmt.Errorf("unexpected hosts in hostrules %+v", hostRule)
				}
				if defaultBackendLink != "" && matcher.DefaultService != defaultBackendLink {
					return fmt.Errorf("expected default backend %v found %v", defaultBackendLink, matcher.DefaultService)
				}
				hostname = hostRule.Hosts[0]
				break
			}
		}
		// These are all pathrules for a single host, found above
		for _, rule := range matcher.PathRules {
			if len(rule.Paths) != 1 {
				return fmt.Errorf("Unexpected rule in pathrules %+v", rule)
			}
			pathRule := rule.Paths[0]
			if !expectedUrlMap.HostExists(hostname) {
				return fmt.Errorf("Expected path rules for host %v", hostname)
			} else if ok, svc := expectedUrlMap.PathExists(hostname, pathRule); !ok {
				return fmt.Errorf("Expected rule %v for host %v", pathRule, hostname)
			} else if utils.BackendServiceRelativeResourcePath(svc.BackendName(f.namer)) != rule.Service {
				return fmt.Errorf("Expected service %v found %v", svc, rule.Service)
			}
		}
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
func (f *FakeLoadBalancers) GetSslCertificate(name string) (*compute.SslCertificate, error) {
	f.calls = append(f.calls, "GetSslCertificate")
	for i := range f.Certs {
		if f.Certs[i].Name == name {
			return f.Certs[i], nil
		}
	}
	return nil, utils.FakeGoogleAPINotFoundErr()
}

func (f *FakeLoadBalancers) ListSslCertificates() ([]*compute.SslCertificate, error) {
	f.calls = append(f.calls, "ListSslCertificates")
	return f.Certs, nil
}

// CreateSslCertificate fakes out certificate creation.
func (f *FakeLoadBalancers) CreateSslCertificate(cert *compute.SslCertificate) (*compute.SslCertificate, error) {
	f.calls = append(f.calls, "CreateSslCertificate")
	cert.SelfLink = cloud.NewSslCertificatesResourceID("mock-project", cert.Name).SelfLink(meta.VersionGA)
	if len(f.Certs) == TargetProxyCertLimit {
		// Simulate cert creation failure
		return nil, fmt.Errorf("Unable to create cert, Exceeded cert limit of %d.", TargetProxyCertLimit)
	}
	f.Certs = append(f.Certs, cert)
	return cert, nil
}

// DeleteSslCertificate fakes out certificate deletion.
func (f *FakeLoadBalancers) DeleteSslCertificate(name string) error {
	f.calls = append(f.calls, "DeleteSslCertificate")
	certs := []*compute.SslCertificate{}
	for i := range f.Certs {
		if f.Certs[i].Name != name {
			certs = append(certs, f.Certs[i])
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
func NewFakeLoadBalancers(name string, namer *utils.Namer) *FakeLoadBalancers {
	return &FakeLoadBalancers{
		Fw:    []*compute.ForwardingRule{},
		name:  name,
		namer: namer,
	}
}
