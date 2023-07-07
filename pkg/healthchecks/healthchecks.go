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
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	computealpha "google.golang.org/api/compute/v0.alpha"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/cloud-provider-gcp/providers/gce"
	backendconfigv1 "k8s.io/ingress-gce/pkg/apis/backendconfig/v1"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/loadbalancers/features"
	"k8s.io/ingress-gce/pkg/translator"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/healthcheck"
	"k8s.io/klog/v2"
)

// HealthChecks manages health checks.
type HealthChecks struct {
	cloud HealthCheckProvider
	// path is the default health check path for backends.
	path string
	// This is a workaround which allows us to not have to maintain
	// a separate health checker for the default backend.
	defaultBackendSvc types.NamespacedName
	recorderGetter    RecorderGetter
	serviceGetter     ServiceGetter
	clusterInfo       healthcheck.ClusterInfo
	healthcheckFlags  HealthcheckFlags
}

type HealthcheckFlags struct {
	EnableTHC                                 bool
	EnableRecalculationOnBackendConfigRemoval bool
	THCPort                                   int64
}

// NewHealthChecker creates a new health checker.
// cloud: the cloud object implementing SingleHealthCheck.
// defaultHealthCheckPath: is the HTTP path to use for health checks.
func NewHealthChecker(cloud HealthCheckProvider, healthCheckPath string, defaultBackendSvc types.NamespacedName, recorderGetter RecorderGetter, serviceGetter ServiceGetter, flags HealthcheckFlags) *HealthChecks {
	ci := generateClusterInfo(cloud.(*gce.Cloud))
	return &HealthChecks{
		cloud:             cloud,
		path:              healthCheckPath,
		defaultBackendSvc: defaultBackendSvc,
		recorderGetter:    recorderGetter,
		serviceGetter:     serviceGetter,
		clusterInfo:       ci,
		healthcheckFlags:  flags,
	}
}

func generateClusterInfo(gceCloud *gce.Cloud) healthcheck.ClusterInfo {
	var location string
	regionalCluster := gceCloud.Regional()
	if regionalCluster {
		location = gceCloud.Region()
	} else {
		location = gceCloud.LocalZone()
	}
	name := flags.F.GKEClusterName
	return healthcheck.ClusterInfo{Name: name, Location: location, Regional: regionalCluster}
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
	hc.Port = sp.NodePort
	hc.RequestPath = h.pathFromSvcPort(sp)
	if sp.THCConfiguration.THCOptInOnSvc {
		translator.OverwriteWithTHC(hc, h.healthcheckFlags.THCPort)
	}
	hc.Name = sp.BackendName()
	hc.Service = h.getService(sp)
	hc.SetHealthcheckInfo(h.clusterInfo, h.generateServiceInfo(sp, hc.ForILB))
	return hc
}

func (h *HealthChecks) getService(sp utils.ServicePort) *v1.Service {
	if !flags.F.EnableUpdateCustomHealthCheckDescription {
		return nil
	}
	namespacedName := h.mainService(sp)
	var err error
	service, err := h.serviceGetter.GetService(namespacedName.Namespace, namespacedName.Name)
	if err != nil {
		klog.Warningf("Service %s/%s needed for emitting an event not found (we'll log instead): %v.", namespacedName.Namespace, namespacedName.Name, err)
	}
	return service
}

func (h *HealthChecks) mainService(sp utils.ServicePort) types.NamespacedName {
	service := h.defaultBackendSvc
	if sp.ID.Service.Name != "" {
		service = sp.ID.Service
	}
	return service
}

func (h *HealthChecks) generateServiceInfo(sp utils.ServicePort, iLB bool) healthcheck.ServiceInfo {
	serviceInfo := healthcheck.ServiceInfo(h.defaultBackendSvc)
	if sp.ID.Service.Name != "" {
		serviceInfo = healthcheck.ServiceInfo(sp.ID.Service)
	}

	return serviceInfo
}

// SyncServicePort implements HealthChecker.
func (h *HealthChecks) SyncServicePort(sp *utils.ServicePort, probe *v1.Probe) (string, error) {
	klog.Infof("SyncServicePort: sp.ID=%v, sp.NodePort=%v, sp.Port=%v, sp.PortName=%v, sp.THCConfiguration.THCOptInOnSvc=%v, h.thcEnabled=%v.", sp.ID, sp.NodePort, sp.Port, sp.PortName, sp.THCConfiguration.THCOptInOnSvc, h.healthcheckFlags.EnableTHC)
	if !h.healthcheckFlags.EnableTHC && sp.THCConfiguration.THCOptInOnSvc {
		klog.Warningf("THC flag disabled for HealthChecks, but ServicePort %v has Transparent Health Checks enabled. Disabling.", sp.ID)
		sp.THCConfiguration.THCOptInOnSvc = false
	}
	defer func() { sp.THCConfiguration.THCEvents = utils.THCEvents{} }()

	hc := h.new(*sp)
	if sp.THCConfiguration.THCOptInOnSvc {
		klog.V(2).Infof("ServicePort %v has Transparent Health Checks enabled", sp.ID)
		return h.sync(hc, nil, sp.THCConfiguration)
	}
	if probe != nil {
		klog.V(2).Infof("Applying httpGet settings of readinessProbe to health check on port %+v", sp)
		translator.ApplyProbeSettingsToHC(probe, hc)
	}
	var bchcc *backendconfigv1.HealthCheckConfig
	if sp.BackendConfig != nil && sp.BackendConfig.Spec.HealthCheck != nil {
		bchcc = sp.BackendConfig.Spec.HealthCheck
		klog.V(2).Infof("ServicePort (%v) has BackendConfig health check override (%+s)", sp.ID, formatBackendConfigHC(bchcc))
	}
	if bchcc != nil {
		klog.V(2).Infof("ServicePort %v has BackendConfig healthcheck override", sp.ID)
	}
	return h.sync(hc, bchcc, sp.THCConfiguration)
}

// emitTHCEvents emits Events about successful or attempted THC configuration.
// Currently called on creation or update of a health check.
func (h *HealthChecks) emitTHCEvents(hc *translator.HealthCheck, thcEvents utils.THCEvents) {
	if thcEvents.THCConfigured {
		message := "Transparent Health Check successfully configured."
		h.recorderGetter.Recorder(hc.Service.Namespace).Event(
			hc.Service, v1.EventTypeNormal, "THCConfigured", message)
		klog.Infof("%s Health check name: %s.", message, hc.Name)
	}
	if thcEvents.BackendConfigOverridesTHC {
		message := "Both THC and BackendConfig annotations present and the BackendConfig has spec.healthCheck. The THC annotation will be ignored."
		h.recorderGetter.Recorder(hc.Service.Namespace).Event(
			hc.Service, v1.EventTypeWarning, "BackendConfigOverridesTHC", message)
		klog.Warningf("%s Health check name: %s.", message, hc.Name)
	}
	if thcEvents.THCAnnotationWithoutFlag {
		message := "THC annotation present, but the Transparent Health Checks feature is not enabled."
		h.recorderGetter.Recorder(hc.Service.Namespace).Event(
			hc.Service, v1.EventTypeWarning, "THCAnnotationWithoutFlag", message)
		klog.Warningf("%s Health check name: %s.", message, hc.Name)
	}
	if thcEvents.THCAnnotationWithoutNEG {
		message := "THC annotation present, but NEG is disabled. Will not enable Transparent Health Checks."
		h.recorderGetter.Recorder(hc.Service.Namespace).Event(
			hc.Service, v1.EventTypeWarning, "THCAnnotationWithoutNEG", message)
		klog.Warningf("%s Health check name: %s.", message, hc.Name)
	}

}

func isBackendConfigRemoved(hcDesc *healthcheck.HealthcheckDesc, bchcc *backendconfigv1.HealthCheckConfig) bool {
	// The flag EnableRecalculationOnBackendConfigRemoval should be tested separately:
	// this function only is only for removal detection, not to decide on healthcheck recalculation.

	// The existing HC is configured with a BackendConfig, but there's no BackendConfig now.
	return hcDesc != nil && hcDesc.Config == healthcheck.BackendConfigHC && bchcc == nil
}

func isTHCRemoved(hcDesc *healthcheck.HealthcheckDesc, thcOptIn bool) bool {
	// This is not behind a feature flag because it should work in particular at the time of disabling the flag EnableTransparentHealthChecks.

	// The existing HC is configured via Transparent Health Checks, but the annotation has been removed.
	return hcDesc != nil && hcDesc.Config == healthcheck.TransparentHC && !thcOptIn
}

func (h *HealthChecks) shouldRecalculateHC(existingHC *translator.HealthCheck, backendConfigHCConfig *backendconfigv1.HealthCheckConfig, thcConf utils.THCConfiguration) bool {
	hcDesc := &healthcheck.HealthcheckDesc{}
	if err := json.Unmarshal([]byte(existingHC.Description), hcDesc); err != nil {
		klog.V(3).Infof("Health check description is not JSONified: %s", existingHC.Description)
		hcDesc = nil
	}
	return thcConf.THCOptInOnSvc || (h.healthcheckFlags.EnableRecalculationOnBackendConfigRemoval && isBackendConfigRemoved(hcDesc, backendConfigHCConfig)) || isTHCRemoved(hcDesc, thcConf.THCOptInOnSvc)
}

// sync retrieves a health check based on port, checks type and settings and updates/creates if necessary.
// sync is only called by the backends.Add func - it's not a pool like other resources.
// We assume that backendConfigHCConfig cannot be non-nil and thcOptIn be true simultaneously.
func (h *HealthChecks) sync(hc *translator.HealthCheck, backendConfigHCConfig *backendconfigv1.HealthCheckConfig, thcConf utils.THCConfiguration) (string, error) {
	if backendConfigHCConfig != nil && thcConf.THCOptInOnSvc {
		klog.Warningf("BackendConfig exists and thcOptIn true simultaneously for %v. Ignoring transparent health check.", hc.Name)
		thcConf.THCOptInOnSvc = false
	}

	var scope meta.KeyType
	// TODO(shance): find a way to remove this
	if hc.ForILB {
		scope = meta.Regional
	} else {
		scope = meta.Global
	}

	existingHC, err := h.Get(hc.Name, hc.Version(), scope)
	if utils.IsHTTPErrorCode(err, http.StatusNotFound) {
		klog.V(2).Infof("Health check %q does not exist, creating (hc=%+v, backendConfigHCConfig=%+v)", hc.Name, hc, backendConfigHCConfig)
		if err = h.create(hc, backendConfigHCConfig); err != nil {
			klog.Errorf("Health check %q creation error: %v", hc.Name, err)
			return "", err
		}
		h.emitTHCEvents(hc, thcConf.THCEvents)
		// TODO(bowei) -- we don't need to fetch the self-link here as it is
		// returned as part of the GCE call.
		selfLink, err := h.getHealthCheckLink(hc.Name, hc.Version(), scope)
		klog.V(2).Infof("Health check %q selflink = %q", hc.Name, selfLink)
		return selfLink, err
	}
	if err != nil {
		return "", err
	}

	// Do not merge the existing settings and perform the full diff in calculateDiff.
	recalculate := h.shouldRecalculateHC(existingHC, backendConfigHCConfig, thcConf)

	if !recalculate {
		// Merge in the configuration from the existing healthcheck to cover
		// the case where the user has changed healthcheck settings outside of
		// GKE.
		premergeHC := hc
		hc = mergeUserSettings(existingHC, hc)
		klog.V(3).Infof("Existing HC = %+v", existingHC)
		klog.V(3).Infof("HC before merge = %+v", premergeHC)
		klog.V(3).Infof("Resulting HC = %+v", hc)
	}

	// Then, BackendConfig will override any fields that are explicitly set.
	if backendConfigHCConfig != nil {
		// BackendConfig healthcheck settings always take precedence.
		hc.UpdateFromBackendConfig(backendConfigHCConfig)
	}

	filter := func(hc *translator.HealthCheck) *translator.HealthCheck {
		var ans = *hc // Shallow copy.
		if !recalculate && !flags.F.EnableUpdateCustomHealthCheckDescription {
			ans.Description = ""
		}
		return &ans
	}

	changes := calculateDiff(filter(existingHC), filter(hc), backendConfigHCConfig, recalculate)
	// The use of 'descriptionOnlyUpdate' guarantees that when BackendConfig is removed, the health check Description is
	// updated accordingly even if changes.hasDiff() is false. The purpose is for the Description to accurately reflect
	// the existence of a backendconfigv1.HealthCheckConfig for the service. This is temporary, see
	// https://github.com/kubernetes/ingress-gce/pull/2181 for details.
	descriptionOnlyUpdate := h.isDescriptionOnlyUpdateNeeded(changes, existingHC, backendConfigHCConfig)
	if changes.hasDiff() || descriptionOnlyUpdate {
		klog.V(2).Infof("Health check %q needs update (%s)", existingHC.Name, changes)
		if descriptionOnlyUpdate {
			message := fmt.Sprintf("Healthcheck will be updated and the only field updated is Description.\nOld: %+v\nNew: %+v\n", existingHC, hc)
			if hc.Service != nil {
				h.recorderGetter.Recorder(hc.Service.Namespace).Event(
					hc.Service, v1.EventTypeNormal, "HealthcheckDescriptionUpdate", message)
			} else {
				klog.Info(message)
			}
		}
		err := h.update(hc)
		if err != nil {
			klog.Errorf("Health check %q update error: %v", existingHC.Name, err)
		}
		h.emitTHCEvents(hc, thcConf.THCEvents)
		return existingHC.SelfLink, err
	}

	klog.V(2).Infof("Health check %q already exists and needs no update", hc.Name)
	return existingHC.SelfLink, nil
}

func (h *HealthChecks) isDescriptionOnlyUpdateNeeded(changes *fieldDiffs, existingHC *translator.HealthCheck, backendConfigHCConfig *backendconfigv1.HealthCheckConfig) bool {
	if flags.F.EnableUpdateCustomHealthCheckDescription {
		// BackendConfig exists, but the health check has had a wrong description.
		if changes.size() == 1 && changes.has("Description") {
			return true
		}

		if h.healthcheckFlags.EnableRecalculationOnBackendConfigRemoval {
			// Further down, true is only returned on BackendConfig removal with changes.size() == 0, which never happens when
			// EnableRecalculationOnBackendConfigRemoval is enabled. The present 'if' exists to make it clear that after
			// EnableRecalculationOnBackendConfigRemoval is successfully rolled out, the ramaining part of the function
			// can be removed.
			return false
		}
		desc := &healthcheck.HealthcheckDesc{}
		err := json.Unmarshal([]byte(existingHC.Description), desc)
		if err != nil {
			klog.V(3).Info("Description for healthcheck %s is not a JSON (probably a plain-text description): %s.", existingHC.Name, existingHC.Description)
			return false
		}
		// BackendConfig existed and has been removed now + no other changes to healthcheck are needed.
		if changes.size() == 0 && desc.Config == healthcheck.BackendConfigHC && backendConfigHCConfig == nil {
			return true
		}
	}
	return false
}

// TODO(shance): merge with existing hc code
func (h *HealthChecks) createILB(hc *translator.HealthCheck) error {
	alpha, err := hc.ToAlphaComputeHealthCheck()
	if err != nil {
		return err
	}
	compositeType, err := composite.AlphaToHealthCheck(alpha)
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
		alphaHC, err := hc.ToAlphaComputeHealthCheck()
		if err != nil {
			return err
		}
		return h.cloud.CreateAlphaHealthCheck(alphaHC)
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
	alpha, err := hc.ToAlphaComputeHealthCheck()
	if err != nil {
		return err
	}
	compositeType, err := composite.AlphaToHealthCheck(alpha)
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
		alpha, err := hc.ToAlphaComputeHealthCheck()
		if err != nil {
			return err
		}
		return h.cloud.UpdateAlphaHealthCheck(alpha)
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
