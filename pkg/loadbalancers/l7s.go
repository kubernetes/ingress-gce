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
	"net/http"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"k8s.io/api/networking/v1beta1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/ingress-gce/pkg/common/operator"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/events"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/loadbalancers/features"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/common"
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
	// ingClassLister is the store of IngressClass resources
	ingClassLister cache.Indexer
	// ingClassLister is the store of GCPIngressParams resources
	ingParamsLister cache.Indexer
}

// NewLoadBalancerPool returns a new loadbalancer pool.
// - cloud: implements LoadBalancers. Used to sync L7 loadbalancer resources
//	 with the cloud.
func NewLoadBalancerPool(cloud *gce.Cloud, v1NamerHelper namer_util.V1FrontendNamer, recorderProducer events.RecorderProducer, namerFactory namer_util.IngressFrontendNamerFactory, ingClassLister, ingParamsLister cache.Indexer) LoadBalancerPool {
	return &L7s{
		cloud:            cloud,
		v1NamerHelper:    v1NamerHelper,
		recorderProducer: recorderProducer,
		namerFactory:     namerFactory,
		ingClassLister:   ingClassLister,
		ingParamsLister:  ingParamsLister,
	}
}

// Ensure implements LoadBalancerPool.
func (l *L7s) Ensure(ri *L7RuntimeInfo) (*L7, error) {
	lb := &L7{
		runtimeInfo: ri,
		cloud:       l.cloud,
		namer:       l.namerFactory.Namer(ri.Ingress),
		recorder:    l.recorderProducer.Recorder(ri.Ingress.Namespace),
		scope:       features.ScopeFromIngress(ri.Ingress, ri.IngressParams),
		ingress:     *ri.Ingress,
	}

	if err := lb.edgeHop(); err != nil {
		return nil, fmt.Errorf("loadbalancer %v does not exist: %v", lb.String(), err)
	}
	return lb, nil
}

// delete deletes a loadbalancer by frontend namer.
func (l *L7s) delete(namer namer_util.IngressFrontendNamer, versions *features.ResourceVersions, scope meta.KeyType) error {
	lb := &L7{
		runtimeInfo: &L7RuntimeInfo{},
		cloud:       l.cloud,
		namer:       namer,
		scope:       scope,
	}

	klog.V(2).Infof("Deleting loadbalancer %s", lb.String())

	if err := lb.Cleanup(versions); err != nil {
		return err
	}
	return nil
}

// list returns a list of urlMaps (the top level LB resource) that belong to the cluster.
func (l *L7s) list(key *meta.Key, version meta.Version) ([]*composite.UrlMap, error) {
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

// GCv2 implements LoadBalancerPool.
func (l *L7s) GCv2(ing *v1beta1.Ingress, scope meta.KeyType) error {
	ingKey := common.NamespacedName(ing)
	klog.V(2).Infof("GCv2(%v)", ingKey)
	_, params := utils.GetIngressClassAndParams(ing.Spec.IngressClassName, l.ingClassLister, l.ingParamsLister)
	if err := l.delete(l.namerFactory.Namer(ing), features.VersionsFromIngress(ing, params), scope); err != nil {
		return err
	}
	klog.V(2).Infof("GCv2(%v) ok", ingKey)
	return nil
}

// FrontendScopeChangeGC returns the scope to GC if the LB has changed scopes
// (e.g. when a user migrates from ILB to ELB on the same ingress or vice versa.)
// This only applies to the V2 Naming Scheme
// TODO(shance): Refactor to avoid calling GCE every sync loop
func (l *L7s) FrontendScopeChangeGC(ing *v1beta1.Ingress) (*meta.KeyType, error) {
	if ing == nil {
		return nil, nil
	}

	namer := l.namerFactory.Namer(ing)
	urlMapName := namer.UrlMap()
	_, params := utils.GetIngressClassAndParams(ing.Spec.IngressClassName, l.ingClassLister, l.ingParamsLister)
	currentScope := features.ScopeFromIngress(ing, params)

	for _, scope := range []meta.KeyType{meta.Global, meta.Regional} {
		if scope != currentScope {
			key, err := composite.CreateKey(l.cloud, urlMapName, scope)
			if err != nil {
				return nil, err
			}

			// Look for existing LBs with the same name but of a different scope
			_, err = composite.GetUrlMap(l.cloud, key, features.VersionsFromIngress(ing, params).UrlMap)
			if err == nil {
				klog.V(2).Infof("GC'ing ing %v for scope %q", ing, scope)
				return &scope, nil
			}
			if !utils.IsHTTPErrorCode(err, http.StatusNotFound) {
				return nil, err
			}
		}
	}
	return nil, nil
}

// GCv1 implements LoadBalancerPool.
// TODO(shance): Update to handle regional and global LB with same name
func (l *L7s) GCv1(names []string) error {
	klog.V(2).Infof("GCv1(%v)", names)

	knownLoadBalancers := make(map[namer_util.LoadBalancerName]bool)
	for _, n := range names {
		knownLoadBalancers[l.v1NamerHelper.LoadBalancer(n)] = true
	}

	// GC L7-ILB LBs if enabled
	if flags.F.EnableL7Ilb {
		key, err := composite.CreateKey(l.cloud, "", meta.Regional)
		if err != nil {
			return fmt.Errorf("error getting regional key: %v", err)
		}
		urlMaps, err := l.list(key, features.L7ILBVersions().UrlMap)
		if err != nil {
			return fmt.Errorf("error listing regional LBs: %v", err)
		}

		if err := l.gc(urlMaps, knownLoadBalancers, features.L7ILBVersions()); err != nil {
			return fmt.Errorf("error gc-ing regional LBs: %v", err)
		}
	}

	// TODO(shance): fix list taking a key
	urlMaps, err := l.list(meta.GlobalKey(""), meta.VersionGA)
	if err != nil {
		return fmt.Errorf("error listing global LBs: %v", err)
	}

	if errors := l.gc(urlMaps, knownLoadBalancers, features.GAResourceVersions); errors != nil {
		return fmt.Errorf("error gcing global LBs: %v", errors)
	}

	return nil
}

// gc is a helper for GCv1.
// TODO(shance): get versions from description
func (l *L7s) gc(urlMaps []*composite.UrlMap, knownLoadBalancers map[namer_util.LoadBalancerName]bool, versions *features.ResourceVersions) []error {
	var errors []error

	// Delete unknown loadbalancers
	for _, um := range urlMaps {
		l7Name := l.v1NamerHelper.LoadBalancerForURLMap(um.Name)

		if knownLoadBalancers[l7Name] {
			klog.V(3).Infof("Load balancer %v is still valid, not GC'ing", l7Name)
			continue
		}

		scope, err := composite.ScopeFromSelfLink(um.SelfLink)
		if err != nil {
			errors = append(errors, fmt.Errorf("error getting scope from self link for urlMap %v: %v", um, err))
			continue
		}

		if err := l.delete(l.namerFactory.NamerForLoadBalancer(l7Name), versions, scope); err != nil {
			errors = append(errors, fmt.Errorf("error deleting loadbalancer %q: %v", l7Name, err))
		}
	}
	return errors
}

// Shutdown implements LoadBalancerPool.
func (l *L7s) Shutdown(ings []*v1beta1.Ingress) error {
	// Delete ingresses that use v1 naming scheme.
	if err := l.GCv1([]string{}); err != nil {
		return fmt.Errorf("error deleting load-balancers for v1 naming policy: %v", err)
	}
	// Delete ingresses that use v2 naming policy.
	var errs []error
	v2Ings := operator.Ingresses(ings).Filter(func(ing *v1beta1.Ingress) bool {
		return namer_util.FrontendNamingScheme(ing) == namer_util.V2NamingScheme
	}).AsList()
	for _, ing := range v2Ings {
		_, params := utils.GetIngressClassAndParams(ing.Spec.IngressClassName, l.ingClassLister, l.ingParamsLister)
		if err := l.GCv2(ing, features.ScopeFromIngress(ing, params)); err != nil {
			errs = append(errs, err)
		}
	}
	if errs != nil {
		return fmt.Errorf("error deleting load-balancers for v2 naming policy: %v", utils.JoinErrs(errs))
	}
	klog.V(2).Infof("Loadbalancer pool shutdown.")
	return nil
}

// HasUrlMap implements LoadBalancerPool.
func (l *L7s) HasUrlMap(ing *v1beta1.Ingress) (bool, error) {
	namer := l.namerFactory.Namer(ing)
	_, params := utils.GetIngressClassAndParams(ing.Spec.IngressClassName, l.ingClassLister, l.ingParamsLister)
	key, err := composite.CreateKey(l.cloud, namer.UrlMap(), features.ScopeFromIngress(ing, params))
	if err != nil {
		return false, err
	}
	if _, err := composite.GetUrlMap(l.cloud, key, features.VersionsFromIngress(ing, params).UrlMap); err != nil {
		if utils.IsHTTPErrorCode(err, http.StatusNotFound) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
