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
	compute "google.golang.org/api/compute/v1"
)

// LoadBalancers is an interface for managing all the gce resources needed by L7
// loadbalancers. We don't have individual pools for each of these resources
// because none of them are usable (or acquirable) stand-alone, unlinke backends
// and instance groups. The dependency graph:
// ForwardingRule -> UrlMaps -> TargetProxies
type LoadBalancers interface {
	// Forwarding Rules
	GetGlobalForwardingRule(name string) (*compute.ForwardingRule, error)
	CreateGlobalForwardingRule(rule *compute.ForwardingRule) error
	DeleteGlobalForwardingRule(name string) error
	SetProxyForGlobalForwardingRule(fw, proxy string) error
	ListGlobalForwardingRules() ([]*compute.ForwardingRule, error)

	// UrlMaps
	GetURLMap(name string) (*compute.UrlMap, error)
	CreateURLMap(urlMap *compute.UrlMap) error
	UpdateURLMap(urlMap *compute.UrlMap) error
	DeleteURLMap(name string) error
	ListURLMaps() ([]*compute.UrlMap, error)

	// TargetProxies
	GetTargetHTTPProxy(name string) (*compute.TargetHttpProxy, error)
	CreateTargetHTTPProxy(proxy *compute.TargetHttpProxy) error
	DeleteTargetHTTPProxy(name string) error
	SetURLMapForTargetHTTPProxy(proxy *compute.TargetHttpProxy, urlMapLink string) error

	// TargetHttpsProxies
	GetTargetHTTPSProxy(name string) (*compute.TargetHttpsProxy, error)
	CreateTargetHTTPSProxy(proxy *compute.TargetHttpsProxy) error
	DeleteTargetHTTPSProxy(name string) error
	SetURLMapForTargetHTTPSProxy(proxy *compute.TargetHttpsProxy, urlMapLink string) error
	SetSslCertificateForTargetHTTPSProxy(proxy *compute.TargetHttpsProxy, sslCertURLs []string) error

	// SslCertificates
	GetSslCertificate(name string) (*compute.SslCertificate, error)
	ListSslCertificates() ([]*compute.SslCertificate, error)
	CreateSslCertificate(certs *compute.SslCertificate) (*compute.SslCertificate, error)
	DeleteSslCertificate(name string) error

	// Static IP

	ReserveGlobalAddress(addr *compute.Address) error
	GetGlobalAddress(name string) (*compute.Address, error)
	DeleteGlobalAddress(name string) error
}

// LoadBalancerPool is an interface to manage the cloud resources associated
// with a gce loadbalancer.
type LoadBalancerPool interface {
	Ensure(ri *L7RuntimeInfo) (*L7, error)
	Delete(name string) error
	GC(names []string) error
	Shutdown() error
	List() ([]string, error)
}
