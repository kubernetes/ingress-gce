/*
Copyright 2017 The Kubernetes Authors.

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

package utils

import (
	"crypto/sha256"
	"fmt"
	"testing"
)

const (
	clusterId = "0123456789abcdef"
)

var (
	longString string
)

func init() {
	for i := 0; i < 100; i++ {
		longString += "x"
	}
}

func TestTruncate(t *testing.T) {
	for i := 0; i < len(longString); i++ {
		s := truncate(longString[:i])
		if len(s) > 63 {
			t.Errorf("truncate(longString[:%v]) = %q, length was greater than 63", i, s)
		}
	}
}

func TestNamerUID(t *testing.T) {
	const uid = "cluster-uid"
	namer := NewNamer(uid, "cluster-fw")
	if namer.UID() != uid {
		t.Errorf("namer.UID() = %q, want %q", namer.UID(), uid)
	}

	for _, tc := range []struct {
		uidToSet string
		want     string
	}{
		{"", ""},
		{"--", ""},
		{"--my-uid", "my-uid"},
		{"xxx--yyyyy", "yyyyy"},
		{"xxx--yyyyy--zzz", "zzz"},
		{"xxx--yyyyy--zzz--abc", "abc"},
	} {
		namer.SetUID(tc.uidToSet)
		if namer.UID() != tc.want {
			t.Errorf("namer.UID() = %q, want %q", namer.UID(), tc.want)
		}
	}
}

func TestNamerFirewall(t *testing.T) {
	const uid = "cluster-uid"
	const fw1 = "fw1"
	namer := NewNamer(uid, fw1)
	if namer.Firewall() != fw1 {
		t.Errorf("namer.Firewall() = %q, want %q", namer.Firewall(), fw1)
	}

	namer = NewNamer(uid, "")
	if namer.Firewall() != uid {
		t.Errorf("when initial firewall is empty, namer.Firewall() = %q, want %q", namer.Firewall(), uid)
	}

	const fw2 = "fw2"
	namer.SetFirewall(fw2)
	if namer.Firewall() != fw2 {
		t.Errorf("namer.Firewall() = %q, want %q", namer.Firewall(), fw2)
	}
}

func TestNamerParseName(t *testing.T) {
	const uid = "uid1"
	namer := NewNamer(uid, "fw1")
	lbName := namer.LoadBalancer("key1")
	certPrefix := namer.SSLCertPrefix("key1")
	secretHash := fmt.Sprintf("%x", sha256.Sum256([]byte("test123")))[:16]
	for _, tc := range []struct {
		in   string
		want *NameComponents
	}{
		{"", &NameComponents{}}, // TODO: this should really be a parse error.
		{namer.Backend(80), &NameComponents{uid, "be", ""}},
		{namer.InstanceGroup(), &NameComponents{uid, "ig", ""}},
		{namer.TargetProxy(lbName, HTTPProtocol), &NameComponents{uid, "tp", ""}},
		{namer.TargetProxy(lbName, HTTPSProtocol), &NameComponents{uid, "tps", ""}},
		{namer.SSLCertName(certPrefix, secretHash), &NameComponents{uid, "ssl", ""}},
		{namer.SSLCertName(certPrefix, secretHash), &NameComponents{uid, "ssl", ""}},
		{namer.ForwardingRule(lbName, HTTPProtocol), &NameComponents{uid, "fw", ""}},
		{namer.ForwardingRule(lbName, HTTPSProtocol), &NameComponents{uid, "fws", ""}},
		{namer.UrlMap(lbName), &NameComponents{uid, "um", ""}},
	} {
		nc := namer.ParseName(tc.in)
		if *nc != *tc.want {
			t.Errorf("namer.ParseName(%q) = %+v, want %+v", tc.in, nc, *tc.want)
		}
	}
}

func TestNameBelongsToCluster(t *testing.T) {
	const uid = "uid1"
	secretHash := fmt.Sprintf("%x", sha256.Sum256([]byte("test123")))[:16]

	for _, prefix := range []string{defaultPrefix, "mci"} {
		namer := NewNamerWithPrefix(prefix, uid, "fw1")
		lbName := namer.LoadBalancer("key1")
		certPrefix := namer.SSLCertPrefix("key1")
		// Positive cases.
		for _, tc := range []string{
			namer.Backend(80),
			namer.InstanceGroup(),
			namer.TargetProxy(lbName, HTTPProtocol),
			namer.TargetProxy(lbName, HTTPSProtocol),
			namer.SSLCertName(certPrefix, secretHash),
			namer.SSLCertName(certPrefix, secretHash),
			namer.ForwardingRule(lbName, HTTPProtocol),
			namer.ForwardingRule(lbName, HTTPSProtocol),
			namer.UrlMap(lbName),
		} {
			if !namer.NameBelongsToCluster(tc) {
				t.Errorf("namer.NameBelongsToCluster(%q) = false, want true", tc)
			}
		}
	}

	// Negative cases.
	namer := NewNamer(uid, "fw1")
	for _, tc := range []string{"", "invalid", "not--the-right-uid"} {
		if namer.NameBelongsToCluster(tc) {
			t.Errorf("namer.NameBelongsToCluster(%q) = true, want false", tc)
		}
	}
}

func TestNamerBackend(t *testing.T) {
	for _, tc := range []struct {
		desc string
		uid  string
		port int64
		want string
	}{
		{desc: "default", port: 80, want: "k8s-be-80--uid1"},
		{desc: "uid", uid: "uid2", port: 80, want: "k8s-be-80--uid2"},
		{desc: "port", port: 8080, want: "k8s-be-8080--uid1"},
		{
			desc: "truncation",
			uid:  longString,
			port: 8080,
			want: "k8s-be-8080--xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx0",
		},
	} {
		namer := NewNamer("uid1", "fw1")
		if tc.uid != "" {
			namer.SetUID(tc.uid)
		}
		name := namer.Backend(tc.port)
		if name != tc.want {
			t.Errorf("%s: namer.Backend() = %q, want %q", tc.desc, name, tc.want)
		}
	}
	// Prefix.
	namer := NewNamerWithPrefix("mci", "uid1", "fw1")
	name := namer.Backend(80)
	const want = "mci-be-80--uid1"
	if name != want {
		t.Errorf("with prefix = %q, namer.Backend(80) = %q, want %q", "mci", name, want)
	}
}

func TestBackendPort(t *testing.T) {
	namer := NewNamer("uid1", "fw1")
	for _, tc := range []struct {
		in    string
		port  string
		valid bool
	}{
		{"", "", false},
		{"k8s-be-80--uid1", "80", true},
		{"k8s-be-8080--uid1", "8080", true},
		{"k8s-be-port1--uid1", "8080", false},
	} {
		port, err := namer.BackendPort(tc.in)
		if err != nil {
			if tc.valid {
				t.Errorf("namer.BackendPort(%q) = _, %v, want _, nil", tc.in, err)
			}
			continue
		}
		if !tc.valid {
			t.Errorf("namer.BackendPort(%q) = _, nil, want error", tc.in)
			continue
		}
		if port != tc.port {
			t.Errorf("namer.BackendPort(%q) = %q, nil, want %q, nil", tc.in, port, tc.port)
		}
	}
}

func TestIsSSLCert(t *testing.T) {
	for _, tc := range []struct {
		prefix string
		in     string
		want   bool
	}{
		{defaultPrefix, "", false},
		{defaultPrefix, "k8s-ssl-foo--uid", true},
		{defaultPrefix, "k8s-tp-foo--uid", false},
		{"mci", "mci-ssl-foo--uid", true},
	} {
		namer := NewNamerWithPrefix(tc.prefix, "uid", "fw")
		res := namer.IsLegacySSLCert("foo", tc.in)
		if res != tc.want {
			t.Errorf("with prefix = %q, namer.IsLegacySSLCert(%q) = %v, want %v", tc.prefix, tc.in, res, tc.want)
		}
	}
}

func TestNamedPort(t *testing.T) {
	namer := NewNamer("uid1", "fw1")
	name := namer.NamedPort(80)
	const want = "port80"
	if name != want {
		t.Errorf("namer.NamedPort(80) = %q, want %q", name, want)
	}
}

func TestNamerInstanceGroup(t *testing.T) {
	namer := NewNamer("uid1", "fw1")
	name := namer.InstanceGroup()
	if name != "k8s-ig--uid1" {
		t.Errorf("namer.InstanceGroup() = %q, want %q", name, "k8s-ig--uid1")
	}
	// Prefix.
	namer = NewNamerWithPrefix("mci", "uid1", "fw1")
	name = namer.InstanceGroup()
	if name != "mci-ig--uid1" {
		t.Errorf("namer.InstanceGroup() = %q, want %q", name, "mci-ig--uid1")
	}
}

func TestNamerFirewallRule(t *testing.T) {
	namer := NewNamer("uid1", "fw1")
	name := namer.FirewallRule()
	if name != "k8s-fw-l7--fw1" {
		t.Errorf("namer.FirewallRule() = %q, want %q", name, "k8s-fw-l7--fw1")
	}
}

func TestNamerLoadBalancer(t *testing.T) {
	secretHash := fmt.Sprintf("%x", sha256.Sum256([]byte("test123")))[:16]
	// namespaceHash is calculated the same way as cert hash
	namespaceHash := fmt.Sprintf("%x", sha256.Sum256([]byte("key1")))[:16]
	for _, tc := range []struct {
		prefix string

		lbName              string
		targetHTTPProxy     string
		targetHTTPSProxy    string
		sslCert             string
		forwardingRuleHTTP  string
		forwardingRuleHTTPS string
		urlMap              string
	}{
		{
			"k8s",
			"key1--uid1",
			"k8s-tp-key1--uid1",
			"k8s-tps-key1--uid1",
			fmt.Sprintf("k8s-ssl-%s--uid1-%s", namespaceHash, secretHash),
			"k8s-fw-key1--uid1",
			"k8s-fws-key1--uid1",
			"k8s-um-key1--uid1",
		},
		{
			"mci",
			"key1--uid1",
			"mci-tp-key1--uid1",
			"mci-tps-key1--uid1",
			fmt.Sprintf("mci-ssl-%s--uid1-%s", namespaceHash, secretHash),
			"mci-fw-key1--uid1",
			"mci-fws-key1--uid1",
			"mci-um-key1--uid1",
		},
	} {
		namer := NewNamerWithPrefix(tc.prefix, "uid1", "fw1")
		lbName := namer.LoadBalancer("key1")
		certPrefix := namer.SSLCertPrefix("key1")
		if lbName != tc.lbName {
			t.Errorf("lbName = %q, want %q", lbName, "key1--uid1")
		}
		var name string
		name = namer.TargetProxy(lbName, HTTPProtocol)
		if name != tc.targetHTTPProxy {
			t.Errorf("namer.TargetProxy(%q, HTTPProtocol) = %q, want %q", lbName, name, tc.targetHTTPProxy)
		}
		name = namer.TargetProxy(lbName, HTTPSProtocol)
		if name != tc.targetHTTPSProxy {
			t.Errorf("namer.TargetProxy(%q, HTTPSProtocol) = %q, want %q", lbName, name, tc.targetHTTPSProxy)
		}
		name = namer.SSLCertName(certPrefix, secretHash)
		if name != tc.sslCert {
			t.Errorf("namer.SSLCertName(%q, true) = %q, want %q", lbName, name, tc.sslCert)
		}
		name = namer.ForwardingRule(lbName, HTTPProtocol)
		if name != tc.forwardingRuleHTTP {
			t.Errorf("namer.ForwardingRule(%q, HTTPProtocol) = %q, want %q", lbName, name, tc.forwardingRuleHTTP)
		}
		name = namer.ForwardingRule(lbName, HTTPSProtocol)
		if name != tc.forwardingRuleHTTPS {
			t.Errorf("namer.ForwardingRule(%q, HTTPSProtocol) = %q, want %q", lbName, name, tc.forwardingRuleHTTPS)
		}
		name = namer.UrlMap(lbName)
		if name != tc.urlMap {
			t.Errorf("namer.UrlMap(%q) = %q, want %q", lbName, name, tc.urlMap)
		}
	}
}

func TestNamerNEG(t *testing.T) {
	longstring := "01234567890123456789012345678901234567890123456789"
	testCases := []struct {
		desc      string
		namespace string
		name      string
		port      string
		expect    string
	}{
		{
			"simple case",
			"namespace",
			"name",
			"80",
			"k8s1-0123456789abcdef-namespace-name-80-1e047e33",
		},
		{
			"63 characters",
			longstring[:10],
			longstring[:10],
			longstring[:10],
			"k8s1-0123456789abcdef-0123456789-0123456789-0123456789-4f7223eb",
		},
		{
			"long namespace",
			longstring,
			"0",
			"0",
			"k8s1-0123456789abcdef-01234567890123456789012345678-0--44255b67",
		},

		{
			"long name and namespace",
			longstring,
			longstring,
			"0",
			"k8s1-0123456789abcdef-012345678901234-012345678901234--525cce3d",
		},
		{
			" long name, namespace and port",
			longstring,
			longstring[:40],
			longstring[:30],
			"k8s1-0123456789abcdef-0123456789012-0123456789-0123456-71877a60",
		},
	}

	namer := NewNamer(clusterId, "")
	for _, tc := range testCases {
		res := namer.NEG(tc.namespace, tc.name, tc.port)
		if len(res) > 63 {
			t.Errorf("%s: got len(res) == %v, want <= 63", tc.desc, len(res))
		}
		if res != tc.expect {
			t.Errorf("%s: got %q, want %q", tc.desc, res, tc.expect)
		}
	}

	// Different prefix.
	namer = NewNamerWithPrefix("mci", clusterId, "fw")
	name := namer.NEG("ns", "svc", "port")
	const want = "mci1-0123456789abcdef-ns-svc-port-16c06497"
	if name != want {
		t.Errorf(`with prefix %q, namer.NEG("ns", "svc", 80) = %q, want %q`, "mci", name, want)
	}
}

func TestIsNEG(t *testing.T) {
	for _, tc := range []struct {
		prefix string
		in     string
		want   bool
	}{
		{defaultPrefix, "", false},
		{defaultPrefix, "k8s-tp-key1--uid1", false},
		{defaultPrefix, "k8s1-uid1-namespace-name-80-1e047e33", true},
		{"mci", "mci1-uid1-ns-svc-port-16c06497", true},
	} {
		namer := NewNamerWithPrefix(tc.prefix, "uid1", "fw1")
		res := namer.IsNEG(tc.in)
		if res != tc.want {
			t.Errorf("with prefix %q, namer.IsNEG(%q) = %v, want %v", tc.prefix, tc.in, res, tc.want)
		}
	}
}
