/*
Copyright 2019 The Kubernetes Authors.

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

package namer

import (
	"crypto/sha256"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	v1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	clusterUID    = "uid1"
	kubeSystemUID = "kubesystem-uid1"
)

func newIngress(namespace, name string) *v1.Ingress {
	return &v1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
}

// TestV1IngressFrontendNamer tests that v1 frontend namer created from load balancer,
// 1. returns expected values.
// 2. returns same values as old namer.
// 3. returns same values as v1 frontend namer created from ingress.
func TestV1IngressFrontendNamer(t *testing.T) {
	longString := "01234567890123456789012345678901234567890123456789"
	testCases := []struct {
		desc      string
		namespace string
		name      string
		// Expected values.
		lbName              LoadBalancerName
		targetHTTPProxy     string
		targetHTTPSProxy    string
		sslCert             string
		forwardingRuleHTTP  string
		forwardingRuleHTTPS string
		urlMap              string
		isValidName         bool
	}{
		{
			"simple case",
			"namespace",
			"name",
			LoadBalancerName("namespace-name--uid1"),
			"%s-tp-namespace-name--uid1",
			"%s-tps-namespace-name--uid1",
			"%s-ssl-9a60a5272f6eee97-%s--uid1",
			"%s-fw-namespace-name--uid1",
			"%s-fws-namespace-name--uid1",
			"%s-um-namespace-name--uid1",
			true,
		},
		{
			"62 characters",
			// Total combined length of namespace and name is 47.
			longString[:23],
			longString[:24],
			LoadBalancerName("01234567890123456789012-012345678901234567890123--uid1"),
			"%s-tp-01234567890123456789012-012345678901234567890123--uid1",
			"%s-tps-01234567890123456789012-012345678901234567890123--uid1",
			"%s-ssl-4169c63684f5e4cd-%s--uid1",
			"%s-fw-01234567890123456789012-012345678901234567890123--uid1",
			"%s-fws-01234567890123456789012-012345678901234567890123--uid1",
			"%s-um-01234567890123456789012-012345678901234567890123--uid1",
			true,
		},
		{
			"63 characters",
			// Total combined length of namespace and name is 48.
			longString[:24],
			longString[:24],
			LoadBalancerName("012345678901234567890123-012345678901234567890123--uid1"),
			"%s-tp-012345678901234567890123-012345678901234567890123--uid1",
			"%s-tps-012345678901234567890123-012345678901234567890123--uid0",
			"%s-ssl-c7616cb0f76c2df2-%s--uid1",
			"%s-fw-012345678901234567890123-012345678901234567890123--uid1",
			"%s-fws-012345678901234567890123-012345678901234567890123--uid0",
			"%s-um-012345678901234567890123-012345678901234567890123--uid1",
			true,
		},
		{
			"64 characters",
			// Total combined length of namespace and name is 49.
			longString[:24],
			longString[:25],
			LoadBalancerName("012345678901234567890123-0123456789012345678901234--uid1"),
			"%s-tp-012345678901234567890123-0123456789012345678901234--uid0",
			"%s-tps-012345678901234567890123-0123456789012345678901234--ui0",
			"%s-ssl-537beba3a874a029-%s--uid1",
			"%s-fw-012345678901234567890123-0123456789012345678901234--uid0",
			"%s-fws-012345678901234567890123-0123456789012345678901234--ui0",
			"%s-um-012345678901234567890123-0123456789012345678901234--uid0",
			true,
		},
		{
			"long namespace",
			longString,
			"0",
			LoadBalancerName("01234567890123456789012345678901234567890123456789-0--uid1"),
			"%s-tp-01234567890123456789012345678901234567890123456789-0--u0",
			"%s-tps-01234567890123456789012345678901234567890123456789-0--0",
			"%s-ssl-92bdb5e4d378b3ce-%s--uid1",
			"%s-fw-01234567890123456789012345678901234567890123456789-0--u0",
			"%s-fws-01234567890123456789012345678901234567890123456789-0--0",
			"%s-um-01234567890123456789012345678901234567890123456789-0--u0",
			true,
		},
		{
			"long name",
			"0",
			longString,
			LoadBalancerName("0-01234567890123456789012345678901234567890123456789--uid1"),
			"%s-tp-0-01234567890123456789012345678901234567890123456789--u0",
			"%s-tps-0-01234567890123456789012345678901234567890123456789--0",
			"%s-ssl-8f3d42933afb5d1c-%s--uid1",
			"%s-fw-0-01234567890123456789012345678901234567890123456789--u0",
			"%s-fws-0-01234567890123456789012345678901234567890123456789--0",
			"%s-um-0-01234567890123456789012345678901234567890123456789--u0",
			true,
		},
		{
			"long name and namespace",
			longString,
			longString,
			LoadBalancerName("01234567890123456789012345678901234567890123456789-012345678900"),
			"%s-tp-01234567890123456789012345678901234567890123456789-01230",
			"%s-tps-01234567890123456789012345678901234567890123456789-0120",
			"%s-ssl-a04f7492b36aeb20-%s--uid1",
			"%s-fw-01234567890123456789012345678901234567890123456789-01230",
			"%s-fws-01234567890123456789012345678901234567890123456789-0120",
			"%s-um-01234567890123456789012345678901234567890123456789-01230",
			true,
		},
		{
			"invalid name with dot character",
			"namespace",
			"test.name",
			LoadBalancerName("namespace-test.name--uid1"),
			"%s-tp-namespace-test.name--uid1",
			"%s-tps-namespace-test.name--uid1",
			"%s-ssl-574f887d47579c4a-%s--uid1",
			"%s-fw-namespace-test.name--uid1",
			"%s-fws-namespace-test.name--uid1",
			"%s-um-namespace-test.name--uid1",
			false,
		},
	}
	for _, prefix := range []string{"k8s", "xyz"} {
		oldNamer := NewNamerWithPrefix(prefix, clusterUID, "")
		secretHash := fmt.Sprintf("%x", sha256.Sum256([]byte("test123")))[:16]
		for _, tc := range testCases {
			tc.desc = fmt.Sprintf("%s prefix %s", tc.desc, prefix)
			t.Run(tc.desc, func(t *testing.T) {
				key := fmt.Sprintf("%s/%s", tc.namespace, tc.name)
				t.Logf("Ingress key %s", key)
				namer := newV1IngressFrontendNamerForLoadBalancer(oldNamer.LoadBalancer(key), oldNamer)
				tc.targetHTTPProxy = fmt.Sprintf(tc.targetHTTPProxy, prefix)
				tc.targetHTTPSProxy = fmt.Sprintf(tc.targetHTTPSProxy, prefix)
				tc.sslCert = fmt.Sprintf(tc.sslCert, prefix, secretHash)
				tc.forwardingRuleHTTP = fmt.Sprintf(tc.forwardingRuleHTTP, prefix)
				tc.forwardingRuleHTTPS = fmt.Sprintf(tc.forwardingRuleHTTPS, prefix)
				tc.urlMap = fmt.Sprintf(tc.urlMap, prefix)

				// Test behavior of V1 Namer created using load-balancer name.
				if diff := cmp.Diff(tc.lbName, namer.LoadBalancer()); diff != "" {
					t.Errorf("namer.LoadBalancer() mismatch (-want +got):\n%s", diff)
				}
				targetHTTPProxyName := namer.TargetProxy(HTTPProtocol)
				if diff := cmp.Diff(tc.targetHTTPProxy, targetHTTPProxyName); diff != "" {
					t.Errorf("namer.TargetProxy(HTTPProtocol) mismatch (-want +got):\n%s", diff)
				}
				targetHTTPSProxyName := namer.TargetProxy(HTTPSProtocol)
				if diff := cmp.Diff(tc.targetHTTPSProxy, targetHTTPSProxyName); diff != "" {
					t.Errorf("namer.TargetProxy(HTTPSProtocol) mismatch (-want +got):\n%s", diff)
				}
				sslCertName := namer.SSLCertName(secretHash)
				if diff := cmp.Diff(tc.sslCert, sslCertName); diff != "" {
					t.Errorf("namer.SSLCertName(%q) mismatch (-want +got):\n%s", secretHash, diff)
				}
				httpForwardingRuleName := namer.ForwardingRule(HTTPProtocol)
				if diff := cmp.Diff(tc.forwardingRuleHTTP, httpForwardingRuleName); diff != "" {
					t.Errorf("namer.ForwardingRule(HTTPProtocol) mismatch (-want +got):\n%s", diff)
				}
				httpsForwardingRuleName := namer.ForwardingRule(HTTPSProtocol)
				if diff := cmp.Diff(tc.forwardingRuleHTTPS, httpsForwardingRuleName); diff != "" {
					t.Errorf("namer.ForwardingRule(HTTPSProtocol) mismatch (-want +got):\n%s", diff)
				}
				urlMapName := namer.UrlMap()
				if diff := cmp.Diff(tc.urlMap, urlMapName); diff != "" {
					t.Errorf("namer.UrlMap() mismatch (-want +got):\n%s", diff)
				}
				if gotIsValidName := namer.IsValidLoadBalancer(); gotIsValidName != tc.isValidName {
					t.Errorf("IsValidLoadBalancer(%s) = %t, want %t", key, gotIsValidName, tc.isValidName)
				}

				// Ensure that V1 Namer returns same values as old namer.
				lbName := oldNamer.LoadBalancer(key)
				if diff := cmp.Diff(lbName, namer.LoadBalancer()); diff != "" {
					t.Errorf("Got diff between old and V1 namers, lbName mismatch (-want +got):\n%s", diff)
				}
				if diff := cmp.Diff(oldNamer.TargetProxy(lbName, HTTPProtocol), targetHTTPProxyName); diff != "" {
					t.Errorf("Got diff between old and V1 namers, target http proxy mismatch (-want +got):\n%s", diff)
				}
				if diff := cmp.Diff(oldNamer.TargetProxy(lbName, HTTPSProtocol), targetHTTPSProxyName); diff != "" {
					t.Errorf("Got diff between old and V1 namers, target https proxy mismatch (-want +got):\n%s", diff)
				}
				if diff := cmp.Diff(oldNamer.SSLCertName(lbName, secretHash), sslCertName); diff != "" {
					t.Errorf("Got diff between old and V1 namers, SSL cert mismatch (-want +got):\n%s", diff)
				}
				if diff := cmp.Diff(oldNamer.ForwardingRule(lbName, HTTPProtocol), httpForwardingRuleName); diff != "" {
					t.Errorf("Got diff between old and V1 namers, http forwarding rule mismatch(-want +got):\n%s", diff)
				}
				if diff := cmp.Diff(oldNamer.ForwardingRule(lbName, HTTPSProtocol), httpsForwardingRuleName); diff != "" {
					t.Errorf("Got diff between old and V1 namers, https forwarding rule mismatch (-want +got):\n%s", diff)
				}
				if diff := cmp.Diff(oldNamer.UrlMap(lbName), urlMapName); diff != "" {
					t.Errorf("Got diff between old and V1 namers, url map mismatch(-want +got):\n%s", diff)
				}

				// Ensure that V1 namer created using ingress returns same values as V1 namer created using lb name.
				namerFromIngress := newV1IngressFrontendNamer(newIngress(tc.namespace, tc.name), oldNamer)
				if diff := cmp.Diff(targetHTTPProxyName, namerFromIngress.TargetProxy(HTTPProtocol)); diff != "" {
					t.Errorf("Got diff for target http proxy (-want +got):\n%s", diff)
				}
				if diff := cmp.Diff(targetHTTPSProxyName, namerFromIngress.TargetProxy(HTTPSProtocol)); diff != "" {
					t.Errorf("Got diff for target https proxy (-want +got):\n%s", diff)
				}
				if diff := cmp.Diff(sslCertName, namerFromIngress.SSLCertName(secretHash)); diff != "" {
					t.Errorf("Got diff for SSL cert (-want +got):\n%s", diff)
				}
				if diff := cmp.Diff(httpForwardingRuleName, namerFromIngress.ForwardingRule(HTTPProtocol)); diff != "" {
					t.Errorf("Got diff for http forwarding rule (-want +got):\n%s", diff)
				}
				if diff := cmp.Diff(httpsForwardingRuleName, namerFromIngress.ForwardingRule(HTTPSProtocol)); diff != "" {
					t.Errorf("Got diff for https forwarding rule (-want +got):\n%s", diff)
				}
				if diff := cmp.Diff(urlMapName, namerFromIngress.UrlMap()); diff != "" {
					t.Errorf("Got diff url map (-want +got):\n%s", diff)
				}
				if gotIsValidName := namerFromIngress.IsValidLoadBalancer(); gotIsValidName != tc.isValidName {
					t.Errorf("IsValidLoadBalancer(%s) = %t, want %t", key, gotIsValidName, tc.isValidName)
				}
			})
		}
	}
}

// TestV2IngressFrontendNamer tests v2 frontend namer workflow.
func TestV2IngressFrontendNamer(t *testing.T) {
	longString := "01234567890123456789012345678901234567890123456789"
	testCases := []struct {
		desc      string
		namespace string
		name      string
		// Expected values.
		lbName              LoadBalancerName
		targetHTTPProxy     string
		targetHTTPSProxy    string
		sslCert             string
		forwardingRuleHTTP  string
		forwardingRuleHTTPS string
		urlMap              string
		isValidName         bool
	}{
		{
			"simple case",
			"namespace",
			"name",
			"7kpbhpki-namespace-name-uhmwf5xi",
			"%s2-tp-7kpbhpki-namespace-name-uhmwf5xi",
			"%s2-ts-7kpbhpki-namespace-name-uhmwf5xi",
			"%s2-cr-7kpbhpki-fvix6gj3ge1emsdj-%s",
			"%s2-fr-7kpbhpki-namespace-name-uhmwf5xi",
			"%s2-fs-7kpbhpki-namespace-name-uhmwf5xi",
			"%s2-um-7kpbhpki-namespace-name-uhmwf5xi",
			true,
		},
		{
			"62 characters",
			longString[:23],
			longString[:24],
			LoadBalancerName("7kpbhpki-012345678901234567-012345678901234567-hg17g9tx"),
			"%s2-tp-7kpbhpki-012345678901234567-012345678901234567-hg17g9tx",
			"%s2-ts-7kpbhpki-012345678901234567-012345678901234567-hg17g9tx",
			"%s2-cr-7kpbhpki-ktiggo5yie4uh72b-%s",
			"%s2-fr-7kpbhpki-012345678901234567-012345678901234567-hg17g9tx",
			"%s2-fs-7kpbhpki-012345678901234567-012345678901234567-hg17g9tx",
			"%s2-um-7kpbhpki-012345678901234567-012345678901234567-hg17g9tx",
			true,
		},
		{
			"63 characters",
			longString[:24],
			longString[:24],
			LoadBalancerName("7kpbhpki-012345678901234567-012345678901234567-o0dahbae"),
			"%s2-tp-7kpbhpki-012345678901234567-012345678901234567-o0dahbae",
			"%s2-ts-7kpbhpki-012345678901234567-012345678901234567-o0dahbae",
			"%s2-cr-7kpbhpki-kk38dnbt6k8zrg76-%s",
			"%s2-fr-7kpbhpki-012345678901234567-012345678901234567-o0dahbae",
			"%s2-fs-7kpbhpki-012345678901234567-012345678901234567-o0dahbae",
			"%s2-um-7kpbhpki-012345678901234567-012345678901234567-o0dahbae",
			true,
		},
		{
			"64 characters",
			longString[:24],
			longString[:25],
			LoadBalancerName("7kpbhpki-012345678901234567-012345678901234567-sxo4pxda"),
			"%s2-tp-7kpbhpki-012345678901234567-012345678901234567-sxo4pxda",
			"%s2-ts-7kpbhpki-012345678901234567-012345678901234567-sxo4pxda",
			"%s2-cr-7kpbhpki-n2b7ixc007o1ddma-%s",
			"%s2-fr-7kpbhpki-012345678901234567-012345678901234567-sxo4pxda",
			"%s2-fs-7kpbhpki-012345678901234567-012345678901234567-sxo4pxda",
			"%s2-um-7kpbhpki-012345678901234567-012345678901234567-sxo4pxda",
			true,
		},
		{
			"long namespace",
			longString,
			"0",
			LoadBalancerName("7kpbhpki-012345678901234567890123456789012345--v8ajgbg3"),
			"%s2-tp-7kpbhpki-012345678901234567890123456789012345--v8ajgbg3",
			"%s2-ts-7kpbhpki-012345678901234567890123456789012345--v8ajgbg3",
			"%s2-cr-7kpbhpki-m6a592dazogk94ra-%s",
			"%s2-fr-7kpbhpki-012345678901234567890123456789012345--v8ajgbg3",
			"%s2-fs-7kpbhpki-012345678901234567890123456789012345--v8ajgbg3",
			"%s2-um-7kpbhpki-012345678901234567890123456789012345--v8ajgbg3",
			true,
		},
		{
			"long name",
			"0",
			longString,
			LoadBalancerName("7kpbhpki-0-01234567890123456789012345678901234-fyhus2f6"),
			"%s2-tp-7kpbhpki-0-01234567890123456789012345678901234-fyhus2f6",
			"%s2-ts-7kpbhpki-0-01234567890123456789012345678901234-fyhus2f6",
			"%s2-cr-7kpbhpki-a33x986k79kbu0me-%s",
			"%s2-fr-7kpbhpki-0-01234567890123456789012345678901234-fyhus2f6",
			"%s2-fs-7kpbhpki-0-01234567890123456789012345678901234-fyhus2f6",
			"%s2-um-7kpbhpki-0-01234567890123456789012345678901234-fyhus2f6",
			true,
		},
		{
			"long name and namespace",
			longString,
			longString,
			LoadBalancerName("7kpbhpki-012345678901234567-012345678901234567-69z4wrm0"),
			"%s2-tp-7kpbhpki-012345678901234567-012345678901234567-69z4wrm0",
			"%s2-ts-7kpbhpki-012345678901234567-012345678901234567-69z4wrm0",
			"%s2-cr-7kpbhpki-5pu4c55s4c47rr9e-%s",
			"%s2-fr-7kpbhpki-012345678901234567-012345678901234567-69z4wrm0",
			"%s2-fs-7kpbhpki-012345678901234567-012345678901234567-69z4wrm0",
			"%s2-um-7kpbhpki-012345678901234567-012345678901234567-69z4wrm0",
			true,
		},
		{
			"invalid name with dot character",
			"namespace",
			"test.name",
			LoadBalancerName("7kpbhpki-namespace-test.name-j4wkfxzl"),
			"%s2-tp-7kpbhpki-namespace-test.name-j4wkfxzl",
			"%s2-ts-7kpbhpki-namespace-test.name-j4wkfxzl",
			"%s2-cr-7kpbhpki-2w95dxzgz98fxgw5-%s",
			"%s2-fr-7kpbhpki-namespace-test.name-j4wkfxzl",
			"%s2-fs-7kpbhpki-namespace-test.name-j4wkfxzl",
			"%s2-um-7kpbhpki-namespace-test.name-j4wkfxzl",
			false,
		},
	}
	for _, prefix := range []string{"k8s", "xyz"} {
		oldNamer := NewNamerWithPrefix(prefix, clusterUID, "")
		secretHash := fmt.Sprintf("%x", sha256.Sum256([]byte("test123")))[:16]
		for _, tc := range testCases {
			tc.desc = fmt.Sprintf("%s namespaceLength %d nameLength %d prefix %s", tc.desc, len(tc.namespace), len(tc.name), prefix)
			t.Run(tc.desc, func(t *testing.T) {
				ing := newIngress(tc.namespace, tc.name)
				namer := newV2IngressFrontendNamer(ing, kubeSystemUID, oldNamer.prefix)
				tc.targetHTTPProxy = fmt.Sprintf(tc.targetHTTPProxy, prefix)
				tc.targetHTTPSProxy = fmt.Sprintf(tc.targetHTTPSProxy, prefix)
				tc.sslCert = fmt.Sprintf(tc.sslCert, prefix, secretHash)
				tc.forwardingRuleHTTP = fmt.Sprintf(tc.forwardingRuleHTTP, prefix)
				tc.forwardingRuleHTTPS = fmt.Sprintf(tc.forwardingRuleHTTPS, prefix)
				tc.urlMap = fmt.Sprintf(tc.urlMap, prefix)

				// Test behavior of v2 Namer.
				if diff := cmp.Diff(tc.lbName, namer.LoadBalancer()); diff != "" {
					t.Errorf("namer.LoadBalancer() mismatch (-want +got):\n%s", diff)
				}
				name := namer.TargetProxy(HTTPProtocol)
				if diff := cmp.Diff(tc.targetHTTPProxy, name); diff != "" {
					t.Errorf("namer.TargetProxy(HTTPProtocol) mismatch (-want +got):\n%s", diff)
				}
				name = namer.TargetProxy(HTTPSProtocol)
				if diff := cmp.Diff(tc.targetHTTPSProxy, name); diff != "" {
					t.Errorf("namer.TargetProxy(HTTPSProtocol) mismatch (-want +got):\n%s", diff)
				}
				name = namer.SSLCertName(secretHash)
				if diff := cmp.Diff(tc.sslCert, name); diff != "" {
					t.Errorf("namer.SSLCertName(%q) mismatch (-want +got):\n%s", secretHash, diff)
				}
				name = namer.ForwardingRule(HTTPProtocol)
				if diff := cmp.Diff(tc.forwardingRuleHTTP, name); diff != "" {
					t.Errorf("namer.ForwardingRule(HTTPProtocol) mismatch (-want +got):\n%s", diff)
				}
				name = namer.ForwardingRule(HTTPSProtocol)
				if diff := cmp.Diff(tc.forwardingRuleHTTPS, name); diff != "" {
					t.Errorf("namer.ForwardingRule(HTTPSProtocol) mismatch (-want +got):\n%s", diff)
				}
				name = namer.UrlMap()
				if diff := cmp.Diff(tc.urlMap, name); diff != "" {
					t.Errorf("namer.UrlMap() mismatch (-want +got):\n%s", diff)
				}
				if gotIsValidName := namer.IsValidLoadBalancer(); gotIsValidName != tc.isValidName {
					t.Errorf("namer.IsValidLoadBalancer() = %t, want %t", gotIsValidName, tc.isValidName)
				}
			})
		}
	}
}
