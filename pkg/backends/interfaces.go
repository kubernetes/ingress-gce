/*
Copyright 2015 The Kubernetes Authors.

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

package backends

import (
	computealpha "google.golang.org/api/compute/v0.alpha"
	compute "google.golang.org/api/compute/v1"
	api_v1 "k8s.io/api/core/v1"
	"k8s.io/ingress-gce/pkg/utils"
)

// ProbeProvider retrieves a probe struct given a nodePort
type ProbeProvider interface {
	GetProbe(sp utils.ServicePort) (*api_v1.Probe, error)
}

// BackendPool is an interface to manage a pool of kubernetes nodePort services
// as gce backendServices, and sync them through the BackendServices interface.
type BackendPool interface {
	Init(p ProbeProvider)
	Ensure(ports []utils.ServicePort, igs []*compute.InstanceGroup) error
	Get(port int64, isAlpha bool) (*BackendService, error)
	Delete(port int64) error
	GC(ports []utils.ServicePort) error
	Shutdown() error
	Status(name string) string
	List() ([]interface{}, error)
	Link(port utils.ServicePort, zones []string) error
}

// BackendServices is an interface for managing gce backend services.
type BackendServices interface {
	GetGlobalBackendService(name string) (*compute.BackendService, error)
	GetAlphaGlobalBackendService(name string) (*computealpha.BackendService, error)
	UpdateGlobalBackendService(bg *compute.BackendService) error
	UpdateAlphaGlobalBackendService(bg *computealpha.BackendService) error
	CreateGlobalBackendService(bg *compute.BackendService) error
	CreateAlphaGlobalBackendService(bg *computealpha.BackendService) error
	DeleteGlobalBackendService(name string) error
	ListGlobalBackendServices() ([]*compute.BackendService, error)
	GetGlobalBackendServiceHealth(name, instanceGroupLink string) (*compute.BackendServiceGroupHealth, error)
}

// NEGGetter is an interface to retrieve NEG object
type NEGGetter interface {
	GetNetworkEndpointGroup(name string, zone string) (*computealpha.NetworkEndpointGroup, error)
}
