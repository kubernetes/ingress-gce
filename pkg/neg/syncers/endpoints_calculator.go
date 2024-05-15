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
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/neg/metrics"
	"k8s.io/ingress-gce/pkg/neg/metrics/metricscollector"
	"k8s.io/ingress-gce/pkg/neg/types"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/ingress-gce/pkg/network"
	"k8s.io/ingress-gce/pkg/utils/zonegetter"
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
	zoneGetter      *zonegetter.ZoneGetter
	subsetSizeLimit int
	svcId           string
	logger          klog.Logger
	networkInfo     *network.NetworkInfo
}

func NewLocalL4ILBEndpointsCalculator(nodeLister listers.NodeLister, zoneGetter *zonegetter.ZoneGetter, svcId string, logger klog.Logger, networkInfo *network.NetworkInfo) *LocalL4ILBEndpointsCalculator {
	return &LocalL4ILBEndpointsCalculator{
		nodeLister:      nodeLister,
		zoneGetter:      zoneGetter,
		subsetSizeLimit: maxSubsetSizeLocal,
		svcId:           svcId,
		logger:          logger.WithName("LocalL4ILBEndpointsCalculator"),
		networkInfo:     networkInfo,
	}
}

// Mode indicates the mode that the EndpointsCalculator is operating in.
func (l *LocalL4ILBEndpointsCalculator) Mode() types.EndpointsCalculatorMode {
	return types.L4LocalMode
}

// CalculateEndpoints determines the endpoints in the NEGs based on the current service endpoints and the current NEGs.
func (l *LocalL4ILBEndpointsCalculator) CalculateEndpoints(eds []types.EndpointsData, currentMap map[string]types.NetworkEndpointSet) (map[string]types.NetworkEndpointSet, types.EndpointPodMap, int, error) {
	// List all nodes where the service endpoints are running. Get a subset of the desired count.
	zoneNodeMap := make(map[string][]*v1.Node)
	processedNodes := sets.String{}
	numEndpoints := 0
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
			zone, err := l.zoneGetter.ZoneForNode(node.Name, l.logger)
			if err != nil {
				l.logger.Error(err, "Unable to find zone for node, skipping", "nodeName", node.Name)
				metrics.PublishNegControllerErrorCountMetrics(err, true)
				continue
			}
			zoneNodeMap[zone] = append(zoneNodeMap[zone], node)
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

func (l *LocalL4ILBEndpointsCalculator) CalculateEndpointsDegradedMode(eds []types.EndpointsData, currentMap map[string]types.NetworkEndpointSet) (map[string]types.NetworkEndpointSet, types.EndpointPodMap, error) {
	// this should be the same as CalculateEndpoints for L4 ec
	subsetMap, podMap, _, err := l.CalculateEndpoints(eds, currentMap)
	return subsetMap, podMap, err
}

func (l *LocalL4ILBEndpointsCalculator) ValidateEndpoints(endpointData []types.EndpointsData, endpointPodMap types.EndpointPodMap, dupCount int) error {
	// this should be a no-op for now
	return nil
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
	zoneGetter *zonegetter.ZoneGetter
	// subsetSizeLimit is the max value of the subset size in this mode.
	subsetSizeLimit int
	// svcId is the unique identifier for the service, that is used as a salt when hashing nodenames.
	svcId       string
	networkInfo *network.NetworkInfo

	logger klog.Logger
}

func NewClusterL4ILBEndpointsCalculator(nodeLister listers.NodeLister, zoneGetter *zonegetter.ZoneGetter, svcId string, logger klog.Logger, networkInfo *network.NetworkInfo) *ClusterL4ILBEndpointsCalculator {
	return &ClusterL4ILBEndpointsCalculator{
		nodeLister:      nodeLister,
		zoneGetter:      zoneGetter,
		subsetSizeLimit: maxSubsetSizeDefault,
		svcId:           svcId,
		logger:          logger.WithName("ClusterL4ILBEndpointsCalculator"),
		networkInfo:     networkInfo,
	}
}

// Mode indicates the mode that the EndpointsCalculator is operating in.
func (l *ClusterL4ILBEndpointsCalculator) Mode() types.EndpointsCalculatorMode {
	return types.L4ClusterMode
}

// CalculateEndpoints determines the endpoints in the NEGs based on the current service endpoints and the current NEGs.
func (l *ClusterL4ILBEndpointsCalculator) CalculateEndpoints(_ []types.EndpointsData, currentMap map[string]types.NetworkEndpointSet) (map[string]types.NetworkEndpointSet, types.EndpointPodMap, int, error) {
	// In this mode, any of the cluster nodes can be part of the subset, whether or not a matching pod runs on it.
	nodes, _ := l.zoneGetter.ListNodes(zonegetter.CandidateAndUnreadyNodesFilter, l.logger)
	zoneNodeMap := make(map[string][]*v1.Node)
	for _, node := range nodes {
		if !l.networkInfo.IsNodeConnected(node) {
			l.logger.Info("Node not connected to service network", "nodeName", node.Name, "network", l.networkInfo.K8sNetwork)
			continue
		}
		zone, err := l.zoneGetter.ZoneForNode(node.Name, l.logger)
		if err != nil {
			l.logger.Error(err, "Unable to find zone for node skipping", "nodeName", node.Name)
			metrics.PublishNegControllerErrorCountMetrics(err, true)
			continue
		}
		zoneNodeMap[zone] = append(zoneNodeMap[zone], node)
	}
	l.logger.V(2).Info("Got zoneNodeMap as input for service", "zoneNodeMap", nodeMapToString(zoneNodeMap), "serviceID", l.svcId)
	// Compute the networkEndpoints, with total endpoints <= l.subsetSizeLimit.
	subsetMap, err := getSubsetPerZone(zoneNodeMap, l.subsetSizeLimit, l.svcId, currentMap, l.logger, l.networkInfo)
	return subsetMap, nil, 0, err
}

func (l *ClusterL4ILBEndpointsCalculator) CalculateEndpointsDegradedMode(eps []types.EndpointsData, currentMap map[string]types.NetworkEndpointSet) (map[string]types.NetworkEndpointSet, types.EndpointPodMap, error) {
	// this should be the same as CalculateEndpoints for L4 ec
	subsetMap, podMap, _, err := l.CalculateEndpoints(eps, currentMap)
	return subsetMap, podMap, err
}

func (l *ClusterL4ILBEndpointsCalculator) ValidateEndpoints(endpointData []types.EndpointsData, endpointPodMap types.EndpointPodMap, dupCount int) error {
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
func (l *L7EndpointsCalculator) CalculateEndpoints(eds []types.EndpointsData, _ map[string]types.NetworkEndpointSet) (map[string]types.NetworkEndpointSet, types.EndpointPodMap, int, error) {
	result, err := toZoneNetworkEndpointMap(eds, l.zoneGetter, l.podLister, l.servicePortName, l.networkEndpointType, l.enableDualStackNEG, l.enableMultiSubnetCluster, l.logger)
	if err == nil { // If current calculation ends up in error, we trigger and emit metrics in degraded mode.
		l.syncMetricsCollector.UpdateSyncerEPMetrics(l.syncerKey, result.EPCount, result.EPSCount)
	}
	return result.NetworkEndpointSet, result.EndpointPodMap, result.EPCount[negtypes.Duplicate], err
}

// CalculateEndpoints determines the endpoints in the NEGs based on the current service endpoints and the current NEGs.
func (l *L7EndpointsCalculator) CalculateEndpointsDegradedMode(eds []types.EndpointsData, _ map[string]types.NetworkEndpointSet) (map[string]types.NetworkEndpointSet, types.EndpointPodMap, error) {
	result := toZoneNetworkEndpointMapDegradedMode(eds, l.zoneGetter, l.podLister, l.nodeLister, l.serviceLister, l.servicePortName, l.networkEndpointType, l.enableDualStackNEG, l.enableMultiSubnetCluster, l.logger)
	l.syncMetricsCollector.UpdateSyncerEPMetrics(l.syncerKey, result.EPCount, result.EPSCount)
	return result.NetworkEndpointSet, result.EndpointPodMap, nil
}

func nodeMapToString(nodeMap map[string][]*v1.Node) string {
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
//	   endpiontPodMap removes the duplicated endpoints, and dupCount stores the number of duplicated it removed
//	   and we compare the endpoint counts with duplicates
//	2. The endpoint count from endpointData or the one from endpointPodMap is 0
func (l *L7EndpointsCalculator) ValidateEndpoints(endpointData []types.EndpointsData, endpointPodMap types.EndpointPodMap, dupCount int) error {
	// Endpoint count from EndpointPodMap
	countFromPodMap := len(endpointPodMap) + dupCount
	if countFromPodMap == 0 {
		l.logger.Info("Detected endpoint count from endpointPodMap going to zero", "endpointPodMap", endpointPodMap)
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
		l.logger.Info("Detected error when comparing endpoint counts", "countFromEndpointData", countFromEndpointData, "countFromPodMap", countFromPodMap, "endpointData", endpointData, "endpointPodMap", endpointPodMap, "dupCount", dupCount)
		return fmt.Errorf("%w: Detect endpoint mismatch, count from endpoint slice=%d, count after calculation=%d", types.ErrEPCountsDiffer, countFromEndpointData, countFromPodMap)
	}
	return nil
}
