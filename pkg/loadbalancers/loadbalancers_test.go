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
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/mock"
	"google.golang.org/api/googleapi"
	"k8s.io/kubernetes/pkg/cloudprovider/providers/gce"
	"k8s.io/kubernetes/pkg/util/slice"
	"net/http"
	"testing"

	"google.golang.org/api/compute/v1"
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

type testJig struct {
	pool    LoadBalancerPool
	fakeGCE *gce.Cloud
	mock    *cloud.MockGCE
	namer   *utils.Namer
	t       *testing.T
}

func newTestJig(t *testing.T) *testJig {
	namer := utils.NewNamer(clusterName, "fw1")
	fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
	mockGCE := fakeGCE.Compute().(*cloud.MockGCE)

	// Add any common hooks needed here, functions can override more specific hooks
	mockGCE.MockUrlMaps.UpdateHook = mock.UpdateURLMapHook
	mockGCE.MockTargetHttpProxies.SetUrlMapHook = mock.SetURLMapTargetHTTPProxyHook
	mockGCE.MockTargetHttpsProxies.SetUrlMapHook = mock.SetURLMapTargetHTTPSProxyHook
	mockGCE.MockTargetHttpsProxies.SetSslCertificatesHook = mock.SetSslCertificateTargetHTTPSProxyHook
	mockGCE.MockSslCertificates.InsertHook = func(ctx context.Context, key *meta.Key, obj *compute.SslCertificate, m *cloud.MockSslCertificates) (b bool, e error) {
		if len(m.Objects) >= FakeCertQuota {
			return true, fmt.Errorf("error exceeded fake cert quota")
		}
		return false, nil
	}
	mockGCE.MockTargetHttpsProxies.SetSslCertificatesHook = func(ctx context.Context, key *meta.Key, request *compute.TargetHttpsProxiesSetSslCertificatesRequest, proxies *cloud.MockTargetHttpsProxies) error {
		tp, err := proxies.Get(ctx, key)
		if err != nil {
			return &googleapi.Error{
				Code:    http.StatusNotFound,
				Message: fmt.Sprintf("Key: %s was not found in UrlMaps", key.String()),
			}
		}

		if len(request.SslCertificates) > TargetProxyCertLimit {
			return fmt.Errorf("error exceeded target proxy cert limit")
		}

		tp.SslCertificates = request.SslCertificates
		return nil
	}
	mockGCE.MockGlobalForwardingRules.InsertHook = InsertGlobalForwardingRuleHook

	return &testJig{
		pool:    newFakeLoadBalancerPool(fakeGCE, t, namer),
		fakeGCE: fakeGCE,
		mock:    mockGCE,
		namer:   namer,
		t:       t,
	}
}

func (j *testJig) UMName(lbName string) string {
	return j.namer.UrlMap(lbName)
}

func (j *testJig) TPName(lbName string, https bool) string {
	protocol := utils.HTTPProtocol
	if https {
		protocol = utils.HTTPSProtocol
	}
	return j.namer.TargetProxy(lbName, protocol)
}

func (j *testJig) FWName(lbName string, https bool) string {
	proto := utils.HTTPProtocol
	if https {
		proto = utils.HTTPSProtocol
	}
	return j.namer.ForwardingRule(lbName, proto)
}

// TODO: (shance) implement this once we switch to composites
func (j *testJig) String() string {
	return "testJig.String() Not implemented"
}

func newIngress() *extensions.Ingress {
	return &extensions.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ingressName,
			Namespace: namespace,
		},
	}
}

func newFakeLoadBalancerPool(cloud *gce.Cloud, t *testing.T, namer *utils.Namer) LoadBalancerPool {
	fakeIGs := instances.NewFakeInstanceGroups(sets.NewString(), namer)
	nodePool := instances.NewNodePool(fakeIGs, namer)
	nodePool.Init(&instances.FakeZoneLister{Zones: []string{defaultZone}})

	return NewLoadBalancerPool(cloud, namer, events.RecorderProducerMock{})
}

func TestCreateHTTPLoadBalancer(t *testing.T) {
	// This should NOT create the forwarding rule and target proxy
	// associated with the HTTPS branch of this loadbalancer.
	j := newTestJig(t)

	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{utils.PathRule{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000}}})
	lbInfo := &L7RuntimeInfo{
		Name:      j.namer.LoadBalancer(ingressName),
		AllowHTTP: true,
		UrlMap:    gceUrlMap,
		Ingress:   newIngress(),
	}

	l7, err := j.pool.Ensure(lbInfo)
	if err != nil || l7 == nil {
		t.Fatalf("Expected l7 not created, err: %v", err)
	}
	verifyHTTPForwardingRuleAndProxyLinks(t, j)
}

func TestCreateHTTPSLoadBalancer(t *testing.T) {
	// This should NOT create the forwarding rule and target proxy
	// associated with the HTTP branch of this loadbalancer.
	j := newTestJig(t)

	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{utils.PathRule{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000}}})
	lbInfo := &L7RuntimeInfo{
		Name:      j.namer.LoadBalancer(ingressName),
		AllowHTTP: false,
		TLS:       []*TLSCerts{createCert("key", "cert", "name")},
		UrlMap:    gceUrlMap,
		Ingress:   newIngress(),
	}

	l7, err := j.pool.Ensure(lbInfo)
	if err != nil || l7 == nil {
		t.Fatalf("Expected l7 not created")
	}
	verifyHTTPSForwardingRuleAndProxyLinks(t, j)
}

func verifyHTTPSForwardingRuleAndProxyLinks(t *testing.T, j *testJig) {
	t.Helper()
	lbName := j.namer.LoadBalancer(ingressName)

	um, err := j.fakeGCE.GetURLMap(j.UMName(lbName))
	tps, err := j.fakeGCE.GetTargetHTTPSProxy(j.TPName(lbName, true))
	if err != nil {
		t.Fatalf("j.fakeGCE.GetTargetHTTPSProxy(%q) = _, %v; want nil", j.TPName(lbName, true), err)
	}
	if !utils.EqualResourcePaths(tps.UrlMap, um.SelfLink) {
		t.Fatalf("tps.UrlMap = %q, want %q", tps.UrlMap, um.SelfLink)
	}
	fws, err := j.fakeGCE.GetGlobalForwardingRule(j.FWName(lbName, true))
	if err != nil {
		t.Fatalf("j.fakeGCE.GetGlobalForwardingRule(%q) = _, %v, want nil", j.FWName(lbName, true), err)
	}
	if !utils.EqualResourcePaths(fws.Target, tps.SelfLink) {
		t.Fatalf("fws.Target = %q, want %q", fws.Target, tps.SelfLink)
	}
	if fws.Description == "" {
		t.Errorf("fws.Description not set; expected it to be")
	}
}

func verifyHTTPForwardingRuleAndProxyLinks(t *testing.T, j *testJig) {
	t.Helper()
	lbName := j.namer.LoadBalancer(ingressName)

	um, err := j.fakeGCE.GetURLMap(j.UMName(lbName))
	tps, err := j.fakeGCE.GetTargetHTTPProxy(j.TPName(lbName, false))
	if err != nil {
		t.Fatalf("j.fakeGCE.GetTargetHTTPProxy(%q) = _, %v; want nil", j.TPName(lbName, false), err)
	}
	if !utils.EqualResourcePaths(tps.UrlMap, um.SelfLink) {
		t.Fatalf("tp.UrlMap = %q, want %q", tps.UrlMap, um.SelfLink)
	}
	fws, err := j.fakeGCE.GetGlobalForwardingRule(j.FWName(lbName, false))
	if err != nil {
		t.Fatalf("j.fakeGCE.GetGlobalForwardingRule(%q) = _, %v, want nil", j.FWName(lbName, false), err)
	}
	if !utils.EqualResourcePaths(fws.Target, tps.SelfLink) {
		t.Fatalf("fw.Target = %q, want %q", fws.Target, tps.SelfLink)
	}
	if fws.Description == "" {
		t.Errorf("fws.Description not set; expected it to be")
	}
}

// Tests that a certificate is created from the provided Key/Cert combo
// and the proxy is updated to another cert when the provided cert changes
func TestCertUpdate(t *testing.T) {
	j := newTestJig(t)

	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{utils.PathRule{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000}}})
	lbName := j.namer.LoadBalancer(ingressName)
	certName1 := j.namer.SSLCertName(lbName, GetCertHash("cert"))
	certName2 := j.namer.SSLCertName(lbName, GetCertHash("cert2"))

	lbInfo := &L7RuntimeInfo{
		Name:      lbName,
		AllowHTTP: false,
		TLS:       []*TLSCerts{createCert("key", "cert", "name")},
		UrlMap:    gceUrlMap,
		Ingress:   newIngress(),
	}

	// Sync first cert
	if _, err := j.pool.Ensure(lbInfo); err != nil {
		t.Fatalf("pool.Ensure() = err %v", err)
	}

	// Verify certs
	t.Logf("lbName=%q, name=%q", lbName, certName1)
	expectCerts := map[string]string{certName1: lbInfo.TLS[0].Cert}
	verifyCertAndProxyLink(expectCerts, expectCerts, j, t)

	// Sync with different cert
	lbInfo.TLS = []*TLSCerts{createCert("key2", "cert2", "name")}
	if _, err := j.pool.Ensure(lbInfo); err != nil {
		t.Fatalf("pool.Ensure() = err %v", err)
	}
	expectCerts = map[string]string{certName2: lbInfo.TLS[0].Cert}
	verifyCertAndProxyLink(expectCerts, expectCerts, j, t)
}

// Test that multiple secrets with the same certificate value don't cause a sync error.
func TestMultipleSecretsWithSameCert(t *testing.T) {
	j := newTestJig(t)

	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{utils.PathRule{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000}}})
	lbName := j.namer.LoadBalancer(ingressName)

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

	// Sync first cert
	if _, err := j.pool.Ensure(lbInfo); err != nil {
		t.Fatalf("pool.Ensure() = err %v", err)
	}
	certName := j.namer.SSLCertName(lbName, GetCertHash("cert"))
	expectCerts := map[string]string{certName: lbInfo.TLS[0].Cert}
	verifyCertAndProxyLink(expectCerts, expectCerts, j, t)
}

// Tests that controller can overwrite existing, unused certificates
func TestCertCreationWithCollision(t *testing.T) {
	j := newTestJig(t)

	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{utils.PathRule{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000}}})
	lbName := j.namer.LoadBalancer(ingressName)
	certName1 := j.namer.SSLCertName(lbName, GetCertHash("cert"))
	certName2 := j.namer.SSLCertName(lbName, GetCertHash("cert2"))

	lbInfo := &L7RuntimeInfo{
		Name:      lbName,
		AllowHTTP: false,
		TLS:       []*TLSCerts{createCert("key", "cert", "name")},
		UrlMap:    gceUrlMap,
		Ingress:   newIngress(),
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
	if _, err := j.pool.Ensure(lbInfo); err != nil {
		t.Fatalf("pool.Ensure() = err %v", err)
	}

	expectCerts := map[string]string{certName1: lbInfo.TLS[0].Cert}
	verifyCertAndProxyLink(expectCerts, expectCerts, j, t)

	// Create another cert where the name matches that of another cert, but contents are different - xyz != cert2.
	// Simulates a hash collision
	j.fakeGCE.CreateSslCertificate(&compute.SslCertificate{
		Name:        certName2,
		Certificate: "xyz",
		SelfLink:    "existing",
	})

	// Sync with different cert
	lbInfo.TLS = []*TLSCerts{createCert("key2", "cert2", "name")}
	if _, err := j.pool.Ensure(lbInfo); err != nil {
		t.Fatalf("pool.Ensure() = err %v", err)
	}
	expectCerts = map[string]string{certName2: "xyz"}
	// xyz instead of cert2 because the name collided and cert did not get updated.
	verifyCertAndProxyLink(expectCerts, expectCerts, j, t)
}

func TestMultipleCertRetentionAfterRestart(t *testing.T) {
	j := newTestJig(t)

	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{utils.PathRule{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000}}})
	cert1 := createCert("key", "cert", "name")
	cert2 := createCert("key2", "cert2", "name2")
	cert3 := createCert("key3", "cert3", "name3")
	lbName := j.namer.LoadBalancer(ingressName)
	certName1 := j.namer.SSLCertName(lbName, cert1.CertHash)
	certName2 := j.namer.SSLCertName(lbName, cert2.CertHash)
	certName3 := j.namer.SSLCertName(lbName, cert3.CertHash)

	expectCerts := map[string]string{}

	lbInfo := &L7RuntimeInfo{
		Name:      lbName,
		AllowHTTP: false,
		TLS:       []*TLSCerts{cert1},
		UrlMap:    gceUrlMap,
		Ingress:   newIngress(),
	}
	expectCerts[certName1] = cert1.Cert

	firstPool := j.pool

	firstPool.Ensure(lbInfo)
	verifyCertAndProxyLink(expectCerts, expectCerts, j, t)
	// Update config to use 2 certs
	lbInfo.TLS = []*TLSCerts{cert1, cert2}
	expectCerts[certName2] = cert2.Cert
	firstPool.Ensure(lbInfo)
	verifyCertAndProxyLink(expectCerts, expectCerts, j, t)

	// Restart of controller represented by a new pool
	secondPool := newFakeLoadBalancerPool(j.fakeGCE, t, j.namer)
	secondPool.Ensure(lbInfo)
	// Verify both certs are still present
	verifyCertAndProxyLink(expectCerts, expectCerts, j, t)

	// Replace the 2 certs with a different, single cert
	lbInfo.TLS = []*TLSCerts{cert3}
	expectCerts = map[string]string{certName3: cert3.Cert}
	secondPool.Ensure(lbInfo)
	// Only the new cert should be present
	verifyCertAndProxyLink(expectCerts, expectCerts, j, t)
}

//TestUpgradeToNewCertNames verifies that certs uploaded using the old naming convention
// are picked up and deleted when upgrading to the new scheme.
func TestUpgradeToNewCertNames(t *testing.T) {
	j := newTestJig(t)

	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{utils.PathRule{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000}}})
	lbName := j.namer.LoadBalancer(ingressName)
	lbInfo := &L7RuntimeInfo{
		Name:      lbName,
		AllowHTTP: false,
		UrlMap:    gceUrlMap,
		Ingress:   newIngress(),
	}
	oldCertName := "k8s-ssl-" + lbInfo.Name
	tlsCert := createCert("key", "cert", "name")
	lbInfo.TLS = []*TLSCerts{tlsCert}
	newCertName := j.namer.SSLCertName(lbName, tlsCert.CertHash)

	// Manually create a target proxy and assign a legacy cert to it.
	sslCert := &compute.SslCertificate{Name: oldCertName, Certificate: "cert"}
	j.fakeGCE.CreateSslCertificate(sslCert)
	tpName := j.TPName(lbName, true)
	newProxy := &compute.TargetHttpsProxy{
		Name:            tpName,
		Description:     "fake",
		SslCertificates: []string{sslCert.SelfLink},
	}
	err := j.fakeGCE.CreateTargetHTTPSProxy(newProxy)
	if err != nil {
		t.Fatalf("Failed to create Target proxy %v - %v", newProxy, err)
	}
	proxyCerts, err := j.fakeGCE.ListSslCertificates()
	if err != nil {
		t.Fatalf("Failed to list certs for load balancer %v - %v", j, err)
	}
	if len(proxyCerts) != 1 {
		t.Fatalf("Unexpected number of certs - Expected 1, got %d", len(proxyCerts))
	}

	if proxyCerts[0].Name != oldCertName {
		t.Fatalf("Expected cert with name %s, Got %s", oldCertName, proxyCerts[0].Name)
	}
	// Sync should replace this oldCert with one following the new naming scheme
	if _, err := j.pool.Ensure(lbInfo); err != nil {
		t.Fatalf("pool.Ensure() = err %v", err)
	}
	// We expect to see only the new cert linked to the proxy and available in the load balancer.
	expectCerts := map[string]string{newCertName: tlsCert.Cert}
	verifyCertAndProxyLink(expectCerts, expectCerts, j, t)
}

// Tests uploading 15 certs which is the limit for the fake loadbalancer. Ensures that creation of the 16th cert fails.
// Tests uploading 10 certs which is the target proxy limit. Uploading 11th cert should fail.
func TestMaxCertsUpload(t *testing.T) {
	j := newTestJig(t)

	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{utils.PathRule{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000}}})
	var tlsCerts []*TLSCerts
	expectCerts := make(map[string]string)
	expectCertsExtra := make(map[string]string)
	lbName := j.namer.LoadBalancer(ingressName)

	for ix := 0; ix < FakeCertQuota; ix++ {
		str := strconv.Itoa(ix)
		tlsCerts = append(tlsCerts, createCert("key-"+str, "cert-"+str, "name-"+str))
		certName := j.namer.SSLCertName(lbName, GetCertHash("cert-"+str))
		expectCerts[certName] = "cert-" + str
	}
	lbInfo := &L7RuntimeInfo{
		Name:      lbName,
		AllowHTTP: false,
		TLS:       tlsCerts,
		UrlMap:    gceUrlMap,
		Ingress:   newIngress(),
	}

	if _, err := j.pool.Ensure(lbInfo); err != nil {
		t.Fatalf("pool.Ensure() = err %v", err)
	}

	verifyCertAndProxyLink(expectCerts, expectCerts, j, t)
	failCert := createCert("key100", "cert100", "name100")
	lbInfo.TLS = append(lbInfo.TLS, failCert)
	_, err := j.pool.Ensure(lbInfo)
	if err == nil {
		t.Fatalf("Creating more than %d certs should have errored out", FakeCertQuota)
	}
	verifyCertAndProxyLink(expectCerts, expectCerts, j, t)
	// Set cert count less than cert creation limit but more than target proxy limit
	lbInfo.TLS = lbInfo.TLS[:TargetProxyCertLimit+1]
	for _, cert := range lbInfo.TLS {
		expectCertsExtra[j.namer.SSLCertName(lbName, cert.CertHash)] = cert.Cert
	}
	_, err = j.pool.Ensure(lbInfo)
	if err == nil {
		t.Fatalf("Assigning more than %d certs should have errored out", TargetProxyCertLimit)
	}
	// load balancer will contain the extra cert, but target proxy will not
	verifyCertAndProxyLink(expectCertsExtra, expectCerts, j, t)
	// Removing the extra cert from ingress spec should delete it
	lbInfo.TLS = lbInfo.TLS[:TargetProxyCertLimit]
	_, err = j.pool.Ensure(lbInfo)
	if err != nil {
		t.Fatalf("Unexpected error %s", err)
	}
	expectCerts = make(map[string]string)
	for _, cert := range lbInfo.TLS {
		expectCerts[j.namer.SSLCertName(lbName, cert.CertHash)] = cert.Cert
	}
	verifyCertAndProxyLink(expectCerts, expectCerts, j, t)
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
	lbName := j.namer.LoadBalancer(ingressName)
	contents := ""

	for ix := 0; ix < 3; ix++ {
		str := strconv.Itoa(ix)
		contents = "cert-" + str + " foo.com"
		tlsCerts = append(tlsCerts, createCert("key-"+str, contents, "name-"+str))
		certName := j.namer.SSLCertName(lbName, GetCertHash(contents))
		expectCerts[certName] = contents
	}
	lbInfo := &L7RuntimeInfo{
		Name:      lbName,
		AllowHTTP: false,
		TLS:       tlsCerts,
		UrlMap:    gceUrlMap,
		Ingress:   newIngress(),
	}
	// Sync multiple times to make sure ordering is preserved
	for i := 0; i < 10; i++ {
		if _, err := j.pool.Ensure(lbInfo); err != nil {
			t.Fatalf("pool.Ensure() = err %v", err)
		}
		verifyCertAndProxyLink(expectCerts, expectCerts, j, t)
		// Fetch the target proxy certs and go through in order
		verifyProxyCertsInOrder(" foo.com", j, t)
		j.pool.Delete(lbInfo.Name)
	}
}

func TestIdenticalHostnameCertsPreShared(t *testing.T) {
	j := newTestJig(t)

	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{utils.PathRule{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000}}})
	lbInfo := &L7RuntimeInfo{
		Name:      j.namer.LoadBalancer(ingressName),
		AllowHTTP: false,
		UrlMap:    gceUrlMap,
		Ingress:   newIngress(),
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
		if _, err := j.pool.Ensure(lbInfo); err != nil {
			t.Fatalf("pool.Ensure() = err %v", err)
		}
		verifyCertAndProxyLink(expectCerts, expectCerts, j, t)
		// Fetch the target proxy certs and go through in order
		verifyProxyCertsInOrder(" foo.com", j, t)
		j.pool.Delete(lbInfo.Name)
	}
}

// TestPreSharedToSecretBasedCertUpdate updates from pre-shared cert
// to secret based cert and verifies the pre-shared cert is retained.
func TestPreSharedToSecretBasedCertUpdate(t *testing.T) {
	j := newTestJig(t)

	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{utils.PathRule{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000}}})
	lbName := j.namer.LoadBalancer(ingressName)
	certName1 := j.namer.SSLCertName(lbName, GetCertHash("cert"))
	certName2 := j.namer.SSLCertName(lbName, GetCertHash("cert2"))

	lbInfo := &L7RuntimeInfo{
		Name:      lbName,
		AllowHTTP: false,
		UrlMap:    gceUrlMap,
		Ingress:   newIngress(),
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
	if _, err := j.pool.Ensure(lbInfo); err != nil {
		t.Fatalf("pool.Ensure() = err %v", err)
	}
	expectCerts := map[string]string{preSharedCert1.Name: preSharedCert1.Certificate,
		preSharedCert2.Name: preSharedCert2.Certificate}
	verifyCertAndProxyLink(expectCerts, expectCerts, j, t)

	// Updates from pre-shared cert to secret based cert.
	lbInfo.TLS = []*TLSCerts{createCert("key", "cert", "name")}
	lbInfo.TLSName = ""
	if _, err := j.pool.Ensure(lbInfo); err != nil {
		t.Fatalf("pool.Ensure() = err %v", err)
	}
	expectCerts[certName1] = lbInfo.TLS[0].Cert
	// fakeLoadBalancer contains the preshared certs as well, but proxy will use only certName1
	expectCertsProxy := map[string]string{certName1: lbInfo.TLS[0].Cert}
	verifyCertAndProxyLink(expectCerts, expectCertsProxy, j, t)

	// Sync a different cert.
	lbInfo.TLS = []*TLSCerts{createCert("key2", "cert2", "name")}
	j.pool.Ensure(lbInfo)
	delete(expectCerts, certName1)
	expectCerts[certName2] = lbInfo.TLS[0].Cert
	expectCertsProxy = map[string]string{certName2: lbInfo.TLS[0].Cert}
	verifyCertAndProxyLink(expectCerts, expectCertsProxy, j, t)

	// Check if pre-shared certs are retained.
	if cert, err := j.fakeGCE.GetSslCertificate(preSharedCert1.Name); err != nil || cert == nil {
		t.Fatalf("Want pre-shared certificate %v to exist, got none, err: %v", preSharedCert1.Name, err)
	}
	if cert, err := j.fakeGCE.GetSslCertificate(preSharedCert2.Name); err != nil || cert == nil {
		t.Fatalf("Want pre-shared certificate %v to exist, got none, err: %v", preSharedCert2.Name, err)
	}
}

func verifyProxyCertsInOrder(hostname string, j *testJig, t *testing.T) {
	t.Helper()
	t.Logf("f =\n%s", j.String())

	lbName := j.namer.LoadBalancer(ingressName)
	tps, err := j.fakeGCE.GetTargetHTTPSProxy(j.TPName(lbName, true))
	if err != nil {
		t.Fatalf("expected https proxy to exist: %v, err: %v", j.TPName(lbName, true), err)
	}
	count := 0
	tmp := ""

	for _, link := range tps.SslCertificates {
		certName, _ := utils.KeyName(link)
		cert, err := j.fakeGCE.GetSslCertificate(certName)
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
func verifyCertAndProxyLink(expectCerts map[string]string, expectCertsProxy map[string]string, j *testJig, t *testing.T) {
	t.Helper()
	t.Logf("f =\n%s", j.String())

	// f needs to contain only the certs in expectCerts, nothing more, nothing less
	allCerts, err := j.fakeGCE.ListSslCertificates()
	if err != nil {
		t.Fatalf("Failed to list certificates for %v - %v", j, err)
	}
	if len(expectCerts) != len(allCerts) {
		t.Fatalf("Unexpected number of certs in FakeLoadBalancer %v, expected %d, actual %d", j, len(expectCerts),
			len(allCerts))
	}
	for certName, certValue := range expectCerts {
		cert, err := j.fakeGCE.GetSslCertificate(certName)
		if err != nil {
			t.Fatalf("expected ssl certificate to exist: %v, err: %v, all certs: %v", certName, err, toCertNames(allCerts))
		}

		if cert.Certificate != certValue {
			t.Fatalf("unexpected certificate value for cert %s; expected %v, actual %v", certName, certValue, cert.Certificate)
		}
	}

	// httpsproxy needs to contain only the certs in expectCerts, nothing more, nothing less
	lbName := j.namer.LoadBalancer(ingressName)
	tps, err := j.fakeGCE.GetTargetHTTPSProxy(j.TPName(lbName, true))
	if err != nil {
		t.Fatalf("expected https proxy to exist: %v, err: %v", j.TPName(lbName, true), err)
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
	j := newTestJig(t)

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

	j.fakeGCE.CreateSslCertificate(&compute.SslCertificate{
		Name: tlsName,
	})
	if _, err := j.pool.Ensure(lbInfo); err != nil {
		t.Fatalf("pool.Ensure() = err %v", err)
	}
	l7, err := j.pool.Ensure(lbInfo)
	if err != nil || l7 == nil {
		t.Fatalf("Expected l7 not created")
	}
	verifyHTTPSForwardingRuleAndProxyLinks(t, j)
}

func TestCreateBothLoadBalancers(t *testing.T) {
	// This should create 2 forwarding rules and target proxies
	// but they should use the same urlmap, and have the same
	// static ip.
	j := newTestJig(t)

	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{utils.PathRule{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000}}})
	lbInfo := &L7RuntimeInfo{
		Name:      j.namer.LoadBalancer(ingressName),
		AllowHTTP: true,
		TLS:       []*TLSCerts{{Key: "key", Cert: "cert"}},
		UrlMap:    gceUrlMap,
		Ingress:   newIngress(),
	}

	if _, err := j.pool.Ensure(lbInfo); err != nil {
		t.Fatalf("pool.Ensure() = err %v", err)
	}
	l7, err := j.pool.Ensure(lbInfo)
	if err != nil || l7 == nil {
		t.Fatalf("Expected l7 not created")
	}

	verifyHTTPSForwardingRuleAndProxyLinks(t, j)
	verifyHTTPForwardingRuleAndProxyLinks(t, j)

	// We know the forwarding rules exist, retrieve their addresses.
	lbName := j.namer.LoadBalancer(ingressName)
	fws, _ := j.fakeGCE.GetGlobalForwardingRule(j.FWName(lbName, true))
	fw, _ := j.fakeGCE.GetGlobalForwardingRule(j.FWName(lbName, false))
	ip, err := j.fakeGCE.GetGlobalAddress(j.FWName(lbName, false))
	if err != nil {
		t.Fatalf("%v", err)
	}
	if ip.Address != fw.IPAddress || ip.Address != fws.IPAddress {
		t.Fatalf("ip.Address = %q, want %q and %q all equal", ip.Address, fw.IPAddress, fws.IPAddress)
	}
}

// verifyURLMap gets the created URLMap and compares it against an expected one.
func verifyURLMap(t *testing.T, j *testJig, name string, wantGCEURLMap *utils.GCEURLMap) {
	t.Helper()

	um, err := j.fakeGCE.GetURLMap(name)
	if err != nil || um == nil {
		t.Errorf("j.fakeGCE.GetUrlMap(%q) = %v, %v; want _, nil", name, um, err)
	}
	wantComputeURLMap := toComputeURLMap(name, wantGCEURLMap, j.namer)
	if !mapsEqual(wantComputeURLMap, um) {
		t.Errorf("mapsEqual() = false, got\n%+v\n  want\n%+v", um, wantComputeURLMap)
	}
}

func TestUrlMapChange(t *testing.T) {
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

	namer := utils.NewNamer(clusterName, "fw1")
	lbInfo := &L7RuntimeInfo{Name: namer.LoadBalancer(ingressName), AllowHTTP: true, UrlMap: um1, Ingress: newIngress()}

	if _, err := j.pool.Ensure(lbInfo); err != nil {
		t.Fatalf("pool.Ensure() = err %v", err)
	}

	l7, err := j.pool.Ensure(lbInfo)
	if err != nil {
		t.Fatalf("%v", err)
	}
	verifyURLMap(t, j, l7.UrlMap().Name, um1)

	// Change url map.
	lbInfo.UrlMap = um2
	if _, err = j.pool.Ensure(lbInfo); err != nil {
		t.Fatalf("pool.Ensure() = err %v", err)
	}
	l7, err = j.pool.Ensure(lbInfo)
	if err != nil {
		t.Fatalf("%v", err)
	}
	verifyURLMap(t, j, l7.UrlMap().Name, um2)
}

func TestPoolSyncNoChanges(t *testing.T) {
	j := newTestJig(t)

	// Add hook to keep track of how many calls are made.
	updateCalls := 0
	j.mock.MockUrlMaps.UpdateHook = func(ctx context.Context, key *meta.Key, obj *compute.UrlMap, m *cloud.MockUrlMaps) error {
		updateCalls += 1
		return nil
	}
	if updateCalls > 0 {
		t.Errorf("UpdateUrlMap() should not have been called")
	}

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
	if _, err := j.pool.Ensure(lbInfo); err != nil {
		t.Fatalf("pool.Ensure() = err %v", err)
	}

	lbInfo.UrlMap = um2
	if _, err := j.pool.Ensure(lbInfo); err != nil {
		t.Fatalf("pool.Ensure() = err %v", err)
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
	lbInfo := &L7RuntimeInfo{
		Name:      j.namer.LoadBalancer(ingressName),
		AllowHTTP: true,
		TLS:       []*TLSCerts{{Key: "key", Cert: "cert"}},
		UrlMap:    gceUrlMap,
		Ingress:   newIngress(),
	}
	if _, err := j.pool.Ensure(lbInfo); err != nil {
		t.Fatalf("j.pool.Ensure() = err %v", err)
	}
	l7, err := j.pool.Ensure(lbInfo)
	if err != nil || l7 == nil {
		t.Fatalf("Expected l7 not created")
	}
	verifyHTTPSForwardingRuleAndProxyLinks(t, j)
	verifyHTTPForwardingRuleAndProxyLinks(t, j)

	newName := "newName"
	j.namer.SetUID(newName)

	// Now the components should get renamed with the next suffix.
	lbInfo.Name = j.namer.LoadBalancer(ingressName)
	l7, err = j.pool.Ensure(lbInfo)
	if err != nil || j.namer.ParseName(l7.Name).ClusterName != newName {
		t.Fatalf("Expected L7 name to change.")
	}
	verifyHTTPSForwardingRuleAndProxyLinks(t, j)
	verifyHTTPForwardingRuleAndProxyLinks(t, j)
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

func syncPool(j *testJig, t *testing.T, lbInfo *L7RuntimeInfo) {
	if _, err := j.pool.Ensure(lbInfo); err != nil {
		t.Fatalf("j.pool.Ensure() = err %v", err)
	}
	l7, err := j.pool.Ensure(lbInfo)
	if err != nil || l7 == nil {
		t.Fatalf("Expected l7 not created")
	}
}

func TestList(t *testing.T) {
	j := newTestJig(t)

	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{utils.PathRule{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000}}})
	lbInfo := &L7RuntimeInfo{
		Name:      j.namer.LoadBalancer(ingressName),
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

	for _, name := range names {
		j.fakeGCE.CreateURLMap(&compute.UrlMap{Name: name})
	}

	if _, err := j.pool.Ensure(lbInfo); err != nil {
		t.Fatalf("j.pool.Ensure() = err %v", err)
	}

	lbNames, err := j.pool.List()
	if err != nil {
		t.Fatalf("j.pool.List() = err %v", err)
	}

	expected := []string{"old-l7--uid1", "test--uid1"}

	if len(lbNames) != 2 ||
		!slice.ContainsString(lbNames, expected[0], nil) ||
		!slice.ContainsString(lbNames, expected[1], nil) {
		t.Fatalf("j.pool.List() returned %+v, want %+v", lbNames, expected)
	}
}

// TestSecretBasedAndPreSharedCerts creates both pre-shared and
// secret-based certs and tests that all should be used.
func TestSecretBasedAndPreSharedCerts(t *testing.T) {
	j := newTestJig(t)

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

	// Prepare pre-shared certs.
	preSharedCert1, _ := j.fakeGCE.CreateSslCertificate(&compute.SslCertificate{
		Name:        "test-pre-shared-cert",
		Certificate: "abc",
		SelfLink:    "existing",
	})
	preSharedCert2, _ := j.fakeGCE.CreateSslCertificate(&compute.SslCertificate{
		Name:        "test-pre-shared-cert2",
		Certificate: "xyz",
		SelfLink:    "existing2",
	})
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
	j.pool.Ensure(lbInfo)
	verifyCertAndProxyLink(expectCerts, expectCerts, j, t)
}

// TestMaxSecretBasedAndPreSharedCerts uploads 10 secret-based certs, which is the limit for the fake target proxy.
// Then creates 5 pre-shared certs, reaching the limit of 15 certs which is the fake certs quota.
// Ensures that creation of the 16th cert fails.
// Trying to use all 15 certs should fail.
// After removing 5 secret-based certs, all remaining certs should be used.
func TestMaxSecretBasedAndPreSharedCerts(t *testing.T) {
	j := newTestJig(t)

	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{utils.PathRule{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000}}})

	var tlsCerts []*TLSCerts
	expectCerts := make(map[string]string)
	expectCertsExtra := make(map[string]string)
	lbName := j.namer.LoadBalancer(ingressName)

	for ix := 0; ix < TargetProxyCertLimit; ix++ {
		str := strconv.Itoa(ix)
		tlsCerts = append(tlsCerts, createCert("key-"+str, "cert-"+str, "name-"+str))
		certName := j.namer.SSLCertName(lbName, GetCertHash("cert-"+str))
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

	if _, err := j.pool.Ensure(lbInfo); err != nil {
		t.Fatalf("j.pool.Ensure() = err %v", err)
	}
	verifyCertAndProxyLink(expectCerts, expectCerts, j, t)

	// Create pre-shared certs up to FakeCertQuota.
	preSharedCerts := []*compute.SslCertificate{}
	tlsNames := []string{}
	for ix := TargetProxyCertLimit; ix < FakeCertQuota; ix++ {
		str := strconv.Itoa(ix)
		cert, err := j.fakeGCE.CreateSslCertificate(&compute.SslCertificate{
			Name:        "test-pre-shared-cert-" + str,
			Certificate: "abc-" + str,
			SelfLink:    "existing-" + str,
		})
		if err != nil {
			t.Fatalf("j.fakeGCE.CreateSslCertificate() = err %v", err)
		}
		preSharedCerts = append(preSharedCerts, cert)
		tlsNames = append(tlsNames, cert.Name)
		expectCertsExtra[cert.Name] = cert.Certificate
	}
	lbInfo.TLSName = strings.Join(tlsNames, ",")

	_, err := j.fakeGCE.CreateSslCertificate(&compute.SslCertificate{
		Name:        "test-pre-shared-cert-100",
		Certificate: "abc-100",
		SelfLink:    "existing-100",
	})
	if err == nil {
		t.Fatalf("Creating more than %d certs should have errored out", FakeCertQuota)
	}

	// Trying to use more than TargetProxyCertLimit certs should fail.
	// Verify that secret-based certs are still used,
	// and the loadbalancer also contains pre-shared certs.
	if _, err := j.pool.Ensure(lbInfo); err == nil {
		t.Fatalf("Trying to use more than %d certs should have errored out", TargetProxyCertLimit)
	}
	verifyCertAndProxyLink(expectCertsExtra, expectCerts, j, t)

	// Remove enough secret-based certs to make room for pre-shared certs.
	lbInfo.TLS = lbInfo.TLS[:TargetProxyCertLimit-len(preSharedCerts)]
	expectCerts = make(map[string]string)
	for _, cert := range lbInfo.TLS {
		expectCerts[j.namer.SSLCertName(lbName, cert.CertHash)] = cert.Cert
	}
	for _, cert := range preSharedCerts {
		expectCerts[cert.Name] = cert.Certificate
	}

	if _, err = j.pool.Ensure(lbInfo); err != nil {
		t.Fatalf("j.pool.Ensure() = err %v", err)
	}
	verifyCertAndProxyLink(expectCerts, expectCerts, j, t)
}

// TestSecretBasedToPreSharedCertUpdate updates from secret-based cert
// to pre-shared cert and verifies the secret-based cert is still used,
// until the secret is removed.
func TestSecretBasedToPreSharedCertUpdate(t *testing.T) {
	j := newTestJig(t)

	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{utils.PathRule{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000}}})
	lbName := j.namer.LoadBalancer(ingressName)
	certName1 := j.namer.SSLCertName(lbName, GetCertHash("cert"))

	lbInfo := &L7RuntimeInfo{
		Name:      lbName,
		AllowHTTP: false,
		UrlMap:    gceUrlMap,
		Ingress:   newIngress(),
	}

	// Sync secret based cert.
	lbInfo.TLS = []*TLSCerts{createCert("key", "cert", "name")}
	lbInfo.TLSName = ""
	if _, err := j.pool.Ensure(lbInfo); err != nil {
		t.Fatalf("pool.Ensure() = err %v", err)
	}
	expectCerts := map[string]string{certName1: lbInfo.TLS[0].Cert}
	verifyCertAndProxyLink(expectCerts, expectCerts, j, t)

	// Prepare pre-shared cert.
	preSharedCert1, _ := j.fakeGCE.CreateSslCertificate(&compute.SslCertificate{
		Name:        "test-pre-shared-cert",
		Certificate: "abc",
		SelfLink:    "existing",
	})
	lbInfo.TLSName = preSharedCert1.Name

	// Sync certs.
	if _, err := j.pool.Ensure(lbInfo); err != nil {
		t.Fatalf("j.pool.Ensure() = err %v", err)
	}
	expectCerts[preSharedCert1.Name] = preSharedCert1.Certificate
	verifyCertAndProxyLink(expectCerts, expectCerts, j, t)

	// Delete the secret.
	lbInfo.TLS = []*TLSCerts{}
	if _, err := j.pool.Ensure(lbInfo); err != nil {
		t.Fatalf("pool.Ensure() = err %v", err)
	}
	delete(expectCerts, certName1)
	verifyCertAndProxyLink(expectCerts, expectCerts, j, t)
}

// TestSecretBasedToPreSharedCertUpdateWithErrors tries to incorrectly update from secret-based cert
// to pre-shared cert, verifying that the secret-based cert is retained.
func TestSecretBasedToPreSharedCertUpdateWithErrors(t *testing.T) {
	j := newTestJig(t)

	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{utils.PathRule{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000}}})
	lbName := j.namer.LoadBalancer(ingressName)
	certName1 := j.namer.SSLCertName(lbName, GetCertHash("cert"))

	lbInfo := &L7RuntimeInfo{
		Name:      lbName,
		AllowHTTP: false,
		UrlMap:    gceUrlMap,
		Ingress:   newIngress(),
	}

	// Sync secret based cert.
	lbInfo.TLS = []*TLSCerts{createCert("key", "cert", "name")}
	lbInfo.TLSName = ""
	if _, err := j.pool.Ensure(lbInfo); err != nil {
		t.Fatalf("pool.Ensure() = err %v", err)
	}
	expectCerts := map[string]string{certName1: lbInfo.TLS[0].Cert}
	verifyCertAndProxyLink(expectCerts, expectCerts, j, t)

	// Prepare pre-shared certs.
	preSharedCert1, _ := j.fakeGCE.CreateSslCertificate(&compute.SslCertificate{
		Name:        "test-pre-shared-cert",
		Certificate: "abc",
		SelfLink:    "existing",
	})

	// Typo in the cert name.
	lbInfo.TLSName = preSharedCert1.Name + "typo"
	if _, err := j.pool.Ensure(lbInfo); err == nil {
		t.Fatalf("pool.Ensure() should have errored out because of the wrong cert name")
	}
	expectCertsProxy := map[string]string{certName1: lbInfo.TLS[0].Cert}
	expectCerts[preSharedCert1.Name] = preSharedCert1.Certificate
	// pre-shared cert is not used by the target proxy.
	verifyCertAndProxyLink(expectCerts, expectCertsProxy, j, t)

	// Fix the typo.
	lbInfo.TLSName = preSharedCert1.Name
	if _, err := j.pool.Ensure(lbInfo); err != nil {
		t.Fatalf("pool.Ensure() = err %v", err)
	}
	verifyCertAndProxyLink(expectCerts, expectCerts, j, t)
}
