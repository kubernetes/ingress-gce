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
	"strings"

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

// nodeSkipReason explains why a node was left out of the NEG. Individual skips
// are logged at a low verbosity because in a misconfigured cluster they repeat
// per node on every sync; the reason is carried back to getSubsetPerZone so that
// losing *every* node in a zone can be reported once, loudly.
type nodeSkipReason string

const (
	skipReasonIPv6Disabled nodeSkipReason = "node has only an IPv6 internal address and --enable-ipv6-node-neg-endpoints is not set"
	skipReasonNotOnNetwork nodeSkipReason = "node is not connected to the network"
	skipReasonNoInternalIP nodeSkipReason = "node has no internal IP address"
)

// nodeEndpoint builds the NetworkEndpoint for a node. The addresses come from
// network.GetNodeIPsForNetwork or utils.GetNodeInternalIPs, both of which guarantee at
// most one canonical address per family, so no further validation is done here.
// It returns false, plus the reason, if the node has no usable address, in which
// case the node must be left out of the NEG.
//
// The endpoint is always single-stack, carrying one address and never both.
// EnableIPv6NodeNEGEndpoints exists to support IPv6-only clusters, where a node
// has no IPv4 address at all, so IPv6 is a fallback rather than an addition:
// whenever a node has an IPv4 address that is the one used. Emitting both
// families would turn every dual-stack node's existing endpoint into a
// different endpoint, and NetworkEndpoint is a set key, so the syncer would
// have to detach and re-attach every node before the NEG converged again.
func nodeEndpoint(node *v1.Node, networkInfo *network.NetworkInfo, populateIPv6 bool, logger klog.Logger) (negtypes.NetworkEndpoint, nodeSkipReason, bool) {
	nodeIPv4, nodeIPv6 := "", ""
	if !networkInfo.IsDefault {
		nodeIPv4, nodeIPv6 = network.GetNodeIPsForNetwork(node, networkInfo.K8sNetwork)
	} else {
		nodeIPv4, nodeIPv6 = utils.GetNodeInternalIPs(node)
	}

	newEndpoint := negtypes.NetworkEndpoint{Node: node.Name}
	switch {
	case nodeIPv4 != "":
		newEndpoint.IP = nodeIPv4
	case populateIPv6 && nodeIPv6 != "":
		newEndpoint.IPv6 = nodeIPv6
	}
	if newEndpoint.IP == "" && newEndpoint.IPv6 == "" {
		// Skipping nodes with no usable address prevents sending malformed data to the
		// Cloud API, which would result in errors.
		var reason nodeSkipReason
		switch {
		case nodeIPv6 != "":
			// The node does have an address; populateIPv6 is what excludes it. Reporting
			// this as a node problem would send the reader to the Node object instead of
			// to the flag that actually governs it.
			reason = skipReasonIPv6Disabled
		case !networkInfo.IsDefault:
			// On a non-default network a node is routinely not attached to the network at
			// all, so this is expected rather than an error.
			reason = skipReasonNotOnNetwork
		default:
			reason = skipReasonNoInternalIP
		}
		logger.V(2).Info("Skipping node", "node", node.Name, "reason", string(reason),
			"ipv4", nodeIPv4, "ipv6", nodeIPv6)
		return negtypes.NetworkEndpoint{}, reason, false
	}

	return newEndpoint, "", true
}

// getSubsetPerZone creates a subset of nodes from the given list of nodes, for each zone provided.
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
func getSubsetPerZone(nodesPerZone map[string][]*nodeWithSubnet, totalLimit int, svcID string, currentMap map[negtypes.NEGLocation]negtypes.NetworkEndpointSet, logger klog.Logger, networkInfo *network.NetworkInfo, populateIPv6 bool) (map[negtypes.NEGLocation]negtypes.NetworkEndpointSet, error) {
	result := make(map[negtypes.NEGLocation]negtypes.NetworkEndpointSet)

	subsetSize := 0
	// initialize zonesRemaining to the total number of zones.
	zonesRemaining := len(nodesPerZone)
	// Sort zones in increasing order of node count.
	zoneList := sortZones(nodesPerZone)

	defaultSubnet, err := utils.KeyName(networkInfo.SubnetworkURL)
	if err != nil {
		logger.Error(err, "Errored getting default subnet from NetworkInfo when calculating L4 endpoints")
		return nil, err
	}

	for _, zone := range zoneList {
		// make sure there is an entry for the defaultSubnet in each zone, even if there will be no endpoints in there (maintains the old behavior).
		result[negtypes.NEGLocation{Zone: zone.Name, Subnet: defaultSubnet}] = negtypes.NewNetworkEndpointSet()
		// split the limit across the leftover zones.
		subsetSize = totalLimit / zonesRemaining
		logger.Info("Picking subset for a zone", "subsetSize", subsetSize, "zone", zone, "svcID", svcID)
		var currentList []negtypes.NetworkEndpoint
		if currentMap != nil {
			currentList = getNetworkEndpointsForZone(zone.Name, currentMap)
		}
		subset := pickSubsetsMinRemovals(nodesPerZone[zone.Name], svcID, subsetSize, currentList)
		skipped, picked := map[nodeSkipReason]int{}, 0
		for _, nodeAndSubnet := range subset {
			newEndpoint, skipReason, ok := nodeEndpoint(nodeAndSubnet.node, networkInfo, populateIPv6, logger)
			if !ok {
				skipped[skipReason]++
				continue
			}
			picked++
			egi := negtypes.NEGLocation{Zone: zone.Name, Subnet: nodeAndSubnet.subnet}
			if _, ok := result[egi]; !ok {
				result[egi] = negtypes.NewNetworkEndpointSet()
			}
			result[egi].Insert(newEndpoint)
		}
		if picked == 0 && len(skipped) > 0 {
			// Every candidate node in this zone was dropped, so the zone's NEG ends up
			// empty and the service loses all of its backends there.
			for reason, count := range skipped {
				logger.Error(nil, "All candidate nodes in zone were skipped", "zone", zone.Name, "skippedNodes", count, "reason", string(reason), "svcID", svcID)
			}
		}
		totalLimit -= len(subset)
		zonesRemaining--
	}
	return result, nil
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

	var sorted []negtypes.NetworkEndpoint
	// Non MSC clusters will have only one result per zone, avoid iterative appends in that case.
	if len(results) == 1 {
		sorted = results[0]
	} else {
		sorted = slices.Concat(results...)
	}

	// We move from an unordered map, but want to have deterministic results later
	sortEndpoints(sorted)
	return sorted
}

// sortEndpoints will sort the endpoints in place
func sortEndpoints(e []negtypes.NetworkEndpoint) {
	slices.SortFunc(e, func(a, b negtypes.NetworkEndpoint) int {
		if c := strings.Compare(a.Node, b.Node); c != 0 {
			return c
		}
		if c := strings.Compare(a.IP, b.IP); c != 0 {
			return c
		}
		if c := strings.Compare(a.IPv6, b.IPv6); c != 0 {
			return c
		}
		return strings.Compare(a.Port, b.Port) // This would probably be empty for GCE_VM_IP
	})
}
