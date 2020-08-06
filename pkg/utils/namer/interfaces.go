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
)

// IngressFrontendNamer is an interface to name GCE frontend resources.
type IngressFrontendNamer interface {
	// ForwardingRule returns the name of the gce forwarding rule for given protocol.
	ForwardingRule(protocol NamerProtocol) string
	// TargetProxy returns the name of the gce target proxy for given protocol.
	TargetProxy(protocol NamerProtocol) string
	// UrlMap returns the name of the URL Map.
	UrlMap() string
	// RedirectUrlMap returns the name of the URL Map and if the namer supports naming redirectUrlMap
	RedirectUrlMap() (string, bool)
	// SSLCertName returns the SSL certificate name given secret hash.
	SSLCertName(secretHash string) string
	// IsCertNameForLB returns true if certName belongs to this ingress.
	IsCertNameForLB(certName string) bool
	// IsLegacySSLCert returns true if certName follows the older naming convention
	// and cert is managed by this ingress.
	// old naming convention is of the form k8s-ssl-<lbName> or k8s-ssl-1-<lbName>.
	IsLegacySSLCert(certName string) bool
	// LoadBalancer returns load-balancer name for the ingress.
	LoadBalancer() LoadBalancerName
}

// IngressFrontendNamerFactory is an interface to create a front namer for an Ingress
// a load balancer.
type IngressFrontendNamerFactory interface {
	// Namer returns IngressFrontendNamer for given ingress.
	Namer(ing *v1beta1.Ingress) IngressFrontendNamer
	// NamerForLoadBalancer returns IngressFrontendNamer given a load-balancer
	// name. This used only for v1 naming scheme.
	NamerForLoadBalancer(loadBalancer LoadBalancerName) IngressFrontendNamer
}

// BackendNamer is an interface to name GCE backend resources. It wraps backend
// naming policy of namer.Namer.
type BackendNamer interface {
	// IGBackend constructs the name for a backend service targeting instance groups.
	IGBackend(nodePort int64) string
	// NEG returns the gce neg name based on the service namespace, name
	// and target port.
	NEG(namespace, name string, Port int32) string
	// VMIPNEG returns the gce neg name based on the service namespace and name
	VMIPNEG(namespace, name string) string
	// InstanceGroup constructs the name for an Instance Group.
	InstanceGroup() string
	// NamedPort returns the name for a named port.
	NamedPort(port int64) string
	// NameBelongsToCluster checks if a given backend resource name is tagged with
	// this cluster's UID.
	NameBelongsToCluster(resourceName string) bool
}

// V1FrontendNamer wraps frontend naming policy helper functions of namer.Namer.
type V1FrontendNamer interface {
	// LoadBalancer constructs a loadbalancer name from the given ingress key.
	LoadBalancer(ingKey string) LoadBalancerName
	// LoadBalancerForURLMap returns the loadbalancer name for given URL map.
	LoadBalancerForURLMap(urlMap string) LoadBalancerName
	// NameBelongsToCluster checks if a given frontend resource name is tagged with
	// this cluster's UID.
	NameBelongsToCluster(resourceName string) bool
}
