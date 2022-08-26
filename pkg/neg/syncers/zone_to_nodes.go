package syncers

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog/v2"
)

type l4ILBLocalZoneToNodesGetter struct {
	nodeLister listers.NodeLister
	zoneGetter types.ZoneGetter
	logger     klog.Logger
}

func newL4ILBLocalZoneToNodesGetter(nodeLister listers.NodeLister, zoneGetter types.ZoneGetter, logger klog.Logger) *l4ILBLocalZoneToNodesGetter {
	return &l4ILBLocalZoneToNodesGetter{
		nodeLister: nodeLister,
		zoneGetter: zoneGetter,
		logger:     logger,
	}
}

func (lzng *l4ILBLocalZoneToNodesGetter) getZoneToNodes(eds []types.EndpointsData) map[string][]*v1.Node {
	zoneNodeMap := make(map[string][]*v1.Node)
	processedNodes := sets.String{}
	candidateNodeCheck := utils.CandidateNodesPredicateIncludeUnreadyExcludeUpgradingNodes
	for _, ed := range eds {
		for _, addr := range ed.Addresses {
			if addr.NodeName == nil {
				lzng.logger.V(2).Info("Address inside Endpoints does not have an associated node. Skipping", "address", addr.Addresses, "endpoints", klog.KRef(ed.Meta.Namespace, ed.Meta.Name))
				continue
			}
			if addr.TargetRef == nil {
				lzng.logger.V(2).Info("Address inside Endpoints does not have an associated pod. Skipping", "address", addr.Addresses, "endpoints", klog.KRef(ed.Meta.Namespace, ed.Meta.Name))
				continue
			}
			if processedNodes.Has(*addr.NodeName) {
				continue
			}
			processedNodes.Insert(*addr.NodeName)
			node, err := lzng.nodeLister.Get(*addr.NodeName)
			if err != nil {
				lzng.logger.Error(err, "failed to retrieve node object", "nodeName", *addr.NodeName)
				continue
			}
			if ok := candidateNodeCheck(node); !ok {
				lzng.logger.Info("Dropping Node from subset since it is not a valid LB candidate", "nodeName", node.Name)
				continue
			}
			zone, err := lzng.zoneGetter.GetZoneForNode(node.Name)
			if err != nil {
				lzng.logger.Error(err, "Unable to find zone for node, skipping", "nodeName", node.Name)
				continue
			}
			zoneNodeMap[zone] = append(zoneNodeMap[zone], node)
		}
	}
	return zoneNodeMap
}

type l4ILBClusterZoneToNodesGetter struct {
	nodeLister listers.NodeLister
	zoneGetter types.ZoneGetter
	logger     klog.Logger
}

func newL4ILBClusterZoneToNodesGetter(nodeLister listers.NodeLister, zoneGetter types.ZoneGetter, logger klog.Logger) *l4ILBClusterZoneToNodesGetter {
	return &l4ILBClusterZoneToNodesGetter{
		nodeLister: nodeLister,
		zoneGetter: zoneGetter,
		logger:     logger,
	}
}

func (lzng *l4ILBClusterZoneToNodesGetter) getZoneToNodes(_ []types.EndpointsData) map[string][]*v1.Node {
	// In this mode, any of the cluster nodes can be part of the subset, whether or not a matching pod runs on it.
	nodes, _ := utils.ListWithPredicate(lzng.nodeLister, utils.CandidateNodesPredicateIncludeUnreadyExcludeUpgradingNodes)

	zoneNodeMap := make(map[string][]*v1.Node)
	for _, node := range nodes {
		zone, err := lzng.zoneGetter.GetZoneForNode(node.Name)
		if err != nil {
			lzng.logger.Error(err, "Unable to find zone for node skipping", "nodeName", node.Name)
			continue
		}
		zoneNodeMap[zone] = append(zoneNodeMap[zone], node)
	}
	return zoneNodeMap
}
