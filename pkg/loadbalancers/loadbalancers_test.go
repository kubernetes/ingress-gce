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
	"context"
	"fmt"
	"testing"

	compute "google.golang.org/api/compute/v1"
	"k8s.io/kubernetes/pkg/cloudprovider/providers/gce"
	"k8s.io/kubernetes/pkg/cloudprovider/providers/gce/cloud"
	"k8s.io/kubernetes/pkg/cloudprovider/providers/gce/cloud/meta"
	"k8s.io/kubernetes/pkg/cloudprovider/providers/gce/cloud/mock"

	"strconv"
	"strings"

	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/utils"
)

const (
	defaultZone = "zone-a"
)

var (
	testDefaultBeNodePort = utils.ServicePort{NodePort: 30000, Protocol: annotations.ProtocolHTTP}
)

type testJig struct {
	pool    LoadBalancerPool
	fakeGCE *gce.GCECloud
	namer   *utils.Namer
	t       *testing.T
}

func newTestJig(t *testing.T) *testJig {
	namer := utils.NewNamer("uid1", "fw1")
	fakeGCE := gce.FakeGCECloud(gce.DefaultTestClusterValues())
	(fakeGCE.Compute().(*cloud.MockGCE)).MockUrlMaps.UpdateHook = mock.UpdateUrlMapHook
	(fakeGCE.Compute().(*cloud.MockGCE)).MockTargetHttpProxies.SetUrlMapHook = mock.TargetHttpProxySetUrlMapHook
	(fakeGCE.Compute().(*cloud.MockGCE)).MockTargetHttpsProxies.SetUrlMapHook = mock.TargetHttpsProxySetUrlMapHook
	(fakeGCE.Compute().(*cloud.MockGCE)).MockTargetHttpsProxies.SetSslCertificatesHook = mock.TargetHttpsProxySetSslCertificatesHook
	return &testJig{
		pool:    NewLoadBalancerPool(fakeGCE, namer),
		fakeGCE: fakeGCE,
		namer:   namer,
		t:       t,
	}
}

func TestCreateHTTPLoadBalancer(t *testing.T) {
	// This should NOT create the forwarding rule and target proxy
	// associated with the HTTPS branch of this loadbalancer.
	j := newTestJig(t)

	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{utils.PathRule{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000}}})

	lbName := j.namer.LoadBalancer("test")
	lbInfo := &L7RuntimeInfo{
		Name:      lbName,
		AllowHTTP: true,
		UrlMap:    gceUrlMap,
	}

	if err := j.pool.Sync(lbInfo); err != nil {
		t.Fatalf("pool.Sync() = err %v", err)
	}

	l7, err := j.pool.Get(lbName)
	if err != nil || l7 == nil {
		t.Fatalf("Expected l7 not created, err: %v", err)
	}
	umName := j.namer.UrlMap(lbName)
	um, err := j.fakeGCE.GetUrlMap(umName)
	if err != nil {
		t.Fatalf("fakeGCE.GetUrlMap(%q) = _, %v; want nil", umName, err)
	}
	tpName := j.namer.TargetProxy(lbName, utils.HTTPProtocol)
	tp, err := j.fakeGCE.GetTargetHttpProxy(tpName)
	if err != nil {
		t.Fatalf("fakeGCE.GetTargetHttpProxy(%q) = _, %v; want nil", tpName, err)
	}
	if tp.UrlMap != um.SelfLink {
		t.Fatalf("%v", err)
	}
	fwName := j.namer.ForwardingRule(lbName, utils.HTTPProtocol)
	fw, err := j.fakeGCE.GetGlobalForwardingRule(fwName)
	if err != nil {
		t.Fatalf("fakeGCE.GetGlobalForwardingRule(%q) = _, %v, want nil", fwName, err)
	}
	if fw.Target != tp.SelfLink {
		t.Fatalf("%v", err)
	}
}

func TestCreateHTTPSLoadBalancer(t *testing.T) {
	// This should NOT create the forwarding rule and target proxy
	// associated with the HTTP branch of this loadbalancer.
	j := newTestJig(t)

	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{utils.PathRule{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000}}})

	lbName := j.namer.LoadBalancer("test")
	lbInfo := &L7RuntimeInfo{
		Name:      lbName,
		AllowHTTP: false,
		TLS:       []*TLSCerts{j.createCert("key", "cert", "name")},
		UrlMap:    gceUrlMap,
	}

	if err := j.pool.Sync(lbInfo); err != nil {
		t.Fatalf("pool.Sync() = err %v", err)
	}

	l7, err := j.pool.Get(lbName)
	if err != nil || l7 == nil {
		t.Fatalf("Expected l7 not created")
	}
	um, err := j.fakeGCE.GetUrlMap(j.namer.UrlMap(lbName))
	tps, err := j.fakeGCE.GetTargetHttpsProxy(j.namer.TargetProxy(lbName, utils.HTTPSProtocol))
	if err != nil || tps.UrlMap != um.SelfLink {
		t.Fatalf("%v", err)
	}
	fws, err := j.fakeGCE.GetGlobalForwardingRule(j.namer.ForwardingRule(lbName, utils.HTTPSProtocol))
	if err != nil || fws.Target != tps.SelfLink {
		t.Fatalf("%v", err)
	}
}

// Tests that a certificate is created from the provided Key/Cert combo
// and the proxy is updated to another cert when the provided cert changes
func TestCertUpdate(t *testing.T) {
	j := newTestJig(t)

	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{utils.PathRule{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000}}})

	lbName := j.namer.LoadBalancer("test")
	certName1 := j.namer.SSLCertName(lbName, GetCertHash("cert"))
	certName2 := j.namer.SSLCertName(lbName, GetCertHash("cert2"))
	lbInfo := &L7RuntimeInfo{
		Name:      lbName,
		AllowHTTP: false,
		TLS:       []*TLSCerts{j.createCert("key", "cert", "name")},
		UrlMap:    gceUrlMap,
	}

	// Sync first cert
	if err := j.pool.Sync(lbInfo); err != nil {
		t.Fatalf("pool.Sync() = err %v", err)
	}

	// Verify certs
	t.Logf("lbName=%q, name=%q", lbName, certName1)
	expectCerts := map[string]string{certName1: lbInfo.TLS[0].Cert}
	j.verifyCertAndProxyLink(expectCerts, expectCerts, lbName)

	// Sync with different cert
	lbInfo.TLS = []*TLSCerts{j.createCert("key2", "cert2", "name")}
	if err := j.pool.Sync(lbInfo); err != nil {
		t.Fatalf("pool.Sync() = err %v", err)
	}
	expectCerts = map[string]string{certName2: lbInfo.TLS[0].Cert}
	j.verifyCertAndProxyLink(expectCerts, expectCerts, lbName)
}

// Test that multiple secrets with the same certificate value don't cause a sync error.
func TestMultipleSecretsWithSameCert(t *testing.T) {
	j := newTestJig(t)

	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{utils.PathRule{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000}}})

	lbName := j.namer.LoadBalancer("test")
	lbInfo := &L7RuntimeInfo{
		Name:      lbName,
		AllowHTTP: false,
		TLS: []*TLSCerts{
			j.createCert("key", "cert", "secret-a"),
			j.createCert("key", "cert", "secret-b"),
		},
		UrlMap: gceUrlMap,
	}

	// Sync first cert
	if err := j.pool.Sync(lbInfo); err != nil {
		t.Fatalf("pool.Sync() = err %v", err)
	}
	certName := j.namer.SSLCertName(lbName, GetCertHash("cert"))
	expectCerts := map[string]string{certName: lbInfo.TLS[0].Cert}
	j.verifyCertAndProxyLink(expectCerts, expectCerts, lbName)
}

// Tests that controller can overwrite existing, unused certificates
func TestCertCreationWithCollision(t *testing.T) {
	j := newTestJig(t)

	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{utils.PathRule{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000}}})

	lbName := j.namer.LoadBalancer("test")
	certName1 := j.namer.SSLCertName(lbName, GetCertHash("cert"))
	certName2 := j.namer.SSLCertName(lbName, GetCertHash("cert2"))
	lbInfo := &L7RuntimeInfo{
		Name:      lbName,
		AllowHTTP: false,
		TLS:       []*TLSCerts{j.createCert("key", "cert", "name")},
		UrlMap:    gceUrlMap,
	}

	// Have the same name used by orphaned cert
	// Since name of the cert is the same, the contents of Certificate have to be the same too, since name contains a
	// hash of the contents.
	j.fakeGCE.CreateSslCertificate(&compute.SslCertificate{
		Name:        certName1,
		Certificate: "cert",
		SelfLink:    "existing",
	})

	// Sync first cert
	if err := j.pool.Sync(lbInfo); err != nil {
		t.Fatalf("pool.Sync() = err %v", err)
	}

	expectCerts := map[string]string{certName1: lbInfo.TLS[0].Cert}
	j.verifyCertAndProxyLink(expectCerts, expectCerts, lbName)

	// Create another cert where the name matches that of another cert, but contents are different - xyz != cert2.
	// Simulates a hash collision
	j.fakeGCE.CreateSslCertificate(&compute.SslCertificate{
		Name:        certName2,
		Certificate: "xyz",
		SelfLink:    "existing",
	})

	// Sync with different cert
	lbInfo.TLS = []*TLSCerts{j.createCert("key2", "cert2", "name")}
	if err := j.pool.Sync(lbInfo); err != nil {
		t.Fatalf("pool.Sync() = err %v", err)
	}
	expectCerts = map[string]string{certName2: "xyz"}
	// xyz instead of cert2 because the name collided and cert did not get updated.
	j.verifyCertAndProxyLink(expectCerts, expectCerts, lbName)
}

func TestMultipleCertRetentionAfterRestart(t *testing.T) {
	j := newTestJig(t)

	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{utils.PathRule{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000}}})

	cert1 := j.createCert("key", "cert", "name")
	cert2 := j.createCert("key2", "cert2", "name2")
	cert3 := j.createCert("key3", "cert3", "name3")
	lbName := j.namer.LoadBalancer("test")
	certName1 := j.namer.SSLCertName(lbName, cert1.CertHash)
	certName2 := j.namer.SSLCertName(lbName, cert2.CertHash)
	certName3 := j.namer.SSLCertName(lbName, cert3.CertHash)
	expectCerts := map[string]string{}
	lbInfo := &L7RuntimeInfo{
		Name:      lbName,
		AllowHTTP: false,
		TLS:       []*TLSCerts{cert1},
		UrlMap:    gceUrlMap,
	}
	expectCerts[certName1] = cert1.Cert

	firstPool := j.pool
	firstPool.Sync(lbInfo)
	j.verifyCertAndProxyLink(expectCerts, expectCerts, lbName)
	// Update config to use 2 certs
	lbInfo.TLS = []*TLSCerts{cert1, cert2}
	expectCerts[certName2] = cert2.Cert
	firstPool.Sync(lbInfo)
	j.verifyCertAndProxyLink(expectCerts, expectCerts, lbName)

	// Restart of controller represented by a new pool
	j.pool = NewLoadBalancerPool(j.fakeGCE, j.namer)
	secondPool := j.pool
	secondPool.Sync(lbInfo)

	// Verify both certs are still present
	j.verifyCertAndProxyLink(expectCerts, expectCerts, lbName)

	// Replace the 2 certs with a different, single cert
	lbInfo.TLS = []*TLSCerts{cert3}
	expectCerts = map[string]string{certName3: cert3.Cert}
	secondPool.Sync(lbInfo)
	// Only the new cert should be present
	j.verifyCertAndProxyLink(expectCerts, expectCerts, lbName)
}

//TestUpgradeToNewCertNames verifies that certs uploaded using the old naming convention
// are picked up and deleted when upgrading to the new scheme.
func TestUpgradeToNewCertNames(t *testing.T) {
	j := newTestJig(t)

	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{utils.PathRule{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000}}})
	lbName := j.namer.LoadBalancer("test")
	lbInfo := &L7RuntimeInfo{
		Name:      lbName,
		AllowHTTP: false,
		UrlMap:    gceUrlMap,
	}
	oldCertName := "k8s-ssl-" + lbInfo.Name
	tlsCert := j.createCert("key", "cert", "name")
	lbInfo.TLS = []*TLSCerts{tlsCert}
	newCertName := j.namer.SSLCertName(lbName, tlsCert.CertHash)

	// Manually create a target proxy and assign a legacy cert to it.
	sslCert := &compute.SslCertificate{Name: oldCertName, Certificate: "cert"}
	j.fakeGCE.CreateSslCertificate(sslCert)
	newProxy := &compute.TargetHttpsProxy{Name: j.namer.TargetProxy(lbName, utils.HTTPSProtocol), SslCertificates: []string{sslCert.SelfLink}}
	err := j.fakeGCE.CreateTargetHttpsProxy(newProxy)
	if err != nil {
		t.Fatalf("Failed to create Target proxy %v - %v", newProxy, err)
	}
	proxyCerts, err := j.fakeGCE.ListSslCertificates()
	if err != nil {
		t.Fatalf("Failed to list certs for load balancer: %v", err)
	}
	if len(proxyCerts) != 1 {
		t.Fatalf("Unexpected number of certs - Expected 1, got %d", len(proxyCerts))
	}

	if proxyCerts[0].Name != oldCertName {
		t.Fatalf("Expected cert with name %s, Got %s", oldCertName, proxyCerts[0].Name)
	}
	// Sync should replace this oldCert with one following the new naming scheme
	if err := j.pool.Sync(lbInfo); err != nil {
		t.Fatalf("pool.Sync() = err %v", err)
	}
	// We expect to see only the new cert linked to the proxy and available in the load balancer.
	expectCerts := map[string]string{newCertName: tlsCert.Cert}
	j.verifyCertAndProxyLink(expectCerts, expectCerts, lbName)
}

// In case multiple certs for the same subject/hostname are uploaded, the certs will be sent in the same order they were
// specified, to the targetproxy. The targetproxy will present the first occurring cert for a given hostname to the client.
// This test verifies this behavior.
func TestIdenticalHostnameCerts(t *testing.T) {
	j := newTestJig(t)

	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{utils.PathRule{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000}}})
	var tlsCerts []*TLSCerts
	expectCerts := make(map[string]string)
	lbName := j.namer.LoadBalancer("test")
	contents := ""

	for ix := 0; ix < 3; ix++ {
		str := strconv.Itoa(ix)
		contents = "cert-" + str + " foo.com"
		tlsCerts = append(tlsCerts, j.createCert("key-"+str, contents, "name-"+str))
		certName := j.namer.SSLCertName(lbName, GetCertHash(contents))
		expectCerts[certName] = contents
	}
	lbInfo := &L7RuntimeInfo{
		Name:      lbName,
		AllowHTTP: false,
		TLS:       tlsCerts,
		UrlMap:    gceUrlMap,
	}

	// Sync multiple times to make sure ordering is preserved
	for i := 0; i < 10; i++ {
		if err := j.pool.Sync(lbInfo); err != nil {
			t.Fatalf("pool.Sync() = err %v", err)
		}
		j.verifyCertAndProxyLink(expectCerts, expectCerts, lbName)
		// Fetch the target proxy certs and go through in order
		j.verifyProxyCertsInOrder("foo.com", lbName)
		j.pool.Delete(lbName)
	}
}

func TestIdenticalHostnameCertsPreShared(t *testing.T) {
	j := newTestJig(t)

	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{utils.PathRule{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000}}})
	lbName := j.namer.LoadBalancer("test")
	lbInfo := &L7RuntimeInfo{
		Name:      lbName,
		AllowHTTP: false,
		UrlMap:    gceUrlMap,
	}

	// Prepare pre-shared certs.
	preSharedCert1, _ := j.fakeGCE.CreateSslCertificate(&compute.SslCertificate{
		Name:        "test-pre-shared-cert",
		Certificate: "cert-0 foo.com",
		SelfLink:    "existing",
	})
	preSharedCert2, _ := j.fakeGCE.CreateSslCertificate(&compute.SslCertificate{
		Name:        "test-pre-shared-cert1",
		Certificate: "cert-1 foo.com",
		SelfLink:    "existing",
	})
	preSharedCert3, _ := j.fakeGCE.CreateSslCertificate(&compute.SslCertificate{
		Name:        "test-pre-shared-cert2",
		Certificate: "cert2",
		SelfLink:    "existing",
	})
	expectCerts := map[string]string{preSharedCert1.Name: preSharedCert1.Certificate,
		preSharedCert2.Name: preSharedCert2.Certificate, preSharedCert3.Name: preSharedCert3.Certificate}

	lbInfo.TLSName = preSharedCert1.Name + "," + preSharedCert2.Name + "," + preSharedCert3.Name
	// Sync multiple times to make sure ordering is preserved
	for i := 0; i < 10; i++ {
		if err := j.pool.Sync(lbInfo); err != nil {
			t.Fatalf("pool.Sync() = err %v", err)
		}
		j.verifyCertAndProxyLink(expectCerts, expectCerts, lbName)
		// Fetch the target proxy certs and go through in order
		j.verifyProxyCertsInOrder("foo.com", lbName)
		j.pool.Delete(lbName)
	}
}

// TestPreSharedToSecretBasedCertUpdate updates from pre-shared cert
// to secret based cert and verifies the pre-shared cert is retained.
func TestPreSharedToSecretBasedCertUpdate(t *testing.T) {
	j := newTestJig(t)

	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{utils.PathRule{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000}}})
	lbName := j.namer.LoadBalancer("test")
	certName1 := j.namer.SSLCertName(lbName, GetCertHash("cert"))
	certName2 := j.namer.SSLCertName(lbName, GetCertHash("cert2"))
	lbInfo := &L7RuntimeInfo{
		Name:      lbName,
		AllowHTTP: false,
		UrlMap:    gceUrlMap,
	}

	// Prepare pre-shared certs.
	preSharedCert1, _ := j.fakeGCE.CreateSslCertificate(&compute.SslCertificate{
		Name:        "test-pre-shared-cert",
		Certificate: "abc",
		SelfLink:    "existing",
	})
	// Prepare pre-shared certs.
	preSharedCert2, _ := j.fakeGCE.CreateSslCertificate(&compute.SslCertificate{
		Name:        "test-pre-shared-cert2",
		Certificate: "xyz",
		SelfLink:    "existing",
	})

	lbInfo.TLSName = preSharedCert1.Name + "," + preSharedCert2.Name

	// Sync pre-shared certs.
	if err := j.pool.Sync(lbInfo); err != nil {
		t.Fatalf("pool.Sync() = err %v", err)
	}
	expectCerts := map[string]string{preSharedCert1.Name: preSharedCert1.Certificate,
		preSharedCert2.Name: preSharedCert2.Certificate}
	j.verifyCertAndProxyLink(expectCerts, expectCerts, lbName)

	// Updates from pre-shared cert to secret based cert.
	lbInfo.TLS = []*TLSCerts{j.createCert("key", "cert", "name")}
	lbInfo.TLSName = ""
	if err := j.pool.Sync(lbInfo); err != nil {
		t.Fatalf("pool.Sync() = err %v", err)
	}
	expectCerts[certName1] = lbInfo.TLS[0].Cert
	// fakeLoadBalancer contains the preshared certs as well, but proxy will use only certName1
	expectCertsProxy := map[string]string{certName1: lbInfo.TLS[0].Cert}
	j.verifyCertAndProxyLink(expectCerts, expectCertsProxy, lbName)

	// Sync a different cert.
	lbInfo.TLS = []*TLSCerts{j.createCert("key2", "cert2", "name")}
	j.pool.Sync(lbInfo)
	delete(expectCerts, certName1)
	expectCerts[certName2] = lbInfo.TLS[0].Cert
	expectCertsProxy = map[string]string{certName2: lbInfo.TLS[0].Cert}
	j.verifyCertAndProxyLink(expectCerts, expectCertsProxy, lbName)

	// Check if pre-shared certs are retained.
	if cert, err := j.fakeGCE.GetSslCertificate(preSharedCert1.Name); err != nil || cert == nil {
		t.Fatalf("Want pre-shared certificate %v to exist, got none, err: %v", preSharedCert1.Name, err)
	}
	if cert, err := j.fakeGCE.GetSslCertificate(preSharedCert2.Name); err != nil || cert == nil {
		t.Fatalf("Want pre-shared certificate %v to exist, got none, err: %v", preSharedCert2.Name, err)
	}
}

func TestCreateHTTPSLoadBalancerAnnotationCert(t *testing.T) {
	// This should NOT create the forwarding rule and target proxy
	// associated with the HTTP branch of this loadbalancer.
	j := newTestJig(t)

	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{utils.PathRule{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000}}})
	tlsName := "external-cert-name"
	lbName := j.namer.LoadBalancer("test")
	lbInfo := &L7RuntimeInfo{
		Name:      lbName,
		AllowHTTP: false,
		TLSName:   tlsName,
		UrlMap:    gceUrlMap,
	}

	j.fakeGCE.CreateSslCertificate(&compute.SslCertificate{
		Name: tlsName,
	})

	if err := j.pool.Sync(lbInfo); err != nil {
		t.Fatalf("pool.Sync() = err %v", err)
	}
	l7, err := j.pool.Get(lbInfo.Name)
	if err != nil || l7 == nil {
		t.Fatalf("Expected l7 not created")
	}
	um, err := j.fakeGCE.GetUrlMap(j.namer.UrlMap(lbName))
	tps, err := j.fakeGCE.GetTargetHttpsProxy(j.namer.TargetProxy(lbName, utils.HTTPSProtocol))
	if err != nil || tps.UrlMap != um.SelfLink {
		t.Fatalf("%v", err)
	}
	fws, err := j.fakeGCE.GetGlobalForwardingRule(j.namer.ForwardingRule(lbName, utils.HTTPSProtocol))
	if err != nil || fws.Target != tps.SelfLink {
		t.Fatalf("%v", err)
	}
}

func TestCreateBothLoadBalancers(t *testing.T) {
	// This should create 2 forwarding rules and target proxies
	// but they should use the same urlmap, and have the same
	// static ip.
	j := newTestJig(t)

	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{utils.PathRule{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000}}})
	lbName := j.namer.LoadBalancer("test")
	lbInfo := &L7RuntimeInfo{
		Name:      lbName,
		AllowHTTP: true,
		TLS:       []*TLSCerts{{Key: "key", Cert: "cert"}},
		UrlMap:    gceUrlMap,
	}

	if err := j.pool.Sync(lbInfo); err != nil {
		t.Fatalf("pool.Sync() = err %v", err)
	}
	l7, err := j.pool.Get(lbInfo.Name)
	if err != nil || l7 == nil {
		t.Fatalf("Expected l7 not created")
	}
	um, err := j.fakeGCE.GetUrlMap(j.namer.UrlMap(lbName))
	tps, err := j.fakeGCE.GetTargetHttpsProxy(j.namer.TargetProxy(lbName, utils.HTTPSProtocol))
	if err != nil || tps.UrlMap != um.SelfLink {
		t.Fatalf("%v", err)
	}
	tp, err := j.fakeGCE.GetTargetHttpProxy(j.namer.TargetProxy(lbName, utils.HTTPProtocol))
	if err != nil || tp.UrlMap != um.SelfLink {
		t.Fatalf("%v", err)
	}
	fws, err := j.fakeGCE.GetGlobalForwardingRule(j.namer.ForwardingRule(lbName, utils.HTTPSProtocol))
	if err != nil || fws.Target != tps.SelfLink {
		t.Fatalf("%v", err)
	}
	fw, err := j.fakeGCE.GetGlobalForwardingRule(j.namer.ForwardingRule(lbName, utils.HTTPProtocol))
	if err != nil || fw.Target != tp.SelfLink {
		t.Fatalf("%v", err)
	}
	ip, err := j.fakeGCE.GetGlobalAddress(j.namer.ForwardingRule(lbName, utils.HTTPProtocol))
	if err != nil || ip.Address != fw.IPAddress || ip.Address != fws.IPAddress {
		t.Fatalf("%v", err)
	}
}

func TestUpdateUrlMap(t *testing.T) {
	j := newTestJig(t)

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

	lbInfo := &L7RuntimeInfo{
		Name:      j.namer.LoadBalancer("test"),
		AllowHTTP: true,
		UrlMap:    um1,
	}

	if err := j.pool.Sync(lbInfo); err != nil {
		t.Fatalf("pool.Sync() = err %v", err)
	}
	l7, err := j.pool.Get(lbInfo.Name)
	if err != nil {
		t.Fatalf("%v", err)
	}
	for _, ir := range []*utils.GCEURLMap{um1, um2} {
		lbInfo.UrlMap = ir
		if err := l7.UpdateUrlMap(); err != nil {
			t.Fatalf("%v", err)
		}
	}
	if err := j.checkURLMap(l7, um2); err != nil {
		t.Errorf("checkURLMap(...) = %v, want nil", err)
	}
}

func TestUpdateUrlMapNoChanges(t *testing.T) {
	j := newTestJig(t)

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

	lbInfo := &L7RuntimeInfo{
		Name:      j.namer.LoadBalancer("test"),
		AllowHTTP: true,
		UrlMap:    um1,
	}

	if err := j.pool.Sync(lbInfo); err != nil {
		t.Fatalf("pool.Sync() = err %v", err)
	}
	l7, err := j.pool.Get(lbInfo.Name)
	if err != nil {
		t.Fatalf("%v", err)
	}
	for _, ir := range []*utils.GCEURLMap{um1, um2} {
		lbInfo.UrlMap = ir
		if err := l7.UpdateUrlMap(); err != nil {
			t.Fatalf("%v", err)
		}

	}

	// Add hook to keep track of how many calls are made.
	updateCalls := 0
	(j.fakeGCE.Compute().(*cloud.MockGCE)).MockUrlMaps.UpdateHook = func(ctx context.Context, key *meta.Key, obj *compute.UrlMap, m *cloud.MockUrlMaps) error {
		updateCalls += 1
		return nil
	}
	if updateCalls > 0 {
		t.Errorf("UpdateUrlMap() should not have been called")
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
	j := newTestJig(t)

	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{utils.PathRule{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000}}})
	lbName := j.namer.LoadBalancer("test")
	lbInfo := &L7RuntimeInfo{
		Name:   lbName,
		TLS:    []*TLSCerts{{Key: "key", Cert: "cert"}},
		UrlMap: gceUrlMap,
	}

	if err := j.pool.Sync(lbInfo); err != nil {
		t.Fatalf("pool.Sync() = err %v", err)
	}
	l7, err := j.pool.Get(lbInfo.Name)
	if err != nil || l7 == nil {
		t.Fatalf("Expected l7 not created")
	}
	um, err := j.fakeGCE.GetUrlMap(j.namer.UrlMap(lbName))
	tps, err := j.fakeGCE.GetTargetHttpsProxy(j.namer.TargetProxy(lbName, utils.HTTPSProtocol))
	if err != nil || tps.UrlMap != um.SelfLink {
		t.Fatalf("%v", err)
	}
	fws, err := j.fakeGCE.GetGlobalForwardingRule(j.namer.ForwardingRule(lbName, utils.HTTPSProtocol))
	if err != nil || fws.Target != tps.SelfLink {
		t.Fatalf("%v", err)
	}
	newName := "newName"
	j.namer.SetUID(newName)

	// Now the components should get renamed with the next suffix.
	if err := j.pool.Sync(lbInfo); err != nil {
		t.Fatalf("pool.Sync() = err %v", err)
	}
	l7, err = j.pool.Get(lbInfo.Name)
	if err != nil || j.namer.ParseName(l7.Name).ClusterName != newName {
		t.Fatalf("Expected L7 name to change.")
	}
	um, err = j.fakeGCE.GetUrlMap(j.namer.UrlMap(lbName))
	if err != nil || j.namer.ParseName(um.Name).ClusterName != newName {
		t.Fatalf("Expected urlmap name to change.")
	}
	tps, err = j.fakeGCE.GetTargetHttpsProxy(j.namer.TargetProxy(lbName, utils.HTTPSProtocol))
	if err != nil || tps.UrlMap != um.SelfLink {
		t.Fatalf("%v", err)
	}
	fws, err = j.fakeGCE.GetGlobalForwardingRule(j.namer.ForwardingRule(lbName, utils.HTTPSProtocol))
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

// checkURLMap checks the compute.UrlMap maintained by the load balancer.
// We check against our internal representation.
func (j *testJig) checkURLMap(l7 *L7, expectedUrlMap *utils.GCEURLMap) error {
	um, err := j.fakeGCE.GetUrlMap(l7.UrlMap().Name)
	if err != nil || um == nil {
		return fmt.Errorf("f.GetUrlMap(%q) = %v, %v; want _, nil", l7.UrlMap().Name, um, err)
	}
	defaultBackendName := expectedUrlMap.DefaultBackend.BackendName(j.namer)
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
			} else if utils.BackendServiceRelativeResourcePath(svc.BackendName(j.namer)) != rule.Service {
				return fmt.Errorf("Expected service %v found %v", svc, rule.Service)
			}
		}
	}
	return nil
}

func (j *testJig) verifyProxyCertsInOrder(hostname, lbName string) {
	j.t.Helper()

	tpName := j.namer.TargetProxy(lbName, utils.HTTPSProtocol)
	tps, err := j.fakeGCE.GetTargetHttpsProxy(tpName)
	if err != nil {
		j.t.Fatalf("expected https proxy %q to exist: %v", tpName, err)
	}
	count := 0
	tmp := ""

	for _, linkName := range tps.SslCertificates {
		cert, err := j.fakeGCE.GetSslCertificate(getResourceNameFromLink(linkName))
		if err != nil {
			j.t.Fatalf("Failed to fetch certificate from link %s - %v", linkName, err)
		}
		if strings.HasSuffix(cert.Certificate, hostname) {
			// cert contents will be of the form "cert-<number> <hostname>", we want the certs with the smaller number
			// to show up first
			tmp = strings.TrimSuffix(cert.Certificate, hostname)
			if int(tmp[len(tmp)-1]-'0') != count {
				j.t.Fatalf("Found cert with index %c, contents - %s, Expected index %d", tmp[len(tmp)-1],
					cert.Certificate, count)
			}
			count++
		}
	}
}

// expectCerts is the mapping of certs expected in the FakeLoadBalancer. expectCertsProxy is the mapping of certs expected
// to be in use by the target proxy. Both values will be different for the PreSharedToSecretBasedCertUpdate test.
// f will contain the preshared as well as secret-based certs, but target proxy will contain only one or the other.
func (j *testJig) verifyCertAndProxyLink(expectCerts map[string]string, expectCertsProxy map[string]string, lbName string) {
	j.t.Helper()

	// f needs to contain only the certs in expectCerts, nothing more, nothing less
	allCerts, err := j.fakeGCE.ListSslCertificates()
	if err != nil {
		j.t.Fatalf("Failed to list certificates for: %v", err)
	}
	if len(expectCerts) != len(allCerts) {
		j.t.Fatalf("Unexpected number of certs. Expected %d, actual %d", len(expectCerts), len(allCerts))
	}
	for certName, certValue := range expectCerts {
		cert, err := j.fakeGCE.GetSslCertificate(certName)
		if err != nil {
			j.t.Fatalf("expected ssl certificate to exist: %v, err: %v, all certs: %v", certName, err, toCertNames(allCerts))
		}

		if cert.Certificate != certValue {
			j.t.Fatalf("unexpected certificate value for cert %s; expected %v, actual %v", certName, certValue, cert.Certificate)
		}
	}

	// httpsproxy needs to contain only the certs in expectCerts, nothing more, nothing less
	tpName := j.namer.TargetProxy(lbName, utils.HTTPSProtocol)
	tps, err := j.fakeGCE.GetTargetHttpsProxy(tpName)
	if err != nil {
		j.t.Fatalf("expected https proxy %q to exist: %v", tpName, err)
	}
	if len(tps.SslCertificates) != len(expectCertsProxy) {
		j.t.Fatalf("Expected https proxy to have %d certs, actual %d", len(expectCertsProxy), len(tps.SslCertificates))
	}
	for _, linkName := range tps.SslCertificates {
		if _, ok := expectCerts[getResourceNameFromLink(linkName)]; !ok {
			j.t.Fatalf("unexpected ssl certificate linked in target proxy; Expected : %v; Target Proxy Certs: %v",
				expectCertsProxy, tps.SslCertificates)
		}
	}
}

func (j *testJig) createCert(key string, contents string, name string) *TLSCerts {
	return &TLSCerts{Key: key, Cert: contents, Name: name, CertHash: GetCertHash(contents)}
}
