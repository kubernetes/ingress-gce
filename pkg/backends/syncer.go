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
	"strings"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/ingress-gce/pkg/backends/features"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/healthchecks"
	lbfeatures "k8s.io/ingress-gce/pkg/loadbalancers/features"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog"
	"k8s.io/legacy-cloud-providers/gce"
)

// backendSyncer manages the lifecycle of backends.
type backendSyncer struct {
	backendPool   Pool
	healthChecker healthchecks.HealthChecker
	prober        ProbeProvider
	cloud         *gce.Cloud
}

// backendSyncer is a Syncer
var _ Syncer = (*backendSyncer)(nil)

func NewBackendSyncer(
	backendPool Pool,
	healthChecker healthchecks.HealthChecker,
	cloud *gce.Cloud) Syncer {
	return &backendSyncer{
		backendPool:   backendPool,
		healthChecker: healthChecker,
		cloud:         cloud,
	}
}

// Init implements Syncer.
func (s *backendSyncer) Init(pp ProbeProvider) {
	s.prober = pp
}

// Sync implements Syncer.
func (s *backendSyncer) Sync(svcPorts []utils.ServicePort) error {
	for _, sp := range svcPorts {
		klog.V(3).Infof("Sync: backend %+v", sp)
		if err := s.ensureBackendService(sp); err != nil {
			return err
		}
	}
	return nil
}

// ensureBackendService will update or create a BackendService for the given port.
func (s *backendSyncer) ensureBackendService(sp utils.ServicePort) error {
	// We must track the ports even if creating the backends failed, because
	// we might've created health-check for them.
	be := &composite.BackendService{}
	beName := sp.BackendName()
	version := features.VersionFromServicePort(&sp)
	scope := features.ScopeFromServicePort(&sp)

	be, getErr := s.backendPool.Get(beName, version, scope)

	// Ensure health check for backend service exists.
	hcLink, err := s.ensureHealthCheck(sp)
	if err != nil {
		return fmt.Errorf("error ensuring health check: %w", err)
	}

	// Verify existence of a backend service for the proper port
	// but do not specify any backends for it (IG / NEG).
	if getErr != nil {
		if !utils.IsNotFoundError(getErr) {
			return getErr
		}
		// Only create the backend service if the error was 404.
		klog.V(2).Infof("Creating backend service for port %v named %v", sp.NodePort, beName)
		be, err = s.backendPool.Create(sp, hcLink)
		if err != nil {
			return err
		}
	}

	needUpdate := ensureProtocol(be, sp)
	needUpdate = ensureHealthCheckLink(be, hcLink) || needUpdate
	needUpdate = ensureDescription(be, &sp) || needUpdate
	if sp.BackendConfig != nil {
		needUpdate = features.EnsureCDN(sp, be) || needUpdate
		needUpdate = features.EnsureIAP(sp, be) || needUpdate
		needUpdate = features.EnsureTimeout(sp, be) || needUpdate
		needUpdate = features.EnsureDraining(sp, be) || needUpdate
		needUpdate = features.EnsureAffinity(sp, be) || needUpdate
		needUpdate = features.EnsureCustomRequestHeaders(sp, be) || needUpdate
		needUpdate = features.EnsureLogging(sp, be) || needUpdate
	}

	if needUpdate {
		if err := s.backendPool.Update(be); err != nil {
			return err
		}
	}

	if sp.BackendConfig != nil {
		// Scope is used to validate if cloud armor security policy feature is
		// available. meta.Key is not needed as security policy supported only for
		// global backends.
		be.Scope = scope
		if err := features.EnsureSecurityPolicy(s.cloud, sp, be); err != nil {
			return err
		}
	}

	return nil
}

// GC implements Syncer.
func (s *backendSyncer) GC(svcPorts []utils.ServicePort) error {
	knownPorts, err := knownPortsFromServicePorts(s.cloud, svcPorts)
	if err != nil {
		return err
	}

	// TODO(shance): Refactor out empty key field
	key, err := composite.CreateKey(s.cloud, "", meta.Regional)
	if err != nil {
		return fmt.Errorf("error creating l7 ilb key: %w", err)
	}
	ilbBackends, err := s.backendPool.List(key, lbfeatures.L7ILBVersions().BackendService)
	if err != nil {
		return fmt.Errorf("error listing regional backends: %w", err)
	}
	err = s.gc(ilbBackends, knownPorts)
	if err != nil {
		return fmt.Errorf("error GCing regional Backends: %w", err)
	}

	// Requires an empty name field until it is refactored out
	key, err = composite.CreateKey(s.cloud, "", meta.Global)
	if err != nil {
		return fmt.Errorf("error creating l7 ilb key: %w", err)
	}
	backends, err := s.backendPool.List(key, meta.VersionGA)
	if err != nil {
		return fmt.Errorf("error listing backends: %w", err)
	}
	err = s.gc(backends, knownPorts)
	if err != nil {
		return fmt.Errorf("error GCing Backends: %w", err)
	}

	return nil
}

// gc deletes the provided backends
func (s *backendSyncer) gc(backends []*composite.BackendService, knownPorts sets.String) error {
	for _, be := range backends {
		// Skip L4 LB backend services
		// backendSyncer currently only GC backend services for L7 XLB/ILB.
		// L4 LB is GC as part of the deletion flow as there is no shared backend services among L4 ILBs.
		if strings.Contains(be.Description, utils.L4ILBServiceDescKey) {
			continue
		}
		var key *meta.Key
		name := be.Name
		scope, err := composite.ScopeFromSelfLink(be.SelfLink)
		if err != nil {
			return err
		}
		if key, err = composite.CreateKey(s.cloud, name, scope); err != nil {
			return err
		}
		if knownPorts.Has(key.String()) {
			continue
		}
		klog.V(2).Infof("GCing backendService for port %s", name)
		err = s.backendPool.Delete(name, be.Version, scope)
		if err != nil {
			klog.Errorf("backendPool.Delete(%v, %v, %v) = %v", name, be.Version, scope, err)
			return err
		}

		if err := s.healthChecker.Delete(name, scope); err != nil {
			return err
		}
	}
	return nil
}

// TODO: (shance) add unit tests
func knownPortsFromServicePorts(cloud *gce.Cloud, svcPorts []utils.ServicePort) (sets.String, error) {
	knownPorts := sets.NewString()

	for _, sp := range svcPorts {
		name := sp.BackendName()
		key, err := composite.CreateKey(cloud, name, features.ScopeFromServicePort(&sp))
		if err != nil {
			return nil, err
		}
		knownPorts.Insert(key.String())

	}

	return knownPorts, nil
}

// Status implements Syncer.
func (s *backendSyncer) Status(name string, version meta.Version, scope meta.KeyType) (string, error) {
	return s.backendPool.Health(name, version, scope)
}

// Shutdown implements Syncer.
func (s *backendSyncer) Shutdown() error {
	if err := s.GC([]utils.ServicePort{}); err != nil {
		return err
	}
	return nil
}

func (s *backendSyncer) ensureHealthCheck(sp utils.ServicePort) (string, error) {
	var probe *v1.Probe
	var err error

	if s.prober != nil {
		probe, err = s.prober.GetProbe(sp)
		if err != nil {
			return "", fmt.Errorf("Error getting prober: %w", err)
		}
	}
	return s.healthChecker.SyncServicePort(&sp, probe)
}

// getHealthCheckLink gets the Healthcheck link off the BackendService
func getHealthCheckLink(be *composite.BackendService) string {
	if len(be.HealthChecks) == 1 {
		return be.HealthChecks[0]
	}
	return "invalid-healthcheck-link"
}

// ensureProtocol updates the BackendService Protocol with the expected value
func ensureProtocol(be *composite.BackendService, p utils.ServicePort) (needsUpdate bool) {
	if be.Protocol == string(p.Protocol) {
		return false
	}
	be.Protocol = string(p.Protocol)
	return true
}

// ensureHealthCheckLink updates the BackendService HealthCheck with the expected value
func ensureHealthCheckLink(be *composite.BackendService, hcLink string) (needsUpdate bool) {
	existingHCLink := getHealthCheckLink(be)

	if utils.EqualResourceIDs(existingHCLink, hcLink) {
		return false
	}

	be.HealthChecks = []string{hcLink}
	return true
}
