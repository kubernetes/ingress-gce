package instancegroups

import (
	listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog/v2"
)

// IGNodeGetter is used to retrieve nodes for the shared instance group.
type IGNodeGetter struct {
	// lister is a cache of the k8s Node resources.
	lister cache.Indexer

	enableMultiSubnetCluster bool

	cloud cloudSubnetworkProvider
}

// cloudSubnetworkProvider is the interface of structs that provide subnetwork URL. This is intended to be gce.Cloud
type cloudSubnetworkProvider interface {
	SubnetworkURL() string
}

// NewNodeGetter creates a new node getter for IGs.
func NewNodeGetter(lister cache.Indexer, enableMultiSubnetCluster bool, cloud cloudSubnetworkProvider) *IGNodeGetter {
	return &IGNodeGetter{
		lister:                   lister,
		enableMultiSubnetCluster: enableMultiSubnetCluster,
		cloud:                    cloud,
	}
}

// GetReadyNodesForSharedInstanceGroup returns the node names that should be present in the shared instance group.
func (ng *IGNodeGetter) GetReadyNodesForSharedInstanceGroup(logger klog.Logger) ([]string, error) {
	if ng.enableMultiSubnetCluster {
		return utils.GetReadyNodeNamesInDefaultSubnet(listers.NewNodeLister(ng.lister), ng.cloud.SubnetworkURL(), logger)
	}
	return utils.GetReadyNodeNames(listers.NewNodeLister(ng.lister), logger)
}
