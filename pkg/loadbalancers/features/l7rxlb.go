/*
Copyright 2019 The Kubernetes Authors.
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

package features

import (
	"context"
	"fmt"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/filter"
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/klog/v2"
)

// RXLBSubnetSourceRange gets Subnet source range for RXLB.
func RXLBSubnetSourceRange(cloud *gce.Cloud, region string, logger klog.Logger) (string, error) {
	subnets, err := cloud.Compute().BetaSubnetworks().List(context.Background(), region, filter.None)
	if err != nil {
		return "", fmt.Errorf("error obtaining subnets for region %s, %v", region, err)
	}

	for _, subnet := range subnets {
		sameNetwork, err := isSameNetwork(subnet.Network, cloud.NetworkURL())
		if err != nil {
			return "", fmt.Errorf("error comparing subnets: %v", err)
		}
		if subnet.Role == "ACTIVE" && (subnet.Purpose == "REGIONAL_MANAGED_PROXY") && sameNetwork {
			logger.V(3).Info("Found L7-RXLB Subnet", "subnetName", subnet.Name, "subnetIpCidrRange", subnet.IpCidrRange)
			return subnet.IpCidrRange, nil
		}
	}
	return "", ErrSubnetNotFound
}
