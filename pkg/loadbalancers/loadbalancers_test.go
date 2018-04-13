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
	"k8s.io/ingress-gce/pkg/backends"
	"k8s.io/ingress-gce/pkg/healthchecks"
	"k8s.io/ingress-gce/pkg/instances"
	"k8s.io/ingress-gce/pkg/neg"
	"k8s.io/ingress-gce/pkg/utils"
)

const (
	defaultZone = "zone-a"
)

var (
	testDefaultBeNodePort = backends.ServicePort{NodePort: 3000, Protocol: annotations.ProtocolHTTP}
)

func newFakeLoadBalancerPool(f LoadBalancers, t *testing.T, namer *utils.Namer) LoadBalancerPool {
	fakeBackends := backends.NewFakeBackendServices(func(op int, be *compute.BackendService) error { return nil }, false)
	fakeIGs := instances.NewFakeInstanceGroups(sets.NewString(), namer)
	fakeHCP := healthchecks.NewFakeHealthCheckProvider()
	fakeNEG := neg.NewFakeNetworkEndpointGroupCloud("test-subnet", "test-network")
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

	// Run Sync
	pool.Sync([]*L7RuntimeInfo{lbInfo})

	l7, err := pool.Get(lbInfo.Name)
	if err != nil || l7 == nil {
		t.Fatalf("Expected l7 not created, err: %v", err)
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
		TLS:       []*TLSCerts{createCert("key", "cert", "name")},
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
	lbName := namer.LoadBalancer("test")
	certName1 := namer.SSLCertName(lbName, GetCertHash("cert"))
	certName2 := namer.SSLCertName(lbName, GetCertHash("cert2"))

	lbInfo := &L7RuntimeInfo{
		Name:      lbName,
		AllowHTTP: false,
		TLS:       []*TLSCerts{createCert("key", "cert", "name")},
	}

	f := NewFakeLoadBalancers(lbInfo.Name, namer)
	pool := newFakeLoadBalancerPool(f, t, namer)

	// Sync first cert
	pool.Sync([]*L7RuntimeInfo{lbInfo})

	// Verify certs
	t.Logf("lbName=%q, name=%q", lbName, certName1)
	expectCerts := map[string]string{certName1: lbInfo.TLS[0].Cert}
	verifyCertAndProxyLink(expectCerts, expectCerts, f, t)

	// Sync with different cert
	lbInfo.TLS = []*TLSCerts{createCert("key2", "cert2", "name")}
	pool.Sync([]*L7RuntimeInfo{lbInfo})
	expectCerts = map[string]string{certName2: lbInfo.TLS[0].Cert}
	verifyCertAndProxyLink(expectCerts, expectCerts, f, t)
}

// Test that multiple secrets with the same certificate value don't cause a sync error.
func TestMultipleSecretsWithSameCert(t *testing.T) {
	namer := utils.NewNamer("uid1", "fw1")
	lbName := namer.LoadBalancer("test")

	lbInfo := &L7RuntimeInfo{
		Name:      lbName,
		AllowHTTP: false,
		TLS: []*TLSCerts{
			createCert("key", "cert", "secret-a"),
			createCert("key", "cert", "secret-b"),
		},
	}
	f := NewFakeLoadBalancers(lbInfo.Name, namer)
	pool := newFakeLoadBalancerPool(f, t, namer)

	// Sync first cert
	if err := pool.Sync([]*L7RuntimeInfo{lbInfo}); err != nil {
		t.Fatalf("pool.Sync() = err %v", err)
	}
	certName := namer.SSLCertName(lbName, GetCertHash("cert"))
	expectCerts := map[string]string{certName: lbInfo.TLS[0].Cert}
	verifyCertAndProxyLink(expectCerts, expectCerts, f, t)
}

// Tests that controller can overwrite existing, unused certificates
func TestCertCreationWithCollision(t *testing.T) {
	namer := utils.NewNamer("uid1", "fw1")
	lbName := namer.LoadBalancer("test")
	certName1 := namer.SSLCertName(lbName, GetCertHash("cert"))
	certName2 := namer.SSLCertName(lbName, GetCertHash("cert2"))

	lbInfo := &L7RuntimeInfo{
		Name:      lbName,
		AllowHTTP: false,
		TLS:       []*TLSCerts{createCert("key", "cert", "name")},
	}
	f := NewFakeLoadBalancers(lbInfo.Name, namer)
	pool := newFakeLoadBalancerPool(f, t, namer)

	// Have the same name used by orphaned cert
	// Since name of the cert is the same, the contents of Certificate have to be the same too, since name contains a
	// hash of the contents.
	f.CreateSslCertificate(&compute.SslCertificate{
		Name:        certName1,
		Certificate: "cert",
		SelfLink:    "existing",
	})

	// Sync first cert
	pool.Sync([]*L7RuntimeInfo{lbInfo})
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
	pool.Sync([]*L7RuntimeInfo{lbInfo})
	expectCerts = map[string]string{certName2: "xyz"}
	// xyz instead of cert2 because the name collided and cert did not get updated.
	verifyCertAndProxyLink(expectCerts, expectCerts, f, t)
}

func TestMultipleCertRetentionAfterRestart(t *testing.T) {
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
	}
	expectCerts[certName1] = cert1.Cert

	f := NewFakeLoadBalancers(lbInfo.Name, namer)
	firstPool := newFakeLoadBalancerPool(f, t, namer)

	firstPool.Sync([]*L7RuntimeInfo{lbInfo})
	verifyCertAndProxyLink(expectCerts, expectCerts, f, t)
	// Update config to use 2 certs
	lbInfo.TLS = []*TLSCerts{cert1, cert2}
	expectCerts[certName2] = cert2.Cert
	firstPool.Sync([]*L7RuntimeInfo{lbInfo})
	verifyCertAndProxyLink(expectCerts, expectCerts, f, t)

	// Restart of controller represented by a new pool
	secondPool := newFakeLoadBalancerPool(f, t, namer)
	secondPool.Sync([]*L7RuntimeInfo{lbInfo})

	// Verify both certs are still present
	verifyCertAndProxyLink(expectCerts, expectCerts, f, t)

	// Replace the 2 certs with a different, single cert
	lbInfo.TLS = []*TLSCerts{cert3}
	expectCerts = map[string]string{certName3: cert3.Cert}
	secondPool.Sync([]*L7RuntimeInfo{lbInfo})
	// Only the new cert should be present
	verifyCertAndProxyLink(expectCerts, expectCerts, f, t)
}

//TestUpgradeToNewCertNames verifies that certs uploaded using the old naming convention
// are picked up and deleted when upgrading to the new scheme.
func TestUpgradeToNewCertNames(t *testing.T) {
	namer := utils.NewNamer("uid1", "fw1")
	lbName := namer.LoadBalancer("test")
	lbInfo := &L7RuntimeInfo{
		Name:      lbName,
		AllowHTTP: false,
	}
	oldCertName := "k8s-ssl-" + lbInfo.Name
	tlsCert := createCert("key", "cert", "name")
	lbInfo.TLS = []*TLSCerts{tlsCert}
	newCertName := namer.SSLCertName(lbName, tlsCert.CertHash)
	f := NewFakeLoadBalancers(lbInfo.Name, namer)
	pool := newFakeLoadBalancerPool(f, t, namer)

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
	pool.Sync([]*L7RuntimeInfo{lbInfo})
	// We expect to see only the new cert linked to the proxy and available in the load balancer.
	expectCerts := map[string]string{newCertName: tlsCert.Cert}
	verifyCertAndProxyLink(expectCerts, expectCerts, f, t)
}

// Tests uploading 10 certs which is the global limit today. Ensures that creation of the 11th cert fails.
func TestMaxCertsUpload(t *testing.T) {
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
	}
	f := NewFakeLoadBalancers(lbInfo.Name, namer)
	pool := newFakeLoadBalancerPool(f, t, namer)
	pool.Sync([]*L7RuntimeInfo{lbInfo})
	verifyCertAndProxyLink(expectCerts, expectCerts, f, t)
	failCert := createCert("key100", "cert100", "name100")
	lbInfo.TLS = append(lbInfo.TLS, failCert)
	pool.Sync([]*L7RuntimeInfo{lbInfo})
	verifyCertAndProxyLink(expectCerts, expectCerts, f, t)
}

// In case multiple certs for the same subject/hostname are uploaded, the certs will be sent in the same order they were
// specified, to the targetproxy. The targetproxy will present the first occuring cert for a given hostname to the client.
// This test verifies this behavior.
func TestIdenticalHostnameCerts(t *testing.T) {
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
	}
	f := NewFakeLoadBalancers(lbInfo.Name, namer)
	pool := newFakeLoadBalancerPool(f, t, namer)
	// Sync multiple times to make sure ordering is preserved
	for i := 0; i < 10; i++ {
		pool.Sync([]*L7RuntimeInfo{lbInfo})
		verifyCertAndProxyLink(expectCerts, expectCerts, f, t)
		// Fetch the target proxy certs and go through in order
		verifyProxyCertsInOrder(" foo.com", f, t)
		pool.Delete(lbInfo.Name)
	}
}

func TestIdenticalHostnameCertsPreShared(t *testing.T) {
	namer := utils.NewNamer("uid1", "fw1")
	lbInfo := &L7RuntimeInfo{
		Name:      namer.LoadBalancer("test"),
		AllowHTTP: false,
	}
	f := NewFakeLoadBalancers(lbInfo.Name, namer)
	pool := newFakeLoadBalancerPool(f, t, namer)
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
		pool.Sync([]*L7RuntimeInfo{lbInfo})
		verifyCertAndProxyLink(expectCerts, expectCerts, f, t)
		// Fetch the target proxy certs and go through in order
		verifyProxyCertsInOrder(" foo.com", f, t)
		pool.Delete(lbInfo.Name)
	}
}

// TestPreSharedToSecretBasedCertUpdate updates from pre-shared cert
// to secret based cert and verifies the pre-shared cert is retained.
func TestPreSharedToSecretBasedCertUpdate(t *testing.T) {
	namer := utils.NewNamer("uid1", "fw1")
	lbName := namer.LoadBalancer("test")
	certName1 := namer.SSLCertName(lbName, GetCertHash("cert"))
	certName2 := namer.SSLCertName(lbName, GetCertHash("cert2"))

	lbInfo := &L7RuntimeInfo{
		Name:      lbName,
		AllowHTTP: false,
	}

	f := NewFakeLoadBalancers(lbInfo.Name, namer)
	pool := newFakeLoadBalancerPool(f, t, namer)

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
	pool.Sync([]*L7RuntimeInfo{lbInfo})
	expectCerts := map[string]string{preSharedCert1.Name: preSharedCert1.Certificate,
		preSharedCert2.Name: preSharedCert2.Certificate}
	verifyCertAndProxyLink(expectCerts, expectCerts, f, t)

	// Updates from pre-shared cert to secret based cert.
	lbInfo.TLS = []*TLSCerts{createCert("key", "cert", "name")}
	lbInfo.TLSName = ""
	pool.Sync([]*L7RuntimeInfo{lbInfo})
	expectCerts[certName1] = lbInfo.TLS[0].Cert
	// fakeLoadBalancer contains the preshared certs as well, but proxy will use only certName1
	expectCertsProxy := map[string]string{certName1: lbInfo.TLS[0].Cert}
	verifyCertAndProxyLink(expectCerts, expectCertsProxy, f, t)

	// Sync a different cert.
	lbInfo.TLS = []*TLSCerts{createCert("key2", "cert2", "name")}
	pool.Sync([]*L7RuntimeInfo{lbInfo})
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

	for _, linkName := range tps.SslCertificates {
		cert, err := f.GetSslCertificate(getResourceNameFromLink(linkName))
		if err != nil {
			t.Fatalf("Failed to fetch certificate from link %s - %v", linkName, err)
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
	for _, linkName := range tps.SslCertificates {
		if _, ok := expectCerts[getResourceNameFromLink(linkName)]; !ok {
			t.Fatalf("unexpected ssl certificate linked in target proxy; Expected : %v; Target Proxy Certs: %v",
				expectCertsProxy, tps.SslCertificates)
		}
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
		TLS:       []*TLSCerts{{Key: "key", Cert: "cert"}},
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
			"/bar2": "bar2svc",
		},
	}
	um2 := utils.GCEURLMap{
		"foo.example.com": {
			"/foo1": "foo1svc",
			"/foo2": "foo2svc",
		},
		"bar.example.com": {
			"/bar1": "bar1svc",
		},
	}
	um2.PutDefaultBackendName("default")

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
			utils.DefaultBackendKey: utils.BackendServiceRelativeResourcePath("default"),
		},
		"foo.example.com": {
			"/foo1": utils.BackendServiceRelativeResourcePath("foo1svc"),
			"/foo2": utils.BackendServiceRelativeResourcePath("foo2svc"),
		},
		"bar.example.com": {
			"/bar1": utils.BackendServiceRelativeResourcePath("bar1svc"),
		},
	}
	if err := f.CheckURLMap(l7, expectedMap); err != nil {
		t.Errorf("CheckURLMap(...) = %v, want nil", err)
	}
}

func TestUpdateUrlMapNoChanges(t *testing.T) {
	um1 := utils.GCEURLMap{
		"foo.example.com": {
			"/foo1": "foo1svc",
			"/foo2": "foo2svc",
		},
		"bar.example.com": {
			"/bar1": "bar1svc",
		},
	}
	um1.PutDefaultBackendName("default")
	um2 := utils.GCEURLMap{
		"foo.example.com": {
			"/foo1": "foo1svc",
			"/foo2": "foo2svc",
		},
		"bar.example.com": {
			"/bar1": "bar1svc",
		},
	}
	um2.PutDefaultBackendName("default")

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
	namer := utils.NewNamer("uid1", "fw1")
	lbInfo := &L7RuntimeInfo{
		Name: namer.LoadBalancer("test"),
		TLS:  []*TLSCerts{{Key: "key", Cert: "cert"}},
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

func createCert(key string, contents string, name string) *TLSCerts {
	return &TLSCerts{Key: key, Cert: contents, Name: name, CertHash: GetCertHash(contents)}
}
