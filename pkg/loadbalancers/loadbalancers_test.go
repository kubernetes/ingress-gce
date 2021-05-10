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
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/mock"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/googleapi"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/ingress-gce/pkg/annotations"
	frontendconfigv1beta1 "k8s.io/ingress-gce/pkg/apis/frontendconfig/v1beta1"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/events"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/instances"
	"k8s.io/ingress-gce/pkg/loadbalancers/features"
	"k8s.io/ingress-gce/pkg/test"
	"k8s.io/ingress-gce/pkg/translator"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/common"
	namer_util "k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/kubernetes/pkg/util/slice"
	"k8s.io/legacy-cloud-providers/gce"
)

const (
	clusterName    = "uid1"
	ingressName    = "test"
	namespace      = "namespace1"
	defaultZone    = "zone-a"
	defaultVersion = meta.VersionGA
	defaultScope   = meta.Global
)

type testJig struct {
	pool    L7s
	fakeGCE *gce.Cloud
	mock    *cloud.MockGCE
	namer   *namer_util.Namer
	ing     *networkingv1.Ingress
	feNamer namer_util.IngressFrontendNamer
	t       *testing.T
}

func newTestJig(t *testing.T) *testJig {
	namer := namer_util.NewNamer(clusterName, "fw1")
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
				Message: fmt.Sprintf("Key: %s was not found in TargetHttpsProxies", key.String()),
			}
		}

		if len(request.SslCertificates) > TargetProxyCertLimit {
			return fmt.Errorf("error exceeded target proxy cert limit")
		}

		// Check that cert exists
		for _, certName := range request.SslCertificates {
			resID, err := cloud.ParseResourceURL(certName)
			if err != nil {
				return err
			}
			_, err = composite.GetSslCertificate(fakeGCE, resID.Key, defaultVersion)
			if err != nil {
				return err
			}
		}

		tp.SslCertificates = request.SslCertificates
		return nil
	}
	mockGCE.MockTargetHttpsProxies.SetSslPolicyHook = func(ctx context.Context, key *meta.Key, ref *compute.SslPolicyReference, proxies *cloud.MockTargetHttpsProxies) error {
		tps, err := proxies.Get(ctx, key)
		fmt.Printf("tps = %+v, err = %v\n\n", tps, err)
		if err != nil {
			return &googleapi.Error{
				Code:    http.StatusNotFound,
				Message: fmt.Sprintf("Key: %s was not found in TargetHttpsProxies", key.String()),
			}
		}
		tps.SslPolicy = ref.SslPolicy
		return nil
	}
	mockGCE.MockGlobalForwardingRules.InsertHook = InsertGlobalForwardingRuleHook

	ing := newIngress()
	feNamer := namer_util.NewFrontendNamerFactory(namer, "").Namer(ing)

	return &testJig{
		pool:    newFakeLoadBalancerPool(fakeGCE, t, namer),
		fakeGCE: fakeGCE,
		mock:    mockGCE,
		namer:   namer,
		ing:     ing,
		feNamer: feNamer,
		t:       t,
	}
}

// TODO: (shance) implement this once we switch to composites
func (j *testJig) String() string {
	return "testJig.String() Not implemented"
}

func newIngress() *networkingv1.Ingress {
	return &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ingressName,
			Namespace: namespace,
		},
	}
}

func newFakeLoadBalancerPool(cloud *gce.Cloud, t *testing.T, namer *namer_util.Namer) L7s {
	fakeIGs := instances.NewFakeInstanceGroups(sets.NewString(), namer)
	nodePool := instances.NewNodePool(fakeIGs, namer, &test.FakeRecorderSource{}, utils.GetBasePath(cloud))
	nodePool.Init(&instances.FakeZoneLister{Zones: []string{defaultZone}})

	return L7s{cloud, namer, events.RecorderProducerMock{}, namer_util.NewFrontendNamerFactory(namer, "")}
}

func newILBIngress() *networkingv1.Ingress {
	return &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:        ingressName,
			Namespace:   namespace,
			Annotations: map[string]string{annotations.IngressClassKey: annotations.GceL7ILBIngressClass},
		},
	}
}

func TestCreateHTTPLoadBalancer(t *testing.T) {
	// This should NOT create the forwarding rule and target proxy
	// associated with the HTTPS branch of this loadbalancer.
	j := newTestJig(t)

	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234, BackendNamer: j.namer}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000, BackendNamer: j.namer}}})
	lbInfo := &L7RuntimeInfo{
		AllowHTTP: true,
		UrlMap:    gceUrlMap,
		Ingress:   newIngress(),
	}

	l7, err := j.pool.Ensure(lbInfo)
	if err != nil || l7 == nil {
		t.Fatalf("Expected l7 not created, err: %v", err)
	}
	verifyHTTPForwardingRuleAndProxyLinks(t, j, l7, "")
}

func TestCreateHTTPILBLoadBalancer(t *testing.T) {
	// This should NOT create the forwarding rule and target proxy
	// associated with the HTTPS branch of this loadbalancer.
	j := newTestJig(t)

	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234, BackendNamer: j.namer}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000, BackendNamer: j.namer}}})
	lbInfo := &L7RuntimeInfo{
		AllowHTTP: true,
		UrlMap:    gceUrlMap,
		Ingress:   newILBIngress(),
	}

	l7, err := j.pool.Ensure(lbInfo)
	if err != nil || l7 == nil {
		t.Fatalf("Expected l7 not created, err: %v", err)
	}
	verifyHTTPForwardingRuleAndProxyLinks(t, j, l7, "")
}

func TestCreateHTTPILBLoadBalancerStaticIp(t *testing.T) {
	// This should NOT create the forwarding rule and target proxy
	// associated with the HTTPS branch of this loadbalancer.
	j := newTestJig(t)

	ipName := "test-ilb-static-ip"
	ip := "10.1.2.3"
	key, err := composite.CreateKey(j.fakeGCE, ipName, features.L7ILBScope())
	if err != nil {
		t.Fatal(err)
	}
	err = composite.CreateAddress(j.fakeGCE, key, &composite.Address{Name: ipName, Version: meta.VersionGA, Address: ip})
	if err != nil {
		t.Fatal(err)
	}

	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234, BackendNamer: j.namer}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000, BackendNamer: j.namer}}})
	lbInfo := &L7RuntimeInfo{
		AllowHTTP:    true,
		UrlMap:       gceUrlMap,
		Ingress:      newILBIngress(),
		StaticIPName: ipName,
	}

	l7, err := j.pool.Ensure(lbInfo)
	if err != nil || l7 == nil {
		t.Fatalf("Expected l7 not created, err: %v", err)
	}

	verifyHTTPForwardingRuleAndProxyLinks(t, j, l7, ip)
}

func TestCreateHTTPSILBLoadBalancer(t *testing.T) {
	// This should NOT create the forwarding rule and target proxy
	// associated with the HTTP branch of this loadbalancer.
	j := newTestJig(t)

	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234, BackendNamer: j.namer}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000, BackendNamer: j.namer}}})
	lbInfo := &L7RuntimeInfo{
		AllowHTTP: false,
		TLS:       []*translator.TLSCerts{createCert("key", "cert", "name")},
		UrlMap:    gceUrlMap,
		Ingress:   newILBIngress(),
	}

	l7, err := j.pool.Ensure(lbInfo)
	if err != nil || l7 == nil {
		t.Fatalf("Expected l7 not created")
	}
	verifyHTTPSForwardingRuleAndProxyLinks(t, j, l7)
}

// Test case with HTTPS ILB Load balancer and AllowHttp set to true (not currently supported)
// Ensure should throw an error
func TestCreateHTTPSILBLoadBalancerAllowHTTP(t *testing.T) {
	j := newTestJig(t)

	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234, BackendNamer: j.namer}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000, BackendNamer: j.namer}}})
	lbInfo := &L7RuntimeInfo{
		AllowHTTP: true,
		TLS:       []*translator.TLSCerts{createCert("key", "cert", "name")},
		UrlMap:    gceUrlMap,
		Ingress:   newILBIngress(),
	}

	if _, err := j.pool.Ensure(lbInfo); err == nil {
		t.Fatalf("j.pool.Ensure(%v) = nil, want err", lbInfo)
	}
}

func TestCreateHTTPSLoadBalancer(t *testing.T) {
	// This should NOT create the forwarding rule and target proxy
	// associated with the HTTP branch of this loadbalancer.
	j := newTestJig(t)

	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234, BackendNamer: j.namer}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000, BackendNamer: j.namer}}})
	lbInfo := &L7RuntimeInfo{
		AllowHTTP: false,
		TLS:       []*translator.TLSCerts{createCert("key", "cert", "name")},
		UrlMap:    gceUrlMap,
		Ingress:   newIngress(),
	}

	l7, err := j.pool.Ensure(lbInfo)
	if err != nil || l7 == nil {
		t.Fatalf("Expected l7 not created")
	}
	verifyHTTPSForwardingRuleAndProxyLinks(t, j, l7)
}

func verifyHTTPSForwardingRuleAndProxyLinks(t *testing.T, j *testJig, l7 *L7) {
	t.Helper()
	versions := l7.Versions()

	key, err := composite.CreateKey(j.fakeGCE, l7.namer.UrlMap(), l7.scope)
	if err != nil {
		t.Fatal(err)
	}
	um, err := composite.GetUrlMap(j.fakeGCE, key, versions.UrlMap)

	tpsName := l7.namer.TargetProxy(namer_util.HTTPSProtocol)
	key.Name = tpsName
	tps, err := composite.GetTargetHttpsProxy(j.fakeGCE, key, versions.TargetHttpsProxy)
	if err != nil {
		t.Fatalf("j.fakeGCE.GetTargetHTTPSProxy(%q) = _, %v; want nil", tpsName, err)
	}
	if !utils.EqualResourcePaths(tps.UrlMap, um.SelfLink) {
		t.Fatalf("tps.UrlMap = %q, want %q", tps.UrlMap, um.SelfLink)
	}

	fwsName := l7.namer.ForwardingRule(namer_util.HTTPSProtocol)
	key.Name = fwsName
	fws, err := composite.GetForwardingRule(j.fakeGCE, key, versions.ForwardingRule)
	if err != nil {
		t.Fatalf("j.fakeGCE.GetGlobalForwardingRule(%q) = _, %v, want nil", fwsName, err)
	}
	if !utils.EqualResourcePaths(fws.Target, tps.SelfLink) {
		t.Fatalf("fws.Target = %q, want %q", fws.Target, tps.SelfLink)
	}
	if fws.Description == "" {
		t.Errorf("fws.Description not set; expected it to be")
	}
}

func verifyHTTPForwardingRuleAndProxyLinks(t *testing.T, j *testJig, l7 *L7, ip string) {
	t.Helper()
	versions := l7.Versions()

	key, err := composite.CreateKey(j.fakeGCE, l7.namer.UrlMap(), l7.scope)
	if err != nil {
		t.Fatal(err)
	}
	um, err := composite.GetUrlMap(j.fakeGCE, key, versions.UrlMap)
	tpName := l7.namer.TargetProxy(namer_util.HTTPProtocol)
	key.Name = tpName
	tps, err := composite.GetTargetHttpProxy(j.fakeGCE, key, versions.TargetHttpProxy)
	if err != nil {
		t.Fatalf("j.fakeGCE.GetTargetHTTPProxy(%q) = _, %v; want nil", tpName, err)
	}
	if !utils.EqualResourcePaths(tps.UrlMap, um.SelfLink) {
		t.Fatalf("tp.UrlMap = %q, want %q", tps.UrlMap, um.SelfLink)
	}
	fwName := l7.namer.ForwardingRule(namer_util.HTTPProtocol)
	key.Name = fwName
	fws, err := composite.GetForwardingRule(j.fakeGCE, key, versions.ForwardingRule)
	if err != nil {
		t.Fatalf("j.fakeGCE.GetGlobalForwardingRule(%q) = _, %v, want nil", fwName, err)
	}
	if !utils.EqualResourcePaths(fws.Target, tps.SelfLink) {
		t.Fatalf("fw.Target = %q, want %q", fws.Target, tps.SelfLink)
	}
	if fws.Description == "" {
		t.Errorf("fws.Description not set; expected it to be")
	}
	if ip != "" {
		if fws.IPAddress != ip {
			t.Fatalf("fws.IPAddress = %q, want %q", fws.IPAddress, ip)
		}
	}
}

// Tests that a certificate is created from the provided Key/Cert combo
// and the proxy is updated to another cert when the provided cert changes
func TestCertUpdate(t *testing.T) {
	j := newTestJig(t)

	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234, BackendNamer: j.namer}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000, BackendNamer: j.namer}}})
	ing := newIngress()
	feNamer := namer_util.NewFrontendNamerFactory(j.namer, "").Namer(ing)
	certName1 := feNamer.SSLCertName(translator.GetCertHash("cert"))
	certName2 := feNamer.SSLCertName(translator.GetCertHash("cert2"))

	lbInfo := &L7RuntimeInfo{
		AllowHTTP: false,
		TLS:       []*translator.TLSCerts{createCert("key", "cert", "name")},
		UrlMap:    gceUrlMap,
		Ingress:   ing,
	}

	// Sync first cert
	if _, err := j.pool.Ensure(lbInfo); err != nil {
		t.Fatalf("pool.Ensure() = err %v", err)
	}

	// Verify certs
	t.Logf("lbName=%q, name=%q", feNamer.LoadBalancer(), certName1)
	expectCerts := map[string]string{certName1: lbInfo.TLS[0].Cert}
	verifyCertAndProxyLink(expectCerts, expectCerts, j, t)

	// Sync with different cert
	lbInfo.TLS = []*translator.TLSCerts{createCert("key2", "cert2", "name")}
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
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234, BackendNamer: j.namer}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000, BackendNamer: j.namer}}})
	ing := newIngress()
	feNamer := namer_util.NewFrontendNamerFactory(j.namer, "").Namer(ing)

	lbInfo := &L7RuntimeInfo{
		AllowHTTP: false,
		TLS: []*translator.TLSCerts{
			createCert("key", "cert", "secret-a"),
			createCert("key", "cert", "secret-b"),
		},
		UrlMap:  gceUrlMap,
		Ingress: ing,
	}

	// Sync first cert
	if _, err := j.pool.Ensure(lbInfo); err != nil {
		t.Fatalf("pool.Ensure() = err %v", err)
	}
	certName := feNamer.SSLCertName(translator.GetCertHash("cert"))
	expectCerts := map[string]string{certName: lbInfo.TLS[0].Cert}
	verifyCertAndProxyLink(expectCerts, expectCerts, j, t)
}

// Tests that controller can overwrite existing, unused certificates
func TestCertCreationWithCollision(t *testing.T) {
	j := newTestJig(t)

	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234, BackendNamer: j.namer}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000, BackendNamer: j.namer}}})
	ing := newIngress()
	feNamer := namer_util.NewFrontendNamerFactory(j.namer, "").Namer(ing)
	certName1 := feNamer.SSLCertName(translator.GetCertHash("cert"))
	certName2 := feNamer.SSLCertName(translator.GetCertHash("cert2"))

	lbInfo := &L7RuntimeInfo{
		AllowHTTP: false,
		TLS:       []*translator.TLSCerts{createCert("key", "cert", "name")},
		UrlMap:    gceUrlMap,
		Ingress:   ing,
	}

	// Have the same name used by orphaned cert
	// Since name of the cert is the same, the contents of Certificate have to be the same too, since name contains a
	// hash of the contents.
	key, err := composite.CreateKey(j.fakeGCE, certName1, defaultScope)
	if err != nil {
		t.Fatal(err)
	}
	composite.CreateSslCertificate(j.fakeGCE, key, &composite.SslCertificate{
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
	key.Name = certName2
	composite.CreateSslCertificate(j.fakeGCE, key, &composite.SslCertificate{
		Name:        certName2,
		Certificate: "xyz",
		SelfLink:    "existing",
	})

	// Sync with different cert
	lbInfo.TLS = []*translator.TLSCerts{createCert("key2", "cert2", "name")}
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
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234, BackendNamer: j.namer}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000, BackendNamer: j.namer}}})
	cert1 := createCert("key", "cert", "name")
	cert2 := createCert("key2", "cert2", "name2")
	cert3 := createCert("key3", "cert3", "name3")
	ing := newIngress()
	feNamer := namer_util.NewFrontendNamerFactory(j.namer, "").Namer(ing)
	certName1 := feNamer.SSLCertName(cert1.CertHash)
	certName2 := feNamer.SSLCertName(cert2.CertHash)
	certName3 := feNamer.SSLCertName(cert3.CertHash)

	expectCerts := map[string]string{}

	lbInfo := &L7RuntimeInfo{
		AllowHTTP: false,
		TLS:       []*translator.TLSCerts{cert1},
		UrlMap:    gceUrlMap,
		Ingress:   ing,
	}
	expectCerts[certName1] = cert1.Cert

	firstPool := j.pool

	firstPool.Ensure(lbInfo)
	verifyCertAndProxyLink(expectCerts, expectCerts, j, t)
	// Update config to use 2 certs
	lbInfo.TLS = []*translator.TLSCerts{cert1, cert2}
	expectCerts[certName2] = cert2.Cert
	firstPool.Ensure(lbInfo)
	verifyCertAndProxyLink(expectCerts, expectCerts, j, t)

	// Restart of controller represented by a new pool
	secondPool := newFakeLoadBalancerPool(j.fakeGCE, t, j.namer)
	secondPool.Ensure(lbInfo)
	// Verify both certs are still present
	verifyCertAndProxyLink(expectCerts, expectCerts, j, t)

	// Replace the 2 certs with a different, single cert
	lbInfo.TLS = []*translator.TLSCerts{cert3}
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
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234, BackendNamer: j.namer}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000, BackendNamer: j.namer}}})
	ing := newIngress()
	feNamer := namer_util.NewFrontendNamerFactory(j.namer, "").Namer(ing)
	lbInfo := &L7RuntimeInfo{
		AllowHTTP: false,
		UrlMap:    gceUrlMap,
		Ingress:   ing,
	}
	oldCertName := fmt.Sprintf("k8s-ssl-%s", feNamer.LoadBalancer())
	tlsCert := createCert("key", "cert", "name")
	lbInfo.TLS = []*translator.TLSCerts{tlsCert}
	newCertName := feNamer.SSLCertName(tlsCert.CertHash)

	// Manually create a target proxy and assign a legacy cert to it.
	sslCert := &composite.SslCertificate{Name: oldCertName, Certificate: "cert", Version: defaultVersion}
	key, err := composite.CreateKey(j.fakeGCE, sslCert.Name, defaultScope)
	if err != nil {
		t.Fatal(err)
	}
	composite.CreateSslCertificate(j.fakeGCE, key, sslCert)
	sslCert, _ = composite.GetSslCertificate(j.fakeGCE, key, defaultVersion)
	tpName := feNamer.TargetProxy(namer_util.HTTPSProtocol)
	newProxy := &composite.TargetHttpsProxy{
		Name:            tpName,
		Description:     "fake",
		SslCertificates: []string{sslCert.SelfLink},
		Version:         defaultVersion,
	}
	key.Name = tpName
	err = composite.CreateTargetHttpsProxy(j.fakeGCE, key, newProxy)
	if err != nil {
		t.Fatalf("Failed to create Target proxy %v - %v", newProxy, err)
	}
	proxyCerts, err := composite.ListSslCertificates(j.fakeGCE, key, defaultVersion)
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
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234, BackendNamer: j.namer}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000, BackendNamer: j.namer}}})
	var tlsCerts []*translator.TLSCerts
	expectCerts := make(map[string]string)
	expectCertsExtra := make(map[string]string)
	ing := newIngress()
	feNamer := namer_util.NewFrontendNamerFactory(j.namer, "").Namer(ing)

	for ix := 0; ix < FakeCertQuota; ix++ {
		str := strconv.Itoa(ix)
		tlsCerts = append(tlsCerts, createCert("key-"+str, "cert-"+str, "name-"+str))
		certName := feNamer.SSLCertName(translator.GetCertHash("cert-" + str))
		expectCerts[certName] = "cert-" + str
	}
	lbInfo := &L7RuntimeInfo{
		AllowHTTP: false,
		TLS:       tlsCerts,
		UrlMap:    gceUrlMap,
		Ingress:   ing,
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
		expectCertsExtra[feNamer.SSLCertName(cert.CertHash)] = cert.Cert
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
		expectCerts[feNamer.SSLCertName(cert.CertHash)] = cert.Cert
	}
	verifyCertAndProxyLink(expectCerts, expectCerts, j, t)
}

// In case multiple certs for the same subject/hostname are uploaded, the certs will be sent in the same order they were
// specified, to the targetproxy. The targetproxy will present the first occurring cert for a given hostname to the client.
// This test verifies this behavior.
func TestIdenticalHostnameCerts(t *testing.T) {
	j := newTestJig(t)

	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234, BackendNamer: j.namer}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000, BackendNamer: j.namer}}})
	var tlsCerts []*translator.TLSCerts
	expectCerts := make(map[string]string)
	ing := newIngress()
	feNamer := namer_util.NewFrontendNamerFactory(j.namer, "").Namer(ing)
	contents := ""

	for ix := 0; ix < 3; ix++ {
		str := strconv.Itoa(ix)
		contents = "cert-" + str + " foo.com"
		tlsCerts = append(tlsCerts, createCert("key-"+str, contents, "name-"+str))
		certName := feNamer.SSLCertName(translator.GetCertHash(contents))
		expectCerts[certName] = contents
	}
	lbInfo := &L7RuntimeInfo{
		AllowHTTP: false,
		TLS:       tlsCerts,
		UrlMap:    gceUrlMap,
		Ingress:   ing,
	}

	// Sync multiple times to make sure ordering is preserved
	for i := 0; i < 10; i++ {
		if _, err := j.pool.Ensure(lbInfo); err != nil {
			t.Fatalf("pool.Ensure() = err %v", err)
		}
		verifyCertAndProxyLink(expectCerts, expectCerts, j, t)
		// Fetch the target proxy certs and go through in order
		verifyProxyCertsInOrder(" foo.com", j, t)
		j.pool.delete(feNamer, features.GAResourceVersions, defaultScope)
	}
}

func TestIdenticalHostnameCertsPreShared(t *testing.T) {
	j := newTestJig(t)

	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234, BackendNamer: j.namer}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000, BackendNamer: j.namer}}})
	lbInfo := &L7RuntimeInfo{
		AllowHTTP: false,
		UrlMap:    gceUrlMap,
		Ingress:   newIngress(),
	}
	key, err := composite.CreateKey(j.fakeGCE, "test-pre-shared-cert", defaultScope)
	if err != nil {
		t.Fatal(err)
	}
	// Prepare pre-shared certs.
	composite.CreateSslCertificate(j.fakeGCE, key, &composite.SslCertificate{
		Name:        "test-pre-shared-cert",
		Certificate: "cert-0 foo.com",
		SelfLink:    "existing",
	})
	preSharedCert1, _ := composite.GetSslCertificate(j.fakeGCE, key, defaultVersion)

	key.Name = "test-pre-shared-cert1"
	composite.CreateSslCertificate(j.fakeGCE, key, &composite.SslCertificate{
		Name:        "test-pre-shared-cert1",
		Certificate: "cert-1 foo.com",
		SelfLink:    "existing",
	})
	preSharedCert2, _ := composite.GetSslCertificate(j.fakeGCE, key, defaultVersion)

	key.Name = "test-pre-shared-cert2"
	composite.CreateSslCertificate(j.fakeGCE, key, &composite.SslCertificate{
		Name:        "test-pre-shared-cert2",
		Certificate: "cert2",
		SelfLink:    "existing",
	})
	preSharedCert3, _ := composite.GetSslCertificate(j.fakeGCE, key, defaultVersion)

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
		feNamer := namer_util.NewFrontendNamerFactory(j.namer, "").Namer(lbInfo.Ingress)
		j.pool.delete(feNamer, features.GAResourceVersions, defaultScope)
	}
}

// TestPreSharedToSecretBasedCertUpdate updates from pre-shared cert
// to secret based cert and verifies the pre-shared cert is retained.
func TestPreSharedToSecretBasedCertUpdate(t *testing.T) {
	j := newTestJig(t)

	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234, BackendNamer: j.namer}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000, BackendNamer: j.namer}}})
	ing := newIngress()
	feNamer := namer_util.NewFrontendNamerFactory(j.namer, "").Namer(ing)
	certName1 := feNamer.SSLCertName(translator.GetCertHash("cert"))
	certName2 := feNamer.SSLCertName(translator.GetCertHash("cert2"))

	lbInfo := &L7RuntimeInfo{
		AllowHTTP: false,
		UrlMap:    gceUrlMap,
		Ingress:   ing,
	}

	// Prepare pre-shared certs.
	key, err := composite.CreateKey(j.fakeGCE, "test-pre-shared-cert", defaultScope)
	if err != nil {
		t.Fatal(err)
	}
	composite.CreateSslCertificate(j.fakeGCE, key, &composite.SslCertificate{
		Name:        "test-pre-shared-cert",
		Certificate: "abc",
		SelfLink:    "existing",
	})
	preSharedCert1, _ := composite.GetSslCertificate(j.fakeGCE, key, defaultVersion)

	// Prepare pre-shared certs.
	key.Name = "test-pre-shared-cert2"
	composite.CreateSslCertificate(j.fakeGCE, key, &composite.SslCertificate{
		Name:        "test-pre-shared-cert2",
		Certificate: "xyz",
		SelfLink:    "existing",
		Version:     defaultVersion,
	})
	preSharedCert2, _ := composite.GetSslCertificate(j.fakeGCE, key, defaultVersion)

	lbInfo.TLSName = preSharedCert1.Name + "," + preSharedCert2.Name

	// Sync pre-shared certs.
	if _, err := j.pool.Ensure(lbInfo); err != nil {
		t.Fatalf("pool.Ensure() = err %v", err)
	}
	expectCerts := map[string]string{preSharedCert1.Name: preSharedCert1.Certificate,
		preSharedCert2.Name: preSharedCert2.Certificate}
	verifyCertAndProxyLink(expectCerts, expectCerts, j, t)

	// Updates from pre-shared cert to secret based cert.
	lbInfo.TLS = []*translator.TLSCerts{createCert("key", "cert", "name")}
	lbInfo.TLSName = ""
	if _, err := j.pool.Ensure(lbInfo); err != nil {
		t.Fatalf("pool.Ensure() = err %v", err)
	}
	expectCerts[certName1] = lbInfo.TLS[0].Cert
	// fakeLoadBalancer contains the preshared certs as well, but proxy will use only certName1
	expectCertsProxy := map[string]string{certName1: lbInfo.TLS[0].Cert}
	verifyCertAndProxyLink(expectCerts, expectCertsProxy, j, t)

	// Sync a different cert.
	lbInfo.TLS = []*translator.TLSCerts{createCert("key2", "cert2", "name")}
	j.pool.Ensure(lbInfo)
	delete(expectCerts, certName1)
	expectCerts[certName2] = lbInfo.TLS[0].Cert
	expectCertsProxy = map[string]string{certName2: lbInfo.TLS[0].Cert}
	verifyCertAndProxyLink(expectCerts, expectCertsProxy, j, t)

	// Check if pre-shared certs are retained.
	key.Name = preSharedCert1.Name
	if cert, err := composite.GetSslCertificate(j.fakeGCE, key, defaultVersion); err != nil || cert == nil {
		t.Fatalf("Want pre-shared certificate %v to exist, got none, err: %v", preSharedCert1.Name, err)
	}
	key.Name = preSharedCert2.Name
	if cert, err := composite.GetSslCertificate(j.fakeGCE, key, defaultVersion); err != nil || cert == nil {
		t.Fatalf("Want pre-shared certificate %v to exist, got none, err: %v", preSharedCert2.Name, err)
	}
}

func verifyProxyCertsInOrder(hostname string, j *testJig, t *testing.T) {
	t.Helper()
	t.Logf("f =\n%s", j.String())

	TPName := j.feNamer.TargetProxy(namer_util.HTTPSProtocol)
	key, err := composite.CreateKey(j.fakeGCE, TPName, defaultScope)
	if err != nil {
		t.Fatal(err)
	}
	tps, err := composite.GetTargetHttpsProxy(j.fakeGCE, key, defaultVersion)
	if err != nil {
		t.Fatalf("expected https proxy to exist: %v, err: %v", TPName, err)
	}
	count := 0
	tmp := ""

	for _, link := range tps.SslCertificates {
		certName, _ := utils.KeyName(link)
		key.Name = certName
		cert, err := composite.GetSslCertificate(j.fakeGCE, key, defaultVersion)
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
	key, err := composite.CreateKey(j.fakeGCE, "", defaultScope)
	if err != nil {
		t.Fatal(err)
	}
	allCerts, err := composite.ListSslCertificates(j.fakeGCE, key, defaultVersion)
	if err != nil {
		t.Fatalf("Failed to list certificates for %v - %v", j, err)
	}
	if len(expectCerts) != len(allCerts) {
		t.Fatalf("Unexpected number of certs in FakeLoadBalancer %v, expected %d, actual %d", j, len(expectCerts),
			len(allCerts))
	}
	for certName, certValue := range expectCerts {
		key.Name = certName
		cert, err := composite.GetSslCertificate(j.fakeGCE, key, defaultVersion)
		if err != nil {
			t.Fatalf("expected ssl certificate to exist: %v, err: %v, all certs: %v", certName, err, toCertNames(allCerts))
		}

		if cert.Certificate != certValue {
			t.Fatalf("unexpected certificate value for cert %s; expected %v, actual %v", certName, certValue, cert.Certificate)
		}
	}

	// httpsproxy needs to contain only the certs in expectCerts, nothing more, nothing less
	key, err = composite.CreateKey(j.fakeGCE, j.feNamer.TargetProxy(namer_util.HTTPSProtocol), defaultScope)
	if err != nil {
		t.Fatal(err)
	}
	tps, err := composite.GetTargetHttpsProxy(j.fakeGCE, key, defaultVersion)
	if err != nil {
		// Return immediately if expected certs is an empty map.
		if len(expectCertsProxy) == 0 && err.(*googleapi.Error).Code == http.StatusNotFound {
			return
		}
		t.Fatalf("expected https proxy to exist: %v, err: %v", j.feNamer.TargetProxy(namer_util.HTTPSProtocol), err)
	}
	if len(tps.SslCertificates) != len(expectCertsProxy) {
		t.Fatalf("Expected https proxy to have %d certs, actual %d", len(expectCertsProxy), len(tps.SslCertificates))
	}
	for _, link := range tps.SslCertificates {
		certName, err := utils.KeyName(link)
		if err != nil {
			t.Fatalf("error getting certName: %v", err)
		}
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
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234, BackendNamer: j.namer}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000, BackendNamer: j.namer}}})
	tlsName := "external-cert-name"
	lbInfo := &L7RuntimeInfo{
		AllowHTTP: false,
		TLSName:   tlsName,
		UrlMap:    gceUrlMap,
		Ingress:   newIngress(),
	}

	key, err := composite.CreateKey(j.fakeGCE, tlsName, defaultScope)
	if err != nil {
		t.Fatal(err)
	}
	composite.CreateSslCertificate(j.fakeGCE, key, &composite.SslCertificate{
		Name: tlsName,
	})
	if _, err := j.pool.Ensure(lbInfo); err != nil {
		t.Fatalf("pool.Ensure() = err %v", err)
	}
	l7, err := j.pool.Ensure(lbInfo)
	if err != nil || l7 == nil {
		t.Fatalf("Expected l7 not created")
	}
	verifyHTTPSForwardingRuleAndProxyLinks(t, j, l7)
}

func TestCreateBothLoadBalancers(t *testing.T) {
	// This should create 2 forwarding rules and target proxies
	// but they should use the same urlmap, and have the same
	// static ip.
	j := newTestJig(t)

	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234, BackendNamer: j.namer}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000, BackendNamer: j.namer}}})
	lbInfo := &L7RuntimeInfo{
		AllowHTTP: true,
		TLS:       []*translator.TLSCerts{{Key: "key", Cert: "cert"}},
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

	verifyHTTPSForwardingRuleAndProxyLinks(t, j, l7)
	verifyHTTPForwardingRuleAndProxyLinks(t, j, l7, "")

	// We know the forwarding rules exist, retrieve their addresses.
	key, err := composite.CreateKey(j.fakeGCE, "", defaultScope)
	if err != nil {
		t.Fatal(err)
	}

	key.Name = j.feNamer.ForwardingRule(namer_util.HTTPSProtocol)
	fws, _ := composite.GetForwardingRule(j.fakeGCE, key, defaultVersion)
	key.Name = j.feNamer.ForwardingRule(namer_util.HTTPProtocol)
	fw, _ := composite.GetForwardingRule(j.fakeGCE, key, defaultVersion)
	ip, err := j.fakeGCE.GetGlobalAddress(j.feNamer.ForwardingRule(namer_util.HTTPProtocol))
	if err != nil {
		t.Fatalf("%v", err)
	}
	if ip.Address != fw.IPAddress || ip.Address != fws.IPAddress {
		t.Fatalf("ip.Address = %q, want %q and %q all equal", ip.Address, fw.IPAddress, fws.IPAddress)
	}
}

// Test StaticIP annotation behavior.
// When a non-existent StaticIP value is specified, ingress creation must fail.
func TestStaticIP(t *testing.T) {
	j := newTestJig(t)
	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234, BackendNamer: j.namer}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000, BackendNamer: j.namer}}})
	ing := newIngress()
	ing.Annotations = map[string]string{
		"StaticIPNameKey": "teststaticip",
	}
	lbInfo := &L7RuntimeInfo{
		AllowHTTP:    true,
		TLS:          []*translator.TLSCerts{{Key: "key", Cert: "cert"}},
		UrlMap:       gceUrlMap,
		Ingress:      ing,
		StaticIPName: "teststaticip",
	}

	if _, err := j.pool.Ensure(lbInfo); err == nil {
		t.Fatalf("expected error ensuring ingress with non-existent static ip")
	}
	// Create static IP
	err := j.fakeGCE.ReserveGlobalAddress(&compute.Address{Name: "teststaticip", Address: "1.2.3.4"})
	if err != nil {
		t.Fatalf("ip address reservation failed - %v", err)
	}
	if _, err := j.pool.Ensure(lbInfo); err != nil {
		t.Fatalf("unexpected error %v", err)
	}
}

// Test setting frontendconfig Ssl policy
func TestFrontendConfigSslPolicy(t *testing.T) {
	flags.F.EnableFrontendConfig = true
	defer func() { flags.F.EnableFrontendConfig = false }()

	j := newTestJig(t)

	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234, BackendNamer: j.namer}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000, BackendNamer: j.namer}}})
	lbInfo := &L7RuntimeInfo{
		AllowHTTP:      false,
		TLS:            []*translator.TLSCerts{createCert("key", "cert", "name")},
		UrlMap:         gceUrlMap,
		Ingress:        newIngress(),
		FrontendConfig: &frontendconfigv1beta1.FrontendConfig{Spec: frontendconfigv1beta1.FrontendConfigSpec{SslPolicy: utils.NewStringPointer("test-policy")}},
	}

	l7, err := j.pool.Ensure(lbInfo)
	if err != nil {
		t.Fatalf("j.pool.Ensure(%v) = %v, want nil", lbInfo, err)
	}

	tpsName := l7.tps.Name
	tps, _ := composite.GetTargetHttpsProxy(j.fakeGCE, meta.GlobalKey(tpsName), meta.VersionGA)

	resourceID, err := cloud.ParseResourceURL(tps.SslPolicy)
	if err != nil {
		t.Errorf("ParseResourceURL(%+v) = %v, want nil", tps.SslPolicy, err)
	}

	path := resourceID.ResourcePath()
	want := "global/sslPolicies/test-policy"

	if path != want {
		t.Errorf("tps ssl policy = %q, want %q", path, want)
	}
}

func TestFrontendConfigRedirects(t *testing.T) {
	flags.F.EnableFrontendConfig = true
	defer func() { flags.F.EnableFrontendConfig = false }()

	j := newTestJig(t)
	ing := newIngress()

	// Use v2 naming scheme since v1 is not supported
	ing.ObjectMeta.Finalizers = []string{common.FinalizerKeyV2}

	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234, BackendNamer: j.namer}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000, BackendNamer: j.namer}}})
	lbInfo := &L7RuntimeInfo{
		AllowHTTP:      true,
		TLS:            []*translator.TLSCerts{createCert("key", "cert", "name")},
		UrlMap:         gceUrlMap,
		Ingress:        ing,
		FrontendConfig: &frontendconfigv1beta1.FrontendConfig{Spec: frontendconfigv1beta1.FrontendConfigSpec{RedirectToHttps: &frontendconfigv1beta1.HttpsRedirectConfig{Enabled: true}}},
	}

	l7, err := j.pool.Ensure(lbInfo)
	if err != nil {
		t.Fatalf("j.pool.Ensure(%v) = %v, want nil", lbInfo, err)
	}

	// Only verify HTTPS since the HTTP Target Proxy points to the redirect url map
	verifyHTTPSForwardingRuleAndProxyLinks(t, j, l7)

	if l7.redirectUm == nil {
		t.Errorf("l7.redirectUm is nil")
	}

	tpName := l7.tp.Name
	tp, err := composite.GetTargetHttpProxy(j.fakeGCE, meta.GlobalKey(tpName), meta.VersionGA)
	if err != nil {
		t.Error(err)
	}

	resourceID, err := cloud.ParseResourceURL(tp.UrlMap)
	if err != nil {
		t.Errorf("ParseResourceURL(%+v) = %v, want nil", tp.UrlMap, err)
	}

	path := resourceID.ResourcePath()
	want := "global/urlMaps/" + l7.redirectUm.Name

	if path != want {
		t.Errorf("tps ssl policy = %q, want %q", path, want)
	}
}

func TestEnsureSslPolicy(t *testing.T) {
	t.Parallel()
	j := newTestJig(t)

	testCases := []struct {
		desc       string
		fc         *frontendconfigv1beta1.FrontendConfig
		proxy      *composite.TargetHttpsProxy
		policyLink string
		want       string
	}{
		{
			desc:  "Empty frontendconfig",
			proxy: &composite.TargetHttpsProxy{Name: "test-proxy-0"},
			fc:    nil,
			want:  "",
		},
		{
			desc:  "frontendconfig with no ssl policy",
			fc:    &frontendconfigv1beta1.FrontendConfig{Spec: frontendconfigv1beta1.FrontendConfigSpec{}},
			proxy: &composite.TargetHttpsProxy{Name: "test-proxy-1"},
			want:  "",
		},
		{
			desc:       "frontendconfig with ssl policy",
			fc:         &frontendconfigv1beta1.FrontendConfig{Spec: frontendconfigv1beta1.FrontendConfigSpec{SslPolicy: utils.NewStringPointer("test-policy")}},
			proxy:      &composite.TargetHttpsProxy{Name: "test-proxy-2"},
			policyLink: "global/sslPolicies/test-policy",
			want:       "global/sslPolicies/test-policy",
		},
		{
			desc:       "proxy with different ssl policy",
			fc:         &frontendconfigv1beta1.FrontendConfig{Spec: frontendconfigv1beta1.FrontendConfigSpec{SslPolicy: utils.NewStringPointer("test-policy")}},
			proxy:      &composite.TargetHttpsProxy{Name: "test-proxy-3", SslPolicy: "global/sslPolicies/wrong-policy"},
			policyLink: "global/sslPolicies/test-policy",
			want:       "global/sslPolicies/test-policy",
		},
		{
			desc:       "proxy with ssl policy and frontend config policy is nil",
			fc:         &frontendconfigv1beta1.FrontendConfig{Spec: frontendconfigv1beta1.FrontendConfigSpec{SslPolicy: nil}},
			proxy:      &composite.TargetHttpsProxy{Name: "test-proxy-4", SslPolicy: "global/sslPolicies/test-policy"},
			policyLink: "global/sslPolicies/test-policy",
			want:       "global/sslPolicies/test-policy",
		},
		{
			desc:  "remove ssl policy",
			fc:    &frontendconfigv1beta1.FrontendConfig{Spec: frontendconfigv1beta1.FrontendConfigSpec{SslPolicy: utils.NewStringPointer("")}},
			proxy: &composite.TargetHttpsProxy{Name: "test-proxy-5", SslPolicy: "global/sslPolicies/wrong-policy"},
			want:  "",
		},
	}

	for _, tc := range testCases {
		key := meta.GlobalKey(tc.proxy.Name)
		if err := composite.CreateTargetHttpsProxy(j.fakeGCE, key, tc.proxy); err != nil {
			t.Error(err)
		}
		l7 := L7{runtimeInfo: &L7RuntimeInfo{FrontendConfig: tc.fc}, cloud: j.fakeGCE, scope: meta.Global}
		env := &translator.Env{FrontendConfig: tc.fc}

		if err := l7.ensureSslPolicy(env, tc.proxy, tc.policyLink); err != nil {
			t.Errorf("desc: %q, l7.ensureSslPolicy() = %v, want nil", tc.desc, err)
		}

		result, err := composite.GetTargetHttpsProxy(j.fakeGCE, key, meta.VersionGA)
		if err != nil {
			t.Error(err)
		}

		if result.SslPolicy != tc.want {
			t.Errorf("desc: %q, want %q, got %q", tc.desc, tc.want, result.SslPolicy)
		}
	}
}

// verifyURLMap gets the created URLMap and compares it against an expected one.
func verifyURLMap(t *testing.T, j *testJig, feNamer namer_util.IngressFrontendNamer, wantGCEURLMap *utils.GCEURLMap) {
	t.Helper()

	name := feNamer.UrlMap()
	key, err := composite.CreateKey(j.fakeGCE, name, defaultScope)
	if err != nil {
		t.Fatal(err)
	}
	um, err := composite.GetUrlMap(j.fakeGCE, key, defaultVersion)
	if err != nil || um == nil {
		t.Errorf("j.fakeGCE.GetUrlMap(%q) = %v, %v; want _, nil", name, um, err)
	}
	wantComputeURLMap := translator.ToCompositeURLMap(wantGCEURLMap, feNamer, key)
	if !mapsEqual(wantComputeURLMap, um) {
		t.Errorf("mapsEqual() = false, got\n%+v\n  want\n%+v", um, wantComputeURLMap)
	}
}

func TestUrlMapChange(t *testing.T) {
	j := newTestJig(t)

	um1 := utils.NewGCEURLMap()
	um2 := utils.NewGCEURLMap()

	um1.PutPathRulesForHost("bar.example.com", []utils.PathRule{{Path: "/bar2", Backend: utils.ServicePort{NodePort: 30000, BackendNamer: j.namer}}})
	um1.DefaultBackend = &utils.ServicePort{NodePort: 31234, BackendNamer: j.namer}

	um2.PutPathRulesForHost("foo.example.com", []utils.PathRule{
		{Path: "/foo1", Backend: utils.ServicePort{NodePort: 30001, BackendNamer: j.namer}},
		{Path: "/foo2", Backend: utils.ServicePort{NodePort: 30002, BackendNamer: j.namer}},
	})
	um2.PutPathRulesForHost("bar.example.com", []utils.PathRule{{Path: "/bar1", Backend: utils.ServicePort{NodePort: 30003, BackendNamer: j.namer}}})
	um2.DefaultBackend = &utils.ServicePort{NodePort: 30004, BackendNamer: j.namer}

	lbInfo := &L7RuntimeInfo{AllowHTTP: true, UrlMap: um1, Ingress: newIngress()}

	if _, err := j.pool.Ensure(lbInfo); err != nil {
		t.Fatalf("pool.Ensure() = err %v", err)
	}

	l7, err := j.pool.Ensure(lbInfo)
	if err != nil {
		t.Fatalf("%v", err)
	}
	verifyURLMap(t, j, l7.namer, um1)

	// Change url map.
	lbInfo.UrlMap = um2
	if _, err = j.pool.Ensure(lbInfo); err != nil {
		t.Fatalf("pool.Ensure() = err %v", err)
	}
	l7, err = j.pool.Ensure(lbInfo)
	if err != nil {
		t.Fatalf("%v", err)
	}
	verifyURLMap(t, j, l7.namer, um2)
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
		{Path: "/foo1", Backend: utils.ServicePort{NodePort: 30000, BackendNamer: j.namer}},
		{Path: "/foo2", Backend: utils.ServicePort{NodePort: 30001, BackendNamer: j.namer}},
	})
	um1.PutPathRulesForHost("bar.example.com", []utils.PathRule{{Path: "/bar1", Backend: utils.ServicePort{NodePort: 30002, BackendNamer: j.namer}}})
	um1.DefaultBackend = &utils.ServicePort{NodePort: 30003, BackendNamer: j.namer}

	um2.PutPathRulesForHost("foo.example.com", []utils.PathRule{
		{Path: "/foo1", Backend: utils.ServicePort{NodePort: 30000, BackendNamer: j.namer}},
		{Path: "/foo2", Backend: utils.ServicePort{NodePort: 30001, BackendNamer: j.namer}},
	})
	um2.PutPathRulesForHost("bar.example.com", []utils.PathRule{{Path: "/bar1", Backend: utils.ServicePort{NodePort: 30002, BackendNamer: j.namer}}})
	um2.DefaultBackend = &utils.ServicePort{NodePort: 30003, BackendNamer: j.namer}

	lbInfo := &L7RuntimeInfo{AllowHTTP: true, UrlMap: um1, Ingress: newIngress()}
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
	namer := namer_util.NewNamer(clusterName, firewallName)
	fullName := namer.ForwardingRule(namer.LoadBalancer("testlb"), namer_util.HTTPProtocol)
	annotationsMap := map[string]string{
		fmt.Sprintf(annotations.HttpForwardingRuleKey): fullName,
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
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234, BackendNamer: j.namer}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000, BackendNamer: j.namer}}})
	lbInfo := &L7RuntimeInfo{
		AllowHTTP: true,
		TLS:       []*translator.TLSCerts{{Key: "key", Cert: "cert"}},
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
	verifyHTTPSForwardingRuleAndProxyLinks(t, j, l7)
	verifyHTTPForwardingRuleAndProxyLinks(t, j, l7, "")

	newName := "new-name"
	j.namer.SetUID(newName)

	// Now the components should get renamed with the next suffix.
	l7, err = j.pool.Ensure(lbInfo)
	if err != nil || j.namer.ParseName(l7.namer.LoadBalancer().String()).ClusterName != newName {
		t.Fatalf("Expected L7 name to change.")
	}
	verifyHTTPSForwardingRuleAndProxyLinks(t, j, l7)
	verifyHTTPForwardingRuleAndProxyLinks(t, j, l7, "")
}

func TestInvalidClusterNameChange(t *testing.T) {
	namer := namer_util.NewNamer("test--123", "test--123")
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

func createCert(key string, contents string, name string) *translator.TLSCerts {
	return &translator.TLSCerts{Key: key, Cert: contents, Name: name, CertHash: translator.GetCertHash(contents)}
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
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234, BackendNamer: j.namer}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000, BackendNamer: j.namer}}})
	lbInfo := &L7RuntimeInfo{
		AllowHTTP: true,
		TLS:       []*translator.TLSCerts{{Key: "key", Cert: "cert"}},
		UrlMap:    gceUrlMap,
		Ingress:   newIngress(),
	}

	names := []string{
		"invalid-url-map-name1",
		"invalid-url-map-name2",
		"wrongprefix-um-test--uid1",
		"k8s-um-old-l7--uid1", // Expect List() to catch old URL maps
	}

	key, err := composite.CreateKey(j.fakeGCE, "", defaultScope)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		key.Name = name
		composite.CreateUrlMap(j.fakeGCE, key, &composite.UrlMap{Name: name})
	}

	if _, err := j.pool.Ensure(lbInfo); err != nil {
		t.Fatalf("j.pool.Ensure() = %v; want nil", err)
	}

	urlMaps, err := j.pool.list(key, defaultVersion)
	if err != nil {
		t.Fatalf("j.pool.List(%q, %q) = %v, want nil", key, defaultVersion, err)
	}
	var umNames []string
	for _, um := range urlMaps {
		umNames = append(umNames, um.Name)
	}
	expected := []string{"k8s-um-namespace1-test--uid1", "k8s-um-old-l7--uid1"}

	for _, name := range expected {
		if !slice.ContainsString(umNames, name, nil) {
			t.Fatalf("j.pool.List(%q, %q) returned names %v, want %v", key, defaultVersion, umNames, expected)
		}
	}
}

// TestSecretBasedAndPreSharedCerts creates both pre-shared and
// secret-based certs and tests that all should be used.
func TestSecretBasedAndPreSharedCerts(t *testing.T) {
	j := newTestJig(t)

	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234, BackendNamer: j.namer}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000, BackendNamer: j.namer}}})
	namer := namer_util.NewNamer(clusterName, "fw1")
	ing := newIngress()
	feNamer := namer_util.NewFrontendNamerFactory(namer, "").Namer(ing)
	certName1 := feNamer.SSLCertName(translator.GetCertHash("cert"))
	certName2 := feNamer.SSLCertName(translator.GetCertHash("cert2"))

	lbInfo := &L7RuntimeInfo{
		AllowHTTP: false,
		UrlMap:    gceUrlMap,
		Ingress:   ing,
	}

	// Prepare pre-shared certs.
	key, err := composite.CreateKey(j.fakeGCE, "", defaultScope)
	if err != nil {
		t.Fatal(err)
	}
	key.Name = "test-pre-shared-cert"
	composite.CreateSslCertificate(j.fakeGCE, key, &composite.SslCertificate{
		Name:        "test-pre-shared-cert",
		Certificate: "abc",
		SelfLink:    "existing",
	})
	preSharedCert1, _ := composite.GetSslCertificate(j.fakeGCE, key, defaultVersion)

	key.Name = "test-pre-shared-cert2"
	composite.CreateSslCertificate(j.fakeGCE, key, &composite.SslCertificate{
		Name:        "test-pre-shared-cert2",
		Certificate: "xyz",
		SelfLink:    "existing2",
	})
	preSharedCert2, _ := composite.GetSslCertificate(j.fakeGCE, key, defaultVersion)
	lbInfo.TLSName = preSharedCert1.Name + "," + preSharedCert2.Name

	// Secret based certs.
	lbInfo.TLS = []*translator.TLSCerts{
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
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234, BackendNamer: j.namer}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000, BackendNamer: j.namer}}})

	var tlsCerts []*translator.TLSCerts
	expectCerts := make(map[string]string)
	expectCertsExtra := make(map[string]string)
	ing := newIngress()
	feNamer := namer_util.NewFrontendNamerFactory(j.namer, "").Namer(ing)

	for ix := 0; ix < TargetProxyCertLimit; ix++ {
		str := strconv.Itoa(ix)
		tlsCerts = append(tlsCerts, createCert("key-"+str, "cert-"+str, "name-"+str))
		certName := feNamer.SSLCertName(translator.GetCertHash("cert-" + str))
		expectCerts[certName] = "cert-" + str
		expectCertsExtra[certName] = "cert-" + str
	}
	lbInfo := &L7RuntimeInfo{
		AllowHTTP: false,
		TLS:       tlsCerts,
		UrlMap:    gceUrlMap,
		Ingress:   ing,
	}

	if _, err := j.pool.Ensure(lbInfo); err != nil {
		t.Fatalf("j.pool.Ensure() = err %v", err)
	}
	verifyCertAndProxyLink(expectCerts, expectCerts, j, t)

	// Create pre-shared certs up to FakeCertQuota.
	preSharedCerts := []*composite.SslCertificate{}
	tlsNames := []string{}
	key, err := composite.CreateKey(j.fakeGCE, "", defaultScope)
	if err != nil {
		t.Fatal(err)
	}
	for ix := TargetProxyCertLimit; ix < FakeCertQuota; ix++ {
		str := strconv.Itoa(ix)
		key.Name = "test-pre-shared-cert-" + str
		err := composite.CreateSslCertificate(j.fakeGCE, key, &composite.SslCertificate{
			Name:        "test-pre-shared-cert-" + str,
			Certificate: "abc-" + str,
			SelfLink:    "existing-" + str,
		})
		if err != nil {
			t.Fatalf("j.fakeGCE.CreateSslCertificate() = err %v", err)
		}
		cert, _ := composite.GetSslCertificate(j.fakeGCE, key, defaultVersion)
		preSharedCerts = append(preSharedCerts, cert)
		tlsNames = append(tlsNames, cert.Name)
		expectCertsExtra[cert.Name] = cert.Certificate
	}
	lbInfo.TLSName = strings.Join(tlsNames, ",")

	key.Name = "test-pre-shared-cert-100"
	err = composite.CreateSslCertificate(j.fakeGCE, key, &composite.SslCertificate{
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
		expectCerts[feNamer.SSLCertName(cert.CertHash)] = cert.Cert
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
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234, BackendNamer: j.namer}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000, BackendNamer: j.namer}}})
	ing := newIngress()
	feNamer := namer_util.NewFrontendNamerFactory(j.namer, "").Namer(ing)
	certName1 := feNamer.SSLCertName(translator.GetCertHash("cert"))

	lbInfo := &L7RuntimeInfo{
		AllowHTTP: false,
		UrlMap:    gceUrlMap,
		Ingress:   ing,
	}

	// Sync secret based cert.
	lbInfo.TLS = []*translator.TLSCerts{createCert("key", "cert", "name")}
	lbInfo.TLSName = ""
	if _, err := j.pool.Ensure(lbInfo); err != nil {
		t.Fatalf("pool.Ensure() = err %v", err)
	}
	expectCerts := map[string]string{certName1: lbInfo.TLS[0].Cert}
	verifyCertAndProxyLink(expectCerts, expectCerts, j, t)

	// Prepare pre-shared cert.
	key, err := composite.CreateKey(j.fakeGCE, "test-pre-shared-cert", defaultScope)
	if err != nil {
		t.Fatal(err)
	}
	composite.CreateSslCertificate(j.fakeGCE, key, &composite.SslCertificate{
		Name:        "test-pre-shared-cert",
		Certificate: "abc",
		SelfLink:    "existing",
	})
	preSharedCert1, _ := composite.GetSslCertificate(j.fakeGCE, key, defaultVersion)
	lbInfo.TLSName = preSharedCert1.Name

	// Sync certs.
	if _, err := j.pool.Ensure(lbInfo); err != nil {
		t.Fatalf("j.pool.Ensure() = err %v", err)
	}
	expectCerts[preSharedCert1.Name] = preSharedCert1.Certificate
	verifyCertAndProxyLink(expectCerts, expectCerts, j, t)

	// Delete the secret.
	lbInfo.TLS = []*translator.TLSCerts{}
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
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234, BackendNamer: j.namer}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000, BackendNamer: j.namer}}})
	ing := newIngress()
	feNamer := namer_util.NewFrontendNamerFactory(j.namer, "").Namer(ing)
	certName1 := feNamer.SSLCertName(translator.GetCertHash("cert"))

	lbInfo := &L7RuntimeInfo{
		AllowHTTP: false,
		UrlMap:    gceUrlMap,
		Ingress:   ing,
	}

	// Sync secret based cert.
	lbInfo.TLS = []*translator.TLSCerts{createCert("key", "cert", "name")}
	lbInfo.TLSName = ""
	if _, err := j.pool.Ensure(lbInfo); err != nil {
		t.Fatalf("pool.Ensure() = err %v", err)
	}
	expectCerts := map[string]string{certName1: lbInfo.TLS[0].Cert}
	verifyCertAndProxyLink(expectCerts, expectCerts, j, t)

	// Prepare pre-shared certs.
	key, err := composite.CreateKey(j.fakeGCE, "test-pre-shared-cert", defaultScope)
	if err != nil {
		t.Fatal(err)
	}
	composite.CreateSslCertificate(j.fakeGCE, key, &composite.SslCertificate{
		Name:        "test-pre-shared-cert",
		Certificate: "abc",
		SelfLink:    "existing",
	})
	preSharedCert1, _ := composite.GetSslCertificate(j.fakeGCE, key, defaultVersion)

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

// TestResourceDeletionWithProtocol asserts that unused resources are cleaned up
// on updating ingress configuration to disable http/https traffic.
func TestResourceDeletionWithProtocol(t *testing.T) {
	// TODO(smatti): Add flag saver to capture current value and reset back.
	flags.F.EnableDeleteUnusedFrontends = true
	j := newTestJig(t)

	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234, BackendNamer: j.namer}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000, BackendNamer: j.namer}}})
	ing := newIngress()
	feNamer := namer_util.NewFrontendNamerFactory(j.namer, "").Namer(ing)
	versions := features.GAResourceVersions
	certName1 := feNamer.SSLCertName(translator.GetCertHash("cert1"))

	for _, tc := range []struct {
		desc         string
		disableHTTP  bool
		disableHTTPS bool
	}{
		{"both enabled", false, false},
		{"http only", false, true},
		{"https only", true, false},
		{"both disabled", true, true},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			lbInfo := &L7RuntimeInfo{
				AllowHTTP: true,
				TLS: []*translator.TLSCerts{
					createCert("key1", "cert1", "secret1"),
				},
				UrlMap:  gceUrlMap,
				Ingress: ing,
			}
			lb, err := j.pool.Ensure(lbInfo)
			if err != nil {
				t.Fatalf("pool.Ensure(%+v) = %v, want nil", lbInfo, err)
			}
			// Update ingress annotations
			ing.Annotations = lb.getFrontendAnnotations(make(map[string]string))
			verifyLBAnnotations(t, lb, ing.Annotations)

			expectCerts := map[string]string{certName1: lbInfo.TLS[0].Cert}
			verifyCertAndProxyLink(expectCerts, expectCerts, j, t)
			if err := checkBothFakeLoadBalancers(j.fakeGCE, feNamer, versions, defaultScope, true, true); err != nil {
				t.Errorf("checkFakeLoadBalancer(..., true, true) = %v, want nil for case %q and key %q", err, tc.desc, common.NamespacedName(ing))
			}

			expectHttp, expectHttps := true, true
			if tc.disableHTTP {
				lbInfo.AllowHTTP = false
				expectHttp = false
			}
			if tc.disableHTTPS {
				lbInfo.TLS = nil
				expectHttps = false
				delete(expectCerts, certName1)
			}

			lb, err = j.pool.Ensure(lbInfo)
			if tc.disableHTTP && tc.disableHTTPS {
				// we expect an invalid ingress configuration error here.
				if err == nil || !strings.Contains(err.Error(), invalidConfigErrorMessage) {
					t.Fatalf("pool.Ensure(%+v) = %v, want %v", lbInfo, err, fmt.Errorf(invalidConfigErrorMessage))
				}
				return
			}
			if err != nil {
				t.Fatalf("pool.Ensure(%+v) = %v, want nil", lbInfo, err)
			}
			// Update ingress annotations
			ing.Annotations = lb.getFrontendAnnotations(make(map[string]string))
			verifyLBAnnotations(t, lb, ing.Annotations)

			verifyCertAndProxyLink(expectCerts, expectCerts, j, t)
			if err := checkBothFakeLoadBalancers(j.fakeGCE, feNamer, versions, defaultScope, expectHttp, expectHttps); err != nil {
				t.Errorf("checkFakeLoadBalancer(..., %t, %t) = %v, want nil for case %q and key %q", expectHttp, expectHttps, err, tc.desc, common.NamespacedName(ing))
			}
		})
	}
}

// TestResourceDeletionWithScopeChange asserts that unused resources are cleaned up
// on updating ingress configuration to change from ELB to ILB or vice versa.
// This test applies to the V2 naming scheme only
func TestResourceDeletionWithScopeChange(t *testing.T) {
	j := newTestJig(t)

	gceUrlMap := utils.NewGCEURLMap()
	gceUrlMap.DefaultBackend = &utils.ServicePort{NodePort: 31234, BackendNamer: j.namer}
	gceUrlMap.PutPathRulesForHost("bar.example.com", []utils.PathRule{{Path: "/bar", Backend: utils.ServicePort{NodePort: 30000, BackendNamer: j.namer}}})

	testCases := []struct {
		desc        string
		beforeClass string
		afterClass  string
		gcScope     meta.KeyType
		ing         *networkingv1.Ingress
	}{
		{
			desc:        "ELB to ILB",
			beforeClass: "gce",
			afterClass:  "gce-internal",
			gcScope:     meta.Global,
			ing: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "ing-1",
					Namespace:   namespace,
					Annotations: map[string]string{},
				},
			},
		},
		{
			desc:        "ILB to ELB",
			beforeClass: "gce-internal",
			afterClass:  "gce",
			gcScope:     meta.Regional,
			ing: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "ing-2",
					Namespace:   namespace,
					Annotations: map[string]string{},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			tc.ing.ObjectMeta.Finalizers = []string{common.FinalizerKeyV2}

			lbInfo := &L7RuntimeInfo{
				AllowHTTP: true,
				UrlMap:    gceUrlMap,
				Ingress:   tc.ing,
			}

			tc.ing.Annotations[annotations.IngressClassKey] = tc.beforeClass
			l7, err := j.pool.Ensure(lbInfo)
			if err != nil || l7 == nil {
				t.Fatalf("Expected l7 not created, err: %v", err)
			}
			verifyHTTPForwardingRuleAndProxyLinks(t, j, l7, "")

			tc.ing.Annotations[annotations.IngressClassKey] = tc.afterClass
			l7, err = j.pool.Ensure(lbInfo)
			if err != nil || l7 == nil {
				t.Fatalf("Expected l7 not created, err: %v", err)
			}
			verifyHTTPForwardingRuleAndProxyLinks(t, j, l7, "")

			// Check to make sure that there is something to GC
			scope, err := j.pool.FrontendScopeChangeGC(tc.ing)
			if scope == nil || *scope != tc.gcScope || err != nil {
				t.Errorf("FrontendScopeChangeGC(%v) = (%v, %v), want (%q, nil)", tc.ing, scope, err, tc.gcScope)
			}

			if err := j.pool.GCv2(tc.ing, tc.gcScope); err != nil {
				t.Errorf("GCv2(%v, %q) = %v, want nil", tc.ing, tc.gcScope, err)
			}

			// Check to make sure that there is nothing to GC
			scope, err = j.pool.FrontendScopeChangeGC(tc.ing)
			if scope != nil || err != nil {
				t.Errorf("FrontendScopeChangeGC(%v) = (%v, %v), want (nil, nil)", tc.ing, scope, err)
			}
		})
	}
}

// verifyLBAnnotations asserts that ingress annotations updated correctly.
func verifyLBAnnotations(t *testing.T, l7 *L7, ingAnnotations map[string]string) {
	var l7Certs []string
	for _, cert := range l7.sslCerts {
		l7Certs = append(l7Certs, cert.Name)
	}
	fw, exists := ingAnnotations[annotations.HttpForwardingRuleKey]
	if l7.fw != nil {
		if !exists {
			t.Errorf("Expected http forwarding rule annotation to exist")
		} else if diff := cmp.Diff(l7.fw.Name, fw); diff != "" {
			t.Errorf("Got diff for http forwarding rule (-want +got):\n%s", diff)
		}
	} else if exists {
		t.Errorf("Expected http forwarding rule annotation to not exist")
	}
	tp, exists := ingAnnotations[annotations.TargetHttpProxyKey]
	if l7.tp != nil {
		if !exists {
			t.Errorf("Expected target http proxy annotation to exist")
		} else if diff := cmp.Diff(l7.tp.Name, tp); diff != "" {
			t.Errorf("Got diff for target http proxy (-want +got):\n%s", diff)
		}
	} else if exists {
		t.Errorf("Expected target http proxy annotation to not exist")
	}
	fws, exists := ingAnnotations[annotations.HttpsForwardingRuleKey]
	if l7.fws != nil {
		if !exists {
			t.Errorf("Expected https forwarding rule annotation to exist")
		} else if diff := cmp.Diff(l7.fws.Name, fws); diff != "" {
			t.Errorf("Got diff for https forwarding rule (-want +got):\n%s", diff)
		}
	} else if exists {
		t.Errorf("Expected https forwarding rule annotation to not exist")
	}
	tps, exists := ingAnnotations[annotations.TargetHttpsProxyKey]
	if l7.tps != nil {
		if !exists {
			t.Errorf("Expected target https proxy annotation to exist")
		} else if diff := cmp.Diff(l7.tps.Name, tps); diff != "" {
			t.Errorf("Got diff for target https proxy (-want +got):\n%s", diff)
		}
	} else if exists {
		t.Errorf("Expected target https proxy annotation to not exist")
	}
	certs, exists := ingAnnotations[annotations.SSLCertKey]
	if len(l7Certs) > 0 {
		if !exists {
			t.Errorf("Expected ssl cert annotation to exist")
		} else if diff := cmp.Diff(strings.Join(l7Certs, ","), certs); diff != "" {
			t.Errorf("Got diff for ssl certs (-want +got):\n%s", diff)
		}
	} else if exists {
		t.Errorf("Expected ssl cert annotation to not exist")
	}
}
