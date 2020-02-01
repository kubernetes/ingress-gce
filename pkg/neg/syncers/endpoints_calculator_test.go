/*
Copyright 2020 The Kubernetes Authors.

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

package syncers

import (
	"fmt"
	"reflect"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	listers "k8s.io/client-go/listers/core/v1"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/legacy-cloud-providers/gce"
)

// TestLocalGetEndpointSet verifies the GetEndpointSet method implemented by the LocalL4ILBEndpointsCalculator.
// The L7 implementation is tested in TestToZoneNetworkEndpointMapUtil.
func TestLocalGetEndpointSet(t *testing.T) {
	t.Parallel()
	_, transactionSyncer := newL4ILBTestTransactionSyncer(negtypes.NewAdapter(gce.NewFakeGCECloud(gce.DefaultTestClusterValues())), false)
	nodeNames := []string{testInstance1, testInstance2, testInstance3, testInstance4, testInstance5, testInstance6}
	for i := 0; i < len(nodeNames); i++ {
		err := transactionSyncer.nodeLister.Add(&v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				//Namespace: testServiceNamespace,
				Name: nodeNames[i],
			},
			Status: v1.NodeStatus{
				Addresses: []v1.NodeAddress{
					{
						Type:    v1.NodeInternalIP,
						Address: fmt.Sprintf("1.2.3.%d", i+1),
					},
				},
				Conditions: []v1.NodeCondition{
					{
						Type:   v1.NodeReady,
						Status: v1.ConditionTrue,
					},
				},
			},
		})
		if err != nil {
			t.Errorf("Failed to add node %s to syncer's nodeLister, err %v", nodeNames[i], err)
		}
	}
	zoneGetter := negtypes.NewFakeZoneGetter()
	nodeLister := listers.NewNodeLister(transactionSyncer.nodeLister)

	testCases := []struct {
		desc                string
		endpoints           *v1.Endpoints
		endpointSets        map[string]negtypes.NetworkEndpointSet
		networkEndpointType negtypes.NetworkEndpointType
	}{
		{
			desc:      "default endpoints",
			endpoints: getDefaultEndpoint(),
			// only 4 out of 6 nodes are picked since there are > 4 endpoints, but they are found only on 4 nodes.
			endpointSets: map[string]negtypes.NetworkEndpointSet{
				negtypes.TestZone1: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.1", Node: testInstance1}, negtypes.NetworkEndpoint{IP: "1.2.3.2", Node: testInstance2}),
				negtypes.TestZone2: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.3", Node: testInstance3}, negtypes.NetworkEndpoint{IP: "1.2.3.4", Node: testInstance4}),
			},
			networkEndpointType: negtypes.VmPrimaryIpEndpointType,
		},
		{
			desc:      "no endpoints",
			endpoints: &v1.Endpoints{},
			// No nodes are picked as there are no service endpoints.
			endpointSets:        nil,
			networkEndpointType: negtypes.VmPrimaryIpEndpointType,
		},
	}
	svcKey := fmt.Sprintf("%s/%s", testServiceName, testServiceNamespace)
	ec := NewLocalL4ILBEndpointsCalculator(nodeLister, zoneGetter, svcKey)
	for _, tc := range testCases {
		retSet, _, err := ec.CalculateEndpoints(tc.endpoints, nil)
		if err != nil {
			t.Errorf("For case %q, expect nil error, but got %v.", tc.desc, err)
		}
		if !reflect.DeepEqual(retSet, tc.endpointSets) {
			t.Errorf("For case %q, expecting endpoint set %v, but got %v.", tc.desc, tc.endpointSets, retSet)
		}
	}
}

// TestClusterGetEndpointSet verifies the GetEndpointSet method implemented by the ClusterL4ILBEndpointsCalculator.
func TestClusterGetEndpointSet(t *testing.T) {
	t.Parallel()
	_, transactionSyncer := newL4ILBTestTransactionSyncer(negtypes.NewAdapter(gce.NewFakeGCECloud(gce.DefaultTestClusterValues())), true)
	nodeNames := []string{testInstance1, testInstance2, testInstance3, testInstance4, testInstance5, testInstance6}
	for i := 0; i < len(nodeNames); i++ {
		err := transactionSyncer.nodeLister.Add(&v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: nodeNames[i],
			},
			Status: v1.NodeStatus{
				Addresses: []v1.NodeAddress{
					{
						Type:    v1.NodeInternalIP,
						Address: fmt.Sprintf("1.2.3.%d", i+1),
					},
				},
				Conditions: []v1.NodeCondition{
					{
						Type:   v1.NodeReady,
						Status: v1.ConditionTrue,
					},
				},
			},
		})
		if err != nil {
			t.Errorf("Failed to add node %s to syncer's nodeLister, err %v", nodeNames[i], err)
		}
	}
	zoneGetter := negtypes.NewFakeZoneGetter()
	nodeLister := listers.NewNodeLister(transactionSyncer.nodeLister)
	testCases := []struct {
		desc                string
		endpoints           *v1.Endpoints
		endpointSets        map[string]negtypes.NetworkEndpointSet
		networkEndpointType negtypes.NetworkEndpointType
	}{
		{
			desc:      "default endpoints",
			endpoints: getDefaultEndpoint(),
			// all nodes are picked since, in this mode, endpoints running do not need to run on the selected node.
			endpointSets: map[string]negtypes.NetworkEndpointSet{
				negtypes.TestZone1: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.1", Node: testInstance1}, negtypes.NetworkEndpoint{IP: "1.2.3.2", Node: testInstance2}),
				negtypes.TestZone2: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.3", Node: testInstance3}, negtypes.NetworkEndpoint{IP: "1.2.3.4", Node: testInstance4},
					negtypes.NetworkEndpoint{IP: "1.2.3.5", Node: testInstance5}, negtypes.NetworkEndpoint{IP: "1.2.3.6", Node: testInstance6}),
			},
			networkEndpointType: negtypes.VmPrimaryIpEndpointType,
		},
		{
			desc: "no endpoints",
			// all nodes are picked since, in this mode, endpoints running do not need to run on the selected node.
			// Even when there are no service endpoints, nodes are selected at random.
			endpoints: &v1.Endpoints{},
			endpointSets: map[string]negtypes.NetworkEndpointSet{
				negtypes.TestZone1: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.1", Node: testInstance1}, negtypes.NetworkEndpoint{IP: "1.2.3.2", Node: testInstance2}),
				negtypes.TestZone2: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.3", Node: testInstance3}, negtypes.NetworkEndpoint{IP: "1.2.3.4", Node: testInstance4},
					negtypes.NetworkEndpoint{IP: "1.2.3.5", Node: testInstance5}, negtypes.NetworkEndpoint{IP: "1.2.3.6", Node: testInstance6}),
			},
			networkEndpointType: negtypes.VmPrimaryIpEndpointType,
		},
	}
	svcKey := fmt.Sprintf("%s/%s", testServiceName, testServiceNamespace)
	ec := NewClusterL4ILBEndpointsCalculator(nodeLister, zoneGetter, svcKey)
	for _, tc := range testCases {
		retSet, _, err := ec.CalculateEndpoints(tc.endpoints, nil)
		if err != nil {
			t.Errorf("For case %q, expect nil error, but got %v.", tc.desc, err)
		}
		if !reflect.DeepEqual(retSet, tc.endpointSets) {
			t.Errorf("For case %q, expecting endpoint set %v, but got %v.", tc.desc, tc.endpointSets, retSet)
		}
	}
}
