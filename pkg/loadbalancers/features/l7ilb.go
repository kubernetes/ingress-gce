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

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/filter"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"k8s.io/klog"
	"k8s.io/legacy-cloud-providers/gce"
)

var ErrSubnetNotFound = errors.New("active subnet not found")

// Get Subnet source range for ILB
// TODO: (shance) refactor to use filter
func ILBSubnetSourceRange(cloud *gce.Cloud, region string) (string, error) {
	subnets, err := cloud.Compute().AlphaSubnetworks().List(context.Background(), region, filter.None)
	if err != nil {
		return "", fmt.Errorf("error obtaining subnets for region %s, %v", region, err)
	}

	for _, subnet := range subnets {
		if subnet.Role == "ACTIVE" && subnet.Purpose == "INTERNAL_HTTPS_LOAD_BALANCER" {
			klog.V(3).Infof("Found L7-ILB Subnet %s - %s", subnet.Name, subnet.IpCidrRange)
			return subnet.IpCidrRange, nil
		}
	}
	return "", ErrSubnetNotFound
}

// L7ILBVersion is a helper to get the version of L7-ILB
func L7ILBVersion(resource LBResource) meta.Version {
	return versionFromFeatures([]string{FeatureL7ILB}, resource)
}

// L7ILBScope is a helper to get the scope of L7-ILB
func L7ILBScope() meta.KeyType {
	return scopeFromFeatures([]string{FeatureL7ILB})
}
