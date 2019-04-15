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

package backends

import (
	"net/http"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	compute "google.golang.org/api/compute/v1"
	"k8s.io/ingress-gce/pkg/backends/features"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog"
	"k8s.io/kubernetes/pkg/cloudprovider/providers/gce"
)

// Backends handles CRUD operations for backends.
type Backends struct {
	cloud *gce.Cloud
	namer *utils.Namer
}

// Backends is a Pool.
var _ Pool = (*Backends)(nil)

// NewPool returns a new backend pool.
// - cloud: implements BackendServices
// - namer: procudes names for backends.
func NewPool(cloud *gce.Cloud, namer *utils.Namer) *Backends {
	return &Backends{
		cloud: cloud,
		namer: namer,
	}
}

// ensureDescription updates the BackendService Description with the expected value
func ensureDescription(be *composite.BackendService, sp *utils.ServicePort) (needsUpdate bool) {
	desc := sp.GetDescription()
	features.SetDescription(&desc, sp)
	descString := desc.String()
	if be.Description == descString {
		return false
	}
	be.Description = descString
	return true
}

// Create implements Pool.
func (b *Backends) Create(sp utils.ServicePort, hcLink string) (*composite.BackendService, error) {
	name := sp.BackendName(b.namer)
	namedPort := &compute.NamedPort{
		Name: b.namer.NamedPort(sp.NodePort),
		Port: sp.NodePort,
	}

	version := features.VersionFromServicePort(&sp)
	be := &composite.BackendService{
		Version:      version,
		Name:         name,
		Protocol:     string(sp.Protocol),
		Port:         namedPort.Port,
		PortName:     namedPort.Name,
		HealthChecks: []string{hcLink},
	}
	ensureDescription(be, &sp)
	if err := composite.CreateBackendService(be, b.cloud); err != nil {
		return nil, err
	}
	// Note: We need to perform a GCE call to re-fetch the object we just created
	// so that the "Fingerprint" field is filled in. This is needed to update the
	// object without error.
	return b.Get(name, version)
}

// Update implements Pool.
func (b *Backends) Update(be *composite.BackendService) error {
	// Ensure the backend service has the proper version before updating.
	be.Version = features.VersionFromDescription(be.Description)
	if err := composite.UpdateBackendService(be, b.cloud); err != nil {
		return err
	}
	return nil
}

// Get implements Pool.
func (b *Backends) Get(name string, version meta.Version) (*composite.BackendService, error) {
	be, err := composite.GetBackendService(name, version, b.cloud)
	if err != nil {
		return nil, err
	}
	// Evaluate the existing features from description to see if a lower
	// API version is required so that we don't lose information from
	// the existing backend service.
	versionRequired := features.VersionFromDescription(be.Description)
	if features.IsLowerVersion(versionRequired, version) {
		be, err = composite.GetBackendService(name, versionRequired, b.cloud)
		if err != nil {
			return nil, err
		}
	}
	return be, nil
}

// Delete implements Pool.
func (b *Backends) Delete(name string) (err error) {
	defer func() {
		if utils.IsHTTPErrorCode(err, http.StatusNotFound) {
			err = nil
		}
	}()

	klog.V(2).Infof("Deleting backend service %v", name)

	// Try deleting health checks even if a backend is not found.
	if err = b.cloud.DeleteGlobalBackendService(name); err != nil && !utils.IsHTTPErrorCode(err, http.StatusNotFound) {
		return err
	}
	return
}

// Health implements Pool.
func (b *Backends) Health(name string) string {
	be, err := b.Get(name, meta.VersionGA)
	if err != nil || len(be.Backends) == 0 {
		return "Unknown"
	}

	// TODO: Look at more than one backend's status
	// TODO: Include port, ip in the status, since it's in the health info.
	hs, err := b.cloud.GetGlobalBackendServiceHealth(name, be.Backends[0].Group)
	if err != nil || len(hs.HealthStatus) == 0 || hs.HealthStatus[0] == nil {
		return "Unknown"
	}
	// TODO: State transition are important, not just the latest.
	return hs.HealthStatus[0].HealthState
}

// List lists all backends managed by this controller.
func (b *Backends) List() ([]string, error) {
	// TODO: for consistency with the rest of this sub-package this method
	// should return a list of backend ports.
	backends, err := b.cloud.ListGlobalBackendServices()
	if err != nil {
		return nil, err
	}

	var names []string

	for _, bs := range backends {
		if b.namer.NameBelongsToCluster(bs.Name) {
			names = append(names, bs.Name)
		}
	}
	return names, nil
}
