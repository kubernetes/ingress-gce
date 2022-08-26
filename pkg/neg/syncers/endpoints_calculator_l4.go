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
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog/v2"
)

type l4EndpointsCalculator struct {
	// nodeLister is used for listing all the nodes in the cluster when calculating the subset.
	nodeLister listers.NodeLister
	// zoneGetter looks up the zone for a given node when calculating subsets.
	zoneGetter types.ZoneGetter
	// subsetSizeLimit is the max value of the subset size in this mode.
	subsetSizeLimit int
	// svcId is the unique identifier for the service, that is used as a salt when hashing nodenames.
	svcId string

	logger klog.Logger
}

// LocalL4ILBEndpointGetter implements the NetworkEndpointsCalculator interface.
// It exposes methods to calculate Network endpoints for GCE_VM_IP NEGs when the service
// uses "ExternalTrafficPolicy: Local" mode.
// In this mode, the endpoints of the NEG are calculated by listing the nodes that host the service endpoints(pods)
// for the given service. These candidate nodes picked as is, if the count is less than the subset size limit(250).
// Otherwise, a subset of nodes is selected.
// In a cluster with nodes node1... node 50. If nodes node10 to node 45 run the pods for a given ILB service, all these
// nodes - node10, node 11 ... node45 will be part of the subset.
type LocalL4ILBEndpointsCalculator struct {
	l4EndpointsCalculator
}

func NewLocalL4ILBEndpointsCalculator(nodeLister listers.NodeLister, zoneGetter types.ZoneGetter, svcId string, logger klog.Logger) *LocalL4ILBEndpointsCalculator {
	return &LocalL4ILBEndpointsCalculator{
		l4EndpointsCalculator{
			nodeLister:      nodeLister,
			zoneGetter:      zoneGetter,
			subsetSizeLimit: maxSubsetSizeLocal,
			svcId:           svcId,
			logger:          logger.WithName("LocalL4ILBEndpointsCalculator"),
		},
	}
}

// Mode indicates the mode that the EndpointsCalculator is operating in.
func (l4Local *LocalL4ILBEndpointsCalculator) Mode() types.EndpointsCalculatorMode {
	return types.L4LocalMode
}

// CalculateEndpoints determines the endpoints in the NEGs based on the current service endpoints and the current NEGs.
func (l4Local *LocalL4ILBEndpointsCalculator) CalculateEndpoints(eds []types.EndpointsData, currentMap map[string]types.NetworkEndpointSet) (map[string]types.NetworkEndpointSet, types.EndpointPodMap, error) {
	// List all nodes where the service endpoints are running. Get a subset of the desired count.
	zoneNodeMap := make(map[string][]*v1.Node)
	processedNodes := sets.String{}
	numEndpoints := 0
	candidateNodeCheck := utils.CandidateNodesPredicateIncludeUnreadyExcludeUpgradingNodes
	for _, ed := range eds {
		for _, addr := range ed.Addresses {
			if addr.NodeName == nil {
				l4Local.logger.V(2).Info("Address inside Endpoints does not have an associated node. Skipping", "address", addr.Addresses, "endpoints", klog.KRef(ed.Meta.Namespace, ed.Meta.Name))
				continue
			}
			if addr.TargetRef == nil {
				l4Local.logger.V(2).Info("Address inside Endpoints does not have an associated pod. Skipping", "address", addr.Addresses, "endpoints", klog.KRef(ed.Meta.Namespace, ed.Meta.Name))
				continue
			}
			numEndpoints++
			if processedNodes.Has(*addr.NodeName) {
				continue
			}
			processedNodes.Insert(*addr.NodeName)
			node, err := l4Local.nodeLister.Get(*addr.NodeName)
			if err != nil {
				l4Local.logger.Error(err, "failed to retrieve node object", "nodeName", *addr.NodeName)
				continue
			}
			if ok := candidateNodeCheck(node); !ok {
				l4Local.logger.Info("Dropping Node from subset since it is not a valid LB candidate", "nodeName", node.Name)
				continue
			}
			zone, err := l4Local.zoneGetter.GetZoneForNode(node.Name)
			if err != nil {
				l4Local.logger.Error(err, "Unable to find zone for node, skipping", "nodeName", node.Name)
				continue
			}
			zoneNodeMap[zone] = append(zoneNodeMap[zone], node)
		}
	}
	if numEndpoints == 0 {
		// Not having backends will cause clients to see connection timeout instead of an "ICMP ConnectionRefused".
		return nil, nil, nil
	}
	// Compute the networkEndpoints, with total endpoints count <= l4Local.subsetSizeLimit
	klog.V(2).Infof("Got zoneNodeMap as input for service", "zoneNodeMap", nodeMapToString(zoneNodeMap), "serviceID", l4Local.svcId)
	subsetMap, err := l4Local.getSubsetPerZone(zoneNodeMap, currentMap)
	return subsetMap, nil, err
}

// ClusterL4ILBEndpointGetter implements the NetworkEndpointsCalculator interface.
// It exposes methods to calculate Network endpoints for GCE_VM_IP NEGs when the service
// uses "ExternalTrafficPolicy: Cluster" mode This is the default mode.
// In this mode, the endpoints of the NEG are calculated by selecting nodes at random. Upto 25(subset size limit in this
// mode) are selected.
type ClusterL4ILBEndpointsCalculator struct {
	l4EndpointsCalculator
}

func NewClusterL4ILBEndpointsCalculator(nodeLister listers.NodeLister, zoneGetter types.ZoneGetter, svcId string, logger klog.Logger) *ClusterL4ILBEndpointsCalculator {
	return &ClusterL4ILBEndpointsCalculator{
		l4EndpointsCalculator{
			nodeLister:      nodeLister,
			zoneGetter:      zoneGetter,
			subsetSizeLimit: maxSubsetSizeDefault,
			svcId:           svcId,
			logger:          logger.WithName("ClusterL4ILBEndpointsCalculator")},
	}
}

// Mode indicates the mode that the EndpointsCalculator is operating in.
func (l4cluster *ClusterL4ILBEndpointsCalculator) Mode() types.EndpointsCalculatorMode {
	return types.L4ClusterMode
}

// CalculateEndpoints determines the endpoints in the NEGs based on the current service endpoints and the current NEGs.
func (l4cluster *ClusterL4ILBEndpointsCalculator) CalculateEndpoints(_ []types.EndpointsData, currentMap map[string]types.NetworkEndpointSet) (map[string]types.NetworkEndpointSet, types.EndpointPodMap, error) {
	// In this mode, any of the cluster nodes can be part of the subset, whether or not a matching pod runs on it.
	nodes, _ := utils.ListWithPredicate(l4cluster.nodeLister, utils.CandidateNodesPredicateIncludeUnreadyExcludeUpgradingNodes)

	zoneNodeMap := make(map[string][]*v1.Node)
	for _, node := range nodes {
		zone, err := l4cluster.zoneGetter.GetZoneForNode(node.Name)
		if err != nil {
			l4cluster.logger.Error(err, "Unable to find zone for node skipping", "nodeName", node.Name)
			continue
		}
		zoneNodeMap[zone] = append(zoneNodeMap[zone], node)
	}
	klog.V(2).Infof("Got zoneNodeMap as input for service", "zoneNodeMap", nodeMapToString(zoneNodeMap), "serviceID", l4cluster.svcId)
	// Compute the networkEndpoints, with total endpoints <= l4cluster.subsetSizeLimit.
	subsetMap, err := l4cluster.getSubsetPerZone(zoneNodeMap, currentMap)
	return subsetMap, nil, err
}

func nodeMapToString(nodeMap map[string][]*v1.Node) string {
	var str []string
	for zone, nodeList := range nodeMap {
		str = append(str, fmt.Sprintf("Zone %s: %d nodes", zone, len(nodeList)))
	}
	return strings.Join(str, ",")
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
func (l4calc l4EndpointsCalculator) getSubsetPerZone(nodesPerZone map[string][]*v1.Node, currentMap map[string]types.NetworkEndpointSet) (map[string]types.NetworkEndpointSet, error) {
	result := make(map[string]types.NetworkEndpointSet)
	var currentList []types.NetworkEndpoint

	subsetSize := 0
	// initialize zonesRemaining to the total number of zones.
	zonesRemaining := len(nodesPerZone)
	// Sort zones in increasing order of node count.
	zoneList := sortZones(nodesPerZone)

	for _, zone := range zoneList {
		// split the limit across the leftover zones.
		subsetSize = l4calc.subsetSizeLimit / zonesRemaining
		l4calc.logger.Info("Picking subset for a zone", "subsetSize", subsetSize, "zone", zone, "svcID", l4calc.svcId)
		result[zone.Name] = types.NewNetworkEndpointSet()
		if currentMap != nil {
			if zset, ok := currentMap[zone.Name]; ok && zset != nil {
				currentList = zset.List()
			} else {
				currentList = nil
			}
		}
		subset := pickSubsetsMinRemovals(nodesPerZone[zone.Name], l4calc.svcId, subsetSize, currentList)
		for _, node := range subset {
			result[zone.Name].Insert(types.NetworkEndpoint{Node: node.Name, IP: utils.GetNodePrimaryIP(node)})
		}
		l4calc.subsetSizeLimit -= len(subset)
		zonesRemaining--
	}
	return result, nil
}
