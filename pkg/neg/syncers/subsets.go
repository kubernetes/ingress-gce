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
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"sort"

	v1 "k8s.io/api/core/v1"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/ingress-gce/pkg/network"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog/v2"
)

const (
	// Max number of subsets in ExternalTrafficPolicy:Local
	maxSubsetSizeLocal = 250
	// Max number of subsets in ExternalTrafficPolicy:Cluster, which is the default mode.
	maxSubsetSizeDefault = 25
	// Max number of subsets for NetLB in ExternalTrafficPolicy:Local
	maxSubsetSizeNetLBLocal = 3000
	// Max number of subsets for NetLB in ExternalTrafficPolicy:Cluster
	maxSubsetSizeNetLBCluster = 250
)

// NodeInfo stores node metadata used to sort nodes and pick a subset.
type NodeInfo struct {
	// index stores the index of the given node in the input node list. This is useful to
	// identify the node in the list after sorting.
	index int
	// hashedName is the sha256 hash of the given node name along with a salt.
	hashedName string
	// skip indicates if this node has already been selected in the subset and hence needs
	// to be skipped.
	skip bool
}

func getHashedName(nodeName, salt string) string {
	hashSum := sha256.Sum256([]byte(nodeName + ":" + salt))
	return hex.EncodeToString(hashSum[:])
}

// pickSubsetsMinRemovals ensures that there are no node removals from current subset unless the node no longer exists
// or the subset size has reduced. Subset size can reduce if a new zone got added in the cluster and the per-zone limit
// now reduces.
// This function takes a list of nodes, hash salt, count, current set and returns a subset of size - 'count'.
// If the input list is smaller than the desired subset count, the entire list is returned. The hash salt
// is used so that a different subset is returned even when the same node list is passed in, for a different salt value.
// It also keeps the subset relatively stable for the same service.
// Example 1 - Recalculate subset, subset size increase.
// nodes = [node1 node2 node3 node4 node5], Current subset - [node3, node2, node5], count 4
// sorted list is [node3 node2 node5 node4 node1]
// Output [node3, node2, node5, node4] - No removals in existing subset.
// ---------------------------------------------------------------------------------------------------------
// Example 2 - Recalculate subset, new node got added.
// nodes = [node1 node2 node3 node4 node5, node6], Current subset - [node3, node2, node5, node4], count 4
// sorted list is [node3 node6 node2 node5 node4 node1]
// Output [node3, node2, node5, node4] - No removals in existing subset even though node6 shows up at a lower index
// in the sorted list.
// ---------------------------------------------------------------------------------------------------------
// Example 2 - Recalculate subset, node3 got removed.
// nodes = [node1 node2 node4 node5, node6], Current subset - [node3, node2, node5, node4], count 4
// sorted list is [node6 node2 node5 node4 node1]
// Output [node2, node5, node4 node6]
func pickSubsetsMinRemovals(nodes []*nodeWithSubnet, salt string, count int, current []negtypes.NetworkEndpoint) []*nodeWithSubnet {
	if len(nodes) < count {
		return nodes
	}
	subset := make([]*nodeWithSubnet, 0, count)
	info := make([]*NodeInfo, len(nodes))
	// Generate hashed names for all cluster nodes and sort them alphabetically, based on the hashed string.
	for i, nodeAndSubnet := range nodes {
		info[i] = &NodeInfo{i, getHashedName(nodeAndSubnet.node.Name, salt), false}
	}
	sort.Slice(info, func(i, j int) bool {
		return info[i].hashedName < info[j].hashedName
	})
	// Pick all nodes from existing subset if still available.
	for _, ep := range current {
		curHashName := getHashedName(ep.Node, salt)
		for _, nodeInfo := range info {
			if nodeInfo.hashedName == curHashName {
				subset = append(subset, nodes[nodeInfo.index])
				nodeInfo.skip = true
			} else if nodeInfo.hashedName > curHashName {
				break
			}
		}
	}
	if len(subset) >= count {
		// trim the subset to the given subset size, remove extra nodes.
		subset = subset[:count]
		return subset
	}
	for _, val := range info {
		if val.skip {
			// This node was already picked as it is part of the current subset.
			continue
		}
		subset = append(subset, nodes[val.index])
		if len(subset) == count {
			break
		}
	}
	return subset
}

// ZoneInfo contains the name and number of nodes for a particular zone.
// this struct is used for sorting zones according to node count.
type ZoneInfo struct {
	Name      string
	NodeCount int
}

func (z ZoneInfo) String() string {
	return fmt.Sprintf("%s: %d", z.Name, z.NodeCount)
}

// ByNodeCount implements sort.Interface for []ZoneInfo based on
// the node count.
type ByNodeCount []ZoneInfo

func (a ByNodeCount) Len() int      { return len(a) }
func (a ByNodeCount) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a ByNodeCount) Less(i, j int) bool {
	// To solve ties and always return the same order between process restarts
	if a[i].NodeCount == a[j].NodeCount {
		return a[i].Name < a[j].Name
	}

	return a[i].NodeCount < a[j].NodeCount
}

// sortZones takes a map of zone to nodes list and returns a list of ZoneInfo.
// The ZoneInfo list is sorted in increasing order of the number of nodes in that zone.
func sortZones(nodesPerZone map[string][]*nodeWithSubnet) []ZoneInfo {
	input := []ZoneInfo{}
	for zone, nodes := range nodesPerZone {
		input = append(input, ZoneInfo{zone, len(nodes)})
	}
	sort.Sort(ByNodeCount(input))
	return input
}

// nodeWithSubnet holds the node object + the subnet the node is in.
// This is to avoid having to resolve node subnets again in the subset calculations.
type nodeWithSubnet struct {
	node   *v1.Node
	subnet string
}

func newNodeWithSubnet(node *v1.Node, subnet string) *nodeWithSubnet {
	return &nodeWithSubnet{
		node:   node,
		subnet: subnet,
	}
}

// getSubsetPerZone creates a subset of nodes from the given list of nodes, for each zone provided.
// The output is a map of zone string to NEG subset.
// In order to pick as many nodes as possible given the total limit, the following algorithm is used:
// A) Leave existing endpoints where they are. Detaching endpoints is time consuming and causes connection draining. We want to avoid that.
// B) For the rest of endpoints following algorithm is used:
//
//  1. The zones are sorted in increasing order of the total number of nodes.
//
//  2. The number of nodes to be selected is divided equally among the zones. If there are 4 zones and the limit is 250,
//
// the algorithm attempts to pick 250/4 from the first zone. If 'n' nodes were selected from zone1, the limit for
// zone2 is (250 - n)/3. For the third zone, it is (250 - n - m)/2, if m nodes were picked from zone2.
// Since the number of nodes will keep increasing in successive zones due to the sorting, even if fewer nodes were
// present in some zones, more nodes will be picked from other nodes, taking the total subset size to the given limit
// whenever possible.
func getSubsetPerZone(nodesPerZone map[string][]*nodeWithSubnet, totalLimit int, svcID string, currentMap map[negtypes.NEGLocation]negtypes.NetworkEndpointSet, logger klog.Logger, networkInfo *network.NetworkInfo) (map[negtypes.NEGLocation]negtypes.NetworkEndpointSet, error) {
	result := make(map[negtypes.NEGLocation]negtypes.NetworkEndpointSet)

	subsetSize := 0
	// initialize zonesRemaining to the total number of zones.
	zonesRemaining := len(nodesPerZone)

	defaultSubnet, err := utils.KeyName(networkInfo.SubnetworkURL)
	if err != nil {
		logger.Error(err, "Errored getting default subnet from NetworkInfo when calculating L4 endpoints")
		return nil, err
	}

	// Remove nodes that are already in use in currentMap
	totalLimit, nodesPerZone = pickOutUsedEndpoints(currentMap, nodesPerZone, totalLimit, result)

	// Sort zones in increasing order of the remaining node count.
	zoneList := sortZones(nodesPerZone)

	for _, zone := range zoneList {
		// make sure there is an entry for the defaultSubnet in each zone, even if there will be no endpoints in there (maintains the old behavior).
		defaultSubnetLocation := negtypes.NEGLocation{Zone: zone.Name, Subnet: defaultSubnet}
		if _, ok := result[defaultSubnetLocation]; !ok {
			result[defaultSubnetLocation] = negtypes.NewNetworkEndpointSet()
		}

		// split the limit across the leftover zones.
		subsetSize = totalLimit / zonesRemaining
		logger.Info("Picking subset for a zone", "subsetSize", subsetSize, "zone", zone, "svcID", svcID)
		var currentList []negtypes.NetworkEndpoint
		if currentMap != nil {
			currentList = getNetworkEndpointsForZone(zone.Name, currentMap)
		}
		subset := pickSubsetsMinRemovals(nodesPerZone[zone.Name], svcID, subsetSize, currentList)
		for _, nodeAndSubnet := range subset {
			var ip string
			if !networkInfo.IsDefault {
				ip = network.GetNodeIPForNetwork(nodeAndSubnet.node, networkInfo.K8sNetwork)
			} else {
				ip = utils.GetNodePrimaryIP(nodeAndSubnet.node, logger)
			}
			egi := negtypes.NEGLocation{Zone: zone.Name, Subnet: nodeAndSubnet.subnet}
			if _, ok := result[egi]; !ok {
				result[egi] = negtypes.NewNetworkEndpointSet()
			}
			result[egi].Insert(negtypes.NetworkEndpoint{Node: nodeAndSubnet.node.Name, IP: ip})
		}
		totalLimit -= len(subset)
		zonesRemaining--
	}
	return result, nil
}

// Removes nodes that are already used in the currentMap.
//
// Adds endpoints to the result in place.
// Returns totalLimit left and nodesPerZone after removal.
func pickOutUsedEndpoints(currentMap map[negtypes.NEGLocation]negtypes.NetworkEndpointSet, nodesPerZone map[string][]*nodeWithSubnet, totalLimit int, result map[negtypes.NEGLocation]negtypes.NetworkEndpointSet) (int, map[string][]*nodeWithSubnet) {
	// We can use map to have O(1) find and delete
	m := nodesToMap(nodesPerZone)

	for location, endpoints := range currentMap {
		for endpoint := range endpoints {
			key := nameSubnetKey{endpoint.Node, location.Subnet}
			if _, ok := m[location.Zone][key]; !ok {
				continue
			}

			delete(m[location.Zone], key)

			if _, ok := result[location]; !ok {
				result[location] = negtypes.NewNetworkEndpointSet()
			}
			result[location].Insert(endpoint)
			totalLimit--
		}
	}

	return totalLimit, mapToNodes(m)
}

type nameSubnetKey struct {
	name   string
	subnet string
}

func nodesToMap(nodesPerZone map[string][]*nodeWithSubnet) map[string]map[nameSubnetKey]*nodeWithSubnet {
	m := make(map[string]map[nameSubnetKey]*nodeWithSubnet)
	for zone, nodes := range nodesPerZone {
		m[zone] = make(map[nameSubnetKey]*nodeWithSubnet)
		for _, node := range nodes {
			m[zone][nameSubnetKey{node.node.Name, node.subnet}] = node
		}
	}
	return m
}

func mapToNodes(m map[string]map[nameSubnetKey]*nodeWithSubnet) map[string][]*nodeWithSubnet {
	nodesPerZone := make(map[string][]*nodeWithSubnet)
	for zone, nodes := range m {
		// We NEED to have at least an empty slice for each zone
		// as some code depends on this behavior.
		nodesPerZone[zone] = make([]*nodeWithSubnet, 0)
		for _, node := range nodes {
			nodesPerZone[zone] = append(nodesPerZone[zone], node)
		}
	}
	return nodesPerZone
}

// getNetworkEndpointsForZone gets all endpoints for a matching zone.
// it will get all nodes in the zone no matter which subnet the nodes are in.
func getNetworkEndpointsForZone(zone string, currentMap map[negtypes.NEGLocation]negtypes.NetworkEndpointSet) []negtypes.NetworkEndpoint {
	var results [][]negtypes.NetworkEndpoint
	for negLocation, endpointSet := range currentMap {
		if negLocation.Zone == zone {
			results = append(results, endpointSet.List())
		}
	}
	// Non MSC clusters will have only one result per zone, avoid iterative appends in that case.
	if len(results) == 1 {
		return results[0]
	}
	return slices.Concat(results...)
}
