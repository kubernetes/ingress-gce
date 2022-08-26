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
	listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/ingress-gce/pkg/neg/types"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/klog/v2"
)

type zoneNodesGetter interface {
	getZoneToNodes(eds []types.EndpointsData) map[string][]*v1.Node
}

type subsetsPerZoneCalculator interface {
	calculateSubsetsPerZone(nodesPerZone map[string][]*v1.Node, currentSubsetsPerZone map[string]negtypes.NetworkEndpointSet) map[string]negtypes.NetworkEndpointSet
}

// L4ILBEndpointsCalculator implements the NetworkEndpointsCalculator interface.
type L4ILBEndpointsCalculator struct {
	zoneNodes      zoneNodesGetter
	subsetsPerZone subsetsPerZoneCalculator
	mode           types.EndpointsCalculatorMode
	logger         klog.Logger
}

func NewLocalL4ILBEndpointsCalculator(nodeLister listers.NodeLister, zoneGetter types.ZoneGetter, svcId string, logger klog.Logger) *L4ILBEndpointsCalculator {
	localEndpointsServiceLogger := logger.WithName(fmt.Sprintf("LocalL4ILBEndpointsCalculator-%s", svcId))
	return &L4ILBEndpointsCalculator{
		zoneNodes:      newL4ILBLocalZoneToNodesGetter(nodeLister, zoneGetter, localEndpointsServiceLogger),
		subsetsPerZone: newL4SubsetPerZoneCalculator(svcId, maxSubsetSizeLocal, localEndpointsServiceLogger),
		logger:         localEndpointsServiceLogger,
		mode:           types.L4LocalMode,
	}
}

func NewClusterL4ILBEndpointsCalculator(nodeLister listers.NodeLister, zoneGetter types.ZoneGetter, svcId string, logger klog.Logger) *L4ILBEndpointsCalculator {
	clusterEndpointsServiceLogger := logger.WithName(fmt.Sprintf("ClusterL4ILBEndpointsCalculator-%s", svcId))
	return &L4ILBEndpointsCalculator{
		zoneNodes:      newL4ILBClusterZoneToNodesGetter(nodeLister, zoneGetter, clusterEndpointsServiceLogger),
		subsetsPerZone: newL4SubsetPerZoneCalculator(svcId, maxSubsetSizeDefault, clusterEndpointsServiceLogger),
		logger:         clusterEndpointsServiceLogger,
		mode:           types.L4ClusterMode,
	}
}

// Mode indicates the mode that the EndpointsCalculator is operating in.
func (lc *L4ILBEndpointsCalculator) Mode() types.EndpointsCalculatorMode {
	return lc.mode
}

// CalculateEndpoints determines the endpoints in the NEGs based on the current service endpoints and the current NEGs.
func (lc *L4ILBEndpointsCalculator) CalculateEndpoints(eds []types.EndpointsData, currentMap map[string]types.NetworkEndpointSet) (map[string]types.NetworkEndpointSet, types.EndpointPodMap, error) {
	zoneNodeMap := lc.zoneNodes.getZoneToNodes(eds)
	if len(zoneNodeMap) == 0 {
		return nil, nil, nil
	}

	lc.logger.Info("Got zoneNodeMap as input for service", "zoneNodeMap", nodeMapToString(zoneNodeMap))
	subsetMap := lc.subsetsPerZone.calculateSubsetsPerZone(zoneNodeMap, currentMap)
	return subsetMap, nil, nil
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
