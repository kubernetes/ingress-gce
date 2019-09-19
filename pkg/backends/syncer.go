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
	"strings"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/ingress-gce/pkg/backends/features"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/healthchecks"
	lbfeatures "k8s.io/ingress-gce/pkg/loadbalancers/features"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/klog"
	"k8s.io/legacy-cloud-providers/gce"
)

// backendSyncer manages the lifecycle of backends.
type backendSyncer struct {
	backendPool   Pool
	healthChecker healthchecks.HealthChecker
	prober        ProbeProvider
	namer         *namer.Namer
	cloud         *gce.Cloud
}

// backendSyncer is a Syncer
var _ Syncer = (*backendSyncer)(nil)

func NewBackendSyncer(
	backendPool Pool,
	healthChecker healthchecks.HealthChecker,
	namer *namer.Namer,
	cloud *gce.Cloud) Syncer {
	return &backendSyncer{
		backendPool:   backendPool,
		healthChecker: healthChecker,
		namer:         namer,
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
	beName := sp.BackendName(s.namer)
	version := features.VersionFromServicePort(&sp)
	scope := features.ScopeFromServicePort(&sp)

	be, getErr := s.backendPool.Get(beName, version, scope)
	hasLegacyHC := false
	if be != nil {
		// If the backend already exists, find out if it is using a legacy health check.
		existingHCLink := getHealthCheckLink(be)
		if strings.Contains(existingHCLink, "/httpHealthChecks/") {
			hasLegacyHC = true
		}
	}

	// Ensure health check for backend service exists.
	hcLink, err := s.ensureHealthCheck(sp, hasLegacyHC)
	if err != nil {
		return fmt.Errorf("error ensuring health check: %v", err)
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
	}

	if needUpdate {
		if err := s.backendPool.Update(be); err != nil {
			return err
		}
	}

	if sp.BackendConfig != nil {
		if err := features.EnsureSecurityPolicy(s.cloud, sp, be, beName); err != nil {
			return err
		}
	}

	return nil
}

// GC implements Syncer.
func (s *backendSyncer) GC(svcPorts []utils.ServicePort) error {
	knownPorts, err := knownPortsFromServicePorts(s.cloud, s.namer, svcPorts)
	if err != nil {
		return err
	}

	// Only GC L7 ILB backends if it's enabled
	if flags.F.EnableL7Ilb {
		// TODO(shance): Refactor out empty key field
		key, err := composite.CreateKey(s.cloud, "", meta.Regional)
		if err != nil {
			return fmt.Errorf("error creating l7 ilb key: %v", err)
		}
		ilbBackends, err := s.backendPool.List(key, lbfeatures.L7ILBVersions().BackendService)
		if err != nil {
			return fmt.Errorf("error listing regional backends: %v", err)
		}
		err = s.gc(ilbBackends, knownPorts)
		if err != nil {
			return fmt.Errorf("error GCing regional Backends: %v", err)
		}
	}

	// Requires an empty name field until it is refactored out
	key, err := composite.CreateKey(s.cloud, "", meta.Global)
	if err != nil {
		return fmt.Errorf("error creating l7 ilb key: %v", err)
	}
	backends, err := s.backendPool.List(key, meta.VersionGA)
	if err != nil {
		return fmt.Errorf("error listing backends: %v", err)
	}
	err = s.gc(backends, knownPorts)
	if err != nil {
		return fmt.Errorf("error GCing Backends: %v", err)
	}

	return nil
}

// gc deletes the provided backends
func (s *backendSyncer) gc(backends []*composite.BackendService, knownPorts sets.String) error {
	for _, be := range backends {
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
			if utils.IsHTTPErrorCode(err, http.StatusNotFound) {
				klog.Infof("backendPool.Delete(%v, %v, %v) = %v; backend service not found, ignoring", name, be.Version, scope, err)
				return nil
			}

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
func knownPortsFromServicePorts(cloud *gce.Cloud, namer *namer.Namer, svcPorts []utils.ServicePort) (sets.String, error) {
	knownPorts := sets.NewString()

	for _, sp := range svcPorts {
		name := sp.BackendName(namer)
		if key, err := composite.CreateKey(cloud, name, features.ScopeFromServicePort(&sp)); err != nil {
			return nil, err
		} else {
			knownPorts.Insert(key.String())
		}
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

func (s *backendSyncer) ensureHealthCheck(sp utils.ServicePort, hasLegacyHC bool) (string, error) {
	if hasLegacyHC {
		klog.Errorf("Backend %+v has legacy health check", sp.ID)
	}
	hc := s.healthChecker.New(sp)
	if s.prober != nil {
		probe, err := s.prober.GetProbe(sp)
		if err != nil {
			return "", fmt.Errorf("Error getting prober: %v", err)
		}
		if probe != nil {
			klog.V(4).Infof("Applying httpGet settings of readinessProbe to health check on port %+v", sp)
			applyProbeSettingsToHC(probe, hc)
		}
	}

	return s.healthChecker.Sync(hc)
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

func applyProbeSettingsToHC(p *v1.Probe, hc *healthchecks.HealthCheck) {
	healthPath := p.Handler.HTTPGet.Path
	// GCE requires a leading "/" for health check urls.
	if !strings.HasPrefix(healthPath, "/") {
		healthPath = "/" + healthPath
	}
	// Extract host from HTTP headers
	host := p.Handler.HTTPGet.Host
	for _, header := range p.Handler.HTTPGet.HTTPHeaders {
		if header.Name == "Host" {
			host = header.Value
			break
		}
	}

	hc.RequestPath = healthPath
	hc.Host = host
	hc.Description = "Kubernetes L7 health check generated with readiness probe settings."
	hc.TimeoutSec = int64(p.TimeoutSeconds)
	if hc.ForNEG {
		// For NEG mode, we can support more aggressive healthcheck interval.
		hc.CheckIntervalSec = int64(p.PeriodSeconds)
	} else {
		// For IG mode, short healthcheck interval may health check flooding problem.
		hc.CheckIntervalSec = int64(p.PeriodSeconds) + int64(healthchecks.DefaultHealthCheckInterval.Seconds())
	}
}
