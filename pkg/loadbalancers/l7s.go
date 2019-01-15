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

	mcrt "github.com/GoogleCloudPlatform/gke-managed-certs/pkg/clientgen/listers/gke.googleapis.com/v1alpha1"
	"github.com/golang/glog"

	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/ingress-gce/pkg/events"
	"k8s.io/ingress-gce/pkg/utils"
)

// L7s implements LoadBalancerPool.
type L7s struct {
	cloud            LoadBalancers
	namer            *utils.Namer
	mcrt             mcrt.ManagedCertificateLister
	recorderProducer events.RecorderProducer
}

// Namer returns the namer associated with the L7s.
func (l *L7s) Namer() *utils.Namer {
	return l.namer
}

// NewLoadBalancerPool returns a new loadbalancer pool.
// - cloud: implements LoadBalancers. Used to sync L7 loadbalancer resources
//	 with the cloud.
func NewLoadBalancerPool(cloud LoadBalancers, namer *utils.Namer, mcrt mcrt.ManagedCertificateLister, recorderProducer events.RecorderProducer) LoadBalancerPool {
	return &L7s{
		cloud:            cloud,
		namer:            namer,
		mcrt:             mcrt,
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
		mcrt:        l.mcrt,
		recorder:    l.recorderProducer.Recorder(ri.Ingress.Namespace),
	}

	if err := lb.edgeHop(); err != nil {
		return nil, fmt.Errorf("loadbalancer %v does not exist: %v", lb.Name, err)
	}
	return lb, nil
}

// Delete deletes a load balancer by name.
func (l *L7s) Delete(name string) error {
	lbName := l.namer.LoadBalancer(name)
	glog.V(3).Infof("Deleting lb %v", lbName)
	if err := Cleanup(lbName, l.cloud, l.namer); err != nil {
		return err
	}
	return nil
}

// List returns a list of names of L7 resources, by listing all URL maps and
// deriving the Loadbalancer name from the URL map name
func (l *L7s) List() ([]string, error) {
	var names []string

	urlMaps, err := l.cloud.ListUrlMaps()
	if err != nil {
		return nil, err
	}

	for _, um := range urlMaps {
		if l.namer.NameBelongsToCluster(um.Name) {
			nameParts := l.namer.ParseName(um.Name)
			names = append(names, nameParts.LbName)
		}
	}

	return names, nil
}

// GC garbage collects loadbalancers not in the input list.
func (l *L7s) GC(names []string) error {
	glog.V(4).Infof("GC(%v)", names)

	knownLoadBalancers := sets.NewString()
	for _, n := range names {
		knownLoadBalancers.Insert(n)
	}
	pool, err := l.List()
	if err != nil {
		return err
	}

	// Delete unknown loadbalancers
	for _, name := range pool {
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

// Shutdown logs whether or not the pool is empty.
func (l *L7s) Shutdown() error {
	if err := l.GC([]string{}); err != nil {
		return err
	}
	glog.V(2).Infof("Loadbalancer pool shutdown.")
	return nil
}
