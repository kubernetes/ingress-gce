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

package resourcemanager

import (
	nodetopologyv1 "github.com/GoogleCloudPlatform/gke-networking-api/apis/nodetopology/v1"
	discovery "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/ingress-gce/pkg/composite"

	"k8s.io/ingress-gce/pkg/network"
	"k8s.io/ingress-gce/pkg/utils/zonegetter"
)

type NegResourceManager interface {
	UpdateStatusNegsEnsured(negObjs []*composite.NetworkEndpointGroup, errList []error)
	UpdateStatusSyncCompleted(syncErr error) (needInint bool)

	LocationsChanged() bool
	GetNegNameForSubnet(subnet string) (string, error)
	ListSubnets() []nodetopologyv1.SubnetConfig
	ListZonesForSubnet(subnet string, filter zonegetter.Filter) ([]string, error)
	GenerateSubnetToNegNameMap(subnetConfigs []nodetopologyv1.SubnetConfig) (map[string]string, error)

	EnsureNeg(negName, zone string, networkInfo network.NetworkInfo) (*composite.NetworkEndpointGroup, error)
	ListEndpoints(negName, zone string, showHealthStatus bool, candidateZonesMap sets.Set[string]) ([]*composite.NetworkEndpointWithHealthStatus, error)

	ComputeEPSStaleness([]*discovery.EndpointSlice)
}
