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

package network

import (
	"fmt"
	"strings"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	apiv1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"
	networkv1 "k8s.io/cloud-provider-gcp/crd/apis/network/v1"
	"k8s.io/klog/v2"
)

const (
	networkingGKEGroup     = networkv1.GroupName
	gkeNetworkParamSetKind = "gkenetworkparamset"

	// NetworkSelectorKey is the key under which services indicate the network to use in their selector.
	NetworkSelectorKey = networkv1.NetworkAnnotationKey
)

// Resolver is the interface to resolve networks that the LB resources should be created in.
type Resolver interface {
	ServiceNetwork(service *apiv1.Service) (*NetworkInfo, error)
	IsMultinetService(service *apiv1.Service) bool
	NetworkNameFromSelector(service *apiv1.Service) string
}

// NetworksResolver is responsible for resolving networks that the LB resources should be created in.
type NetworksResolver struct {
	networkLister            cache.Indexer
	gkeNetworkParamSetLister cache.Indexer
	cloudProvider            cloudNetworkProvider
	enableMultinetworking    bool
	logger                   klog.Logger
}

// NewNetworksResolver creates a new instance of the NetworksResolver.
func NewNetworksResolver(networkLister, gkeNetworkParamSetLister cache.Indexer, cloudProvider cloudNetworkProvider, enableMultinetworking bool, logger klog.Logger) *NetworksResolver {
	return &NetworksResolver{
		networkLister:            networkLister,
		gkeNetworkParamSetLister: gkeNetworkParamSetLister,
		cloudProvider:            cloudProvider,
		enableMultinetworking:    enableMultinetworking,
		logger:                   logger,
	}
}

// ServiceNetwork determines the network data to be used for the LB resources.
// This function currently returns only the default network but will provide
// secondary networks information for multi-networked services in the future.
func (nr *NetworksResolver) ServiceNetwork(service *apiv1.Service) (*NetworkInfo, error) {
	if !nr.enableMultinetworking || nr.networkLister == nil || nr.gkeNetworkParamSetLister == nil {
		return DefaultNetwork(nr.cloudProvider), nil
	}
	nr.logger.Info("Network lookup for service", "service", service.Name, "namespace", service.Namespace)
	networkName, ok := service.Spec.Selector[NetworkSelectorKey]
	if !ok || networkName == "" || networkName == networkv1.DefaultPodNetworkName {
		return DefaultNetwork(nr.cloudProvider), nil
	}
	obj, exists, err := nr.networkLister.GetByKey(networkName)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("network %s does not exist", networkName)
	}
	network := obj.(*networkv1.Network)
	if network == nil {
		return nil, fmt.Errorf("cannot convert to Network (%T)", obj)
	}
	nr.logger.Info("Found network for service", "network", network.Name, "service", service.Name, "namespace", service.Namespace)
	parametersRef := network.Spec.ParametersRef
	if !refersGKENetworkParamSet(parametersRef) {
		return nil, fmt.Errorf("network.Spec.ParametersRef does not refer a GKENetworkParamSet resource")
	}
	if parametersRef.Namespace != nil {
		return nil, fmt.Errorf("network.Spec.ParametersRef.namespace must not be set for GKENetworkParamSet reference as it is a cluster scope resource")
	}
	gkeParamsObj, exists, err := nr.gkeNetworkParamSetLister.GetByKey(parametersRef.Name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("GKENetworkParamSet %s was not found", parametersRef.Name)
	}
	gkeNetworkParamSet := gkeParamsObj.(*networkv1.GKENetworkParamSet)
	if network == nil {
		return nil, fmt.Errorf("cannot convert to GKENetworkParamSet (%T)", gkeParamsObj)
	}
	netURL := networkURL(nr.cloudProvider, gkeNetworkParamSet.Spec.VPC)
	subnetURL := subnetworkURL(nr.cloudProvider, gkeNetworkParamSet.Spec.VPCSubnet)

	nr.logger.Info("Found GKE network parameters for service", "NetworkURL", netURL, "SubnetworkURL", subnetURL, "service", service.Name, "namespace", service.Namespace)
	return &NetworkInfo{
		IsDefault:     false,
		K8sNetwork:    networkName,
		NetworkURL:    netURL,
		SubnetworkURL: subnetURL,
	}, nil
}

// IsMultinetService checks if the service is a multinet service.
// It can only be true if multinetworking is enabled and the service has the multinet selector with a non-default network name.
func (nr *NetworksResolver) IsMultinetService(service *apiv1.Service) bool {
	if !nr.enableMultinetworking {
		return false
	}
	networkName, ok := service.Spec.Selector[NetworkSelectorKey]
	if !ok || networkName == "" || networkName == networkv1.DefaultPodNetworkName {
		return false
	}
	return true
}

func (nr *NetworksResolver) NetworkNameFromSelector(service *apiv1.Service) string {
	if !nr.enableMultinetworking {
		return ""
	}
	networkName := service.Spec.Selector[NetworkSelectorKey]
	return networkName
}

// DefaultNetwork creates network information struct of the default network. Default network is the main cluster network.
func DefaultNetwork(cloudProvider cloudNetworkProvider) *NetworkInfo {
	return &NetworkInfo{
		IsDefault:     true,
		K8sNetwork:    networkv1.DefaultPodNetworkName,
		NetworkURL:    cloudProvider.NetworkURL(),
		SubnetworkURL: cloudProvider.SubnetworkURL(),
	}
}

// refersGKENetworkParamSet checks if a NetworkParametersReference points to GKENetworkParamSet resource.
func refersGKENetworkParamSet(parametersRef *networkv1.NetworkParametersReference) bool {
	return parametersRef != nil &&
		parametersRef.Group == networkingGKEGroup &&
		strings.ToLower(parametersRef.Kind) == gkeNetworkParamSetKind &&
		parametersRef.Name != ""
}

func networkURL(cloudProvider cloudNetworkProvider, vpc string) string {
	key := meta.GlobalKey(vpc)
	return cloud.SelfLink(meta.VersionGA, cloudProvider.NetworkProjectID(), "networks", key)
}

func subnetworkURL(cloudProvider cloudNetworkProvider, subnetwork string) string {
	key := meta.RegionalKey(subnetwork, cloudProvider.Region())
	return cloud.SelfLink(meta.VersionGA, cloudProvider.NetworkProjectID(), "subnetworks", key)
}

// GetNodeIPForNetwork retrieves the IP of the interface of the node connected to the network.
// The addresses come from the 'networking.gke.io/north-interfaces' annotation.
func GetNodeIPForNetwork(node *apiv1.Node, network string) string {
	northInterfacesAnnotation, ok := node.Annotations[networkv1.NorthInterfacesAnnotationKey]
	if !ok || northInterfacesAnnotation == "" {
		return ""
	}
	northInterfaces, err := networkv1.ParseNorthInterfacesAnnotation(northInterfacesAnnotation)
	if err != nil {
		return ""
	}
	for _, northInterface := range northInterfaces {
		if northInterface.Network == network {
			return northInterface.IpAddress
		}
	}
	return ""
}

type cloudNetworkProvider interface {
	NetworkURL() string
	SubnetworkURL() string
	NetworkProjectID() string
	Region() string
}

// NetworkInfo contains the information about the network the LB resources should be created in.
type NetworkInfo struct {
	// IsDefault indicates if the network is the default one.
	IsDefault bool
	// K8sNetwork is the network name of the Network resource in the cluster.
	// This name should be used when referring to k8s API network.
	K8sNetwork string
	// NetworkURL is the GCE VPC URL (to be used in GCE LB resources).
	NetworkURL string
	// SubnetworkURL is the GCE subnetwork URL (to be used in GCE LB resources).
	SubnetworkURL string
}

// IsNodeConnected checks if the node is connected to the given network.
// All nodes are connected to the default network.
// For non default networks the result is based on the data from
// the 'networking.gke.io/north-interfaces' node annotation.
func (ni *NetworkInfo) IsNodeConnected(node *apiv1.Node) bool {
	return ni.IsDefault || GetNodeIPForNetwork(node, ni.K8sNetwork) != ""
}

// FakeNetworkResolver is an implementation of a Resolver that just returns a previously set NetworkInfo. To be used only in tests.
type FakeNetworkResolver struct {
	networkInfo *NetworkInfo
	err         error
}

func NewFakeResolver(networkInfo *NetworkInfo) *FakeNetworkResolver {
	return &FakeNetworkResolver{networkInfo: networkInfo}
}

func NewFakeResolverWithError(err error) *FakeNetworkResolver {
	return &FakeNetworkResolver{err: err}
}

func (fake *FakeNetworkResolver) ServiceNetwork(service *apiv1.Service) (*NetworkInfo, error) {
	if fake.err != nil {
		return nil, fake.err
	}
	return fake.networkInfo, nil
}

func (fake *FakeNetworkResolver) NetworkNameFromSelector(service *apiv1.Service) string {
	return service.Spec.Selector[NetworkSelectorKey]
}

func (fake *FakeNetworkResolver) IsMultinetService(service *apiv1.Service) bool {
	networkName, ok := service.Spec.Selector[NetworkSelectorKey]
	if !ok || networkName == "" || networkName == networkv1.DefaultPodNetworkName {
		return false
	}
	return true
}
