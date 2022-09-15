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
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"k8s.io/ingress-gce/pkg/composite"
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
		c:             g,
		networkURL:    g.NetworkURL(),
		subnetworkURL: g.SubnetworkURL(),
	}
}

func NewAdapterWithNetwork(g *gce.Cloud, network, subnetwork string) NetworkEndpointGroupCloud {
	return &cloudProviderAdapter{
		c:             g,
		networkURL:    network,
		subnetworkURL: subnetwork,
	}
}

// cloudProviderAdapter is a temporary shim to consolidate accesses to
// Cloud and push them outside of this package.
type cloudProviderAdapter struct {
	c             *gce.Cloud
	networkURL    string
	subnetworkURL string
}

// GetNetworkEndpointGroup implements NetworkEndpointGroupCloud.
func (a *cloudProviderAdapter) GetNetworkEndpointGroup(name string, zone string, version meta.Version) (*composite.NetworkEndpointGroup, error) {
	return composite.GetNetworkEndpointGroup(a.c, meta.ZonalKey(name, zone), version)

}

// ListNetworkEndpointGroup implements NetworkEndpointGroupCloud.
func (a *cloudProviderAdapter) ListNetworkEndpointGroup(zone string, version meta.Version) ([]*composite.NetworkEndpointGroup, error) {
	return composite.ListNetworkEndpointGroups(a.c, meta.ZonalKey("", zone), version)
}

// AggregatedListNetworkEndpointGroup returns a map of zone -> endpoint group.
func (a *cloudProviderAdapter) AggregatedListNetworkEndpointGroup(version meta.Version) (map[*meta.Key]*composite.NetworkEndpointGroup, error) {
	// TODO: filter for the region the cluster is in.
	return composite.AggregatedListNetworkEndpointGroup(a.c, version)
}

// CreateNetworkEndpointGroup implements NetworkEndpointGroupCloud.
func (a *cloudProviderAdapter) CreateNetworkEndpointGroup(neg *composite.NetworkEndpointGroup, zone string) error {
	return composite.CreateNetworkEndpointGroup(a.c, meta.ZonalKey(neg.Name, zone), neg)
}

// DeleteNetworkEndpointGroup implements NetworkEndpointGroupCloud.
func (a *cloudProviderAdapter) DeleteNetworkEndpointGroup(name string, zone string, version meta.Version) error {
	return composite.DeleteNetworkEndpointGroup(a.c, meta.ZonalKey(name, zone), version)
}

// AttachNetworkEndpoints implements NetworkEndpointGroupCloud.
func (a cloudProviderAdapter) AttachNetworkEndpoints(name, zone string, endpoints []*composite.NetworkEndpoint, version meta.Version) error {
	req := &composite.NetworkEndpointGroupsAttachEndpointsRequest{NetworkEndpoints: endpoints}
	return composite.AttachNetworkEndpoints(a.c, meta.ZonalKey(name, zone), version, req)
}

// DetachNetworkEndpoints implements NetworkEndpointGroupCloud.
func (a *cloudProviderAdapter) DetachNetworkEndpoints(name, zone string, endpoints []*composite.NetworkEndpoint, version meta.Version) error {
	req := &composite.NetworkEndpointGroupsDetachEndpointsRequest{NetworkEndpoints: endpoints}
	return composite.DetachNetworkEndpoints(a.c, meta.ZonalKey(name, zone), version, req)
}

// ListNetworkEndpoints implements NetworkEndpointGroupCloud.
func (a *cloudProviderAdapter) ListNetworkEndpoints(name, zone string, showHealthStatus bool, version meta.Version) ([]*composite.NetworkEndpointWithHealthStatus, error) {
	healthStatus := "SKIP"
	if showHealthStatus {
		healthStatus = "SHOW"
	}
	req := &composite.NetworkEndpointGroupsListEndpointsRequest{HealthStatus: healthStatus}
	return composite.ListNetworkEndpoints(a.c, meta.ZonalKey(name, zone), version, req)
}

// NetworkURL implements NetworkEndpointGroupCloud.
func (a *cloudProviderAdapter) NetworkURL() string {
	return a.networkURL
}

// SubnetworkURL implements NetworkEndpointGroupCloud.
func (a *cloudProviderAdapter) SubnetworkURL() string {
	return a.subnetworkURL
}
