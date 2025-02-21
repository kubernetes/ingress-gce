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
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	api_v1 "k8s.io/api/core/v1"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog/v2"
)

// GroupKey represents a single group for a backend. The implementation of
// this Group could be InstanceGroup or NEG.
type GroupKey struct {
	Zone string
	Name string
}

// Linker is an interface to link backends with their associated groups.
type Linker interface {
	// Link a BackendService to its groups.
	Link(sp utils.ServicePort, groups []GroupKey) error
}

// NEGGetter is an interface to retrieve NEG object
type NEGGetter interface {
	GetNetworkEndpointGroup(name string, zone string, version meta.Version, logger klog.Logger) (*composite.NetworkEndpointGroup, error)
}

// ProbeProvider retrieves a probe struct given a nodePort
type ProbeProvider interface {
	GetProbe(sp utils.ServicePort) (*api_v1.Probe, error)
}
