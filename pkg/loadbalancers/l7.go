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

	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/translator"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/ingress-gce/pkg/annotations"
	frontendconfigv1beta1 "k8s.io/ingress-gce/pkg/apis/frontendconfig/v1beta1"
	"k8s.io/ingress-gce/pkg/backends"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/loadbalancers/features"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/klog"
	"k8s.io/legacy-cloud-providers/gce"
)

const (
	invalidConfigErrorMessage = "invalid ingress frontend configuration, please check your usage of the 'kubernetes.io/ingress.allow-http' annotation."
)

// L7RuntimeInfo is info passed to this module from the controller runtime.
type L7RuntimeInfo struct {
	// IP is the desired ip of the loadbalancer, eg from a staticIP.
	IP string
	// TLS are the tls certs to use in termination.
	TLS []*translator.TLSCerts
	// TLSName is the name of the preshared cert to use. Multiple certs can be specified as a comma-separated string
	TLSName string
	// Ingress is the processed Ingress API object.
	Ingress *v1.Ingress
	// AllowHTTP will not setup :80, if TLS is nil and AllowHTTP is set,
	// no loadbalancer is created.
	AllowHTTP bool
	// The name of a Global/Regional Static IP. If specified, the IP associated with
	// this name is used in the Forwarding Rules for this loadbalancer.
	// If this is an l7-ILB ingress, the static IP is assumed to be internal
	StaticIPName string
	// The name of the static IP subnet, this is only used for L7-ILB Ingress static IPs
	StaticIPSubnet string
	// UrlMap is our internal representation of a url map.
	UrlMap *utils.GCEURLMap
	// FrontendConfig is the type which encapsulates features for the load balancer.
	FrontendConfig *frontendconfigv1beta1.FrontendConfig
}

// L7 represents a single L7 loadbalancer.
type L7 struct {
	// runtimeInfo is non-cloudprovider information passed from the controller.
	runtimeInfo *L7RuntimeInfo
	// ingress stores the ingress
	ingress v1.Ingress
	// cloud is an interface to manage loadbalancers in the GCE cloud.
	cloud *gce.Cloud
	// um is the UrlMap associated with this L7.
	um *composite.UrlMap
	// rum is the Http Redirect only UrlMap associated with this L7.
	redirectUm *composite.UrlMap
	// tp is the TargetHTTPProxy associated with this L7.
	tp *composite.TargetHttpProxy
	// tps is the TargetHTTPSProxy associated with this L7.
	tps *composite.TargetHttpsProxy
	// fw is the GlobalForwardingRule that points to the TargetHTTPProxy.
	fw *composite.ForwardingRule
	// fws is the GlobalForwardingRule that points to the TargetHTTPSProxy.
	fws *composite.ForwardingRule
	// ip is the static-ip associated with both ForwardingRules.
	ip *composite.Address
	// sslCerts is the list of ssl certs associated with the targetHTTPSProxy.
	sslCerts []*composite.SslCertificate
	// oldSSLCerts is the list of certs that used to be hooked up to the
	// targetHTTPSProxy. We can't update a cert in place, so we need
	// to create - update - delete and storing the old certs in a list
	// prevents leakage if there's a failure along the way.
	oldSSLCerts []*composite.SslCertificate
	// namer is used to compute names of the various sub-components of an L7.
	namer namer.IngressFrontendNamer
	// recorder is used to generate k8s Events.
	recorder record.EventRecorder
	// resource type stores the KeyType of the resources in the loadbalancer (e.g. Regional)
	scope meta.KeyType
}

// String returns the name of the loadbalancer.
// Warning: This should be used only for logging and should not be used to
// retrieve/ delete gce resource names.
func (l *L7) String() string {
	return l.namer.LoadBalancer().String()
}

// Versions returns the struct listing the versions for every resource
func (l *L7) Versions() *features.ResourceVersions {
	return features.VersionsFromIngress(&l.ingress)
}

// CreateKey creates a meta.Key for use with composite types
func (l *L7) CreateKey(name string) (*meta.Key, error) {
	return composite.CreateKey(l.cloud, name, l.scope)
}

// Regional returns true if the l7 scope is regional
func (l *L7) Regional() bool {
	return l.scope == meta.Regional
}

// RuntimeInfo returns the L7RuntimeInfo associated with the L7 load balancer.
func (l *L7) RuntimeInfo() *L7RuntimeInfo {
	return l.runtimeInfo
}

// UrlMap returns the UrlMap associated with the L7 load balancer.
func (l *L7) UrlMap() *composite.UrlMap {
	return l.um
}

func (l *L7) edgeHop() error {
	sslConfigured := l.runtimeInfo.TLS != nil || l.runtimeInfo.TLSName != ""
	// Return an error if user configuration species that both HTTP & HTTPS are not to be configured.
	if !l.runtimeInfo.AllowHTTP && !sslConfigured {
		return fmt.Errorf(invalidConfigErrorMessage)
	}

	// Check for invalid L7-ILB HTTPS config before attempting sync
	if utils.IsGCEL7ILBIngress(l.runtimeInfo.Ingress) && sslConfigured && l.runtimeInfo.AllowHTTP {
		l.recorder.Eventf(l.runtimeInfo.Ingress, corev1.EventTypeWarning, "WillNotConfigureFrontend", "gce-internal Ingress class does not currently support both HTTP and HTTPS served on the same IP (kubernetes.io/ingress.allow-http must be false when using HTTPS).")
		return fmt.Errorf("error invalid internal ingress https config")
	}

	if err := l.ensureComputeURLMap(); err != nil {
		return err
	}

	if flags.F.EnableFrontendConfig {
		if err := l.ensureRedirectURLMap(); err != nil {
			return fmt.Errorf("ensureRedirectUrlMap() = %v", err)
		}
	}

	if l.runtimeInfo.AllowHTTP {
		if err := l.edgeHopHttp(); err != nil {
			return err
		}
	} else if flags.F.EnableDeleteUnusedFrontends && requireDeleteFrontend(l.ingress, namer.HTTPProtocol) {
		if err := l.deleteHttp(features.VersionsFromIngress(&l.ingress)); err != nil {
			return err
		}
		klog.V(2).Infof("Successfully deleted unused HTTP frontend resources for load-balancer %s", l)
	}
	// Defer promoting an ephemeral to a static IP until it's really needed.
	if l.runtimeInfo.AllowHTTP && sslConfigured {
		klog.V(3).Infof("checking static ip for %v", l)
		if err := l.checkStaticIP(); err != nil {
			return err
		}
	}
	if sslConfigured {
		klog.V(3).Infof("validating https for %v", l)
		if err := l.edgeHopHttps(); err != nil {
			return err
		}
	} else if flags.F.EnableDeleteUnusedFrontends && requireDeleteFrontend(l.ingress, namer.HTTPSProtocol) {
		if err := l.deleteHttps(features.VersionsFromIngress(&l.ingress)); err != nil {
			return err
		}
		klog.V(2).Infof("Successfully deleted unused HTTPS frontend resources for load-balancer %s", l)
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

// requireDeleteFrontend returns true if gce loadbalancer resources needs to deleted for given protocol.
func requireDeleteFrontend(ing v1.Ingress, protocol namer.NamerProtocol) bool {
	var keys []string
	switch protocol {
	case namer.HTTPSProtocol:
		keys = append(keys, []string{
			annotations.HttpsForwardingRuleKey,
			annotations.TargetHttpsProxyKey,
		}...)
	case namer.HTTPProtocol:
		keys = append(keys, []string{
			annotations.HttpForwardingRuleKey,
			annotations.TargetHttpProxyKey,
		}...)
	default:
		klog.Errorf("Unexpected frontend resource protocol %v", protocol)
	}

	for _, key := range keys {
		if _, exists := ing.Annotations[key]; exists {
			return true
		}
	}
	return false
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

// deleteForwardingRule deletes forwarding rule for given protocol.
func (l *L7) deleteForwardingRule(versions *features.ResourceVersions, protocol namer.NamerProtocol) error {
	frName := l.namer.ForwardingRule(protocol)
	klog.V(2).Infof("Deleting forwarding rule %v", frName)
	key, err := l.CreateKey(frName)
	if err != nil {
		return err
	}
	if err := utils.IgnoreHTTPNotFound(composite.DeleteForwardingRule(l.cloud, key, versions.ForwardingRule)); err != nil {
		return err
	}
	return nil
}

// deleteTargetProxy deletes target proxy for given protocol.
func (l *L7) deleteTargetProxy(versions *features.ResourceVersions, protocol namer.NamerProtocol) error {
	tpName := l.namer.TargetProxy(protocol)
	klog.V(2).Infof("Deleting target %v proxy %v", protocol, tpName)
	key, err := l.CreateKey(tpName)
	if err != nil {
		return err
	}
	switch protocol {
	case namer.HTTPProtocol:
		if err := utils.IgnoreHTTPNotFound(composite.DeleteTargetHttpProxy(l.cloud, key, versions.TargetHttpProxy)); err != nil {
			return err
		}
	case namer.HTTPSProtocol:
		if err := utils.IgnoreHTTPNotFound(composite.DeleteTargetHttpsProxy(l.cloud, key, versions.TargetHttpsProxy)); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unexpected frontend resource protocol: %v", protocol)
	}
	return nil
}

// deleteHttp deletes http forwarding rule and target http proxy.
func (l *L7) deleteHttp(versions *features.ResourceVersions) error {
	// Delete http forwarding rule.
	if err := l.deleteForwardingRule(versions, namer.HTTPProtocol); err != nil {
		return err
	}
	// Delete target http proxy.
	return l.deleteTargetProxy(versions, namer.HTTPProtocol)
}

// deleteHttps deletes https forwarding rule, target https proxy and ingress controller
// managed ssl certificates.
func (l *L7) deleteHttps(versions *features.ResourceVersions) error {
	// Delete https forwarding rule.
	if err := l.deleteForwardingRule(versions, namer.HTTPSProtocol); err != nil {
		return err
	}
	// Get list of ssl certificates owned by this load-balancer that needs to be deleted.
	// We are using https target proxy to list legacy certs, so this list needs to be
	// populated before deleting https target proxy.
	secretsSslCerts, err := l.getIngressManagedSslCerts()
	if err != nil {
		return err
	}
	// Delete target https proxy.
	if err := l.deleteTargetProxy(versions, namer.HTTPSProtocol); err != nil {
		return err
	}
	// Delete ingress managed ssl certificates those created from a secret,
	// not referencing a pre-created GCE cert or managed certificates.
	return l.deleteSSLCertificates(secretsSslCerts, versions)
}

// deleteSSLCertificates deletes given ssl certificates.
func (l *L7) deleteSSLCertificates(sslCertificates []*composite.SslCertificate, versions *features.ResourceVersions) error {
	if len(sslCertificates) == 0 {
		return nil
	}
	var certErr error
	for _, cert := range sslCertificates {
		klog.V(2).Infof("Deleting sslcert %s", cert.Name)
		key, err := l.CreateKey(cert.Name)
		if err != nil {
			return err
		}
		if err := utils.IgnoreHTTPNotFound(composite.DeleteSslCertificate(l.cloud, key, versions.SslCertificate)); err != nil {
			klog.Errorf("Old cert delete failed - %v", err)
			certErr = err
		}
	}
	l.sslCerts = nil
	return certErr
}

// deleteStaticIP deletes ingress managed static ip.
func (l *L7) deleteStaticIP() error {
	frName := l.namer.ForwardingRule(namer.HTTPProtocol)
	ip, err := l.cloud.GetGlobalAddress(frName)
	if ip != nil && utils.IgnoreHTTPNotFound(err) == nil {
		klog.V(2).Infof("Deleting static IP %v(%v)", ip.Name, ip.Address)
		if err := utils.IgnoreHTTPNotFound(l.cloud.DeleteGlobalAddress(ip.Name)); err != nil {
			return err
		}
	}
	return nil
}

// Cleanup deletes resources specific to this l7 in the right order.
// forwarding rule -> target proxy -> url map
// This leaves backends and health checks, which are shared across loadbalancers.
func (l *L7) Cleanup(versions *features.ResourceVersions) error {
	var err error
	// Delete http frontend resources.
	if err := l.deleteHttp(versions); err != nil {
		return err
	}
	// Delete static ip.
	if err := l.deleteStaticIP(); err != nil {
		return err
	}
	// Delete https frontend resources.
	if err := l.deleteHttps(versions); err != nil {
		return err
	}
	// Delete URL map.
	umName := l.namer.UrlMap()
	klog.V(2).Infof("Deleting URL Map %v", umName)
	key, err := l.CreateKey(umName)
	if err != nil {
		return err
	}
	if err := utils.IgnoreHTTPNotFound(composite.DeleteUrlMap(l.cloud, key, versions.UrlMap)); err != nil {
		return err
	}

	// Delete RedirectUrlMap if exists
	if flags.F.EnableFrontendConfig {
		umName, supported := l.namer.RedirectUrlMap()
		if !supported {
			// Skip deletion
			return nil
		}
		klog.V(2).Infof("Deleting Redirect URL Map %v", umName)
		key, err := l.CreateKey(umName)
		if err != nil {
			return err
		}
		if err := utils.IgnoreHTTPNotFound(composite.DeleteUrlMap(l.cloud, key, versions.UrlMap)); err != nil {
			return err
		}
	}
	return nil
}

func (l *L7) getFrontendAnnotations(existing map[string]string) map[string]string {
	if existing == nil {
		existing = map[string]string{}
	}

	var certs []string
	for _, cert := range l.sslCerts {
		certs = append(certs, cert.Name)
	}

	existing[annotations.UrlMapKey] = l.um.Name
	// Forwarding rule and target proxy might not exist if allowHTTP == false
	if l.fw != nil {
		existing[annotations.HttpForwardingRuleKey] = l.fw.Name
	} else {
		delete(existing, annotations.HttpForwardingRuleKey)
	}
	if l.tp != nil {
		existing[annotations.TargetHttpProxyKey] = l.tp.Name
	} else {
		delete(existing, annotations.TargetHttpProxyKey)
	}
	// HTTPs resources might not exist if TLS == nil
	if l.fws != nil {
		existing[annotations.HttpsForwardingRuleKey] = l.fws.Name
	} else {
		delete(existing, annotations.HttpsForwardingRuleKey)
	}
	if l.tps != nil {
		existing[annotations.TargetHttpsProxyKey] = l.tps.Name
	} else {
		delete(existing, annotations.TargetHttpsProxyKey)
	}

	// Handle Https Redirect Map
	if flags.F.EnableFrontendConfig {
		if l.redirectUm != nil {
			existing[annotations.RedirectUrlMapKey] = l.redirectUm.Name
		} else {
			delete(existing, annotations.RedirectUrlMapKey)
		}
	}

	// Note that ingress IP annotation is not deleted when user disables one of http/https.
	// This is because the promoted static IP is retained for use and will be deleted only
	// when load-balancer is deleted or user specifies a different IP.
	if l.ip != nil {
		existing[annotations.StaticIPKey] = l.ip.Name
	}
	if len(certs) > 0 {
		existing[annotations.SSLCertKey] = strings.Join(certs, ",")
	} else {
		delete(existing, annotations.SSLCertKey)
	}
	return existing
}

// GetLBAnnotations returns the annotations of an l7. This includes it's current status.
func GetLBAnnotations(l7 *L7, existing map[string]string, backendSyncer backends.Syncer) (map[string]string, error) {
	backends, err := getBackendNames(l7.um)
	if err != nil {
		return nil, err
	}
	backendState := map[string]string{}
	for _, beName := range backends {
		version := l7.Versions().BackendService
		state, err := backendSyncer.Status(beName, version, l7.scope)
		// Don't return error here since we want to keep syncing
		if err != nil {
			klog.Errorf("Error syncing backend status for %s - %s - %s: %v", beName, version, l7.scope, err)
		}
		backendState[beName] = state
	}
	jsonBackendState := "Unknown"
	b, err := json.Marshal(backendState)
	if err == nil {
		jsonBackendState = string(b)
	}
	// Update annotations for frontend resources.
	existing = l7.getFrontendAnnotations(existing)
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

// description gets a description for the ingress GCP resources.
func (l *L7) description() (string, error) {
	if l.runtimeInfo.Ingress == nil {
		return "", fmt.Errorf("missing Ingress object to construct description for %s", l)
	}

	namespace := l.runtimeInfo.Ingress.ObjectMeta.Namespace
	ingressName := l.runtimeInfo.Ingress.ObjectMeta.Name
	namespacedName := types.NamespacedName{Name: ingressName, Namespace: namespace}

	return fmt.Sprintf(`{"kubernetes.io/ingress-name": %q}`, namespacedName.String()), nil
}
