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
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/loadbalancers/features"
	"net/http"
	"time"

	computealpha "google.golang.org/api/compute/v0.alpha"
	computebeta "google.golang.org/api/compute/v0.beta"
	"google.golang.org/api/compute/v1"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog"
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
	// DefaultTimeout defines the timeout of each probe for NEG
	DefaultNEGTimeout = 15 * time.Second

	// This is a constant for GCE API.
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
	compositeCloud *composite.Cloud
	// TODO: (shance) find a way to remove this
	cloud HealthCheckProvider
	// path is the default health check path for backends.
	path string
	// defaultBackend is the default health check path for the default backend.
	defaultBackendPath string
	namer              *utils.Namer
	// This is a workaround which allows us to not have to maintain
	// a separate health checker for the default backend.
	defaultBackendSvc types.NamespacedName
}

// NewHealthChecker creates a new health checker.
// cloud: the cloud object implementing SingleHealthCheck.
// defaultHealthCheckPath: is the HTTP path to use for health checks.
func NewHealthChecker(cloud HealthCheckProvider, healthCheckPath string, defaultBackendHealthCheckPath string, namer *utils.Namer, defaultBackendSvc types.NamespacedName) HealthChecker {
	return &HealthChecks{composite.NewCloud(cloud), cloud, healthCheckPath, defaultBackendHealthCheckPath, namer, defaultBackendSvc}
}

// New returns a *HealthCheck with default settings and specified port/protocol
func (h *HealthChecks) New(sp utils.ServicePort) *HealthCheck {
	var hc *HealthCheck
	if sp.NEGEnabled && !sp.ILBEnabled {
		hc = DefaultNEGHealthCheck(sp.Protocol)
	} else if sp.ILBEnabled {
		hc = DefaultILBHealthCheck(sp.Protocol, h.compositeCloud.GceCloud().Region())
	} else {
		hc = DefaultHealthCheck(sp.NodePort, sp.Protocol)
	}
	// port is the key for retrieving existing health-check
	// TODO: rename backend-service and health-check to not use port as key
	hc.Name = sp.BackendName(h.namer)
	hc.Port = sp.NodePort
	hc.RequestPath = h.pathFromSvcPort(sp)

	//klog.V(3).Infof("Created new health check %v", pretty.Sprint(hc))
	return hc
}

// Sync retrieves a health check based on port, checks type and settings and updates/creates if necessary.
// Sync is only called by the backends.Add func - it's not a pool like other resources.
func (h *HealthChecks) Sync(hc *HealthCheck) (string, error) {
	//klog.V(3).Infof("HealthChecks.Sync(%v)", pretty.Sprint(hc))

	existingHC, err := h.Get(hc.Name, hc.Version(), hc.ForILB)
	if err != nil {
		if !utils.IsHTTPErrorCode(err, http.StatusNotFound) {
			return "", err
		}

		if err = h.create(hc); err != nil {
			return "", err
		}

		return h.getHealthCheckLink(hc.Name, hc.Version(), hc.ForILB)
	}

	if needToUpdate(existingHC, hc) {
		err = h.update(existingHC, hc)
		return existingHC.SelfLink, err
	}

	//klog.V(3).Infof("Sync -> ExistingHC = %v", pretty.Sprint(existingHC))
	if existingHC.RequestPath != hc.RequestPath {
		// TODO: reconcile health checks, and compare headers interval etc.
		// Currently Ingress doesn't expose all the health check params
		// natively, so some users prefer to hand modify the check.
		klog.V(2).Infof("Unexpected request path on health check %v, has %v want %v, NOT reconciling", hc.Name, existingHC.RequestPath, hc.RequestPath)
	} else {
		klog.V(2).Infof("Health check %v already exists and has the expected path %v", hc.Name, hc.RequestPath)
	}

	return existingHC.SelfLink, nil
}

func (h *HealthChecks) create(hc *HealthCheck) error {
	// special case ILB to avoid mucking with stable HC code
	if hc.ForILB {
		if len(hc.PortSpecification) > 0 {
			hc.Port = 0
		}
		hc.merge()
		compositeType, err := composite.ToHealthCheck(hc)
		if err != nil {
			return fmt.Errorf("Error converting hc to composite: %v", err)
		}
		key := h.compositeCloud.CreateKey(hc.Name, true)
		compositeType.Version = features.ILBVersion
		compositeType.Region = key.Region

		err = h.compositeCloud.CreateHealthCheck(compositeType, key)
		if err != nil {
			return fmt.Errorf("Error creating health check %v: %v", compositeType, err)
		}
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

func (h *HealthChecks) update(oldHC, newHC *HealthCheck) error {
	// special case ILB to avoid mucking with stable HC code
	//klog.V(3).Infof("Updating HC, old = %v, new = %v", pretty.Sprint(oldHC), pretty.Sprint(newHC))

	if oldHC.ForILB {
		compositeType, err := composite.ToHealthCheck(newHC)
		if err != nil {
			return fmt.Errorf("Error converting newHC to composite: %v", err)
		}
		key := h.compositeCloud.CreateKey(newHC.Name, true)
		compositeType.Version = features.ILBVersion
		compositeType.Region = key.Region
		compositeType.HttpHealthCheck.Port = 0
		compositeType.HttpHealthCheck.PortSpecification = oldHC.HttpHealthCheck.PortSpecification
		//compositeType.HttpHealthCheck.RequestPath = oldHC.HttpHealthCheck.RequestPath
		return h.compositeCloud.UpdateHealthCheck(compositeType, key)
	}

	switch newHC.Version() {
	case meta.VersionAlpha:
		klog.V(2).Infof("Updating alpha health check with protocol %v", newHC.Type)
		return h.cloud.UpdateAlphaHealthCheck(mergeHealthcheck(oldHC, newHC).ToAlphaComputeHealthCheck())
	case meta.VersionBeta:
		klog.V(2).Infof("Updating beta health check with protocol %v", newHC.Type)
		betaHC, err := mergeHealthcheck(oldHC, newHC).ToBetaComputeHealthCheck()
		if err != nil {
			return err
		}
		return h.cloud.UpdateBetaHealthCheck(betaHC)
	case meta.VersionGA:
		klog.V(2).Infof("Updating health check for port %v with protocol %v", newHC.Port, newHC.Type)
		v1hc, err := newHC.ToComputeHealthCheck()
		if err != nil {
			return err
		}
		return h.cloud.UpdateHealthCheck(v1hc)
	default:
		return fmt.Errorf("unknown Version: %q", newHC.Version())

	}
}

// mergeHealthcheck merges old health check configuration (potentially for IG) with the new one.
// This is to preserve the existing health check setting as much as possible.
// WARNING: if a service backend is converted from IG mode to NEG mode,
// the existing health check setting will be preserve, although it may not suit the customer needs.
func mergeHealthcheck(oldHC, newHC *HealthCheck) *HealthCheck {
	portSpec := newHC.PortSpecification
	port := newHC.Port
	newHC.HTTPHealthCheck = oldHC.HTTPHealthCheck

	// Cannot specify both portSpecification and port field.
	if newHC.ForNEG {
		newHC.HTTPHealthCheck.Port = 0
		newHC.PortSpecification = portSpec
	} else {
		newHC.PortSpecification = ""
		newHC.Port = port
	}
	return newHC
}

func (h *HealthChecks) getHealthCheckLink(name string, version meta.Version, regional bool) (string, error) {
	hc, err := h.Get(name, version, regional)
	if err != nil {
		return "", err
	}
	return hc.SelfLink, nil
}

// Delete deletes the health check by port.
func (h *HealthChecks) Delete(name string, regional bool) error {
	if regional {
		return h.compositeCloud.DeleteHealthCheck(features.ILBVersion, h.compositeCloud.CreateKey(name, true))
	}

	klog.V(2).Infof("Deleting health check %v", name)
	return h.cloud.DeleteHealthCheck(name)
}

// Get returns the health check by port
func (h *HealthChecks) Get(name string, version meta.Version, regional bool) (*HealthCheck, error) {
	klog.V(3).Infof("Getting Health Check, name: %s, version: %v, regional: %v", name, version, regional)

	if regional {
		hc, err := h.compositeCloud.GetHealthCheck(meta.VersionAlpha, h.compositeCloud.CreateKey(name, true))
		if err != nil {
			return nil, err
		}
		alphaHC, err := hc.ToAlpha()
		if err != nil {
			return nil, err
		}
		return NewHealthCheck(alphaHC)
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
		ForNEG:          false,
	}
}

// DefaultHealthCheck simply returns the default health check.
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
		ForNEG:          true,
	}
}

func DefaultILBHealthCheck(protocol annotations.AppProtocol, region string) *HealthCheck {
	httpSettings := computealpha.HTTPHealthCheck{PortSpecification: UseServingPortSpecification}
	klog.V(3).Infof("DefaultILBHealthCheck(%v, %v)", protocol, region)

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
		ForILB:          true,
		ForNEG:          true,
	}
}

// HealthCheck embeds two types - the generic healthcheck compute.HealthCheck
// and the HTTP settings compute.HTTPHealthCheck. By embedding both, consumers can modify
// all relevant settings (HTTP specific and HealthCheck generic) regardless of Type
// Consumers should call .Out() func to generate a compute.HealthCheck
// with the proper child struct (.HttpHealthCheck, .HttpshealthCheck, etc).
type HealthCheck struct {
	computealpha.HTTPHealthCheck
	computealpha.HealthCheck
	//compositeHealthCheck composite.HealthCheck
	ForNEG bool
	// ForILB designates whether the health check is for an ILB
	// This means that the HC should be alpha + regional
	ForILB bool
}

// NewHealthCheck creates a HealthCheck which abstracts nested structs away
func NewHealthCheck(hc *computealpha.HealthCheck) (*HealthCheck, error) {
	if hc == nil {
		return nil, nil
	}

	v := &HealthCheck{HealthCheck: *hc}
	var err error
	switch annotations.AppProtocol(hc.Type) {
	case annotations.ProtocolHTTP:
		if hc.HttpHealthCheck == nil {
			err = fmt.Errorf(newHealthCheckErrorMessageTemplate, annotations.ProtocolHTTP, hc.Name)
			return nil, err
		}
		v.HTTPHealthCheck = *hc.HttpHealthCheck
	case annotations.ProtocolHTTPS:
		if hc.HttpsHealthCheck == nil {
			err = fmt.Errorf(newHealthCheckErrorMessageTemplate, annotations.ProtocolHTTPS, hc.Name)
			return nil, err
		}
		v.HTTPHealthCheck = computealpha.HTTPHealthCheck(*hc.HttpsHealthCheck)
	case annotations.ProtocolHTTP2:
		if hc.Http2HealthCheck == nil {
			err = fmt.Errorf(newHealthCheckErrorMessageTemplate, annotations.ProtocolHTTP2, hc.Name)
			return nil, err
		}
		v.HTTPHealthCheck = computealpha.HTTPHealthCheck(*hc.Http2HealthCheck)
	}

	// Users should be modifying HTTP(S) specific settings on the embedded
	// HTTPHealthCheck. Setting these to nil for preventing confusion.
	v.HealthCheck.HttpHealthCheck = nil
	v.HealthCheck.HttpsHealthCheck = nil
	v.HealthCheck.Http2HealthCheck = nil

	return v, err
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
	// Cannot specify both portSpecification and port field.
	if len(hc.PortSpecification) > 0 {
		hc.Port = 0
	}
	hc.merge()
	return toBetaHealthCheck(&hc.HealthCheck)
}

// ToAlphaComputeHealthCheck returns a valid computealpha.HealthCheck object
func (hc *HealthCheck) ToAlphaComputeHealthCheck() *computealpha.HealthCheck {
	// Cannot specify both portSpecification and port field.
	if len(hc.PortSpecification) > 0 {
		hc.Port = 0
	}
	hc.merge()
	return &hc.HealthCheck
}

func (hc *HealthCheck) merge() {
	// Zeroing out child settings as a precaution. GoogleAPI throws an error
	// if the wrong child struct is set.
	hc.HealthCheck.Http2HealthCheck = nil
	hc.HealthCheck.HttpsHealthCheck = nil
	hc.HealthCheck.HttpHealthCheck = nil

	switch hc.Protocol() {
	case annotations.ProtocolHTTP:
		hc.HealthCheck.HttpHealthCheck = &hc.HTTPHealthCheck
	case annotations.ProtocolHTTPS:
		https := computealpha.HTTPSHealthCheck(hc.HTTPHealthCheck)
		hc.HealthCheck.HttpsHealthCheck = &https
	case annotations.ProtocolHTTP2:
		http2 := computealpha.HTTP2HealthCheck(hc.HTTPHealthCheck)
		hc.HealthCheck.Http2HealthCheck = &http2
	}
}

func (hc *HealthCheck) isHttp2() bool {
	return hc.Protocol() == annotations.ProtocolHTTP2
}

// Version returns the appropriate API version to handle the health check
// Use Beta API for NEG as PORT_SPECIFICATION is required, and HTTP2
func (hc *HealthCheck) Version() meta.Version {
	if hc.isHttp2() || (hc.ForNEG && !hc.ForILB) {
		return meta.VersionBeta
	} else if hc.ForILB {
		return features.ILBVersion
	}
	return meta.VersionGA
}

func needToUpdate(old, new *HealthCheck) bool {
	if old.Protocol() != new.Protocol() {
		klog.V(2).Infof("Updating health check %v because it has protocol %v but need %v", old.Name, old.Type, new.Type)
		return true
	}

	if old.PortSpecification != new.PortSpecification {
		klog.V(2).Infof("Updating health check %v because it has port specification %q but need %q", old.Name, old.PortSpecification, new.PortSpecification)
		return true
	}
	return false
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
		return h.defaultBackendPath
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
