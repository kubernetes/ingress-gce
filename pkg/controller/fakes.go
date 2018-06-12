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
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/kubernetes/pkg/cloudprovider/providers/gce"

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
	testBackendPort      = intstr.IntOrString{Type: intstr.Int, IntVal: 80}
	testDefaultBeSvcPort = utils.ServicePort{
		ID:       utils.ServicePortID{Service: types.NamespacedName{Namespace: "system", Name: "default"}, Port: testBackendPort},
		NodePort: 30000,
		Protocol: annotations.ProtocolHTTP,
	}
	testSrcRanges      = []string{"1.1.1.1/20"}
	testNodePortRanges = []string{"30000-32767"}
)

// ClusterManager fake
type fakeClusterManager struct {
	*ClusterManager
	fakeLbs      *gce.GCECloud
	fakeBackends *gce.GCECloud
	fakeIGs      *instances.FakeInstanceGroups
	Namer        *utils.Namer
}

// NewFakeClusterManager creates a new fake ClusterManager.
func NewFakeClusterManager(clusterName, firewallName string) *fakeClusterManager {
	fakeGCE := gce.FakeGCECloud(gce.DefaultTestClusterValues())
	namer := utils.NewNamer(clusterName, firewallName)
	fakeIGs := instances.NewFakeInstanceGroups(sets.NewString(), namer)
	fakeHCP := healthchecks.NewFakeHealthCheckProvider()
	fakeNEG := neg.NewFakeNetworkEndpointGroupCloud("test-subnet", "test-network")

	nodePool := instances.NewNodePool(fakeIGs, namer)
	nodePool.Init(&instances.FakeZoneLister{Zones: []string{"zone-a"}})

	healthChecker := healthchecks.NewHealthChecker(fakeHCP, "/", "/healthz", namer, testDefaultBeSvcPort.ID.Service)

	backendPool := backends.NewBackendPool(
		fakeGCE,
		fakeNEG,
		healthChecker, nodePool, namer, false)
	l7Pool := loadbalancers.NewLoadBalancerPool(fakeGCE, namer)
	frPool := firewalls.NewFirewallPool(firewalls.NewFakeFirewallsProvider(false, false), namer, testSrcRanges, testNodePortRanges)
	cm := &ClusterManager{
		ClusterNamer:            namer,
		instancePool:            nodePool,
		backendPool:             backendPool,
		l7Pool:                  l7Pool,
		firewallPool:            frPool,
		defaultBackendSvcPortID: testDefaultBeSvcPort.ID,
	}
	return &fakeClusterManager{cm, fakeGCE, fakeGCE, fakeIGs, namer}
}
