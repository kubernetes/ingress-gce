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

package controller

import (
	compute "google.golang.org/api/compute/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"

	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/backends"
	"k8s.io/ingress-gce/pkg/firewalls"
	"k8s.io/ingress-gce/pkg/healthchecks"
	"k8s.io/ingress-gce/pkg/instances"
	"k8s.io/ingress-gce/pkg/loadbalancers"
	"k8s.io/ingress-gce/pkg/neg"
	"k8s.io/ingress-gce/pkg/utils"
)

var (
	testDefaultBeSvcPort = utils.ServicePort{
		NodePort: 30000,
		Protocol: annotations.ProtocolHTTP,
		SvcName:  types.NamespacedName{Namespace: "system", Name: "default"},
	}
	testBackendPort    = intstr.IntOrString{Type: intstr.Int, IntVal: 80}
	testSrcRanges      = []string{"1.1.1.1/20"}
	testNodePortRanges = []string{"30000-32767"}
)

// ClusterManager fake
type fakeClusterManager struct {
	*ClusterManager
	fakeLbs      *loadbalancers.FakeLoadBalancers
	fakeBackends *backends.FakeBackendServices
	fakeIGs      *instances.FakeInstanceGroups
	Namer        *utils.Namer
}

// NewFakeClusterManager creates a new fake ClusterManager.
func NewFakeClusterManager(clusterName, firewallName string) *fakeClusterManager {
	namer := utils.NewNamer(clusterName, firewallName)
	fakeLbs := loadbalancers.NewFakeLoadBalancers(clusterName, namer)
	fakeBackends := backends.NewFakeBackendServices(func(op int, be *compute.BackendService) error { return nil }, false)
	fakeIGs := instances.NewFakeInstanceGroups(sets.NewString(), namer)
	fakeHCP := healthchecks.NewFakeHealthCheckProvider()
	fakeNEG := neg.NewFakeNetworkEndpointGroupCloud("test-subnet", "test-network")

	nodePool := instances.NewNodePool(fakeIGs, namer)
	nodePool.Init(&instances.FakeZoneLister{Zones: []string{"zone-a"}})

	healthChecker := healthchecks.NewHealthChecker(fakeHCP, "/", "/healthz", namer, testDefaultBeSvcPort.SvcName)

	backendPool := backends.NewBackendPool(
		fakeBackends,
		fakeNEG,
		healthChecker, nodePool, namer, false)
	l7Pool := loadbalancers.NewLoadBalancerPool(fakeLbs, namer)
	frPool := firewalls.NewFirewallPool(firewalls.NewFakeFirewallsProvider(false, false), namer, testSrcRanges, testNodePortRanges)
	cm := &ClusterManager{
		ClusterNamer:          namer,
		instancePool:          nodePool,
		backendPool:           backendPool,
		l7Pool:                l7Pool,
		firewallPool:          frPool,
		defaultBackendSvcPort: testDefaultBeSvcPort,
	}
	return &fakeClusterManager{cm, fakeLbs, fakeBackends, fakeIGs, namer}
}
