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
	"time"

	"github.com/golang/glog"
	compute "google.golang.org/api/compute/v1"

	"k8s.io/apimachinery/pkg/util/sets"

	"k8s.io/ingress-gce/pkg/storage"
	"k8s.io/ingress-gce/pkg/utils"
)

// L7s implements LoadBalancerPool.
type L7s struct {
	cloud       LoadBalancers
	snapshotter storage.Snapshotter
	namer       *utils.Namer
}

// Namer returns the namer associated with the L7s.
func (l *L7s) Namer() *utils.Namer {
	return l.namer
}

// NewLoadBalancerPool returns a new loadbalancer pool.
// - cloud: implements LoadBalancers. Used to sync L7 loadbalancer resources
//	 with the cloud.
func NewLoadBalancerPool(cloud LoadBalancers, namer *utils.Namer, resyncWithCloud bool) LoadBalancerPool {
	l7Pool := &L7s{
		cloud: cloud,
		namer: namer,
	}
	if !resyncWithCloud {
		l7Pool.snapshotter = storage.NewInMemoryPool()
	}
	keyFunc := func(i interface{}) (string, error) {
		um := i.(*compute.UrlMap)
		if !namer.NameBelongsToCluster(um.Name) {
			return "", fmt.Errorf("unrecognized name %v", um.Name)
		}
		// Scrub out the UrlMap prefix of the name to get the base LB name.
		return namer.ScrubUrlMapPrefix(um.Name), nil
	}
	l7Pool.snapshotter = storage.NewCloudListingPool("loadbalancers", keyFunc, l7Pool, 30*time.Second)
	return l7Pool
}

// Get implements LoadBalancerPool.
// Note: This is currently only used for testing.
func (l *L7s) Get(name string) bool {
	name = l.namer.LoadBalancer(name)
	_, exists := l.snapshotter.Get(name)
	return exists
}

// Sync implements LoadBalancerPool.
func (l *L7s) Sync(ri *L7RuntimeInfo) (*L7, error) {
	name := l.namer.LoadBalancer(ri.Name)
	glog.V(3).Infof("Sync: LB %s", name)
	lb := &L7{
		runtimeInfo: ri,
		Name:        l.namer.LoadBalancer(ri.Name),
		cloud:       l.cloud,
		namer:       l.namer,
	}

	// Add the lb to the pool, in case we create an UrlMap but run out
	// of quota in creating the ForwardingRule we still need to cleanup
	// the UrlMap during GC.
	defer l.snapshotter.Add(name, lb)

	// Why edge hop for the create?
	// The loadbalancer is a fictitious resource, it doesn't exist in gce. To
	// make it exist we need to create a collection of gce resources, done
	// through the edge hop.
	if err := lb.edgeHop(); err != nil {
		return lb, err
	}

	return lb, nil
}

// Delete implements LoadBalancerPool.
func (l *L7s) Delete(name string) error {
	name = l.namer.LoadBalancer(name)
	glog.V(3).Infof("Deleting lb %v", name)
	if err := Cleanup(name, l.cloud, l.namer); err != nil {
		return err
	}
	l.snapshotter.Delete(name)
	return nil
}

// GC implements LoadBalancerPool.
func (l *L7s) GC(names []string) error {
	glog.V(4).Infof("GC(%v)", names)

	knownLoadBalancers := sets.NewString()
	for _, n := range names {
		knownLoadBalancers.Insert(l.namer.LoadBalancer(n))
	}
	pool := l.snapshotter.Snapshot()

	// Delete unknown loadbalancers
	for name := range pool {
		if knownLoadBalancers.Has(name) {
			continue
		}
		glog.V(2).Infof("GCing loadbalancer %v", name)
		if err := l.Delete(name); err != nil {
			return err
		}
	}

	return nil
}

// Shutdown implemented LoadBalancerPool.
func (l *L7s) Shutdown() error {
	if err := l.GC([]string{}); err != nil {
		return err
	}
	glog.V(2).Infof("Loadbalancer pool shutdown.")
	return nil
}

// List lists all loadbalancers via listing all URLMap's.
func (l *L7s) List() ([]interface{}, error) {
	urlMaps, err := l.cloud.ListUrlMaps()
	if err != nil {
		return nil, err
	}
	var ret []interface{}
	for _, x := range urlMaps {
		ret = append(ret, x)
	}
	return ret, nil
}
