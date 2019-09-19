/*
Copyright 2018 The Kubernetes Authors.

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
	"fmt"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/events"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/loadbalancers/features"
	namer_util "k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/klog"
	"k8s.io/legacy-cloud-providers/gce"
)

// L7s implements LoadBalancerPool.
type L7s struct {
	cloud *gce.Cloud
	// v1NamerHelper is an interface for helper functions for v1 frontend naming scheme.
	v1NamerHelper    namer_util.V1FrontendNamer
	recorderProducer events.RecorderProducer
	// namerFactory creates frontend naming policy for ingress/ load balancer.
	namerFactory namer_util.IngressFrontendNamerFactory
}

// NewLoadBalancerPool returns a new loadbalancer pool.
// - cloud: implements LoadBalancers. Used to sync L7 loadbalancer resources
//	 with the cloud.
func NewLoadBalancerPool(cloud *gce.Cloud, v1NamerHelper namer_util.V1FrontendNamer, recorderProducer events.RecorderProducer, namerFactory namer_util.IngressFrontendNamerFactory) LoadBalancerPool {
	return &L7s{
		cloud:            cloud,
		v1NamerHelper:    v1NamerHelper,
		recorderProducer: recorderProducer,
		namerFactory:     namerFactory,
	}
}

// Ensure ensures a loadbalancer and its resources given the RuntimeInfo
func (l *L7s) Ensure(ri *L7RuntimeInfo) (*L7, error) {
	lb := &L7{
		runtimeInfo: ri,
		cloud:       l.cloud,
		namer:       l.namerFactory.Namer(ri.Ingress),
		recorder:    l.recorderProducer.Recorder(ri.Ingress.Namespace),
		scope:       features.ScopeFromIngress(ri.Ingress),
		ingress:     *ri.Ingress,
	}

	if err := lb.edgeHop(); err != nil {
		return nil, fmt.Errorf("loadbalancer %v does not exist: %v", lb.String(), err)
	}
	return lb, nil
}

// Delete deletes a load balancer by name.
func (l *L7s) Delete(name string, versions *features.ResourceVersions, scope meta.KeyType) error {
	lb := &L7{
		runtimeInfo: &L7RuntimeInfo{},
		cloud:       l.cloud,
		namer:       l.namerFactory.NamerForLbName(name),
		scope:       scope,
	}

	klog.V(3).Infof("Deleting lb %v", lb.String())

	if err := lb.Cleanup(versions); err != nil {
		return err
	}
	return nil
}

// List returns a list of urlMaps (the top level LB resource) that belong to the cluster
func (l *L7s) List(key *meta.Key, version meta.Version) ([]*composite.UrlMap, error) {
	var result []*composite.UrlMap
	urlMaps, err := composite.ListUrlMaps(l.cloud, key, version)
	if err != nil {
		return nil, err
	}

	for _, um := range urlMaps {
		if l.v1NamerHelper.NameBelongsToCluster(um.Name) {
			result = append(result, um)
		}
	}

	return result, nil
}

// GC garbage collects loadbalancers not in the input list.
// TODO(shance): Update to handle regional and global LB with same name
func (l *L7s) GC(names []string) error {
	klog.V(2).Infof("GC(%v)", names)

	knownLoadBalancers := sets.NewString()
	for _, n := range names {
		knownLoadBalancers.Insert(l.v1NamerHelper.LoadBalancer(n))
	}

	// GC L7-ILB LBs if enabled
	if flags.F.EnableL7Ilb {
		key, err := composite.CreateKey(l.cloud, "", meta.Regional)
		if err != nil {
			return fmt.Errorf("error getting regional key: %v", err)
		}
		urlMaps, err := l.List(key, features.L7ILBVersions().UrlMap)
		if err != nil {
			return fmt.Errorf("error listing regional LBs: %v", err)
		}

		if err := l.gc(urlMaps, knownLoadBalancers, features.L7ILBVersions()); err != nil {
			return fmt.Errorf("error gc-ing regional LBs: %v", err)
		}
	}

	// TODO(shance): fix list taking a key
	urlMaps, err := l.List(meta.GlobalKey(""), meta.VersionGA)
	if err != nil {
		return fmt.Errorf("error listing global LBs: %v", err)
	}

	if errors := l.gc(urlMaps, knownLoadBalancers, features.GAResourceVersions); errors != nil {
		return fmt.Errorf("error gcing global LBs: %v", errors)
	}

	return nil
}

// gc is a helper for GC
// TODO(shance): get versions from description
func (l *L7s) gc(urlMaps []*composite.UrlMap, knownLoadBalancers sets.String, versions *features.ResourceVersions) []error {
	var errors []error

	// Delete unknown loadbalancers
	for _, um := range urlMaps {
		nameParts := l.v1NamerHelper.ParseName(um.Name)
		l7Name := l.v1NamerHelper.LoadBalancerFromLbName(nameParts.LbName)

		if knownLoadBalancers.Has(l7Name) {
			klog.V(3).Infof("Load balancer %v is still valid, not GC'ing", l7Name)
			continue
		}

		scope, err := composite.ScopeFromSelfLink(um.SelfLink)
		if err != nil {
			errors = append(errors, fmt.Errorf("error getting scope from self link for urlMap %v: %v", um, err))
			continue
		}

		klog.V(2).Infof("GCing loadbalancer %v", l7Name)
		if err := l.Delete(l7Name, versions, scope); err != nil {
			errors = append(errors, fmt.Errorf("error deleting loadbalancer %q", l7Name))
		}
	}
	return nil
}

// Shutdown logs whether or not the pool is empty.
func (l *L7s) Shutdown() error {
	if err := l.GC([]string{}); err != nil {
		return err
	}
	klog.V(2).Infof("Loadbalancer pool shutdown.")
	return nil
}
