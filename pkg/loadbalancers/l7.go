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
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/golang/glog"

	compute "google.golang.org/api/compute/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	"crypto/sha256"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/backends"
	"k8s.io/ingress-gce/pkg/utils"
)

const (
	// The gce api uses the name of a path rule to match a host rule.
	hostRulePrefix = "host"

	// DefaultHost is the host used if none is specified. It is a valid value
	// for the "Host" field recognized by GCE.
	DefaultHost = "*"

	// DefaultPath is the path used if none is specified. It is a valid path
	// recognized by GCE.
	DefaultPath = "/*"

	httpDefaultPortRange  = "80-80"
	httpsDefaultPortRange = "443-443"

	// Every target https proxy accepts upto 10 ssl certificates.
	TargetProxyCertLimit = 10
)

// TLSCerts encapsulates .pem encoded TLS information.
type TLSCerts struct {
	// Key is private key.
	Key string
	// Cert is a public key.
	Cert string
	// Chain is a certificate chain.
	Chain string
	Name  string
	// md5 hash(first 8 bytes) of the cert contents
	CertHash string
}

// L7RuntimeInfo is info passed to this module from the controller runtime.
type L7RuntimeInfo struct {
	// Name is the name of a loadbalancer.
	Name string
	// IP is the desired ip of the loadbalancer, eg from a staticIP.
	IP string
	// TLS are the tls certs to use in termination.
	TLS []*TLSCerts
	// TLSName is the name of the preshared cert to use. Multiple certs can be specified as a comma-separated string
	TLSName string
	// AllowHTTP will not setup :80, if TLS is nil and AllowHTTP is set,
	// no loadbalancer is created.
	AllowHTTP bool
	// The name of a Global Static IP. If specified, the IP associated with
	// this name is used in the Forwarding Rules for this loadbalancer.
	StaticIPName string
}

// String returns the load balancer name
func (l *L7RuntimeInfo) String() string {
	return l.Name
}

// L7 represents a single L7 loadbalancer.
type L7 struct {
	Name string
	// runtimeInfo is non-cloudprovider information passed from the controller.
	runtimeInfo *L7RuntimeInfo
	// cloud is an interface to manage loadbalancers in the GCE cloud.
	cloud LoadBalancers
	// um is the UrlMap associated with this L7.
	um *compute.UrlMap
	// tp is the TargetHTTPProxy associated with this L7.
	tp *compute.TargetHttpProxy
	// tps is the TargetHTTPSProxy associated with this L7.
	tps *compute.TargetHttpsProxy
	// fw is the GlobalForwardingRule that points to the TargetHTTPProxy.
	fw *compute.ForwardingRule
	// fws is the GlobalForwardingRule that points to the TargetHTTPSProxy.
	fws *compute.ForwardingRule
	// ip is the static-ip associated with both GlobalForwardingRules.
	ip *compute.Address
	// prefix to use in ssl cert names
	sslCertPrefix string
	// sslCerts is the list of ssl certs associated with the targetHTTPSProxy.
	sslCerts []*compute.SslCertificate
	// oldSSLCerts is the list of certs that used to be hooked up to the
	// targetHTTPSProxy. We can't update a cert in place, so we need
	// to create - update - delete and storing the old certs in a list
	// prevents leakage if there's a failure along the way.
	oldSSLCerts []*compute.SslCertificate
	// glbcDefaultBacked is the backend to use if no path rules match.
	// TODO: Expose this to users.
	glbcDefaultBackend *compute.BackendService
	// namer is used to compute names of the various sub-components of an L7.
	namer *utils.Namer
}

// UrlMap returns the UrlMap associated with the L7 load balancer.
func (l *L7) UrlMap() *compute.UrlMap {
	return l.um
}

func (l *L7) checkUrlMap(backend *compute.BackendService) (err error) {
	if l.glbcDefaultBackend == nil {
		return fmt.Errorf("cannot create urlmap without default backend")
	}
	urlMapName := l.namer.UrlMap(l.Name)
	urlMap, _ := l.cloud.GetUrlMap(urlMapName)
	if urlMap != nil {
		glog.V(3).Infof("Url map %v already exists", urlMap.Name)
		l.um = urlMap
		return nil
	}

	glog.Infof("Creating url map %v for backend %v", urlMapName, l.glbcDefaultBackend.Name)
	newUrlMap := &compute.UrlMap{
		Name:           urlMapName,
		DefaultService: l.glbcDefaultBackend.SelfLink,
	}
	if err = l.cloud.CreateUrlMap(newUrlMap); err != nil {
		return err
	}
	urlMap, err = l.cloud.GetUrlMap(urlMapName)
	if err != nil {
		return err
	}
	l.um = urlMap
	return nil
}

func (l *L7) checkProxy() (err error) {
	if l.um == nil {
		return fmt.Errorf("cannot create proxy without urlmap")
	}
	proxyName := l.namer.TargetProxy(l.Name, utils.HTTPProtocol)
	proxy, _ := l.cloud.GetTargetHttpProxy(proxyName)
	if proxy == nil {
		glog.Infof("Creating new http proxy for urlmap %v", l.um.Name)
		newProxy := &compute.TargetHttpProxy{
			Name:   proxyName,
			UrlMap: l.um.SelfLink,
		}
		if err = l.cloud.CreateTargetHttpProxy(newProxy); err != nil {
			return err
		}
		proxy, err = l.cloud.GetTargetHttpProxy(proxyName)
		if err != nil {
			return err
		}
		l.tp = proxy
		return nil
	}
	if !utils.CompareLinks(proxy.UrlMap, l.um.SelfLink) {
		glog.Infof("Proxy %v has the wrong url map, setting %v overwriting %v",
			proxy.Name, l.um, proxy.UrlMap)
		if err := l.cloud.SetUrlMapForTargetHttpProxy(proxy, l.um); err != nil {
			return err
		}
	}
	l.tp = proxy
	return nil
}

func (l *L7) deleteOldSSLCerts() (err error) {
	if len(l.oldSSLCerts) == 0 {
		return nil
	}
	certsMap := getMapfromCertList(l.sslCerts)
	for _, cert := range l.oldSSLCerts {
		if !l.IsSSLCert(cert.Name) && !l.namer.IsLegacySSLCert(cert.Name) {
			// retain cert if it is managed by GCE(non-ingress)
			continue
		}
		if _, ok := certsMap[cert.Name]; ok {
			// cert found in current map
			continue
		}
		glog.Infof("Cleaning up old SSL Certificate %s", cert.Name)
		if certErr := utils.IgnoreHTTPNotFound(l.cloud.DeleteSslCertificate(cert.Name)); certErr != nil {
			glog.Errorf("Old cert delete failed - %v", certErr)
			err = certErr
		}
	}
	return err
}

// Returns the name portion of a link - which is the last section
func getResourceNameFromLink(link string) string {
	s := strings.Split(link, "/")
	if len(s) == 0 {
		return ""
	}
	return s[len(s)-1]
}

func (l *L7) usePreSharedCert() (bool, error) {
	// Use the named GCE cert when it is specified by the annotation.
	preSharedCertName := l.runtimeInfo.TLSName
	if preSharedCertName == "" {
		return false, nil
	}
	preSharedCerts := strings.Split(preSharedCertName, ",")
	if len(preSharedCerts) > TargetProxyCertLimit {
		glog.Warningf("Specified %d preshared certs, limit is %d, rest will be ignored",
			len(preSharedCerts), TargetProxyCertLimit)
		preSharedCerts = preSharedCerts[:TargetProxyCertLimit]
	}

	l.sslCerts = make([]*compute.SslCertificate, 0, len(preSharedCerts))
	var failedCerts []string

	for _, sharedCert := range preSharedCerts {
		// Ask GCE for the cert, checking for problems and existence.
		sharedCert = strings.TrimSpace(sharedCert)
		cert, err := l.cloud.GetSslCertificate(sharedCert)
		if err != nil {
			failedCerts = append(failedCerts, sharedCert+" Error: "+err.Error())
			continue
		}
		if cert == nil {
			failedCerts = append(failedCerts, sharedCert+" Error: unable to find existing sslCertificate")
			continue
		}

		glog.V(2).Infof("Using existing sslCertificate %v for %v", sharedCert, l.Name)
		l.sslCerts = append(l.sslCerts, cert)
	}
	if len(failedCerts) != 0 {
		return true, fmt.Errorf("PreSharedCert errors - %s", strings.Join(failedCerts, ","))
	}
	return true, nil
}

// IsSSLCert returns true if name is ingress managed, specifically by this loadbalancer instance
func (l *L7) IsSSLCert(name string) bool {
	return strings.HasPrefix(name, l.sslCertPrefix)
}

func getMapfromCertList(certs []*compute.SslCertificate) map[string]*compute.SslCertificate {
	if len(certs) == 0 {
		return nil
	}
	certMap := make(map[string]*compute.SslCertificate)
	for _, cert := range certs {
		certMap[cert.Name] = cert
	}
	return certMap
}

func (l *L7) populateSSLCert() error {
	l.sslCerts = make([]*compute.SslCertificate, 0)
	// Currently we list all certs available in gcloud and filter the ones managed by this loadbalancer instance. This is
	// to make sure we garbage collect any old certs that this instance might have lost track of due to crashes.
	// Can be a performance issue if there are too many global certs, default quota is only 10.
	certs, err := l.cloud.ListSslCertificates()
	if err != nil {
		return utils.IgnoreHTTPNotFound(err)
	}
	for _, c := range certs {
		if l.IsSSLCert(c.Name) {
			glog.Infof("Populating ssl cert %s for l7 %s", c.Name, l.Name)
			l.sslCerts = append(l.sslCerts, c)
		}
	}
	if len(l.sslCerts) == 0 {
		// Check for legacy cert since that follows a different naming convention
		glog.Infof("Looking for legacy ssl certs")
		expectedCertNames := l.getSslCertLinkInUse()
		for _, link := range expectedCertNames {
			// Retrieve the certificate and ignore error if certificate wasn't found
			name := getResourceNameFromLink(link)
			if !l.namer.IsLegacySSLCert(name) {
				continue
			}
			cert, _ := l.cloud.GetSslCertificate(getResourceNameFromLink(name))
			if cert != nil {
				glog.Infof("Populating legacy ssl cert %s for l7 %s", cert.Name, l.Name)
				l.sslCerts = append(l.sslCerts, cert)
			}
		}
	}
	return nil
}

func (l *L7) checkSSLCert() error {
	// Handle Pre-Shared cert and early return if used
	if used, err := l.usePreSharedCert(); used {
		return err
	}

	// Get updated value of certificate for comparison
	if err := l.populateSSLCert(); err != nil {
		return err
	}

	var newCerts []*compute.SslCertificate
	// mapping of currently configured certs
	certsMap := getMapfromCertList(l.sslCerts)
	var failedCerts []string

	for _, tlsCert := range l.runtimeInfo.TLS {
		ingCert := tlsCert.Cert
		ingKey := tlsCert.Key
		newCertName := l.namer.SSLCertName(l.sslCertPrefix, tlsCert.CertHash)

		// PrivateKey is write only, so compare certs alone. We're assuming that
		// no one will change just the key. We can remember the key and compare,
		// but a bug could end up leaking it, which feels worse.
		// If the cert contents have changed, its hash would be different, so would be the cert name. So it is enough
		// to check if this cert name exists in the map.
		if certsMap != nil {
			if cert, ok := certsMap[newCertName]; ok {
				glog.Infof("Retaining cert - %s", tlsCert.Name)
				newCerts = append(newCerts, cert)
				continue
			}
		}
		// Controller needs to create the certificate, no need to check if it exists and delete. If it did exist, it
		// would have been listed in the populateSSLCert function and matched in the check above.
		glog.V(2).Infof("Creating new sslCertificate %v for %v", newCertName, l.Name)
		cert, err := l.cloud.CreateSslCertificate(&compute.SslCertificate{
			Name:        newCertName,
			Certificate: ingCert,
			PrivateKey:  ingKey,
		})
		if err != nil {
			glog.Errorf("Failed to create new sslCertificate %v for %v - %s", newCertName, l.Name, err)
			failedCerts = append(failedCerts, newCertName+" Error:"+err.Error())
			continue
		}
		newCerts = append(newCerts, cert)
	}

	// Save the old certs for cleanup after we update the target proxy.
	l.oldSSLCerts = l.sslCerts
	l.sslCerts = newCerts
	if len(failedCerts) > 0 {
		return fmt.Errorf("Cert creation failures - %s", strings.Join(failedCerts, ","))
	}
	return nil
}

func (l *L7) getSslCertLinkInUse() []string {
	proxyName := l.namer.TargetProxy(l.Name, utils.HTTPSProtocol)
	proxy, _ := l.cloud.GetTargetHttpsProxy(proxyName)
	if proxy != nil && len(proxy.SslCertificates) > 0 {
		return proxy.SslCertificates
	}
	return nil
}

func (l *L7) checkHttpsProxy() (err error) {
	if len(l.sslCerts) == 0 {
		glog.V(3).Infof("No SSL certificates for %v, will not create HTTPS proxy.", l.Name)
		return nil
	}
	if l.um == nil {
		return fmt.Errorf("no UrlMap for %v, will not create HTTPS proxy", l.Name)
	}

	proxyName := l.namer.TargetProxy(l.Name, utils.HTTPSProtocol)
	proxy, _ := l.cloud.GetTargetHttpsProxy(proxyName)
	if proxy == nil {
		glog.Infof("Creating new https proxy for urlmap %v", l.um.Name)
		newProxy := &compute.TargetHttpsProxy{
			Name:   proxyName,
			UrlMap: l.um.SelfLink,
		}

		for _, c := range l.sslCerts {
			newProxy.SslCertificates = append(newProxy.SslCertificates, c.SelfLink)
		}

		if err = l.cloud.CreateTargetHttpsProxy(newProxy); err != nil {
			return err
		}

		proxy, err = l.cloud.GetTargetHttpsProxy(proxyName)
		if err != nil {
			return err
		}

		l.tps = proxy
		return nil
	}
	if !utils.CompareLinks(proxy.UrlMap, l.um.SelfLink) {
		glog.Infof("Https proxy %v has the wrong url map, setting %v overwriting %v",
			proxy.Name, l.um, proxy.UrlMap)
		if err := l.cloud.SetUrlMapForTargetHttpsProxy(proxy, l.um); err != nil {
			return err
		}
	}

	if !l.compareCerts(proxy.SslCertificates) {
		glog.Infof("Https proxy %v has the wrong ssl certs, setting %v overwriting %v",
			proxy.Name, l.sslCerts, proxy.SslCertificates)
		if err := l.cloud.SetSslCertificateForTargetHttpsProxy(proxy, l.sslCerts); err != nil {
			return err
		}

	}
	glog.V(3).Infof("Created target https proxy %v", proxy.Name)
	l.tps = proxy
	return nil
}

// Returns true if the input array of certs is identical to the certs in the L7 config.
// Returns false if there is any mismatch
func (l *L7) compareCerts(certLinks []string) bool {
	certsMap := getMapfromCertList(l.sslCerts)
	if len(certLinks) != len(certsMap) {
		glog.Infof("Loadbalancer has %d certs, target proxy has %d certs", len(certsMap), len(certLinks))
		return false
	}
	var certName string
	for _, linkName := range certLinks {
		certName = getResourceNameFromLink(linkName)
		if cert, ok := certsMap[certName]; !ok {
			glog.Infof("Cannot find cert with name %s in certsMap %+v", certName, certsMap)
			return false
		} else if ok && !utils.CompareLinks(linkName, cert.SelfLink) {
			glog.Infof("Selflink compare failed for certs - %s in loadbalancer, %s in targetproxy", cert.SelfLink, linkName)
			return false
		}
	}
	return true
}

func (l *L7) checkForwardingRule(name, proxyLink, ip, portRange string) (fw *compute.ForwardingRule, err error) {
	fw, _ = l.cloud.GetGlobalForwardingRule(name)
	if fw != nil && (ip != "" && fw.IPAddress != ip || fw.PortRange != portRange) {
		glog.Warningf("Recreating forwarding rule %v(%v), so it has %v(%v)",
			fw.IPAddress, fw.PortRange, ip, portRange)
		if err = utils.IgnoreHTTPNotFound(l.cloud.DeleteGlobalForwardingRule(name)); err != nil {
			return nil, err
		}
		fw = nil
	}
	if fw == nil {
		parts := strings.Split(proxyLink, "/")
		glog.Infof("Creating forwarding rule for proxy %v and ip %v:%v", parts[len(parts)-1:], ip, portRange)
		rule := &compute.ForwardingRule{
			Name:       name,
			IPAddress:  ip,
			Target:     proxyLink,
			PortRange:  portRange,
			IPProtocol: "TCP",
		}
		if err = l.cloud.CreateGlobalForwardingRule(rule); err != nil {
			return nil, err
		}
		fw, err = l.cloud.GetGlobalForwardingRule(name)
		if err != nil {
			return nil, err
		}
	}
	// TODO: If the port range and protocol don't match, recreate the rule
	if utils.CompareLinks(fw.Target, proxyLink) {
		glog.V(3).Infof("Forwarding rule %v already exists", fw.Name)
	} else {
		glog.Infof("Forwarding rule %v has the wrong proxy, setting %v overwriting %v",
			fw.Name, fw.Target, proxyLink)
		if err := l.cloud.SetProxyForGlobalForwardingRule(fw.Name, proxyLink); err != nil {
			return nil, err
		}
	}
	return fw, nil
}

// getEffectiveIP returns a string with the IP to use in the HTTP and HTTPS
// forwarding rules, and a boolean indicating if this is an IP the controller
// should manage or not.
func (l *L7) getEffectiveIP() (string, bool) {

	// A note on IP management:
	// User specifies a different IP on startup:
	//	- We create a forwarding rule with the given IP.
	//		- If this ip doesn't exist in GCE, we create another one in the hope
	//		  that they will rectify it later on.
	//	- In the happy case, no static ip is created or deleted by this controller.
	// Controller allocates a staticIP/ephemeralIP, but user changes it:
	//  - We still delete the old static IP, but only when we tear down the
	//	  Ingress in Cleanup(). Till then the static IP stays around, but
	//    the forwarding rules get deleted/created with the new IP.
	//  - There will be a period of downtime as we flip IPs.
	// User specifies the same static IP to 2 Ingresses:
	//  - GCE will throw a 400, and the controller will keep trying to use
	//    the IP in the hope that the user manually resolves the conflict
	//    or deletes/modifies the Ingress.
	// TODO: Handle the last case better.

	if l.runtimeInfo.StaticIPName != "" {
		// Existing static IPs allocated to forwarding rules will get orphaned
		// till the Ingress is torn down.
		if ip, err := l.cloud.GetGlobalAddress(l.runtimeInfo.StaticIPName); err != nil || ip == nil {
			glog.Warningf("The given static IP name %v doesn't translate to an existing global static IP, ignoring it and allocating a new IP: %v",
				l.runtimeInfo.StaticIPName, err)
		} else {
			return ip.Address, false
		}
	}
	if l.ip != nil {
		return l.ip.Address, true
	}
	return "", true
}

func (l *L7) checkHttpForwardingRule() (err error) {
	if l.tp == nil {
		return fmt.Errorf("cannot create forwarding rule without proxy")
	}
	name := l.namer.ForwardingRule(l.Name, utils.HTTPProtocol)
	address, _ := l.getEffectiveIP()
	fw, err := l.checkForwardingRule(name, l.tp.SelfLink, address, httpDefaultPortRange)
	if err != nil {
		return err
	}
	l.fw = fw
	return nil
}

func (l *L7) checkHttpsForwardingRule() (err error) {
	if l.tps == nil {
		glog.V(3).Infof("No https target proxy for %v, not created https forwarding rule", l.Name)
		return nil
	}
	name := l.namer.ForwardingRule(l.Name, utils.HTTPSProtocol)
	address, _ := l.getEffectiveIP()
	fws, err := l.checkForwardingRule(name, l.tps.SelfLink, address, httpsDefaultPortRange)
	if err != nil {
		return err
	}
	l.fws = fws
	return nil
}

// checkStaticIP reserves a static IP allocated to the Forwarding Rule.
func (l *L7) checkStaticIP() (err error) {
	if l.fw == nil || l.fw.IPAddress == "" {
		return fmt.Errorf("will not create static IP without a forwarding rule")
	}
	// Don't manage staticIPs if the user has specified an IP.
	if address, manageStaticIP := l.getEffectiveIP(); !manageStaticIP {
		glog.V(3).Infof("Not managing user specified static IP %v", address)
		return nil
	}
	staticIPName := l.namer.ForwardingRule(l.Name, utils.HTTPProtocol)
	ip, _ := l.cloud.GetGlobalAddress(staticIPName)
	if ip == nil {
		glog.Infof("Creating static ip %v", staticIPName)
		err = l.cloud.ReserveGlobalAddress(&compute.Address{Name: staticIPName, Address: l.fw.IPAddress})
		if err != nil {
			if utils.IsHTTPErrorCode(err, http.StatusConflict) ||
				utils.IsHTTPErrorCode(err, http.StatusBadRequest) {
				glog.V(3).Infof("IP %v(%v) is already reserved, assuming it is OK to use.",
					l.fw.IPAddress, staticIPName)
				return nil
			}
			return err
		}
		ip, err = l.cloud.GetGlobalAddress(staticIPName)
		if err != nil {
			return err
		}
	}
	l.ip = ip
	return nil
}

func (l *L7) edgeHop() error {
	if err := l.checkUrlMap(l.glbcDefaultBackend); err != nil {
		return err
	}
	if l.runtimeInfo.AllowHTTP {
		if err := l.edgeHopHttp(); err != nil {
			return err
		}
	}
	// Defer promoting an ephemeral to a static IP until it's really needed.
	if l.runtimeInfo.AllowHTTP && (l.runtimeInfo.TLS != nil || l.runtimeInfo.TLSName != "") {
		glog.V(3).Infof("checking static ip for %v", l.Name)
		if err := l.checkStaticIP(); err != nil {
			return err
		}
	}
	if l.runtimeInfo.TLS != nil || l.runtimeInfo.TLSName != "" {
		glog.V(3).Infof("validating https for %v", l.Name)
		if err := l.edgeHopHttps(); err != nil {
			return err
		}
	}
	return nil
}

func (l *L7) edgeHopHttp() error {
	if err := l.checkProxy(); err != nil {
		return err
	}
	if err := l.checkHttpForwardingRule(); err != nil {
		return err
	}
	return nil
}

func (l *L7) edgeHopHttps() error {
	if err := l.checkSSLCert(); err != nil {
		return err
	}
	if err := l.checkHttpsProxy(); err != nil {
		return err
	}
	if err := l.checkHttpsForwardingRule(); err != nil {
		return err
	}
	if err := l.deleteOldSSLCerts(); err != nil {
		return err
	}
	return nil
}

// GetIP returns the ip associated with the forwarding rule for this l7.
func (l *L7) GetIP() string {
	if l.fw != nil {
		return l.fw.IPAddress
	}
	if l.fws != nil {
		return l.fws.IPAddress
	}
	return ""
}

// getNameForPathMatcher returns a name for a pathMatcher based on the given host rule.
// The host rule can be a regex, the path matcher name used to associate the 2 cannot.
func getNameForPathMatcher(hostRule string) string {
	hasher := md5.New()
	hasher.Write([]byte(hostRule))
	return fmt.Sprintf("%v%v", hostRulePrefix, hex.EncodeToString(hasher.Sum(nil)))
}

// UpdateUrlMap translates the given hostname: endpoint->port mapping into a gce url map.
//
// HostRule: Conceptually contains all PathRules for a given host.
// PathMatcher: Associates a path rule with a host rule. Mostly an optimization.
// PathRule: Maps a single path regex to a backend.
//
// The GCE url map allows multiple hosts to share url->backend mappings without duplication, eg:
//   Host: foo(PathMatcher1), bar(PathMatcher1,2)
//   PathMatcher1:
//     /a -> b1
//     /b -> b2
//   PathMatcher2:
//     /c -> b1
// This leads to a lot of complexity in the common case, where all we want is a mapping of
// host->{/path: backend}.
//
// Consider some alternatives:
// 1. Using a single backend per PathMatcher:
//   Host: foo(PathMatcher1,3) bar(PathMatcher1,2,3)
//   PathMatcher1:
//     /a -> b1
//   PathMatcher2:
//     /c -> b1
//   PathMatcher3:
//     /b -> b2
// 2. Using a single host per PathMatcher:
//   Host: foo(PathMatcher1)
//   PathMatcher1:
//     /a -> b1
//     /b -> b2
//   Host: bar(PathMatcher2)
//   PathMatcher2:
//     /a -> b1
//     /b -> b2
//     /c -> b1
// In the context of kubernetes services, 2 makes more sense, because we
// rarely want to lookup backends (service:nodeport). When a service is
// deleted, we need to find all host PathMatchers that have the backend
// and remove the mapping. When a new path is added to a host (happens
// more frequently than service deletion) we just need to lookup the 1
// pathmatcher of the host.
func (l *L7) UpdateUrlMap(ingressRules utils.GCEURLMap) error {
	if l.um == nil {
		return fmt.Errorf("cannot add url without an urlmap")
	}

	// All UrlMaps must have a default backend. If the Ingress has a default
	// backend, it applies to all host rules as well as to the urlmap itself.
	// If it doesn't the urlmap might have a stale default, so replace it with
	// glbc's default backend.
	defaultBackend := ingressRules.GetDefaultBackend()
	if defaultBackend != nil {
		l.um.DefaultService = defaultBackend.SelfLink
	} else {
		l.um.DefaultService = l.glbcDefaultBackend.SelfLink
	}

	// Every update replaces the entire urlmap.
	// TODO:  when we have multiple loadbalancers point to a single gce url map
	// this needs modification. For now, there is a 1:1 mapping of urlmaps to
	// Ingresses, so if the given Ingress doesn't have a host rule we should
	// delete the path to that backend.
	l.um.HostRules = []*compute.HostRule{}
	l.um.PathMatchers = []*compute.PathMatcher{}

	for hostname, urlToBackend := range ingressRules {
		// Create a host rule
		// Create a path matcher
		// Add all given endpoint:backends to pathRules in path matcher
		pmName := getNameForPathMatcher(hostname)
		l.um.HostRules = append(l.um.HostRules, &compute.HostRule{
			Hosts:       []string{hostname},
			PathMatcher: pmName,
		})

		pathMatcher := &compute.PathMatcher{
			Name:           pmName,
			DefaultService: l.um.DefaultService,
			PathRules:      []*compute.PathRule{},
		}

		// Longest prefix wins. For equal rules, first hit wins, i.e the second
		// /foo rule when the first is deleted.
		for expr, be := range urlToBackend {
			pathMatcher.PathRules = append(
				pathMatcher.PathRules, &compute.PathRule{Paths: []string{expr}, Service: be.SelfLink})
		}
		l.um.PathMatchers = append(l.um.PathMatchers, pathMatcher)
	}
	oldMap, _ := l.cloud.GetUrlMap(l.um.Name)
	if oldMap != nil && mapsEqual(oldMap, l.um) {
		glog.Infof("UrlMap for l7 %v is unchanged", l.Name)
		return nil
	}

	glog.V(3).Infof("Updating URLMap: %q", l.Name)
	if err := l.cloud.UpdateUrlMap(l.um); err != nil {
		return err
	}

	um, err := l.cloud.GetUrlMap(l.um.Name)
	if err != nil {
		return err
	}

	l.um = um
	return nil
}

func mapsEqual(a, b *compute.UrlMap) bool {
	if a.DefaultService != b.DefaultService {
		return false
	}
	if len(a.HostRules) != len(b.HostRules) {
		return false
	}
	for i := range a.HostRules {
		a := a.HostRules[i]
		b := b.HostRules[i]
		if a.Description != b.Description {
			return false
		}
		if len(a.Hosts) != len(b.Hosts) {
			return false
		}
		for i := range a.Hosts {
			if a.Hosts[i] != b.Hosts[i] {
				return false
			}
		}
		if a.PathMatcher != b.PathMatcher {
			return false
		}
	}
	if len(a.PathMatchers) != len(b.PathMatchers) {
		return false
	}
	for i := range a.PathMatchers {
		a := a.PathMatchers[i]
		b := b.PathMatchers[i]
		if a.DefaultService != b.DefaultService {
			return false
		}
		if a.Description != b.Description {
			return false
		}
		if a.Name != b.Name {
			return false
		}
		if len(a.PathRules) != len(b.PathRules) {
			return false
		}
		for i := range a.PathRules {
			a := a.PathRules[i]
			b := b.PathRules[i]
			if len(a.Paths) != len(b.Paths) {
				return false
			}
			for i := range a.Paths {
				if a.Paths[i] != b.Paths[i] {
					return false
				}
			}
			if a.Service != b.Service {
				return false
			}
		}
	}
	return true
}

// Cleanup deletes resources specific to this l7 in the right order.
// forwarding rule -> target proxy -> url map
// This leaves backends and health checks, which are shared across loadbalancers.
func (l *L7) Cleanup() error {
	if l.fw != nil {
		glog.V(2).Infof("Deleting global forwarding rule %v", l.fw.Name)
		if err := utils.IgnoreHTTPNotFound(l.cloud.DeleteGlobalForwardingRule(l.fw.Name)); err != nil {
			return err
		}
		l.fw = nil
	}
	if l.fws != nil {
		glog.V(2).Infof("Deleting global forwarding rule %v", l.fws.Name)
		if err := utils.IgnoreHTTPNotFound(l.cloud.DeleteGlobalForwardingRule(l.fws.Name)); err != nil {
			return err
		}
		l.fws = nil
	}
	if l.ip != nil {
		glog.V(2).Infof("Deleting static IP %v(%v)", l.ip.Name, l.ip.Address)
		if err := utils.IgnoreHTTPNotFound(l.cloud.DeleteGlobalAddress(l.ip.Name)); err != nil {
			return err
		}
		l.ip = nil
	}
	if l.tps != nil {
		glog.V(2).Infof("Deleting target https proxy %v", l.tps.Name)
		if err := utils.IgnoreHTTPNotFound(l.cloud.DeleteTargetHttpsProxy(l.tps.Name)); err != nil {
			return err
		}
		l.tps = nil
	}
	// Delete the SSL cert if it is from a secret, not referencing a pre-created GCE cert.
	if len(l.sslCerts) != 0 && l.runtimeInfo.TLSName == "" {
		var certErr error
		for _, cert := range l.sslCerts {
			glog.V(2).Infof("Deleting sslcert %s", cert.Name)
			if err := utils.IgnoreHTTPNotFound(l.cloud.DeleteSslCertificate(cert.Name)); err != nil {
				glog.Errorf("Old cert delete failed - %v", err)
				certErr = err
			}

		}
		l.sslCerts = nil
		if certErr != nil {
			return certErr
		}
	}
	if l.tp != nil {
		glog.V(2).Infof("Deleting target http proxy %v", l.tp.Name)
		if err := utils.IgnoreHTTPNotFound(l.cloud.DeleteTargetHttpProxy(l.tp.Name)); err != nil {
			return err
		}
		l.tp = nil
	}
	if l.um != nil {
		glog.V(2).Infof("Deleting url map %v", l.um.Name)
		if err := utils.IgnoreHTTPNotFound(l.cloud.DeleteUrlMap(l.um.Name)); err != nil {
			return err
		}
		l.um = nil
	}
	return nil
}

// getBackendNames returns the names of backends in this L7 urlmap.
func (l *L7) getBackendNames() []string {
	if l.um == nil {
		return []string{}
	}
	beNames := sets.NewString()
	for _, pathMatcher := range l.um.PathMatchers {
		for _, pathRule := range pathMatcher.PathRules {
			// This is gross, but the urlmap only has links to backend services.
			parts := strings.Split(pathRule.Service, "/")
			name := parts[len(parts)-1]
			if name != "" {
				beNames.Insert(name)
			}
		}
	}
	// The default Service recorded in the urlMap is a link to the backend.
	// Note that this can either be user specified, or the L7 controller's
	// global default.
	parts := strings.Split(l.um.DefaultService, "/")
	defaultBackendName := parts[len(parts)-1]
	if defaultBackendName != "" {
		beNames.Insert(defaultBackendName)
	}
	return beNames.List()
}

// GetLBAnnotations returns the annotations of an l7. This includes it's current status.
func GetLBAnnotations(l7 *L7, existing map[string]string, backendPool backends.BackendPool) map[string]string {
	if existing == nil {
		existing = map[string]string{}
	}
	backends := l7.getBackendNames()
	backendState := map[string]string{}
	for _, beName := range backends {
		backendState[beName] = backendPool.Status(beName)
	}
	jsonBackendState := "Unknown"
	b, err := json.Marshal(backendState)
	if err == nil {
		jsonBackendState = string(b)
	}
	certs := []string{}
	for _, cert := range l7.sslCerts {
		certs = append(certs, cert.Name)
	}

	existing[fmt.Sprintf("%v/url-map", annotations.StatusPrefix)] = l7.um.Name
	// Forwarding rule and target proxy might not exist if allowHTTP == false
	if l7.fw != nil {
		existing[fmt.Sprintf("%v/forwarding-rule", annotations.StatusPrefix)] = l7.fw.Name
	}
	if l7.tp != nil {
		existing[fmt.Sprintf("%v/target-proxy", annotations.StatusPrefix)] = l7.tp.Name
	}
	// HTTPs resources might not exist if TLS == nil
	if l7.fws != nil {
		existing[fmt.Sprintf("%v/https-forwarding-rule", annotations.StatusPrefix)] = l7.fws.Name
	}
	if l7.tps != nil {
		existing[fmt.Sprintf("%v/https-target-proxy", annotations.StatusPrefix)] = l7.tps.Name
	}
	if l7.ip != nil {
		existing[fmt.Sprintf("%v/static-ip", annotations.StatusPrefix)] = l7.ip.Name
	}
	if len(certs) > 0 {
		existing[fmt.Sprintf("%v/ssl-cert", annotations.StatusPrefix)] = strings.Join(certs, ",")
	}
	// TODO: We really want to know *when* a backend flipped states.
	existing[fmt.Sprintf("%v/backends", annotations.StatusPrefix)] = jsonBackendState
	return existing
}

// GCEResourceName retrieves the name of the gce resource created for this
// Ingress, of the given resource type, by inspecting the map of ingress
// annotations.
func GCEResourceName(ingAnnotations map[string]string, resourceName string) string {
	// Even though this function is trivial, it exists to keep the annotation
	// parsing logic in a single location.
	resourceName, _ = ingAnnotations[fmt.Sprintf("%v/%v", annotations.StatusPrefix, resourceName)]
	return resourceName
}

func GetCertHash(contents string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(contents)))[:16]
}
