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
	"errors"
	"fmt"
	"reflect"
	"testing"

	networkv1 "github.com/GoogleCloudPlatform/gke-networking-api/apis/network/v1"
	"github.com/google/go-cmp/cmp"
	v1 "k8s.io/api/core/v1"
	discovery "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/neg/metrics/metricscollector"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/ingress-gce/pkg/network"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/zonegetter"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
)

// TestLocalGetEndpointSet verifies the GetEndpointSet method implemented by the LocalL4EndpointsCalculator.
// The L7 implementation is tested in TestToZoneNetworkEndpointMapUtil.
func TestLocalGetEndpointSet(t *testing.T) {
	nodeInformer := zonegetter.FakeNodeInformer()
	zoneGetter, err := zonegetter.NewFakeZoneGetter(nodeInformer, zonegetter.FakeNodeTopologyInformer(), defaultTestSubnetURL, false)
	if err != nil {
		t.Fatalf("failed to initialize zone getter: %v", err)
	}
	zonegetter.PopulateFakeNodeInformer(nodeInformer, false)
	zonegetter.SetNodeTopologyHasSynced(zoneGetter, func() bool { return true })
	defaultNetwork := network.NetworkInfo{IsDefault: true, K8sNetwork: "default", SubnetworkURL: defaultTestSubnetURL}
	prevFlag := flags.F.EnableMultiSubnetCluster
	defer func() { flags.F.EnableMultiSubnetCluster = prevFlag }()
	flags.F.EnableMultiSubnetCluster = false

	testCases := []struct {
		desc                string
		endpointsData       []negtypes.EndpointsData
		wantEndpointSets    map[negtypes.NEGLocation]negtypes.NetworkEndpointSet
		networkEndpointType negtypes.NetworkEndpointType
		nodeLabelsMap       map[string]map[string]string
		nodeAnnotationsMap  map[string]map[string]string
		nodeReadyStatusMap  map[string]v1.ConditionStatus
		nodeNames           []string
		network             network.NetworkInfo
	}{
		{
			desc:          "default endpoints",
			endpointsData: negtypes.EndpointsDataFromEndpointSlices(getDefaultEndpointSlices()),
			// only 4 out of 6 nodes are picked since there are > 4 endpoints, but they are found only on 4 nodes.
			wantEndpointSets: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.1", Node: testInstance1}, negtypes.NetworkEndpoint{IP: "1.2.3.2", Node: testInstance2}),
				{Zone: negtypes.TestZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.3", Node: testInstance3}, negtypes.NetworkEndpoint{IP: "1.2.3.4", Node: testInstance4}),
			},
			networkEndpointType: negtypes.VmIpEndpointType,
			nodeNames:           []string{testInstance1, testInstance2, testInstance3, testInstance4, testInstance5, testInstance6},
			network:             defaultNetwork,
		},
		{
			desc:          "default endpoints, all nodes unready",
			endpointsData: negtypes.EndpointsDataFromEndpointSlices(getDefaultEndpointSlices()),
			// only 4 out of 6 nodes are picked since there are > 4 endpoints, but they are found only on 4 nodes.
			wantEndpointSets: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.1", Node: testInstance1}, negtypes.NetworkEndpoint{IP: "1.2.3.2", Node: testInstance2}),
				{Zone: negtypes.TestZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.3", Node: testInstance3}, negtypes.NetworkEndpoint{IP: "1.2.3.4", Node: testInstance4}),
			},
			networkEndpointType: negtypes.VmIpEndpointType,
			nodeNames:           []string{testInstance1, testInstance2, testInstance3, testInstance4, testInstance5, testInstance6},
			nodeReadyStatusMap: map[string]v1.ConditionStatus{
				testInstance1: v1.ConditionFalse, testInstance2: v1.ConditionFalse, testInstance3: v1.ConditionFalse, testInstance4: v1.ConditionFalse, testInstance5: v1.ConditionFalse, testInstance6: v1.ConditionFalse,
			},
			network: defaultNetwork,
		},
		{
			desc:          "default endpoints, some nodes marked as non-candidates",
			endpointsData: negtypes.EndpointsDataFromEndpointSlices(getDefaultEndpointSlices()),
			nodeLabelsMap: map[string]map[string]string{
				testInstance1: {utils.LabelNodeRoleExcludeBalancer: "true"},
				testInstance3: {utils.GKECurrentOperationLabel: utils.NodeDrain},
				// label for testInstance4 will not remove it from endpoints list, since operation value is "random".
				testInstance4: {utils.GKECurrentOperationLabel: "random"},
			},
			nodeNames: []string{testInstance1, testInstance2, testInstance3, testInstance4, testInstance5, testInstance6},
			// only 2 out of 6 nodes are picked since there are > 4 endpoints, but they are found only on 4 nodes. 2 out of those 4 are non-candidates.
			wantEndpointSets: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.2", Node: testInstance2}),
				{Zone: negtypes.TestZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.4", Node: testInstance4}),
			},
			networkEndpointType: negtypes.VmIpEndpointType,
			network:             defaultNetwork,
		},
		{
			desc:          "no endpoints",
			endpointsData: []negtypes.EndpointsData{},
			// No nodes are picked as there are no service endpoints.
			wantEndpointSets:    nil,
			networkEndpointType: negtypes.VmIpEndpointType,
			nodeNames:           []string{testInstance1, testInstance2, testInstance3, testInstance4, testInstance5, testInstance6},
			network:             defaultNetwork,
		},
		{
			desc:          "multinetwork, endpoints only for nodes connected to a matching non-default network",
			endpointsData: negtypes.EndpointsDataFromEndpointSlices(getDefaultEndpointSlices()),
			network:       network.NetworkInfo{IsDefault: false, K8sNetwork: "other", SubnetworkURL: multinetworkingTestSubnetURL},
			nodeAnnotationsMap: map[string]map[string]string{
				testInstance1: {networkv1.NorthInterfacesAnnotationKey: nodeInterfacesAnnotation(t, "other", "20.2.3.1")},
				testInstance2: {networkv1.NorthInterfacesAnnotationKey: nodeInterfacesAnnotation(t, "other", "20.2.3.2")},
				testInstance3: {networkv1.NorthInterfacesAnnotationKey: nodeInterfacesAnnotation(t, "other", "20.2.3.3")},
			},
			//networkEndpointType: negtypes.VmIpEndpointType,
			nodeNames: []string{testInstance1, testInstance2, testInstance3, testInstance4, testInstance5, testInstance6},
			// only 3 out of 6 nodes are picked because only 3 have multi-nic annotation with a matching network name
			wantEndpointSets: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: negtypes.TestZone1, Subnet: multinetworkingTestSubnetName}: negtypes.NewNetworkEndpointSet(
					negtypes.NetworkEndpoint{IP: "20.2.3.1", Node: testInstance1},
					negtypes.NetworkEndpoint{IP: "20.2.3.2", Node: testInstance2}),
				{Zone: negtypes.TestZone2, Subnet: multinetworkingTestSubnetName}: negtypes.NewNetworkEndpointSet(
					negtypes.NetworkEndpoint{IP: "20.2.3.3", Node: testInstance3}),
			},
		},
		{
			desc:          "endpoints with MSC nodes",
			endpointsData: negtypes.EndpointsDataFromEndpointSlices(getDefaultEndpointSlices()),
			nodeLabelsMap: map[string]map[string]string{
				testInstance1: {utils.LabelNodeSubnet: secondaryTestSubnet1},
				testInstance2: {utils.LabelNodeSubnet: secondaryTestSubnet2},
				testInstance3: {utils.LabelNodeSubnet: secondaryTestSubnet1},
			},
			// only 4 out of 6 nodes are picked since there are > 4 endpoints, but they are found only on 4 nodes.
			wantEndpointSets: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}:    negtypes.NewNetworkEndpointSet(),
				{Zone: negtypes.TestZone1, Subnet: secondaryTestSubnet1}: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.1", Node: testInstance1}),
				{Zone: negtypes.TestZone1, Subnet: secondaryTestSubnet2}: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.2", Node: testInstance2}),
				{Zone: negtypes.TestZone2, Subnet: secondaryTestSubnet1}: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.3", Node: testInstance3}),
				{Zone: negtypes.TestZone2, Subnet: defaultTestSubnet}:    negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.4", Node: testInstance4}),
			},
			networkEndpointType: negtypes.VmIpEndpointType,
			nodeNames:           []string{testInstance1, testInstance2, testInstance3, testInstance4, testInstance5, testInstance6},
			network:             defaultNetwork,
		},
	}
	svcKey := fmt.Sprintf("%s/%s", testServiceName, testServiceNamespace)
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			ec := NewLocalL4EndpointsCalculator(listers.NewNodeLister(nodeInformer.GetIndexer()), zoneGetter, svcKey, klog.TODO(), &tc.network, negtypes.L4InternalLB)
			updateNodes(t, tc.nodeNames, tc.nodeLabelsMap, tc.nodeAnnotationsMap, tc.nodeReadyStatusMap, nodeInformer.GetIndexer())
			retSet, _, _, err := ec.CalculateEndpoints(tc.endpointsData, nil)
			if err != nil {
				t.Errorf("For case %q, expect nil error, but got %v.", tc.desc, err)
			}
			if diff := cmp.Diff(retSet, tc.wantEndpointSets); diff != "" {
				t.Errorf("For case %q, expecting endpoint set %v, but got %v.\nDiff: (-want +got):\n%s", tc.desc, tc.wantEndpointSets, retSet, diff)
			}
			degradedSet, _, err := ec.CalculateEndpointsDegradedMode(tc.endpointsData, nil)
			if err != nil {
				t.Errorf("For degraded mode case %q, expect nil error, but got %v.", tc.desc, err)
			}
			if diff := cmp.Diff(degradedSet, tc.wantEndpointSets); diff != "" {
				t.Errorf("For degraded mode case %q, expecting endpoint set %v, but got %v.\nDiff: (-want +got):\n%s", tc.desc, tc.wantEndpointSets, retSet, diff)
			}
		})
	}
}

func nodeInterfacesAnnotation(t *testing.T, network, ip string) string {
	t.Helper()
	annotation, err := networkv1.MarshalNorthInterfacesAnnotation(networkv1.NorthInterfacesAnnotation{
		{
			Network:   network,
			IpAddress: ip,
		},
	})
	if err != nil {
		t.Errorf("could not create node annotations")
		return ""
	}
	return annotation
}

// TestClusterGetEndpointSet verifies the GetEndpointSet method implemented by the ClusterL4EndpointsCalculator.
func TestClusterGetEndpointSet(t *testing.T) {
	nodeInformer := zonegetter.FakeNodeInformer()
	zoneGetter, err := zonegetter.NewFakeZoneGetter(nodeInformer, zonegetter.FakeNodeTopologyInformer(), defaultTestSubnetURL, false)
	if err != nil {
		t.Fatalf("failed to initialize zone getter: %v", err)
	}
	zonegetter.PopulateFakeNodeInformer(nodeInformer, false)
	zonegetter.SetNodeTopologyHasSynced(zoneGetter, func() bool { return true })
	defaultNetwork := network.NetworkInfo{IsDefault: true, K8sNetwork: "default", SubnetworkURL: defaultTestSubnetURL}
	prevFlag := flags.F.EnableMultiSubnetCluster
	defer func() { flags.F.EnableMultiSubnetCluster = prevFlag }()
	flags.F.EnableMultiSubnetCluster = false

	testCases := []struct {
		desc               string
		endpointsData      []negtypes.EndpointsData
		wantEndpointSets   map[negtypes.NEGLocation]negtypes.NetworkEndpointSet
		nodeLabelsMap      map[string]map[string]string
		nodeAnnotationsMap map[string]map[string]string
		nodeReadyStatusMap map[string]v1.ConditionStatus
		nodeNames          []string
		network            network.NetworkInfo
	}{
		{
			desc:          "default endpoints",
			endpointsData: negtypes.EndpointsDataFromEndpointSlices(getDefaultEndpointSlices()),
			// all nodes are picked since, in this mode, endpoints running do not need to run on the selected node.
			wantEndpointSets: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.1", Node: testInstance1}, negtypes.NetworkEndpoint{IP: "1.2.3.2", Node: testInstance2}),
				{Zone: negtypes.TestZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.3", Node: testInstance3}, negtypes.NetworkEndpoint{IP: "1.2.3.4", Node: testInstance4},
					negtypes.NetworkEndpoint{IP: "1.2.3.5", Node: testInstance5}, negtypes.NetworkEndpoint{IP: "1.2.3.6", Node: testInstance6}),
				{Zone: negtypes.TestZone3, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.7", Node: testUnreadyInstance1}, negtypes.NetworkEndpoint{IP: "1.2.3.8", Node: testUnreadyInstance2}),
			},
			nodeNames: []string{testInstance1, testInstance2, testInstance3, testInstance4, testInstance5, testInstance6},
			network:   defaultNetwork,
		},
		{
			desc:          "default endpoints, all nodes unready",
			endpointsData: negtypes.EndpointsDataFromEndpointSlices(getDefaultEndpointSlices()),
			// all nodes are picked since, in this mode, endpoints running do not need to run on the selected node.
			wantEndpointSets: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.1", Node: testInstance1}, negtypes.NetworkEndpoint{IP: "1.2.3.2", Node: testInstance2}),
				{Zone: negtypes.TestZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.3", Node: testInstance3}, negtypes.NetworkEndpoint{IP: "1.2.3.4", Node: testInstance4},
					negtypes.NetworkEndpoint{IP: "1.2.3.5", Node: testInstance5}, negtypes.NetworkEndpoint{IP: "1.2.3.6", Node: testInstance6}),
				{Zone: negtypes.TestZone3, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.7", Node: testUnreadyInstance1}, negtypes.NetworkEndpoint{IP: "1.2.3.8", Node: testUnreadyInstance2}),
			},
			nodeNames: []string{testInstance1, testInstance2, testInstance3, testInstance4, testInstance5, testInstance6},
			nodeReadyStatusMap: map[string]v1.ConditionStatus{
				testInstance1: v1.ConditionFalse, testInstance2: v1.ConditionFalse, testInstance3: v1.ConditionFalse, testInstance4: v1.ConditionFalse, testInstance5: v1.ConditionFalse, testInstance6: v1.ConditionFalse,
			},
			network: defaultNetwork,
		},
		{
			desc:          "default endpoints, some nodes marked as non-candidates",
			endpointsData: negtypes.EndpointsDataFromEndpointSlices(getDefaultEndpointSlices()),
			// all valid candidate nodes are picked since, in this mode, endpoints running do not need to run on the selected node.
			wantEndpointSets: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.2", Node: testInstance2}),
				{Zone: negtypes.TestZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.4", Node: testInstance4},
					negtypes.NetworkEndpoint{IP: "1.2.3.5", Node: testInstance5}, negtypes.NetworkEndpoint{IP: "1.2.3.6", Node: testInstance6}),
				{Zone: negtypes.TestZone3, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.7", Node: testUnreadyInstance1}, negtypes.NetworkEndpoint{IP: "1.2.3.8", Node: testUnreadyInstance2}),
			},
			nodeNames: []string{testInstance1, testInstance2, testInstance3, testInstance4, testInstance5, testInstance6},
			nodeLabelsMap: map[string]map[string]string{
				testInstance1: {utils.LabelNodeRoleExcludeBalancer: "true"},
				testInstance3: {utils.GKECurrentOperationLabel: utils.NodeDrain},
				// label for testInstance4 will not remove it from endpoints list, since operation value is "random".
				testInstance4: {utils.GKECurrentOperationLabel: "random"},
			},
			network: defaultNetwork,
		},
		{
			desc: "no endpoints",
			// all nodes are picked since, in this mode, endpoints running do not need to run on the selected node.
			// Even when there are no service endpoints, nodes are selected at random.
			endpointsData: []negtypes.EndpointsData{},
			wantEndpointSets: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.1", Node: testInstance1}, negtypes.NetworkEndpoint{IP: "1.2.3.2", Node: testInstance2}),
				{Zone: negtypes.TestZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.3", Node: testInstance3}, negtypes.NetworkEndpoint{IP: "1.2.3.4", Node: testInstance4},
					negtypes.NetworkEndpoint{IP: "1.2.3.5", Node: testInstance5}, negtypes.NetworkEndpoint{IP: "1.2.3.6", Node: testInstance6}),
				{Zone: negtypes.TestZone3, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.7", Node: testUnreadyInstance1}, negtypes.NetworkEndpoint{IP: "1.2.3.8", Node: testUnreadyInstance2}),
			},
			nodeNames: []string{testInstance1, testInstance2, testInstance3, testInstance4, testInstance5, testInstance6},
			network:   defaultNetwork,
		},
		{
			desc:          "multinetwork endpoints, only for nodes connected to the specified network",
			endpointsData: negtypes.EndpointsDataFromEndpointSlices(getDefaultEndpointSlices()),
			wantEndpointSets: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: negtypes.TestZone1, Subnet: multinetworkingTestSubnetName}: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "20.2.3.1", Node: testInstance1}, negtypes.NetworkEndpoint{IP: "20.2.3.2", Node: testInstance2}),
				{Zone: negtypes.TestZone2, Subnet: multinetworkingTestSubnetName}: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "20.2.3.3", Node: testInstance3}, negtypes.NetworkEndpoint{IP: "20.2.3.6", Node: testInstance6}),
			},
			network: network.NetworkInfo{IsDefault: false, K8sNetwork: "other", SubnetworkURL: multinetworkingTestSubnetURL},
			nodeAnnotationsMap: map[string]map[string]string{
				testInstance1: {networkv1.NorthInterfacesAnnotationKey: nodeInterfacesAnnotation(t, "other", "20.2.3.1")},
				testInstance2: {networkv1.NorthInterfacesAnnotationKey: nodeInterfacesAnnotation(t, "other", "20.2.3.2")},
				testInstance3: {networkv1.NorthInterfacesAnnotationKey: nodeInterfacesAnnotation(t, "other", "20.2.3.3")},
				testInstance6: {networkv1.NorthInterfacesAnnotationKey: nodeInterfacesAnnotation(t, "other", "20.2.3.6")},
			},
			nodeNames: []string{testInstance1, testInstance2, testInstance3, testInstance4, testInstance5, testInstance6},
		},
		{
			desc:          "endpoints with MSC nodes",
			endpointsData: negtypes.EndpointsDataFromEndpointSlices(getDefaultEndpointSlices()),
			// all nodes are picked up, but since nodes are spread across subnets there is a separate endpoint set per zone/subnet.
			wantEndpointSets: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}:    negtypes.NewNetworkEndpointSet(),
				{Zone: negtypes.TestZone1, Subnet: secondaryTestSubnet1}: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.1", Node: testInstance1}),
				{Zone: negtypes.TestZone1, Subnet: secondaryTestSubnet2}: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.2", Node: testInstance2}),
				{Zone: negtypes.TestZone2, Subnet: secondaryTestSubnet1}: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.3", Node: testInstance3}),
				{Zone: negtypes.TestZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.4", Node: testInstance4},
					negtypes.NetworkEndpoint{IP: "1.2.3.5", Node: testInstance5}, negtypes.NetworkEndpoint{IP: "1.2.3.6", Node: testInstance6}),
				{Zone: negtypes.TestZone3, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.7", Node: testUnreadyInstance1}, negtypes.NetworkEndpoint{IP: "1.2.3.8", Node: testUnreadyInstance2}),
			},
			nodeNames: []string{testInstance1, testInstance2, testInstance3, testInstance4, testInstance5, testInstance6},
			nodeLabelsMap: map[string]map[string]string{
				testInstance1: {utils.LabelNodeSubnet: secondaryTestSubnet1},
				testInstance2: {utils.LabelNodeSubnet: secondaryTestSubnet2},
				testInstance3: {utils.LabelNodeSubnet: secondaryTestSubnet1},
			},
			network: defaultNetwork,
		},
	}
	svcKey := fmt.Sprintf("%s/%s", testServiceName, testServiceNamespace)
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			ec := NewClusterL4EndpointsCalculator(listers.NewNodeLister(nodeInformer.GetIndexer()), zoneGetter, svcKey, klog.TODO(), &tc.network, negtypes.L4InternalLB)
			updateNodes(t, tc.nodeNames, tc.nodeLabelsMap, tc.nodeAnnotationsMap, tc.nodeReadyStatusMap, nodeInformer.GetIndexer())
			retSet, _, _, err := ec.CalculateEndpoints(tc.endpointsData, nil)
			if err != nil {
				t.Errorf("For case %q, expect nil error, but got %v.", tc.desc, err)
			}
			if !reflect.DeepEqual(retSet, tc.wantEndpointSets) {
				t.Errorf("For case %q, expecting endpoint set %v, but got %v.", tc.desc, tc.wantEndpointSets, retSet)
			}
			degradedSet, _, err := ec.CalculateEndpointsDegradedMode(tc.endpointsData, nil)
			if err != nil {
				t.Errorf("For degraded mode case %q, expect nil error, but got %v.", tc.desc, err)
			}
			if !reflect.DeepEqual(degradedSet, tc.wantEndpointSets) {
				t.Errorf("For degraded mode case %q, expecting endpoint set %v, but got %v.", tc.desc, tc.wantEndpointSets, retSet)
			}
		})
	}
}

// TestClusterWantedNEGsCount verifies logic for
func TestClusterWantedNEGsCount(t *testing.T) {
	// we care only about total numbers in this test
	testCases := []struct {
		desc             string
		internal         bool
		podsCount        int
		currentNEGsCount map[negtypes.NEGLocation]int
		nodesCount       map[negtypes.NEGLocation]int
		wantCount        map[negtypes.NEGLocation]int
	}{
		{
			desc:             "1 zone, 1 pod",
			podsCount:        1,
			currentNEGsCount: map[negtypes.NEGLocation]int{},
			nodesCount: map[negtypes.NEGLocation]int{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: 3,
			},
			wantCount: map[negtypes.NEGLocation]int{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: 3,
			},
		},
		{
			desc:             "4 zones, 1 pod",
			podsCount:        1,
			currentNEGsCount: map[negtypes.NEGLocation]int{},
			nodesCount: map[negtypes.NEGLocation]int{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: 3,
				{Zone: negtypes.TestZone2, Subnet: defaultTestSubnet}: 3,
				{Zone: negtypes.TestZone3, Subnet: defaultTestSubnet}: 3,
				{Zone: negtypes.TestZone4, Subnet: defaultTestSubnet}: 3,
			},
			wantCount: map[negtypes.NEGLocation]int{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: 3,
				{Zone: negtypes.TestZone2, Subnet: defaultTestSubnet}: 3,
				{Zone: negtypes.TestZone3, Subnet: defaultTestSubnet}: 3,
				{Zone: negtypes.TestZone4, Subnet: defaultTestSubnet}: 3,
			},
		},
		{
			desc:             "2 zones, 2 subnets, 1 pod",
			podsCount:        1,
			currentNEGsCount: map[negtypes.NEGLocation]int{},
			nodesCount: map[negtypes.NEGLocation]int{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}:    3,
				{Zone: negtypes.TestZone2, Subnet: defaultTestSubnet}:    3,
				{Zone: negtypes.TestZone1, Subnet: secondaryTestSubnet1}: 3,
				{Zone: negtypes.TestZone2, Subnet: secondaryTestSubnet1}: 3,
			},
			wantCount: map[negtypes.NEGLocation]int{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}:    3,
				{Zone: negtypes.TestZone2, Subnet: defaultTestSubnet}:    3,
				{Zone: negtypes.TestZone1, Subnet: secondaryTestSubnet1}: 3,
				{Zone: negtypes.TestZone2, Subnet: secondaryTestSubnet1}: 3,
			},
		},
		{
			desc:             "1 zone, 1 pod, 3 nodes, ILB",
			podsCount:        1,
			internal:         true,
			currentNEGsCount: map[negtypes.NEGLocation]int{},
			nodesCount: map[negtypes.NEGLocation]int{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: 3,
			},
			wantCount: map[negtypes.NEGLocation]int{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: 3,
			},
		},
		{
			desc:             "1 zone, 1 pod, 100 nodes, ILB",
			podsCount:        1,
			internal:         true,
			currentNEGsCount: map[negtypes.NEGLocation]int{},
			nodesCount: map[negtypes.NEGLocation]int{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: 100,
			},
			wantCount: map[negtypes.NEGLocation]int{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: 24,
			},
		},
		{
			desc:             "1 zone, 50 pods, 100 nodes",
			podsCount:        50,
			currentNEGsCount: map[negtypes.NEGLocation]int{},
			nodesCount: map[negtypes.NEGLocation]int{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: 100,
			},
			wantCount: map[negtypes.NEGLocation]int{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: 50,
			},
		},
		{
			desc:             "1 zone, 100 pods, 100 nodes",
			podsCount:        100,
			currentNEGsCount: map[negtypes.NEGLocation]int{},
			nodesCount: map[negtypes.NEGLocation]int{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: 100,
			},
			wantCount: map[negtypes.NEGLocation]int{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: 100,
			},
		},
		{
			desc:             "1 zone, 150 pods, 100 nodes",
			podsCount:        150,
			currentNEGsCount: map[negtypes.NEGLocation]int{},
			nodesCount: map[negtypes.NEGLocation]int{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: 100,
			},
			wantCount: map[negtypes.NEGLocation]int{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: 100,
			},
		},
		{
			desc:      "1 zone, 100 -> 10 pods, 100 nodes, keep NEGs",
			podsCount: 10,
			currentNEGsCount: map[negtypes.NEGLocation]int{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: 100,
			},
			nodesCount: map[negtypes.NEGLocation]int{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: 100,
			},
			wantCount: map[negtypes.NEGLocation]int{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: 100,
			},
		},
		{
			desc:      "1 zone, 10 -> 10 pods, 100 nodes, keep NEGs",
			podsCount: 10,
			currentNEGsCount: map[negtypes.NEGLocation]int{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: 10,
			},
			nodesCount: map[negtypes.NEGLocation]int{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: 100,
			},
			wantCount: map[negtypes.NEGLocation]int{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: 10,
			},
		},
		{
			desc:      "1 zone, 10 -> 100 pods, 100 nodes, increase NEGs",
			podsCount: 100,
			currentNEGsCount: map[negtypes.NEGLocation]int{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: 10,
			},
			nodesCount: map[negtypes.NEGLocation]int{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: 100,
			},
			wantCount: map[negtypes.NEGLocation]int{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: 100,
			},
		},
		{
			desc:             "3 zone, 10k pods, 30k nodes, NetLB",
			podsCount:        10_000,
			currentNEGsCount: map[negtypes.NEGLocation]int{},
			nodesCount: map[negtypes.NEGLocation]int{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: 10_000,
				{Zone: negtypes.TestZone2, Subnet: defaultTestSubnet}: 10_000,
				{Zone: negtypes.TestZone3, Subnet: defaultTestSubnet}: 10_000,
			},
			// We split 250 between 3 zones
			wantCount: map[negtypes.NEGLocation]int{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: 83,
				{Zone: negtypes.TestZone2, Subnet: defaultTestSubnet}: 83,
				{Zone: negtypes.TestZone3, Subnet: defaultTestSubnet}: 84,
			},
		},
		{
			desc:             "3 zone, 10k pods, 30k nodes, ILB",
			internal:         true,
			podsCount:        10_000,
			currentNEGsCount: map[negtypes.NEGLocation]int{},
			nodesCount: map[negtypes.NEGLocation]int{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: 10_000,
				{Zone: negtypes.TestZone2, Subnet: defaultTestSubnet}: 10_000,
				{Zone: negtypes.TestZone3, Subnet: defaultTestSubnet}: 10_000,
			},
			// We split 24 between 3 zones
			wantCount: map[negtypes.NEGLocation]int{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: 8,
				{Zone: negtypes.TestZone2, Subnet: defaultTestSubnet}: 8,
				{Zone: negtypes.TestZone3, Subnet: defaultTestSubnet}: 8,
			},
		},
		{
			desc:             "3 zone, 10k pods, 30k+1 nodes, ILB",
			internal:         true,
			podsCount:        10_000,
			currentNEGsCount: map[negtypes.NEGLocation]int{},
			nodesCount: map[negtypes.NEGLocation]int{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: 10_000,
				{Zone: negtypes.TestZone2, Subnet: defaultTestSubnet}: 10_001,
				{Zone: negtypes.TestZone3, Subnet: defaultTestSubnet}: 10_000,
			},
			// We split 24 between 3 zones
			wantCount: map[negtypes.NEGLocation]int{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: 8,
				{Zone: negtypes.TestZone2, Subnet: defaultTestSubnet}: 8,
				{Zone: negtypes.TestZone3, Subnet: defaultTestSubnet}: 8,
			},
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			// Arrange
			nodeInformer := zonegetter.FakeNodeInformer()
			zoneGetter, err := zonegetter.NewFakeZoneGetter(nodeInformer, nodeInformer, defaultTestSubnetURL, false)
			if err != nil {
				t.Fatalf("failed to initialize zone getter: %v", err)
			}
			for loc, count := range tC.nodesCount {
				zonegetter.AddFakeNodesCount(zoneGetter, nodeInformer, loc.Zone, loc.Subnet, count)
			}
			zonegetter.SetNodeTopologyHasSynced(zoneGetter, func() bool { return true })
			defaultNetwork := network.NetworkInfo{IsDefault: true, K8sNetwork: "default", SubnetworkURL: defaultTestSubnetURL}
			svcKey := "testSvc"

			prevFlag := flags.F.EnableMultiSubnetCluster
			defer func() { flags.F.EnableMultiSubnetCluster = prevFlag }()
			flags.F.EnableMultiSubnetCluster = false

			lbType := negtypes.L4ExternalLB
			if tC.internal {
				lbType = negtypes.L4InternalLB
			}
			currentMap := make(map[negtypes.NEGLocation]negtypes.NetworkEndpointSet)
			for loc, count := range tC.currentNEGsCount {
				set := negtypes.NewNetworkEndpointSet()
				for i := range count {
					// This will overflow the IP, but for this test this doesn't matter
					set.Insert(negtypes.NetworkEndpoint{IP: fmt.Sprintf("1.2.3.%d", i)})
				}
				currentMap[loc] = set
			}

			eds := make([]negtypes.EndpointsData, tC.podsCount)
			for i := range tC.podsCount {
				eds[i] = negtypes.EndpointsData{
					Addresses: []negtypes.AddressData{
						{
							Ready:    true,
							NodeName: ptr.To("this-is-duplicated-but-thats-okay"),
							TargetRef: &v1.ObjectReference{
								Namespace: testServiceNamespace,
							},
						},
					},
				}
			}

			ec := NewClusterL4EndpointsCalculator(listers.NewNodeLister(nodeInformer.GetIndexer()), zoneGetter, svcKey, klog.TODO(), &defaultNetwork, lbType)

			// Act
			res, _, _, err := ec.CalculateEndpoints(eds, currentMap)

			// Assert
			if err != nil {
				t.Errorf("expected no err, got %v", err)
			}

			resCount := make(map[negtypes.NEGLocation]int)
			for loc, set := range res {
				resCount[loc] = set.Len()
			}
			if diff := cmp.Diff(tC.wantCount, resCount); diff != "" {
				t.Errorf("unexpected result, - want, + got: %s", diff)
			}
		})
	}
}

// TestEndpointsSplitAcrossZonesILB
// We want to verify not only that the number of endpoints is the same,
// but also that the algorithm doesn't provision different nodes each time it is called.
// This is a golden test.
func TestEndpointsSplitAcrossZonesILB(t *testing.T) {
	t.Parallel()

	type stage struct {
		desc  string
		nodes map[string][]*nodeWithSubnet

		want map[negtypes.NEGLocation]negtypes.NetworkEndpointSet
	}

	scenarios := []struct {
		desc   string
		stages []stage
	}{{
		desc: "zonal",
		stages: []stage{{
			desc: "start",
			nodes: map[string][]*nodeWithSubnet{
				"zone1": makeNodes(1000, 10),
			},
			want: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: "zone1", Subnet: "default"}: { // 10 endpoints
					{Node: "node1000"}: {}, {Node: "node1001"}: {}, {Node: "node1002"}: {}, {Node: "node1003"}: {}, {Node: "node1004"}: {},
					{Node: "node1005"}: {}, {Node: "node1006"}: {}, {Node: "node1007"}: {}, {Node: "node1008"}: {}, {Node: "node1009"}: {},
				},
			},
		}, {
			desc: "scale up over limit",
			nodes: map[string][]*nodeWithSubnet{
				"zone1": makeNodes(1000, 50),
			},
			want: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: "zone1", Subnet: "default"}: { // 24 total =
					// 10 existing endpoints
					{Node: "node1000"}: {}, {Node: "node1001"}: {}, {Node: "node1002"}: {}, {Node: "node1003"}: {}, {Node: "node1004"}: {},
					{Node: "node1005"}: {}, {Node: "node1006"}: {}, {Node: "node1007"}: {}, {Node: "node1008"}: {}, {Node: "node1009"}: {},
					// 14 new (sorted)
					{Node: "node1010"}: {}, {Node: "node1012"}: {}, {Node: "node1014"}: {}, {Node: "node1016"}: {}, {Node: "node1020"}: {},
					{Node: "node1021"}: {}, {Node: "node1024"}: {}, {Node: "node1027"}: {}, {Node: "node1029"}: {}, {Node: "node1030"}: {},
					{Node: "node1031"}: {}, {Node: "node1037"}: {}, {Node: "node1038"}: {}, {Node: "node1047"}: {},
				},
			},
		}, {
			desc: "scale up second time",
			nodes: map[string][]*nodeWithSubnet{
				"zone1": makeNodes(1000, 100),
			},
			want: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: "zone1", Subnet: "default"}: { // 24 total - no changes
					{Node: "node1000"}: {}, {Node: "node1001"}: {}, {Node: "node1002"}: {}, {Node: "node1003"}: {}, {Node: "node1004"}: {},
					{Node: "node1005"}: {}, {Node: "node1006"}: {}, {Node: "node1007"}: {}, {Node: "node1008"}: {}, {Node: "node1009"}: {},
					{Node: "node1010"}: {}, {Node: "node1012"}: {}, {Node: "node1014"}: {}, {Node: "node1016"}: {}, {Node: "node1020"}: {},
					{Node: "node1021"}: {}, {Node: "node1024"}: {}, {Node: "node1027"}: {}, {Node: "node1029"}: {}, {Node: "node1030"}: {},
					{Node: "node1031"}: {}, {Node: "node1037"}: {}, {Node: "node1038"}: {}, {Node: "node1047"}: {},
				},
			},
		}, {
			desc: "scale down under limit",
			nodes: map[string][]*nodeWithSubnet{
				"zone1": makeNodes(1000, 20),
			},
			want: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: "zone1", Subnet: "default"}: { // 20 total - compact and delete overhead
					{Node: "node1000"}: {}, {Node: "node1001"}: {}, {Node: "node1002"}: {}, {Node: "node1003"}: {}, {Node: "node1004"}: {},
					{Node: "node1005"}: {}, {Node: "node1006"}: {}, {Node: "node1007"}: {}, {Node: "node1008"}: {}, {Node: "node1009"}: {},
					{Node: "node1010"}: {}, {Node: "node1011"}: {}, {Node: "node1012"}: {}, {Node: "node1013"}: {}, {Node: "node1014"}: {},
					{Node: "node1015"}: {}, {Node: "node1016"}: {}, {Node: "node1017"}: {}, {Node: "node1018"}: {}, {Node: "node1019"}: {},
				},
			},
		}},
	}, {
		desc: "2+1 zones",
		stages: []stage{{
			desc: "start",
			nodes: map[string][]*nodeWithSubnet{
				"zone1": makeNodes(1000, 10),
				"zone2": makeNodes(2000, 10),
			},
			want: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: "zone1", Subnet: "default"}: { // 10 endpoints
					{Node: "node1000"}: {}, {Node: "node1001"}: {}, {Node: "node1002"}: {}, {Node: "node1003"}: {}, {Node: "node1004"}: {},
					{Node: "node1005"}: {}, {Node: "node1006"}: {}, {Node: "node1007"}: {}, {Node: "node1008"}: {}, {Node: "node1009"}: {},
				},
				{Zone: "zone2", Subnet: "default"}: { // 10 endpoints
					{Node: "node2000"}: {}, {Node: "node2001"}: {}, {Node: "node2002"}: {}, {Node: "node2003"}: {}, {Node: "node2004"}: {},
					{Node: "node2005"}: {}, {Node: "node2006"}: {}, {Node: "node2007"}: {}, {Node: "node2008"}: {}, {Node: "node2009"}: {},
				},
			},
		}, {
			desc: "remove nodes from zone",
			nodes: map[string][]*nodeWithSubnet{
				"zone1": makeNodes(1000, 5),
				"zone2": makeNodes(2000, 10),
			},
			want: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: "zone1", Subnet: "default"}: { // 5 endpoints - delete last 5
					{Node: "node1000"}: {}, {Node: "node1001"}: {}, {Node: "node1002"}: {}, {Node: "node1003"}: {}, {Node: "node1004"}: {},
				},
				{Zone: "zone2", Subnet: "default"}: { // 10 endpoints
					{Node: "node2000"}: {}, {Node: "node2001"}: {}, {Node: "node2002"}: {}, {Node: "node2003"}: {}, {Node: "node2004"}: {},
					{Node: "node2005"}: {}, {Node: "node2006"}: {}, {Node: "node2007"}: {}, {Node: "node2008"}: {}, {Node: "node2009"}: {},
				},
			},
		}, {
			desc: "split evenly",
			nodes: map[string][]*nodeWithSubnet{
				"zone1": makeNodes(1000, 20),
				"zone2": makeNodes(2000, 20),
			},
			want: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: "zone1", Subnet: "default"}: {
					// 5 existing endpoints
					{Node: "node1000"}: {}, {Node: "node1001"}: {}, {Node: "node1002"}: {}, {Node: "node1003"}: {}, {Node: "node1004"}: {},
					// 7 new
					{Node: "node1007"}: {}, {Node: "node1009"}: {}, {Node: "node1010"}: {}, {Node: "node1012"}: {}, {Node: "node1014"}: {},
					{Node: "node1016"}: {}, {Node: "node1019"}: {},
				},
				{Zone: "zone2", Subnet: "default"}: {
					// 10 existing endpoints
					{Node: "node2000"}: {}, {Node: "node2001"}: {}, {Node: "node2002"}: {}, {Node: "node2003"}: {}, {Node: "node2004"}: {},
					{Node: "node2005"}: {}, {Node: "node2006"}: {}, {Node: "node2007"}: {}, {Node: "node2008"}: {}, {Node: "node2009"}: {},
					// 2 new
					{Node: "node2011"}: {}, {Node: "node2017"}: {},
				},
			},
		}, {
			desc: "add zone3",
			nodes: map[string][]*nodeWithSubnet{
				"zone1": makeNodes(1000, 20),
				"zone2": makeNodes(2000, 20),
				"zone3": makeNodes(3000, 20),
			},
			want: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: "zone1", Subnet: "default"}: { // Remove to leave 8 endpoints from previous set
					{Node: "node1000"}: {}, {Node: "node1001"}: {}, {Node: "node1002"}: {}, {Node: "node1003"}: {}, {Node: "node1004"}: {},
					{Node: "node1007"}: {}, {Node: "node1009"}: {}, {Node: "node1010"}: {},
				},
				{Zone: "zone2", Subnet: "default"}: { // Remove to leave 8 endpoints from previous set
					{Node: "node2000"}: {}, {Node: "node2001"}: {}, {Node: "node2002"}: {}, {Node: "node2003"}: {}, {Node: "node2004"}: {},
					{Node: "node2005"}: {}, {Node: "node2006"}: {}, {Node: "node2007"}: {},
				},
				{Zone: "zone3", Subnet: "default"}: { // Add 8 new endpoints
					{Node: "node3000"}: {}, {Node: "node3003"}: {}, {Node: "node3009"}: {}, {Node: "node3012"}: {}, {Node: "node3015"}: {},
					{Node: "node3016"}: {}, {Node: "node3017"}: {}, {Node: "node3018"}: {},
				},
			},
		}, {
			desc: "remove zone1",
			nodes: map[string][]*nodeWithSubnet{
				"zone2": makeNodes(2000, 20),
				"zone3": makeNodes(3000, 20),
			},
			want: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: "zone2", Subnet: "default"}: { // 12 total
					// 8 Existing endpoints
					{Node: "node2000"}: {}, {Node: "node2001"}: {}, {Node: "node2002"}: {}, {Node: "node2003"}: {}, {Node: "node2004"}: {},
					{Node: "node2005"}: {}, {Node: "node2006"}: {}, {Node: "node2007"}: {},
					// 4 New endpoints
					{Node: "node2011"}: {}, {Node: "node2015"}: {}, {Node: "node2017"}: {}, {Node: "node2019"}: {},
				},
				{Zone: "zone3", Subnet: "default"}: {
					// 8 Existing endpoints
					{Node: "node3000"}: {}, {Node: "node3003"}: {}, {Node: "node3009"}: {}, {Node: "node3012"}: {}, {Node: "node3015"}: {},
					{Node: "node3016"}: {}, {Node: "node3017"}: {}, {Node: "node3018"}: {},
					// 4 New endpoints
					{Node: "node3002"}: {}, {Node: "node3008"}: {}, {Node: "node3010"}: {}, {Node: "node3011"}: {},
				},
			},
		}},
	}, {
		desc: "4 zone with flickering",
		stages: []stage{{
			desc: "max nodes 1",
			nodes: map[string][]*nodeWithSubnet{
				"zone1": makeNodes(1000, 21),
				"zone2": makeNodes(2000, 20),
				"zone3": makeNodes(3000, 20),
				"zone4": makeNodes(4000, 20),
			},
			want: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: "zone1", Subnet: "default"}: { // 6 endpoints
					{Node: "node1002"}: {}, {Node: "node1003"}: {}, {Node: "node1009"}: {}, {Node: "node1012"}: {}, {Node: "node1014"}: {},
					{Node: "node1016"}: {},
				},
				{Zone: "zone2", Subnet: "default"}: { // 6 endpoints
					{Node: "node2003"}: {}, {Node: "node2004"}: {}, {Node: "node2011"}: {}, {Node: "node2015"}: {}, {Node: "node2017"}: {},
					{Node: "node2019"}: {},
				},
				{Zone: "zone3", Subnet: "default"}: { // 6 endpoints
					{Node: "node3000"}: {}, {Node: "node3003"}: {}, {Node: "node3015"}: {}, {Node: "node3016"}: {}, {Node: "node3017"}: {},
					{Node: "node3018"}: {},
				},
				{Zone: "zone4", Subnet: "default"}: { // 6 endpoints
					{Node: "node4004"}: {}, {Node: "node4007"}: {}, {Node: "node4009"}: {}, {Node: "node4010"}: {}, {Node: "node4017"}: {},
					{Node: "node4018"}: {},
				},
			},
		}, {
			desc: "max nodes 2",
			nodes: map[string][]*nodeWithSubnet{
				"zone1": makeNodes(1000, 21),
				"zone2": makeNodes(2000, 20),
				"zone3": makeNodes(3000, 20),
				"zone4": makeNodes(4000, 20),
			},
			want: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{ // no change
				{Zone: "zone1", Subnet: "default"}: { // 6 endpoints
					{Node: "node1002"}: {}, {Node: "node1003"}: {}, {Node: "node1009"}: {}, {Node: "node1012"}: {}, {Node: "node1014"}: {},
					{Node: "node1016"}: {},
				},
				{Zone: "zone2", Subnet: "default"}: { // 6 endpoints
					{Node: "node2003"}: {}, {Node: "node2004"}: {}, {Node: "node2011"}: {}, {Node: "node2015"}: {}, {Node: "node2017"}: {},
					{Node: "node2019"}: {},
				},
				{Zone: "zone3", Subnet: "default"}: { // 6 endpoints
					{Node: "node3000"}: {}, {Node: "node3003"}: {}, {Node: "node3015"}: {}, {Node: "node3016"}: {}, {Node: "node3017"}: {},
					{Node: "node3018"}: {},
				},
				{Zone: "zone4", Subnet: "default"}: { // 6 endpoints
					{Node: "node4004"}: {}, {Node: "node4007"}: {}, {Node: "node4009"}: {}, {Node: "node4010"}: {}, {Node: "node4017"}: {},
					{Node: "node4018"}: {},
				},
			},
		}, {
			desc: "max nodes 4",
			nodes: map[string][]*nodeWithSubnet{
				"zone1": makeNodes(1000, 20),
				"zone2": makeNodes(2000, 20),
				"zone3": makeNodes(3000, 20),
				"zone4": makeNodes(4000, 21),
			},
			want: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{ // no change
				{Zone: "zone1", Subnet: "default"}: { // 6 endpoints
					{Node: "node1002"}: {}, {Node: "node1003"}: {}, {Node: "node1009"}: {}, {Node: "node1012"}: {}, {Node: "node1014"}: {},
					{Node: "node1016"}: {},
				},
				{Zone: "zone2", Subnet: "default"}: { // 6 endpoints
					{Node: "node2003"}: {}, {Node: "node2004"}: {}, {Node: "node2011"}: {}, {Node: "node2015"}: {}, {Node: "node2017"}: {},
					{Node: "node2019"}: {},
				},
				{Zone: "zone3", Subnet: "default"}: { // 6 endpoints
					{Node: "node3000"}: {}, {Node: "node3003"}: {}, {Node: "node3015"}: {}, {Node: "node3016"}: {}, {Node: "node3017"}: {},
					{Node: "node3018"}: {},
				},
				{Zone: "zone4", Subnet: "default"}: { // 6 endpoints
					{Node: "node4004"}: {}, {Node: "node4007"}: {}, {Node: "node4009"}: {}, {Node: "node4010"}: {}, {Node: "node4017"}: {},
					{Node: "node4018"}: {},
				},
			},
		}},
	}, {
		desc: "5 zones",
		stages: []stage{{
			desc: "split evenly regardless of node count",
			nodes: map[string][]*nodeWithSubnet{
				"zone1": makeNodes(1000, 30),
				"zone2": makeNodes(2000, 40),
				"zone3": makeNodes(3000, 20),
				"zone4": makeNodes(4000, 20),
				"zone5": makeNodes(5000, 20),
			},
			want: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: "zone1", Subnet: "default"}: {
					{Node: "node1009"}: {}, {Node: "node1014"}: {}, {Node: "node1016"}: {}, {Node: "node1021"}: {}, {Node: "node1027"}: {},
				},
				{Zone: "zone2", Subnet: "default"}: {
					{Node: "node2011"}: {}, {Node: "node2017"}: {}, {Node: "node2023"}: {}, {Node: "node2026"}: {}, {Node: "node2031"}: {},
				},
				{Zone: "zone3", Subnet: "default"}: {
					{Node: "node3000"}: {}, {Node: "node3003"}: {}, {Node: "node3015"}: {}, {Node: "node3017"}: {}, {Node: "node3018"}: {},
				},
				{Zone: "zone4", Subnet: "default"}: {
					{Node: "node4004"}: {}, {Node: "node4009"}: {}, {Node: "node4010"}: {}, {Node: "node4017"}: {}, {Node: "node4018"}: {},
				},
				{Zone: "zone5", Subnet: "default"}: {
					{Node: "node5001"}: {}, {Node: "node5002"}: {}, {Node: "node5008"}: {}, {Node: "node5009"}: {}, {Node: "node5012"}: {},
				},
			},
		}, {
			desc: "shift nodes around without requiring changes",
			nodes: map[string][]*nodeWithSubnet{
				"zone1": makeNodes(1000, 28),
				"zone2": makeNodes(2000, 35),
				"zone3": makeNodes(3000, 25),
				"zone4": makeNodes(4000, 25),
				"zone5": makeNodes(5000, 30),
			},
			want: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: "zone1", Subnet: "default"}: {
					{Node: "node1009"}: {}, {Node: "node1014"}: {}, {Node: "node1016"}: {}, {Node: "node1021"}: {}, {Node: "node1027"}: {},
				},
				{Zone: "zone2", Subnet: "default"}: {
					{Node: "node2011"}: {}, {Node: "node2017"}: {}, {Node: "node2023"}: {}, {Node: "node2026"}: {}, {Node: "node2031"}: {},
				},
				{Zone: "zone3", Subnet: "default"}: {
					{Node: "node3000"}: {}, {Node: "node3003"}: {}, {Node: "node3015"}: {}, {Node: "node3017"}: {}, {Node: "node3018"}: {},
				},
				{Zone: "zone4", Subnet: "default"}: {
					{Node: "node4004"}: {}, {Node: "node4009"}: {}, {Node: "node4010"}: {}, {Node: "node4017"}: {}, {Node: "node4018"}: {},
				},
				{Zone: "zone5", Subnet: "default"}: {
					{Node: "node5001"}: {}, {Node: "node5002"}: {}, {Node: "node5008"}: {}, {Node: "node5009"}: {}, {Node: "node5012"}: {},
				},
			},
		}},
	}}

	for _, tc := range scenarios {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			// Arrange
			nodeInformer := zonegetter.FakeNodeInformer()
			zoneGetter, err := zonegetter.NewFakeZoneGetter(nodeInformer, nodeInformer, defaultTestSubnetURL, false)
			if err != nil {
				t.Fatalf("failed to initialize zone getter: %v", err)
			}
			defaultNetwork := network.NetworkInfo{IsDefault: true, K8sNetwork: "default", SubnetworkURL: defaultTestSubnetURL}

			// We use ILB so that it doesn't trigger code that linearly calculates number of NEGs needed
			// based on actual number of Pods.
			c := NewClusterL4EndpointsCalculator(
				listers.NewNodeLister(nodeInformer.GetIndexer()),
				zoneGetter, "svc", klog.TODO(), &defaultNetwork, negtypes.L4InternalLB,
			)

			var endpointsMap map[negtypes.NEGLocation]negtypes.NetworkEndpointSet
			for _, stage := range tc.stages {
				t.Run(stage.desc, func(t *testing.T) {
					// No t.Parallel()

					// Set up nodes
					for zone, nodes := range stage.nodes {
						names := make([]string, 0, len(nodes))
						for _, node := range nodes {
							names = append(names, node.node.Name)
						}
						if err := zonegetter.AddFakeNodes(zoneGetter, zone, names...); err != nil {
							t.Fatalf("failed to add fake node: %v", err)
						}
					}

					// Act
					res, _, _, err := c.CalculateEndpoints(nil, endpointsMap)
					if err != nil {
						t.Fatalf("expected no err, got %v", err)
					}

					// Assert
					if diff := cmp.Diff(stage.want, res); diff != "" {
						t.Errorf("want != got, -want +got:\n%s", diff)
					}
					endpointsMap = stage.want

					// Cleanup
					for zone := range stage.nodes {
						zonegetter.DeleteFakeNodesInZone(t, zone, zoneGetter)
					}
				})
			}
		})
	}
}

// TestEndpointsMigrationFrom25To24ForILBEtpCluster verifies that one endpoint will be removed from LBs that already use 25 endpoints.
// This is so that we use a number that will be divisible equally across zones (24 endpoints for 1,2,3,4 and 6 zones; 25 otherwise)
func TestEndpointsMigrationFrom25To24ForILBEtpCluster(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		desc   string
		nodes  map[string][]*nodeWithSubnet
		before map[negtypes.NEGLocation]negtypes.NetworkEndpointSet
		want   map[negtypes.NEGLocation]negtypes.NetworkEndpointSet
	}{{
		desc: "zonal 25->24",
		nodes: map[string][]*nodeWithSubnet{
			"zone1": makeNodes(1000, 100),
		},
		before: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
			{Zone: "zone1", Subnet: "default"}: {
				{Node: "node1000"}: {}, {Node: "node1001"}: {}, {Node: "node1002"}: {}, {Node: "node1003"}: {}, {Node: "node1004"}: {},
				{Node: "node1005"}: {}, {Node: "node1006"}: {}, {Node: "node1007"}: {}, {Node: "node1008"}: {}, {Node: "node1009"}: {},
				{Node: "node1010"}: {}, {Node: "node1011"}: {}, {Node: "node1012"}: {}, {Node: "node1013"}: {}, {Node: "node1014"}: {},
				{Node: "node1015"}: {}, {Node: "node1016"}: {}, {Node: "node1017"}: {}, {Node: "node1018"}: {}, {Node: "node1019"}: {},
				{Node: "node1020"}: {}, {Node: "node1021"}: {}, {Node: "node1022"}: {}, {Node: "node1023"}: {}, {Node: "node1024"}: {},
			},
		},
		want: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
			{Zone: "zone1", Subnet: "default"}: {
				{Node: "node1000"}: {}, {Node: "node1001"}: {}, {Node: "node1002"}: {}, {Node: "node1003"}: {}, {Node: "node1004"}: {},
				{Node: "node1005"}: {}, {Node: "node1006"}: {}, {Node: "node1007"}: {}, {Node: "node1008"}: {}, {Node: "node1009"}: {},
				{Node: "node1010"}: {}, {Node: "node1011"}: {}, {Node: "node1012"}: {}, {Node: "node1013"}: {}, {Node: "node1014"}: {},
				{Node: "node1015"}: {}, {Node: "node1016"}: {}, {Node: "node1017"}: {}, {Node: "node1018"}: {}, {Node: "node1019"}: {},
				{Node: "node1020"}: {}, {Node: "node1021"}: {}, {Node: "node1022"}: {}, {Node: "node1023"}: {}, // Node24 removed
			},
		},
	}, {
		desc: "3 zones, 8,8,9 to 8,8,8",
		nodes: map[string][]*nodeWithSubnet{
			"zone1": makeNodes(1000, 100),
			"zone2": makeNodes(2000, 100),
			"zone3": makeNodes(3000, 100),
		},
		before: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
			{Zone: "zone1", Subnet: "default"}: { // 8
				{Node: "node1000"}: {}, {Node: "node1001"}: {}, {Node: "node1002"}: {}, {Node: "node1003"}: {}, {Node: "node1004"}: {},
				{Node: "node1005"}: {}, {Node: "node1006"}: {}, {Node: "node1007"}: {},
			},
			{Zone: "zone2", Subnet: "default"}: { // 8
				{Node: "node2000"}: {}, {Node: "node2001"}: {}, {Node: "node2002"}: {}, {Node: "node2003"}: {}, {Node: "node2004"}: {},
				{Node: "node2005"}: {}, {Node: "node2006"}: {}, {Node: "node2007"}: {},
			},
			{Zone: "zone3", Subnet: "default"}: { // 9
				{Node: "node3000"}: {}, {Node: "node3001"}: {}, {Node: "node3002"}: {}, {Node: "node3003"}: {}, {Node: "node3004"}: {},
				{Node: "node3005"}: {}, {Node: "node3006"}: {}, {Node: "node3007"}: {}, {Node: "node3008"}: {},
			},
		},
		want: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
			{Zone: "zone1", Subnet: "default"}: { // 8
				{Node: "node1000"}: {}, {Node: "node1001"}: {}, {Node: "node1002"}: {}, {Node: "node1003"}: {}, {Node: "node1004"}: {},
				{Node: "node1005"}: {}, {Node: "node1006"}: {}, {Node: "node1007"}: {},
			},
			{Zone: "zone2", Subnet: "default"}: { // 8
				{Node: "node2000"}: {}, {Node: "node2001"}: {}, {Node: "node2002"}: {}, {Node: "node2003"}: {}, {Node: "node2004"}: {},
				{Node: "node2005"}: {}, {Node: "node2006"}: {}, {Node: "node2007"}: {},
			},
			{Zone: "zone3", Subnet: "default"}: { // 8
				{Node: "node3000"}: {}, {Node: "node3001"}: {}, {Node: "node3002"}: {}, {Node: "node3003"}: {}, {Node: "node3004"}: {},
				{Node: "node3005"}: {}, {Node: "node3006"}: {}, {Node: "node3007"}: {},
			},
		},
	}}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			// Arrange
			nodeInformer := zonegetter.FakeNodeInformer()
			zoneGetter, err := zonegetter.NewFakeZoneGetter(nodeInformer, nodeInformer, defaultTestSubnetURL, false)
			if err != nil {
				t.Fatalf("failed to initialize zone getter: %v", err)
			}
			defaultNetwork := network.NetworkInfo{IsDefault: true, K8sNetwork: "default", SubnetworkURL: defaultTestSubnetURL}

			// We use ILB so that it doesn't trigger code that linearly calculates number of NEGs needed
			// based on actual number of Pods.
			c := NewClusterL4EndpointsCalculator(
				listers.NewNodeLister(nodeInformer.GetIndexer()),
				zoneGetter, "svc", klog.TODO(), &defaultNetwork, negtypes.L4InternalLB,
			)

			// Set up nodes
			for zone, nodes := range tc.nodes {
				zonegetter.DeleteFakeNodesInZone(t, zone, zoneGetter)
				names := make([]string, 0, len(nodes))
				for _, node := range nodes {
					names = append(names, node.node.Name)
				}
				if err := zonegetter.AddFakeNodes(zoneGetter, zone, names...); err != nil {
					t.Fatalf("failed to add fake node: %v", err)
				}
			}

			// Act
			res, _, _, err := c.CalculateEndpoints(nil, tc.before)
			if err != nil {
				t.Fatalf("expected no err, got %v", err)
			}

			// Assert
			if diff := cmp.Diff(tc.want, res); diff != "" {
				t.Errorf("want != got, -want +got:\n%s", diff)
			}
		})
	}
}

func TestValidateEndpoints(t *testing.T) {
	testPortName := ""
	emptyNamedPort := ""
	protocolTCP := v1.ProtocolTCP
	port80 := int32(80)

	instance1 := negtypes.TestInstance1
	instance2 := negtypes.TestInstance2
	duplicatePodName := "pod2-duplicate"
	noPodCIDRInstance := negtypes.TestNoPodCIDRInstance
	noPodCIDRPod := negtypes.TestNoPodCIDRPod
	nonDefaultSubnetInstance := negtypes.TestNonDefaultSubnetInstance
	nonDefaultSubnetPod := negtypes.TestNonDefaultSubnetPod
	svcPort := negtypes.NegSyncerKey{
		Namespace: testServiceNamespace,
		Name:      testServiceName,
		NegType:   negtypes.VmIpPortEndpointType,
		PortTuple: negtypes.SvcPortTuple{
			Port:       80,
			TargetPort: "8080",
			Name:       testPortName,
		},
		NegName: testNegName,
	}

	testContext := negtypes.NewTestContext()
	podLister := testContext.PodInformer.GetIndexer()
	addPodsToLister(podLister, getDefaultEndpointSlices())

	// Add duplicate pod that shares the same IP and node as pod2.
	podLister.Add(&v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testServiceNamespace,
			Name:      duplicatePodName,
			Labels: map[string]string{
				discovery.LabelServiceName: testServiceName,
				discovery.LabelManagedBy:   managedByEPSControllerValue,
			},
		},
		Spec: v1.PodSpec{
			NodeName: instance2,
		},
		Status: v1.PodStatus{
			Phase: v1.PodRunning,
			PodIP: "10.100.1.2",
			PodIPs: []v1.PodIP{
				{IP: "10.100.1.2"},
			},
		},
	})

	nodeLister := testContext.NodeInformer.GetIndexer()
	serviceLister := testContext.ServiceInformer.GetIndexer()
	zonegetter.PopulateFakeNodeInformer(testContext.NodeInformer, true)
	zoneGetter, err := zonegetter.NewFakeZoneGetter(testContext.NodeInformer, zonegetter.FakeNodeTopologyInformer(), defaultTestSubnetURL, false)
	if err != nil {
		t.Fatalf("failed to initialize zone getter: %v", err)
	}
	L7EndpointsCalculator := NewL7EndpointsCalculator(zoneGetter, podLister, nodeLister, serviceLister, svcPort, klog.TODO(), testContext.EnableDualStackNEG, metricscollector.FakeSyncerMetrics())

	zoneGetterMSC, err := zonegetter.NewFakeZoneGetter(testContext.NodeInformer, zonegetter.FakeNodeTopologyInformer(), defaultTestSubnetURL, true)
	if err != nil {
		t.Fatalf("failed to initialize msc zone getter: %v", err)
	}
	L7EndpointsCalculatorMSC := NewL7EndpointsCalculator(zoneGetterMSC, podLister, nodeLister, serviceLister, svcPort, klog.TODO(), testContext.EnableDualStackNEG, metricscollector.FakeSyncerMetrics())
	L7EndpointsCalculatorMSC.enableMultiSubnetCluster = true
	L4LocalEndpointCalculator := NewLocalL4EndpointsCalculator(listers.NewNodeLister(nodeLister), zoneGetter, fmt.Sprintf("%s/%s", testServiceName, testServiceNamespace), klog.TODO(), &network.NetworkInfo{SubnetworkURL: defaultTestSubnetURL}, negtypes.L4InternalLB)
	L4ClusterEndpointCalculator := NewClusterL4EndpointsCalculator(listers.NewNodeLister(nodeLister), zoneGetter, fmt.Sprintf("%s/%s", testServiceName, testServiceNamespace), klog.TODO(), &network.NetworkInfo{SubnetworkURL: defaultTestSubnetURL}, negtypes.L4InternalLB)

	l7TestEPS := []*discovery.EndpointSlice{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      testServiceName,
				Namespace: testServiceNamespace,
				Labels: map[string]string{
					discovery.LabelServiceName: testServiceName,
				},
			},
			AddressType: "IPv4",
			Endpoints: []discovery.Endpoint{
				{
					Addresses: []string{"10.100.1.1"},
					NodeName:  &instance1,
					TargetRef: &v1.ObjectReference{
						Namespace: testServiceNamespace,
						Name:      "pod1",
					},
				},
				{
					Addresses: []string{"10.100.1.2"},
					NodeName:  &instance1,
					TargetRef: &v1.ObjectReference{
						Namespace: testServiceNamespace,
						Name:      "pod2",
					},
				},
			},
			Ports: []discovery.EndpointPort{
				{
					Name:     &emptyNamedPort,
					Port:     &port80,
					Protocol: &protocolTCP,
				},
			},
		},
	}
	noopMutation := func(ed []negtypes.EndpointsData, podMap negtypes.EndpointPodMap) ([]negtypes.EndpointsData, negtypes.EndpointPodMap) {
		return ed, podMap
	}

	prevFlag := flags.F.EnableMultiSubnetCluster
	defer func() { flags.F.EnableMultiSubnetCluster = prevFlag }()
	flags.F.EnableMultiSubnetCluster = true

	testCases := []struct {
		desc               string
		ec                 negtypes.NetworkEndpointsCalculator
		ecMSC              negtypes.NetworkEndpointsCalculator
		testEndpointSlices []*discovery.EndpointSlice
		currentMap         map[negtypes.NEGLocation]negtypes.NetworkEndpointSet
		// Use mutation to inject error into that we cannot trigger currently.
		mutation func([]negtypes.EndpointsData, negtypes.EndpointPodMap) ([]negtypes.EndpointsData, negtypes.EndpointPodMap)
		expect   error
	}{
		{
			desc:               "ValidateEndpoints for L4 local endpoint calculator", // we are adding this test to make sure the test is updated when the functionality is added
			ec:                 L4LocalEndpointCalculator,
			ecMSC:              L4LocalEndpointCalculator,
			testEndpointSlices: nil, // for now it is a no-op
			mutation:           noopMutation,
			currentMap:         nil,
			expect:             nil,
		},
		{
			desc:               "ValidateEndpoints for L4 cluster endpoint calculator", // we are adding this test to make sure the test is updated when the functionality is added
			ec:                 L4ClusterEndpointCalculator,
			ecMSC:              L4LocalEndpointCalculator,
			testEndpointSlices: nil, // for now it is a no-op
			mutation:           noopMutation,
			currentMap:         nil,
			expect:             nil,
		},
		{
			desc:               "ValidateEndpoints for L7 Endpoint Calculator. Endpoint counts equal, endpointData has no duplicated endpoints",
			ec:                 L7EndpointsCalculator,
			ecMSC:              L7EndpointsCalculatorMSC,
			testEndpointSlices: l7TestEPS,
			mutation:           noopMutation,
			currentMap:         nil,
			expect:             nil,
		},
		{
			desc:  "ValidateEndpoints for L7 Endpoint Calculator. Endpoint counts equal, endpointData has one duplicated endpoint",
			ec:    L7EndpointsCalculator,
			ecMSC: L7EndpointsCalculatorMSC,
			testEndpointSlices: []*discovery.EndpointSlice{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testServiceName,
						Namespace: testServiceNamespace,
						Labels: map[string]string{
							discovery.LabelServiceName: testServiceName,
						},
					},
					AddressType: "IPv4",
					Endpoints: []discovery.Endpoint{
						{
							Addresses: []string{"10.100.1.1"},
							NodeName:  &instance1,
							TargetRef: &v1.ObjectReference{
								Namespace: testServiceNamespace,
								Name:      "pod1",
							},
						},
						{
							Addresses: []string{"10.100.1.2"},
							NodeName:  &instance1,
							TargetRef: &v1.ObjectReference{
								Namespace: testServiceNamespace,
								Name:      "pod2",
							},
						},
						{
							Addresses: []string{"10.100.1.2"},
							NodeName:  &instance1,
							TargetRef: &v1.ObjectReference{
								Namespace: testServiceNamespace,
								Name:      duplicatePodName,
							},
						},
					},
					Ports: []discovery.EndpointPort{
						{
							Name:     &emptyNamedPort,
							Port:     &port80,
							Protocol: &protocolTCP,
						},
					},
				},
			},
			mutation:   noopMutation,
			currentMap: nil,
			expect:     nil,
		},
		{
			desc:               "ValidateEndpoints for L7 Endpoint Calculator. Endpoint counts not equal",
			ec:                 L7EndpointsCalculator,
			ecMSC:              L7EndpointsCalculatorMSC,
			testEndpointSlices: l7TestEPS,
			mutation: func(endpointData []negtypes.EndpointsData, endpointPodMap negtypes.EndpointPodMap) ([]negtypes.EndpointsData, negtypes.EndpointPodMap) {
				// Add one additional endpoint in endpointData
				endpointData[0].Addresses = append(endpointData[0].Addresses, negtypes.AddressData{})
				return endpointData, endpointPodMap
			},
			currentMap: nil,
			expect:     negtypes.ErrEPCountsDiffer,
		},
		{
			desc:               "ValidateEndpoints for L7 Endpoint Calculator. EndpointData has zero endpoint",
			ec:                 L7EndpointsCalculator,
			ecMSC:              L7EndpointsCalculatorMSC,
			testEndpointSlices: l7TestEPS,
			mutation: func(endpointData []negtypes.EndpointsData, endpointPodMap negtypes.EndpointPodMap) ([]negtypes.EndpointsData, negtypes.EndpointPodMap) {
				for i := range endpointData {
					endpointData[i].Addresses = []negtypes.AddressData{}
				}
				return endpointData, endpointPodMap
			},
			currentMap: nil,
			expect:     negtypes.ErrEPSEndpointCountZero,
		},
		{
			desc:               "ValidateEndpoints for L7 Endpoint Calculator. EndpointPodMap has zero endpoint",
			ec:                 L7EndpointsCalculator,
			ecMSC:              L7EndpointsCalculatorMSC,
			testEndpointSlices: l7TestEPS,
			mutation: func(endpointData []negtypes.EndpointsData, endpointPodMap negtypes.EndpointPodMap) ([]negtypes.EndpointsData, negtypes.EndpointPodMap) {
				endpointPodMap = negtypes.EndpointPodMap{}
				return endpointData, endpointPodMap
			},
			currentMap: nil,
			expect:     negtypes.ErrEPCalculationCountZero,
		},
		{
			desc:               "ValidateEndpoints for L7 Endpoint Calculator. EndpointData and endpointPodMap both have zero endpoint",
			ec:                 L7EndpointsCalculator,
			ecMSC:              L7EndpointsCalculatorMSC,
			testEndpointSlices: l7TestEPS,
			mutation: func(endpointData []negtypes.EndpointsData, endpointPodMap negtypes.EndpointPodMap) ([]negtypes.EndpointsData, negtypes.EndpointPodMap) {
				for i := range endpointData {
					endpointData[i].Addresses = []negtypes.AddressData{}
				}
				endpointPodMap = negtypes.EndpointPodMap{}
				return endpointData, endpointPodMap
			},
			currentMap: nil,
			expect:     negtypes.ErrEPCalculationCountZero, // PodMap count is check and returned first,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			endpointData := negtypes.EndpointsDataFromEndpointSlices(tc.testEndpointSlices)
			_, endpointPodMap, endpointsExcludedInCalculation, err := tc.ec.CalculateEndpoints(endpointData, tc.currentMap)
			if err != nil {
				t.Errorf("Received error when calculating endpoint: %v", err)
			}
			endpointData, endpointPodMap = tc.mutation(endpointData, endpointPodMap)
			if got := tc.ec.ValidateEndpoints(endpointData, endpointPodMap, endpointsExcludedInCalculation); !errors.Is(got, tc.expect) {
				t.Errorf("ValidateEndpoints() = %v, expected %v", got, tc.expect)
			}

			// Run tests with multi-subnet cluster enabled.
			endpointData = negtypes.EndpointsDataFromEndpointSlices(tc.testEndpointSlices)
			_, endpointPodMap, endpointsExcludedInCalculation, err = tc.ecMSC.CalculateEndpoints(endpointData, tc.currentMap)
			if err != nil {
				t.Errorf("With multi-subnet cluster enabled, received error when calculating endpoint: %v", err)
			}
			endpointData, endpointPodMap = tc.mutation(endpointData, endpointPodMap)
			if got := tc.ecMSC.ValidateEndpoints(endpointData, endpointPodMap, endpointsExcludedInCalculation); !errors.Is(got, tc.expect) {
				t.Errorf("With multi-subnet cluster enabled, ValidateEndpoints() = %v, expected %v", got, tc.expect)
			}
		})
	}

	// Add noPodCIDRPod that corresponds to noPodCIDRInstance.
	podLister.Add(&v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testServiceNamespace,
			Name:      noPodCIDRPod,
			Labels: map[string]string{
				discovery.LabelServiceName: testServiceName,
				discovery.LabelManagedBy:   managedByEPSControllerValue,
			},
		},
		Spec: v1.PodSpec{
			NodeName: noPodCIDRInstance,
		},
		Status: v1.PodStatus{
			Phase: v1.PodRunning,
			PodIP: "10.101.3.1",
			PodIPs: []v1.PodIP{
				{IP: "10.101.3.1"},
			},
		},
	})
	podLister.Add(&v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testServiceNamespace,
			Name:      nonDefaultSubnetPod,
			Labels: map[string]string{
				discovery.LabelServiceName: testServiceName,
				discovery.LabelManagedBy:   managedByEPSControllerValue,
			},
		},
		Spec: v1.PodSpec{
			NodeName: nonDefaultSubnetInstance,
		},
		Status: v1.PodStatus{
			Phase: v1.PodRunning,
			PodIP: "10.200.1.1",
			PodIPs: []v1.PodIP{
				{IP: "10.200.1.1"},
			},
		},
	})

	podLister.Add(&v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testServiceNamespace,
			Name:      nonDefaultSubnetPod,
			Labels: map[string]string{
				discovery.LabelServiceName: testServiceName,
				discovery.LabelManagedBy:   managedByEPSControllerValue,
			},
		},
		Spec: v1.PodSpec{
			NodeName: nonDefaultSubnetInstance,
		},
		Status: v1.PodStatus{
			Phase: v1.PodRunning,
			PodIP: "10.200.1.1",
			PodIPs: []v1.PodIP{
				{IP: "10.200.1.1"},
			},
		},
	})

	mscTestCases := []struct {
		desc               string
		ecMSC              negtypes.NetworkEndpointsCalculator
		testEndpointSlices []*discovery.EndpointSlice
		currentMap         map[negtypes.NEGLocation]negtypes.NetworkEndpointSet
		// Use mutation to inject error into that we cannot trigger currently.
		mutation             func([]negtypes.EndpointsData, negtypes.EndpointPodMap) ([]negtypes.EndpointsData, negtypes.EndpointPodMap)
		expectCalculationErr error
		expectValidationErr  error
	}{
		{
			desc:  "ValidateEndpoints for L7 Endpoint Calculator. Endpoint counts equal, endpointData has an endpoint corresponds to node without PodCIDR",
			ecMSC: L7EndpointsCalculatorMSC,
			testEndpointSlices: []*discovery.EndpointSlice{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testServiceName,
						Namespace: testServiceNamespace,
						Labels: map[string]string{
							discovery.LabelServiceName: testServiceName,
						},
					},
					AddressType: "IPv4",
					Endpoints: []discovery.Endpoint{
						{
							Addresses: []string{"10.100.1.1"},
							NodeName:  &instance1,
							TargetRef: &v1.ObjectReference{
								Namespace: testServiceNamespace,
								Name:      "pod1",
							},
						},
						{
							Addresses: []string{"10.100.1.2"},
							NodeName:  &instance1,
							TargetRef: &v1.ObjectReference{
								Namespace: testServiceNamespace,
								Name:      "pod2",
							},
						},
						{
							Addresses: []string{"10.101.1.3"},
							NodeName:  &noPodCIDRInstance,
							TargetRef: &v1.ObjectReference{
								Namespace: testServiceNamespace,
								Name:      noPodCIDRPod,
							},
						},
					},
					Ports: []discovery.EndpointPort{
						{
							Name:     &emptyNamedPort,
							Port:     &port80,
							Protocol: &protocolTCP,
						},
					},
				},
			},
			mutation:             noopMutation,
			currentMap:           nil,
			expectCalculationErr: zonegetter.ErrNodePodCIDRNotSet,
			expectValidationErr:  negtypes.ErrEPCalculationCountZero,
		},
		{
			desc:  "ValidateEndpoints for L7 Endpoint Calculator. Endpoint counts equal, endpointData has non-default subnet endpoint",
			ecMSC: L7EndpointsCalculatorMSC,
			testEndpointSlices: []*discovery.EndpointSlice{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testServiceName,
						Namespace: testServiceNamespace,
						Labels: map[string]string{
							discovery.LabelServiceName: testServiceName,
						},
					},
					AddressType: "IPv4",
					Endpoints: []discovery.Endpoint{
						{
							Addresses: []string{"10.100.1.1"},
							NodeName:  &instance1,
							TargetRef: &v1.ObjectReference{
								Namespace: testServiceNamespace,
								Name:      "pod1",
							},
						},
						{
							Addresses: []string{"10.100.1.2"},
							NodeName:  &instance1,
							TargetRef: &v1.ObjectReference{
								Namespace: testServiceNamespace,
								Name:      "pod2",
							},
						},
						{
							Addresses: []string{"10.200.1.1"},
							NodeName:  &nonDefaultSubnetInstance,
							TargetRef: &v1.ObjectReference{
								Namespace: testServiceNamespace,
								Name:      nonDefaultSubnetPod,
							},
						},
					},
					Ports: []discovery.EndpointPort{
						{
							Name:     &emptyNamedPort,
							Port:     &port80,
							Protocol: &protocolTCP,
						},
					},
				},
			},
			mutation:             noopMutation,
			currentMap:           nil,
			expectCalculationErr: nil,
			expectValidationErr:  nil,
		},
	}

	for _, tc := range mscTestCases {
		t.Run(tc.desc, func(t *testing.T) {
			endpointData := negtypes.EndpointsDataFromEndpointSlices(tc.testEndpointSlices)
			_, endpointPodMap, endpointsExcludedInCalculation, err := tc.ecMSC.CalculateEndpoints(endpointData, tc.currentMap)
			if !errors.Is(err, tc.expectCalculationErr) {
				t.Errorf("With multi-subnet cluster enabled, received error when calculating endpoint: %v", err)
			}
			endpointData, endpointPodMap = tc.mutation(endpointData, endpointPodMap)
			if got := tc.ecMSC.ValidateEndpoints(endpointData, endpointPodMap, endpointsExcludedInCalculation); !errors.Is(got, tc.expectValidationErr) {
				t.Errorf("With multi-subnet cluster enabled, ValidateEndpoints() = %v, expected %v", got, tc.expectValidationErr)
			}
		})
	}
}

// updateNodes overwrites the label, annotation, or the readyStatus on a node if the node exists and the overwrite is provided.
func updateNodes(t *testing.T, nodeNames []string, nodeLabels map[string]map[string]string, nodeAnnotations map[string]map[string]string, nodeReadyStatus map[string]v1.ConditionStatus, nodeIndexer cache.Indexer) {
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
		obj, exists, err := nodeIndexer.GetByKey(nodeName)
		if err != nil || !exists {
			t.Fatalf("Failed to get node %s in nodeLister, exists: %v, err: %v", nodeNames[i], exists, err)
		}
		node, ok := obj.(*v1.Node)
		if !ok {
			t.Fatalf("Failed to cast %v as type Node", obj)
		}
		node.ObjectMeta.Labels = labels
		node.ObjectMeta.Annotations = annotations
		node.Status.Conditions = []v1.NodeCondition{
			{
				Type:   v1.NodeReady,
				Status: readyStatus,
			},
		}
		err = nodeIndexer.Update(node)
		if err != nil {
			t.Fatalf("Failed to update node %q, err: %v", nodeName[i], err)
		}
	}

}
