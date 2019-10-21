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
	"k8s.io/api/networking/v1beta1"
	"k8s.io/ingress-gce/pkg/utils/common"
)

const (
	V1NamingScheme = Scheme("v1")
	V2NamingScheme = Scheme("v2")
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

// FrontendNamerFactory implements IngressFrontendNamerFactory.
type FrontendNamerFactory struct {
	namer *Namer
}

// NewFrontendNamerFactory returns IngressFrontendNamerFactory given a v1 namer.
func NewFrontendNamerFactory(namer *Namer) IngressFrontendNamerFactory {
	return &FrontendNamerFactory{namer: namer}
}

// Namer implements IngressFrontendNamerFactory.
func (rn *FrontendNamerFactory) Namer(ing *v1beta1.Ingress) IngressFrontendNamer {
	switch frontendNamingScheme(ing) {
	case V1NamingScheme:
		return newV1IngressFrontendNamer(ing, rn.namer)
	case V2NamingScheme:
		// TODO(smatti): return V2 Namer.
		return nil
	default:
		return newV1IngressFrontendNamer(ing, rn.namer)
	}
}

// NamerForLbName implements IngressFrontendNamerFactory.
func (rn *FrontendNamerFactory) NamerForLbName(lbName string) IngressFrontendNamer {
	return newV1IngressFrontendNamerFromLBName(lbName, rn.namer)
}
