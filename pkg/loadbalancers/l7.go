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
	"encoding/json"
	"fmt"
	"strings"

	mcrt "github.com/GoogleCloudPlatform/gke-managed-certs/pkg/clientgen/listers/gke.googleapis.com/v1alpha1"
	"github.com/golang/glog"

	compute "google.golang.org/api/compute/v1"

	extensions "k8s.io/api/extensions/v1beta1"
	"k8s.io/client-go/tools/record"

	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/backends"
	"k8s.io/ingress-gce/pkg/utils"
)

const (
	// DefaultHost is the host used if none is specified. It is a valid value
	// for the "Host" field recognized by GCE.
	DefaultHost = "*"

	// DefaultPath is the path used if none is specified. It is a valid path
	// recognized by GCE.
	DefaultPath = "/*"
)

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
	// Ingress is the processed Ingress API object.
	Ingress *extensions.Ingress
	// ManagedCertificates is a comma-separated list of managed SSL certificates to use.
	ManagedCertificates string
	// AllowHTTP will not setup :80, if TLS is nil and AllowHTTP is set,
	// no loadbalancer is created.
	AllowHTTP bool
	// The name of a Global Static IP. If specified, the IP associated with
	// this name is used in the Forwarding Rules for this loadbalancer.
	StaticIPName string
	// UrlMap is our internal representation of a url map.
	UrlMap *utils.GCEURLMap
}

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
	// sslCerts is the list of ssl certs associated with the targetHTTPSProxy.
	sslCerts []*compute.SslCertificate
	// oldSSLCerts is the list of certs that used to be hooked up to the
	// targetHTTPSProxy. We can't update a cert in place, so we need
	// to create - update - delete and storing the old certs in a list
	// prevents leakage if there's a failure along the way.
	oldSSLCerts []*compute.SslCertificate
	// namer is used to compute names of the various sub-components of an L7.
	namer *utils.Namer
	// mcrt is an interface to ManagedCertificate resources.
	mcrt mcrt.ManagedCertificateLister
	// recorder is used to generate k8s Events.
	recorder record.EventRecorder
}

// RuntimeInfo returns the L7RuntimeInfo associated with the L7 load balancer.
func (l *L7) RuntimeInfo() *L7RuntimeInfo {
	return l.runtimeInfo
}

// UrlMap returns the UrlMap associated with the L7 load balancer.
func (l *L7) UrlMap() *compute.UrlMap {
	return l.um
}

func (l *L7) edgeHop() error {
	if err := l.ensureComputeURLMap(); err != nil {
		return err
	}
	if l.runtimeInfo.AllowHTTP {
		if err := l.edgeHopHttp(); err != nil {
			return err
		}
	}
	// Defer promoting an ephemeral to a static IP until it's really needed.
	sslConfigured := l.runtimeInfo.TLS != nil || l.runtimeInfo.TLSName != "" || l.runtimeInfo.ManagedCertificates != ""
	if l.runtimeInfo.AllowHTTP && sslConfigured {
		glog.V(3).Infof("checking static ip for %v", l.Name)
		if err := l.checkStaticIP(); err != nil {
			return err
		}
	}
	if sslConfigured {
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
	defer l.deleteOldSSLCerts()
	if err := l.checkSSLCert(); err != nil {
		return err
	}

	if err := l.checkHttpsProxy(); err != nil {
		return err
	}
	return l.checkHttpsForwardingRule()
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

// Cleanup deletes resources specific to this l7 in the right order.
// forwarding rule -> target proxy -> url map
// This leaves backends and health checks, which are shared across loadbalancers.
func Cleanup(name string, cloud LoadBalancers, namer *utils.Namer) error {
	fwName := namer.ForwardingRule(name, utils.HTTPProtocol)
	glog.V(2).Infof("Deleting global forwarding rule %v", fwName)
	if err := utils.IgnoreHTTPNotFound(cloud.DeleteGlobalForwardingRule(fwName)); err != nil {
		return err
	}

	fwsName := namer.ForwardingRule(name, utils.HTTPSProtocol)
	glog.V(2).Infof("Deleting global forwarding rule %v", fwsName)
	if err := utils.IgnoreHTTPNotFound(cloud.DeleteGlobalForwardingRule(fwsName)); err != nil {
		return err
	}

	ip, err := cloud.GetGlobalAddress(fwName)
	if ip != nil && utils.IgnoreHTTPNotFound(err) == nil {
		glog.V(2).Infof("Deleting static IP %v(%v)", ip.Name, ip.Address)
		if err := utils.IgnoreHTTPNotFound(cloud.DeleteGlobalAddress(ip.Name)); err != nil {
			return err
		}
	}

	tpName := namer.TargetProxy(name, utils.HTTPProtocol)
	glog.V(2).Infof("Deleting target http proxy %v", tpName)
	if err := utils.IgnoreHTTPNotFound(cloud.DeleteTargetHttpProxy(tpName)); err != nil {
		return err
	}

	tpsName := namer.TargetProxy(name, utils.HTTPSProtocol)
	glog.V(2).Infof("Deleting target https proxy %v", tpsName)
	if err := utils.IgnoreHTTPNotFound(cloud.DeleteTargetHttpsProxy(tpsName)); err != nil {
		return err
	}

	// Delete the SSL cert if it is from a secret, not referencing a pre-created GCE cert or managed certificates.
	secretsSslCerts, err := getIngressManagedSslCerts(name, cloud, namer)
	if err != nil {
		return err
	}

	if len(secretsSslCerts) != 0 {
		var certErr error
		for _, cert := range secretsSslCerts {
			glog.V(2).Infof("Deleting sslcert %s", cert.Name)
			if err := utils.IgnoreHTTPNotFound(cloud.DeleteSslCertificate(cert.Name)); err != nil {
				glog.Errorf("Old cert delete failed - %v", err)
				certErr = err
			}
		}

		if certErr != nil {
			return certErr
		}
	}

	umName := namer.UrlMap(name)
	glog.V(2).Infof("Deleting URL Map %v", umName)
	if err := utils.IgnoreHTTPNotFound(cloud.DeleteUrlMap(umName)); err != nil {
		return err
	}

	return nil
}

// GetLBAnnotations returns the annotations of an l7. This includes it's current status.
func GetLBAnnotations(l7 *L7, existing map[string]string, backendSyncer backends.Syncer) (map[string]string, error) {
	if existing == nil {
		existing = map[string]string{}
	}
	backends, err := getBackendNames(l7.um)
	if err != nil {
		return nil, err
	}
	backendState := map[string]string{}
	for _, beName := range backends {
		backendState[beName] = backendSyncer.Status(beName)
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
	return existing, nil
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
