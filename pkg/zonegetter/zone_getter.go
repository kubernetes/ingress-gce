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
	"fmt"

	api_v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/sets"
	listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog/v2"
)

// ZoneGetter implements ZoneGetter interface
type ZoneGetter struct {
	NodeInformer cache.SharedIndexInformer
}

// GetZoneForNode returns the zone for a given node by looking up its zone label.
func (z *ZoneGetter) GetZoneForNode(name string) (string, error) {
	nodeLister := z.NodeInformer.GetIndexer()
	nodes, err := listers.NewNodeLister(nodeLister).List(labels.Everything())
	if err != nil {
		return "", err
	}
	for _, n := range nodes {
		if n.Name == name {
			// TODO: Make this more resilient to label changes by listing
			// cloud nodes and figuring out zone.
			return getZone(n), nil
		}
	}
	return "", fmt.Errorf("node not found %v", name)
}

// ListZones returns a list of zones containing nodes that satisfy the given predicate.
func (z *ZoneGetter) ListZones(predicate utils.NodeConditionPredicate) ([]string, error) {
	nodeLister := z.NodeInformer.GetIndexer()
	return z.listZones(listers.NewNodeLister(nodeLister), predicate)
}

func (z *ZoneGetter) listZones(lister listers.NodeLister, predicate utils.NodeConditionPredicate) ([]string, error) {
	zones := sets.String{}
	nodes, err := utils.ListWithPredicate(lister, predicate)
	if err != nil {
		return zones.List(), err
	}
	for _, n := range nodes {
		zones.Insert(getZone(n))
	}
	return zones.List(), nil
}

func getZone(node *api_v1.Node) string {
	zone, ok := node.Labels[annotations.ZoneKey]
	if !ok {
		klog.Warningf("Node without zone label %q, returning %q as zone. Node name: %v, node labels: %v", annotations.ZoneKey, annotations.DefaultZone, node.Name, node.Labels)
		return annotations.DefaultZone
	}
	return zone
}
