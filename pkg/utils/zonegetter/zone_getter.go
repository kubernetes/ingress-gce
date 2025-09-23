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

	listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/ingress-gce/pkg/flags"
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
	// In Legacy mode, the ZoneGetter assumes that all nodes are in the default
	// subnet. Legacy networks have no concept of subnets, so the subnet returned
	// is always empty.
	Legacy
)

const (
	AllNodesFilter                 = Filter("AllNodesFilter")
	CandidateNodesFilter           = Filter("CandidateNodesFilter")
	CandidateAndUnreadyNodesFilter = Filter("CandidateAndUnreadyNodesFilter")
)

var ErrProviderIDNotFound = errors.New("providerID not found")
var ErrSplitProviderID = errors.New("error splitting providerID")
var ErrNodeNotFound = errors.New("node not found")
var ErrNodeNotInDefaultSubnet = errors.New("Node not in default subnet")
var ErrNodePodCIDRNotSet = errors.New("Node does not have PodCIDR set")

// providerIDRE is the regex to process providerID.
// A providerID is build out of '${ProviderName}://${project-id}/${zone}/${instance-name}'
var providerIDRE = regexp.MustCompile(`^` + "gce" + `://([^/]+)/([^/]+)/([^/]+)$`)

// ZoneGetter manages lookups for GCE instances to zones.
type ZoneGetter struct {
	nodeLister cache.Indexer
	// Mode indicates if the ZoneGetter is in GCP, Non-GCP, or Legacy mode
	// GCP mode ZoneGetter fetches zones from k8s node resource objects.
	// Non-GCP mode ZoneGetter always return its one single stored zone
	mode Mode
	// singleStoredZone is the single stored zone in the zoneGetter
	// It is only used in Non-GCP mode.
	singleStoredZone string

	// Whether zoneGetter should only list default subnet nodes.
	onlyIncludeDefaultSubnetNodes bool

	// The subnetURL of the cluster's default subnet.
	defaultSubnetURL string
}

// ZoneAndSubnetForNode returns the zone and subnet for a given node by looking up providerID.
// In Legacy mode, the default subnet check is skipped. The subnet obtained from z.getSubnet
// should be empty so in legacy mode only the zone is returned for the given node.
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

	subnet, err := z.getSubnet(node, z.defaultSubnetURL)
	if err != nil {
		nodeLogger.Error(err, "Failed to get subnet from node's LabelNodeSubnet")
		return "", "", err
	}
	if z.onlyIncludeDefaultSubnetNodes && z.mode != Legacy {
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
		if err != nil {
			filterLogger.Error(err, "Failed to get zone from providerID", "nodeName", n.Name)
			continue
		}
		zones.Insert(zone)
	}
	return zones.List(), nil
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
	if z.onlyIncludeDefaultSubnetNodes {
		isInDefaultSubnet, err := z.isNodeInDefaultSubnet(node, z.defaultSubnetURL, nodeLogger)
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
	if z.onlyIncludeDefaultSubnetNodes {
		isInDefaultSubnet, err := z.isNodeInDefaultSubnet(node, z.defaultSubnetURL, nodeAndFilterLogger)
		if err != nil {
			nodeAndFilterLogger.Error(err, "Failed to verify if the node is in default subnet")
			return false
		}
		if !isInDefaultSubnet {
			nodeAndFilterLogger.Error(ErrNodeNotInDefaultSubnet, "Ignoring node since it is not in the default subnet")
			return false
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

// IsDefaultSubnetNode returns if the given node is in default subnet.
func (z *ZoneGetter) IsDefaultSubnetNode(nodeName string, logger klog.Logger) (bool, error) {
	nodeLogger := logger.WithValues("nodeName", nodeName)
	node, err := listers.NewNodeLister(z.nodeLister).Get(nodeName)
	if err != nil {
		nodeLogger.Error(err, "Failed to get node")
		return false, err
	}
	return z.isNodeInDefaultSubnet(node, z.defaultSubnetURL, logger)
}

// isNodeInDefaultSubnet checks if the node is in the default subnet.
//
// For any new nodes created after multi-subnet cluster is enabled, they are
// guaranteed to have the subnet label if PodCIDR is populated. For any
// existing nodes, they will not have label and can only be in the default
// subnet. In Legacy mode all nodes are considered to be in the default subnet,
// so the only check done is whether the Node's PodCIDR is populated.
func (z *ZoneGetter) isNodeInDefaultSubnet(node *api_v1.Node, defaultSubnetURL string, nodeLogger klog.Logger) (bool, error) {
	if z.mode == Legacy {
		if node.Spec.PodCIDR == "" {
			return false, ErrNodePodCIDRNotSet
		}
		return true, nil
	}
	nodeSubnet, err := z.getSubnet(node, defaultSubnetURL)
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
	if matches[2] == "" {
		return "", fmt.Errorf("%w: node %s has an empty zone", ErrSplitProviderID, node.Name)
	}
	return matches[2], nil
}

// getSubnet gets subnet information from node's LabelNodeSubnet.
// If a node doesn't have this label, or the label value is empty, it means
// this node is in the defaultSubnet and we will parse the subnet from the
// defaultSubnetURL. In Legacy mode, the subnet returned is always empty.
func (z *ZoneGetter) getSubnet(node *api_v1.Node, defaultSubnetURL string) (string, error) {
	if node.Spec.PodCIDR == "" {
		return "", ErrNodePodCIDRNotSet
	}
	if z.mode == Legacy {
		return "", nil
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
func NewZoneGetter(nodeInformer cache.SharedIndexInformer, defaultSubnetURL string) *ZoneGetter {
	return &ZoneGetter{
		mode:                          GCP,
		nodeLister:                    nodeInformer.GetIndexer(),
		onlyIncludeDefaultSubnetNodes: flags.F.EnableMultiSubnetCluster && !flags.F.EnableMultiSubnetClusterPhase1,
		defaultSubnetURL:              defaultSubnetURL,
	}
}

// NewLegacyZoneGetter initialize a ZoneGetter in legacy network mode.
// Legacy Networks do not have subnets and therefore the subnet should
// always be empty. Regardless of the labels on the nodes, consider every
// node as part of the default subnet. onlyIncludeDefaultSubnetNodes is
// considered true as with Legacy Networks everything is in the default subnet.
func NewLegacyZoneGetter(nodeInformer cache.SharedIndexInformer) *ZoneGetter {
	return &ZoneGetter{
		mode:                          Legacy,
		nodeLister:                    nodeInformer.GetIndexer(),
		onlyIncludeDefaultSubnetNodes: true,
	}
}

// NewFakeZoneGetter initialize a fake ZoneGetter in GCP mode to use in test.
func NewFakeZoneGetter(nodeInformer cache.SharedIndexInformer, defaultSubnetURL string, onlyIncludeDefaultSubnetNodes bool) *ZoneGetter {
	return &ZoneGetter{
		mode:                          GCP,
		nodeLister:                    nodeInformer.GetIndexer(),
		onlyIncludeDefaultSubnetNodes: onlyIncludeDefaultSubnetNodes,
		defaultSubnetURL:              defaultSubnetURL,
	}
}
