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
	"k8s.io/client-go/tools/cache"
	"k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog/v2"
)

// LocalL4ILBEndpointGetter implements the NetworkEndpointsCalculator interface.
// It exposes methods to calculate Network endpoints for GCE_VM_IP NEGs when the service
// uses "ExternalTrafficPolicy: Local" mode.
// In this mode, the endpoints of the NEG are calculated by listing the nodes that host the service endpoints(pods)
// for the given service. These candidate nodes picked as is, if the count is less than the subset size limit(250).
// Otherwise, a subset of nodes is selected.
// In a cluster with nodes node1... node 50. If nodes node10 to node 45 run the pods for a given ILB service, all these
// nodes - node10, node 11 ... node45 will be part of the subset.
type LocalL4ILBEndpointsCalculator struct {
	nodeLister      listers.NodeLister
	zoneGetter      types.ZoneGetter
	subsetSizeLimit int
	svcId           string
	logger          klog.Logger
}

func NewLocalL4ILBEndpointsCalculator(nodeLister listers.NodeLister, zoneGetter types.ZoneGetter, svcId string, logger klog.Logger) *LocalL4ILBEndpointsCalculator {
	return &LocalL4ILBEndpointsCalculator{
		nodeLister:      nodeLister,
		zoneGetter:      zoneGetter,
		subsetSizeLimit: maxSubsetSizeLocal,
		svcId:           svcId,
		logger:          logger.WithName("LocalL4ILBEndpointsCalculator"),
	}
}

// Mode indicates the mode that the EndpointsCalculator is operating in.
func (l *LocalL4ILBEndpointsCalculator) Mode() types.EndpointsCalculatorMode {
	return types.L4LocalMode
}

// CalculateEndpoints determines the endpoints in the NEGs based on the current service endpoints and the current NEGs.
func (l *LocalL4ILBEndpointsCalculator) CalculateEndpoints(eds []types.EndpointsData, currentMap map[string]types.NetworkEndpointSet) (map[string]types.NetworkEndpointSet, types.EndpointPodMap, error) {
	// List all nodes where the service endpoints are running. Get a subset of the desired count.
	zoneNodeMap := make(map[string][]*v1.Node)
	processedNodes := sets.String{}
	numEndpoints := 0
	candidateNodeCheck := utils.CandidateNodesPredicateIncludeUnreadyExcludeUpgradingNodes
	for _, ed := range eds {
		for _, addr := range ed.Addresses {
			if addr.NodeName == nil {
				l.logger.V(2).Info("Address inside Endpoints does not have an associated node. Skipping", "address", addr.Addresses, "endpoints", klog.KRef(ed.Meta.Namespace, ed.Meta.Name))
				continue
			}
			if addr.TargetRef == nil {
				l.logger.V(2).Info("Address inside Endpoints does not have an associated pod. Skipping", "address", addr.Addresses, "endpoints", klog.KRef(ed.Meta.Namespace, ed.Meta.Name))
				continue
			}
			numEndpoints++
			if processedNodes.Has(*addr.NodeName) {
				continue
			}
			processedNodes.Insert(*addr.NodeName)
			node, err := l.nodeLister.Get(*addr.NodeName)
			if err != nil {
				l.logger.Error(err, "failed to retrieve node object", "nodeName", *addr.NodeName)
				continue
			}
			if ok := candidateNodeCheck(node); !ok {
				l.logger.Info("Dropping Node from subset since it is not a valid LB candidate", "nodeName", node.Name)
				continue
			}
			zone, err := l.zoneGetter.GetZoneForNode(node.Name)
			if err != nil {
				l.logger.Error(err, "Unable to find zone for node, skipping", "nodeName", node.Name)
				continue
			}
			zoneNodeMap[zone] = append(zoneNodeMap[zone], node)
		}
	}
	if numEndpoints == 0 {
		// Not having backends will cause clients to see connection timeout instead of an "ICMP ConnectionRefused".
		return nil, nil, nil
	}
	// Compute the networkEndpoints, with total endpoints count <= l.subsetSizeLimit
	klog.V(2).Infof("Got zoneNodeMap as input for service", "zoneNodeMap", nodeMapToString(zoneNodeMap), "serviceID", l.svcId)
	subsetMap, err := getSubsetPerZone(zoneNodeMap, l.subsetSizeLimit, l.svcId, currentMap, l.logger)
	return subsetMap, nil, err
}

// ClusterL4ILBEndpointGetter implements the NetworkEndpointsCalculator interface.
// It exposes methods to calculate Network endpoints for GCE_VM_IP NEGs when the service
// uses "ExternalTrafficPolicy: Cluster" mode This is the default mode.
// In this mode, the endpoints of the NEG are calculated by selecting nodes at random. Up to 25(subset size limit in this
// mode) are selected.
type ClusterL4ILBEndpointsCalculator struct {
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

func NewClusterL4ILBEndpointsCalculator(nodeLister listers.NodeLister, zoneGetter types.ZoneGetter, svcId string, logger klog.Logger) *ClusterL4ILBEndpointsCalculator {
	return &ClusterL4ILBEndpointsCalculator{
		nodeLister:      nodeLister,
		zoneGetter:      zoneGetter,
		subsetSizeLimit: maxSubsetSizeDefault,
		svcId:           svcId,
		logger:          logger.WithName("ClusterL4ILBEndpointsCalculator")}
}

// Mode indicates the mode that the EndpointsCalculator is operating in.
func (l *ClusterL4ILBEndpointsCalculator) Mode() types.EndpointsCalculatorMode {
	return types.L4ClusterMode
}

// CalculateEndpoints determines the endpoints in the NEGs based on the current service endpoints and the current NEGs.
func (l *ClusterL4ILBEndpointsCalculator) CalculateEndpoints(_ []types.EndpointsData, currentMap map[string]types.NetworkEndpointSet) (map[string]types.NetworkEndpointSet, types.EndpointPodMap, error) {
	// In this mode, any of the cluster nodes can be part of the subset, whether or not a matching pod runs on it.
	nodes, _ := utils.ListWithPredicate(l.nodeLister, utils.CandidateNodesPredicateIncludeUnreadyExcludeUpgradingNodes)

	zoneNodeMap := make(map[string][]*v1.Node)
	for _, node := range nodes {
		zone, err := l.zoneGetter.GetZoneForNode(node.Name)
		if err != nil {
			l.logger.Error(err, "Unable to find zone for node skipping", "nodeName", node.Name)
			continue
		}
		zoneNodeMap[zone] = append(zoneNodeMap[zone], node)
	}
	klog.V(2).Infof("Got zoneNodeMap as input for service", "zoneNodeMap", nodeMapToString(zoneNodeMap), "serviceID", l.svcId)
	// Compute the networkEndpoints, with total endpoints <= l.subsetSizeLimit.
	subsetMap, err := getSubsetPerZone(zoneNodeMap, l.subsetSizeLimit, l.svcId, currentMap, l.logger)
	return subsetMap, nil, err
}

// L7EndpointsCalculator implements methods to calculate Network endpoints for VM_IP_PORT NEGs
type L7EndpointsCalculator struct {
	zoneGetter          types.ZoneGetter
	servicePortName     string
	podLister           cache.Indexer
	subsetLabels        string
	networkEndpointType types.NetworkEndpointType
	logger              klog.Logger
}

func NewL7EndpointsCalculator(zoneGetter types.ZoneGetter, podLister cache.Indexer, svcPortName, subsetLabels string, endpointType types.NetworkEndpointType, logger klog.Logger) *L7EndpointsCalculator {
	return &L7EndpointsCalculator{
		zoneGetter:          zoneGetter,
		servicePortName:     svcPortName,
		podLister:           podLister,
		subsetLabels:        subsetLabels,
		networkEndpointType: endpointType,
		logger:              logger.WithName("L7EndpointsCalculator"),
	}
}

// Mode indicates the mode that the EndpointsCalculator is operating in.
func (l *L7EndpointsCalculator) Mode() types.EndpointsCalculatorMode {
	return types.L7Mode
}

// CalculateEndpoints determines the endpoints in the NEGs based on the current service endpoints and the current NEGs.
func (l *L7EndpointsCalculator) CalculateEndpoints(eds []types.EndpointsData, _ map[string]types.NetworkEndpointSet) (map[string]types.NetworkEndpointSet, types.EndpointPodMap, error) {
	return toZoneNetworkEndpointMap(eds, l.zoneGetter, l.servicePortName, l.podLister, l.subsetLabels, l.networkEndpointType)
}

func nodeMapToString(nodeMap map[string][]*v1.Node) string {
	var str []string
	for zone, nodeList := range nodeMap {
		str = append(str, fmt.Sprintf("Zone %s: %d nodes", zone, len(nodeList)))
	}
	return strings.Join(str, ",")
}
