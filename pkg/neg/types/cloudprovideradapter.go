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

package types

import (
	"strings"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/filter"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"google.golang.org/api/compute/v1"
	"k8s.io/klog"
	"k8s.io/legacy-cloud-providers/gce"
)

const (
	// aggregatedListZonalKeyPrefix is the prefix for the zonal key from AggregatedList
	aggregatedListZonalKeyPrefix = "zones"
	// aggregatedListGlobalKey is the global key from AggregatedList
	aggregatedListGlobalKey = "global"
)

// NewAdapter takes a Cloud and returns a NetworkEndpointGroupCloud.
func NewAdapter(g *gce.Cloud) NetworkEndpointGroupCloud {
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
func (a *cloudProviderAdapter) GetNetworkEndpointGroup(name string, zone string) (*compute.NetworkEndpointGroup, error) {
	ctx, cancel := cloud.ContextWithCallTimeout()
	defer cancel()

	return a.c.NetworkEndpointGroups().Get(ctx, meta.ZonalKey(name, zone))
}

// ListNetworkEndpointGroup implements NetworkEndpointGroupCloud.
func (a *cloudProviderAdapter) ListNetworkEndpointGroup(zone string) ([]*compute.NetworkEndpointGroup, error) {
	ctx, cancel := cloud.ContextWithCallTimeout()
	defer cancel()

	return a.c.NetworkEndpointGroups().List(ctx, zone, filter.None)
}

// AggregatedListNetworkEndpointGroup returns a map of zone -> endpoint group.
func (a *cloudProviderAdapter) AggregatedListNetworkEndpointGroup() (map[string][]*compute.NetworkEndpointGroup, error) {
	ctx, cancel := cloud.ContextWithCallTimeout()
	defer cancel()

	// TODO: filter for the region the cluster is in.
	all, err := a.c.NetworkEndpointGroups().AggregatedList(ctx, filter.None)
	if err != nil {
		return nil, err
	}
	ret := map[string][]*compute.NetworkEndpointGroup{}
	for key, byZone := range all {
		// key is scope
		// zonal key is "zones/<zone name>"
		// regional key is "regions/<region name>"
		// global key is "global"
		// TODO: use cloud provider meta.KeyType and scope name as key
		parts := strings.Split(key, "/")
		if len(parts) == 1 && parts[0] == aggregatedListGlobalKey {
			klog.V(4).Infof("Ignoring key %q as it is global", key)
			continue
		}
		if len(parts) != 2 || parts[0] != aggregatedListZonalKeyPrefix {
			klog.Warningf("Key %q is not in a known format, ignoring", key)
			continue
		}
		zone := parts[1]
		ret[zone] = append(ret[zone], byZone...)
	}
	return ret, nil
}

// CreateNetworkEndpointGroup implements NetworkEndpointGroupCloud.
func (a *cloudProviderAdapter) CreateNetworkEndpointGroup(neg *compute.NetworkEndpointGroup, zone string) error {
	ctx, cancel := cloud.ContextWithCallTimeout()
	defer cancel()

	return a.c.NetworkEndpointGroups().Insert(ctx, meta.ZonalKey(neg.Name, zone), neg)
}

// DeleteNetworkEndpointGroup implements NetworkEndpointGroupCloud.
func (a *cloudProviderAdapter) DeleteNetworkEndpointGroup(name string, zone string) error {
	ctx, cancel := cloud.ContextWithCallTimeout()
	defer cancel()

	return a.c.NetworkEndpointGroups().Delete(ctx, meta.ZonalKey(name, zone))
}

// AttachNetworkEndpoints implements NetworkEndpointGroupCloud.
func (a cloudProviderAdapter) AttachNetworkEndpoints(name, zone string, endpoints []*compute.NetworkEndpoint) error {
	ctx, cancel := cloud.ContextWithCallTimeout()
	defer cancel()

	req := &compute.NetworkEndpointGroupsAttachEndpointsRequest{NetworkEndpoints: endpoints}
	return a.c.NetworkEndpointGroups().AttachNetworkEndpoints(ctx, meta.ZonalKey(name, zone), req)
}

// DetachNetworkEndpoints implements NetworkEndpointGroupCloud.
func (a *cloudProviderAdapter) DetachNetworkEndpoints(name, zone string, endpoints []*compute.NetworkEndpoint) error {
	ctx, cancel := cloud.ContextWithCallTimeout()
	defer cancel()

	req := &compute.NetworkEndpointGroupsDetachEndpointsRequest{NetworkEndpoints: endpoints}
	return a.c.NetworkEndpointGroups().DetachNetworkEndpoints(ctx, meta.ZonalKey(name, zone), req)
}

// ListNetworkEndpoints implements NetworkEndpointGroupCloud.
func (a *cloudProviderAdapter) ListNetworkEndpoints(name, zone string, showHealthStatus bool) ([]*compute.NetworkEndpointWithHealthStatus, error) {
	ctx, cancel := cloud.ContextWithCallTimeout()
	defer cancel()

	healthStatus := "SKIP"
	if showHealthStatus {
		healthStatus = "SHOW"
	}
	req := &compute.NetworkEndpointGroupsListEndpointsRequest{HealthStatus: healthStatus}
	return a.c.NetworkEndpointGroups().ListNetworkEndpoints(ctx, meta.ZonalKey(name, zone), req, filter.None)
}

// NetworkURL implements NetworkEndpointGroupCloud.
func (a *cloudProviderAdapter) NetworkURL() string {
	return a.networkURL
}

// SubnetworkURL implements NetworkEndpointGroupCloud.
func (a *cloudProviderAdapter) SubnetworkURL() string {
	return a.subnetworkURL
}
