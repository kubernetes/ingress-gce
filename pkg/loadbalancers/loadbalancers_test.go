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
	"testing"

	compute "google.golang.org/api/compute/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	"k8s.io/ingress-gce/pkg/backends"
	"k8s.io/ingress-gce/pkg/healthchecks"
	"k8s.io/ingress-gce/pkg/instances"
	"k8s.io/ingress-gce/pkg/networkendpointgroup"
	"k8s.io/ingress-gce/pkg/utils"
)

const (
	defaultZone = "zone-a"
)

var (
	testDefaultBeNodePort = backends.ServicePort{Port: 3000, Protocol: utils.ProtocolHTTP}
)

func newFakeLoadBalancerPool(f LoadBalancers, t *testing.T, namer *utils.Namer) LoadBalancerPool {
	fakeBackends := backends.NewFakeBackendServices(func(op int, be *compute.BackendService) error { return nil })
	fakeIGs := instances.NewFakeInstanceGroups(sets.NewString(), namer)
	fakeHCP := healthchecks.NewFakeHealthCheckProvider()
	fakeNEG := networkendpointgroup.NewFakeNetworkEndpointGroupCloud("test-subnet", "test-network")
	healthChecker := healthchecks.NewHealthChecker(fakeHCP, "/", namer)
	nodePool := instances.NewNodePool(fakeIGs, namer)
	nodePool.Init(&instances.FakeZoneLister{Zones: []string{defaultZone}})
	backendPool := backends.NewBackendPool(
		fakeBackends, fakeNEG, healthChecker, nodePool, namer, []int64{}, false)

	return NewLoadBalancerPool(f, backendPool, testDefaultBeNodePort, namer)
}

func TestCreateHTTPLoadBalancer(t *testing.T) {
	// This should NOT create the forwarding rule and target proxy
	// associated with the HTTPS branch of this loadbalancer.
	namer := utils.NewNamer("uid1", "fw1")
	lbInfo := &L7RuntimeInfo{
		Name:      namer.LoadBalancer("test"),
		AllowHTTP: true,
	}
	f := NewFakeLoadBalancers(lbInfo.Name, namer)
	pool := newFakeLoadBalancerPool(f, t, namer)
	pool.Sync([]*L7RuntimeInfo{lbInfo})
	l7, err := pool.Get(lbInfo.Name)

	if err != nil || l7 == nil {
		t.Fatalf("Expected l7 not created")
	}
	um, err := f.GetUrlMap(f.UMName())
	if err != nil {
		t.Fatalf("f.GetUrlMap(%q) = _, %v; want nil", f.UMName(), err)
	}
	if um.DefaultService != pool.(*L7s).GLBCDefaultBackend().SelfLink {
		t.Fatalf("got um.DefaultService = %v; want %v", um.DefaultService, pool.(*L7s).GLBCDefaultBackend().SelfLink)
	}
	tp, err := f.GetTargetHttpProxy(f.TPName(false))
	if err != nil {
		t.Fatalf("f.GetTargetHttpProxy(%q) = _, %v; want nil", f.TPName(false), err)
	}
	if tp.UrlMap != um.SelfLink {
		t.Fatalf("%v", err)
	}
	fw, err := f.GetGlobalForwardingRule(f.FWName(false))
	if err != nil {
		t.Fatalf("f.GetGlobalForwardingRule(%q) = _, %v, want nil", f.FWName(false), err)
	}
	if fw.Target != tp.SelfLink {
		t.Fatalf("%v", err)
	}
}

func TestCreateHTTPSLoadBalancer(t *testing.T) {
	// This should NOT create the forwarding rule and target proxy
	// associated with the HTTP branch of this loadbalancer.
	namer := utils.NewNamer("uid1", "fw1")
	lbInfo := &L7RuntimeInfo{
		Name:      namer.LoadBalancer("test"),
		AllowHTTP: false,
		TLS:       &TLSCerts{Key: "key", Cert: "cert"},
	}
	f := NewFakeLoadBalancers(lbInfo.Name, namer)
	pool := newFakeLoadBalancerPool(f, t, namer)
	pool.Sync([]*L7RuntimeInfo{lbInfo})
	l7, err := pool.Get(lbInfo.Name)
	if err != nil || l7 == nil {
		t.Fatalf("Expected l7 not created")
	}
	um, err := f.GetUrlMap(f.UMName())
	if err != nil ||
		um.DefaultService != pool.(*L7s).GLBCDefaultBackend().SelfLink {
		t.Fatalf("%v", err)
	}
	tps, err := f.GetTargetHttpsProxy(f.TPName(true))
	if err != nil || tps.UrlMap != um.SelfLink {
		t.Fatalf("%v", err)
	}
	fws, err := f.GetGlobalForwardingRule(f.FWName(true))
	if err != nil || fws.Target != tps.SelfLink {
		t.Fatalf("%v", err)
	}
}

// Tests that a certificate is created from the provided Key/Cert combo
// and the proxy is updated to another cert when the provided cert changes
func TestCertUpdate(t *testing.T) {
	namer := utils.NewNamer("uid1", "fw1")
	primaryCertName := namer.SSLCert(namer.LoadBalancer("test"), true)
	secondaryCertName := namer.SSLCert(namer.LoadBalancer("test"), false)

	lbInfo := &L7RuntimeInfo{
		Name:      namer.LoadBalancer("test"),
		AllowHTTP: false,
		TLS:       &TLSCerts{Key: "key", Cert: "cert"},
	}

	f := NewFakeLoadBalancers(lbInfo.Name, namer)
	pool := newFakeLoadBalancerPool(f, t, namer)

	// Sync first cert
	pool.Sync([]*L7RuntimeInfo{lbInfo})
	t.Logf("name=%q", primaryCertName)
	verifyCertAndProxyLink(primaryCertName, lbInfo.TLS.Cert, f, t)

	// Sync with different cert
	lbInfo.TLS = &TLSCerts{Key: "key2", Cert: "cert2"}
	pool.Sync([]*L7RuntimeInfo{lbInfo})
	verifyCertAndProxyLink(secondaryCertName, lbInfo.TLS.Cert, f, t)
}

// Tests that controller can overwrite existing, unused certificates
func TestCertCreationWithCollision(t *testing.T) {
	namer := utils.NewNamer("uid1", "fw1")
	primaryCertName := namer.SSLCert(namer.LoadBalancer("test"), true)
	secondaryCertName := namer.SSLCert(namer.LoadBalancer("test"), false)

	lbInfo := &L7RuntimeInfo{
		Name:      namer.LoadBalancer("test"),
		AllowHTTP: false,
		TLS:       &TLSCerts{Key: "key", Cert: "cert"},
	}
	f := NewFakeLoadBalancers(lbInfo.Name, namer)
	pool := newFakeLoadBalancerPool(f, t, namer)

	// Have both names already used by orphaned certs
	f.CreateSslCertificate(&compute.SslCertificate{
		Name:        primaryCertName,
		Certificate: "abc",
		SelfLink:    "existing",
	})
	f.CreateSslCertificate(&compute.SslCertificate{
		Name:        secondaryCertName,
		Certificate: "xyz",
		SelfLink:    "existing",
	})

	// Sync first cert
	pool.Sync([]*L7RuntimeInfo{lbInfo})
	verifyCertAndProxyLink(primaryCertName, lbInfo.TLS.Cert, f, t)

	// Sync with different cert
	lbInfo.TLS = &TLSCerts{Key: "key2", Cert: "cert2"}
	pool.Sync([]*L7RuntimeInfo{lbInfo})
	verifyCertAndProxyLink(secondaryCertName, lbInfo.TLS.Cert, f, t)
}

func TestCertRetentionAfterRestart(t *testing.T) {
	namer := utils.NewNamer("uid1", "fw1")
	primaryCertName := namer.SSLCert(namer.LoadBalancer("test"), true)
	secondaryCertName := namer.SSLCert(namer.LoadBalancer("test"), false)

	lbInfo := &L7RuntimeInfo{
		Name:      namer.LoadBalancer("test"),
		AllowHTTP: false,
		TLS:       &TLSCerts{Key: "key", Cert: "cert"},
	}

	f := NewFakeLoadBalancers(lbInfo.Name, namer)
	firstPool := newFakeLoadBalancerPool(f, t, namer)

	// Sync twice so the expected certificate uses the secondary name
	firstPool.Sync([]*L7RuntimeInfo{lbInfo})
	verifyCertAndProxyLink(primaryCertName, lbInfo.TLS.Cert, f, t)
	lbInfo.TLS = &TLSCerts{Key: "key2", Cert: "cert2"}
	firstPool.Sync([]*L7RuntimeInfo{lbInfo})
	verifyCertAndProxyLink(secondaryCertName, lbInfo.TLS.Cert, f, t)

	// Restart of controller represented by a new pool
	secondPool := newFakeLoadBalancerPool(f, t, namer)
	secondPool.Sync([]*L7RuntimeInfo{lbInfo})

	// Verify second name is still used
	verifyCertAndProxyLink(secondaryCertName, lbInfo.TLS.Cert, f, t)

	// Update cert one more time to verify loop
	lbInfo.TLS = &TLSCerts{Key: "key3", Cert: "cert3"}
	secondPool.Sync([]*L7RuntimeInfo{lbInfo})
	verifyCertAndProxyLink(primaryCertName, lbInfo.TLS.Cert, f, t)

}

func verifyCertAndProxyLink(certName, certValue string, f *FakeLoadBalancers, t *testing.T) {
	t.Helper()

	t.Logf("f =\n%s", f)

	cert, err := f.GetSslCertificate(certName)
	if err != nil {
		t.Fatalf("expected ssl certificate to exist: %v, err: %v", certName, err)
	}

	if cert.Certificate != certValue {
		t.Fatalf("unexpected certificate value; expected %v, actual %v", certValue, cert.Certificate)
	}

	tps, err := f.GetTargetHttpsProxy(f.TPName(true))
	if err != nil {
		t.Fatalf("expected https proxy to exist: %v, err: %v", certName, err)
	}

	if len(tps.SslCertificates) == 0 || tps.SslCertificates[0] != cert.SelfLink {
		t.Fatalf("expected ssl certificate to be linked in target proxy; Cert Link: %q; Target Proxy Certs: %v", cert.SelfLink, tps.SslCertificates)
	}
}

func TestCreateHTTPSLoadBalancerAnnotationCert(t *testing.T) {
	// This should NOT create the forwarding rule and target proxy
	// associated with the HTTP branch of this loadbalancer.
	tlsName := "external-cert-name"

	namer := utils.NewNamer("uid1", "fw1")
	lbInfo := &L7RuntimeInfo{
		Name:      namer.LoadBalancer("test"),
		AllowHTTP: false,
		TLSName:   tlsName,
	}

	f := NewFakeLoadBalancers(lbInfo.Name, namer)
	f.CreateSslCertificate(&compute.SslCertificate{
		Name: tlsName,
	})
	pool := newFakeLoadBalancerPool(f, t, namer)
	pool.Sync([]*L7RuntimeInfo{lbInfo})
	l7, err := pool.Get(lbInfo.Name)
	if err != nil || l7 == nil {
		t.Fatalf("Expected l7 not created")
	}
	um, err := f.GetUrlMap(f.UMName())
	if err != nil ||
		um.DefaultService != pool.(*L7s).GLBCDefaultBackend().SelfLink {
		t.Fatalf("%v", err)
	}
	tps, err := f.GetTargetHttpsProxy(f.TPName(true))
	if err != nil || tps.UrlMap != um.SelfLink {
		t.Fatalf("%v", err)
	}
	fws, err := f.GetGlobalForwardingRule(f.FWName(true))
	if err != nil || fws.Target != tps.SelfLink {
		t.Fatalf("%v", err)
	}
}

func TestCreateBothLoadBalancers(t *testing.T) {
	// This should create 2 forwarding rules and target proxies
	// but they should use the same urlmap, and have the same
	// static ip.
	namer := utils.NewNamer("uid1", "fw1")
	lbInfo := &L7RuntimeInfo{
		Name:      namer.LoadBalancer("test"),
		AllowHTTP: true,
		TLS:       &TLSCerts{Key: "key", Cert: "cert"},
	}
	f := NewFakeLoadBalancers(lbInfo.Name, namer)
	pool := newFakeLoadBalancerPool(f, t, namer)

	pool.Sync([]*L7RuntimeInfo{lbInfo})
	l7, err := pool.Get(lbInfo.Name)
	if err != nil || l7 == nil {
		t.Fatalf("Expected l7 not created")
	}
	um, err := f.GetUrlMap(f.UMName())
	if err != nil ||
		um.DefaultService != pool.(*L7s).GLBCDefaultBackend().SelfLink {
		t.Fatalf("%v", err)
	}
	tps, err := f.GetTargetHttpsProxy(f.TPName(true))
	if err != nil || tps.UrlMap != um.SelfLink {
		t.Fatalf("%v", err)
	}
	tp, err := f.GetTargetHttpProxy(f.TPName(false))
	if err != nil || tp.UrlMap != um.SelfLink {
		t.Fatalf("%v", err)
	}
	fws, err := f.GetGlobalForwardingRule(f.FWName(true))
	if err != nil || fws.Target != tps.SelfLink {
		t.Fatalf("%v", err)
	}
	fw, err := f.GetGlobalForwardingRule(f.FWName(false))
	if err != nil || fw.Target != tp.SelfLink {
		t.Fatalf("%v", err)
	}
	ip, err := f.GetGlobalAddress(f.FWName(false))
	if err != nil || ip.Address != fw.IPAddress || ip.Address != fws.IPAddress {
		t.Fatalf("%v", err)
	}
}

func TestUpdateUrlMap(t *testing.T) {
	um1 := utils.GCEURLMap{
		"bar.example.com": {
			"/bar2": &compute.BackendService{SelfLink: "bar2svc"},
		},
	}
	um2 := utils.GCEURLMap{
		"foo.example.com": {
			"/foo1": &compute.BackendService{SelfLink: "foo1svc"},
			"/foo2": &compute.BackendService{SelfLink: "foo2svc"},
		},
		"bar.example.com": {
			"/bar1": &compute.BackendService{SelfLink: "bar1svc"},
		},
	}
	um2.PutDefaultBackend(&compute.BackendService{SelfLink: "default"})

	namer := utils.NewNamer("uid1", "fw1")
	lbInfo := &L7RuntimeInfo{Name: namer.LoadBalancer("test"), AllowHTTP: true}

	f := NewFakeLoadBalancers(lbInfo.Name, namer)
	pool := newFakeLoadBalancerPool(f, t, namer)
	pool.Sync([]*L7RuntimeInfo{lbInfo})
	l7, err := pool.Get(lbInfo.Name)
	if err != nil {
		t.Fatalf("%v", err)
	}
	for _, ir := range []utils.GCEURLMap{um1, um2} {
		if err := l7.UpdateUrlMap(ir); err != nil {
			t.Fatalf("%v", err)
		}
	}
	// The final map doesn't contain /bar2
	expectedMap := map[string]utils.FakeIngressRuleValueMap{
		utils.DefaultBackendKey: {
			utils.DefaultBackendKey: "default",
		},
		"foo.example.com": {
			"/foo1": "foo1svc",
			"/foo2": "foo2svc",
		},
		"bar.example.com": {
			"/bar1": "bar1svc",
		},
	}
	if err := f.CheckURLMap(l7, expectedMap); err != nil {
		t.Errorf("CheckURLMap(...) = %v, want nil", err)
	}
}

func TestUpdateUrlMapNoChanges(t *testing.T) {
	um1 := utils.GCEURLMap{
		"foo.example.com": {
			"/foo1": &compute.BackendService{SelfLink: "foo1svc"},
			"/foo2": &compute.BackendService{SelfLink: "foo2svc"},
		},
		"bar.example.com": {
			"/bar1": &compute.BackendService{SelfLink: "bar1svc"},
		},
	}
	um1.PutDefaultBackend(&compute.BackendService{SelfLink: "default"})
	um2 := utils.GCEURLMap{
		"foo.example.com": {
			"/foo1": &compute.BackendService{SelfLink: "foo1svc"},
			"/foo2": &compute.BackendService{SelfLink: "foo2svc"},
		},
		"bar.example.com": {
			"/bar1": &compute.BackendService{SelfLink: "bar1svc"},
		},
	}
	um2.PutDefaultBackend(&compute.BackendService{SelfLink: "default"})

	namer := utils.NewNamer("uid1", "fw1")
	lbInfo := &L7RuntimeInfo{Name: namer.LoadBalancer("test"), AllowHTTP: true}
	f := NewFakeLoadBalancers(lbInfo.Name, namer)
	pool := newFakeLoadBalancerPool(f, t, namer)
	pool.Sync([]*L7RuntimeInfo{lbInfo})
	l7, err := pool.Get(lbInfo.Name)
	if err != nil {
		t.Fatalf("%v", err)
	}
	for _, ir := range []utils.GCEURLMap{um1, um2} {
		if err := l7.UpdateUrlMap(ir); err != nil {
			t.Fatalf("%v", err)
		}
	}
	for _, call := range f.calls {
		if call == "UpdateUrlMap" {
			t.Errorf("UpdateUrlMap() should not have been called")
		}
	}
}

func TestNameParsing(t *testing.T) {
	clusterName := "123"
	firewallName := clusterName
	namer := utils.NewNamer(clusterName, firewallName)
	fullName := namer.ForwardingRule(namer.LoadBalancer("testlb"), utils.HTTPProtocol)
	annotationsMap := map[string]string{
		fmt.Sprintf("%v/forwarding-rule", utils.K8sAnnotationPrefix): fullName,
	}
	components := namer.ParseName(GCEResourceName(annotationsMap, "forwarding-rule"))
	t.Logf("components = %+v", components)
	if components.ClusterName != clusterName {
		t.Errorf("namer.ParseName(%q), got components.ClusterName == %q, want %q", fullName, clusterName, components.ClusterName)
	}
	resourceName := "fw"
	if components.Resource != resourceName {
		t.Errorf("Failed to parse resource from %v, expected %v got %v", fullName, resourceName, components.Resource)
	}
}

func TestClusterNameChange(t *testing.T) {
	namer := utils.NewNamer("uid1", "fw1")
	lbInfo := &L7RuntimeInfo{
		Name: namer.LoadBalancer("test"),
		TLS:  &TLSCerts{Key: "key", Cert: "cert"},
	}
	f := NewFakeLoadBalancers(lbInfo.Name, namer)
	pool := newFakeLoadBalancerPool(f, t, namer)
	pool.Sync([]*L7RuntimeInfo{lbInfo})
	l7, err := pool.Get(lbInfo.Name)
	if err != nil || l7 == nil {
		t.Fatalf("Expected l7 not created")
	}
	um, err := f.GetUrlMap(f.UMName())
	if err != nil ||
		um.DefaultService != pool.(*L7s).GLBCDefaultBackend().SelfLink {
		t.Fatalf("%v", err)
	}
	tps, err := f.GetTargetHttpsProxy(f.TPName(true))
	if err != nil || tps.UrlMap != um.SelfLink {
		t.Fatalf("%v", err)
	}
	fws, err := f.GetGlobalForwardingRule(f.FWName(true))
	if err != nil || fws.Target != tps.SelfLink {
		t.Fatalf("%v", err)
	}
	newName := "newName"
	namer = pool.(*L7s).Namer()
	namer.SetUID(newName)
	f.name = fmt.Sprintf("%v--%v", lbInfo.Name, newName)

	// Now the components should get renamed with the next suffix.
	pool.Sync([]*L7RuntimeInfo{lbInfo})
	l7, err = pool.Get(lbInfo.Name)
	if err != nil || namer.ParseName(l7.Name).ClusterName != newName {
		t.Fatalf("Expected L7 name to change.")
	}
	um, err = f.GetUrlMap(f.UMName())
	if err != nil || namer.ParseName(um.Name).ClusterName != newName {
		t.Fatalf("Expected urlmap name to change.")
	}
	if err != nil ||
		um.DefaultService != pool.(*L7s).GLBCDefaultBackend().SelfLink {
		t.Fatalf("%v", err)
	}

	tps, err = f.GetTargetHttpsProxy(f.TPName(true))
	if err != nil || tps.UrlMap != um.SelfLink {
		t.Fatalf("%v", err)
	}
	fws, err = f.GetGlobalForwardingRule(f.FWName(true))
	if err != nil || fws.Target != tps.SelfLink {
		t.Fatalf("%v", err)
	}
}

func TestInvalidClusterNameChange(t *testing.T) {
	namer := utils.NewNamer("test--123", "test--123")
	if got := namer.UID(); got != "123" {
		t.Fatalf("Expected name 123, got %v", got)
	}
	// A name with `--` should take the last token
	for _, testCase := range []struct{ newName, expected string }{
		{"foo--bar", "bar"},
		{"--", ""},
		{"", ""},
		{"foo--bar--com", "com"},
	} {
		namer.SetUID(testCase.newName)
		if got := namer.UID(); got != testCase.expected {
			t.Fatalf("Expected %q got %q", testCase.expected, got)
		}
	}

}
