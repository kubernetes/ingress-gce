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
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/filter"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
)

const (
	ILBVersion = meta.VersionAlpha
)

// Get Subnet source range for ILB from FrontendConfig
// TODO: (shance) refactor to use filter
func ILBSubnetSourceRange(cloud cloud.Cloud, region string) (string, error) {
	subnets, err := cloud.AlphaSubnetworks().List(context.Background(), region, filter.None)
	if err != nil {
		return "", fmt.Errorf("error obtaining subnets for region: %s", region)
	}

	for _, subnet := range subnets {
		if subnet.Role == "ACTIVE" && subnet.Purpose == "INTERNAL_HTTPS_LOAD_BALANCER" {
			return subnet.IpCidrRange, nil
		}
	}

	return "", nil
}
