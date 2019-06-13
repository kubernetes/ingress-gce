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
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"google.golang.org/api/compute/v1"
	"k8s.io/ingress-gce/pkg/composite"
)

// LoadBalancers is an interface for managing all the gce resources needed by L7
// loadbalancers. We don't have individual pools for each of these resources
// because none of them are usable (or acquirable) stand-alone, unlike backends
// and instance groups. The dependency graph:
// ForwardingRule -> UrlMaps -> TargetProxies
type LoadBalancers interface {
	// ForwardingRules
	CreateForwardingRule(forwardingRule *composite.ForwardingRule, key *meta.Key) error
	GetForwardingRule(version meta.Version, key *meta.Key) (*composite.ForwardingRule, error)
	DeleteForwardingRule(version meta.Version, key *meta.Key) error
	ListForwardingRules(version meta.Version, key *meta.Key) ([]*composite.ForwardingRule, error)
	SetProxyForForwardingRule(forwardingRule *composite.ForwardingRule, key *meta.Key, targetProxyLink string) error

	// TargetHttpProxies
	CreateTargetHttpProxy(targetHttpProxy *composite.TargetHttpProxy, key *meta.Key) error
	GetTargetHttpProxy(version meta.Version, key *meta.Key) (*composite.TargetHttpProxy, error)
	DeleteTargetHttpProxy(version meta.Version, key *meta.Key) error
	SetUrlMapForTargetHttpProxy(proxy *composite.TargetHttpProxy, urlMapLink string, key *meta.Key) error

	// TargetHttpsProxies
	CreateTargetHttpsProxy(targetHttpsProxy *composite.TargetHttpsProxy, key *meta.Key) error
	GetTargetHttpsProxy(version meta.Version, key *meta.Key) (*composite.TargetHttpsProxy, error)
	DeleteTargetHttpsProxy(version meta.Version, key *meta.Key) error
	SetSslCertificateForTargetHttpsProxy(targetHttpsProxy *composite.TargetHttpsProxy, sslCertURLs []string, key *meta.Key) error
	SetUrlMapForTargetHttpsProxy(targetHttpsProxy *composite.TargetHttpsProxy, urlMapLink string, key *meta.Key) error

	// UrlMaps
	CreateUrlMap(urlMap *composite.UrlMap, key *meta.Key) error
	GetUrlMap(version meta.Version, key *meta.Key) (*composite.UrlMap, error)
	UpdateUrlMap(urlMap *composite.UrlMap, key *meta.Key) error
	DeleteUrlMap(version meta.Version, key *meta.Key) error
	ListUrlMaps(version meta.Version, key *meta.Key) ([]*composite.UrlMap, error)

	// SslCertificates
	GetSslCertificate(version meta.Version, key *meta.Key) (*composite.SslCertificate, error)
	CreateSslCertificate(sslCertificate *composite.SslCertificate, key *meta.Key) error
	DeleteSslCertificate(version meta.Version, key *meta.Key) error
	ListSslCertificates(version meta.Version, key *meta.Key) ([]*composite.SslCertificate, error)

	// Static IP
	ReserveGlobalAddress(addr *compute.Address) error
	GetGlobalAddress(name string) (*compute.Address, error)
	DeleteGlobalAddress(name string) error

	// Helpers
	CreateKey(name string, regional bool) *meta.Key
	ListAllUrlMaps() ([]*composite.UrlMap, error)
}

// LoadBalancerPool is an interface to manage the cloud resources associated
// with a gce loadbalancer.
type LoadBalancerPool interface {
	Ensure(ri *L7RuntimeInfo) (*L7, error)
	Delete(name string, regional bool) error
	GC(names []string) error
	Shutdown() error
	List() ([]string, []bool, error)
}
