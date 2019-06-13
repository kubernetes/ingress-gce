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
	meta "github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"k8s.io/ingress-gce/pkg/composite"
	"reflect"
	"testing"

	extensions "k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	"strconv"
	"strings"

	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/events"
	"k8s.io/ingress-gce/pkg/instances"
	"k8s.io/ingress-gce/pkg/utils"
)

const (
	clusterName = "uid1"
	ingressName = "test"
	namespace   = "namespace1"
	defaultZone = "zone-a"
)

var (
	testDefaultBeNodePort = utils.ServicePort{NodePort: 30000, Protocol: annotations.ProtocolHTTP}
)

func newIngress() *extensions.Ingress {
	return &extensions.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ingressName,
			Namespace: namespace,
		},
	}
}

func newILBIngress() *extensions.Ingress {
	return &extensions.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:        ingressName,
			Namespace:   namespace,
			Annotations: map[string]string{annotations.IngressClassKey: annotations.GceILBIngressClass},
		},
	}
}

func newFakeLoadBalancerPool(f LoadBalancers, t *testing.T, namer *utils.Namer) LoadBalancerPool {
	fakeIGs := instances.NewFakeInstanceGroups(sets.NewString(), namer)
	nodePool := instances.NewNodePool(fakeIGs, namer)
	nodePool.Init(&instances.FakeZoneLister{Zones: []string{defaultZone}})

	return NewLoadBalancerPool(f, namer, events.RecorderProducerMock{})
}

func TestCreateHTTPLoadBalancer(t *testing.T) {
	// This should NOT create the forwarding rule and target proxy
	// associated with the HTTPS branch of this loadbalancer.
	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{utils.PathRule{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000}}})
	namer := utils.NewNamer(clusterName, "fw1")
	lbInfo := &L7RuntimeInfo{
		Name:      namer.LoadBalancer(ingressName),
		AllowHTTP: true,
		UrlMap:    gceUrlMap,
		Ingress:   newIngress(),
	}
	f := NewFakeLoadBalancers(lbInfo.Name, namer, meta.VersionGA, meta.Global)
	pool := newFakeLoadBalancerPool(f, t, namer)

	l7, err := pool.Ensure(lbInfo)
	if err != nil || l7 == nil {
		t.Fatalf("Expected l7 not created, err: %v", err)
	}
	verifyHTTPForwardingRuleAndProxyLinks(t, f)
}

func TestCreateHTTPSLoadBalancer(t *testing.T) {
	// This should NOT create the forwarding rule and target proxy
	// associated with the HTTP branch of this loadbalancer.
	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{utils.PathRule{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000}}})
	namer := utils.NewNamer(clusterName, "fw1")
	lbInfo := &L7RuntimeInfo{
		Name:      namer.LoadBalancer(ingressName),
		AllowHTTP: false,
		TLS:       []*TLSCerts{createCert("key", "cert", "name")},
		UrlMap:    gceUrlMap,
		Ingress:   newIngress(),
	}
	f := NewFakeLoadBalancers(lbInfo.Name, namer, meta.VersionGA, meta.Global)
	pool := newFakeLoadBalancerPool(f, t, namer)

	l7, err := pool.Ensure(lbInfo)
	if err != nil || l7 == nil {
		t.Fatalf("Expected l7 not created")
	}
	verifyHTTPSForwardingRuleAndProxyLinks(t, f)
}

// Test creation of a L7-ILB
func TestCreateHTTPILBLoadBalancer(t *testing.T) {
	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{utils.PathRule{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000}}})
	namer := utils.NewNamer(clusterName, "fw1")
	lbInfo := &L7RuntimeInfo{
		Name:      namer.LoadBalancer(ingressName),
		AllowHTTP: true,
		UrlMap:    gceUrlMap,
		Ingress:   newILBIngress(),
	}
	f := NewFakeLoadBalancers(lbInfo.Name, namer, ILBVersion, meta.Regional)
	pool := newFakeLoadBalancerPool(f, t, namer)

	l7, err := pool.Ensure(lbInfo)
	if err != nil || l7 == nil {
		t.Fatalf("Expected l7 not created, err: %v", err)
	}
	verifyHTTPForwardingRuleAndProxyLinks(t, f)
}

func TestCreateHTTPSILBLoadBalancer(t *testing.T) {
	// This should NOT create the forwarding rule and target proxy
	// associated with the HTTP branch of this loadbalancer.
	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{utils.PathRule{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000}}})
	namer := utils.NewNamer(clusterName, "fw1")
	lbInfo := &L7RuntimeInfo{
		Name:      namer.LoadBalancer(ingressName),
		AllowHTTP: false,
		TLS:       []*TLSCerts{createCert("key", "cert", "name")},
		UrlMap:    gceUrlMap,
		Ingress:   newILBIngress(),
	}
	f := NewFakeLoadBalancers(lbInfo.Name, namer, ILBVersion, meta.Regional)
	pool := newFakeLoadBalancerPool(f, t, namer)

	l7, err := pool.Ensure(lbInfo)
	if err != nil || l7 == nil {
		t.Fatalf("Expected l7 not created")
	}
	verifyHTTPSForwardingRuleAndProxyLinks(t, f)
}

func verifyHTTPSForwardingRuleAndProxyLinks(t *testing.T, f *FakeLoadBalancers) {
	t.Helper()
	regional := f.ResourceType == meta.Regional

	um, err := f.GetUrlMap(f.Version, f.CreateKey(f.UMName(), regional))
	if err != nil {
		t.Fatalf("f.GetUrlMap(%q) = _, %v; want nil", f.UMName(), err)
	}
	tps, err := f.GetTargetHttpsProxy(f.Version, f.CreateKey(f.TPName(true), regional))
	if err != nil {
		t.Fatalf("f.GetTargetHTTPSProxy(%q) = _, %v; want nil", f.TPName(true), err)
	}
	if !utils.EqualResourcePaths(tps.UrlMap, um.SelfLink) {
		t.Fatalf("tps.UrlMap = %q, want %q", tps.UrlMap, um.SelfLink)
	}
	fws, err := f.GetForwardingRule(f.Version, f.CreateKey(f.FWName(true), regional))
	if err != nil {
		t.Fatalf("f.GetGlobalForwardingRule(%q) = _, %v, want nil", f.FWName(true), err)
	}
	if !utils.EqualResourcePaths(fws.Target, tps.SelfLink) {
		t.Fatalf("fws.Target = %q, want %q", fws.Target, tps.SelfLink)
	}
	if fws.Description == "" {
		t.Errorf("fws.Description not set; expected it to be")
	}
}

func verifyHTTPForwardingRuleAndProxyLinks(t *testing.T, f *FakeLoadBalancers) {
	t.Helper()
	regional := f.ResourceType == meta.Regional

	um, err := f.GetUrlMap(f.Version, f.CreateKey(f.UMName(), regional))
	if err != nil {
		t.Fatalf("f.GetUrlMap(%q) = _, %v; want nil", f.UMName(), err)
	}
	tp, err := f.GetTargetHttpProxy(f.Version, f.CreateKey(f.TPName(false), regional))
	if err != nil {
		t.Fatalf("f.GetTargetHTTPProxy(%q) = _, %v; want nil", f.TPName(false), err)
	}
	if !utils.EqualResourcePaths(tp.UrlMap, um.SelfLink) {
		t.Fatalf("tp.UrlMap = %q, want %q", tp.UrlMap, um.SelfLink)
	}
	fws, err := f.GetForwardingRule(f.Version, f.CreateKey(f.FWName(false), regional))
	if err != nil {
		t.Fatalf("f.GetGlobalForwardingRule(%q) = _, %v, want nil", f.FWName(false), err)
	}
	if !utils.EqualResourcePaths(fws.Target, tp.SelfLink) {
		t.Fatalf("fw.Target = %q, want %q", fws.Target, tp.SelfLink)
	}
	if fws.Description == "" {
		t.Errorf("fws.Description not set; expected it to be")
	}
}

// Tests that a certificate is created from the provided Key/Cert combo
// and the proxy is updated to another cert when the provided cert changes
func TestCertUpdate(t *testing.T) {
	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{utils.PathRule{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000}}})
	namer := utils.NewNamer(clusterName, "fw1")
	lbName := namer.LoadBalancer(ingressName)
	certName1 := namer.SSLCertName(lbName, GetCertHash("cert"))
	certName2 := namer.SSLCertName(lbName, GetCertHash("cert2"))

	lbInfo := &L7RuntimeInfo{
		Name:      lbName,
		AllowHTTP: false,
		TLS:       []*TLSCerts{createCert("key", "cert", "name")},
		UrlMap:    gceUrlMap,
		Ingress:   newIngress(),
	}

	f := NewFakeLoadBalancers(lbInfo.Name, namer, meta.VersionGA, meta.Global)
	pool := newFakeLoadBalancerPool(f, t, namer)

	// Sync first cert
	if _, err := pool.Ensure(lbInfo); err != nil {
		t.Fatalf("pool.Ensure() = err %v", err)
	}

	// Verify certs
	t.Logf("lbName=%q, name=%q", lbName, certName1)
	expectCerts := map[string]string{certName1: lbInfo.TLS[0].Cert}
	verifyCertAndProxyLink(expectCerts, expectCerts, f, t)

	// Sync with different cert
	lbInfo.TLS = []*TLSCerts{createCert("key2", "cert2", "name")}
	if _, err := pool.Ensure(lbInfo); err != nil {
		t.Fatalf("pool.Ensure() = err %v", err)
	}
	expectCerts = map[string]string{certName2: lbInfo.TLS[0].Cert}
	verifyCertAndProxyLink(expectCerts, expectCerts, f, t)
}

// Test that multiple secrets with the same certificate value don't cause a sync error.
func TestMultipleSecretsWithSameCert(t *testing.T) {
	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{utils.PathRule{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000}}})
	namer := utils.NewNamer(clusterName, "fw1")
	lbName := namer.LoadBalancer(ingressName)

	lbInfo := &L7RuntimeInfo{
		Name:      lbName,
		AllowHTTP: false,
		TLS: []*TLSCerts{
			createCert("key", "cert", "secret-a"),
			createCert("key", "cert", "secret-b"),
		},
		UrlMap:  gceUrlMap,
		Ingress: newIngress(),
	}
	f := NewFakeLoadBalancers(lbInfo.Name, namer, meta.VersionGA, meta.Global)
	pool := newFakeLoadBalancerPool(f, t, namer)

	// Sync first cert
	if _, err := pool.Ensure(lbInfo); err != nil {
		t.Fatalf("pool.Ensure() = err %v", err)
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
	namer := utils.NewNamer(clusterName, "fw1")
	lbName := namer.LoadBalancer(ingressName)
	certName1 := namer.SSLCertName(lbName, GetCertHash("cert"))
	certName2 := namer.SSLCertName(lbName, GetCertHash("cert2"))

	lbInfo := &L7RuntimeInfo{
		Name:      lbName,
		AllowHTTP: false,
		TLS:       []*TLSCerts{createCert("key", "cert", "name")},
		UrlMap:    gceUrlMap,
		Ingress:   newIngress(),
	}
	f := NewFakeLoadBalancers(lbInfo.Name, namer, meta.VersionGA, meta.Global)
	pool := newFakeLoadBalancerPool(f, t, namer)

	// Have the same name used by orphaned cert
	// Since name of the cert is the same, the contents of Certificate have to be the same too, since name contains a
	// hash of the contents.
	f.CreateSslCertificate(&composite.SslCertificate{
		Name:        certName1,
		Certificate: "cert",
		SelfLink:    "existing",
	}, f.CreateKey(certName1, false))

	// Sync first cert
	if _, err := pool.Ensure(lbInfo); err != nil {
		t.Fatalf("pool.Ensure() = err %v", err)
	}

	expectCerts := map[string]string{certName1: lbInfo.TLS[0].Cert}
	verifyCertAndProxyLink(expectCerts, expectCerts, f, t)

	// Create another cert where the name matches that of another cert, but contents are different - xyz != cert2.
	// Simulates a hash collision
	f.CreateSslCertificate(&composite.SslCertificate{
		Name:        certName2,
		Certificate: "xyz",
		SelfLink:    "existing",
	}, f.CreateKey(certName2, false))

	// Sync with different cert
	lbInfo.TLS = []*TLSCerts{createCert("key2", "cert2", "name")}
	if _, err := pool.Ensure(lbInfo); err != nil {
		t.Fatalf("pool.Ensure() = err %v", err)
	}
	expectCerts = map[string]string{certName2: "xyz"}
	// xyz instead of cert2 because the name collided and cert did not get updated.
	verifyCertAndProxyLink(expectCerts, expectCerts, f, t)
}

func TestMultipleCertRetentionAfterRestart(t *testing.T) {
	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{utils.PathRule{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000}}})
	namer := utils.NewNamer(clusterName, "fw1")
	cert1 := createCert("key", "cert", "name")
	cert2 := createCert("key2", "cert2", "name2")
	cert3 := createCert("key3", "cert3", "name3")
	lbName := namer.LoadBalancer(ingressName)
	certName1 := namer.SSLCertName(lbName, cert1.CertHash)
	certName2 := namer.SSLCertName(lbName, cert2.CertHash)
	certName3 := namer.SSLCertName(lbName, cert3.CertHash)

	expectCerts := map[string]string{}

	lbInfo := &L7RuntimeInfo{
		Name:      lbName,
		AllowHTTP: false,
		TLS:       []*TLSCerts{cert1},
		UrlMap:    gceUrlMap,
		Ingress:   newIngress(),
	}
	expectCerts[certName1] = cert1.Cert

	f := NewFakeLoadBalancers(lbInfo.Name, namer, meta.VersionGA, meta.Global)
	firstPool := newFakeLoadBalancerPool(f, t, namer)

	firstPool.Ensure(lbInfo)
	verifyCertAndProxyLink(expectCerts, expectCerts, f, t)
	// Update config to use 2 certs
	lbInfo.TLS = []*TLSCerts{cert1, cert2}
	expectCerts[certName2] = cert2.Cert
	firstPool.Ensure(lbInfo)
	verifyCertAndProxyLink(expectCerts, expectCerts, f, t)

	// Restart of controller represented by a new pool
	secondPool := newFakeLoadBalancerPool(f, t, namer)
	secondPool.Ensure(lbInfo)
	// Verify both certs are still present
	verifyCertAndProxyLink(expectCerts, expectCerts, f, t)

	// Replace the 2 certs with a different, single cert
	lbInfo.TLS = []*TLSCerts{cert3}
	expectCerts = map[string]string{certName3: cert3.Cert}
	secondPool.Ensure(lbInfo)
	// Only the new cert should be present
	verifyCertAndProxyLink(expectCerts, expectCerts, f, t)
}

//TestUpgradeToNewCertNames verifies that certs uploaded using the old naming convention
// are picked up and deleted when upgrading to the new scheme.
func TestUpgradeToNewCertNames(t *testing.T) {
	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{utils.PathRule{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000}}})
	namer := utils.NewNamer(clusterName, "fw1")
	lbName := namer.LoadBalancer(ingressName)
	lbInfo := &L7RuntimeInfo{
		Name:      lbName,
		AllowHTTP: false,
		UrlMap:    gceUrlMap,
		Ingress:   newIngress(),
	}
	oldCertName := "k8s-ssl-" + lbInfo.Name
	tlsCert := createCert("key", "cert", "name")
	lbInfo.TLS = []*TLSCerts{tlsCert}
	newCertName := namer.SSLCertName(lbName, tlsCert.CertHash)
	f := NewFakeLoadBalancers(lbInfo.Name, namer, meta.VersionGA, meta.Global)
	pool := newFakeLoadBalancerPool(f, t, namer)

	// Manually create a target proxy and assign a legacy cert to it.
	sslCert := &composite.SslCertificate{Name: oldCertName, Certificate: "cert"}
	f.CreateSslCertificate(sslCert, f.CreateKey(sslCert.Name, false))
	tpName := f.TPName(true)
	newProxy := &composite.TargetHttpsProxy{Name: tpName,
		SslCertificates: []string{sslCert.SelfLink},
		Description:     "fake"}
	err := f.CreateTargetHttpsProxy(newProxy, f.CreateKey(tpName, false))
	if err != nil {
		t.Fatalf("Failed to create Target proxy %v - %v", newProxy, err)
	}
	proxyCerts, err := f.ListSslCertificates(f.Version, f.CreateKey("", false))
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
	if _, err := pool.Ensure(lbInfo); err != nil {
		t.Fatalf("pool.Ensure() = err %v", err)
	}
	// We expect to see only the new cert linked to the proxy and available in the load balancer.
	expectCerts := map[string]string{newCertName: tlsCert.Cert}
	verifyCertAndProxyLink(expectCerts, expectCerts, f, t)
}

// Tests uploading 15 certs which is the limit for the fake loadbalancer. Ensures that creation of the 16th cert fails.
// Tests uploading 10 certs which is the target proxy limit. Uploading 11th cert should fail.
func TestMaxCertsUpload(t *testing.T) {
	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{utils.PathRule{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000}}})
	var tlsCerts []*TLSCerts
	expectCerts := make(map[string]string)
	expectCertsExtra := make(map[string]string)
	namer := utils.NewNamer(clusterName, "fw1")
	lbName := namer.LoadBalancer(ingressName)

	for ix := 0; ix < FakeCertQuota; ix++ {
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
		Ingress:   newIngress(),
	}
	f := NewFakeLoadBalancers(lbInfo.Name, namer, meta.VersionGA, meta.Global)
	pool := newFakeLoadBalancerPool(f, t, namer)

	if _, err := pool.Ensure(lbInfo); err != nil {
		t.Fatalf("pool.Ensure() = err %v", err)
	}

	verifyCertAndProxyLink(expectCerts, expectCerts, f, t)
	failCert := createCert("key100", "cert100", "name100")
	lbInfo.TLS = append(lbInfo.TLS, failCert)
	_, err := pool.Ensure(lbInfo)
	if err == nil {
		t.Fatalf("Creating more than %d certs should have errored out", FakeCertQuota)
	}
	verifyCertAndProxyLink(expectCerts, expectCerts, f, t)
	// Set cert count less than cert creation limit but more than target proxy limit
	lbInfo.TLS = lbInfo.TLS[:TargetProxyCertLimit+1]
	for _, cert := range lbInfo.TLS {
		expectCertsExtra[namer.SSLCertName(lbName, cert.CertHash)] = cert.Cert
	}
	_, err = pool.Ensure(lbInfo)
	if err == nil {
		t.Fatalf("Assigning more than %d certs should have errored out", TargetProxyCertLimit)
	}
	// load balancer will contain the extra cert, but target proxy will not
	verifyCertAndProxyLink(expectCertsExtra, expectCerts, f, t)
	// Removing the extra cert from ingress spec should delete it
	lbInfo.TLS = lbInfo.TLS[:TargetProxyCertLimit]
	_, err = pool.Ensure(lbInfo)
	if err != nil {
		t.Fatalf("Unexpected error %s", err)
	}
	expectCerts = make(map[string]string)
	for _, cert := range lbInfo.TLS {
		expectCerts[namer.SSLCertName(lbName, cert.CertHash)] = cert.Cert
	}
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
	namer := utils.NewNamer(clusterName, "fw1")
	lbName := namer.LoadBalancer(ingressName)
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
		Ingress:   newIngress(),
	}
	f := NewFakeLoadBalancers(lbInfo.Name, namer, meta.VersionGA, meta.Global)
	pool := newFakeLoadBalancerPool(f, t, namer)
	// Sync multiple times to make sure ordering is preserved
	for i := 0; i < 10; i++ {
		if _, err := pool.Ensure(lbInfo); err != nil {
			t.Fatalf("pool.Ensure() = err %v", err)
		}
		verifyCertAndProxyLink(expectCerts, expectCerts, f, t)
		// Fetch the target proxy certs and go through in order
		verifyProxyCertsInOrder(" foo.com", f, t)
		pool.Delete(lbInfo.Name, false)
	}
}

func TestIdenticalHostnameCertsPreShared(t *testing.T) {
	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{utils.PathRule{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000}}})
	namer := utils.NewNamer(clusterName, "fw1")
	lbInfo := &L7RuntimeInfo{
		Name:      namer.LoadBalancer(ingressName),
		AllowHTTP: false,
		UrlMap:    gceUrlMap,
		Ingress:   newIngress(),
	}
	f := NewFakeLoadBalancers(lbInfo.Name, namer, meta.VersionGA, meta.Global)
	pool := newFakeLoadBalancerPool(f, t, namer)
	// Prepare pre-shared certs.
	f.CreateSslCertificate(&composite.SslCertificate{
		Name:        "test-pre-shared-cert",
		Certificate: "cert-0 foo.com",
		SelfLink:    "existing",
	}, f.CreateKey("test-pre-shared-cert", false))
	preSharedCert1, _ := f.GetSslCertificate(f.Version, f.CreateKey("test-pre-shared-cert", false))

	f.CreateSslCertificate(&composite.SslCertificate{
		Name:        "test-pre-shared-cert1",
		Certificate: "cert-1 foo.com",
		SelfLink:    "existing",
	}, f.CreateKey("test-pre-shared-cert1", false))
	preSharedCert2, _ := f.GetSslCertificate(f.Version, f.CreateKey("test-pre-shared-cert1", false))

	f.CreateSslCertificate(&composite.SslCertificate{
		Name:        "test-pre-shared-cert2",
		Certificate: "cert2",
		SelfLink:    "existing",
	}, f.CreateKey("test-pre-shared-cert2", false))
	preSharedCert3, _ := f.GetSslCertificate(f.Version, f.CreateKey("test-pre-shared-cert2", false))

	expectCerts := map[string]string{preSharedCert1.Name: preSharedCert1.Certificate,
		preSharedCert2.Name: preSharedCert2.Certificate, preSharedCert3.Name: preSharedCert3.Certificate}

	lbInfo.TLSName = preSharedCert1.Name + "," + preSharedCert2.Name + "," + preSharedCert3.Name
	// Sync multiple times to make sure ordering is preserved
	for i := 0; i < 10; i++ {
		if _, err := pool.Ensure(lbInfo); err != nil {
			t.Fatalf("pool.Ensure() = err %v", err)
		}
		verifyCertAndProxyLink(expectCerts, expectCerts, f, t)
		// Fetch the target proxy certs and go through in order
		verifyProxyCertsInOrder(" foo.com", f, t)
		pool.Delete(lbInfo.Name, false)
	}
}

// TestPreSharedToSecretBasedCertUpdate updates from pre-shared cert
// to secret based cert and verifies the pre-shared cert is retained.
func TestPreSharedToSecretBasedCertUpdate(t *testing.T) {
	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{utils.PathRule{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000}}})
	namer := utils.NewNamer(clusterName, "fw1")
	lbName := namer.LoadBalancer(ingressName)
	certName1 := namer.SSLCertName(lbName, GetCertHash("cert"))
	certName2 := namer.SSLCertName(lbName, GetCertHash("cert2"))

	lbInfo := &L7RuntimeInfo{
		Name:      lbName,
		AllowHTTP: false,
		UrlMap:    gceUrlMap,
		Ingress:   newIngress(),
	}

	f := NewFakeLoadBalancers(lbInfo.Name, namer, meta.VersionGA, meta.Global)
	pool := newFakeLoadBalancerPool(f, t, namer)

	// Prepare pre-shared certs.
	f.CreateSslCertificate(&composite.SslCertificate{
		Name:        "test-pre-shared-cert",
		Certificate: "abc",
		SelfLink:    "existing",
	}, f.CreateKey("test-pre-shared-cert", false))
	preSharedCert1, _ := f.GetSslCertificate(f.Version, f.CreateKey("test-pre-shared-cert", false))

	// Prepare pre-shared certs.
	f.CreateSslCertificate(&composite.SslCertificate{
		Name:        "test-pre-shared-cert2",
		Certificate: "xyz",
		SelfLink:    "existing",
	}, f.CreateKey("test-pre-shared-cert", false))
	preSharedCert2, _ := f.GetSslCertificate(f.Version, f.CreateKey("test-pre-shared-cert2", false))

	lbInfo.TLSName = preSharedCert1.Name + "," + preSharedCert2.Name

	// Sync pre-shared certs.
	if _, err := pool.Ensure(lbInfo); err != nil {
		t.Fatalf("pool.Ensure() = err %v", err)
	}
	expectCerts := map[string]string{preSharedCert1.Name: preSharedCert1.Certificate,
		preSharedCert2.Name: preSharedCert2.Certificate}
	verifyCertAndProxyLink(expectCerts, expectCerts, f, t)

	// Updates from pre-shared cert to secret based cert.
	lbInfo.TLS = []*TLSCerts{createCert("key", "cert", "name")}
	lbInfo.TLSName = ""
	if _, err := pool.Ensure(lbInfo); err != nil {
		t.Fatalf("pool.Ensure() = err %v", err)
	}
	expectCerts[certName1] = lbInfo.TLS[0].Cert
	// fakeLoadBalancer contains the preshared certs as well, but proxy will use only certName1
	expectCertsProxy := map[string]string{certName1: lbInfo.TLS[0].Cert}
	verifyCertAndProxyLink(expectCerts, expectCertsProxy, f, t)

	// Sync a different cert.
	lbInfo.TLS = []*TLSCerts{createCert("key2", "cert2", "name")}
	pool.Ensure(lbInfo)
	delete(expectCerts, certName1)
	expectCerts[certName2] = lbInfo.TLS[0].Cert
	expectCertsProxy = map[string]string{certName2: lbInfo.TLS[0].Cert}
	verifyCertAndProxyLink(expectCerts, expectCertsProxy, f, t)

	// Check if pre-shared certs are retained.
	if cert, err := f.GetSslCertificate(f.Version, f.CreateKey(preSharedCert1.Name, false)); err != nil || cert == nil {
		t.Fatalf("Want pre-shared certificate %v to exist, got none, err: %v", preSharedCert1.Name, err)
	}
	if cert, err := f.GetSslCertificate(f.Version, f.CreateKey(preSharedCert2.Name, false)); err != nil || cert == nil {
		t.Fatalf("Want pre-shared certificate %v to exist, got none, err: %v", preSharedCert2.Name, err)
	}
}

func verifyProxyCertsInOrder(hostname string, f *FakeLoadBalancers, t *testing.T) {
	t.Helper()
	t.Logf("f =\n%s", f.String())

	tps, err := f.GetTargetHttpsProxy(f.Version, f.CreateKey(f.TPName(true), false))
	if err != nil {
		t.Fatalf("expected https proxy to exist: %v, err: %v", f.TPName(true), err)
	}
	count := 0
	tmp := ""

	for _, link := range tps.SslCertificates {
		certName, _ := utils.KeyName(link)
		cert, err := f.GetSslCertificate(f.Version, f.CreateKey(certName, false))
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
	allCerts, err := f.ListSslCertificates(f.Version, f.CreateKey("", false))
	if err != nil {
		t.Fatalf("Failed to list certificates for %v - %v", f, err)
	}
	if len(expectCerts) != len(allCerts) {
		t.Fatalf("Unexpected number of certs in FakeLoadBalancer %v, expected %d, actual %d", f, len(expectCerts),
			len(allCerts))
	}
	for certName, certValue := range expectCerts {
		cert, err := f.GetSslCertificate(f.Version, f.CreateKey(certName, false))
		if err != nil {
			t.Fatalf("expected ssl certificate to exist: %v, err: %v, all certs: %v", certName, err, toCertNames(allCerts))
		}

		if cert.Certificate != certValue {
			t.Fatalf("unexpected certificate value for cert %s; expected %v, actual %v", certName, certValue, cert.Certificate)
		}
	}

	// httpsproxy needs to contain only the certs in expectCerts, nothing more, nothing less
	tps, err := f.GetTargetHttpsProxy(f.Version, f.CreateKey(f.TPName(true), false))
	if err != nil {
		t.Fatalf("expected https proxy to exist: %v, err: %v", f.TPName(true), err)
	}
	if len(tps.SslCertificates) != len(expectCertsProxy) {
		t.Fatalf("Expected https proxy to have %d certs, actual %d", len(expectCertsProxy), len(tps.SslCertificates))
	}
	for _, link := range tps.SslCertificates {
		certName, _ := utils.KeyName(link)
		if _, ok := expectCertsProxy[certName]; !ok {
			t.Fatalf("unexpected ssl certificate '%s' linked in target proxy; Expected : %v; Target Proxy Certs: %v",
				certName, expectCertsProxy, tps.SslCertificates)
		}
	}

	if tps.Description == "" {
		t.Errorf("tps.Description not set; expected it to be")
	}
}

func TestCreateHTTPSLoadBalancerAnnotationCert(t *testing.T) {
	// This should NOT create the forwarding rule and target proxy
	// associated with the HTTP branch of this loadbalancer.
	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{utils.PathRule{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000}}})
	tlsName := "external-cert-name"
	namer := utils.NewNamer(clusterName, "fw1")
	lbInfo := &L7RuntimeInfo{
		Name:      namer.LoadBalancer(ingressName),
		AllowHTTP: false,
		TLSName:   tlsName,
		UrlMap:    gceUrlMap,
		Ingress:   newIngress(),
	}

	f := NewFakeLoadBalancers(lbInfo.Name, namer, meta.VersionGA, meta.Global)
	f.CreateSslCertificate(&composite.SslCertificate{
		Name: tlsName,
	}, f.CreateKey(tlsName, false))
	pool := newFakeLoadBalancerPool(f, t, namer)
	if _, err := pool.Ensure(lbInfo); err != nil {
		t.Fatalf("pool.Ensure() = err %v", err)
	}
	l7, err := pool.Ensure(lbInfo)
	if err != nil || l7 == nil {
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
	namer := utils.NewNamer(clusterName, "fw1")
	lbInfo := &L7RuntimeInfo{
		Name:      namer.LoadBalancer(ingressName),
		AllowHTTP: true,
		TLS:       []*TLSCerts{{Key: "key", Cert: "cert"}},
		UrlMap:    gceUrlMap,
		Ingress:   newIngress(),
	}
	f := NewFakeLoadBalancers(lbInfo.Name, namer, meta.VersionGA, meta.Global)
	pool := newFakeLoadBalancerPool(f, t, namer)

	if _, err := pool.Ensure(lbInfo); err != nil {
		t.Fatalf("pool.Ensure() = err %v", err)
	}
	l7, err := pool.Ensure(lbInfo)
	if err != nil || l7 == nil {
		t.Fatalf("Expected l7 not created")
	}

	verifyHTTPSForwardingRuleAndProxyLinks(t, f)
	verifyHTTPForwardingRuleAndProxyLinks(t, f)

	// We know the forwarding rules exist, retrieve their addresses.
	fws, _ := f.GetForwardingRule(f.Version, f.CreateKey(f.FWName(true), false))
	fw, _ := f.GetForwardingRule(f.Version, f.CreateKey(f.FWName(false), false))
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
	um, err := f.GetUrlMap(f.Version, f.CreateKey(name, f.ResourceType == meta.Regional))
	if err != nil || um == nil {
		t.Errorf("f.GetUrlMap(%q) = %v, %v; want _, nil", name, um, err)
	}
	wantComputeURLMap := toCompositeURLMap(name, wantGCEURLMap, f.namer, f.CreateKey("", f.ResourceType == meta.Regional))
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

	namer := utils.NewNamer(clusterName, "fw1")
	lbInfo := &L7RuntimeInfo{Name: namer.LoadBalancer(ingressName), AllowHTTP: true, UrlMap: um1, Ingress: newIngress()}

	f := NewFakeLoadBalancers(lbInfo.Name, namer, meta.VersionGA, meta.Global)
	pool := newFakeLoadBalancerPool(f, t, namer)
	if _, err := pool.Ensure(lbInfo); err != nil {
		t.Fatalf("pool.Ensure() = err %v", err)
	}

	l7, err := pool.Ensure(lbInfo)
	if err != nil {
		t.Fatalf("%v", err)
	}
	verifyURLMap(t, f, l7.UrlMap().Name, um1)

	// Change url map.
	lbInfo.UrlMap = um2
	if _, err = pool.Ensure(lbInfo); err != nil {
		t.Fatalf("pool.Ensure() = err %v", err)
	}
	l7, err = pool.Ensure(lbInfo)
	if err != nil {
		t.Fatalf("%v", err)
	}
	verifyURLMap(t, f, l7.UrlMap().Name, um2)
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

	namer := utils.NewNamer(clusterName, "fw1")
	lbInfo := &L7RuntimeInfo{Name: namer.LoadBalancer(ingressName), AllowHTTP: true, UrlMap: um1, Ingress: newIngress()}
	f := NewFakeLoadBalancers(lbInfo.Name, namer, meta.VersionGA, meta.Global)
	pool := newFakeLoadBalancerPool(f, t, namer)
	if _, err := pool.Ensure(lbInfo); err != nil {
		t.Fatalf("pool.Ensure() = err %v", err)
	}

	lbInfo.UrlMap = um2
	if _, err := pool.Ensure(lbInfo); err != nil {
		t.Fatalf("pool.Ensure() = err %v", err)
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
	namer := utils.NewNamer(clusterName, "fw1")
	lbInfo := &L7RuntimeInfo{
		Name:      namer.LoadBalancer(ingressName),
		AllowHTTP: true,
		TLS:       []*TLSCerts{{Key: "key", Cert: "cert"}},
		UrlMap:    gceUrlMap,
		Ingress:   newIngress(),
	}
	f := NewFakeLoadBalancers(lbInfo.Name, namer, meta.VersionGA, meta.Global)
	pool := newFakeLoadBalancerPool(f, t, namer)
	if _, err := pool.Ensure(lbInfo); err != nil {
		t.Fatalf("pool.Ensure() = err %v", err)
	}
	l7, err := pool.Ensure(lbInfo)
	if err != nil || l7 == nil {
		t.Fatalf("Expected l7 not created")
	}
	verifyHTTPSForwardingRuleAndProxyLinks(t, f)
	verifyHTTPForwardingRuleAndProxyLinks(t, f)

	newName := "newName"
	namer = pool.(*L7s).Namer()
	namer.SetUID(newName)
	f.name = fmt.Sprintf("%v--%v", lbInfo.Name, newName)

	// Now the components should get renamed with the next suffix.
	l7, err = pool.Ensure(lbInfo)
	if err != nil || namer.ParseName(l7.Name).ClusterName != newName {
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

func syncPool(f *FakeLoadBalancers, t *testing.T, namer *utils.Namer, lbInfo *L7RuntimeInfo) {
	pool := newFakeLoadBalancerPool(f, t, namer)
	if _, err := pool.Ensure(lbInfo); err != nil {
		t.Fatalf("pool.Ensure() = err %v", err)
	}
	l7, err := pool.Ensure(lbInfo)
	if err != nil || l7 == nil {
		t.Fatalf("Expected l7 not created")
	}
}

func TestList(t *testing.T) {
	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{utils.PathRule{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000}}})
	namer := utils.NewNamer(clusterName, "fw1")
	lbInfo := &L7RuntimeInfo{
		Name:      namer.LoadBalancer(ingressName),
		AllowHTTP: true,
		TLS:       []*TLSCerts{{Key: "key", Cert: "cert"}},
		UrlMap:    gceUrlMap,
		Ingress:   newIngress(),
	}

	names := []string{
		"invalid-url-map-name1",
		"invalid-url-map-name2",
		"wrongprefix-um-test--uid1",
		"k8s-um-old-l7--uid1", // Expect List() to catch old URL maps
	}

	f := NewFakeLoadBalancers(lbInfo.Name, namer, meta.VersionGA, meta.Global)
	for _, name := range names {
		f.CreateUrlMap(&composite.UrlMap{Name: name}, f.CreateKey(name, false))
	}

	pool := newFakeLoadBalancerPool(f, t, namer)
	if _, err := pool.Ensure(lbInfo); err != nil {
		t.Fatalf("pool.Ensure() = err %v", err)
	}

	lbNames, _, err := pool.List()
	if err != nil {
		t.Fatalf("pool.List() = err %v", err)
	}

	expected := []string{"old-l7--uid1", "test--uid1"}

	if !reflect.DeepEqual(lbNames, expected) {
		t.Fatalf("pool.List() returned %+v, want %+v", lbNames, expected)
	}
}

// TestSecretBasedAndPreSharedCerts creates both pre-shared and
// secret-based certs and tests that all should be used.
func TestSecretBasedAndPreSharedCerts(t *testing.T) {
	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{utils.PathRule{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000}}})
	namer := utils.NewNamer(clusterName, "fw1")
	lbName := namer.LoadBalancer(ingressName)
	certName1 := namer.SSLCertName(lbName, GetCertHash("cert"))
	certName2 := namer.SSLCertName(lbName, GetCertHash("cert2"))

	lbInfo := &L7RuntimeInfo{
		Name:      lbName,
		AllowHTTP: false,
		UrlMap:    gceUrlMap,
		Ingress:   newIngress(),
	}

	f := NewFakeLoadBalancers(lbInfo.Name, namer, meta.VersionGA, meta.Global)
	pool := newFakeLoadBalancerPool(f, t, namer)

	// Prepare pre-shared certs.
	f.CreateSslCertificate(&composite.SslCertificate{
		Name:        "test-pre-shared-cert",
		Certificate: "abc",
		SelfLink:    "existing",
	}, f.CreateKey("test-pre-shared-cert", false))
	preSharedCert1, _ := f.GetSslCertificate(f.Version, f.CreateKey("test-pre-shared-cert", false))

	f.CreateSslCertificate(&composite.SslCertificate{
		Name:        "test-pre-shared-cert2",
		Certificate: "xyz",
		SelfLink:    "existing2",
	}, f.CreateKey("test-pre-shared-cert2", false))
	preSharedCert2, _ := f.GetSslCertificate(f.Version, f.CreateKey("test-pre-shared-cert2", false))
	lbInfo.TLSName = preSharedCert1.Name + "," + preSharedCert2.Name

	// Secret based certs.
	lbInfo.TLS = []*TLSCerts{
		createCert("key", "cert", "name"),
		createCert("key2", "cert2", "name"),
	}

	expectCerts := map[string]string{
		preSharedCert1.Name: preSharedCert1.Certificate,
		preSharedCert2.Name: preSharedCert2.Certificate,
		certName1:           lbInfo.TLS[0].Cert,
		certName2:           lbInfo.TLS[1].Cert,
	}

	// Verify that both secret-based and pre-shared certs are used.
	pool.Ensure(lbInfo)
	verifyCertAndProxyLink(expectCerts, expectCerts, f, t)
}

// TestMaxSecretBasedAndPreSharedCerts uploads 10 secret-based certs, which is the limit for the fake target proxy.
// Then creates 5 pre-shared certs, reaching the limit of 15 certs which is the fake certs quota.
// Ensures that creation of the 16th cert fails.
// Trying to use all 15 certs should fail.
// After removing 5 secret-based certs, all remaining certs should be used.
func TestMaxSecretBasedAndPreSharedCerts(t *testing.T) {
	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{utils.PathRule{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000}}})

	var tlsCerts []*TLSCerts
	expectCerts := make(map[string]string)
	expectCertsExtra := make(map[string]string)
	namer := utils.NewNamer(clusterName, "fw1")
	lbName := namer.LoadBalancer(ingressName)

	for ix := 0; ix < TargetProxyCertLimit; ix++ {
		str := strconv.Itoa(ix)
		tlsCerts = append(tlsCerts, createCert("key-"+str, "cert-"+str, "name-"+str))
		certName := namer.SSLCertName(lbName, GetCertHash("cert-"+str))
		expectCerts[certName] = "cert-" + str
		expectCertsExtra[certName] = "cert-" + str
	}
	lbInfo := &L7RuntimeInfo{
		Name:      lbName,
		AllowHTTP: false,
		TLS:       tlsCerts,
		UrlMap:    gceUrlMap,
		Ingress:   newIngress(),
	}
	f := NewFakeLoadBalancers(lbInfo.Name, namer, meta.VersionGA, meta.Global)
	pool := newFakeLoadBalancerPool(f, t, namer)

	if _, err := pool.Ensure(lbInfo); err != nil {
		t.Fatalf("pool.Ensure() = err %v", err)
	}
	verifyCertAndProxyLink(expectCerts, expectCerts, f, t)

	// Create pre-shared certs up to FakeCertQuota.
	preSharedCerts := []*composite.SslCertificate{}
	tlsNames := []string{}
	for ix := TargetProxyCertLimit; ix < FakeCertQuota; ix++ {
		str := strconv.Itoa(ix)
		err := f.CreateSslCertificate(&composite.SslCertificate{
			Name:        "test-pre-shared-cert-" + str,
			Certificate: "abc-" + str,
			SelfLink:    "existing-" + str,
		}, f.CreateKey("test-pre-shared-cert-"+str, false))
		if err != nil {
			t.Fatalf("f.CreateSslCertificate() = err %v", err)
		}
		cert, _ := f.GetSslCertificate(f.Version, f.CreateKey("test-pre-shared-cert-"+str, false))

		preSharedCerts = append(preSharedCerts, cert)
		tlsNames = append(tlsNames, cert.Name)
		expectCertsExtra[cert.Name] = cert.Certificate
	}
	lbInfo.TLSName = strings.Join(tlsNames, ",")

	err := f.CreateSslCertificate(&composite.SslCertificate{
		Name:        "test-pre-shared-cert-100",
		Certificate: "abc-100",
		SelfLink:    "existing-100",
	}, f.CreateKey("test-pre-shared-cert-100", false))
	if err == nil {
		t.Fatalf("Creating more than %d certs should have errored out", FakeCertQuota)
	}

	// Trying to use more than TargetProxyCertLimit certs should fail.
	// Verify that secret-based certs are still used,
	// and the loadbalancer also contains pre-shared certs.
	if _, err := pool.Ensure(lbInfo); err == nil {
		t.Fatalf("Trying to use more than %d certs should have errored out", TargetProxyCertLimit)
	}
	verifyCertAndProxyLink(expectCertsExtra, expectCerts, f, t)

	// Remove enough secret-based certs to make room for pre-shared certs.
	lbInfo.TLS = lbInfo.TLS[:TargetProxyCertLimit-len(preSharedCerts)]
	expectCerts = make(map[string]string)
	for _, cert := range lbInfo.TLS {
		expectCerts[namer.SSLCertName(lbName, cert.CertHash)] = cert.Cert
	}
	for _, cert := range preSharedCerts {
		expectCerts[cert.Name] = cert.Certificate
	}

	if _, err = pool.Ensure(lbInfo); err != nil {
		t.Fatalf("pool.Ensure() = err %v", err)
	}
	verifyCertAndProxyLink(expectCerts, expectCerts, f, t)
}

// TestSecretBasedToPreSharedCertUpdate updates from secret-based cert
// to pre-shared cert and verifies the secret-based cert is still used,
// until the secret is removed.
func TestSecretBasedToPreSharedCertUpdate(t *testing.T) {
	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{utils.PathRule{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000}}})
	namer := utils.NewNamer(clusterName, "fw1")
	lbName := namer.LoadBalancer(ingressName)
	certName1 := namer.SSLCertName(lbName, GetCertHash("cert"))

	lbInfo := &L7RuntimeInfo{
		Name:      lbName,
		AllowHTTP: false,
		UrlMap:    gceUrlMap,
		Ingress:   newIngress(),
	}

	f := NewFakeLoadBalancers(lbInfo.Name, namer, meta.VersionGA, meta.Global)
	pool := newFakeLoadBalancerPool(f, t, namer)

	// Sync secret based cert.
	lbInfo.TLS = []*TLSCerts{createCert("key", "cert", "name")}
	lbInfo.TLSName = ""
	if _, err := pool.Ensure(lbInfo); err != nil {
		t.Fatalf("pool.Ensure() = err %v", err)
	}
	expectCerts := map[string]string{certName1: lbInfo.TLS[0].Cert}
	verifyCertAndProxyLink(expectCerts, expectCerts, f, t)

	// Prepare pre-shared cert.
	f.CreateSslCertificate(&composite.SslCertificate{
		Name:        "test-pre-shared-cert",
		Certificate: "abc",
		SelfLink:    "existing",
	}, f.CreateKey("test-pre-shared-cert", false))
	preSharedCert1, _ := f.GetSslCertificate(f.Version, f.CreateKey("test-pre-shared-cert", false))

	lbInfo.TLSName = preSharedCert1.Name

	// Sync certs.
	if _, err := pool.Ensure(lbInfo); err != nil {
		t.Fatalf("pool.Ensure() = err %v", err)
	}
	expectCerts[preSharedCert1.Name] = preSharedCert1.Certificate
	verifyCertAndProxyLink(expectCerts, expectCerts, f, t)

	// Delete the secret.
	lbInfo.TLS = []*TLSCerts{}
	if _, err := pool.Ensure(lbInfo); err != nil {
		t.Fatalf("pool.Ensure() = err %v", err)
	}
	delete(expectCerts, certName1)
	verifyCertAndProxyLink(expectCerts, expectCerts, f, t)
}

// TestSecretBasedToPreSharedCertUpdateWithErrors tries to incorrectly update from secret-based cert
// to pre-shared cert, verifying that the secret-based cert is retained.
func TestSecretBasedToPreSharedCertUpdateWithErrors(t *testing.T) {
	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{utils.PathRule{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000}}})
	namer := utils.NewNamer(clusterName, "fw1")
	lbName := namer.LoadBalancer(ingressName)
	certName1 := namer.SSLCertName(lbName, GetCertHash("cert"))

	lbInfo := &L7RuntimeInfo{
		Name:      lbName,
		AllowHTTP: false,
		UrlMap:    gceUrlMap,
		Ingress:   newIngress(),
	}

	f := NewFakeLoadBalancers(lbInfo.Name, namer, meta.VersionGA, meta.Global)
	pool := newFakeLoadBalancerPool(f, t, namer)

	// Sync secret based cert.
	lbInfo.TLS = []*TLSCerts{createCert("key", "cert", "name")}
	lbInfo.TLSName = ""
	if _, err := pool.Ensure(lbInfo); err != nil {
		t.Fatalf("pool.Ensure() = err %v", err)
	}
	expectCerts := map[string]string{certName1: lbInfo.TLS[0].Cert}
	verifyCertAndProxyLink(expectCerts, expectCerts, f, t)

	// Prepare pre-shared certs.
	f.CreateSslCertificate(&composite.SslCertificate{
		Name:        "test-pre-shared-cert",
		Certificate: "abc",
		SelfLink:    "existing",
	}, f.CreateKey("test-pre-shared-cert", false))
	preSharedCert1, _ := f.GetSslCertificate(f.Version, f.CreateKey("test-pre-shared-cert", false))

	// Typo in the cert name.
	lbInfo.TLSName = preSharedCert1.Name + "typo"
	if _, err := pool.Ensure(lbInfo); err == nil {
		t.Fatalf("pool.Ensure() should have errored out because of the wrong cert name")
	}
	expectCertsProxy := map[string]string{certName1: lbInfo.TLS[0].Cert}
	expectCerts[preSharedCert1.Name] = preSharedCert1.Certificate
	// pre-shared cert is not used by the target proxy.
	verifyCertAndProxyLink(expectCerts, expectCertsProxy, f, t)

	// Fix the typo.
	lbInfo.TLSName = preSharedCert1.Name
	if _, err := pool.Ensure(lbInfo); err != nil {
		t.Fatalf("pool.Ensure() = err %v", err)
	}
	verifyCertAndProxyLink(expectCerts, expectCerts, f, t)
}
