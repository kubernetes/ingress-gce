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
	networkSelector        = networkv1.NetworkAnnotationKey
)

// ServiceNetwork determines the network data to be used for the LB resources.
// This function currently returns only the default network but will provide
// secondary networks information for multi-networked services in the future.
func ServiceNetwork(service *apiv1.Service, networkLister, gkeNetworkParamSetLister cache.Indexer, cloudProvider cloudNetworkProvider, logger klog.Logger) (*NetworkInfo, error) {
	if networkLister == nil || gkeNetworkParamSetLister == nil {
		return DefaultNetwork(cloudProvider), nil
	}
	logger.Info("Network lookup for service", "service", service.Name, "namespace", service.Namespace)
	networkName, ok := service.Spec.Selector[networkSelector]
	if !ok || networkName == "" || networkName == networkv1.DefaultPodNetworkName {
		return DefaultNetwork(cloudProvider), nil
	}
	obj, exists, err := networkLister.GetByKey(networkName)
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
	logger.Info("Found network for service", "network", network.Name, "service", service.Name, "namespace", service.Namespace)
	parametersRef := network.Spec.ParametersRef
	if !refersGKENetworkParamSet(parametersRef) {
		return nil, fmt.Errorf("network.Spec.ParametersRef does not refer a GKENetworkParamSet resource")
	}
	if parametersRef.Namespace != nil {
		return nil, fmt.Errorf("network.Spec.ParametersRef.namespace must not be set for GKENetworkParamSet reference as it is a cluster scope resource")
	}
	gkeParamsObj, exists, err := gkeNetworkParamSetLister.GetByKey(parametersRef.Name)
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
	netURL := networkURL(cloudProvider, gkeNetworkParamSet.Spec.VPC)
	subnetURL := subnetworkURL(cloudProvider, gkeNetworkParamSet.Spec.VPCSubnet)

	logger.Info("Found GKE network parameters for service", "NetworkURL", netURL, "SubnetworkURL", subnetURL, "service", service.Name, "namespace", service.Namespace)
	return &NetworkInfo{
		IsDefault:     false,
		K8sNetwork:    networkName,
		NetworkURL:    netURL,
		SubnetworkURL: subnetURL,
	}, nil
}

func DefaultNetwork(cloudProvider cloudNetworkProvider) *NetworkInfo {
	return &NetworkInfo{
		IsDefault:     true,
		K8sNetwork:    networkv1.DefaultPodNetworkName,
		NetworkURL:    cloudProvider.NetworkURL(),
		SubnetworkURL: cloudProvider.SubnetworkURL(),
	}
}

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
