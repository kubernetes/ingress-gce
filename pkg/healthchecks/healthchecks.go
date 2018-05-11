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
	"net/http"
	"time"

	computealpha "google.golang.org/api/compute/v0.alpha"
	compute "google.golang.org/api/compute/v1"

	"github.com/golang/glog"

	"encoding/json"

	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/utils"
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
)

// HealthChecks manages health checks.
type HealthChecks struct {
	cloud       HealthCheckProvider
	defaultPath string
	namer       *utils.Namer
}

// NewHealthChecker creates a new health checker.
// cloud: the cloud object implementing SingleHealthCheck.
// defaultHealthCheckPath: is the HTTP path to use for health checks.
func NewHealthChecker(cloud HealthCheckProvider, defaultHealthCheckPath string, namer *utils.Namer) HealthChecker {
	return &HealthChecks{cloud, defaultHealthCheckPath, namer}
}

// New returns a *HealthCheck with default settings and specified port/protocol
func (h *HealthChecks) New(name string, port int64, protocol annotations.AppProtocol, enableNEG bool) *HealthCheck {
	var hc *HealthCheck
	if enableNEG {
		hc = DefaultNEGHealthCheck(protocol)
	} else {
		hc = DefaultHealthCheck(port, protocol)
	}

	hc.Name = name
	return hc
}

// Sync retrieves a health check based on port, checks type and settings and updates/creates if necessary.
// Sync is only called by the backends.Add func - it's not a pool like other resources.
func (h *HealthChecks) Sync(hc *HealthCheck) (string, error) {
	// Verify default path
	if hc.RequestPath == "" {
		hc.RequestPath = h.defaultPath
	}

	// Use alpha API when PORT_SPECIFICATION field is specified or when Type
	// is HTTP2
	existingHC, err := h.Get(hc.Name, hc.isHttp2() || hc.ForNEG)
	if err != nil {
		if !utils.IsHTTPErrorCode(err, http.StatusNotFound) {
			return "", err
		}

		if err = h.create(hc); err != nil {
			return "", err
		}

		return h.getHealthCheckLink(hc.Name, hc.isHttp2())
	}

	if needToUpdate(existingHC, hc) {
		err = h.update(existingHC, hc)
		return existingHC.SelfLink, err
	}

	if existingHC.RequestPath != hc.RequestPath {
		// TODO: reconcile health checks, and compare headers interval etc.
		// Currently Ingress doesn't expose all the health check params
		// natively, so some users prefer to hand modify the check.
		glog.V(2).Infof("Unexpected request path on health check %v, has %v want %v, NOT reconciling", hc.Name, existingHC.RequestPath, hc.RequestPath)
	} else {
		glog.V(2).Infof("Health check %v already exists and has the expected path %v", hc.Name, hc.RequestPath)
	}

	return existingHC.SelfLink, nil
}

func (h *HealthChecks) create(hc *HealthCheck) error {
	if hc.isHttp2() || hc.ForNEG {
		glog.V(2).Infof("Creating health check with protocol %v", hc.Type)
		return h.cloud.CreateAlphaHealthCheck(hc.ToAlphaComputeHealthCheck())
	} else {
		glog.V(2).Infof("Creating health check for port %v with protocol %v", hc.Port, hc.Type)
		v1hc, err := hc.ToComputeHealthCheck()
		if err != nil {
			return err
		}
		return h.cloud.CreateHealthCheck(v1hc)
	}
}

func (h *HealthChecks) update(oldHC, newHC *HealthCheck) error {
	if newHC.isHttp2() || newHC.ForNEG {
		glog.V(2).Infof("Updating health check with protocol %v, ForNEG: %v", newHC.Type, newHC.ForNEG)
		return h.cloud.UpdateAlphaHealthCheck(mergeHealthcheck(oldHC, newHC).ToAlphaComputeHealthCheck())
	} else {
		glog.V(2).Infof("Updating health check for port %v with protocol %v", newHC.Port, newHC.Type)
		v1hc, err := newHC.ToComputeHealthCheck()
		if err != nil {
			return err
		}
		return h.cloud.UpdateHealthCheck(v1hc)
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

func (h *HealthChecks) getHealthCheckLink(name string, alpha bool) (string, error) {
	hc, err := h.Get(name, alpha)
	if err != nil {
		return "", err
	}
	return hc.SelfLink, nil
}

// Delete deletes the health check by port.
func (h *HealthChecks) Delete(name string) error {
	glog.V(2).Infof("Deleting health check %v", name)
	return h.cloud.DeleteHealthCheck(name)
}

// Get returns the health check by port
func (h *HealthChecks) Get(name string, alpha bool) (*HealthCheck, error) {
	var hc *computealpha.HealthCheck
	var err error
	if alpha {
		hc, err = h.cloud.GetAlphaHealthCheck(name)
	} else {
		var v1hc *compute.HealthCheck
		v1hc, err = h.cloud.GetHealthCheck(name)
		if err != nil {
			return nil, err
		}
		hc, err = toAlphaHealthCheck(v1hc)
	}
	return NewHealthCheck(hc), err
}

// GetLegacy deletes legacy HTTP health checks
func (h *HealthChecks) GetLegacy(port int64) (*compute.HttpHealthCheck, error) {
	name := h.namer.IGBackend(port)
	return h.cloud.GetHttpHealthCheck(name)
}

// DeleteLegacy deletes legacy HTTP health checks
func (h *HealthChecks) DeleteLegacy(port int64) error {
	name := h.namer.IGBackend(port)
	glog.V(2).Infof("Deleting legacy HTTP health check %v", name)
	return h.cloud.DeleteHttpHealthCheck(name)
}

// DefaultHealthCheck simply returns the default health check.
func DefaultHealthCheck(port int64, protocol annotations.AppProtocol) *HealthCheck {
	httpSettings := computealpha.HTTPHealthCheck{
		Port: port,
		// Empty string is used as a signal to the caller to use the appropriate
		// default.
		RequestPath: "",
	}

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
	httpSettings := computealpha.HTTPHealthCheck{
		PortSpecification: UseServingPortSpecification,
		// Empty string is used as a signal to the caller to use the appropriate
		// default.
		RequestPath: "",
	}

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

// HealthCheck embeds two types - the generic healthcheck compute.HealthCheck
// and the HTTP settings compute.HTTPHealthCheck. By embedding both, consumers can modify
// all relevant settings (HTTP specific and HealthCheck generic) regardless of Type
// Consumers should call .Out() func to generate a compute.HealthCheck
// with the proper child struct (.HttpHealthCheck, .HttpshealthCheck, etc).
type HealthCheck struct {
	computealpha.HTTPHealthCheck
	computealpha.HealthCheck
	ForNEG bool
}

// NewHealthCheck creates a HealthCheck which abstracts nested structs away
func NewHealthCheck(hc *computealpha.HealthCheck) *HealthCheck {
	if hc == nil {
		return nil
	}

	v := &HealthCheck{HealthCheck: *hc}
	switch annotations.AppProtocol(hc.Type) {
	case annotations.ProtocolHTTP:
		v.HTTPHealthCheck = *hc.HttpHealthCheck
	case annotations.ProtocolHTTPS:
		v.HTTPHealthCheck = computealpha.HTTPHealthCheck(*hc.HttpsHealthCheck)
	case annotations.ProtocolHTTP2:
		v.HTTPHealthCheck = computealpha.HTTPHealthCheck(*hc.Http2HealthCheck)
	}

	// Users should be modifying HTTP(S) specific settings on the embedded
	// HTTPHealthCheck. Setting these to nil for preventing confusion.
	v.HealthCheck.HttpHealthCheck = nil
	v.HealthCheck.HttpsHealthCheck = nil
	v.HealthCheck.Http2HealthCheck = nil

	return v
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

// ToComputeHealthCheck returns a valid compute.HealthCheck object
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

func needToUpdate(old, new *HealthCheck) bool {
	if old.Protocol() != new.Protocol() {
		glog.V(2).Infof("Updating health check %v because it has protocol %v but need %v", old.Name, old.Type, new.Type)
		return true
	}

	if old.PortSpecification != new.PortSpecification {
		glog.V(2).Infof("Updating health check %v because it has port specification %q but need %q", old.Name, old.PortSpecification, new.PortSpecification)
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

// toV1HealthCheck converts v1 health check to alpha health check.
// There should be no information lost after conversion.
func toAlphaHealthCheck(hc *compute.HealthCheck) (*computealpha.HealthCheck, error) {
	ret := &computealpha.HealthCheck{}
	err := copyViaJSON(ret, hc)
	return ret, err
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
