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
	apiv1 "k8s.io/api/core/v1"
)

// ServiceNetwork determines the network data to be used for the LB resources.
// This function currently returns only the default network but will provide
// secondary networks information for multi-networked services in the future.
func ServiceNetwork(_ *apiv1.Service, cloudProvider cloudNetworkProvider) (*NetworkInfo, error) {
	return &NetworkInfo{
		IsNonDefault:  false,
		K8sNetwork:    "default",
		NetworkURL:    cloudProvider.NetworkURL(),
		SubnetworkURL: cloudProvider.SubnetworkURL(),
	}, nil
}

type cloudNetworkProvider interface {
	NetworkURL() string
	SubnetworkURL() string
}

// NetworkInfo contains the information about the network the LB resources should be created in.
type NetworkInfo struct {
	// IsNonDefault indicates if the network is not the default one.
	IsNonDefault bool
	// K8sNetwork is the network name of the Network resource in the cluster.
	// This name should be used when referring to k8s API network.
	K8sNetwork string
	// NetworkURL is the GCE VPC URL (to be used in GCE LB resources).
	NetworkURL string
	// SubnetworkURL is the GCE subnetwork URL (to be used in GCE LB resources).
	SubnetworkURL string
}
