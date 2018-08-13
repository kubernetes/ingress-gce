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
	"fmt"
	"net/http"
	"time"

	"github.com/golang/glog"
	compute "google.golang.org/api/compute/v1"
	"k8s.io/ingress-gce/pkg/backends/features"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/storage"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/kubernetes/pkg/cloudprovider/providers/gce"
	"k8s.io/kubernetes/pkg/cloudprovider/providers/gce/cloud/meta"
)

// Backends handles CRUD operations for backends.
type Backends struct {
	cloud       *gce.GCECloud
	snapshotter storage.Snapshotter
	namer       *utils.Namer
}

// Backends is a Pool.
var _ Pool = (*Backends)(nil)

// NewPool returns a new backend pool.
// - cloud: implements BackendServices
// - namer: procudes names for backends.
// - resyncWithCloud: if true, periodically syncs with cloud resources.
func NewPool(
	cloud *gce.GCECloud,
	namer *utils.Namer,
	resyncWithCloud bool) *Backends {

	backendPool := &Backends{
		cloud: cloud,
		namer: namer,
	}
	if !resyncWithCloud {
		backendPool.snapshotter = storage.NewInMemoryPool()
		return backendPool
	}
	keyFunc := func(i interface{}) (string, error) {
		bs := i.(*compute.BackendService)
		if !namer.NameBelongsToCluster(bs.Name) {
			return "", fmt.Errorf("unrecognized name %v", bs.Name)
		}
		return bs.Name, nil
	}
	backendPool.snapshotter = storage.NewCloudListingPool("backends", keyFunc, backendPool, 30*time.Second)
	return backendPool
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
func (b *Backends) Create(sp utils.ServicePort) (*composite.BackendService, error) {
	name := sp.BackendName(b.namer)
	namedPort := &compute.NamedPort{
		Name: b.namer.NamedPort(sp.NodePort),
		Port: sp.NodePort,
	}

	version := features.VersionFromServicePort(&sp)
	be := &composite.BackendService{
		Version:  version,
		Name:     name,
		Protocol: string(sp.Protocol),
		Port:     namedPort.Port,
		PortName: namedPort.Name,
	}
	ensureDescription(be, &sp)
	if err := composite.CreateBackendService(be, b.cloud); err != nil {
		return nil, err
	}
	b.snapshotter.Add(name, be)
	// Note: We need to perform a GCE call to re-fetch the object we just created
	// so that the "Fingerprint" field is filled in. This is needed to update the
	// object without error.
	return b.Get(name, version)
}

// Update implements Pool.
func (b *Backends) Update(be *composite.BackendService) error {
	if err := composite.UpdateBackendService(be, b.cloud); err != nil {
		return err
	}
	b.snapshotter.Add(be.Name, be)
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
	b.snapshotter.Add(name, be)
	return be, nil
}

// Delete implements Pool.
func (b *Backends) Delete(name string) (err error) {
	defer func() {
		if utils.IsHTTPErrorCode(err, http.StatusNotFound) {
			err = nil
		}
		if err == nil {
			b.snapshotter.Delete(name)
		}
	}()

	glog.V(2).Infof("Deleting backend service %v", name)

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

// GetLocalSnapshot implements Pool.
func (b *Backends) GetLocalSnapshot() []string {
	pool := b.snapshotter.Snapshot()
	var keys []string
	for name := range pool {
		keys = append(keys, name)
	}
	return keys
}

// List lists all backends.
func (b *Backends) List() ([]interface{}, error) {
	// TODO: for consistency with the rest of this sub-package this method
	// should return a list of backend ports.
	backends, err := b.cloud.ListGlobalBackendServices()
	if err != nil {
		return nil, err
	}
	var ret []interface{}
	for _, x := range backends {
		ret = append(ret, x)
	}
	return ret, nil
}
