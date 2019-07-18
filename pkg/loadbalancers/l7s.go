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
	"k8s.io/ingress-gce/pkg/loadbalancers/features"
	"k8s.io/kubernetes/pkg/cloudprovider/providers/gce"

	"k8s.io/klog"

	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/events"
	"k8s.io/ingress-gce/pkg/utils"
)

// L7s implements LoadBalancerPool.
type L7s struct {
	cloud            *gce.Cloud
	namer            *utils.Namer
	recorderProducer events.RecorderProducer
}

// Namer returns the namer associated with the L7s.
func (l *L7s) Namer() *utils.Namer {
	return l.namer
}

// NewLoadBalancerPool returns a new loadbalancer pool.
// - cloud: implements LoadBalancers. Used to sync L7 loadbalancer resources
//	 with the cloud.
func NewLoadBalancerPool(cloud *gce.Cloud, namer *utils.Namer, recorderProducer events.RecorderProducer) LoadBalancerPool {
	return &L7s{
		cloud:            cloud,
		namer:            namer,
		recorderProducer: recorderProducer,
	}
}

// Ensure ensures a loadbalancer and its resources given the RuntimeInfo
func (l *L7s) Ensure(ri *L7RuntimeInfo) (*L7, error) {
	lb := &L7{
		runtimeInfo: ri,
		Name:        l.namer.LoadBalancer(ri.Name),
		cloud:       l.cloud,
		namer:       l.namer,
		recorder:    l.recorderProducer.Recorder(ri.Ingress.Namespace),
		version:     features.VersionFromIngress(ri.Ingress),
		scope:       features.ScopeFromIngress(ri.Ingress),
	}

	if err := lb.edgeHop(); err != nil {
		return nil, fmt.Errorf("loadbalancer %v does not exist: %v", lb.Name, err)
	}
	return lb, nil
}

// Delete deletes a load balancer by name.
func (l *L7s) Delete(name string, version meta.Version, scope meta.KeyType) error {
	lb := &L7{
		runtimeInfo: &L7RuntimeInfo{Name: name},
		Name:        l.namer.LoadBalancer(name),
		cloud:       l.cloud,
		namer:       l.namer,
		scope:       scope,
		version:     version,
	}

	klog.V(3).Infof("Deleting lb %v", lb.Name)
	if err := lb.Cleanup(); err != nil {
		return err
	}
	return nil
}

// List returns a list of names of L7 resources, by listing all URL maps and
// deriving the Loadbalancer name from the URL map name
func (l *L7s) List() ([]string, []meta.KeyType, error) {
	var names []string
	var scopes []meta.KeyType

	urlMaps, err := composite.ListAllUrlMaps(l.cloud)
	if err != nil {
		return nil, nil, err
	}

	for _, um := range urlMaps {
		if l.namer.NameBelongsToCluster(um.Name) {
			nameParts := l.namer.ParseName(um.Name)
			l7Name := l.namer.LoadBalancerFromLbName(nameParts.LbName)
			names = append(names, l7Name)
			scope, err := composite.ScopeFromSelfLink(um.SelfLink)
			if err != nil {
				return nil, nil, err
			}
			scopes = append(scopes, scope)
		}
	}

	return names, scopes, nil
}

// GC garbage collects loadbalancers not in the input list.
func (l *L7s) GC(names []string) error {
	klog.V(2).Infof("GC(%v)", names)

	knownLoadBalancers := sets.NewString()
	for _, n := range names {
		knownLoadBalancers.Insert(l.namer.LoadBalancer(n))
	}
	pool, scopes, err := l.List()
	if err != nil {
		return err
	}

	// Delete unknown loadbalancers
	for i, name := range pool {
		if knownLoadBalancers.Has(name) {
			continue
		}
		klog.V(2).Infof("GCing loadbalancer %v", name)

		version := meta.VersionGA
		// TODO: (shance) figure out a cleaner way to determine this
		// Regional resources are alpha only
		if scopes[i] != meta.Global {
			version = meta.VersionAlpha
		}

		if err := l.Delete(name, version, scopes[i]); err != nil {
			return err
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
