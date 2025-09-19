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
	"fmt"
	"strings"
	"time"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/filter"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	compute "google.golang.org/api/compute/v1"
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/neg/metrics"
	"k8s.io/ingress-gce/pkg/network"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog/v2"
)

const (
	negServiceName         = "NetworkEndpointGroups"
	listNetworkEndpoints   = "ListNetworkEndpoints"
	attachNetworkEndpoints = "AttachNetworkEndpoints"
	detachNetworkEndpoints = "DetachNetworkEndpoints"
)

// NewAdapter takes a Cloud and returns a NetworkEndpointGroupCloud.
func NewAdapter(g *gce.Cloud, negMetrics *metrics.NegMetrics) NetworkEndpointGroupCloud {
	return NewAdapterWithNetwork(g, g.NetworkURL(), g.SubnetworkURL(), negMetrics)
}

func NewAdapterWithNetwork(g *gce.Cloud, network string, subnetwork string, negMetrics *metrics.NegMetrics) NetworkEndpointGroupCloud {
	return &cloudProviderAdapter{
		c:             g,
		networkURL:    network,
		subnetworkURL: subnetwork,
		negMetrics:    negMetrics,
	}
}

// NewAdapterWithRateLimitSpecs takes a cloud and rate limit specs and returns a NetworkEndpointGroupCloud.
func NewAdapterWithRateLimitSpecs(g *gce.Cloud, specs []string, provider network.CloudNetworkProvider, negMetrics *metrics.NegMetrics) NetworkEndpointGroupCloud {
	strategyKeys := make(map[string]struct{})
	for _, spec := range specs {
		params := strings.Split(spec, ",")
		if params[1] == "strategy" {
			strategyKeys[params[0]] = struct{}{}
		}
	}
	return &cloudProviderAdapter{
		c:             g,
		networkURL:    provider.NetworkURL(),
		subnetworkURL: provider.SubnetworkURL(),
		strategyKeys:  strategyKeys,
		negMetrics:    negMetrics,
	}
}

// cloudProviderAdapter is a temporary shim to consolidate accesses to
// Cloud and push them outside of this package.
type cloudProviderAdapter struct {
	c             *gce.Cloud
	networkURL    string
	subnetworkURL string
	strategyKeys  map[string]struct{}
	negMetrics    *metrics.NegMetrics
}

// GetNetworkEndpointGroup implements NetworkEndpointGroupCloud.
func (a *cloudProviderAdapter) GetNetworkEndpointGroup(name string, zone string, version meta.Version, logger klog.Logger) (*composite.NetworkEndpointGroup, error) {
	start := time.Now()
	neg, err := composite.GetNetworkEndpointGroup(a.c, meta.ZonalKey(name, zone), version, logger)
	a.negMetrics.PublishGCERequestCountMetrics(start, metrics.GetRequest, err)
	return neg, err

}

// ListNetworkEndpointGroup implements NetworkEndpointGroupCloud.
func (a *cloudProviderAdapter) ListNetworkEndpointGroup(zone string, version meta.Version, logger klog.Logger) ([]*composite.NetworkEndpointGroup, error) {
	start := time.Now()
	negs, err := composite.ListNetworkEndpointGroups(a.c, meta.ZonalKey("", zone), version, logger, filter.None)
	a.negMetrics.PublishGCERequestCountMetrics(start, metrics.ListRequest, err)
	return negs, err
}

// AggregatedListNetworkEndpointGroup returns a map of zone -> endpoint group.
func (a *cloudProviderAdapter) AggregatedListNetworkEndpointGroup(version meta.Version, logger klog.Logger) (map[*meta.Key]*composite.NetworkEndpointGroup, error) {
	start := time.Now()
	// TODO: filter for the region the cluster is in.
	negs, err := composite.AggregatedListNetworkEndpointGroup(a.c, version, logger)
	a.negMetrics.PublishGCERequestCountMetrics(start, metrics.AggregatedListRequest, err)
	return negs, err
}

// CreateNetworkEndpointGroup implements NetworkEndpointGroupCloud.
func (a *cloudProviderAdapter) CreateNetworkEndpointGroup(neg *composite.NetworkEndpointGroup, zone string, logger klog.Logger) error {
	start := time.Now()
	err := composite.CreateNetworkEndpointGroup(a.c, meta.ZonalKey(neg.Name, zone), neg, logger)
	a.negMetrics.PublishGCERequestCountMetrics(start, metrics.CreateRequest, err)
	return err
}

// DeleteNetworkEndpointGroup implements NetworkEndpointGroupCloud.
func (a *cloudProviderAdapter) DeleteNetworkEndpointGroup(name string, zone string, version meta.Version, logger klog.Logger) error {
	start := time.Now()
	err := composite.DeleteNetworkEndpointGroup(a.c, meta.ZonalKey(name, zone), version, logger)
	a.negMetrics.PublishGCERequestCountMetrics(start, metrics.DeleteRequest, err)
	return err
}

// AttachNetworkEndpoints implements NetworkEndpointGroupCloud.
func (a cloudProviderAdapter) AttachNetworkEndpoints(name, zone string, endpoints []*composite.NetworkEndpoint, version meta.Version, logger klog.Logger) error {
	req := &composite.NetworkEndpointGroupsAttachEndpointsRequest{NetworkEndpoints: endpoints}
	start := time.Now()
	err := composite.AttachNetworkEndpoints(a.c, meta.ZonalKey(name, zone), version, req, logger)
	a.negMetrics.PublishGCERequestCountMetrics(start, metrics.AttachNERequest, err)
	_, strategyUsed := a.strategyKeys[fmt.Sprintf("%s.%s.%s", version, negServiceName, attachNetworkEndpoints)]
	if utils.IsQuotaExceededError(err) && strategyUsed {
		err = &StrategyQuotaError{Err: err}
	}
	return err
}

// DetachNetworkEndpoints implements NetworkEndpointGroupCloud.
func (a *cloudProviderAdapter) DetachNetworkEndpoints(name, zone string, endpoints []*composite.NetworkEndpoint, version meta.Version, logger klog.Logger) error {
	req := &composite.NetworkEndpointGroupsDetachEndpointsRequest{NetworkEndpoints: endpoints}
	start := time.Now()
	err := composite.DetachNetworkEndpoints(a.c, meta.ZonalKey(name, zone), version, req, logger)
	a.negMetrics.PublishGCERequestCountMetrics(start, metrics.DetachNERequest, err)
	_, strategyUsed := a.strategyKeys[fmt.Sprintf("%s.%s.%s", version, negServiceName, detachNetworkEndpoints)]
	if utils.IsQuotaExceededError(err) && strategyUsed {
		err = &StrategyQuotaError{Err: err}
	}
	return err
}

// ListNetworkEndpoints implements NetworkEndpointGroupCloud.
func (a *cloudProviderAdapter) ListNetworkEndpoints(name, zone string, showHealthStatus bool, version meta.Version, logger klog.Logger) ([]*composite.NetworkEndpointWithHealthStatus, error) {
	healthStatus := "SKIP"
	metricLabel := metrics.ListNERequest
	if showHealthStatus {
		healthStatus = "SHOW"
		metricLabel = metrics.ListNEHealthRequest
	}
	req := &composite.NetworkEndpointGroupsListEndpointsRequest{HealthStatus: healthStatus}
	start := time.Now()
	networkEndpoints, err := composite.ListNetworkEndpoints(a.c, meta.ZonalKey(name, zone), version, req, logger)
	_, strategyUsed := a.strategyKeys[fmt.Sprintf("%s.%s.%s", version, negServiceName, listNetworkEndpoints)]
	if utils.IsQuotaExceededError(err) && strategyUsed {
		err = &StrategyQuotaError{Err: err}
	}
	a.negMetrics.PublishGCERequestCountMetrics(start, metricLabel, err)
	return networkEndpoints, err
}

// NetworkURL implements NetworkEndpointGroupCloud.
func (a *cloudProviderAdapter) NetworkURL() string {
	return a.networkURL
}

// SubnetworkURL implements NetworkEndpointGroupCloud.
func (a *cloudProviderAdapter) SubnetworkURL() string {
	return a.subnetworkURL
}

func (a *cloudProviderAdapter) NetworkProjectID() string {
	return a.c.NetworkProjectID()
}

func (a *cloudProviderAdapter) Region() string {
	return a.c.Region()
}

func (a *cloudProviderAdapter) GetNetwork(networkName string) (*compute.Network, error) {
	return a.c.GetNetwork(networkName)
}
