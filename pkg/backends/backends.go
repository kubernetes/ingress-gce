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

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"google.golang.org/api/compute/v1"
	"k8s.io/ingress-gce/pkg/backends/features"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/klog"
	"k8s.io/legacy-cloud-providers/gce"
)

// Backends handles CRUD operations for backends.
type Backends struct {
	cloud *gce.Cloud
	namer *namer.Namer
}

// Backends is a Pool.
var _ Pool = (*Backends)(nil)

// NewPool returns a new backend pool.
// - cloud: implements BackendServices
// - namer: produces names for backends.
func NewPool(cloud *gce.Cloud, namer *namer.Namer) *Backends {
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

	if sp.L7ILBEnabled {
		be.LoadBalancingScheme = "INTERNAL"
	}

	ensureDescription(be, &sp)
	scope := features.ScopeFromServicePort(&sp)
	key, err := composite.CreateKey(b.cloud, name, scope)
	if err != nil {
		return nil, err
	}

	if err := composite.CreateBackendService(b.cloud, key, be); err != nil {
		return nil, err
	}
	// Note: We need to perform a GCE call to re-fetch the object we just created
	// so that the "Fingerprint" field is filled in. This is needed to update the
	// object without error.
	return b.Get(name, version, scope)
}

// Update implements Pool.
func (b *Backends) Update(be *composite.BackendService) error {
	// Ensure the backend service has the proper version before updating.
	be.Version = features.VersionFromDescription(be.Description)
	scope, err := composite.ScopeFromSelfLink(be.SelfLink)
	if err != nil {
		return err
	}

	key, err := composite.CreateKey(b.cloud, be.Name, scope)
	if err != nil {
		return err
	}
	if err := composite.UpdateBackendService(b.cloud, key, be); err != nil {
		return err
	}
	return nil
}

// Get implements Pool.
func (b *Backends) Get(name string, version meta.Version, scope meta.KeyType) (*composite.BackendService, error) {
	key, err := composite.CreateKey(b.cloud, name, scope)
	if err != nil {
		return nil, err
	}
	be, err := composite.GetBackendService(b.cloud, key, version)
	if err != nil {
		return nil, err
	}
	// Evaluate the existing features from description to see if a lower
	// API version is required so that we don't lose information from
	// the existing backend service.
	versionRequired := features.VersionFromDescription(be.Description)

	if features.IsLowerVersion(versionRequired, version) {
		be, err = composite.GetBackendService(b.cloud, key, versionRequired)
		if err != nil {
			return nil, err
		}
	}
	return be, nil
}

// Delete implements Pool.
func (b *Backends) Delete(name string, version meta.Version, scope meta.KeyType) error {
	klog.V(2).Infof("Deleting backend service %v", name)

	key, err := composite.CreateKey(b.cloud, name, scope)
	if err != nil {
		return err
	}
	err = composite.DeleteBackendService(b.cloud, key, version)
	if err != nil {
		if utils.IsHTTPErrorCode(err, http.StatusNotFound) || utils.IsInUsedByError(err) {
			klog.Infof("DeleteBackendService(_, %v, %v) = %v; ignorable error", key, version, err)
			return nil
		}
		klog.Errorf("DeleteBackendService(_, %v, %v) = %v", key, version, err)
		return err
	}
	klog.V(2).Infof("DeleteBackendService(_, %v, %v) ok", key, version)
	return nil
}

// Health implements Pool.
func (b *Backends) Health(name string, version meta.Version, scope meta.KeyType) (string, error) {
	be, err := b.Get(name, version, scope)
	if err != nil || len(be.Backends) == 0 {
		return "Unknown", fmt.Errorf("error getting health for backend %s: %v", name, err)
	}

	// TODO: Look at more than one backend's status
	// TODO: Include port, ip in the status, since it's in the health info.
	// TODO (shance) convert to composite types
	var hs *compute.BackendServiceGroupHealth
	switch scope {
	case meta.Global:
		hs, err = b.cloud.GetGlobalBackendServiceHealth(name, be.Backends[0].Group)
	case meta.Regional:
		hs, err = b.cloud.GetRegionalBackendServiceHealth(name, b.cloud.Region(), be.Backends[0].Group)
	default:
		return "Unknown", fmt.Errorf("invalid scope for Health(): %s", scope)
	}

	if err != nil || len(hs.HealthStatus) == 0 || hs.HealthStatus[0] == nil {
		return "Unknown", fmt.Errorf("error getting health for backend %q: %v", name, err)
	}
	// TODO: State transition are important, not just the latest.
	return hs.HealthStatus[0].HealthState, nil
}

// List lists all backends managed by this controller.
func (b *Backends) List(key *meta.Key, version meta.Version) ([]*composite.BackendService, error) {
	// TODO: for consistency with the rest of this sub-package this method
	// should return a list of backend ports.
	var backends []*composite.BackendService
	var err error

	backends, err = composite.ListBackendServices(b.cloud, key, version)
	if err != nil {
		return nil, err
	}

	var clusterBackends []*composite.BackendService

	for _, bs := range backends {
		if b.namer.NameBelongsToCluster(bs.Name) {
			scope, err := composite.ScopeFromSelfLink(bs.SelfLink)
			if err != nil {
				return nil, err
			}
			bs.Scope = scope

			clusterBackends = append(clusterBackends, bs)
		}
	}
	return clusterBackends, nil
}
