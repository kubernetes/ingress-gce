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
	"k8s.io/client-go/tools/cache"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/legacy-cloud-providers/gce"
)

// TestLocalGetEndpointSet verifies the GetEndpointSet method implemented by the LocalL4ILBEndpointsCalculator.
// The L7 implementation is tested in TestToZoneNetworkEndpointMapUtil.
func TestLocalGetEndpointSet(t *testing.T) {
	t.Parallel()
	mode := negtypes.L4LocalMode
	for _, use_endpoint_slices := range []bool{true, false} {
		_, transactionSyncer := newL4ILBTestTransactionSyncer(negtypes.NewAdapter(gce.NewFakeGCECloud(gce.DefaultTestClusterValues())), mode, use_endpoint_slices)
		zoneGetter := negtypes.NewFakeZoneGetter()
		nodeLister := listers.NewNodeLister(transactionSyncer.nodeLister)

		testCases := []struct {
			desc                string
			endpointsData       []negtypes.EndpointsData
			endpointSets        map[string]negtypes.NetworkEndpointSet
			networkEndpointType negtypes.NetworkEndpointType
			nodeLabelsMap       map[string]map[string]string
			nodeAnnotationsMap  map[string]map[string]string
			nodeReadyStatusMap  map[string]v1.ConditionStatus
			nodeNames           []string
		}{
			{
				desc:          "default endpoints",
				endpointsData: negtypes.EndpointsDataFromEndpointSlices(getDefaultEndpointSlices()),
				// only 4 out of 6 nodes are picked since there are > 4 endpoints, but they are found only on 4 nodes.
				endpointSets: map[string]negtypes.NetworkEndpointSet{
					negtypes.TestZone1: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.1", Node: testInstance1}, negtypes.NetworkEndpoint{IP: "1.2.3.2", Node: testInstance2}),
					negtypes.TestZone2: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.3", Node: testInstance3}, negtypes.NetworkEndpoint{IP: "1.2.3.4", Node: testInstance4}),
				},
				networkEndpointType: negtypes.VmIpEndpointType,
				nodeNames:           []string{testInstance1, testInstance2, testInstance3, testInstance4, testInstance5, testInstance6},
			},
			{
				desc:          "default endpoints, all nodes unready",
				endpointsData: negtypes.EndpointsDataFromEndpointSlices(getDefaultEndpointSlices()),
				// only 4 out of 6 nodes are picked since there are > 4 endpoints, but they are found only on 4 nodes.
				endpointSets: map[string]negtypes.NetworkEndpointSet{
					negtypes.TestZone1: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.1", Node: testInstance1}, negtypes.NetworkEndpoint{IP: "1.2.3.2", Node: testInstance2}),
					negtypes.TestZone2: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.3", Node: testInstance3}, negtypes.NetworkEndpoint{IP: "1.2.3.4", Node: testInstance4}),
				},
				networkEndpointType: negtypes.VmIpEndpointType,
				nodeNames:           []string{testInstance1, testInstance2, testInstance3, testInstance4, testInstance5, testInstance6},
				nodeReadyStatusMap: map[string]v1.ConditionStatus{
					testInstance1: v1.ConditionFalse, testInstance2: v1.ConditionFalse, testInstance3: v1.ConditionFalse, testInstance4: v1.ConditionFalse, testInstance5: v1.ConditionFalse, testInstance6: v1.ConditionFalse,
				},
			},
			{
				desc:          "default endpoints, some nodes marked as non-candidates",
				endpointsData: negtypes.EndpointsDataFromEndpointSlices(getDefaultEndpointSlices()),
				nodeLabelsMap: map[string]map[string]string{
					testInstance1: {utils.LabelNodeRoleExcludeBalancer: "true"},
				},
				nodeAnnotationsMap: map[string]map[string]string{
					testInstance3: {utils.GKECurrentOperationAnnotation: fmt.Sprintf("{'Timestamp': '12345', 'Operation': '%s'}", utils.GKEUpgradeOperation)},
					// annotation for testInstance4 will not remove it from endpoints list, since operation value is "random".
					testInstance4: {utils.GKECurrentOperationAnnotation: "{'Timestamp': '12345', 'Operation': 'random'}"},
				},
				nodeNames: []string{testInstance1, testInstance2, testInstance3, testInstance4, testInstance5, testInstance6},
				// only 2 out of 6 nodes are picked since there are > 4 endpoints, but they are found only on 4 nodes. 2 out of those 4 are non-candidates.
				endpointSets: map[string]negtypes.NetworkEndpointSet{
					negtypes.TestZone1: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.2", Node: testInstance2}),
					negtypes.TestZone2: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.4", Node: testInstance4}),
				},
				networkEndpointType: negtypes.VmIpEndpointType,
			},
			{
				desc:          "no endpoints",
				endpointsData: []negtypes.EndpointsData{},
				// No nodes are picked as there are no service endpoints.
				endpointSets:        nil,
				networkEndpointType: negtypes.VmIpEndpointType,
				nodeNames:           []string{testInstance1, testInstance2, testInstance3, testInstance4, testInstance5, testInstance6},
			},
		}
		svcKey := fmt.Sprintf("%s/%s", testServiceName, testServiceNamespace)
		ec := NewLocalL4ILBEndpointsCalculator(nodeLister, zoneGetter, svcKey)
		for _, tc := range testCases {
			createNodes(t, tc.nodeNames, tc.nodeLabelsMap, tc.nodeAnnotationsMap, tc.nodeReadyStatusMap, transactionSyncer.nodeLister)
			retSet, _, err := ec.CalculateEndpoints(tc.endpointsData, nil)
			if err != nil {
				t.Errorf("For case %q, expect nil error, but got %v.", tc.desc, err)
			}
			if !reflect.DeepEqual(retSet, tc.endpointSets) {
				t.Errorf("For case %q, expecting endpoint set %v, but got %v.", tc.desc, tc.endpointSets, retSet)
			}
			deleteNodes(t, tc.nodeNames, transactionSyncer.nodeLister)
		}
	}
}

// TestClusterGetEndpointSet verifies the GetEndpointSet method implemented by the ClusterL4ILBEndpointsCalculator.
func TestClusterGetEndpointSet(t *testing.T) {
	t.Parallel()
	mode := negtypes.L4ClusterMode
	// The "enableEndpointSlices=false" case is enough since this test does not depend on endpoints at all.
	_, transactionSyncer := newL4ILBTestTransactionSyncer(negtypes.NewAdapter(gce.NewFakeGCECloud(gce.DefaultTestClusterValues())), mode, false)
	zoneGetter := negtypes.NewFakeZoneGetter()
	nodeLister := listers.NewNodeLister(transactionSyncer.nodeLister)
	testCases := []struct {
		desc                string
		endpointsData       []negtypes.EndpointsData
		endpointSets        map[string]negtypes.NetworkEndpointSet
		networkEndpointType negtypes.NetworkEndpointType
		nodeLabelsMap       map[string]map[string]string
		nodeAnnotationsMap  map[string]map[string]string
		nodeReadyStatusMap  map[string]v1.ConditionStatus
		nodeNames           []string
	}{
		{
			desc:          "default endpoints",
			endpointsData: negtypes.EndpointsDataFromEndpointSlices(getDefaultEndpointSlices()),
			// all nodes are picked since, in this mode, endpoints running do not need to run on the selected node.
			endpointSets: map[string]negtypes.NetworkEndpointSet{
				negtypes.TestZone1: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.1", Node: testInstance1}, negtypes.NetworkEndpoint{IP: "1.2.3.2", Node: testInstance2}),
				negtypes.TestZone2: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.3", Node: testInstance3}, negtypes.NetworkEndpoint{IP: "1.2.3.4", Node: testInstance4},
					negtypes.NetworkEndpoint{IP: "1.2.3.5", Node: testInstance5}, negtypes.NetworkEndpoint{IP: "1.2.3.6", Node: testInstance6}),
			},
			networkEndpointType: negtypes.VmIpEndpointType,
			nodeNames:           []string{testInstance1, testInstance2, testInstance3, testInstance4, testInstance5, testInstance6},
		},
		{
			desc:          "default endpoints, all nodes unready",
			endpointsData: negtypes.EndpointsDataFromEndpointSlices(getDefaultEndpointSlices()),
			// all nodes are picked since, in this mode, endpoints running do not need to run on the selected node.
			endpointSets: map[string]negtypes.NetworkEndpointSet{
				negtypes.TestZone1: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.1", Node: testInstance1}, negtypes.NetworkEndpoint{IP: "1.2.3.2", Node: testInstance2}),
				negtypes.TestZone2: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.3", Node: testInstance3}, negtypes.NetworkEndpoint{IP: "1.2.3.4", Node: testInstance4},
					negtypes.NetworkEndpoint{IP: "1.2.3.5", Node: testInstance5}, negtypes.NetworkEndpoint{IP: "1.2.3.6", Node: testInstance6}),
			},
			networkEndpointType: negtypes.VmIpEndpointType,
			nodeNames:           []string{testInstance1, testInstance2, testInstance3, testInstance4, testInstance5, testInstance6},
			nodeReadyStatusMap: map[string]v1.ConditionStatus{
				testInstance1: v1.ConditionFalse, testInstance2: v1.ConditionFalse, testInstance3: v1.ConditionFalse, testInstance4: v1.ConditionFalse, testInstance5: v1.ConditionFalse, testInstance6: v1.ConditionFalse,
			},
		},
		{
			desc:          "default endpoints, some nodes marked as non-candidates",
			endpointsData: negtypes.EndpointsDataFromEndpointSlices(getDefaultEndpointSlices()),
			// all valid candidate nodes are picked since, in this mode, endpoints running do not need to run on the selected node.
			endpointSets: map[string]negtypes.NetworkEndpointSet{
				negtypes.TestZone1: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.2", Node: testInstance2}),
				negtypes.TestZone2: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.4", Node: testInstance4},
					negtypes.NetworkEndpoint{IP: "1.2.3.5", Node: testInstance5}, negtypes.NetworkEndpoint{IP: "1.2.3.6", Node: testInstance6}),
			},
			networkEndpointType: negtypes.VmIpEndpointType,
			nodeNames:           []string{testInstance1, testInstance2, testInstance3, testInstance4, testInstance5, testInstance6},
			nodeLabelsMap: map[string]map[string]string{
				testInstance1: {utils.LabelNodeRoleExcludeBalancer: "true"},
			},
			nodeAnnotationsMap: map[string]map[string]string{
				testInstance3: {utils.GKECurrentOperationAnnotation: fmt.Sprintf("{'Timestamp': '12345', 'Operation': '%s'}", utils.GKEUpgradeOperation)},
				// annotation for testInstance4 will not remove it from endpoints list, since operation value is "random".
				testInstance4: {utils.GKECurrentOperationAnnotation: "{'Timestamp': '12345', 'Operation': 'random'}"},
			},
		},
		{
			desc: "no endpoints",
			// all nodes are picked since, in this mode, endpoints running do not need to run on the selected node.
			// Even when there are no service endpoints, nodes are selected at random.
			endpointsData: []negtypes.EndpointsData{},
			endpointSets: map[string]negtypes.NetworkEndpointSet{
				negtypes.TestZone1: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.1", Node: testInstance1}, negtypes.NetworkEndpoint{IP: "1.2.3.2", Node: testInstance2}),
				negtypes.TestZone2: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.3", Node: testInstance3}, negtypes.NetworkEndpoint{IP: "1.2.3.4", Node: testInstance4},
					negtypes.NetworkEndpoint{IP: "1.2.3.5", Node: testInstance5}, negtypes.NetworkEndpoint{IP: "1.2.3.6", Node: testInstance6}),
			},
			networkEndpointType: negtypes.VmIpEndpointType,
			nodeNames:           []string{testInstance1, testInstance2, testInstance3, testInstance4, testInstance5, testInstance6},
		},
	}
	svcKey := fmt.Sprintf("%s/%s", testServiceName, testServiceNamespace)
	ec := NewClusterL4ILBEndpointsCalculator(nodeLister, zoneGetter, svcKey)
	for _, tc := range testCases {
		createNodes(t, tc.nodeNames, tc.nodeLabelsMap, tc.nodeAnnotationsMap, tc.nodeReadyStatusMap, transactionSyncer.nodeLister)
		retSet, _, err := ec.CalculateEndpoints(tc.endpointsData, nil)
		if err != nil {
			t.Errorf("For case %q, expect nil error, but got %v.", tc.desc, err)
		}
		if !reflect.DeepEqual(retSet, tc.endpointSets) {
			t.Errorf("For case %q, expecting endpoint set %v, but got %v.", tc.desc, tc.endpointSets, retSet)
		}
		deleteNodes(t, tc.nodeNames, transactionSyncer.nodeLister)
	}
}

func createNodes(t *testing.T, nodeNames []string, nodeLabels, nodeAnnotations map[string]map[string]string, nodeReadyStatus map[string]v1.ConditionStatus, nodeIndexer cache.Indexer) {
	t.Helper()
	for i, nodeName := range nodeNames {
		var labels, annotations map[string]string
		readyStatus := v1.ConditionTrue
		if nodeLabels != nil {
			labels = nodeLabels[nodeName]
		}
		if nodeAnnotations != nil {
			annotations = nodeAnnotations[nodeName]
		}
		if nodeReadyStatus != nil {
			status, ok := nodeReadyStatus[nodeName]
			if ok {
				readyStatus = status
			}
		}

		err := nodeIndexer.Add(&v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:        nodeName,
				Labels:      labels,
				Annotations: annotations,
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
						Status: readyStatus,
					},
				},
			},
		})
		if err != nil {
			t.Errorf("Failed to add node %s to syncer's nodeLister, err %v", nodeNames[i], err)
		}
	}

}

func deleteNodes(t *testing.T, nodeNames []string, nodeIndexer cache.Indexer) {
	t.Helper()
	for _, nodeName := range nodeNames {
		node, exists, err := nodeIndexer.GetByKey(nodeName)
		if err != nil || !exists {
			t.Errorf("Could not lookuo node %q, err - %v", nodeName, err)
			continue
		}
		if err := nodeIndexer.Delete(node); err != nil {
			t.Errorf("Failed to delete node %q, err - %v", nodeName, err)
		}
	}
}
