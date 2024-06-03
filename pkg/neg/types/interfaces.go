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
	"k8s.io/klog/v2"
)

// NetworkEndpointGroupCloud is an interface for managing gce network endpoint group.
type NetworkEndpointGroupCloud interface {
	GetNetworkEndpointGroup(name string, zone string, version meta.Version, logger klog.Logger) (*composite.NetworkEndpointGroup, error)
	ListNetworkEndpointGroup(zone string, version meta.Version, logger klog.Logger) ([]*composite.NetworkEndpointGroup, error)
	AggregatedListNetworkEndpointGroup(version meta.Version, logger klog.Logger) (map[*meta.Key]*composite.NetworkEndpointGroup, error)
	CreateNetworkEndpointGroup(neg *composite.NetworkEndpointGroup, zone string, logger klog.Logger) error
	DeleteNetworkEndpointGroup(name string, zone string, version meta.Version, logger klog.Logger) error
	AttachNetworkEndpoints(name, zone string, endpoints []*composite.NetworkEndpoint, version meta.Version, logger klog.Logger) error
	DetachNetworkEndpoints(name, zone string, endpoints []*composite.NetworkEndpoint, version meta.Version, logger klog.Logger) error
	ListNetworkEndpoints(name, zone string, showHealthStatus bool, version meta.Version, logger klog.Logger) ([]*composite.NetworkEndpointWithHealthStatus, error)
	NetworkURL() string
	SubnetworkURL() string
	NetworkProjectID() string
	Region() string
}

// NetworkEndpointGroupNamer is an interface for generating network endpoint group name.
type NetworkEndpointGroupNamer interface {
	NEG(namespace, name string, port int32) string
	IsNEG(name string) bool
}

// NegSyncer is an interface to interact with syncer
type NegSyncer interface {
	// Start starts the syncer. This call is synchronous. It will return after syncer is started.
	Start() error
	// Stop stops the syncer. This call is asynchronous. It will not block until syncer is stopped.
	Stop()
	// Sync signals the syncer to sync NEG. This call is asynchronous. Syncer will sync once it becomes idle.
	Sync() bool
	// IsStopped returns true if syncer is stopped
	IsStopped() bool
	// IsShuttingDown returns true if syncer is shutting down
	IsShuttingDown() bool
}

// NegSyncerManager is an interface for controllers to manage syncer
type NegSyncerManager interface {
	// EnsureSyncer ensures corresponding syncers are started and stops any unnecessary syncer
	// portMap is a map of ServicePort Port to TargetPort. Returns counts of successful Neg syncers
	// and failed Neg syncer creations
	EnsureSyncers(namespace, name string, portMap PortInfoMap) (int, int, error)
	// StopSyncer stops all syncers related to the service. This call is asynchronous. It will not wait for all syncers to stop.
	StopSyncer(namespace, name string)
	// Sync signals all syncers related to the service to sync. This call is asynchronous.
	Sync(namespace, name string)
	// SyncNodes signals all syncers watching nodes to sync. This call is asynchronous.
	SyncNodes()
	// GC garbage collects network endpoint group and syncers
	GC() error
	// ShutDown shuts down the manager
	ShutDown()
}

type NetworkEndpointsCalculator interface {
	// CalculateEndpoints computes the NEG endpoints based on service endpoints and the current NEG state and returns a
	// map of zone name to network endpoint set
	CalculateEndpoints(eds []EndpointsData, currentMap map[string]NetworkEndpointSet) (map[string]NetworkEndpointSet, EndpointPodMap, int, error)
	// CalculateEndpointsDegradedMode computes the NEG endpoints using degraded mode calculation
	CalculateEndpointsDegradedMode(eds []EndpointsData, currentMap map[string]NetworkEndpointSet) (map[string]NetworkEndpointSet, EndpointPodMap, error)
	// Mode indicates the mode that the EndpointsCalculator is operating in.
	Mode() EndpointsCalculatorMode
	// ValidateEndpoints validates the NEG endpoint information is correct
	ValidateEndpoints(endpointData []EndpointsData, endpointPodMap EndpointPodMap, endpointsExcludedInCalculation int) error
}
