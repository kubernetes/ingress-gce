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
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/filter"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"google.golang.org/api/compute/v1"
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/ingress-gce/pkg/backends/features"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/klog/v2"
	"net/http"
)

// Pool handles CRUD operations on a pool of GCE Backend Services.
type Pool struct {
	cloud *gce.Cloud
	namer namer.BackendNamer
}

// NewPool returns a new backend pool.
// - cloud: implements BackendServices
// - namer: produces names for backends.
func NewPool(cloud *gce.Cloud, namer namer.BackendNamer) *Pool {
	return &Pool{
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

// Create a composite BackendService and returns it.
func (p *Pool) Create(sp utils.ServicePort, hcLink string, beLogger klog.Logger) (*composite.BackendService, error) {
	name := sp.BackendName()
	namedPort := &compute.NamedPort{
		Name: p.namer.NamedPort(sp.NodePort),
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
		// LogConfig is using GA API so this is not considered for computing API version.
		LogConfig: &composite.BackendServiceLogConfig{
			Enable: true,
			// Sampling rate needs to be specified explicitly.
			SampleRate: 1.0,
		},
	}

	if sp.L7ILBEnabled {
		// This enables l7-ILB and advanced traffic management features
		be.LoadBalancingScheme = "INTERNAL_MANAGED"
	} else if sp.L7XLBRegionalEnabled {
		be.LoadBalancingScheme = "EXTERNAL_MANAGED"
	}

	ensureDescription(be, &sp)
	scope := features.ScopeFromServicePort(&sp)
	key, err := composite.CreateKey(p.cloud, name, scope)
	if err != nil {
		return nil, err
	}

	if err := composite.CreateBackendService(p.cloud, key, be, beLogger); err != nil {
		return nil, err
	}
	// Note: We need to perform a GCE call to re-fetch the object we just created
	// so that the "Fingerprint" field is filled in. This is needed to update the
	// object without error.
	return p.Get(name, version, scope, beLogger)
}

// Update a BackendService given the composite type.
func (p *Pool) Update(be *composite.BackendService, beLogger klog.Logger) error {
	// Ensure the backend service has the proper version before updating.
	be.Version = features.VersionFromDescription(be.Description)
	scope, err := composite.ScopeFromSelfLink(be.SelfLink)
	if err != nil {
		return err
	}

	key, err := composite.CreateKey(p.cloud, be.Name, scope)
	if err != nil {
		return err
	}
	if err := composite.UpdateBackendService(p.cloud, key, be, beLogger); err != nil {
		return err
	}
	return nil
}

// Get a composite BackendService given a required version.
func (p *Pool) Get(name string, version meta.Version, scope meta.KeyType, beLogger klog.Logger) (*composite.BackendService, error) {
	key, err := composite.CreateKey(p.cloud, name, scope)
	if err != nil {
		return nil, err
	}
	be, err := composite.GetBackendService(p.cloud, key, version, beLogger)
	if err != nil {
		return nil, err
	}
	// Evaluate the existing features from description to see if a lower
	// API version is required so that we don't lose information from
	// the existing backend service.
	versionRequired := features.VersionFromDescription(be.Description)

	if features.IsLowerVersion(versionRequired, version) {
		be, err = composite.GetBackendService(p.cloud, key, versionRequired, beLogger)
		if err != nil {
			return nil, err
		}
	}
	return be, nil
}

// Delete a BackendService given its name.
func (p *Pool) Delete(name string, version meta.Version, scope meta.KeyType, beLogger klog.Logger) error {
	beLogger.Info("Deleting backend service")

	key, err := composite.CreateKey(p.cloud, name, scope)
	if err != nil {
		return err
	}
	beLogger = beLogger.WithValues("backendKey", key)
	err = composite.DeleteBackendService(p.cloud, key, version, beLogger)
	if err != nil {
		if utils.IsHTTPErrorCode(err, http.StatusNotFound) || utils.IsInUsedByError(err) {
			// key also contains region information.
			beLogger.Info("DeleteBackendService(): ignorable error", "err", err)
			return nil
		}
		beLogger.Error(err, "DeleteBackendService()")
		return err
	}
	beLogger.Info("DeleteBackendService() ok")
	return nil
}

// Health checks the health of a BackendService given its name.
// Returns ("HEALTHY", nil) if healthy, otherwise ("Unknown", err)
func (p *Pool) Health(name string, version meta.Version, scope meta.KeyType, beLogger klog.Logger) (string, error) {
	be, err := p.Get(name, version, scope, beLogger)
	if err != nil {
		return "Unknown", fmt.Errorf("error getting backend service %s: %w", name, err)
	}
	if len(be.Backends) == 0 {
		return "Unknown", fmt.Errorf("no backends found for backend service %q", name)
	}

	// TODO: Include port, ip in the status, since it's in the health info.
	// TODO (shance) convert to composite types
	ret := "Unknown"
	for _, backend := range be.Backends {
		var hs *compute.BackendServiceGroupHealth
		switch scope {
		case meta.Global:
			hs, err = p.cloud.GetGlobalBackendServiceHealth(name, backend.Group)
		case meta.Regional:
			hs, err = p.cloud.GetRegionalBackendServiceHealth(name, p.cloud.Region(), backend.Group)
		default:
			return "Unknown", fmt.Errorf("invalid scope for Health(): %s", scope)
		}

		if err != nil {
			return "Unknown", fmt.Errorf("error getting health for backend %q: %w", name, err)
		}
		if len(hs.HealthStatus) == 0 || hs.HealthStatus[0] == nil {
			beLogger.Info("backend service does not have health status", "healthStatus", hs.HealthStatus)
			continue
		}

		for _, instanceStatus := range hs.HealthStatus {
			ret = instanceStatus.HealthState
			// return immediately with the value if we found at least one healthy instance
			if ret == "HEALTHY" {
				return ret, nil
			}
		}
	}
	return ret, nil
}

// List BackendService names that are managed by this pool.
func (p *Pool) List(key *meta.Key, version meta.Version, beLogger klog.Logger) ([]*composite.BackendService, error) {
	// TODO: for consistency with the rest of this sub-package this method
	// should return a list of backend ports.
	var backends []*composite.BackendService
	var err error

	backends, err = composite.ListBackendServices(p.cloud, key, version, beLogger, filter.None)
	if err != nil {
		return nil, err
	}

	var clusterBackends []*composite.BackendService

	for _, bs := range backends {
		if p.namer.NameBelongsToEntity(bs.Name) {
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

// AddSignedURLKey adds a SignedUrlKey to a BackendService
func (p *Pool) AddSignedURLKey(be *composite.BackendService, signedurlkey *composite.SignedUrlKey, urlKeyLogger klog.Logger) error {
	urlKeyLogger.Info("Adding SignedUrlKey")

	scope, err := composite.ScopeFromSelfLink(be.SelfLink)
	if err != nil {
		return err
	}

	key, err := composite.CreateKey(p.cloud, be.Name, scope)
	if err != nil {
		return err
	}
	if err := composite.AddSignedUrlKey(p.cloud, key, be, signedurlkey, urlKeyLogger); err != nil {
		return err
	}
	return nil
}

// DeleteSignedURLKey deletes a SignedUrlKey from BackendService
func (p *Pool) DeleteSignedURLKey(be *composite.BackendService, keyName string, urlKeyLogger klog.Logger) error {
	urlKeyLogger.Info("Deleting SignedUrlKey")

	scope, err := composite.ScopeFromSelfLink(be.SelfLink)
	if err != nil {
		return err
	}

	key, err := composite.CreateKey(p.cloud, be.Name, scope)
	if err != nil {
		return err
	}
	if err := composite.DeleteSignedUrlKey(p.cloud, key, be, keyName, urlKeyLogger); err != nil {
		return err
	}
	return nil
}
