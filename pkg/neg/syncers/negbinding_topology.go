/*
Copyright 2026 The Kubernetes Authors.

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

	nodetopologyv1 "github.com/GoogleCloudPlatform/gke-networking-api/apis/nodetopology/v1"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/cache"
	negbindingv1beta1 "k8s.io/ingress-gce/pkg/apis/negbinding/v1beta1"
	"k8s.io/ingress-gce/pkg/neg/types/shared"
	"k8s.io/ingress-gce/pkg/network"
	"k8s.io/ingress-gce/pkg/utils/zonegetter"
	"k8s.io/klog/v2"
)

// NEGBindingTopologyProvider provides subnets and zones where NEGs should be managed
// based on NetworkEndpointGroupBinding CR
type NEGBindingTopologyProvider struct {
	negBindingName   string
	namespace        string
	negBindingLister cache.Indexer
	defaultSubnetID  *cloud.ResourceID
}

// NewNEGBindingTopologyProvider constructs a new NEGBindingTopologyProvider
func NewNEGBindingTopologyProvider(namespace, negBindingName string, negBindingLister cache.Indexer, defaultSubnetURL string) (*NEGBindingTopologyProvider, error) {
	defaultSubnetID, err := cloud.ParseResourceURL(defaultSubnetURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse default subnetwork URL %q: %w", defaultSubnetURL, err)
	}

	return &NEGBindingTopologyProvider{
		negBindingName:   negBindingName,
		namespace:        namespace,
		negBindingLister: negBindingLister,
		defaultSubnetID:  defaultSubnetID,
	}, nil
}

func (p *NEGBindingTopologyProvider) getBinding() (*negbindingv1beta1.NetworkEndpointGroupBinding, error) {
	key := fmt.Sprintf("%s/%s", p.namespace, p.negBindingName)
	obj, exists, err := p.negBindingLister.GetByKey(key)
	if err != nil {
		return nil, fmt.Errorf("error getting negbinding from cache: %w", err)
	}
	if !exists {
		return nil, fmt.Errorf("negbinding %s is not in store", key)
	}
	binding, ok := obj.(*negbindingv1beta1.NetworkEndpointGroupBinding)
	if !ok {
		return nil, fmt.Errorf("cached object %q is of type %T, expected *NetworkEndpointGroupBinding", key, obj)
	}
	return binding, nil
}

// ListSubnetsInDefaultNetwork returns the list of subnets declared inside the NegBinding CR Spec.
func (p *NEGBindingTopologyProvider) ListSubnetsInDefaultNetwork(logger klog.Logger) []nodetopologyv1.SubnetConfig {
	binding, err := p.getBinding()
	if err != nil {
		logger.Error(err, "Failed to get NegBinding from store", "namespace", p.namespace, "negBindingName", p.negBindingName)
		return nil
	}

	configs := make([]nodetopologyv1.SubnetConfig, len(binding.Spec.NetworkEndpointGroups))
	for i, ref := range binding.Spec.NetworkEndpointGroups {
		key := &meta.Key{
			Name:   ref.Subnet,
			Region: p.defaultSubnetID.Key.Region,
		}
		subnetPath := cloud.SelfLink(meta.VersionGA, p.defaultSubnetID.ProjectID, p.defaultSubnetID.Resource, key)
		configs[i] = nodetopologyv1.SubnetConfig{
			Name:       ref.Subnet,
			SubnetPath: subnetPath,
		}
	}
	return configs
}

// ListZonesPerSubnet returns a map of subnet to zones defined inside the NegBinding CR Spec.
// NEGBinding contains explicit locations (subnet + zone pairs), where NEG controller is expected to
// manage NEGs ignoring if any endpoints available there. Therefore ignoring filtering.
func (p *NEGBindingTopologyProvider) ListZonesPerSubnet(_ zonegetter.Filter, networkInfo network.NetworkInfo, logger klog.Logger) (shared.ZonesPerSubnetMap, error) {
	if !networkInfo.IsDefault {
		return nil, fmt.Errorf("NEGBinding does not support multi-network mode")
	}

	binding, err := p.getBinding()
	if err != nil {
		return nil, fmt.Errorf("failed to get NegBinding from store: %w", err)
	}

	zonesPerSubnet := make(shared.ZonesPerSubnetMap)
	for _, ref := range binding.Spec.NetworkEndpointGroups {
		zonesPerSubnet[ref.Subnet] = sets.New(ref.Zones...)
	}
	return zonesPerSubnet, nil
}
