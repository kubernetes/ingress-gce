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

	"k8s.io/apimachinery/pkg/util/sets"
	listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/neg/metrics"
	"k8s.io/ingress-gce/pkg/neg/metrics/metricscollector"
	"k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/ingress-gce/pkg/network"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/zonegetter"
	"k8s.io/klog/v2"
)

// LocalL4EndpointsCalculator implements the NetworkEndpointsCalculator interface.
// It exposes methods to calculate Network endpoints for GCE_VM_IP NEGs when the service
// uses "ExternalTrafficPolicy: Local" mode.
// In this mode, the endpoints of the NEG are calculated by listing the nodes that host the service endpoints(pods)
// for the given service. These candidate nodes picked as is, if the count is less than the subset size limit(250).
// Otherwise, a subset of nodes is selected.
// In a cluster with nodes node1... node 50. If nodes node10 to node 45 run the pods for a given ILB service, all these
// nodes - node10, node 11 ... node45 will be part of the subset.
type LocalL4EndpointsCalculator struct {
	nodeLister      listers.NodeLister
	zoneGetter      *zonegetter.ZoneGetter
	subsetSizeLimit int
	svcId           string
	logger          klog.Logger
	networkInfo     *network.NetworkInfo
}

func NewLocalL4EndpointsCalculator(nodeLister listers.NodeLister, zoneGetter *zonegetter.ZoneGetter, svcId string, logger klog.Logger, networkInfo *network.NetworkInfo, lbType types.L4LBType) *LocalL4EndpointsCalculator {
	subsetSize := maxSubsetSizeLocal
	if lbType == types.L4ExternalLB {
		subsetSize = maxSubsetSizeNetLBLocal
	}

	return &LocalL4EndpointsCalculator{
		nodeLister:      nodeLister,
		zoneGetter:      zoneGetter,
		subsetSizeLimit: subsetSize,
		svcId:           svcId,
		logger:          logger.WithName("LocalL4EndpointsCalculator"),
		networkInfo:     networkInfo,
	}
}

// Mode indicates the mode that the EndpointsCalculator is operating in.
func (l *LocalL4EndpointsCalculator) Mode() types.EndpointsCalculatorMode {
	return types.L4LocalMode
}

// CalculateEndpoints determines the endpoints in the NEGs based on the current service endpoints and the current NEGs.
func (l *LocalL4EndpointsCalculator) CalculateEndpoints(eds []types.EndpointsData, currentMap map[types.NEGLocation]types.NetworkEndpointSet) (map[types.NEGLocation]types.NetworkEndpointSet, types.EndpointPodMap, int, error) {
	// List all nodes where the service endpoints are running. Get a subset of the desired count.
	zoneNodeMap := make(map[string][]*nodeWithSubnet)
	processedNodes := sets.String{}
	numEndpoints := 0
	networkInfoSubnetName, err := utils.KeyName(l.networkInfo.SubnetworkURL)
	if err != nil {
		return nil, nil, 0, err
	}
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
				metrics.PublishNegControllerErrorCountMetrics(err, true)
				continue
			}
			if ok := l.zoneGetter.IsNodeSelectedByFilter(node, zonegetter.CandidateAndUnreadyNodesFilter, l.logger); !ok {
				l.logger.Info("Dropping Node from subset since it is not a valid LB candidate", "nodeName", node.Name)
				continue
			}
			if !l.networkInfo.IsNodeConnected(node) {
				l.logger.Info("Node not connected to service network", "nodeName", node.Name, "network", l.networkInfo.K8sNetwork)
				continue
			}
			zone, subnet, err := l.zoneGetter.ZoneAndSubnetForNode(node.Name, l.logger)
			if err != nil || zone == zonegetter.EmptyZone {
				l.logger.Error(err, "Unable to find zone for node, skipping", "nodeName", node.Name)
				metrics.PublishNegControllerErrorCountMetrics(err, true)
				continue
			}
			// For MN services, use the MN subnet as the subnet for the NEG.
			if !l.networkInfo.IsDefault {
				subnet = networkInfoSubnetName
			}
			zoneNodeMap[zone] = append(zoneNodeMap[zone], newNodeWithSubnet(node, subnet))
		}
	}
	if numEndpoints == 0 {
		// Not having backends will cause clients to see connection timeout instead of an "ICMP ConnectionRefused".
		return nil, nil, 0, nil
	}
	// Compute the networkEndpoints, with total endpoints count <= l.subsetSizeLimit
	l.logger.V(2).Info("Got zoneNodeMap as input for service", "zoneNodeMap", nodeMapToString(zoneNodeMap), "serviceID", l.svcId)
	subsetMap, err := getSubsetPerZone(zoneNodeMap, l.subsetSizeLimit, l.svcId, currentMap, l.logger, l.networkInfo)

	return subsetMap, nil, 0, err
}

func (l *LocalL4EndpointsCalculator) CalculateEndpointsDegradedMode(eds []types.EndpointsData, currentMap map[types.NEGLocation]types.NetworkEndpointSet) (map[types.NEGLocation]types.NetworkEndpointSet, types.EndpointPodMap, error) {
	// this should be the same as CalculateEndpoints for L4 ec
	subsetMap, podMap, _, err := l.CalculateEndpoints(eds, currentMap)
	return subsetMap, podMap, err
}

func (l *LocalL4EndpointsCalculator) ValidateEndpoints(endpointData []types.EndpointsData, endpointPodMap types.EndpointPodMap, endpointsExcludedInCalculation int) error {
	// this should be a no-op for now
	return nil
}

// ClusterL4EndpointsCalculator implements the NetworkEndpointsCalculator interface.
// It exposes methods to calculate Network endpoints for GCE_VM_IP NEGs when the service
// uses "ExternalTrafficPolicy: Cluster" mode This is the default mode.
// In this mode, the endpoints of the NEG are calculated by selecting nodes at random. Up to 25(subset size limit in this
// mode) are selected.
type ClusterL4EndpointsCalculator struct {
	// zoneGetter looks up the zone for a given node when calculating subsets.
	zoneGetter *zonegetter.ZoneGetter
	// subsetSizeLimit is the max value of the subset size in this mode.
	subsetSizeLimit int
	// svcId is the unique identifier for the service, that is used as a salt when hashing nodenames.
	svcId       string
	networkInfo *network.NetworkInfo
	// lbType denotes the type of underlying LoadBalancer. Either EXTERNAL or INTERNAL.
	lbType types.L4LBType

	logger klog.Logger
}

func NewClusterL4EndpointsCalculator(nodeLister listers.NodeLister, zoneGetter *zonegetter.ZoneGetter, svcId string, logger klog.Logger, networkInfo *network.NetworkInfo, l4LBtype types.L4LBType) *ClusterL4EndpointsCalculator {
	subsetSize := maxSubsetSizeDefault
	if l4LBtype == types.L4ExternalLB {
		subsetSize = maxSubsetSizeNetLBCluster
	}
	return &ClusterL4EndpointsCalculator{
		zoneGetter:      zoneGetter,
		subsetSizeLimit: subsetSize,
		svcId:           svcId,
		lbType:          l4LBtype,
		logger:          logger.WithName("ClusterL4EndpointsCalculator"),
		networkInfo:     networkInfo,
	}
}

// Mode indicates the mode that the EndpointsCalculator is operating in.
func (l *ClusterL4EndpointsCalculator) Mode() types.EndpointsCalculatorMode {
	return types.L4ClusterMode
}

// CalculateEndpoints determines the endpoints in the NEGs based on the current service endpoints and the current NEGs.
func (l *ClusterL4EndpointsCalculator) CalculateEndpoints(eds []types.EndpointsData, currentMap map[types.NEGLocation]types.NetworkEndpointSet) (map[types.NEGLocation]types.NetworkEndpointSet, types.EndpointPodMap, int, error) {
	// In this mode, any of the cluster nodes can be part of the subset, whether or not a matching pod runs on it.
	nodes, _ := l.zoneGetter.ListNodes(zonegetter.CandidateAndUnreadyNodesFilter, l.logger)
	zoneNodeMap := make(map[string][]*nodeWithSubnet)
	zoneSubnetPairs := make(map[string]any)
	networkInfoSubnetName, err := utils.KeyName(l.networkInfo.SubnetworkURL)
	if err != nil {
		return nil, nil, 0, err
	}
	for _, node := range nodes {
		if !l.networkInfo.IsNodeConnected(node) {
			l.logger.Info("Node not connected to service network", "nodeName", node.Name, "network", l.networkInfo.K8sNetwork)
			continue
		}
		zone, subnet, err := l.zoneGetter.ZoneAndSubnetForNode(node.Name, l.logger)
		if err != nil || zone == zonegetter.EmptyZone {
			l.logger.Error(err, "Unable to find zone for node skipping", "nodeName", node.Name)
			metrics.PublishNegControllerErrorCountMetrics(err, true)
			continue
		}
		// For MN services, use the MN subnet as the subnet for the NEG.
		if !l.networkInfo.IsDefault {
			subnet = networkInfoSubnetName
		}
		zoneNodeMap[zone] = append(zoneNodeMap[zone], newNodeWithSubnet(node, subnet))
		zoneSubnetPairs[zone+":"+subnet] = struct{}{}
	}
	l.logger.V(2).Info("Got zoneNodeMap as input for service", "zoneNodeMap", nodeMapToString(zoneNodeMap), "serviceID", l.svcId)

	wanted := l.wantedInternalEndpointsCount(len(zoneNodeMap))
	if l.lbType == types.L4ExternalLB {
		wanted = l.wantedExternalEndpointsCount(eds, currentMap, len(zoneSubnetPairs))
	}

	subsetMap, err := getSubsetPerZone(zoneNodeMap, wanted, l.svcId, currentMap, l.logger, l.networkInfo)
	return subsetMap, nil, 0, err
}

// wantedExternalEndpointsCount will determine the amount of endpoints that:
// * scales linearly based on the number of pods
// * won't over provision endpoints over the nodes or pods count
// * won't delete already existing endpoints, as that requires connection draining
// * will provide at least 3 endpoints per zone/subnet pair
// * takes into account limits for passthrough NetLB and ILB
func (l *ClusterL4EndpointsCalculator) wantedExternalEndpointsCount(eds []types.EndpointsData, currentMap map[types.NEGLocation]types.NetworkEndpointSet, zoneSubnetPairCount int) int {
	// Compute the networkEndpoints, with total endpoints <= l.subsetSizeLimit.
	optimal := linearEndpointsPerPods(zoneSubnetPairCount, edsLen(eds), l.subsetSizeLimit)

	// Decreasing the number of Endpoints requires connection draining, so we want to avoid that.
	used := negsLen(currentMap)
	wanted := max(optimal, used)

	l.logger.V(2).Info("Calculated wanted endpoints", "optimal", optimal, "used", used, "wanted", wanted)

	return wanted
}

// wantedInternalEndpointsCount will determine the amount of endpoints that is divisible by the number of zones
// This should lead to improvements in the amount of connection draining that is done,
func (l *ClusterL4EndpointsCalculator) wantedInternalEndpointsCount(zonesCount int) int {
	// | zones | endpoints per zone | total endpoints |
	// | ----- | ------------------ | --------------- |
	// |     1 |                 24 |              24 |
	// |     2 |                 12 |              24 |
	// |     3 |                  8 |              24 |
	// |     4 |                  6 |              24 |
	// |     5 |                  5 |              25 |
	// |     6 |                  4 |              24 |
	// | ----- | ------------------ | --------------- |
	// Based on https://cloud.google.com/compute/docs/regions-zones#available
	// There are no regions with more than 4 zones currently.
	if zonesCount != 0 && 24%zonesCount == 0 { // If there is some issue with zonesCount we don't want to divide by zero
		return 24
	}
	return 25
}

func linearEndpointsPerPods(zoneSubnetPairCount, endpointsCount, subsetSizeLimit int) int {
	const minCountPerZoneSubnetPair = 3
	lowerZonalBasedBound := zoneSubnetPairCount * minCountPerZoneSubnetPair
	upperLimit := subsetSizeLimit

	return min(
		upperLimit,
		max(
			endpointsCount,
			lowerZonalBasedBound,
		),
	)
}

func negsLen(m map[types.NEGLocation]types.NetworkEndpointSet) int {
	total := 0
	for _, v := range m {
		total += v.Len()
	}
	return total
}

func edsLen(eds []types.EndpointsData) int {
	total := 0
	for _, ed := range eds {
		for _, addr := range ed.Addresses {
			if addr.NodeName == nil || addr.TargetRef == nil {
				continue
			}
			total++
		}
	}
	return total
}

func (l *ClusterL4EndpointsCalculator) CalculateEndpointsDegradedMode(eps []types.EndpointsData, currentMap map[types.NEGLocation]types.NetworkEndpointSet) (map[types.NEGLocation]types.NetworkEndpointSet, types.EndpointPodMap, error) {
	// this should be the same as CalculateEndpoints for L4 ec
	subsetMap, podMap, _, err := l.CalculateEndpoints(eps, currentMap)
	return subsetMap, podMap, err
}

func (l *ClusterL4EndpointsCalculator) ValidateEndpoints(endpointData []types.EndpointsData, endpointPodMap types.EndpointPodMap, endpointsExcludedInCalculation int) error {
	// this should be a no-op for now
	return nil
}

// L7EndpointsCalculator implements methods to calculate Network endpoints for VM_IP_PORT NEGs
type L7EndpointsCalculator struct {
	zoneGetter               *zonegetter.ZoneGetter
	servicePortName          string
	podLister                cache.Indexer
	nodeLister               cache.Indexer
	serviceLister            cache.Indexer
	syncerKey                types.NegSyncerKey
	networkEndpointType      types.NetworkEndpointType
	enableDualStackNEG       bool
	enableMultiSubnetCluster bool
	logger                   klog.Logger
	syncMetricsCollector     *metricscollector.SyncerMetrics
}

func NewL7EndpointsCalculator(zoneGetter *zonegetter.ZoneGetter, podLister, nodeLister, serviceLister cache.Indexer, syncerKey types.NegSyncerKey, logger klog.Logger, enableDualStackNEG bool, syncMetricsCollector *metricscollector.SyncerMetrics) *L7EndpointsCalculator {
	return &L7EndpointsCalculator{
		zoneGetter:               zoneGetter,
		servicePortName:          syncerKey.PortTuple.Name,
		podLister:                podLister,
		nodeLister:               nodeLister,
		serviceLister:            serviceLister,
		syncerKey:                syncerKey,
		networkEndpointType:      syncerKey.NegType,
		enableDualStackNEG:       enableDualStackNEG,
		enableMultiSubnetCluster: flags.F.EnableMultiSubnetCluster,
		logger:                   logger.WithName("L7EndpointsCalculator"),
		syncMetricsCollector:     syncMetricsCollector,
	}
}

// Mode indicates the mode that the EndpointsCalculator is operating in.
func (l *L7EndpointsCalculator) Mode() types.EndpointsCalculatorMode {
	return types.L7Mode
}

// CalculateEndpoints determines the endpoints in the NEGs based on the current service endpoints and the current NEGs.
func (l *L7EndpointsCalculator) CalculateEndpoints(eds []types.EndpointsData, _ map[types.NEGLocation]types.NetworkEndpointSet) (map[types.NEGLocation]types.NetworkEndpointSet, types.EndpointPodMap, int, error) {
	result, err := toZoneNetworkEndpointMap(eds, l.zoneGetter, l.podLister, l.servicePortName, l.networkEndpointType, l.enableDualStackNEG, l.enableMultiSubnetCluster, l.logger)
	if err == nil { // If current calculation ends up in error, we trigger and emit metrics in degraded mode.
		l.syncMetricsCollector.UpdateSyncerEPMetrics(l.syncerKey, result.EPCount, result.EPSCount)
	}
	return result.NetworkEndpointSet, result.EndpointPodMap, result.EPCount[types.Duplicate] + result.EPCount[types.NodeInNonDefaultSubnet], err
}

// CalculateEndpoints determines the endpoints in the NEGs based on the current service endpoints and the current NEGs.
func (l *L7EndpointsCalculator) CalculateEndpointsDegradedMode(eds []types.EndpointsData, _ map[types.NEGLocation]types.NetworkEndpointSet) (map[types.NEGLocation]types.NetworkEndpointSet, types.EndpointPodMap, error) {
	result := toZoneNetworkEndpointMapDegradedMode(eds, l.zoneGetter, l.podLister, l.nodeLister, l.serviceLister, l.servicePortName, l.networkEndpointType, l.enableDualStackNEG, l.enableMultiSubnetCluster, l.logger)
	l.syncMetricsCollector.UpdateSyncerEPMetrics(l.syncerKey, result.EPCount, result.EPSCount)
	return result.NetworkEndpointSet, result.EndpointPodMap, nil
}

func nodeMapToString(nodeMap map[string][]*nodeWithSubnet) string {
	var str []string
	for zone, nodeList := range nodeMap {
		str = append(str, fmt.Sprintf("Zone %s: %d nodes", zone, len(nodeList)))
	}
	return strings.Join(str, ",")
}

// ValidateEndpoints checks if endpoint information is correct.
//
//	For L7 Endpoint Calculator, it returns error if one of the two checks fails:
//	1. The endpoint count from endpointData doesn't equal to the one from endpointPodMap:
//	   endpiontPodMap removes the duplicated endpoints, and endpointsExcludedInCalculation stores the number of duplicated it removed
//	   and we compare the endpoint counts with duplicates
//	2. The endpoint count from endpointData or the one from endpointPodMap is 0
func (l *L7EndpointsCalculator) ValidateEndpoints(endpointData []types.EndpointsData, endpointPodMap types.EndpointPodMap, endpointsExcludedInCalculation int) error {
	// Endpoint count from EndpointPodMap
	countFromPodMap := len(endpointPodMap) + endpointsExcludedInCalculation
	if countFromPodMap == 0 {
		l.logger.Info("Detected endpoint count from endpointPodMap going to zero", "endpointPodMap", fmt.Sprintf("%+v", endpointPodMap))
		return fmt.Errorf("%w: Detect endpoint count goes to zero", types.ErrEPCalculationCountZero)
	}
	// Endpoint count from EndpointData
	countFromEndpointData := 0
	for _, ed := range endpointData {
		countFromEndpointData += len(ed.Addresses)
	}
	if countFromEndpointData == 0 {
		l.logger.Info("Detected endpoint count from endpointData going to zero", "endpointData", endpointData)
		return fmt.Errorf("%w: Detect endpoint count goes to zero", types.ErrEPSEndpointCountZero)
	}

	if countFromEndpointData != countFromPodMap {
		l.logger.Info("Detected error when comparing endpoint counts",
			"countFromEndpointData", countFromEndpointData,
			"countFromPodMap", countFromPodMap,
			"endpointData", endpointData,
			"endpointPodMap", fmt.Sprintf("%+v", endpointPodMap),
			"endpointsExcludedInCalculation", endpointsExcludedInCalculation,
		)
		return fmt.Errorf("%w: Detect endpoint mismatch, count from endpoint slice=%d, count after calculation=%d", types.ErrEPCountsDiffer, countFromEndpointData, countFromPodMap)
	}
	return nil
}
