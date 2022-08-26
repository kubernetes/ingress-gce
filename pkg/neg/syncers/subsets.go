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
	"sort"

	v1 "k8s.io/api/core/v1"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog/v2"
)

const (
	// Max number of subsets in ExternalTrafficPolicy:Local
	maxSubsetSizeLocal = 250
	// Max number of subsets in ExternalTrafficPolicy:Cluster, which is the default mode.
	maxSubsetSizeDefault = 25
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

func (a ByNodeCount) Len() int           { return len(a) }
func (a ByNodeCount) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ByNodeCount) Less(i, j int) bool { return a[i].NodeCount < a[j].NodeCount }

// sortZones takes a map of zone to nodes list and returns a list of ZoneInfo.
// The ZoneInfo list is sorted in increasing order of the number of nodes in that zone.
func sortZones(nodesPerZone map[string][]*v1.Node) []ZoneInfo {
	input := []ZoneInfo{}
	for zone, nodes := range nodesPerZone {
		input = append(input, ZoneInfo{zone, len(nodes)})
	}
	sort.Sort(ByNodeCount(input))
	return input
}

type hasher interface {
	hash(s string) string
}

type l4SubsetPerZoneCalculator struct {
	nodeHasher hasher

	maxSubsetSize int
	logger        klog.Logger
}

func newL4SubsetPerZoneCalculator(serviceID string, maxSubsetSize int, logger klog.Logger) *l4SubsetPerZoneCalculator {
	return &l4SubsetPerZoneCalculator{
		nodeHasher:    newSaltHasher(serviceID),
		maxSubsetSize: maxSubsetSize,
		logger:        logger,
	}
}

// getSubsetsPerZone creates a subset of nodes from the given list of nodes, for each zone provided.
// The output is a map of zone string to NEG subset.
// In order to pick as many nodes as possible given the total limit, the following algorithm is used:
// 1) The zones are sorted in increasing order of the total number of nodes.
// 2) The number of nodes to be selected is divided equally among the zones. If there are 4 zones and the limit is 250,
//
//	the algorithm attempts to pick 250/4 from the first zone. If 'n' nodes were selected from zone1, the limit for
//	zone2 is (250 - n)/3. For the third zone, it is (250 - n - m)/2, if m nodes were picked from zone2.
//	Since the number of nodes will keep increasing in successive zones due to the sorting, even if fewer nodes were
//	present in some zones, more nodes will be picked from other nodes, taking the total subset size to the given limit
//	whenever possible.
func (sc *l4SubsetPerZoneCalculator) calculateSubsetsPerZone(nodesPerZone map[string][]*v1.Node, currentSubsetsPerZone map[string]negtypes.NetworkEndpointSet) map[string]negtypes.NetworkEndpointSet {
	result := make(map[string]negtypes.NetworkEndpointSet)

	// Sort zones in increasing order of node count.
	zoneList := sortZones(nodesPerZone)
	leftSubsetSpace := sc.maxSubsetSize

	for i, zone := range zoneList {
		var currentList []negtypes.NetworkEndpoint
		if currentSubsetsPerZone != nil {
			if zset, ok := currentSubsetsPerZone[zone.Name]; ok && zset != nil {
				currentList = zset.List()
			}
		}

		// split the limit across the leftover zones.
		zonesRemaining := len(nodesPerZone) - i
		subsetSize := leftSubsetSpace / zonesRemaining

		sc.logger.Info("Picking subset for a zone", "maxSubsetSize", subsetSize, "zone", zone)
		subset := sc.pickSubsetMinRemovals(nodesPerZone[zone.Name], currentList, subsetSize)
		result[zone.Name] = nodesToNetworkEndpointSet(subset)
		leftSubsetSpace -= len(subset)
	}
	return result
}

// pickSubsetsMinRemovals ensures that there are no node removals from current subset unless the node no longer exists
// or the subset size has reduced. Subset size can reduce if a new zone got added in the cluster and the per-zone limit
// now reduces.
// This function takes a list of nodes, hash salt, count, current set and returns a subset of size - 'count'.
// If the input list is smaller than the desired subset count, the entire list is returned.
// The hash salt is used so that a different subset is returned even when the same node list is passed in, for a different salt value.
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
func (sc *l4SubsetPerZoneCalculator) pickSubsetMinRemovals(nodes []*v1.Node, current []negtypes.NetworkEndpoint, expectedSize int) []*v1.Node {
	if len(nodes) < expectedSize {
		return nodes
	}
	info := make([]*NodeInfo, len(nodes))
	// Generate hashed names for all cluster nodes and sort them alphabetically, based on the hashed string.
	for i, node := range nodes {
		info[i] = &NodeInfo{i, sc.nodeHasher.hash(node.Name), false}
	}
	sort.Slice(info, func(i, j int) bool {
		return info[i].hashedName < info[j].hashedName
	})

	subset := make([]*v1.Node, 0, expectedSize)
	// Pick all nodes from existing subset if still available.
	for _, ep := range current {
		curHashName := sc.nodeHasher.hash(ep.Node)
		for _, nodeInfo := range info {
			if nodeInfo.hashedName == curHashName {
				subset = append(subset, nodes[nodeInfo.index])
				nodeInfo.skip = true
			} else if nodeInfo.hashedName > curHashName {
				break
			}
		}
	}
	if len(subset) >= expectedSize {
		// trim the subset to the given subset size, remove extra nodes.
		subset = subset[:expectedSize]
		return subset
	}
	for _, val := range info {
		if val.skip {
			// This node was already picked as it is part of the current subset.
			continue
		}
		subset = append(subset, nodes[val.index])
		if len(subset) == expectedSize {
			break
		}
	}
	return subset
}

func nodesToNetworkEndpointSet(nodes []*v1.Node) negtypes.NetworkEndpointSet {
	result := negtypes.NewNetworkEndpointSet()
	for _, node := range nodes {
		result.Insert(negtypes.NetworkEndpoint{Node: node.Name, IP: utils.GetNodePrimaryIP(node)})
	}
	return result
}
