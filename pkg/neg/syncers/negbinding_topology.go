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

type negOwnershipRegistry interface {
	Acquire(negName string, owner string) (bool, string)
	ReleaseAllOwnedExcept(owner string, keep sets.Set[string])
}

// NEGBindingTopologyProvider provides subnets and zones where NEGs should be managed
// based on NetworkEndpointGroupBinding CR
type NEGBindingTopologyProvider struct {
	negBindingName   string
	namespace        string
	negBindingLister cache.Indexer
	defaultSubnetID  *cloud.ResourceID
	registry         negOwnershipRegistry
}

// NewNEGBindingTopologyProvider constructs a new NEGBindingTopologyProvider
func NewNEGBindingTopologyProvider(namespace, negBindingName string, negBindingLister cache.Indexer, defaultSubnetURL string, registry negOwnershipRegistry) (*NEGBindingTopologyProvider, error) {
	defaultSubnetID, err := cloud.ParseResourceURL(defaultSubnetURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse default subnetwork URL %q: %w", defaultSubnetURL, err)
	}

	return &NEGBindingTopologyProvider{
		negBindingName:   negBindingName,
		namespace:        namespace,
		negBindingLister: negBindingLister,
		defaultSubnetID:  defaultSubnetID,
		registry:         registry,
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

// acquireSpecNEGRefs tries to acquire ownership of the NEG names from NEGBinding.Spec.
func (p *NEGBindingTopologyProvider) acquireSpecNEGRefs(binding *negbindingv1beta1.NetworkEndpointGroupBinding, logger klog.Logger) []negbindingv1beta1.SpecNegRef {
	statusNEGNamesPerSubnet := make(map[string]string)
	for _, ref := range binding.Status.NetworkEndpointGroups {
		subnetID, err := cloud.ParseResourceURL(ref.SubnetURL)
		if err != nil {
			continue
		}
		negID, err := cloud.ParseResourceURL(ref.ResourceURL)
		if err != nil {
			continue
		}
		statusNEGNamesPerSubnet[subnetID.Key.Name] = negID.Key.Name
	}

	keepNEGs := sets.New[string]()
	for _, negName := range statusNEGNamesPerSubnet {
		keepNEGs.Insert(negName)
	}

	ownerKey := fmt.Sprintf("%s/%s", p.namespace, p.negBindingName)
	if binding.DeletionTimestamp != nil {
		// Don't acquire any new NEGs if cleanup in progress
		p.registry.ReleaseAllOwnedExcept(ownerKey, keepNEGs)
		return nil
	}

	desiredSpecNEGs := sets.New[string]()
	for _, ref := range binding.Spec.NetworkEndpointGroups {
		if negName, ok := statusNEGNamesPerSubnet[ref.Subnet]; ok && negName != ref.Name {
			continue
		}
		desiredSpecNEGs.Insert(ref.Name)
		keepNEGs.Insert(ref.Name)
	}

	p.registry.ReleaseAllOwnedExcept(ownerKey, keepNEGs)

	var acquiredRefs []negbindingv1beta1.SpecNegRef
	for _, ref := range binding.Spec.NetworkEndpointGroups {
		if !desiredSpecNEGs.Has(ref.Name) {
			// There is currently NEG in status with same subnet which should be cleaned up before acquiring one from Spec
			continue
		}

		acquired, owner := p.registry.Acquire(ref.Name, ownerKey)
		if acquired {
			acquiredRefs = append(acquiredRefs, ref)
		} else {
			logger.Info("NEG name is owned by another binding, skipping", "negName", ref.Name, "owner", ownerKey, "currentOwner", owner)
		}
	}
	return acquiredRefs
}

// getSubnetsFromStatus returns the list of subnet names present in the NegBinding CR Status.
func (p *NEGBindingTopologyProvider) getSubnetsFromStatus(binding *negbindingv1beta1.NetworkEndpointGroupBinding, logger klog.Logger) []string {
	subnets := sets.New[string]()
	for _, ref := range binding.Status.NetworkEndpointGroups {
		subnetID, err := cloud.ParseResourceURL(ref.SubnetURL)
		if err != nil {
			logger.Error(err, "Failed to parse subnet URL from status", "subnetURL", ref.SubnetURL)
			continue
		}
		subnets.Insert(subnetID.Key.Name)
	}
	return subnets.UnsortedList()
}

// ListSubnetsInDefaultNetwork returns the list of subnets declared inside the NegBinding CR Spec.
func (p *NEGBindingTopologyProvider) ListSubnetsInDefaultNetwork(logger klog.Logger) []nodetopologyv1.SubnetConfig {
	binding, err := p.getBinding()
	if err != nil {
		logger.Error(err, "Failed to get NegBinding from store", "namespace", p.namespace, "negBindingName", p.negBindingName)
		return nil
	}

	// Return only subnets where NEGs are owned
	subnets := sets.New[string]()
	ownedSpecRefs := p.acquireSpecNEGRefs(binding, logger)
	for _, ref := range ownedSpecRefs {
		subnets.Insert(ref.Subnet)
	}
	subnets.Insert(p.getSubnetsFromStatus(binding, logger)...)

	configs := []nodetopologyv1.SubnetConfig{}
	for subnet := range subnets {
		key := &meta.Key{
			Name:   subnet,
			Region: p.defaultSubnetID.Key.Region,
		}
		subnetPath := cloud.SelfLink(meta.VersionGA, p.defaultSubnetID.ProjectID, p.defaultSubnetID.Resource, key)
		configs = append(configs, nodetopologyv1.SubnetConfig{
			Name:       subnet,
			SubnetPath: subnetPath,
		})
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

	// Return only zones of subnets, where NEGs are owned
	ownedSpecRefs := p.acquireSpecNEGRefs(binding, logger)
	zonesPerSubnet := make(shared.ZonesPerSubnetMap)
	for _, ref := range ownedSpecRefs {
		zonesPerSubnet[ref.Subnet] = sets.New(ref.Zones...)
	}
	return zonesPerSubnet, nil
}
