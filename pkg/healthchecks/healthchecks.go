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

package healthchecks

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	computealpha "google.golang.org/api/compute/v0.alpha"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	backendconfigv1 "k8s.io/ingress-gce/pkg/apis/backendconfig/v1"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/loadbalancers/features"
	"k8s.io/ingress-gce/pkg/translator"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog"
	"k8s.io/legacy-cloud-providers/gce"
)

// HealthChecks manages health checks.
type HealthChecks struct {
	cloud HealthCheckProvider
	// path is the default health check path for backends.
	path string
	// This is a workaround which allows us to not have to maintain
	// a separate health checker for the default backend.
	defaultBackendSvc types.NamespacedName
}

// NewHealthChecker creates a new health checker.
// cloud: the cloud object implementing SingleHealthCheck.
// defaultHealthCheckPath: is the HTTP path to use for health checks.
func NewHealthChecker(cloud HealthCheckProvider, healthCheckPath string, defaultBackendSvc types.NamespacedName) *HealthChecks {
	return &HealthChecks{cloud, healthCheckPath, defaultBackendSvc}
}

// new returns a *HealthCheck with default settings and specified port/protocol
func (h *HealthChecks) new(sp utils.ServicePort) *translator.HealthCheck {
	var hc *translator.HealthCheck
	if sp.NEGEnabled && !sp.L7ILBEnabled {
		hc = translator.DefaultNEGHealthCheck(sp.Protocol)
	} else if sp.L7ILBEnabled {
		hc = translator.DefaultILBHealthCheck(sp.Protocol)
	} else {
		hc = translator.DefaultHealthCheck(sp.NodePort, sp.Protocol)
	}
	// port is the key for retrieving existing health-check
	// TODO: rename backend-service and health-check to not use port as key
	hc.Name = sp.BackendName()
	hc.Port = sp.NodePort
	hc.RequestPath = h.pathFromSvcPort(sp)
	return hc
}

// SyncServicePort implements HealthChecker.
func (h *HealthChecks) SyncServicePort(sp *utils.ServicePort, probe *v1.Probe) (string, error) {
	hc := h.new(*sp)
	if probe != nil {
		klog.V(2).Infof("Applying httpGet settings of readinessProbe to health check on port %+v", sp)
		translator.ApplyProbeSettingsToHC(probe, hc)
	}
	var bchcc *backendconfigv1.HealthCheckConfig
	if flags.F.EnableBackendConfigHealthCheck && sp.BackendConfig != nil && sp.BackendConfig.Spec.HealthCheck != nil {
		bchcc = sp.BackendConfig.Spec.HealthCheck
		klog.V(2).Infof("ServicePort (%v) has BackendConfig health check override (%+s)", sp.ID, formatBackendConfigHC(bchcc))
	}
	if bchcc != nil {
		klog.V(2).Infof("ServicePort %v has BackendConfig healthcheck override", sp.ID)
	}
	return h.sync(hc, bchcc)
}

// sync retrieves a health check based on port, checks type and settings and updates/creates if necessary.
// sync is only called by the backends.Add func - it's not a pool like other resources.
func (h *HealthChecks) sync(hc *translator.HealthCheck, bchcc *backendconfigv1.HealthCheckConfig) (string, error) {
	var scope meta.KeyType
	// TODO(shance): find a way to remove this
	if hc.ForILB {
		scope = meta.Regional
	} else {
		scope = meta.Global
	}

	existingHC, err := h.Get(hc.Name, hc.Version(), scope)
	if utils.IsHTTPErrorCode(err, http.StatusNotFound) {
		klog.V(2).Infof("Health check %q does not exist, creating (hc=%+v, bchcc=%+v)", hc.Name, hc, bchcc)
		if err = h.create(hc, bchcc); err != nil {
			klog.Errorf("Health check %q creation error: %v", hc.Name, err)
			return "", err
		}
		// TODO(bowei) -- we don't need to fetch the self-link here as it is
		// returned as part of the GCE call.
		selfLink, err := h.getHealthCheckLink(hc.Name, hc.Version(), scope)
		klog.V(2).Infof("Health check %q selflink = %q", hc.Name, selfLink)
		return selfLink, err
	}
	if err != nil {
		return "", err
	}

	// First, merge in the configuration from the existing healthcheck to cover
	// the case where the user has changed healthcheck settings outside of
	// GKE.
	premergeHC := hc
	hc = mergeUserSettings(existingHC, hc)
	klog.V(3).Infof("Existing HC = %+v", existingHC)
	klog.V(3).Infof("HC before merge = %+v", premergeHC)
	klog.V(3).Infof("Resulting HC = %+v", hc)

	// Then, BackendConfig will override any fields that are explicitly set.
	if bchcc != nil {
		// BackendConfig healthcheck settings always take precedence.
		hc.UpdateFromBackendConfig(bchcc)
	}

	changes := calculateDiff(existingHC, hc, bchcc)
	if changes.hasDiff() {
		klog.V(2).Infof("Health check %q needs update (%s)", existingHC.Name, changes)
		err := h.update(hc)
		if err != nil {
			klog.Errorf("Health check %q update error: %v", existingHC.Name, err)
		}
		return existingHC.SelfLink, err
	}

	klog.V(2).Infof("Health check %q already exists and needs no update", hc.Name)
	return existingHC.SelfLink, nil
}

// TODO(shance): merge with existing hc code
func (h *HealthChecks) createILB(hc *translator.HealthCheck) error {
	compositeType, err := composite.AlphaToHealthCheck(hc.ToAlphaComputeHealthCheck())
	if err != nil {
		return fmt.Errorf("Error converting hc to composite: %w", err)
	}

	cloud := h.cloud.(*gce.Cloud)
	key, err := composite.CreateKey(cloud, hc.Name, features.L7ILBScope())
	if err != nil {
		return err
	}

	compositeType.Version = features.L7ILBVersions().HealthCheck
	compositeType.Region = key.Region
	err = composite.CreateHealthCheck(cloud, key, compositeType)
	if err != nil {
		return fmt.Errorf("Error creating health check %v: %w", compositeType, err)
	}

	return nil
}

func (h *HealthChecks) create(hc *translator.HealthCheck, bchcc *backendconfigv1.HealthCheckConfig) error {
	if bchcc != nil {
		// BackendConfig healthcheck settings always take precedence.
		hc.UpdateFromBackendConfig(bchcc)
	}
	// special case ILB to avoid mucking with stable HC code
	if hc.ForILB {
		return h.createILB(hc)
	}

	switch hc.Version() {
	case meta.VersionAlpha:
		klog.V(2).Infof("Creating alpha health check with protocol %v", hc.Type)
		return h.cloud.CreateAlphaHealthCheck(hc.ToAlphaComputeHealthCheck())
	case meta.VersionBeta:
		klog.V(2).Infof("Creating beta health check with protocol %v", hc.Type)
		betaHC, err := hc.ToBetaComputeHealthCheck()
		if err != nil {
			return err
		}
		return h.cloud.CreateBetaHealthCheck(betaHC)
	case meta.VersionGA:
		klog.V(2).Infof("Creating health check for port %v with protocol %v", hc.Port, hc.Type)
		v1hc, err := hc.ToComputeHealthCheck()
		if err != nil {
			return err
		}
		return h.cloud.CreateHealthCheck(v1hc)
	default:
		return fmt.Errorf("unknown Version: %q", hc.Version())
	}
}

// TODO(shance): merge with existing hc code
func (h *HealthChecks) updateILB(hc *translator.HealthCheck) error {
	// special case ILB to avoid mucking with stable HC code
	compositeType, err := composite.AlphaToHealthCheck(hc.ToAlphaComputeHealthCheck())
	if err != nil {
		return fmt.Errorf("Error converting newHC to composite: %w", err)
	}
	cloud := h.cloud.(*gce.Cloud)
	key, err := composite.CreateKey(cloud, hc.Name, features.L7ILBScope())

	// Update fields
	compositeType.Version = features.L7ILBVersions().HealthCheck
	compositeType.Region = key.Region

	return composite.UpdateHealthCheck(cloud, key, compositeType)
}

func (h *HealthChecks) update(hc *translator.HealthCheck) error {
	if hc.ForILB {
		return h.updateILB(hc)
	}
	switch hc.Version() {
	case meta.VersionAlpha:
		klog.V(2).Infof("Updating alpha health check with protocol %v", hc.Type)
		return h.cloud.UpdateAlphaHealthCheck(hc.ToAlphaComputeHealthCheck())
	case meta.VersionBeta:
		klog.V(2).Infof("Updating beta health check with protocol %v", hc.Type)
		beta, err := hc.ToBetaComputeHealthCheck()
		if err != nil {
			return err
		}
		return h.cloud.UpdateBetaHealthCheck(beta)
	case meta.VersionGA:
		klog.V(2).Infof("Updating health check %q for port %v with protocol %v", hc.Name, hc.Port, hc.Type)
		ga, err := hc.ToComputeHealthCheck()
		if err != nil {
			return err
		}
		return h.cloud.UpdateHealthCheck(ga)
	default:
		return fmt.Errorf("unknown Version: %q", hc.Version())
	}
}

func (h *HealthChecks) getHealthCheckLink(name string, version meta.Version, scope meta.KeyType) (string, error) {
	hc, err := h.Get(name, version, scope)
	if err != nil {
		return "", err
	}
	return hc.SelfLink, nil
}

// Delete deletes the health check by port.
func (h *HealthChecks) Delete(name string, scope meta.KeyType) error {
	if scope == meta.Regional {
		cloud := h.cloud.(*gce.Cloud)
		key, err := composite.CreateKey(cloud, name, meta.Regional)
		if err != nil {
			return err
		}
		klog.V(2).Infof("Deleting regional health check %v", name)
		// L7-ILB is the only use of regional right now
		if err = composite.DeleteHealthCheck(cloud, key, features.L7ILBVersions().HealthCheck); err != nil {
			// Ignore error if the deletion candidate is being used by another resource.
			// In most of the cases, this is the associated backend resource itself.
			if utils.IsHTTPErrorCode(err, http.StatusNotFound) || utils.IsInUsedByError(err) {
				klog.V(4).Infof("DeleteRegionalHealthCheck(%s, _): %v, ignorable error", name, err)
				return nil
			}
			return err
		}
		return nil
	}

	klog.V(2).Infof("Deleting health check %v", name)
	// Not using composite here since the tests still rely on the fake health check interface
	if err := h.cloud.DeleteHealthCheck(name); err != nil {
		// Ignore error if the deletion candidate does not exist or is being used
		// by another resource.
		if utils.IsHTTPErrorCode(err, http.StatusNotFound) || utils.IsInUsedByError(err) {
			klog.V(4).Infof("DeleteHealthCheck(%s, _): %v, ignorable error", name, err)
			return nil
		}
		return err
	}
	return nil
}

// TODO(shance): merge with existing hc code
func (h *HealthChecks) getILB(name string) (*translator.HealthCheck, error) {
	klog.V(3).Infof("Getting ILB Health Check, name: %s", name)
	cloud := h.cloud.(*gce.Cloud)
	key, err := composite.CreateKey(cloud, name, meta.Regional)
	if err != nil {
		return nil, err
	}
	// L7-ILB is the only use of regional right now
	hc, err := composite.GetHealthCheck(cloud, key, features.L7ILBVersions().HealthCheck)
	if err != nil {
		return nil, err
	}
	gceHC, err := hc.ToAlpha()
	if err != nil {
		return nil, err
	}

	newHC, err := translator.NewHealthCheck(gceHC)
	if err != nil {
		return nil, err
	}

	// Update fields for future update() calls
	newHC.ForILB = true
	newHC.ForNEG = true

	return newHC, nil
}

// Get returns the health check by port
func (h *HealthChecks) Get(name string, version meta.Version, scope meta.KeyType) (*translator.HealthCheck, error) {
	klog.V(3).Infof("Getting Health Check, name: %s, version: %v, scope: %v", name, version, scope)

	// L7-ILB is the only use of regional right now
	if scope == meta.Regional {
		return h.getILB(name)
	}

	var hc *computealpha.HealthCheck
	var err error
	switch version {
	case meta.VersionAlpha:
		hc, err = h.cloud.GetAlphaHealthCheck(name)
		if err != nil {
			return nil, err
		}
	case meta.VersionBeta:
		betaHC, err := h.cloud.GetBetaHealthCheck(name)
		if err != nil {
			return nil, err
		}
		hc, err = utils.BetaToAlphaHealthCheck(betaHC)
	case meta.VersionGA:
		v1hc, err := h.cloud.GetHealthCheck(name)
		if err != nil {
			return nil, err
		}
		hc, err = utils.V1ToAlphaHealthCheck(v1hc)
	default:
		return nil, fmt.Errorf("unknown version %v", version)
	}
	if err != nil {
		return nil, err
	}
	return translator.NewHealthCheck(hc)
}

// pathFromSvcPort returns the default path for a health check based on whether
// the passed in ServicePort is associated with the system default backend.
func (h *HealthChecks) pathFromSvcPort(sp utils.ServicePort) string {
	if h.defaultBackendSvc == sp.ID.Service {
		return flags.F.DefaultSvcHealthCheckPath
	}
	return h.path
}

// formatBackendConfigHC returns a human readable string version of the HealthCheckConfig
func formatBackendConfigHC(b *backendconfigv1.HealthCheckConfig) string {
	var ret []string

	for _, e := range []struct {
		v *int64
		k string
	}{
		{k: "checkIntervalSec", v: b.CheckIntervalSec},
		{k: "healthyThreshold", v: b.HealthyThreshold},
		{k: "unhealthyThreshold", v: b.UnhealthyThreshold},
		{k: "timeoutSec", v: b.TimeoutSec},
		{k: "port", v: b.Port},
	} {
		if e.v != nil {
			ret = append(ret, fmt.Sprintf("%s=%d", e.k, *e.v))
		}
	}
	if b.Type != nil {
		ret = append(ret, fmt.Sprintf("type=%s", *b.Type))
	}
	if b.RequestPath != nil {
		ret = append(ret, fmt.Sprintf("requestPath=%q", *b.RequestPath))
	}
	return strings.Join(ret, ", ")
}
