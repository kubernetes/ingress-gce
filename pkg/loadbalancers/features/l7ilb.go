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

// This file contains functionality and constants for the L7-ILB feature
// Since this also currently affects backend resources (since they are alpha-regional
// instead of ga-global), this feature is also included in pkg/backends/features.go
package features

import (
	"context"
	"errors"
	"fmt"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/filter"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"k8s.io/klog"
	"k8s.io/legacy-cloud-providers/gce"
)

var ErrSubnetNotFound = errors.New("active subnet not found")

// Get Subnet source range for ILB
// TODO: (shance) refactor to use filter
func ILBSubnetSourceRange(cloud *gce.Cloud, region string) (string, error) {
	subnets, err := cloud.Compute().BetaSubnetworks().List(context.Background(), region, filter.None)
	if err != nil {
		return "", fmt.Errorf("error obtaining subnets for region %s, %v", region, err)
	}

	for _, subnet := range subnets {
		sameNetwork, err := isSameNetwork(subnet.Network, cloud.NetworkURL())
		if err != nil {
			return "", fmt.Errorf("error comparing subnets: %v", err)
		}
		if subnet.Role == "ACTIVE" && subnet.Purpose == "INTERNAL_HTTPS_LOAD_BALANCER" && sameNetwork {
			klog.V(3).Infof("Found L7-ILB Subnet %s - %s", subnet.Name, subnet.IpCidrRange)
			return subnet.IpCidrRange, nil
		}
	}
	return "", ErrSubnetNotFound
}

// isSameNetwork() is a helper for comparing networks across API versions
func isSameNetwork(l, r string) (bool, error) {
	lID, err := cloud.ParseResourceURL(l)
	if err != nil {
		return false, err
	}
	rID, err := cloud.ParseResourceURL(r)
	if err != nil {
		return false, err
	}

	return lID.Equal(rID), nil
}

// IsCustomModeNetwork is a helper for determining if a network is a custom mode network
// (as opposed to a auto mode network).  This is used for l7-ilb since custom mode networks
// require an additional field on the forwarding rule
func IsCustomModeNetwork(c *gce.Cloud, networkURL string) (bool, error) {
	netID, err := cloud.ParseResourceURL(networkURL)
	if err != nil {
		return false, err
	}

	network, err := c.Compute().Networks().Get(context.Background(), netID.Key)
	if err != nil {
		return false, err
	}

	return !network.AutoCreateSubnetworks, nil
}

// L7ILBVersion is a helper to get the version of L7-ILB
func L7ILBVersions() *ResourceVersions {
	return versionsFromFeatures([]string{FeatureL7ILB})
}

// L7ILBScope is a helper to get the scope of L7-ILB
func L7ILBScope() meta.KeyType {
	return scopeFromFeatures([]string{FeatureL7ILB})
}
