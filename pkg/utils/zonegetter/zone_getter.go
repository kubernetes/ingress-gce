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
)

const (
	AllNodesFilter                 = "AllNodesFilter"
	CandidateNodesFilter           = "CandidateNodesFilter"
	CandidateAndUnreadyNodesFilter = "CandidateAndUnreadyNodesFilter"
)

var ErrProviderIDNotFound = errors.New("providerID not found")
var ErrSplitProviderID = errors.New("error splitting providerID")
var ErrNodeNotFound = errors.New("node not found")

// providerIDRE is the regex to process providerID.
// A providerID is build out of '${ProviderName}://${project-id}/${zone}/${instance-name}'
var providerIDRE = regexp.MustCompile(`^` + "gce" + `://([^/]+)/([^/]+)/([^/]+)$`)

// ZoneGetter manages lookups for GCE instances to zones.
type ZoneGetter struct {
	nodeInformer cache.SharedIndexInformer
	// Mode indicates if the ZoneGetter is in GCP or Non-GCP mode
	// GCP mode ZoneGetter fetches zones from k8s node resource objects.
	// Non-GCP mode ZoneGetter always return its one single stored zone
	mode Mode
	// singleStoredZone is the single stored zone in the zoneGetter
	// It is only used in Non-GCP mode.
	singleStoredZone string
}

// ZoneForNode returns the zone for a given node by looking up providerID.
func (z *ZoneGetter) ZoneForNode(name string, logger klog.Logger) (string, error) {
	// Return the single stored zone if the zoneGetter is in non-gcp mode.
	if z.mode == NonGCP {
		logger.Info("ZoneGetter in non-gcp mode, return the single stored zone", "zone", z.singleStoredZone)
		return z.singleStoredZone, nil
	}

	nodeLister := z.nodeInformer.GetIndexer()
	node, err := listers.NewNodeLister(nodeLister).Get(name)
	if err != nil {
		logger.Error(err, "Failed to get node", "nodeName", name)
		return "", fmt.Errorf("%w: failed to get node %s", ErrNodeNotFound, name)
	}
	if node.Spec.ProviderID == "" {
		logger.Error(ErrProviderIDNotFound, "Node does not have providerID", "nodeName", name)
		return "", ErrProviderIDNotFound
	}
	zone, err := getZone(node)
	if err != nil {
		logger.Error(err, "Failed to get zone from the providerID", "nodeName", name)
		return "", err
	}
	return zone, nil
}

// List returns a list of zones containing nodes that satisfy the given
// node filtering mode.
func (z *ZoneGetter) List(filter Filter, logger klog.Logger) ([]string, error) {
	// Return the single stored zone if the zoneGetter is in non-gcp mode.
	if z.mode == NonGCP {
		logger.Info("ZoneGetter in non-gcp mode, return the single stored zone", "zone", z.singleStoredZone)
		return []string{z.singleStoredZone}, nil
	}
	nodeLister := z.nodeInformer.GetIndexer()
	zones := sets.String{}
	nodes, err := listNodesWithFilter(listers.NewNodeLister(nodeLister), filter, logger)
	if err != nil {
		logger.Error(err, "Failed to list nodes")
		return zones.List(), err
	}
	for _, n := range nodes {
		zone, err := getZone(n)
		if err != nil {
			logger.Error(err, "Failed to get zone from providerID", "nodeName", n.Name)
			continue
		}
		zones.Insert(zone)
	}
	return zones.List(), nil
}

// listNodesWithFilter gets nodes that matches node filter mode.
func listNodesWithFilter(nodeLister listers.NodeLister, filter Filter, logger klog.Logger) ([]*api_v1.Node, error) {
	var filtered []*api_v1.Node
	var predicate utils.NodeConditionPredicate
	logger.Info("Filtering nodes", "filter", filter)
	switch filter {
	case AllNodesFilter:
		predicate = utils.AllNodesPredicate
	case CandidateNodesFilter:
		predicate = utils.CandidateNodesPredicate
	case CandidateAndUnreadyNodesFilter:
		predicate = utils.CandidateNodesPredicateIncludeUnreadyExcludeUpgradingNodes
	default:
		return filtered, nil
	}

	nodes, err := nodeLister.List(labels.Everything())
	if err != nil {
		return nil, err
	}

	filteredOut := []string{}
	for _, node := range nodes {
		// Filter nodes with predicate and exclude nodes without providerID
		if predicate(node, logger) {
			filtered = append(filtered, node)
		} else {
			filteredOut = append(filteredOut, node.Name)
		}
	}
	if len(filteredOut) <= 50 {
		logger.Info("Filtered out nodes when listing node zones", "nodes", filteredOut, "filter", filter)
	}

	return filtered, nil
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

// NewNonGCPZoneGetter initialize a ZoneGetter in Non-GCP mode.
func NewNonGCPZoneGetter(zone string) *ZoneGetter {
	return &ZoneGetter{
		mode:             NonGCP,
		singleStoredZone: zone,
	}
}

// NewZoneGetter initialize a ZoneGetter in GCP mode.
func NewZoneGetter(nodeInformer cache.SharedIndexInformer) *ZoneGetter {
	return &ZoneGetter{
		mode:         GCP,
		nodeInformer: nodeInformer,
	}
}
