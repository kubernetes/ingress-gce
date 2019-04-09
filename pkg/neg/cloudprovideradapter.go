/*
Copyright 2018 The Kubernetes Authors.

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

package neg

import (
	"fmt"
	"strings"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/filter"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	computebeta "google.golang.org/api/compute/v0.beta"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/kubernetes/pkg/cloudprovider/providers/gce"
)

// NewAdapter takes a Cloud and returns a NetworkEndpointGroupCloud.
func NewAdapter(g *gce.Cloud) negtypes.NetworkEndpointGroupCloud {
	return &cloudProviderAdapter{
		c:             g.Compute(),
		networkURL:    g.NetworkURL(),
		subnetworkURL: g.SubnetworkURL(),
	}
}

// cloudProviderAdapter is a temporary shim to consolidate accesses to
// Cloud and push them outside of this package.
type cloudProviderAdapter struct {
	c             cloud.Cloud
	networkURL    string
	subnetworkURL string
}

// GetNetworkEndpointGroup inmplements NetworkEndpointGroupCloud.
func (a *cloudProviderAdapter) GetNetworkEndpointGroup(name string, zone string) (*computebeta.NetworkEndpointGroup, error) {
	ctx, cancel := cloud.ContextWithCallTimeout()
	defer cancel()

	return a.c.BetaNetworkEndpointGroups().Get(ctx, meta.ZonalKey(name, zone))
}

// ListNetworkEndpointGroup implements NetworkEndpointGroupCloud.
func (a *cloudProviderAdapter) ListNetworkEndpointGroup(zone string) ([]*computebeta.NetworkEndpointGroup, error) {
	ctx, cancel := cloud.ContextWithCallTimeout()
	defer cancel()

	return a.c.BetaNetworkEndpointGroups().List(ctx, zone, filter.None)
}

// AggregatedListNetworkEndpointGroup returns a map of zone -> endpoint group.
func (a *cloudProviderAdapter) AggregatedListNetworkEndpointGroup() (map[string][]*computebeta.NetworkEndpointGroup, error) {
	ctx, cancel := cloud.ContextWithCallTimeout()
	defer cancel()

	// TODO: filter for the region the cluster is in.
	all, err := a.c.BetaNetworkEndpointGroups().AggregatedList(ctx, filter.None)
	if err != nil {
		return nil, err
	}
	ret := map[string][]*computebeta.NetworkEndpointGroup{}
	for key, byZone := range all {
		// key is "zones/<zone name>"
		parts := strings.Split(key, "/")
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid key for AggregatedListNetworkEndpointGroup: %q", key)
		}
		zone := parts[1]
		ret[zone] = append(ret[zone], byZone...)
	}
	return ret, nil
}

// CreateNetworkEndpointGroup implements NetworkEndpointGroupCloud.
func (a *cloudProviderAdapter) CreateNetworkEndpointGroup(neg *computebeta.NetworkEndpointGroup, zone string) error {
	ctx, cancel := cloud.ContextWithCallTimeout()
	defer cancel()

	return a.c.BetaNetworkEndpointGroups().Insert(ctx, meta.ZonalKey(neg.Name, zone), neg)
}

// DeleteNetworkEndpointGroup implements NetworkEndpointGroupCloud.
func (a *cloudProviderAdapter) DeleteNetworkEndpointGroup(name string, zone string) error {
	ctx, cancel := cloud.ContextWithCallTimeout()
	defer cancel()

	return a.c.BetaNetworkEndpointGroups().Delete(ctx, meta.ZonalKey(name, zone))
}

// AttachNetworkEndpoints implements NetworkEndpointGroupCloud.
func (a cloudProviderAdapter) AttachNetworkEndpoints(name, zone string, endpoints []*computebeta.NetworkEndpoint) error {
	ctx, cancel := cloud.ContextWithCallTimeout()
	defer cancel()

	req := &computebeta.NetworkEndpointGroupsAttachEndpointsRequest{NetworkEndpoints: endpoints}
	return a.c.BetaNetworkEndpointGroups().AttachNetworkEndpoints(ctx, meta.ZonalKey(name, zone), req)
}

// DetachNetworkEndpoints implements NetworkEndpointGroupCloud.
func (a *cloudProviderAdapter) DetachNetworkEndpoints(name, zone string, endpoints []*computebeta.NetworkEndpoint) error {
	ctx, cancel := cloud.ContextWithCallTimeout()
	defer cancel()

	req := &computebeta.NetworkEndpointGroupsDetachEndpointsRequest{NetworkEndpoints: endpoints}
	return a.c.BetaNetworkEndpointGroups().DetachNetworkEndpoints(ctx, meta.ZonalKey(name, zone), req)
}

// ListNetworkEndpoints implements NetworkEndpointGroupCloud.
func (a *cloudProviderAdapter) ListNetworkEndpoints(name, zone string, showHealthStatus bool) ([]*computebeta.NetworkEndpointWithHealthStatus, error) {
	ctx, cancel := cloud.ContextWithCallTimeout()
	defer cancel()

	healthStatus := "SKIP"
	if showHealthStatus {
		healthStatus = "SHOW"
	}
	req := &computebeta.NetworkEndpointGroupsListEndpointsRequest{HealthStatus: healthStatus}
	return a.c.BetaNetworkEndpointGroups().ListNetworkEndpoints(ctx, meta.ZonalKey(name, zone), req, filter.None)
}

// NetworkURL implements NetworkEndpointGroupCloud.
func (a *cloudProviderAdapter) NetworkURL() string {
	return a.networkURL
}

// SubnetworkURL implements NetworkEndpointGroupCloud.
func (a *cloudProviderAdapter) SubnetworkURL() string {
	return a.subnetworkURL
}
