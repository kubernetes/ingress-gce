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
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/ingress-gce/pkg/backends/features"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/healthchecks"
	lbfeatures "k8s.io/ingress-gce/pkg/loadbalancers/features"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog/v2"
)

// Syncer manages the lifecycle of GCE BackendServices based on Kubernetes services.
type Syncer struct {
	backendPool   *Pool
	healthChecker healthchecks.HealthChecker
	prober        ProbeProvider
	cloud         *gce.Cloud
}

func NewBackendSyncer(
	backendPool *Pool,
	healthChecker healthchecks.HealthChecker,
	cloud *gce.Cloud,
	prober ProbeProvider,
) *Syncer {
	return &Syncer{
		backendPool:   backendPool,
		healthChecker: healthChecker,
		cloud:         cloud,
		prober:        prober,
	}
}

// Sync a BackendService. Implementations should only create the BackendService
// but not its groups.
func (s *Syncer) Sync(svcPorts []utils.ServicePort, ingLogger klog.Logger) error {
	for _, sp := range svcPorts {
		ingLogger.Info("Sync backend", "servicePort", fmt.Sprintf("%v", sp))
		if err := s.ensureBackendService(sp, ingLogger); err != nil {
			return err
		}
	}
	return nil
}

// ensureBackendService will update or create a BackendService for the given port.
func (s *Syncer) ensureBackendService(sp utils.ServicePort, ingLogger klog.Logger) error {
	// We must track the ports even if creating the backends failed, because
	// we might've created health-check for them.
	be := &composite.BackendService{}
	beName := sp.BackendName()
	version := features.VersionFromServicePort(&sp)
	scope := features.ScopeFromServicePort(&sp)

	beLogger := ingLogger.WithValues(
		"backendServiceName", beName,
		"backendVersion", version,
		"backendScope", scope,
		"port", sp.NodePort,
	)
	be, getErr := s.backendPool.Get(beName, version, scope, beLogger)

	// Ensure health check for backend service exists.
	hcLink, err := s.ensureHealthCheck(sp, beLogger)
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
		beLogger.Info("Creating backend service")
		be, err = s.backendPool.Create(sp, hcLink, beLogger)
		if err != nil {
			return err
		}
	}

	needUpdate := ensureProtocol(be, sp)
	needUpdate = ensureHealthCheckLink(be, hcLink) || needUpdate
	needUpdate = ensureDescription(be, &sp) || needUpdate
	if sp.BackendConfig != nil {
		needUpdate = features.EnsureCDN(sp, be, beLogger) || needUpdate
		needUpdate = features.EnsureTimeout(sp, be, beLogger) || needUpdate
		needUpdate = features.EnsureDraining(sp, be, beLogger) || needUpdate
		needUpdate = features.EnsureAffinity(sp, be, beLogger) || needUpdate
		needUpdate = features.EnsureCustomRequestHeaders(sp, be, beLogger) || needUpdate
		needUpdate = features.EnsureCustomResponseHeaders(sp, be, beLogger) || needUpdate
		needUpdate = features.EnsureLogging(sp, be, beLogger) || needUpdate

		updateIAP, err := features.EnsureIAP(sp, be, beLogger)
		if err != nil {
			beLogger.Error(err, "Errored ensuring IAP")
			return err
		}
		needUpdate = updateIAP || needUpdate
	}

	if needUpdate {
		if err := s.backendPool.Update(be, beLogger); err != nil {
			return err
		}
	}

	if err := s.ensureBackendSignedURLKeys(sp, be, beLogger); err != nil {
		return err
	}

	if sp.BackendConfig != nil {
		// Scope is used to validate if cloud armor security policy feature is
		// available. meta.Key is not needed as security policy supported only for
		// global backends.
		be.Scope = scope
		if err := features.EnsureSecurityPolicy(s.cloud, sp, be, beLogger); err != nil {
			return err
		}
	}

	return nil
}

func (s *Syncer) ensureBackendSignedURLKeys(sp utils.ServicePort, be *composite.BackendService, beLogger klog.Logger) error {
	existingKeyNames := map[string]bool{}
	if be.CdnPolicy != nil && be.CdnPolicy.SignedUrlKeyNames != nil {
		for _, key := range be.CdnPolicy.SignedUrlKeyNames {
			existingKeyNames[key] = false
		}
	}
	// there is a hard limit of maximum 3 keys per backend, if you add before remove you will hit the limit
	// find existing and the new keys first
	newSignedUrlKeys := []*composite.SignedUrlKey{}
	if sp.BackendConfig != nil && sp.BackendConfig.Spec.Cdn != nil && sp.BackendConfig.Spec.Cdn.SignedUrlKeys != nil {
		for _, key := range sp.BackendConfig.Spec.Cdn.SignedUrlKeys {
			urlKeyLogger := beLogger.WithValues("SignedUrlKey", key.KeyName)
			urlKeyLogger.Info("Search for SignedUrlKey")
			if _, found := existingKeyNames[key.KeyName]; !found {
				newSignedUrlKeys = append(newSignedUrlKeys, &composite.SignedUrlKey{KeyName: key.KeyName, KeyValue: key.KeyValue})
			} else {
				urlKeyLogger.Info("SignedUrlKey found, no update needed")
				existingKeyNames[key.KeyName] = true
			}
		}
	}
	// delete all removed keys
	for keyName, found := range existingKeyNames {
		urlKeyLogger := beLogger.WithValues("SignedUrlKey", keyName)
		if !found {
			urlKeyLogger.Info("Removing SignedUrlKey")
			if err := s.backendPool.DeleteSignedURLKey(be, keyName, urlKeyLogger); err != nil {
				return err
			}
		}
	}
	// add all appended keys
	for _, key := range newSignedUrlKeys {
		urlKeyLogger := beLogger.WithValues("SignedUrlKey", key.KeyName)
		urlKeyLogger.Info("Adding SignedUrlKey")
		if err := s.backendPool.AddSignedURLKey(be, key, urlKeyLogger); err != nil {
			return err
		}
	}
	return nil
}

// GC garbage collects unused BackendService's
func (s *Syncer) GC(svcPorts []utils.ServicePort, ingLogger klog.Logger) error {
	knownPorts, err := knownPortsFromServicePorts(s.cloud, svcPorts)
	if err != nil {
		return err
	}

	ilbBeLogger := ingLogger.WithName("ILBBackends")
	// TODO(shance): Refactor out empty key field
	key, err := composite.CreateKey(s.cloud, "", meta.Regional)
	if err != nil {
		return fmt.Errorf("error creating l7 ilb key: %w", err)
	}
	ilbBackends, err := s.backendPool.List(key, lbfeatures.L7ILBVersions().BackendService, ilbBeLogger)
	if err != nil {
		return fmt.Errorf("error listing regional backends: %w", err)
	}
	err = s.gc(ilbBackends, knownPorts, ilbBeLogger)
	if err != nil {
		return fmt.Errorf("error GCing regional Backends: %w", err)
	}

	gaBeLogger := ingLogger.WithName("GABackends")
	// Requires an empty name field until it is refactored out
	key, err = composite.CreateKey(s.cloud, "", meta.Global)
	if err != nil {
		return fmt.Errorf("error creating l7 ilb key: %w", err)
	}
	backends, err := s.backendPool.List(key, meta.VersionGA, gaBeLogger)
	if err != nil {
		return fmt.Errorf("error listing backends: %w", err)
	}
	err = s.gc(backends, knownPorts, gaBeLogger)
	if err != nil {
		return fmt.Errorf("error GCing Backends: %w", err)
	}

	return nil
}

// gc deletes the provided backends
func (s *Syncer) gc(backends []*composite.BackendService, knownPorts sets.String, ingLogger klog.Logger) error {
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
		beLogger := ingLogger.WithValues(
			"backendName", name,
			"backendVersion", be.Version,
			"backendScope", scope,
		)
		beLogger.Info("GCing backendService")
		err = s.backendPool.Delete(name, be.Version, scope, beLogger)
		if err != nil {
			beLogger.Error(err, "backendPool.Delete()")
			return err
		}

		if err := s.healthChecker.Delete(name, scope, beLogger); err != nil {
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

// Status returns the status of a BackendService given its name.
func (s *Syncer) Status(name string, version meta.Version, scope meta.KeyType, ingLogger klog.Logger) (string, error) {
	beLogger := ingLogger.WithValues(
		"backendName", name,
		"backendVersion", version,
		"backendScope", scope,
	)
	return s.backendPool.Health(name, version, scope, beLogger)
}

// Shutdown cleans up all BackendService's previously synced.
// TODO(cheungdavid): Shutdown() should be deprecated after the removal of delateAll option.
func (s *Syncer) Shutdown() error {
	if err := s.GC([]utils.ServicePort{}, klog.TODO()); err != nil {
		return err
	}
	return nil
}

func (s *Syncer) ensureHealthCheck(sp utils.ServicePort, beLogger klog.Logger) (string, error) {
	var probe *v1.Probe
	var err error

	if s.prober != nil {
		probe, err = s.prober.GetProbe(sp)
		if err != nil {
			return "", fmt.Errorf("Error getting prober: %w", err)
		}
	}
	return s.healthChecker.SyncServicePort(&sp, probe, beLogger)
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
