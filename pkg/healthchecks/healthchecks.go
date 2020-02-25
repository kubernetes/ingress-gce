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
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	computealpha "google.golang.org/api/compute/v0.alpha"
	computebeta "google.golang.org/api/compute/v0.beta"
	"google.golang.org/api/compute/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/ingress-gce/pkg/annotations"
	backendconfigv1 "k8s.io/ingress-gce/pkg/apis/backendconfig/v1"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/loadbalancers/features"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog"
	"k8s.io/legacy-cloud-providers/gce"
)

const (
	// These values set a low health threshold and a high failure threshold.
	// We're just trying to detect if the node networking is
	// borked, service level outages will get detected sooner
	// by kube-proxy.
	// DefaultHealthCheckInterval defines how frequently a probe runs with IG backends
	DefaultHealthCheckInterval = 60 * time.Second
	// DefaultNEGHealthCheckInterval defines how frequently a probe runs with NEG backends
	DefaultNEGHealthCheckInterval = 15 * time.Second
	// DefaultHealthyThreshold defines the threshold of success probes that declare a backend "healthy"
	DefaultHealthyThreshold = 1
	// DefaultUnhealthyThreshold defines the threshold of failure probes that declare a instance "unhealthy"
	DefaultUnhealthyThreshold = 10
	// DefaultNEGUnhealthyThreshold defines the threshold of failure probes that declare a network endpoint "unhealthy"
	// In NEG mode, cloud loadbalancer health check request will no longer be loadbalanced by kube-proxy(iptables).
	// Instead, health checks can reach endpoints directly. Hence the loadbalancer health check can get a clear signal
	// of endpoint health status. As a result, we are able to tune down the unhealthy threshold to 2.
	DefaultNEGUnhealthyThreshold = 2
	// DefaultTimeout defines the timeout of each probe for IG
	DefaultTimeout = 60 * time.Second
	// DefaultNEGTimeout defines the timeout of each probe for NEG
	DefaultNEGTimeout = 15 * time.Second

	// UseServingPortSpecification is a constant for GCE API.
	// USE_SERVING_PORT: For NetworkEndpointGroup, the port specified for
	// each network endpoint is used for health checking. For other
	// backends, the port or named port specified in the Backend Service is
	// used for health checking.
	UseServingPortSpecification = "USE_SERVING_PORT"

	// TODO: revendor the GCE API go client so that this error will not be hit.
	newHealthCheckErrorMessageTemplate = "the %v health check configuration on the existing health check %v is nil. " +
		"This is usually caused by an application protocol change on the k8s service spec. " +
		"Please revert the change on application protocol to avoid this error message."
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
func (h *HealthChecks) new(sp utils.ServicePort) *HealthCheck {
	var hc *HealthCheck
	if sp.NEGEnabled && !sp.L7ILBEnabled {
		hc = DefaultNEGHealthCheck(sp.Protocol)
	} else if sp.L7ILBEnabled {
		hc = defaultILBHealthCheck(sp.Protocol)
	} else {
		hc = DefaultHealthCheck(sp.NodePort, sp.Protocol)
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
		klog.V(4).Infof("Applying httpGet settings of readinessProbe to health check on port %+v", sp)
		applyProbeSettingsToHC(probe, hc)
	}
	var bchcc *backendconfigv1.HealthCheckConfig
	if flags.F.EnableBackendConfigHealthCheck && sp.BackendConfig != nil {
		bchcc = sp.BackendConfig.Spec.HealthCheck
	}
	if bchcc != nil {
		klog.V(2).Infof("ServicePort %v has BackendConfig healthcheck override", sp.ID)
	}
	return h.sync(hc, bchcc)
}

// sync retrieves a health check based on port, checks type and settings and updates/creates if necessary.
// sync is only called by the backends.Add func - it's not a pool like other resources.
func (h *HealthChecks) sync(hc *HealthCheck, bchcc *backendconfigv1.HealthCheckConfig) (string, error) {
	var scope meta.KeyType
	// TODO(shance): find a way to remove this
	if hc.forILB {
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
	klog.V(3).Infof("mergeHealthcheck(%+v,%+v) = %+v", existingHC, premergeHC, hc)
	// Then, BackendConfig will override any fields that are explicitly set.
	if bchcc != nil {
		klog.V(2).Infof("Health check %q has backendconfig override (%+v)", hc.Name, bchcc)
		// BackendConfig healthcheck settings always take precedence.
		hc.updateFromBackendConfig(bchcc)
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
func (h *HealthChecks) createILB(hc *HealthCheck) error {
	compositeType, err := composite.AlphaToHealthCheck(hc.ToAlphaComputeHealthCheck())
	if err != nil {
		return fmt.Errorf("Error converting hc to composite: %v", err)
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
		return fmt.Errorf("Error creating health check %v: %v", compositeType, err)
	}

	return nil
}

func (h *HealthChecks) create(hc *HealthCheck, bchcc *backendconfigv1.HealthCheckConfig) error {
	if bchcc != nil {
		klog.V(2).Infof("Health check %q has backendconfig override (%+v)", hc.Name, bchcc)
		// BackendConfig healthcheck settings always take precedence.
		hc.updateFromBackendConfig(bchcc)
	}
	// special case ILB to avoid mucking with stable HC code
	if hc.forILB {
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
func (h *HealthChecks) updateILB(hc *HealthCheck) error {
	// special case ILB to avoid mucking with stable HC code
	compositeType, err := composite.AlphaToHealthCheck(hc.ToAlphaComputeHealthCheck())
	if err != nil {
		return fmt.Errorf("Error converting newHC to composite: %v", err)
	}
	cloud := h.cloud.(*gce.Cloud)
	key, err := composite.CreateKey(cloud, hc.Name, features.L7ILBScope())

	// Update fields
	compositeType.Version = features.L7ILBVersions().HealthCheck
	compositeType.Region = key.Region

	return composite.UpdateHealthCheck(cloud, key, compositeType)
}

func (h *HealthChecks) update(hc *HealthCheck) error {
	if hc.forILB {
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

// mergeUserSettings merges old health check configuration that the user may
// have customized. This is to preserve the existing health check setting as
// much as possible.
//
// TODO(bowei) -- fix below.
// WARNING: if a service backend is converted from IG mode to NEG mode,
// the existing health check setting will be preserved, although it may not
// suit the customer needs.
//
// TODO(bowei): this is very unstable in combination with Probe, we do not
// have a clear signal as to where the settings are coming from. Once a
// healthcheck is created, it will basically not change.
func mergeUserSettings(existing, newHC *HealthCheck) *HealthCheck {
	hc := *newHC // return a copy

	hc.HTTPHealthCheck = existing.HTTPHealthCheck
	hc.HealthCheck.CheckIntervalSec = existing.HealthCheck.CheckIntervalSec
	hc.HealthCheck.HealthyThreshold = existing.HealthCheck.HealthyThreshold
	hc.HealthCheck.TimeoutSec = existing.HealthCheck.TimeoutSec
	hc.HealthCheck.UnhealthyThreshold = existing.HealthCheck.UnhealthyThreshold

	if existing.HealthCheck.LogConfig != nil {
		l := *existing.HealthCheck.LogConfig
		hc.HealthCheck.LogConfig = &l
	}

	// Cannot specify both portSpecification and port field.
	if hc.forNEG {
		hc.HTTPHealthCheck.Port = 0
		hc.PortSpecification = newHC.PortSpecification
	} else {
		hc.PortSpecification = ""
		hc.Port = newHC.Port
	}
	return &hc
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
		// L7-ILB is the only use of regional right now
		err = composite.DeleteHealthCheck(cloud, key, features.L7ILBVersions().HealthCheck)
		// Ignore error if the deletion candidate is being used by another resource.
		// In most of the cases, this is the associated backend resource itself.
		if utils.IsHTTPErrorCode(err, http.StatusNotFound) || utils.IsInUsedByError(err) {
			klog.V(4).Infof("DeleteRegionalHealthCheck(%s, _): %v, ignorable error", name, err)
			return nil
		}
	}

	klog.V(2).Infof("Deleting health check %v", name)
	// Not using composite here since the tests still rely on the fake health check interface
	if err := h.cloud.DeleteHealthCheck(name); err != nil {
		// Ignore error if the deletion candidate is being used by another resource.
		if utils.IsInUsedByError(err) {
			klog.V(4).Infof("DeleteHealthCheck(%s, _): %v, ignorable error", name, err)
			return nil
		}
		return err
	}
	return nil
}

// TODO(shance): merge with existing hc code
func (h *HealthChecks) getILB(name string) (*HealthCheck, error) {
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

	newHC, err := NewHealthCheck(gceHC)
	if err != nil {
		return nil, err
	}

	// Update fields for future update() calls
	newHC.forILB = true
	newHC.forNEG = true

	return newHC, nil
}

// Get returns the health check by port
func (h *HealthChecks) Get(name string, version meta.Version, scope meta.KeyType) (*HealthCheck, error) {
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
		hc, err = betaToAlphaHealthCheck(betaHC)
	case meta.VersionGA:
		v1hc, err := h.cloud.GetHealthCheck(name)
		if err != nil {
			return nil, err
		}
		hc, err = v1ToAlphaHealthCheck(v1hc)
	default:
		return nil, fmt.Errorf("unknown version %v", version)
	}
	if err != nil {
		return nil, err
	}
	return NewHealthCheck(hc)
}

// DefaultHealthCheck simply returns the default health check.
func DefaultHealthCheck(port int64, protocol annotations.AppProtocol) *HealthCheck {
	klog.V(3).Infof("DefaultHealthCheck(%v, %v)", port, protocol)
	httpSettings := computealpha.HTTPHealthCheck{Port: port}

	hcSettings := computealpha.HealthCheck{
		// How often to health check.
		CheckIntervalSec: int64(DefaultHealthCheckInterval.Seconds()),
		// How long to wait before claiming failure of a health check.
		TimeoutSec: int64(DefaultTimeout.Seconds()),
		// Number of healthchecks to pass for a vm to be deemed healthy.
		HealthyThreshold: DefaultHealthyThreshold,
		// Number of healthchecks to fail before the vm is deemed unhealthy.
		UnhealthyThreshold: DefaultUnhealthyThreshold,
		Description:        "Default kubernetes L7 Loadbalancing health check.",
		Type:               string(protocol),
	}

	return &HealthCheck{
		HTTPHealthCheck: httpSettings,
		HealthCheck:     hcSettings,
		forNEG:          false,
	}
}

// DefaultNEGHealthCheck simply returns the default health check.
func DefaultNEGHealthCheck(protocol annotations.AppProtocol) *HealthCheck {
	httpSettings := computealpha.HTTPHealthCheck{PortSpecification: UseServingPortSpecification}
	klog.V(3).Infof("DefaultNEGHealthCheck(%v)", protocol)

	hcSettings := computealpha.HealthCheck{
		// How often to health check.
		CheckIntervalSec: int64(DefaultNEGHealthCheckInterval.Seconds()),
		// How long to wait before claiming failure of a health check.
		TimeoutSec: int64(DefaultNEGTimeout.Seconds()),
		// Number of healthchecks to pass for a vm to be deemed healthy.
		HealthyThreshold: DefaultHealthyThreshold,
		// Number of healthchecks to fail before the vm is deemed unhealthy.
		UnhealthyThreshold: DefaultNEGUnhealthyThreshold,
		Description:        "Default kubernetes L7 Loadbalancing health check for NEG.",
		Type:               string(protocol),
	}

	return &HealthCheck{
		HTTPHealthCheck: httpSettings,
		HealthCheck:     hcSettings,
		forNEG:          true,
	}
}

func defaultILBHealthCheck(protocol annotations.AppProtocol) *HealthCheck {
	httpSettings := computealpha.HTTPHealthCheck{PortSpecification: UseServingPortSpecification}
	klog.V(3).Infof("DefaultILBHealthCheck(%v)", protocol)

	hcSettings := computealpha.HealthCheck{
		// How often to health check.
		CheckIntervalSec: int64(DefaultNEGHealthCheckInterval.Seconds()),
		// How long to wait before claiming failure of a health check.
		TimeoutSec: int64(DefaultNEGTimeout.Seconds()),
		// Number of healthchecks to pass for a vm to be deemed healthy.
		HealthyThreshold: DefaultHealthyThreshold,
		// Number of healthchecks to fail before the vm is deemed unhealthy.
		UnhealthyThreshold: DefaultNEGUnhealthyThreshold,
		Description:        "Default kubernetes L7 Loadbalancing health check for ILB.",
		Type:               string(protocol),
	}

	return &HealthCheck{
		HTTPHealthCheck: httpSettings,
		HealthCheck:     hcSettings,
		forILB:          true,
		forNEG:          true,
	}
}

// HealthCheck is a wrapper for different versions of the compute struct.
// TODO(bowei): replace inner workings with composite.
type HealthCheck struct {
	// As the {HTTP, HTTPS, HTTP2} settings are identical, we mantain the
	// settings at the outer-level and copy into the appropriate struct
	// in the HealthCheck embedded struct (see `merge()`) when getting the
	// compute struct back.
	computealpha.HTTPHealthCheck
	computealpha.HealthCheck

	forNEG bool
	forILB bool
}

// NewHealthCheck creates a HealthCheck which abstracts nested structs away
func NewHealthCheck(hc *computealpha.HealthCheck) (*HealthCheck, error) {
	// TODO(bowei): should never handle nil like this.
	if hc == nil {
		return nil, errors.New("nil hc to NewHealthCheck")
	}

	v := &HealthCheck{HealthCheck: *hc}
	switch annotations.AppProtocol(hc.Type) {
	case annotations.ProtocolHTTP:
		if hc.HttpHealthCheck == nil {
			return nil, fmt.Errorf(newHealthCheckErrorMessageTemplate, annotations.ProtocolHTTP, hc.Name)
		}
		v.HTTPHealthCheck = *hc.HttpHealthCheck
	case annotations.ProtocolHTTPS:
		if hc.HttpsHealthCheck == nil {
			return nil, fmt.Errorf(newHealthCheckErrorMessageTemplate, annotations.ProtocolHTTPS, hc.Name)
		}
		v.HTTPHealthCheck = computealpha.HTTPHealthCheck(*hc.HttpsHealthCheck)
	case annotations.ProtocolHTTP2:
		if hc.Http2HealthCheck == nil {
			return nil, fmt.Errorf(newHealthCheckErrorMessageTemplate, annotations.ProtocolHTTP2, hc.Name)
		}
		v.HTTPHealthCheck = computealpha.HTTPHealthCheck(*hc.Http2HealthCheck)
	}

	// Users should be modifying HTTP(S) specific settings on the embedded
	// HTTPHealthCheck. Setting these to nil for preventing confusion.
	v.HealthCheck.HttpHealthCheck = nil
	v.HealthCheck.HttpsHealthCheck = nil
	v.HealthCheck.Http2HealthCheck = nil

	return v, nil
}

// Protocol returns the type cased to AppProtocol
func (hc *HealthCheck) Protocol() annotations.AppProtocol {
	return annotations.AppProtocol(hc.Type)
}

// ToComputeHealthCheck returns a valid compute.HealthCheck object
func (hc *HealthCheck) ToComputeHealthCheck() (*compute.HealthCheck, error) {
	hc.merge()
	return toV1HealthCheck(&hc.HealthCheck)
}

// ToBetaComputeHealthCheck returns a valid computebeta.HealthCheck object
func (hc *HealthCheck) ToBetaComputeHealthCheck() (*computebeta.HealthCheck, error) {
	hc.merge()
	return toBetaHealthCheck(&hc.HealthCheck)
}

// ToAlphaComputeHealthCheck returns a valid computealpha.HealthCheck object
func (hc *HealthCheck) ToAlphaComputeHealthCheck() *computealpha.HealthCheck {
	hc.merge()
	x := hc.HealthCheck // Make a copy to ensure no aliasing.
	return &x
}

func (hc *HealthCheck) merge() {
	// Cannot specify both portSpecification and port field.
	if hc.PortSpecification != "" {
		hc.Port = 0
	}

	// Zeroing out child settings as a precaution. GoogleAPI throws an error
	// if the wrong child struct is set.
	hc.HealthCheck.Http2HealthCheck = nil
	hc.HealthCheck.HttpsHealthCheck = nil
	hc.HealthCheck.HttpHealthCheck = nil

	switch hc.Protocol() {
	case annotations.ProtocolHTTP:
		x := hc.HTTPHealthCheck // Make a copy to ensure no aliasing.
		hc.HealthCheck.HttpHealthCheck = &x
	case annotations.ProtocolHTTPS:
		https := computealpha.HTTPSHealthCheck(hc.HTTPHealthCheck)
		hc.HealthCheck.HttpsHealthCheck = &https
	case annotations.ProtocolHTTP2:
		http2 := computealpha.HTTP2HealthCheck(hc.HTTPHealthCheck)
		hc.HealthCheck.Http2HealthCheck = &http2
	}
}

// Version returns the appropriate API version to handle the health check
// Use Beta API for NEG as PORT_SPECIFICATION is required, and HTTP2
func (hc *HealthCheck) Version() meta.Version {
	if hc.forILB {
		return features.L7ILBVersions().HealthCheck
	}
	if hc.Protocol() == annotations.ProtocolHTTP2 || hc.forNEG {
		return meta.VersionBeta
	}
	return meta.VersionGA
}

func (hc *HealthCheck) updateFromBackendConfig(c *backendconfigv1.HealthCheckConfig) {
	if c.CheckIntervalSec != nil {
		hc.CheckIntervalSec = *c.CheckIntervalSec
	}
	if c.TimeoutSec != nil {
		hc.TimeoutSec = *c.TimeoutSec
	}
	if c.HealthyThreshold != nil {
		hc.HealthyThreshold = *c.HealthyThreshold
	}
	if c.UnhealthyThreshold != nil {
		hc.UnhealthyThreshold = *c.UnhealthyThreshold
	}
	if c.Type != nil {
		hc.Type = *c.Type
	}
	if c.RequestPath != nil {
		hc.RequestPath = *c.RequestPath
	}
	if c.Port != nil {
		klog.Warningf("Setting Port is not supported (healthcheck %q, backendconfig = %+v)", hc.Name, c)
	}
}

// fieldDiffs encapsulate which fields are different between health checks.
type fieldDiffs struct {
	f []string
}

func (c *fieldDiffs) add(field, oldv, newv string) {
	c.f = append(c.f, fmt.Sprintf("%s:%s -> %s", field, oldv, newv))
}
func (c *fieldDiffs) String() string { return strings.Join(c.f, ", ") }
func (c *fieldDiffs) hasDiff() bool  { return len(c.f) > 0 }

func calculateDiff(old, new *HealthCheck, c *backendconfigv1.HealthCheckConfig) *fieldDiffs {
	var changes fieldDiffs

	if old.Protocol() != new.Protocol() {
		changes.add("Protocol", string(old.Protocol()), string(new.Protocol()))
	}
	if old.PortSpecification != new.PortSpecification {
		changes.add("PortSpecification", old.PortSpecification, new.PortSpecification)
	}

	// TODO(bowei): why don't we check Port, timeout etc.

	if c == nil {
		return &changes
	}

	// This code assumes that the changes wrt to `c` has been applied to `new`.
	if c.CheckIntervalSec != nil && old.CheckIntervalSec != new.CheckIntervalSec {
		changes.add("CheckIntervalSec", strconv.FormatInt(old.CheckIntervalSec, 10), strconv.FormatInt(new.CheckIntervalSec, 10))
	}
	if c.TimeoutSec != nil && old.TimeoutSec != new.TimeoutSec {
		changes.add("TimeoutSec", strconv.FormatInt(old.TimeoutSec, 10), strconv.FormatInt(new.TimeoutSec, 10))
	}
	if c.HealthyThreshold != nil && old.HealthyThreshold != new.HealthyThreshold {
		changes.add("HeathyThreshold", strconv.FormatInt(old.HealthyThreshold, 10), strconv.FormatInt(new.HealthyThreshold, 10))
	}
	if c.UnhealthyThreshold != nil && old.UnhealthyThreshold != new.UnhealthyThreshold {
		changes.add("UnhealthyThreshold", strconv.FormatInt(old.UnhealthyThreshold, 10), strconv.FormatInt(new.UnhealthyThreshold, 10))
	}
	// c.Type is handled by Protocol above.
	if c.RequestPath != nil && old.RequestPath != new.RequestPath {
		changes.add("RequestPath", old.RequestPath, new.RequestPath)
	}
	// TODO(bowei): Host seems to be missing.

	return &changes
}

// toV1HealthCheck converts alpha health check to v1 health check.
// WARNING: alpha health check has a additional PORT_SPECIFICATION field.
// This field will be omitted after conversion.
func toV1HealthCheck(hc *computealpha.HealthCheck) (*compute.HealthCheck, error) {
	ret := &compute.HealthCheck{}
	err := copyViaJSON(ret, hc)
	return ret, err
}

// toBetaHealthCheck converts alpha health check to beta health check.
func toBetaHealthCheck(hc *computealpha.HealthCheck) (*computebeta.HealthCheck, error) {
	ret := &computebeta.HealthCheck{}
	err := copyViaJSON(ret, hc)
	return ret, err
}

// v1ToAlphaHealthCheck converts v1 health check to alpha health check.
// There should be no information lost after conversion.
func v1ToAlphaHealthCheck(hc *compute.HealthCheck) (*computealpha.HealthCheck, error) {
	ret := &computealpha.HealthCheck{}
	err := copyViaJSON(ret, hc)
	return ret, err
}

// betaToAlphaHealthCheck converts beta health check to alpha health check.
// There should be no information lost after conversion.
func betaToAlphaHealthCheck(hc *computebeta.HealthCheck) (*computealpha.HealthCheck, error) {
	ret := &computealpha.HealthCheck{}
	err := copyViaJSON(ret, hc)
	return ret, err
}

// pathFromSvcPort returns the default path for a health check based on whether
// the passed in ServicePort is associated with the system default backend.
func (h *HealthChecks) pathFromSvcPort(sp utils.ServicePort) string {
	if h.defaultBackendSvc == sp.ID.Service {
		return flags.F.DefaultSvcHealthCheckPath
	}
	return h.path
}

type jsonConvertable interface {
	MarshalJSON() ([]byte, error)
}

func copyViaJSON(dest interface{}, src jsonConvertable) error {
	var err error
	bytes, err := src.MarshalJSON()
	if err != nil {
		return err
	}
	return json.Unmarshal(bytes, dest)
}

// applyProbeSettingsToHC takes the Pod healthcheck settings and applies it
// to the healthcheck.
//
// TODO: what if the port changes?
// TODO: does not handle protocol?
func applyProbeSettingsToHC(p *v1.Probe, hc *HealthCheck) {
	if p.Handler.HTTPGet == nil {
		return
	}

	healthPath := p.Handler.HTTPGet.Path
	// GCE requires a leading "/" for health check urls.
	if !strings.HasPrefix(healthPath, "/") {
		healthPath = "/" + healthPath
	}
	hc.RequestPath = healthPath

	// Extract host from HTTP headers
	host := p.Handler.HTTPGet.Host
	for _, header := range p.Handler.HTTPGet.HTTPHeaders {
		if header.Name == "Host" {
			host = header.Value
			break
		}
	}
	hc.Host = host

	hc.TimeoutSec = int64(p.TimeoutSeconds)
	if hc.forNEG {
		// For NEG mode, we can support more aggressive healthcheck interval.
		hc.CheckIntervalSec = int64(p.PeriodSeconds)
	} else {
		// For IG mode, short healthcheck interval may health check flooding problem.
		hc.CheckIntervalSec = int64(p.PeriodSeconds) + int64(DefaultHealthCheckInterval.Seconds())
	}

	hc.Description = "Kubernetes L7 health check generated with readiness probe settings."
}
