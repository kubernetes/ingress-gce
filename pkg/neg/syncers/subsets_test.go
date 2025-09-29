/*
Copyright 2019 The Kubernetes Authors.

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
	"slices"
	"strings"
	"testing"

	networkv1 "github.com/GoogleCloudPlatform/gke-networking-api/apis/network/v1"

	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/ingress-gce/pkg/network"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog/v2"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestBasicSubset(t *testing.T) {
	t.Parallel()
	nodes := []*v1.Node{
		{ObjectMeta: metav1.ObjectMeta{Name: "node0"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "node73"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "node986"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "node25"}},
	}
	var nodesWithSubnet []*nodeWithSubnet
	for _, node := range nodes {
		nodesWithSubnet = append(nodesWithSubnet, newNodeWithSubnet(node, defaultTestSubnet))
	}
	count := 3
	subset1 := pickSubsetsMinRemovals(nodesWithSubnet, "svc123", count, nil)
	if len(subset1) < 3 {
		t.Errorf("Expected %d subsets, got only %d - %v", count, len(subset1), subset1)
	}
	if !validateSubset(subset1, nodesWithSubnet) {
		t.Errorf("Invalid subset list %v from %v", subset1, nodes)
	}
	subset2 := pickSubsetsMinRemovals(nodesWithSubnet, "svc345", count, nil)
	subset3 := pickSubsetsMinRemovals(nodesWithSubnet, "svc56", count, nil)
	t.Logf("Subset2 is %s", nodeNames(subset2))
	t.Logf("Subset3 is %s", nodeNames(subset3))
	if isIdentical(subset1, subset2) || isIdentical(subset3, subset2) || isIdentical(subset1, subset3) {
		t.Errorf("2 out of 3 subsets are identical")
	}
}

func TestEmptyNodes(t *testing.T) {
	t.Parallel()
	count := 3
	subset1 := pickSubsetsMinRemovals(nil, "svc123", count, nil)
	if len(subset1) != 0 {
		t.Errorf("Expected empty subset, got - %s", nodeNames(subset1))
	}
}

// Tests the case where there are fewer nodes than subsets
func TestFewerNodes(t *testing.T) {
	t.Parallel()
	nodes := makeNodes(0, 5)
	count := 10
	subset1 := pickSubsetsMinRemovals(nodes, "svc123", count, nil)
	if len(subset1) != len(nodes) {
		t.Errorf("Expected subset of length %d, got %d, subsets - %s", len(nodes), len(subset1), nodeNames(subset1))
	}
	if !isIdentical(nodes, subset1) {
		t.Errorf("Subset list is different from list of nodes, subsets - %s", nodeNames(subset1))
	}
}

// Tests the case where there is uneven distribution of nodes in various zones. The goal is to select as many nodes as
// possible in all cases.
func TestUnevenNodesInZones(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		description   string
		nodesMap      map[string][]*nodeWithSubnet
		svcKey        string
		subsetLimit   int
		expectedCount int
		// expectEmpty indicates that some zones can have empty subsets
		expectEmpty bool
	}{
		{
			description: "Total number of nodes > limit(250), some zones have only a couple of nodes.",
			nodesMap: map[string][]*nodeWithSubnet{
				"zone1": makeNodes(1, 1),
				"zone2": makeNodes(2, 5),
				"zone3": makeNodes(7, 10),
				"zone4": makeNodes(17, 250),
			},
			svcKey:        "svc123",
			subsetLimit:   maxSubsetSizeLocal,
			expectedCount: maxSubsetSizeLocal,
		},
		{
			description: "Total number of nodes > limit(250), 3 zones, some zones have only a couple of nodes.",
			nodesMap: map[string][]*nodeWithSubnet{
				"zone1": makeNodes(1, 1),
				"zone2": makeNodes(2, 5),
				"zone4": makeNodes(7, 250),
			},
			svcKey:        "svc123",
			subsetLimit:   maxSubsetSizeLocal,
			expectedCount: maxSubsetSizeLocal,
		},
		{
			description: "Total number of nodes > limit(250), all zones have 100 nodes.",
			nodesMap: map[string][]*nodeWithSubnet{
				"zone1": makeNodes(1, 100),
				"zone2": makeNodes(100, 100),
				"zone3": makeNodes(200, 100),
				"zone4": makeNodes(300, 100),
			},
			svcKey:        "svc123",
			subsetLimit:   maxSubsetSizeLocal,
			expectedCount: maxSubsetSizeLocal,
		},
		{
			description: "Total number of nodes > limit(250), 3 zones, all zones have 100 nodes.",
			nodesMap: map[string][]*nodeWithSubnet{
				"zone1": makeNodes(1, 100),
				"zone2": makeNodes(100, 100),
				"zone3": makeNodes(200, 100),
			},
			svcKey:        "svc123",
			subsetLimit:   maxSubsetSizeLocal,
			expectedCount: maxSubsetSizeLocal,
		},
		{
			description: "Total number of nodes < limit(250), some have only a couple of nodes.",
			nodesMap: map[string][]*nodeWithSubnet{
				"zone1": makeNodes(1, 1),
				"zone2": makeNodes(2, 5),
				"zone3": makeNodes(7, 10),
				"zone4": makeNodes(17, 33),
			},
			svcKey:      "svc123",
			subsetLimit: maxSubsetSizeLocal,
			// All the nodes should be picked
			expectedCount: 49,
		},
		{
			description: "Total number of nodes < limit(250), all have only a couple of nodes.",
			nodesMap: map[string][]*nodeWithSubnet{
				"zone1": makeNodes(1, 1),
				"zone2": makeNodes(2, 5),
				"zone3": makeNodes(7, 3),
				"zone4": makeNodes(10, 4),
			},
			svcKey:      "svc123",
			subsetLimit: maxSubsetSizeLocal,
			// All the nodes should be picked
			expectedCount: 13,
		},
		{
			description: "Total number of nodes > limit(25), some zones have only a couple of nodes.",
			nodesMap: map[string][]*nodeWithSubnet{
				"zone1": makeNodes(1, 1),
				"zone2": makeNodes(2, 5),
				"zone3": makeNodes(7, 10),
				"zone4": makeNodes(17, 250),
			},
			svcKey:        "svc123",
			subsetLimit:   maxSubsetSizeDefault,
			expectedCount: maxSubsetSizeDefault,
		},
		{
			description: "Total number of nodes > limit(25), one zone has no nodes.",
			nodesMap: map[string][]*nodeWithSubnet{
				"zone1": makeNodes(1, 1),
				"zone2": makeNodes(2, 5),
				"zone3": nil,
				"zone4": makeNodes(17, 250),
			},
			svcKey:        "svc123",
			subsetLimit:   maxSubsetSizeDefault,
			expectedCount: maxSubsetSizeDefault,
			expectEmpty:   true,
		},
		{
			description: "Nodes across subnets.",
			nodesMap: map[string][]*nodeWithSubnet{
				"zone1": makeNodesInSubnet(1, 1, "sub1"),
				"zone2": makeNodes(2, 5),
				"zone3": slices.Concat(makeNodes(7, 2), makeNodesInSubnet(9, 8, "sub2")),
				"zone4": slices.Concat(makeNodes(17, 3), makeNodesInSubnet(20, 250, "sub1")),
			},
			svcKey:        "svc123",
			subsetLimit:   maxSubsetSizeLocal,
			expectedCount: maxSubsetSizeLocal,
		},
		{
			description: "Over limit across subnets",
			nodesMap: map[string][]*nodeWithSubnet{
				"zone1": slices.Concat(makeNodes(1, 9), makeNodesInSubnet(10, 90, "subnetB")),
				"zone2": makeNodesInSubnet(100, 100, "subnetB"),
				"zone3": makeNodesInSubnet(200, 100, "subnetC"),
				"zone4": slices.Concat(makeNodes(300, 1), makeNodesInSubnet(2, 99, "subnetB")),
			},
			svcKey:        "svc123",
			subsetLimit:   maxSubsetSizeLocal,
			expectedCount: maxSubsetSizeLocal,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {

			subsetMap, err := getSubsetPerZone(tc.nodesMap, tc.subsetLimit, tc.svcKey, nil, klog.TODO(), &network.NetworkInfo{SubnetworkURL: defaultTestSubnetURL})
			if err != nil {
				t.Errorf("Failed to get subset for test '%s', err %v", tc.description, err)
			}
			zones := make(map[string]struct{})
			for egi := range subsetMap {
				zones[egi.Zone] = struct{}{}
			}
			if len(zones) != len(tc.nodesMap) {
				t.Errorf("Not all input zones were included in the subset.  subset map - %v, nodesMap %v, test '%s'",
					subsetMap, tc.nodesMap, tc.description)
			}
			totalSubsetSize := 0
			for zone := range zones {
				subset := getNetworkEndpointsForZone(zone, subsetMap)
				if len(subset) == 0 && !tc.expectEmpty {
					t.Errorf("Got empty subset in zone %s for test '%s'", zone, tc.description)
				}
				totalSubsetSize += len(subset)
			}
			if totalSubsetSize != tc.expectedCount {
				t.Errorf("Expected %d nodes in subset, Got %d for test '%s'", maxSubsetSizeLocal, totalSubsetSize,
					tc.description)
			}
		})
	}
}

func TestGetSubsetPerZoneMultinetwork(t *testing.T) {
	t.Parallel()
	const ipv6Network = "2001:db8:1::/64"
	ipv6Subnet := defaultTestSubnetURL + "-ipv6"

	testCases := []struct {
		description   string
		nodesMap      map[string][]*nodeWithSubnet
		svcKey        string
		expectedCount int
		// expectEmpty indicates that some zones can have empty subsets
		expectEmpty      bool
		networkInfo      network.NetworkInfo
		expectedNodesMap map[types.NEGLocation]map[string]types.NetworkEndpoint
		enableIPv6       bool
	}{
		{
			description: "default network, gets primary interface, IPv4-only (flag diasbled)",
			nodesMap: map[string][]*nodeWithSubnet{
				"zone1": {makeNodeWithNetwork(t, "n1_1", "zone1", map[string]string{"net1": "172.168.1.1"}), makeNodeWithNetwork(t, "n1_2", "zone1", map[string]string{"net1": "172.168.1.2", "net2": "192.168.1.2"})},
				"zone2": {makeNodeWithNetwork(t, "n2_1", "zone2", map[string]string{"net1": "172.168.2.1"}), makeNodeWithNetwork(t, "n2_2", "zone2", map[string]string{"net1": "172.168.2.2"})},
				"zone3": {makeNodeWithNetwork(t, "n3_1", "zone3", map[string]string{"net1": "172.168.3.1", "net2": "192.168.3.1"})},
			},
			svcKey: "svc123",
			networkInfo: network.NetworkInfo{
				SubnetworkURL: defaultTestSubnetURL,
				IsDefault:     true,
			},
			enableIPv6: false,
			// empty IPs since test can't get the primary IP
			expectedNodesMap: map[types.NEGLocation]map[string]types.NetworkEndpoint{
				{Zone: "zone1", Subnet: defaultTestSubnet}: {"n1_1": {Node: "n1_1", IP: ""}, "n1_2": {Node: "n1_2", IP: ""}},
				{Zone: "zone2", Subnet: defaultTestSubnet}: {"n2_1": {Node: "n2_1", IP: ""}, "n2_2": {Node: "n2_2", IP: ""}},
				{Zone: "zone3", Subnet: defaultTestSubnet}: {"n3_1": {Node: "n3_1", IP: ""}},
			},
		},
		{
			description: "non-default network IPs, IPv4-only (flag diasbled)",
			nodesMap: map[string][]*nodeWithSubnet{
				"zone1": {makeNodeWithNetwork(t, "n1_1", "zone1", map[string]string{"net1": "172.168.1.1"}), makeNodeWithNetwork(t, "n1_2", "zone1", map[string]string{"net2": "192.168.1.2", "net1": "172.168.1.2"})},
				"zone2": {makeNodeWithNetwork(t, "n2_1", "zone2", map[string]string{"net1": "172.168.2.1"}), makeNodeWithNetwork(t, "n2_2", "zone2", map[string]string{"net1": "172.168.2.2"})},
				"zone3": {makeNodeWithNetwork(t, "n3_1", "zone3", map[string]string{"net1": "172.168.3.1", "net2": "192.168.3.1"})},
			},
			svcKey: "svc123",
			networkInfo: network.NetworkInfo{
				IsDefault:     false,
				K8sNetwork:    "net1",
				SubnetworkURL: defaultTestSubnetURL,
			},
			enableIPv6: false,
			expectedNodesMap: map[types.NEGLocation]map[string]types.NetworkEndpoint{
				{Zone: "zone1", Subnet: defaultTestSubnet}: {"n1_1": {Node: "n1_1", IP: "172.168.1.1"}, "n1_2": {Node: "n1_2", IP: "172.168.1.2"}},
				{Zone: "zone2", Subnet: defaultTestSubnet}: {"n2_1": {Node: "n2_1", IP: "172.168.2.1"}, "n2_2": {Node: "n2_2", IP: "172.168.2.2"}},
				{Zone: "zone3", Subnet: defaultTestSubnet}: {"n3_1": {Node: "n3_1", IP: "172.168.3.1"}},
			},
		},
		{
			description: "non-default network, dual-stack nodes, IPv6 disabled (flag diasbled)",
			nodesMap: map[string][]*nodeWithSubnet{
				"zone1": {makeNodeWithNetwork(t, "n1_1", "zone1", map[string]string{"net-dual": "172.168.1.1", ipv6Network: "2001:db8:1::1"}), makeNodeWithNetwork(t, "n1_2", "zone1", map[string]string{"net-dual": "172.168.1.2", ipv6Network: "2001:db8:1::2"})},
				"zone2": {makeNodeWithNetwork(t, "n2_1", "zone2", map[string]string{"net-dual": "172.168.2.1", ipv6Network: "2001:db8:2::1"})},
			},
			svcKey: "svc123",
			networkInfo: network.NetworkInfo{
				IsDefault:     false,
				K8sNetwork:    "net-dual",
				SubnetworkURL: defaultTestSubnetURL,
			},
			enableIPv6: false,
			expectedNodesMap: map[types.NEGLocation]map[string]types.NetworkEndpoint{
				{Zone: "zone1", Subnet: defaultTestSubnet}: {"n1_1": {Node: "n1_1", IP: "172.168.1.1"}, "n1_2": {Node: "n1_2", IP: "172.168.1.2"}},
				{Zone: "zone2", Subnet: defaultTestSubnet}: {"n2_1": {Node: "n2_1", IP: "172.168.2.1"}},
			},
		},
		{
			description: "non-default network, dual-stack nodes, IPv6 enabled, target IPv4 (flag enabled)",
			nodesMap: map[string][]*nodeWithSubnet{
				"zone1": {makeNodeWithNetwork(t, "n1_1", "zone1", map[string]string{"net-dual": "172.168.1.1", ipv6Network: "2001:db8:1::1"}), makeNodeWithNetwork(t, "n1_2", "zone1", map[string]string{"net-dual": "172.168.1.2", ipv6Network: "2001:db8:1::2"})},
				"zone2": {makeNodeWithNetwork(t, "n2_1", "zone2", map[string]string{"net-dual": "172.168.2.1", ipv6Network: "2001:db8:2::1"})},
			},
			svcKey: "svc123",
			networkInfo: network.NetworkInfo{
				IsDefault:     false,
				K8sNetwork:    "net-dual",
				SubnetworkURL: defaultTestSubnetURL,
			},
			enableIPv6: true,
			expectedNodesMap: map[types.NEGLocation]map[string]types.NetworkEndpoint{
				{Zone: "zone1", Subnet: defaultTestSubnet}: {"n1_1": {Node: "n1_1", IP: "172.168.1.1"}, "n1_2": {Node: "n1_2", IP: "172.168.1.2"}},
				{Zone: "zone2", Subnet: defaultTestSubnet}: {"n2_1": {Node: "n2_1", IP: "172.168.2.1"}},
			},
		},
		{
			description: "non-default network, IPv6 nodes, IPv6 enabled, target IPv6 network (flag enabled)",
			nodesMap: map[string][]*nodeWithSubnet{
				"zone1": {makeNodeWithNetwork(t, "n1_1", "zone1", map[string]string{"net-ipv6": "2001:db8:1::1"}), newNodeWithSubnet(makeNodeWithNetwork(t, "n1_2", "zone1", map[string]string{"net-ipv6": "2001:db8:1::2"}).node, ipv6Subnet)},
				"zone2": {newNodeWithSubnet(makeNodeWithNetwork(t, "n2_1", "zone2", map[string]string{"net-ipv6": "2001:db8:2::1"}).node, ipv6Subnet)},
			},
			svcKey: "svc123",
			networkInfo: network.NetworkInfo{
				IsDefault:     false,
				K8sNetwork:    "net-ipv6",
				SubnetworkURL: ipv6Subnet,
			},
			enableIPv6: true,
			expectedNodesMap: map[types.NEGLocation]map[string]types.NetworkEndpoint{
				{Zone: "zone1", Subnet: defaultTestSubnet}: {"n1_1": {Node: "n1_1", IPv6: "2001:db8:1::1"}},
				{Zone: "zone1", Subnet: ipv6Subnet}:        {"n1_2": {Node: "n1_2", IPv6: "2001:db8:1::2"}},
				{Zone: "zone2", Subnet: ipv6Subnet}:        {"n2_1": {Node: "n2_1", IPv6: "2001:db8:2::1"}},
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			oldFlags := flags.F.EnableIPv6NodeNEGEndpoints
			flags.F.EnableIPv6NodeNEGEndpoints = true
			defer func() {
				flags.F.EnableIPv6NodeNEGEndpoints = oldFlags
			}()

			subsetMap, err := getSubsetPerZone(tc.nodesMap, maxSubsetSizeLocal, tc.svcKey, nil, klog.TODO(), &tc.networkInfo)
			if err != nil {
				t.Errorf("Failed to get subset for test '%s', err %v", tc.description, err)
			}
			for zoneAndSubnet, wantNodesAndEndpoints := range tc.expectedNodesMap {
				gotSet, ok := subsetMap[zoneAndSubnet]
				if !ok {
					t.Errorf("Expected NEGLocation %s not found in result for test '%s'", zoneAndSubnet, tc.description)
					continue
				}

				for node, wantEndpoint := range wantNodesAndEndpoints {
					if !gotSet.Has(wantEndpoint) {
						found := false
						for _, gotEndpoint := range gotSet.List() {
							if gotEndpoint.Node == node {
								found = true
								if gotEndpoint.IP != wantEndpoint.IP || gotEndpoint.IPv6 != wantEndpoint.IPv6 {
									t.Errorf("Node %s in zoneAndSubnet %s: Expected endpoint %+v, but got %+v", node, zoneAndSubnet, wantEndpoint, gotEndpoint)
								}
								break
							}
						}
						if !found {
							t.Errorf("Node %s in zoneAndSubnet %s was expected but not found in endpoints: %+v", node, zoneAndSubnet, gotSet.List())
						}
					}
				}
			}
		})
	}
}

func makeNodes(startIndex, count int) []*nodeWithSubnet {
	return makeNodesInSubnet(startIndex, count, defaultTestSubnet)
}

func makeNodesInSubnet(startIndex, count int, subnet string) []*nodeWithSubnet {
	var nodes []*nodeWithSubnet

	for i := startIndex; i < startIndex+count; i++ {
		n := &v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:   fmt.Sprintf("node%d", i),
				Labels: map[string]string{utils.LabelNodeSubnet: subnet},
			},
		}
		nodes = append(nodes, newNodeWithSubnet(n, subnet))
	}
	return nodes
}

// makeNodeWithNetwork creates a node with multi-networking annotations
// networksAndIPs param should contain a map of network names to the IPs of the interface
// of that network.
func makeNodeWithNetwork(t *testing.T, name string, zone string, networksAndIPs map[string]string) *nodeWithSubnet {
	t.Helper()
	var providerID string
	if zone != "" {
		providerID = fmt.Sprintf("gce://testProject/%s/%s", zone, name)
	}
	node := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Annotations: map[string]string{},
		},
		Spec: v1.NodeSpec{
			ProviderID: providerID,
			PodCIDR:    "10.0.0.0/24",
		},
	}
	var northInterfaces networkv1.NorthInterfacesAnnotation
	for netName, ip := range networksAndIPs {
		northInterfaces = append(northInterfaces, networkv1.NorthInterface{
			Network:   netName,
			IpAddress: ip,
		})

	}
	if len(northInterfaces) > 0 {
		annotation, err := networkv1.MarshalNorthInterfacesAnnotation(northInterfaces)
		if err != nil {
			t.Errorf("could not create node annotations")
		}
		node.ObjectMeta.Annotations[networkv1.NorthInterfacesAnnotationKey] = annotation
	}
	return newNodeWithSubnet(node, defaultTestSubnet)
}

func TestNoRemovals(t *testing.T) {
	t.Parallel()
	// pick a random startIndex which is used to construct nodeName.
	nodes := makeNodes(78, 5)
	count := 5
	subset1 := pickSubsetsMinRemovals(nodes, "svc123", count, nil)
	if len(subset1) < 5 {
		t.Errorf("Expected %d subsets, got only %d - %v", count, len(subset1), subset1)
	}
	// nodeName abcd shows up 2nd in the sorted list for the given salt. So picking a subset of 5 will remove one of the
	// existing nodes.
	nodes = append(nodes, newNodeWithSubnet(&v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node:abcd"}}, defaultTestSubnet))
	subset2 := pickSubsetsMinRemovals(nodes, "svc123", count, nil)
	if len(subset2) < 5 {
		t.Errorf("Expected %d subsets, got only %d - %v", count, len(subset2), subset2)
	}
	if isIdentical(subset1, subset2) {
		t.Errorf("Got identical subsets %+v", subset1)
	}
	existingEp := []types.NetworkEndpoint{}
	for _, node := range subset1 {
		existingEp = append(existingEp, types.NetworkEndpoint{Node: node.node.Name})
	}
	subset3 := pickSubsetsMinRemovals(nodes, "svc123", count, existingEp)
	if len(subset3) < 5 {
		t.Errorf("Expected %d subsets, got only %d - %v", count, len(subset3), subset3)
	}
	if !isIdentical(subset1, subset3) {
		t.Errorf("Got subsets %+v and %+v, expected identical subsets %+v", subset1, subset3, subset1)
	}
}

func validateSubset(subset []*nodeWithSubnet, nodes []*nodeWithSubnet) bool {
	for _, val := range subset {
		found := false
		for _, node := range nodes {
			if val.node == node.node {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func nodeNames(subset []*nodeWithSubnet) string {
	names := []string{}
	for _, n := range subset {
		names = append(names, n.node.Name)
	}
	return strings.Join(names, " ")
}

func isIdentical(subset1, subset2 []*nodeWithSubnet) bool {
	foundCount := 0
	if len(subset1) != len(subset2) {
		return false
	}
	for _, node1 := range subset1 {
		found := false
		for _, node2 := range subset2 {
			if node1.node == node2.node {
				found = true
				break
			}
		}
		if found {
			foundCount = foundCount + 1
		}
	}
	return foundCount == len(subset1)
}
