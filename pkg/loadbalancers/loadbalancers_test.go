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

	"strconv"
	"strings"

	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/instances"
	"k8s.io/ingress-gce/pkg/utils"
)

const (
	defaultZone = "zone-a"
)

var (
	testDefaultBeNodePort = utils.ServicePort{NodePort: 30000, Protocol: annotations.ProtocolHTTP}
)

func newFakeLoadBalancerPool(f LoadBalancers, t *testing.T, namer *utils.Namer, resyncWithCloud bool) LoadBalancerPool {
	fakeIGs := instances.NewFakeInstanceGroups(sets.NewString(), namer)
	nodePool := instances.NewNodePool(fakeIGs, namer)
	nodePool.Init(&instances.FakeZoneLister{Zones: []string{defaultZone}})

	return NewLoadBalancerPool(f, namer, resyncWithCloud)
}

func TestCreateHTTPLoadBalancer(t *testing.T) {
	// This should NOT create the forwarding rule and target proxy
	// associated with the HTTPS branch of this loadbalancer.
	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{utils.PathRule{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000}}})
	namer := utils.NewNamer("uid1", "fw1")
	lbInfo := &L7RuntimeInfo{
		Name:      namer.LoadBalancer("test"),
		AllowHTTP: true,
		UrlMap:    gceUrlMap,
	}
	f := NewFakeLoadBalancers(lbInfo.Name, namer)
	pool := newFakeLoadBalancerPool(f, t, namer, false)

	if _, err := pool.Sync(lbInfo); err != nil {
		t.Fatalf("pool.Sync() = err %v", err)
	}

	if !pool.Get(lbInfo.Name) {
		t.Fatalf("Expected l7 not created")
	}
	verifyHTTPForwardingRuleAndProxyLinks(t, f)
}

func TestCreateHTTPSLoadBalancer(t *testing.T) {
	// This should NOT create the forwarding rule and target proxy
	// associated with the HTTP branch of this loadbalancer.
	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{utils.PathRule{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000}}})
	namer := utils.NewNamer("uid1", "fw1")
	lbInfo := &L7RuntimeInfo{
		Name:      namer.LoadBalancer("test"),
		AllowHTTP: false,
		TLS:       []*TLSCerts{createCert("key", "cert", "name")},
		UrlMap:    gceUrlMap,
	}
	f := NewFakeLoadBalancers(lbInfo.Name, namer)
	pool := newFakeLoadBalancerPool(f, t, namer, false)

	if _, err := pool.Sync(lbInfo); err != nil {
		t.Fatalf("pool.Sync() = err %v", err)
	}

	if !pool.Get(lbInfo.Name) {
		t.Fatalf("Expected l7 not created")
	}
	verifyHTTPSForwardingRuleAndProxyLinks(t, f)
}

func verifyHTTPSForwardingRuleAndProxyLinks(t *testing.T, f *FakeLoadBalancers) {
	t.Helper()

	um, err := f.GetUrlMap(f.UMName())
	tps, err := f.GetTargetHttpsProxy(f.TPName(true))
	if err != nil {
		t.Fatalf("f.GetTargetHttpsProxy(%q) = _, %v; want nil", f.TPName(true), err)
	}
	if !utils.EqualResourcePaths(tps.UrlMap, um.SelfLink) {
		t.Fatalf("tps.UrlMap = %q, want %q", tps.UrlMap, um.SelfLink)
	}
	fws, err := f.GetGlobalForwardingRule(f.FWName(true))
	if err != nil {
		t.Fatalf("f.GetGlobalForwardingRule(%q) = _, %v, want nil", f.FWName(true), err)
	}
	if !utils.EqualResourcePaths(fws.Target, tps.SelfLink) {
		t.Fatalf("fws.Target = %q, want %q", fws.Target, tps.SelfLink)
	}
}

func verifyHTTPForwardingRuleAndProxyLinks(t *testing.T, f *FakeLoadBalancers) {
	t.Helper()

	um, err := f.GetUrlMap(f.UMName())
	tps, err := f.GetTargetHttpProxy(f.TPName(false))
	if err != nil {
		t.Fatalf("f.GetTargetHttpProxy(%q) = _, %v; want nil", f.TPName(false), err)
	}
	if !utils.EqualResourcePaths(tps.UrlMap, um.SelfLink) {
		t.Fatalf("tp.UrlMap = %q, want %q", tps.UrlMap, um.SelfLink)
	}
	fws, err := f.GetGlobalForwardingRule(f.FWName(false))
	if err != nil {
		t.Fatalf("f.GetGlobalForwardingRule(%q) = _, %v, want nil", f.FWName(false), err)
	}
	if !utils.EqualResourcePaths(fws.Target, tps.SelfLink) {
		t.Fatalf("fw.Target = %q, want %q", fws.Target, tps.SelfLink)
	}
}

// Tests that a certificate is created from the provided Key/Cert combo
// and the proxy is updated to another cert when the provided cert changes
func TestCertUpdate(t *testing.T) {
	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{utils.PathRule{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000}}})
	namer := utils.NewNamer("uid1", "fw1")
	lbName := namer.LoadBalancer("test")
	certName1 := namer.SSLCertName(lbName, GetCertHash("cert"))
	certName2 := namer.SSLCertName(lbName, GetCertHash("cert2"))

	lbInfo := &L7RuntimeInfo{
		Name:      lbName,
		AllowHTTP: false,
		TLS:       []*TLSCerts{createCert("key", "cert", "name")},
		UrlMap:    gceUrlMap,
	}

	f := NewFakeLoadBalancers(lbInfo.Name, namer)
	pool := newFakeLoadBalancerPool(f, t, namer, false)

	// Sync first cert
	if _, err := pool.Sync(lbInfo); err != nil {
		t.Fatalf("pool.Sync() = err %v", err)
	}

	// Verify certs
	t.Logf("lbName=%q, name=%q", lbName, certName1)
	expectCerts := map[string]string{certName1: lbInfo.TLS[0].Cert}
	verifyCertAndProxyLink(expectCerts, expectCerts, f, t)

	// Sync with different cert
	lbInfo.TLS = []*TLSCerts{createCert("key2", "cert2", "name")}
	if _, err := pool.Sync(lbInfo); err != nil {
		t.Fatalf("pool.Sync() = err %v", err)
	}
	expectCerts = map[string]string{certName2: lbInfo.TLS[0].Cert}
	verifyCertAndProxyLink(expectCerts, expectCerts, f, t)
}

// Test that multiple secrets with the same certificate value don't cause a sync error.
func TestMultipleSecretsWithSameCert(t *testing.T) {
	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{utils.PathRule{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000}}})
	namer := utils.NewNamer("uid1", "fw1")
	lbName := namer.LoadBalancer("test")

	lbInfo := &L7RuntimeInfo{
		Name:      lbName,
		AllowHTTP: false,
		TLS: []*TLSCerts{
			createCert("key", "cert", "secret-a"),
			createCert("key", "cert", "secret-b"),
		},
		UrlMap: gceUrlMap,
	}
	f := NewFakeLoadBalancers(lbInfo.Name, namer)
	pool := newFakeLoadBalancerPool(f, t, namer, false)

	// Sync first cert
	if _, err := pool.Sync(lbInfo); err != nil {
		t.Fatalf("pool.Sync() = err %v", err)
	}
	certName := namer.SSLCertName(lbName, GetCertHash("cert"))
	expectCerts := map[string]string{certName: lbInfo.TLS[0].Cert}
	verifyCertAndProxyLink(expectCerts, expectCerts, f, t)
}

// Tests that controller can overwrite existing, unused certificates
func TestCertCreationWithCollision(t *testing.T) {
	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{utils.PathRule{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000}}})
	namer := utils.NewNamer("uid1", "fw1")
	lbName := namer.LoadBalancer("test")
	certName1 := namer.SSLCertName(lbName, GetCertHash("cert"))
	certName2 := namer.SSLCertName(lbName, GetCertHash("cert2"))

	lbInfo := &L7RuntimeInfo{
		Name:      lbName,
		AllowHTTP: false,
		TLS:       []*TLSCerts{createCert("key", "cert", "name")},
		UrlMap:    gceUrlMap,
	}
	f := NewFakeLoadBalancers(lbInfo.Name, namer)
	pool := newFakeLoadBalancerPool(f, t, namer, false)

	// Have the same name used by orphaned cert
	// Since name of the cert is the same, the contents of Certificate have to be the same too, since name contains a
	// hash of the contents.
	f.CreateSslCertificate(&compute.SslCertificate{
		Name:        certName1,
		Certificate: "cert",
		SelfLink:    "existing",
	})

	// Sync first cert
	if _, err := pool.Sync(lbInfo); err != nil {
		t.Fatalf("pool.Sync() = err %v", err)
	}

	expectCerts := map[string]string{certName1: lbInfo.TLS[0].Cert}
	verifyCertAndProxyLink(expectCerts, expectCerts, f, t)

	// Create another cert where the name matches that of another cert, but contents are different - xyz != cert2.
	// Simulates a hash collision
	f.CreateSslCertificate(&compute.SslCertificate{
		Name:        certName2,
		Certificate: "xyz",
		SelfLink:    "existing",
	})

	// Sync with different cert
	lbInfo.TLS = []*TLSCerts{createCert("key2", "cert2", "name")}
	if _, err := pool.Sync(lbInfo); err != nil {
		t.Fatalf("pool.Sync() = err %v", err)
	}
	expectCerts = map[string]string{certName2: "xyz"}
	// xyz instead of cert2 because the name collided and cert did not get updated.
	verifyCertAndProxyLink(expectCerts, expectCerts, f, t)
}

func TestMultipleCertRetentionAfterRestart(t *testing.T) {
	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{utils.PathRule{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000}}})
	namer := utils.NewNamer("uid1", "fw1")
	cert1 := createCert("key", "cert", "name")
	cert2 := createCert("key2", "cert2", "name2")
	cert3 := createCert("key3", "cert3", "name3")
	lbName := namer.LoadBalancer("test")
	certName1 := namer.SSLCertName(lbName, cert1.CertHash)
	certName2 := namer.SSLCertName(lbName, cert2.CertHash)
	certName3 := namer.SSLCertName(lbName, cert3.CertHash)

	expectCerts := map[string]string{}

	lbInfo := &L7RuntimeInfo{
		Name:      lbName,
		AllowHTTP: false,
		TLS:       []*TLSCerts{cert1},
		UrlMap:    gceUrlMap,
	}
	expectCerts[certName1] = cert1.Cert

	f := NewFakeLoadBalancers(lbInfo.Name, namer)
	firstPool := newFakeLoadBalancerPool(f, t, namer, false)

	firstPool.Sync(lbInfo)
	verifyCertAndProxyLink(expectCerts, expectCerts, f, t)
	// Update config to use 2 certs
	lbInfo.TLS = []*TLSCerts{cert1, cert2}
	expectCerts[certName2] = cert2.Cert
	firstPool.Sync(lbInfo)
	verifyCertAndProxyLink(expectCerts, expectCerts, f, t)

	// Restart of controller represented by a new pool
	secondPool := newFakeLoadBalancerPool(f, t, namer, false)
	secondPool.Sync(lbInfo)
	// Verify both certs are still present
	verifyCertAndProxyLink(expectCerts, expectCerts, f, t)

	// Replace the 2 certs with a different, single cert
	lbInfo.TLS = []*TLSCerts{cert3}
	expectCerts = map[string]string{certName3: cert3.Cert}
	secondPool.Sync(lbInfo)
	// Only the new cert should be present
	verifyCertAndProxyLink(expectCerts, expectCerts, f, t)
}

//TestUpgradeToNewCertNames verifies that certs uploaded using the old naming convention
// are picked up and deleted when upgrading to the new scheme.
func TestUpgradeToNewCertNames(t *testing.T) {
	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{utils.PathRule{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000}}})
	namer := utils.NewNamer("uid1", "fw1")
	lbName := namer.LoadBalancer("test")
	lbInfo := &L7RuntimeInfo{
		Name:      lbName,
		AllowHTTP: false,
		UrlMap:    gceUrlMap,
	}
	oldCertName := "k8s-ssl-" + lbInfo.Name
	tlsCert := createCert("key", "cert", "name")
	lbInfo.TLS = []*TLSCerts{tlsCert}
	newCertName := namer.SSLCertName(lbName, tlsCert.CertHash)
	f := NewFakeLoadBalancers(lbInfo.Name, namer)
	pool := newFakeLoadBalancerPool(f, t, namer, false)

	// Manually create a target proxy and assign a legacy cert to it.
	sslCert := &compute.SslCertificate{Name: oldCertName, Certificate: "cert"}
	f.CreateSslCertificate(sslCert)
	tpName := f.TPName(true)
	newProxy := &compute.TargetHttpsProxy{Name: tpName, SslCertificates: []string{sslCert.SelfLink}}
	err := f.CreateTargetHttpsProxy(newProxy)
	if err != nil {
		t.Fatalf("Failed to create Target proxy %v - %v", newProxy, err)
	}
	proxyCerts, err := f.ListSslCertificates()
	if err != nil {
		t.Fatalf("Failed to list certs for load balancer %v - %v", f, err)
	}
	if len(proxyCerts) != 1 {
		t.Fatalf("Unexpected number of certs - Expected 1, got %d", len(proxyCerts))
	}

	if proxyCerts[0].Name != oldCertName {
		t.Fatalf("Expected cert with name %s, Got %s", oldCertName, proxyCerts[0].Name)
	}
	// Sync should replace this oldCert with one following the new naming scheme
	if _, err := pool.Sync(lbInfo); err != nil {
		t.Fatalf("pool.Sync() = err %v", err)
	}
	// We expect to see only the new cert linked to the proxy and available in the load balancer.
	expectCerts := map[string]string{newCertName: tlsCert.Cert}
	verifyCertAndProxyLink(expectCerts, expectCerts, f, t)
}

// Tests uploading 10 certs which is the global limit today. Ensures that creation of the 11th cert fails.
func TestMaxCertsUpload(t *testing.T) {
	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{utils.PathRule{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000}}})
	var tlsCerts []*TLSCerts
	expectCerts := make(map[string]string)
	namer := utils.NewNamer("uid1", "fw1")
	lbName := namer.LoadBalancer("test")

	for ix := 0; ix < TargetProxyCertLimit; ix++ {
		str := strconv.Itoa(ix)
		tlsCerts = append(tlsCerts, createCert("key-"+str, "cert-"+str, "name-"+str))
		certName := namer.SSLCertName(lbName, GetCertHash("cert-"+str))
		expectCerts[certName] = "cert-" + str
	}
	lbInfo := &L7RuntimeInfo{
		Name:      lbName,
		AllowHTTP: false,
		TLS:       tlsCerts,
		UrlMap:    gceUrlMap,
	}
	f := NewFakeLoadBalancers(lbInfo.Name, namer)
	pool := newFakeLoadBalancerPool(f, t, namer, false)

	if _, err := pool.Sync(lbInfo); err != nil {
		t.Fatalf("pool.Sync() = err %v", err)
	}

	verifyCertAndProxyLink(expectCerts, expectCerts, f, t)
	failCert := createCert("key100", "cert100", "name100")
	lbInfo.TLS = append(lbInfo.TLS, failCert)
	pool.Sync(lbInfo)
	verifyCertAndProxyLink(expectCerts, expectCerts, f, t)
}

// In case multiple certs for the same subject/hostname are uploaded, the certs will be sent in the same order they were
// specified, to the targetproxy. The targetproxy will present the first occurring cert for a given hostname to the client.
// This test verifies this behavior.
func TestIdenticalHostnameCerts(t *testing.T) {
	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{utils.PathRule{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000}}})
	var tlsCerts []*TLSCerts
	expectCerts := make(map[string]string)
	namer := utils.NewNamer("uid1", "fw1")
	lbName := namer.LoadBalancer("test")
	contents := ""

	for ix := 0; ix < 3; ix++ {
		str := strconv.Itoa(ix)
		contents = "cert-" + str + " foo.com"
		tlsCerts = append(tlsCerts, createCert("key-"+str, contents, "name-"+str))
		certName := namer.SSLCertName(lbName, GetCertHash(contents))
		expectCerts[certName] = contents
	}
	lbInfo := &L7RuntimeInfo{
		Name:      lbName,
		AllowHTTP: false,
		TLS:       tlsCerts,
		UrlMap:    gceUrlMap,
	}
	f := NewFakeLoadBalancers(lbInfo.Name, namer)
	pool := newFakeLoadBalancerPool(f, t, namer, false)
	// Sync multiple times to make sure ordering is preserved
	for i := 0; i < 10; i++ {
		if _, err := pool.Sync(lbInfo); err != nil {
			t.Fatalf("pool.Sync() = err %v", err)
		}
		verifyCertAndProxyLink(expectCerts, expectCerts, f, t)
		// Fetch the target proxy certs and go through in order
		verifyProxyCertsInOrder(" foo.com", f, t)
		pool.Delete(lbInfo.Name)
	}
}

func TestIdenticalHostnameCertsPreShared(t *testing.T) {
	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{utils.PathRule{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000}}})
	namer := utils.NewNamer("uid1", "fw1")
	lbInfo := &L7RuntimeInfo{
		Name:      namer.LoadBalancer("test"),
		AllowHTTP: false,
		UrlMap:    gceUrlMap,
	}
	f := NewFakeLoadBalancers(lbInfo.Name, namer)
	pool := newFakeLoadBalancerPool(f, t, namer, false)
	// Prepare pre-shared certs.
	preSharedCert1, _ := f.CreateSslCertificate(&compute.SslCertificate{
		Name:        "test-pre-shared-cert",
		Certificate: "cert-0 foo.com",
		SelfLink:    "existing",
	})
	preSharedCert2, _ := f.CreateSslCertificate(&compute.SslCertificate{
		Name:        "test-pre-shared-cert1",
		Certificate: "cert-1 foo.com",
		SelfLink:    "existing",
	})
	preSharedCert3, _ := f.CreateSslCertificate(&compute.SslCertificate{
		Name:        "test-pre-shared-cert2",
		Certificate: "cert2",
		SelfLink:    "existing",
	})
	expectCerts := map[string]string{preSharedCert1.Name: preSharedCert1.Certificate,
		preSharedCert2.Name: preSharedCert2.Certificate, preSharedCert3.Name: preSharedCert3.Certificate}

	lbInfo.TLSName = preSharedCert1.Name + "," + preSharedCert2.Name + "," + preSharedCert3.Name
	// Sync multiple times to make sure ordering is preserved
	for i := 0; i < 10; i++ {
		if _, err := pool.Sync(lbInfo); err != nil {
			t.Fatalf("pool.Sync() = err %v", err)
		}
		verifyCertAndProxyLink(expectCerts, expectCerts, f, t)
		// Fetch the target proxy certs and go through in order
		verifyProxyCertsInOrder(" foo.com", f, t)
		pool.Delete(lbInfo.Name)
	}
}

// TestPreSharedToSecretBasedCertUpdate updates from pre-shared cert
// to secret based cert and verifies the pre-shared cert is retained.
func TestPreSharedToSecretBasedCertUpdate(t *testing.T) {
	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{utils.PathRule{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000}}})
	namer := utils.NewNamer("uid1", "fw1")
	lbName := namer.LoadBalancer("test")
	certName1 := namer.SSLCertName(lbName, GetCertHash("cert"))
	certName2 := namer.SSLCertName(lbName, GetCertHash("cert2"))

	lbInfo := &L7RuntimeInfo{
		Name:      lbName,
		AllowHTTP: false,
		UrlMap:    gceUrlMap,
	}

	f := NewFakeLoadBalancers(lbInfo.Name, namer)
	pool := newFakeLoadBalancerPool(f, t, namer, false)

	// Prepare pre-shared certs.
	preSharedCert1, _ := f.CreateSslCertificate(&compute.SslCertificate{
		Name:        "test-pre-shared-cert",
		Certificate: "abc",
		SelfLink:    "existing",
	})
	// Prepare pre-shared certs.
	preSharedCert2, _ := f.CreateSslCertificate(&compute.SslCertificate{
		Name:        "test-pre-shared-cert2",
		Certificate: "xyz",
		SelfLink:    "existing",
	})

	lbInfo.TLSName = preSharedCert1.Name + "," + preSharedCert2.Name

	// Sync pre-shared certs.
	if _, err := pool.Sync(lbInfo); err != nil {
		t.Fatalf("pool.Sync() = err %v", err)
	}
	expectCerts := map[string]string{preSharedCert1.Name: preSharedCert1.Certificate,
		preSharedCert2.Name: preSharedCert2.Certificate}
	verifyCertAndProxyLink(expectCerts, expectCerts, f, t)

	// Updates from pre-shared cert to secret based cert.
	lbInfo.TLS = []*TLSCerts{createCert("key", "cert", "name")}
	lbInfo.TLSName = ""
	if _, err := pool.Sync(lbInfo); err != nil {
		t.Fatalf("pool.Sync() = err %v", err)
	}
	expectCerts[certName1] = lbInfo.TLS[0].Cert
	// fakeLoadBalancer contains the preshared certs as well, but proxy will use only certName1
	expectCertsProxy := map[string]string{certName1: lbInfo.TLS[0].Cert}
	verifyCertAndProxyLink(expectCerts, expectCertsProxy, f, t)

	// Sync a different cert.
	lbInfo.TLS = []*TLSCerts{createCert("key2", "cert2", "name")}
	pool.Sync(lbInfo)
	delete(expectCerts, certName1)
	expectCerts[certName2] = lbInfo.TLS[0].Cert
	expectCertsProxy = map[string]string{certName2: lbInfo.TLS[0].Cert}
	verifyCertAndProxyLink(expectCerts, expectCertsProxy, f, t)

	// Check if pre-shared certs are retained.
	if cert, err := f.GetSslCertificate(preSharedCert1.Name); err != nil || cert == nil {
		t.Fatalf("Want pre-shared certificate %v to exist, got none, err: %v", preSharedCert1.Name, err)
	}
	if cert, err := f.GetSslCertificate(preSharedCert2.Name); err != nil || cert == nil {
		t.Fatalf("Want pre-shared certificate %v to exist, got none, err: %v", preSharedCert2.Name, err)
	}
}

func verifyProxyCertsInOrder(hostname string, f *FakeLoadBalancers, t *testing.T) {
	t.Helper()
	t.Logf("f =\n%s", f.String())

	tps, err := f.GetTargetHttpsProxy(f.TPName(true))
	if err != nil {
		t.Fatalf("expected https proxy to exist: %v, err: %v", f.TPName(true), err)
	}
	count := 0
	tmp := ""

	for _, link := range tps.SslCertificates {
		certName, _ := utils.KeyName(link)
		cert, err := f.GetSslCertificate(certName)
		if err != nil {
			t.Fatalf("Failed to fetch certificate from link %s - %v", link, err)
		}
		if strings.HasSuffix(cert.Certificate, hostname) {
			// cert contents will be of the form "cert-<number> <hostname>", we want the certs with the smaller number
			// to show up first
			tmp = strings.TrimSuffix(cert.Certificate, hostname)
			if int(tmp[len(tmp)-1]-'0') != count {
				t.Fatalf("Found cert with index %c, contents - %s, Expected index %d", tmp[len(tmp)-1],
					cert.Certificate, count)
			}
			count++
		}
	}
}

// expectCerts is the mapping of certs expected in the FakeLoadBalancer. expectCertsProxy is the mapping of certs expected
// to be in use by the target proxy. Both values will be different for the PreSharedToSecretBasedCertUpdate test.
// f will contain the preshared as well as secret-based certs, but target proxy will contain only one or the other.
func verifyCertAndProxyLink(expectCerts map[string]string, expectCertsProxy map[string]string, f *FakeLoadBalancers, t *testing.T) {
	t.Helper()
	t.Logf("f =\n%s", f.String())

	// f needs to contain only the certs in expectCerts, nothing more, nothing less
	allCerts, err := f.ListSslCertificates()
	if err != nil {
		t.Fatalf("Failed to list certificates for %v - %v", f, err)
	}
	if len(expectCerts) != len(allCerts) {
		t.Fatalf("Unexpected number of certs in FakeLoadBalancer %v, expected %d, actual %d", f, len(expectCerts),
			len(allCerts))
	}
	for certName, certValue := range expectCerts {
		cert, err := f.GetSslCertificate(certName)
		if err != nil {
			t.Fatalf("expected ssl certificate to exist: %v, err: %v, all certs: %v", certName, err, toCertNames(allCerts))
		}

		if cert.Certificate != certValue {
			t.Fatalf("unexpected certificate value for cert %s; expected %v, actual %v", certName, certValue, cert.Certificate)
		}
	}

	// httpsproxy needs to contain only the certs in expectCerts, nothing more, nothing less
	tps, err := f.GetTargetHttpsProxy(f.TPName(true))
	if err != nil {
		t.Fatalf("expected https proxy to exist: %v, err: %v", f.TPName(true), err)
	}
	if len(tps.SslCertificates) != len(expectCertsProxy) {
		t.Fatalf("Expected https proxy to have %d certs, actual %d", len(expectCertsProxy), len(tps.SslCertificates))
	}
	for _, link := range tps.SslCertificates {
		certName, _ := utils.KeyName(link)
		if _, ok := expectCerts[certName]; !ok {
			t.Fatalf("unexpected ssl certificate linked in target proxy; Expected : %v; Target Proxy Certs: %v",
				expectCertsProxy, tps.SslCertificates)
		}
	}
}

func TestCreateHTTPSLoadBalancerAnnotationCert(t *testing.T) {
	// This should NOT create the forwarding rule and target proxy
	// associated with the HTTP branch of this loadbalancer.
	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{utils.PathRule{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000}}})
	tlsName := "external-cert-name"
	namer := utils.NewNamer("uid1", "fw1")
	lbInfo := &L7RuntimeInfo{
		Name:      namer.LoadBalancer("test"),
		AllowHTTP: false,
		TLSName:   tlsName,
		UrlMap:    gceUrlMap,
	}

	f := NewFakeLoadBalancers(lbInfo.Name, namer)
	f.CreateSslCertificate(&compute.SslCertificate{
		Name: tlsName,
	})
	pool := newFakeLoadBalancerPool(f, t, namer, false)
	if _, err := pool.Sync(lbInfo); err != nil {
		t.Fatalf("pool.Sync() = err %v", err)
	}
	if !pool.Get(lbInfo.Name) {
		t.Fatalf("Expected l7 not created")
	}
	verifyHTTPSForwardingRuleAndProxyLinks(t, f)
}

func TestCreateBothLoadBalancers(t *testing.T) {
	// This should create 2 forwarding rules and target proxies
	// but they should use the same urlmap, and have the same
	// static ip.
	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{utils.PathRule{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000}}})
	namer := utils.NewNamer("uid1", "fw1")
	lbInfo := &L7RuntimeInfo{
		Name:      namer.LoadBalancer("test"),
		AllowHTTP: true,
		TLS:       []*TLSCerts{{Key: "key", Cert: "cert"}},
		UrlMap:    gceUrlMap,
	}
	f := NewFakeLoadBalancers(lbInfo.Name, namer)
	pool := newFakeLoadBalancerPool(f, t, namer, false)

	if _, err := pool.Sync(lbInfo); err != nil {
		t.Fatalf("pool.Sync() = err %v", err)
	}
	if !pool.Get(lbInfo.Name) {
		t.Fatalf("Expected l7 not created")
	}

	verifyHTTPSForwardingRuleAndProxyLinks(t, f)
	verifyHTTPForwardingRuleAndProxyLinks(t, f)

	// We know the forwarding rules exist, retrieve their addresses.
	fws, _ := f.GetGlobalForwardingRule(f.FWName(true))
	fw, _ := f.GetGlobalForwardingRule(f.FWName(false))
	ip, err := f.GetGlobalAddress(f.FWName(false))
	if err != nil {
		t.Fatalf("%v", err)
	}
	if ip.Address != fw.IPAddress || ip.Address != fws.IPAddress {
		t.Fatalf("ip.Address = %q, want %q and %q all equal", ip.Address, fw.IPAddress, fws.IPAddress)
	}
}

// verifyURLMap gets the created URLMap and compares it against an expected one.
func verifyURLMap(t *testing.T, f *FakeLoadBalancers, name string, wantGCEURLMap *utils.GCEURLMap) {
	t.Helper()

	um, err := f.GetUrlMap(name)
	if err != nil || um == nil {
		t.Errorf("f.GetUrlMap(%q) = %v, %v; want _, nil", name, um, err)
	}
	wantComputeURLMap := toComputeURLMap(name, wantGCEURLMap, f.namer)
	if !mapsEqual(wantComputeURLMap, um) {
		t.Errorf("mapsEqual() = false, got\n%+v\n  want\n%+v", um, wantComputeURLMap)
	}
}

func TestUrlMapChange(t *testing.T) {
	um1 := utils.NewGCEURLMap()
	um2 := utils.NewGCEURLMap()

	um1.PutPathRulesForHost("bar.example.com", []utils.PathRule{utils.PathRule{Path: "/bar2", Backend: utils.ServicePort{NodePort: 30000}}})
	um1.DefaultBackend = &utils.ServicePort{NodePort: 31234}

	um2.PutPathRulesForHost("foo.example.com", []utils.PathRule{
		utils.PathRule{Path: "/foo1", Backend: utils.ServicePort{NodePort: 30001}},
		utils.PathRule{Path: "/foo2", Backend: utils.ServicePort{NodePort: 30002}},
	})
	um2.PutPathRulesForHost("bar.example.com", []utils.PathRule{utils.PathRule{Path: "/bar1", Backend: utils.ServicePort{NodePort: 30003}}})
	um2.DefaultBackend = &utils.ServicePort{NodePort: 30004}

	namer := utils.NewNamer("uid1", "fw1")
	lbInfo := &L7RuntimeInfo{Name: namer.LoadBalancer("test"), AllowHTTP: true, UrlMap: um1}

	f := NewFakeLoadBalancers(lbInfo.Name, namer)
	pool := newFakeLoadBalancerPool(f, t, namer, false)
	if _, err := pool.Sync(lbInfo); err != nil {
		t.Fatalf("pool.Sync() = err %v", err)
	}

	if !pool.Get(lbInfo.Name) {
		t.Fatalf("Expected l7 not created")
	}
	verifyURLMap(t, f, namer.UrlMap(lbInfo.Name), um1)

	// Change url map.
	lbInfo.UrlMap = um2
	if _, err := pool.Sync(lbInfo); err != nil {
		t.Fatalf("pool.Sync() = err %v", err)
	}
	if !pool.Get(lbInfo.Name) {
		t.Fatalf("Expected l7 does not exist")
	}
	verifyURLMap(t, f, namer.UrlMap(lbInfo.Name), um2)
}

func TestPoolSyncNoChanges(t *testing.T) {
	um1 := utils.NewGCEURLMap()
	um2 := utils.NewGCEURLMap()

	um1.PutPathRulesForHost("foo.example.com", []utils.PathRule{
		utils.PathRule{Path: "/foo1", Backend: utils.ServicePort{NodePort: 30000}},
		utils.PathRule{Path: "/foo2", Backend: utils.ServicePort{NodePort: 30001}},
	})
	um1.PutPathRulesForHost("bar.example.com", []utils.PathRule{utils.PathRule{Path: "/bar1", Backend: utils.ServicePort{NodePort: 30002}}})
	um1.DefaultBackend = &utils.ServicePort{NodePort: 30003}

	um2.PutPathRulesForHost("foo.example.com", []utils.PathRule{
		utils.PathRule{Path: "/foo1", Backend: utils.ServicePort{NodePort: 30000}},
		utils.PathRule{Path: "/foo2", Backend: utils.ServicePort{NodePort: 30001}},
	})
	um2.PutPathRulesForHost("bar.example.com", []utils.PathRule{utils.PathRule{Path: "/bar1", Backend: utils.ServicePort{NodePort: 30002}}})
	um2.DefaultBackend = &utils.ServicePort{NodePort: 30003}

	namer := utils.NewNamer("uid1", "fw1")
	lbInfo := &L7RuntimeInfo{Name: namer.LoadBalancer("test"), AllowHTTP: true, UrlMap: um1}
	f := NewFakeLoadBalancers(lbInfo.Name, namer)
	pool := newFakeLoadBalancerPool(f, t, namer, false)
	if _, err := pool.Sync(lbInfo); err != nil {
		t.Fatalf("pool.Sync() = err %v", err)
	}

	lbInfo.UrlMap = um2
	if _, err := pool.Sync(lbInfo); err != nil {
		t.Fatalf("pool.Sync() = err %v", err)
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
		fmt.Sprintf("%v/forwarding-rule", annotations.StatusPrefix): fullName,
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
	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{utils.PathRule{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000}}})
	namer := utils.NewNamer("uid1", "fw1")
	lbInfo := &L7RuntimeInfo{
		Name:      namer.LoadBalancer("test"),
		AllowHTTP: true,
		TLS:       []*TLSCerts{{Key: "key", Cert: "cert"}},
		UrlMap:    gceUrlMap,
	}
	f := NewFakeLoadBalancers(lbInfo.Name, namer)
	pool := newFakeLoadBalancerPool(f, t, namer, false)
	if _, err := pool.Sync(lbInfo); err != nil {
		t.Fatalf("pool.Sync() = err %v", err)
	}
	if !pool.Get(lbInfo.Name) {
		t.Fatalf("Expected l7 not created")
	}
	verifyHTTPSForwardingRuleAndProxyLinks(t, f)
	verifyHTTPForwardingRuleAndProxyLinks(t, f)

	newName := "newName"
	namer = pool.(*L7s).Namer()
	namer.SetUID(newName)
	f.name = fmt.Sprintf("%v--%v", lbInfo.Name, newName)

	// Now the components should get renamed with the next suffix.
	l7, err := pool.Sync(lbInfo)
	if err != nil {
		t.Fatalf("pool.Sync() = err %v", err)
	}
	if !pool.Get(lbInfo.Name) {
		t.Fatalf("Expected l7 does not exist")
	}
	if namer.ParseName(l7.Name).ClusterName != newName {
		t.Fatalf("Expected L7 name to change.")
	}
	verifyHTTPSForwardingRuleAndProxyLinks(t, f)
	verifyHTTPForwardingRuleAndProxyLinks(t, f)
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

func createCert(key string, contents string, name string) *TLSCerts {
	return &TLSCerts{Key: key, Cert: contents, Name: name, CertHash: GetCertHash(contents)}
}
