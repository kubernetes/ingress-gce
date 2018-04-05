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
	"reflect"

	"github.com/golang/glog"

	compute "google.golang.org/api/compute/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	"k8s.io/ingress-gce/pkg/backends"
	"k8s.io/ingress-gce/pkg/storage"
	"k8s.io/ingress-gce/pkg/utils"
)

// L7s implements LoadBalancerPool.
type L7s struct {
	cloud       LoadBalancers
	snapshotter storage.Snapshotter
	// TODO: Remove this field and always ask the BackendPool using the NodePort.
	glbcDefaultBackend     *compute.BackendService
	defaultBackendPool     backends.BackendPool
	defaultBackendNodePort backends.ServicePort
	namer                  *utils.Namer
}

// GLBCDefaultBackend returns the BackendService used when no path
// rules match.
func (l *L7s) GLBCDefaultBackend() *compute.BackendService {
	return l.glbcDefaultBackend
}

// Namer returns the namer associated with the L7s.
func (l *L7s) Namer() *utils.Namer {
	return l.namer
}

// NewLoadBalancerPool returns a new loadbalancer pool.
// - cloud: implements LoadBalancers. Used to sync L7 loadbalancer resources
//	 with the cloud.
// - defaultBackendPool: a BackendPool used to manage the GCE BackendService for
//   the default backend.
// - defaultBackendNodePort: The nodePort of the Kubernetes service representing
//   the default backend.
func NewLoadBalancerPool(
	cloud LoadBalancers,
	defaultBackendPool backends.BackendPool,
	defaultBackendNodePort backends.ServicePort, namer *utils.Namer) LoadBalancerPool {
	return &L7s{cloud, storage.NewInMemoryPool(), nil, defaultBackendPool, defaultBackendNodePort, namer}
}

func (l *L7s) create(ri *L7RuntimeInfo) (*L7, error) {
	if l.glbcDefaultBackend == nil {
		glog.Warningf("Creating l7 without a default backend")
	}
	return &L7{
		runtimeInfo:        ri,
		Name:               l.namer.LoadBalancer(ri.Name),
		cloud:              l.cloud,
		glbcDefaultBackend: l.glbcDefaultBackend,
		namer:              l.namer,
		sslCertPrefix:      l.namer.SSLCertPrefix(ri.Name),
	}, nil
}

// Get returns the loadbalancer by name.
func (l *L7s) Get(name string) (*L7, error) {
	name = l.namer.LoadBalancer(name)
	lb, exists := l.snapshotter.Get(name)
	if !exists {
		return nil, fmt.Errorf("loadbalancer %v not in pool", name)
	}
	return lb.(*L7), nil
}

// Add gets or creates a loadbalancer.
// If the loadbalancer already exists, it checks that its edges are valid.
func (l *L7s) Add(ri *L7RuntimeInfo) (err error) {
	name := l.namer.LoadBalancer(ri.Name)

	lb, _ := l.Get(name)
	if lb == nil {
		glog.Infof("Creating l7 %v", name)
		lb, err = l.create(ri)
		if err != nil {
			return err
		}
	} else {
		if !reflect.DeepEqual(lb.runtimeInfo, ri) {
			glog.Infof("LB %v runtime info changed, old %+v new %+v", lb.Name, lb.runtimeInfo, ri)
			lb.runtimeInfo = ri
		}
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
		return err
	}

	return nil
}

// Delete deletes a loadbalancer by name.
func (l *L7s) Delete(name string) error {
	name = l.namer.LoadBalancer(name)
	lb, err := l.Get(name)
	if err != nil {
		return err
	}
	glog.Infof("Deleting lb %v", name)
	if err := lb.Cleanup(); err != nil {
		return err
	}
	l.snapshotter.Delete(name)
	return nil
}

// Sync loadbalancers with the given runtime info from the controller.
func (l *L7s) Sync(lbs []*L7RuntimeInfo) error {
	glog.V(3).Infof("Syncing loadbalancers %v", lbs)

	if len(lbs) != 0 {
		// Lazily create a default backend so we don't tax users who don't care
		// about Ingress by consuming 1 of their 3 GCE BackendServices. This
		// BackendService is GC'd when there are no more Ingresses.
		if err := l.defaultBackendPool.Ensure([]backends.ServicePort{l.defaultBackendNodePort}, nil); err != nil {
			return err
		}
		defaultBackend, err := l.defaultBackendPool.Get(l.defaultBackendNodePort.NodePort, false)
		if err != nil {
			return err
		}
		l.glbcDefaultBackend = defaultBackend.Ga
	}
	// create new loadbalancers, validate existing
	for _, ri := range lbs {
		if err := l.Add(ri); err != nil {
			return err
		}
	}
	return nil
}

// GC garbage collects loadbalancers not in the input list.
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
	// Tear down the default backend when there are no more loadbalancers.
	// This needs to happen after we've deleted all url-maps that might be
	// using it.
	if len(names) == 0 {
		if err := l.defaultBackendPool.Delete(l.defaultBackendNodePort.NodePort); err != nil {
			return err
		}
		l.glbcDefaultBackend = nil
	}
	return nil
}

// Shutdown logs whether or not the pool is empty.
func (l *L7s) Shutdown() error {
	if err := l.GC([]string{}); err != nil {
		return err
	}
	if err := l.defaultBackendPool.Shutdown(); err != nil {
		return err
	}
	glog.Infof("Loadbalancer pool shutdown.")
	return nil
}
