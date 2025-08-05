/*
Copyright 2023 The Kubernetes Authors.

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

package zonegetter

import (
	"errors"
	"fmt"
	"regexp"

	api_v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/sets"

	nodetopologyv1 "github.com/GoogleCloudPlatform/gke-networking-api/apis/nodetopology/v1"
	listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/nodetopology"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog/v2"
)

type Mode int
type Filter string

const (
	// In GCP mode, the ZoneGetter will fetch the zone information from
	// k8s Node objects.
	GCP Mode = iota
	// In NonGCP mode, the ZoneGetter will only return the zone specified
	// in gce.conf directly.
	NonGCP
	// Legacy mode indicates that the cluster is attached to a legacy GCE
	// network. This is detected when the provided subnetURL is empty.
	// ZoneGetter will not do any subnet checks and assumes that all nodes
	// are relevant.
	Legacy
)

const (
	AllNodesFilter                 = Filter("AllNodesFilter")
	CandidateNodesFilter           = Filter("CandidateNodesFilter")
	CandidateAndUnreadyNodesFilter = Filter("CandidateAndUnreadyNodesFilter")
	EmptyZone                      = ""
	EmptySubnet                    = ""
)

var ErrProviderIDNotFound = errors.New("providerID not found")
var ErrSplitProviderID = errors.New("error splitting providerID")
var ErrNodeNotFound = errors.New("node not found")
var ErrNodeNotInDefaultSubnet = errors.New("Node not in default subnet")
var ErrNodePodCIDRNotSet = errors.New("Node does not have PodCIDR set")

// providerIDRE is the regex to process providerID.
// A providerID is build out of '${ProviderName}://${project-id}/${zone}/${instance-name}'
var providerIDRE = regexp.MustCompile(`^` + "gce" + `://([^/]+)/([^/]*)/([^/]+)$`)

// ZoneGetter manages lookups for GCE instances to zones.
type ZoneGetter struct {
	nodeLister           cache.Indexer
	nodeTopologyInformer cache.SharedIndexInformer
	// Mode indicates if the ZoneGetter is in GCP or Non-GCP mode
	// GCP mode ZoneGetter fetches zones from k8s node resource objects.
	// Non-GCP mode ZoneGetter always return its one single stored zone
	mode Mode
	// singleStoredZone is the single stored zone in the zoneGetter
	// It is only used in Non-GCP mode.
	singleStoredZone string

	// Whether zoneGetter should only list default subnet nodes.
	// onlyIncludeDefaultSubnetNodes is a static value that only depends on the phase/flags.
	onlyIncludeDefaultSubnetNodes bool

	// The subnetURL of the cluster's default subnet.
	defaultSubnetURL string

	// The default subnet config
	defaultSubnetConfig nodetopologyv1.SubnetConfig

	// nodeTopologyHasSynced is a function that is evaluated every time to
	// check if we can trust nodeTopology Informer.
	nodeTopologyHasSynced func() bool
}

// ZoneAndSubnetForNode returns the zone and subnet for a given node by looking up providerID.
func (z *ZoneGetter) ZoneAndSubnetForNode(name string, logger klog.Logger) (string, string, error) {
	// Return the single stored zone if the zoneGetter is in non-gcp mode.
	// In non-gcp mode, the subnet will be empty, so it matches the behavior
	// for non-gcp NEGs since their SubnetworkURL is empty.
	if z.mode == NonGCP {
		logger.Info("ZoneGetter in non-gcp mode, return the single stored zone", "zone", z.singleStoredZone)
		return z.singleStoredZone, "", nil
	}

	nodeLogger := logger.WithValues("nodeName", name)
	node, err := listers.NewNodeLister(z.nodeLister).Get(name)
	if err != nil {
		nodeLogger.Error(err, "Failed to get node")
		return "", "", fmt.Errorf("%w: failed to get node %s", ErrNodeNotFound, name)
	}
	if node.Spec.ProviderID == "" {
		nodeLogger.Error(ErrProviderIDNotFound, "Node does not have providerID")
		return "", "", ErrProviderIDNotFound
	}
	zone, err := getZone(node)
	if err != nil {
		nodeLogger.Error(err, "Failed to get zone from the providerID")
		return "", "", err
	}
	if zone == EmptyZone {
		return EmptyZone, "", nil
	}

	if z.mode == Legacy {
		return zone, EmptySubnet, nil
	}

	subnet, err := getSubnet(node, z.defaultSubnetURL)
	if err != nil {
		nodeLogger.Error(err, "Failed to get subnet from node's LabelNodeSubnet")
		return "", "", err
	}

	nodeTopologySynced := z.nodeTopologyHasSynced()
	if z.onlyIncludeDefaultSubnetNodes || !nodeTopologySynced {
		logger.Info("Falling back to only using default subnet when getting subnet for node", "z.onlyIncludeDefaultSubnetNodes", z.onlyIncludeDefaultSubnetNodes, "nodeTopologySynced", nodeTopologySynced)

		defaultSubnet, err := utils.KeyName(z.defaultSubnetURL)
		if err != nil {
			nodeLogger.Error(err, "Failed to extract default subnet information from URL", "defaultSubnetURL", z.defaultSubnetURL)
			return "", "", err
		}
		if subnet != defaultSubnet {
			return "", "", ErrNodeNotInDefaultSubnet
		}
	}
	return zone, subnet, nil
}

// ListNodes returns a list of nodes that satisfy the given node filtering mode.
func (z *ZoneGetter) ListNodes(filter Filter, logger klog.Logger) ([]*api_v1.Node, error) {
	filterLogger := logger.WithValues("filter", filter)
	filterLogger.Info("Listing nodes")

	nodes, err := listers.NewNodeLister(z.nodeLister).List(labels.Everything())
	if err != nil {
		filterLogger.Error(err, "Failed to list all nodes")
		return nil, err
	}

	var selected []*api_v1.Node
	var filteredOut []string
	for _, node := range nodes {
		// Filter nodes with predicate and exclude nodes without providerID
		if z.IsNodeSelectedByFilter(node, filter, filterLogger) {
			selected = append(selected, node)
		} else {
			filteredOut = append(filteredOut, node.Name)
		}
	}
	if len(filteredOut) <= 50 {
		filterLogger.Info("Filtered out nodes when listing node zones", "nodes", filteredOut)
	}

	return selected, nil
}

// ListZones returns a list of zones containing nodes that satisfy the given
// node filtering mode.
func (z *ZoneGetter) ListZones(filter Filter, logger klog.Logger) ([]string, error) {
	if z.mode == NonGCP {
		logger.Info("ZoneGetter in non-gcp mode, return the single stored zone", "zone", z.singleStoredZone)
		return []string{z.singleStoredZone}, nil
	}

	filterLogger := logger.WithValues("filter", filter)
	filterLogger.Info("Listing zones")
	nodes, err := z.ListNodes(filter, logger)
	if err != nil {
		filterLogger.Error(err, "Failed to list nodes")
		return []string{}, err
	}
	zones := sets.String{}
	for _, n := range nodes {
		zone, err := getZone(n)
		if err != nil || zone == EmptyZone {
			filterLogger.Error(err, "Failed to get zone from providerID", "nodeName", n.Name)
			continue
		}
		zones.Insert(zone)
	}
	return zones.List(), nil
}

// ListSubnets returns the lists of subnets in the cluster based on the
// NodeTopology CR.
// If the CR does not exist or it is not ready, ListSubnets will return only the
// default subnet.
func (z *ZoneGetter) ListSubnets(logger klog.Logger) []nodetopologyv1.SubnetConfig {
	if z.mode == Legacy {
		logger.Info("ListSubnets is being called with legacy zone getter. Ignoring error")
		return nil
	}

	nodeTopologyCRName := flags.F.NodeTopologyCRName
	nodeTopologySynced := z.nodeTopologyHasSynced()
	if z.onlyIncludeDefaultSubnetNodes || !nodeTopologySynced {
		logger.Info("Falling back to only using default subnet when listing subnets", "z.onlyIncludeDefaultSubnetNodes", z.onlyIncludeDefaultSubnetNodes, "nodeTopologySynced", nodeTopologySynced)
		return []nodetopologyv1.SubnetConfig{z.defaultSubnetConfig}
	}

	n, exists, err := z.nodeTopologyInformer.GetIndexer().GetByKey(nodeTopologyCRName)
	if err != nil {
		logger.Error(err, "Failed trying to get node topology CR in the store", "nodeTopologyCRName", nodeTopologyCRName)
		return []nodetopologyv1.SubnetConfig{z.defaultSubnetConfig}
	}
	if !exists {
		logger.Info("Unable to find node topology CR in the store", "nodeTopologyCRName", nodeTopologyCRName)
		return []nodetopologyv1.SubnetConfig{z.defaultSubnetConfig}
	}
	nodeTopologyCR, ok := n.(*nodetopologyv1.NodeTopology)
	if !ok {
		logger.Error(err, "failed to cast topology CR to node topology type", "nodeTopologyCRName", nodeTopologyCRName)
		return []nodetopologyv1.SubnetConfig{z.defaultSubnetConfig}
	}
	return nodeTopologyCR.Status.Subnets
}

// IsNodeSelectedByFilter checks if the node matches the node filter mode.
func (z *ZoneGetter) IsNodeSelectedByFilter(node *api_v1.Node, filter Filter, filterLogger klog.Logger) bool {
	nodeAndFilterLogger := filterLogger.WithValues("nodeName", node.Name)
	switch filter {
	case AllNodesFilter:
		return z.allNodesPredicate(node, nodeAndFilterLogger)
	case CandidateNodesFilter:
		return z.candidateNodesPredicate(node, nodeAndFilterLogger)
	case CandidateAndUnreadyNodesFilter:
		return z.candidateNodesPredicateIncludeUnreadyExcludeUpgradingNodes(node, nodeAndFilterLogger)
	default:
		return false
	}
}

// allNodesPredicate selects all nodes.
func (z *ZoneGetter) allNodesPredicate(node *api_v1.Node, nodeLogger klog.Logger) bool {
	// For legacy networks, no subnets exists, so always return true for every node.
	if z.mode == Legacy {
		return true
	}

	nodeTopologySynced := z.nodeTopologyHasSynced()

	if z.onlyIncludeDefaultSubnetNodes || !nodeTopologySynced {
		nodeLogger.Info("Falling back to only using default subnet when listing all nodes", "z.onlyIncludeDefaultSubnetNodes", z.onlyIncludeDefaultSubnetNodes, "nodeTopologySynced", nodeTopologySynced)

		isInDefaultSubnet, err := isNodeInDefaultSubnet(node, z.defaultSubnetURL, nodeLogger)
		if err != nil {
			nodeLogger.Error(err, "Failed to verify if the node is in default subnet")
			return false
		}
		if !isInDefaultSubnet {
			nodeLogger.Error(ErrNodeNotInDefaultSubnet, "Ignoring node since it is not in the default subnet")
			return false
		}
	}
	return true
}

// candidateNodesPredicate selects all nodes that are in ready state and devoid of any exclude labels.
// This is a duplicate definition of the function in:
// https://github.com/kubernetes/kubernetes/blob/3723713c550f649b6ba84964edef9da6cc334f9d/staging/src/k8s.io/cloud-provider/controllers/service/controller.go#L668
func (z *ZoneGetter) candidateNodesPredicate(node *api_v1.Node, nodeAndFilterLogger klog.Logger) bool {
	return z.nodePredicateInternal(node, false, false, nodeAndFilterLogger)
}

// candidateNodesPredicateIncludeUnreadyExcludeUpgradingNodes selects all nodes except ones that are upgrading and/or have any exclude labels. This function tolerates unready nodes.
// TODO(prameshj) - Once the kubernetes/kubernetes Predicate function includes Unready nodes and the GKE nodepool code sets exclude labels on upgrade, this can be replaced with CandidateNodesPredicate.
func (z *ZoneGetter) candidateNodesPredicateIncludeUnreadyExcludeUpgradingNodes(node *api_v1.Node, nodeAndFilterLogger klog.Logger) bool {
	return z.nodePredicateInternal(node, true, true, nodeAndFilterLogger)
}

func (z *ZoneGetter) nodePredicateInternal(node *api_v1.Node, includeUnreadyNodes, excludeUpgradingNodes bool, nodeAndFilterLogger klog.Logger) bool {
	if z.mode != Legacy {
		nodeTopologySynced := z.nodeTopologyHasSynced()
		if z.onlyIncludeDefaultSubnetNodes || !nodeTopologySynced {
			nodeAndFilterLogger.Info("Falling back to only using default subnet when listing nodes", "z.onlyIncludeDefaultSubnetNodes", z.onlyIncludeDefaultSubnetNodes, "nodeTopologySynced", nodeTopologySynced)
			isInDefaultSubnet, err := isNodeInDefaultSubnet(node, z.defaultSubnetURL, nodeAndFilterLogger)
			if err != nil {
				nodeAndFilterLogger.Error(err, "Failed to verify if the node is in default subnet")
				return false
			}
			if !isInDefaultSubnet {
				nodeAndFilterLogger.Error(ErrNodeNotInDefaultSubnet, "Ignoring node since it is not in the default subnet")
				return false
			}
		}
	}

	// Get all nodes that have a taint with NoSchedule effect
	for _, taint := range node.Spec.Taints {
		if taint.Key == utils.ToBeDeletedTaint {
			return false
		}
	}

	// As of 1.6, we will taint the master, but not necessarily mark it unschedulable.
	// Recognize nodes labeled as master, and filter them also, as we were doing previously.
	if _, hasMasterRoleLabel := node.Labels[utils.LabelNodeRoleMaster]; hasMasterRoleLabel {
		return false
	}

	// Will be removed in 1.18
	if _, hasExcludeBalancerLabel := node.Labels[utils.LabelAlphaNodeRoleExcludeBalancer]; hasExcludeBalancerLabel {
		return false
	}

	if _, hasExcludeBalancerLabel := node.Labels[utils.LabelNodeRoleExcludeBalancer]; hasExcludeBalancerLabel {
		return false
	}
	if excludeUpgradingNodes {
		// This node is about to be upgraded or deleted as part of resize.
		if operation, _ := node.Labels[utils.GKECurrentOperationLabel]; operation == utils.NodeDrain {
			return false
		}
	}

	// If we have no info, don't accept
	if len(node.Status.Conditions) == 0 {
		return false
	}
	if includeUnreadyNodes {
		return true
	}
	for _, cond := range node.Status.Conditions {
		// We consider the node for load balancing only when its NodeReady condition status
		// is ConditionTrue
		if cond.Type == api_v1.NodeReady && cond.Status != api_v1.ConditionTrue {
			nodeAndFilterLogger.V(4).Info("Ignoring node", "conditionType", cond.Type, "conditionStatus", cond.Status)
			return false
		}
	}
	return true
}

// ZoneForNode returns if the given node is in default subnet.
func (z *ZoneGetter) IsDefaultSubnetNode(nodeName string, logger klog.Logger) (bool, error) {
	nodeLogger := logger.WithValues("nodeName", nodeName)
	node, err := listers.NewNodeLister(z.nodeLister).Get(nodeName)
	if err != nil {
		nodeLogger.Error(err, "Failed to get node")
		return false, err
	}
	return isNodeInDefaultSubnet(node, z.defaultSubnetURL, logger)
}

// isNodeInDefaultSubnet checks if the node is in the default subnet.
//
// For any new nodes created after multi-subnet cluster is enabled, they are
// guaranteed to have the subnet label if PodCIDR is populated. For any
// existing nodes, they will not have label and can only be in the default
// subnet.
func isNodeInDefaultSubnet(node *api_v1.Node, defaultSubnetURL string, nodeLogger klog.Logger) (bool, error) {
	nodeSubnet, err := getSubnet(node, defaultSubnetURL)
	if err != nil {
		nodeLogger.Error(err, "Failed to get node subnet", "nodeName", node.Name)
		return false, err
	}
	defaultSubnet, err := utils.KeyName(defaultSubnetURL)
	if err != nil {
		nodeLogger.Error(err, "Failed to extract default subnet information from URL", "defaultSubnetURL", defaultSubnetURL)
		return false, err
	}
	return nodeSubnet == defaultSubnet, nil
}

// getZone gets zone information from node provider id.
// A providerID is build out of '${ProviderName}://${project-id}/${zone}/${instance-name}'
func getZone(node *api_v1.Node) (string, error) {
	if node.Spec.ProviderID == "" {
		return "", fmt.Errorf("%w: node %s does not have providerID", ErrProviderIDNotFound, node.Name)
	}
	matches := providerIDRE.FindStringSubmatch(node.Spec.ProviderID)
	if len(matches) != 4 {
		return "", fmt.Errorf("%w: providerID %q of node %s is not valid", ErrSplitProviderID, node.Spec.ProviderID, node.Name)
	}
	return matches[2], nil
}

// getSubnet gets subnet information from node's LabelNodeSubnet.
// If a node doesn't have this label, or the label value is empty, it means
// this node is in the defaultSubnet and we will parse the subnet from the
// defaultSubnetURL.
func getSubnet(node *api_v1.Node, defaultSubnetURL string) (string, error) {
	if node.Spec.PodCIDR == "" {
		return "", ErrNodePodCIDRNotSet
	}

	nodeSubnet, exist := node.Labels[utils.LabelNodeSubnet]
	if exist && nodeSubnet != "" {
		return nodeSubnet, nil
	}
	defaultSubnet, err := utils.KeyName(defaultSubnetURL)
	if err != nil {
		return "", err
	}
	return defaultSubnet, nil
}

// NewNonGCPZoneGetter initialize a ZoneGetter in Non-GCP mode.
func NewNonGCPZoneGetter(zone string) *ZoneGetter {
	return &ZoneGetter{
		mode:             NonGCP,
		singleStoredZone: zone,
	}
}

// NewZoneGetter initialize a ZoneGetter in GCP mode.
func NewZoneGetter(nodeInformer, nodeTopologyInformer cache.SharedIndexInformer, defaultSubnetURL string) (*ZoneGetter, error) {

	subnetConfig, err := nodetopology.SubnetConfigFromSubnetURL(defaultSubnetURL)
	if err != nil {
		return nil, fmt.Errorf("failed to generate subnet config from default subnet url: %w", err)
	}

	return &ZoneGetter{
		mode:                          GCP,
		nodeLister:                    nodeInformer.GetIndexer(),
		nodeTopologyInformer:          nodeTopologyInformer,
		onlyIncludeDefaultSubnetNodes: flags.F.EnableMultiSubnetCluster && !flags.F.EnableMultiSubnetClusterPhase1,
		defaultSubnetURL:              defaultSubnetURL,
		defaultSubnetConfig:           subnetConfig,
		nodeTopologyHasSynced: func() bool {
			return nodeTopologyInformer != nil && nodeTopologyInformer.HasSynced()
		},
	}, nil
}

// NewZoneGetter initialize a ZoneGetter in Legacy mode.
func NewLegacyZoneGetter(nodeInformer, nodeTopologyInformer cache.SharedIndexInformer) *ZoneGetter {

	return &ZoneGetter{
		mode:                 Legacy,
		nodeLister:           nodeInformer.GetIndexer(),
		nodeTopologyInformer: nodeTopologyInformer,
	}
}

// NewFakeZoneGetter initialize a fake ZoneGetter in GCP mode to use in test.
func NewFakeZoneGetter(nodeInformer, nodeTopologyInformer cache.SharedIndexInformer, defaultSubnetURL string, onlyIncludeDefaultSubnetNodes bool) (*ZoneGetter, error) {
	subnetConfig, err := nodetopology.SubnetConfigFromSubnetURL(defaultSubnetURL)
	if err != nil {
		return nil, fmt.Errorf("failed to generate subnet config from default subnet url: %w", err)
	}

	return &ZoneGetter{
		mode:                          GCP,
		nodeLister:                    nodeInformer.GetIndexer(),
		nodeTopologyInformer:          nodeTopologyInformer,
		onlyIncludeDefaultSubnetNodes: onlyIncludeDefaultSubnetNodes,
		defaultSubnetURL:              defaultSubnetURL,
		defaultSubnetConfig:           subnetConfig,
		nodeTopologyHasSynced: func() bool {
			return nodeTopologyInformer != nil && nodeTopologyInformer.HasSynced()
		},
	}, nil
}
