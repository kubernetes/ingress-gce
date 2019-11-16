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

	"k8s.io/api/networking/v1beta1"
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
	ing    *v1beta1.Ingress
	namer  *Namer
	lbName string
}

// newV1IngressFrontendNamer returns v1 frontend namer for given ingress.
func newV1IngressFrontendNamer(ing *v1beta1.Ingress, namer *Namer) IngressFrontendNamer {
	lbName := namer.LoadBalancer(common.IngressKeyFunc(ing))
	return &V1IngressFrontendNamer{ing: ing, namer: namer, lbName: lbName}
}

// newV1IngressFrontendNamerFromLBName returns v1 frontend namer for load balancer.
func newV1IngressFrontendNamerFromLBName(lbName string, namer *Namer) IngressFrontendNamer {
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

// LbName implements IngressFrontendNamer.
func (ln *V1IngressFrontendNamer) LbName() string {
	return ln.lbName
}

// V2IngressFrontendNamer implements IngressFrontendNamer.
type V2IngressFrontendNamer struct {
	ing *v1beta1.Ingress
	// prefix for all resource names (ex.: "k8s").
	prefix string
	// Load balancer name to be included in resource name.
	lbName string
	// clusterUID is an 8 character hash to be included in resource names.
	// This is immutable after the cluster is created. Kube-system uid which is
	// immutable is used as cluster UID for v2 naming scheme.
	clusterUID string
}

// newV2IngressFrontendNamer returns a v2 frontend namer for given ingress, cluster uid and prefix.
// Example:
// For Ingress - namespace/ingress, clusterUID - uid0123, prefix - k8s
// The resource names are -
// LoadBalancer          : uid0123-namespace-ingress-cysix1wq
// HTTP Forwarding Rule  : k8s2-fr-uid0123-namespace-ingress-cysix1wq
// HTTPS Forwarding Rule : k8s2-fs-uid0123-namespace-ingress-cysix1wq
// Target HTTP Proxy     : k8s2-tp-uid0123-namespace-ingress-cysix1wq
// Target HTTPS Proxy    : k8s2-ts-uid0123-namespace-ingress-cysix1wq
// URL Map               : k8s2-um-uid0123-namespace-ingress-cysix1wq
// SSL Certificate       : k8s2-cr-uid0123-<lb-hash>-<secret-hash>
func newV2IngressFrontendNamer(ing *v1beta1.Ingress, clusterUID string, prefix string) IngressFrontendNamer {
	namer := &V2IngressFrontendNamer{ing: ing, prefix: prefix, clusterUID: clusterUID}
	// Initialize LbName.
	truncFields := TrimFieldsEvenly(maximumAllowedCombinedLength, ing.Namespace, ing.Name)
	truncNamespace := truncFields[0]
	truncName := truncFields[1]
	suffix := namer.suffix(clusterUID, ing.Namespace, ing.Name)
	namer.lbName = fmt.Sprintf("%s-%s-%s-%s", clusterUID, truncNamespace, truncName, suffix)
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

// LbName returns loadbalancer name.
// Note that this is used for generating GCE resource names.
func (vn *V2IngressFrontendNamer) LbName() string {
	return vn.lbName
}

// setLbName sets loadbalancer name.
func (vn *V2IngressFrontendNamer) setLbName() {
	truncFields := TrimFieldsEvenly(maximumAllowedCombinedLength, vn.ing.Namespace, vn.ing.Name)
	truncNamespace := truncFields[0]
	truncName := truncFields[1]
	suffix := vn.suffix(vn.clusterUID, vn.ing.Namespace, vn.ing.Name)
	vn.lbName = fmt.Sprintf("%s-%s-%s-%s", vn.clusterUID, truncNamespace, truncName, suffix)
}

// suffix returns hash string of length 8 of a concatenated string generated from
// uid (an 8 character hash of kube-system uid), namespace and name.
// These fields define an ingress/ load-balancer uniquely.
func (vn *V2IngressFrontendNamer) suffix(uid, namespace, name string) string {
	lbString := strings.Join([]string{uid, namespace, name}, ";")
	return common.ContentHash(lbString, 8)
}

// lbNameToHash returns hash string of length 16 of lbName.
func (vn *V2IngressFrontendNamer) lbNameToHash() string {
	return common.ContentHash(vn.lbName, 16)
}

// FrontendNamerFactory implements IngressFrontendNamerFactory.
type FrontendNamerFactory struct {
	namer *Namer
	// clusterUID is an 8 character hash of kube-system UID that is included
	// in resource names.
	// Note that this is used only for V2IngressFrontendNamer.
	clusterUID string
}

// NewFrontendNamerFactory returns IngressFrontendNamerFactory given a v1 namer and uid.
func NewFrontendNamerFactory(namer *Namer, uid types.UID) IngressFrontendNamerFactory {
	clusterUID := common.ContentHash(string(uid), clusterUIDLength)
	return &FrontendNamerFactory{namer: namer, clusterUID: clusterUID}
}

// Namer implements IngressFrontendNamerFactory.
func (rn *FrontendNamerFactory) Namer(ing *v1beta1.Ingress) IngressFrontendNamer {
	namingScheme := FrontendNamingScheme(ing)
	switch namingScheme {
	case V1NamingScheme:
		return newV1IngressFrontendNamer(ing, rn.namer)
	case V2NamingScheme:
		return newV2IngressFrontendNamer(ing, rn.clusterUID, rn.namer.prefix)
	default:
		klog.Errorf("Unexpected frontend naming scheme %s", namingScheme)
		return newV1IngressFrontendNamer(ing, rn.namer)
	}
}

// NamerForLbName implements IngressFrontendNamerFactory.
func (rn *FrontendNamerFactory) NamerForLbName(lbName string) IngressFrontendNamer {
	return newV1IngressFrontendNamerFromLBName(lbName, rn.namer)
}
