/*
Copyright 2020 The Kubernetes Authors.

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
	"strconv"
	"strings"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	computealpha "google.golang.org/api/compute/v0.alpha"
	computebeta "google.golang.org/api/compute/v0.beta"
	"google.golang.org/api/compute/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/ingress-gce/pkg/annotations"
	backendconfigv1 "k8s.io/ingress-gce/pkg/apis/backendconfig/v1"
	"k8s.io/ingress-gce/pkg/loadbalancers/features"
	"k8s.io/klog"
)

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
	// Cannot specify both portSpecification and port field unless fixed port is specified.
	// This can happen if the user overrides the port using backendconfig
	if hc.PortSpecification != "" && hc.PortSpecification != "USE_FIXED_PORT" {
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
		hc.Port = *c.Port
		// This override is necessary regardless of type
		hc.PortSpecification = "USE_FIXED_PORT"
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
	if c.Port != nil && old.Port != new.Port {
		changes.add("Port", strconv.FormatInt(old.Port, 10), strconv.FormatInt(new.Port, 10))
	}

	// TODO(bowei): Host seems to be missing.

	return &changes
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
