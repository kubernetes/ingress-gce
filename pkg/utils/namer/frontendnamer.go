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
	"fmt"
	"strings"

	v1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/ingress-gce/pkg/utils/common"
	"k8s.io/klog"
)

const (
	// V1NamingScheme is v1 frontend naming scheme.
	V1NamingScheme = Scheme("v1")
	// V2NamingScheme is v2 frontend naming scheme.
	V2NamingScheme = Scheme("v2")
	// schemaVersionV2 is suffix to be appended to resource prefix for v2 naming scheme.
	schemaVersionV2 = "2"
	// maximumAllowedCombinedLength is the maximum combined length of namespace and
	// name portions in the resource name.
	// This is computed by subtracting: k8s1 - 4, dashes - 5, resource prefix - 2,
	// clusterUID - 8,  suffix hash- 8.
	// maximumAllowedCombinedLength = maximum name length of gce resource(63) - 27
	maximumAllowedCombinedLength = 36
	// urlMapPrefixV2 is URL map prefix for v2 naming scheme.
	urlMapPrefixV2 = "um"
	// urlMapPrefixV2 is Https-Redirect-Only URL map prefix for v2 naming scheme.
	redirectUrlMapPrefixV2 = "rm"
	// forwardingRulePrefixV2 is http forwarding rule prefix for v2 naming scheme.
	forwardingRulePrefixV2 = "fr"
	// httpsForwardingRulePrefixV2 is https forwarding rule prefix for v2 naming scheme.
	httpsForwardingRulePrefixV2 = "fs"
	// targetHTTPProxyPrefixV2 is target http proxy prefix for v2 naming scheme.
	targetHTTPProxyPrefixV2 = "tp"
	// targetHTTPSProxyPrefixV2 is target https proxy prefix for v2 naming scheme.
	targetHTTPSProxyPrefixV2 = "ts"
	// sslCertPrefixV2 is ssl certificate prefix for v2 naming scheme.
	sslCertPrefixV2 = "cr"
	// clusterUIDLength is length of cluster UID to be included in resource names.
	clusterUIDLength = 8
)

// Scheme is ingress frontend name scheme.
type Scheme string

// V1IngressFrontendNamer implements IngressFrontendNamer. This is a wrapper on top of namer.Namer.
type V1IngressFrontendNamer struct {
	ing    *v1.Ingress
	namer  *Namer
	lbName LoadBalancerName
}

// newV1IngressFrontendNamer returns v1 frontend namer for given ingress.
func newV1IngressFrontendNamer(ing *v1.Ingress, namer *Namer) IngressFrontendNamer {
	lbName := namer.LoadBalancer(common.IngressKeyFunc(ing))
	return &V1IngressFrontendNamer{ing: ing, namer: namer, lbName: lbName}
}

// newV1IngressFrontendNamerForLoadBalancer returns v1 frontend namer for load balancer.
func newV1IngressFrontendNamerForLoadBalancer(lbName LoadBalancerName, namer *Namer) IngressFrontendNamer {
	return &V1IngressFrontendNamer{namer: namer, lbName: lbName}
}

// ForwardingRule implements IngressFrontendNamer.
func (ln *V1IngressFrontendNamer) ForwardingRule(protocol NamerProtocol) string {
	return ln.namer.ForwardingRule(ln.lbName, protocol)
}

// TargetProxy implements IngressFrontendNamer.
func (ln *V1IngressFrontendNamer) TargetProxy(protocol NamerProtocol) string {
	return ln.namer.TargetProxy(ln.lbName, protocol)
}

// UrlMap implements IngressFrontendNamer.
func (ln *V1IngressFrontendNamer) UrlMap() string {
	return ln.namer.UrlMap(ln.lbName)
}

// RedirectUrlMap implements IngressFrontendNamer.
func (ln *V1IngressFrontendNamer) RedirectUrlMap() (string, bool) {
	return "", false
}

// SSLCertName implements IngressFrontendNamer.
func (ln *V1IngressFrontendNamer) SSLCertName(secretHash string) string {
	return ln.namer.SSLCertName(ln.lbName, secretHash)
}

// IsCertNameForLB implements IngressFrontendNamer.
func (ln *V1IngressFrontendNamer) IsCertNameForLB(certName string) bool {
	return ln.namer.IsCertUsedForLB(ln.lbName, certName)
}

// IsLegacySSLCert implements IngressFrontendNamer.
func (ln *V1IngressFrontendNamer) IsLegacySSLCert(certName string) bool {
	return ln.namer.IsLegacySSLCert(ln.lbName, certName)
}

// LoadBalancer implements IngressFrontendNamer.
func (ln *V1IngressFrontendNamer) LoadBalancer() LoadBalancerName {
	return ln.lbName
}

// IsValidLoadBalancer implements IngressFrontendNamer.
func (ln *V1IngressFrontendNamer) IsValidLoadBalancer() bool {
	// Verify if URL Map is a valid GCE resource name.
	return isValidGCEResourceName(ln.UrlMap())
}

// V2IngressFrontendNamer implements IngressFrontendNamer.
type V2IngressFrontendNamer struct {
	ing *v1.Ingress
	// prefix for all resource names (ex.: "k8s").
	prefix string
	// Load balancer name to be included in resource name.
	lbName LoadBalancerName
	// clusterUID is an 8 character hash to be included in resource names.
	// This is immutable after the cluster is created. Kube-system uid which is
	// immutable is used as cluster UID for v2 naming scheme.
	clusterUID string
}

// newV2IngressFrontendNamer returns a v2 frontend namer for given ingress, kube-system uid and prefix.
// Example:
// For Ingress - namespace/ingress, clusterUID - uid01234, prefix - k8s
// The resource names are -
// LoadBalancer          : uid0123-namespace-ingress-cysix1wq
// HTTP Forwarding Rule  : k8s2-fr-uid01234-namespace-ingress-cysix1wq
// HTTPS Forwarding Rule : k8s2-fs-uid01234-namespace-ingress-cysix1wq
// Target HTTP Proxy     : k8s2-tp-uid01234-namespace-ingress-cysix1wq
// Target HTTPS Proxy    : k8s2-ts-uid01234-namespace-ingress-cysix1wq
// URL Map               : k8s2-um-uid01234-namespace-ingress-cysix1wq
// SSL Certificate       : k8s2-cr-uid01234-<lb-hash>-<secret-hash>
func newV2IngressFrontendNamer(ing *v1.Ingress, kubeSystemUID string, prefix string) IngressFrontendNamer {
	clusterUID := common.ContentHash(kubeSystemUID, clusterUIDLength)
	namer := &V2IngressFrontendNamer{ing: ing, prefix: prefix, clusterUID: clusterUID}
	// Initialize lbName.
	truncFields := TrimFieldsEvenly(maximumAllowedCombinedLength, ing.Namespace, ing.Name)
	truncNamespace := truncFields[0]
	truncName := truncFields[1]
	suffix := namer.suffix(kubeSystemUID, ing.Namespace, ing.Name)
	namer.lbName = LoadBalancerName(fmt.Sprintf("%s-%s-%s-%s", clusterUID, truncNamespace, truncName, suffix))
	return namer
}

// ForwardingRule returns the name of forwarding rule based on given protocol.
func (vn *V2IngressFrontendNamer) ForwardingRule(protocol NamerProtocol) string {
	switch protocol {
	case HTTPProtocol:
		return fmt.Sprintf("%s%s-%s-%s", vn.prefix, schemaVersionV2, forwardingRulePrefixV2, vn.lbName)
	case HTTPSProtocol:
		return fmt.Sprintf("%s%s-%s-%s", vn.prefix, schemaVersionV2, httpsForwardingRulePrefixV2, vn.lbName)
	default:
		klog.Fatalf("invalid ForwardingRule protocol: %q", protocol)
		return "invalid"
	}
}

// TargetProxy returns the name of target proxy based on given protocol.
func (vn *V2IngressFrontendNamer) TargetProxy(protocol NamerProtocol) string {
	switch protocol {
	case HTTPProtocol:
		return fmt.Sprintf("%s%s-%s-%s", vn.prefix, schemaVersionV2, targetHTTPProxyPrefixV2, vn.lbName)
	case HTTPSProtocol:
		return fmt.Sprintf("%s%s-%s-%s", vn.prefix, schemaVersionV2, targetHTTPSProxyPrefixV2, vn.lbName)
	default:
		klog.Fatalf("invalid TargetProxy protocol: %q", protocol)
		return "invalid"
	}
}

// UrlMap returns the name of URL map.
func (vn *V2IngressFrontendNamer) UrlMap() string {
	return fmt.Sprintf("%s%s-%s-%s", vn.prefix, schemaVersionV2, urlMapPrefixV2, vn.lbName)
}

// RedirectUrlMap returns the name of Redirect URL map.
func (vn *V2IngressFrontendNamer) RedirectUrlMap() (string, bool) {
	return fmt.Sprintf("%s%s-%s-%s", vn.prefix, schemaVersionV2, redirectUrlMapPrefixV2, vn.lbName), true
}

// SSLCertName returns the name of the certificate.
func (vn *V2IngressFrontendNamer) SSLCertName(secretHash string) string {
	return fmt.Sprintf("%s%s-%s-%s-%s-%s", vn.prefix, schemaVersionV2, sslCertPrefixV2, vn.clusterUID, vn.lbNameToHash(), secretHash)
}

// IsCertNameForLB returns true if the certName belongs to this cluster's ingress.
// It checks that the hashed lbName exists.
func (vn *V2IngressFrontendNamer) IsCertNameForLB(certName string) bool {
	prefix := fmt.Sprintf("%s%s-%s-%s-%s", vn.prefix, schemaVersionV2, sslCertPrefixV2, vn.clusterUID, vn.lbNameToHash())
	return strings.HasPrefix(certName, prefix)
}

// IsLegacySSLCert always return false as v2 naming scheme does not use legacy certs.
func (vn *V2IngressFrontendNamer) IsLegacySSLCert(certName string) bool {
	return false
}

// LoadBalancer returns loadbalancer name.
// Note that this is used for generating GCE resource names.
func (vn *V2IngressFrontendNamer) LoadBalancer() LoadBalancerName {
	return vn.lbName
}

// IsValidLoadBalancer implements IngressFrontendNamer.
func (vn *V2IngressFrontendNamer) IsValidLoadBalancer() bool {
	// Verify if URL Map is a valid GCE resource name.
	return isValidGCEResourceName(vn.UrlMap())
}

// suffix returns hash string of length 8 of a concatenated string generated from
// uid, namespace and name. These fields in combination define an ingress/load-balancer uniquely.
func (vn *V2IngressFrontendNamer) suffix(uid, namespace, name string) string {
	lbString := strings.Join([]string{uid, namespace, name}, ";")
	return common.ContentHash(lbString, 8)
}

// lbNameToHash returns hash string of length 16 of lbName.
func (vn *V2IngressFrontendNamer) lbNameToHash() string {
	return common.ContentHash(vn.lbName.String(), 16)
}

// FrontendNamerFactory implements IngressFrontendNamerFactory.
type FrontendNamerFactory struct {
	namer *Namer
	// kubeSystemUID is the UID of kube-system namespace which is unique for a k8s cluster.
	// Note that this is used only for V2IngressFrontendNamer.
	kubeSystemUID string
}

// NewFrontendNamerFactory returns IngressFrontendNamerFactory given a v1 namer and kube-system uid.
func NewFrontendNamerFactory(namer *Namer, kubeSystemUID types.UID) IngressFrontendNamerFactory {
	return &FrontendNamerFactory{namer: namer, kubeSystemUID: string(kubeSystemUID)}
}

// Namer implements IngressFrontendNamerFactory.
func (rn *FrontendNamerFactory) Namer(ing *v1.Ingress) IngressFrontendNamer {
	namingScheme := FrontendNamingScheme(ing)
	switch namingScheme {
	case V1NamingScheme:
		return newV1IngressFrontendNamer(ing, rn.namer)
	case V2NamingScheme:
		return newV2IngressFrontendNamer(ing, rn.kubeSystemUID, rn.namer.prefix)
	default:
		klog.Errorf("Unexpected frontend naming scheme %s", namingScheme)
		return newV1IngressFrontendNamer(ing, rn.namer)
	}
}

// NamerForLoadBalancer implements IngressFrontendNamerFactory.
func (rn *FrontendNamerFactory) NamerForLoadBalancer(lbName LoadBalancerName) IngressFrontendNamer {
	return newV1IngressFrontendNamerForLoadBalancer(lbName, rn.namer)
}
