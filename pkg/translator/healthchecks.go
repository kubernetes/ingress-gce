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

package translator

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	computealpha "google.golang.org/api/compute/v0.alpha"
	computebeta "google.golang.org/api/compute/v0.beta"
	"google.golang.org/api/compute/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/ingress-gce/pkg/annotations"
	backendconfigv1 "k8s.io/ingress-gce/pkg/apis/backendconfig/v1"
	"k8s.io/ingress-gce/pkg/loadbalancers/features"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog"
)

const (
	// These values set a low health threshold and a high failure threshold.
	// We're just trying to detect if the node networking is
	// borked, service level outages will get detected sooner
	// by kube-proxy.

	// defaultHealthCheckInterval defines how frequently a probe runs with IG backends
	defaultHealthCheckInterval = 60 * time.Second
	// defaultNEGHealthCheckInterval defines how frequently a probe runs with NEG backends
	defaultNEGHealthCheckInterval = 15 * time.Second
	// defaultHealthyThreshold defines the threshold of success probes that declare a backend "healthy"
	defaultHealthyThreshold = 1
	// defaultUnhealthyThreshold defines the threshold of failure probes that declare a instance "unhealthy"
	defaultUnhealthyThreshold = 10
	// defaultNEGUnhealthyThreshold defines the threshold of failure probes that declare a network endpoint "unhealthy"
	// In NEG mode, cloud loadbalancer health check request will no longer be loadbalanced by kube-proxy(iptables).
	// Instead, health checks can reach endpoints directly. Hence the loadbalancer health check can get a clear signal
	// of endpoint health status. As a result, we are able to tune down the unhealthy threshold to 2.
	defaultNEGUnhealthyThreshold = 2
	// defaultTimeout defines the timeout of each probe for IG
	defaultTimeout = 60 * time.Second
	// defaultNEGTimeout defines the timeout of each probe for NEG
	defaultNEGTimeout = 15 * time.Second

	// useServingPortSpecification is a constant for GCE API.
	// USE_SERVING_PORT: For NetworkEndpointGroup, the port specified for
	// each network endpoint is used for health checking. For other
	// backends, the port or named port specified in the Backend Service is
	// used for health checking.
	useServingPortSpecification = "USE_SERVING_PORT"

	// TODO: revendor the GCE API go client so that this error will not be hit.
	newHealthCheckErrorMessageTemplate = "the %v health check configuration on the existing health check %v is nil. " +
		"This is usually caused by an application protocol change on the k8s service spec. " +
		"Please revert the change on application protocol to avoid this error message."
)

// HealthCheck is a wrapper for different versions of the compute struct.
// TODO(bowei): replace inner workings with composite.
type HealthCheck struct {
	ForNEG bool
	ForILB bool

	// As the {HTTP, HTTPS, HTTP2} settings are identical, we mantain the
	// settings at the outer-level and copy into the appropriate struct
	// in the HealthCheck embedded struct (see `merge()`) when getting the
	// compute struct back.
	computealpha.HTTPHealthCheck
	computealpha.HealthCheck
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
	if err := hc.merge(); err != nil {
		return nil, err
	}
	return utils.ToV1HealthCheck(&hc.HealthCheck)
}

// ToBetaComputeHealthCheck returns a valid computebeta.HealthCheck object
func (hc *HealthCheck) ToBetaComputeHealthCheck() (*computebeta.HealthCheck, error) {
	if err := hc.merge(); err != nil {
		return nil, err
	}
	return utils.ToBetaHealthCheck(&hc.HealthCheck)
}

// ToAlphaComputeHealthCheck returns a valid computealpha.HealthCheck object
func (hc *HealthCheck) ToAlphaComputeHealthCheck() (*computealpha.HealthCheck, error) {
	if err := hc.merge(); err != nil {
		return nil, err
	}
	x := hc.HealthCheck // Make a copy to ensure no aliasing.
	return &x, nil
}

func (hc *HealthCheck) merge() error {
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
	default:
		return fmt.Errorf("Protocol %q is not valid, must be one of [%q,%q,%q]",
			hc.Protocol(), annotations.ProtocolHTTP, annotations.ProtocolHTTPS, annotations.ProtocolHTTP2,
		)
	}
	return nil
}

// Version returns the appropriate API version to handle the health check
// Use Beta API for NEG as PORT_SPECIFICATION is required, and HTTP2
func (hc *HealthCheck) Version() meta.Version {
	if hc.ForILB {
		return features.L7ILBVersions().HealthCheck
	}
	if hc.Protocol() == annotations.ProtocolHTTP2 || hc.ForNEG {
		return meta.VersionBeta
	}
	return meta.VersionGA
}

func (hc *HealthCheck) UpdateFromBackendConfig(c *backendconfigv1.HealthCheckConfig) {
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

// DefaultHealthCheck simply returns the default health check.
func DefaultHealthCheck(port int64, protocol annotations.AppProtocol) *HealthCheck {
	httpSettings := computealpha.HTTPHealthCheck{Port: port}
	hcSettings := computealpha.HealthCheck{
		// How often to health check.
		CheckIntervalSec: int64(defaultHealthCheckInterval.Seconds()),
		// How long to wait before claiming failure of a health check.
		TimeoutSec: int64(defaultTimeout.Seconds()),
		// Number of healthchecks to pass for a vm to be deemed healthy.
		HealthyThreshold: defaultHealthyThreshold,
		// Number of healthchecks to fail before the vm is deemed unhealthy.
		UnhealthyThreshold: defaultUnhealthyThreshold,
		Description:        "Default kubernetes L7 Loadbalancing health check.",
		Type:               string(protocol),
	}
	return &HealthCheck{
		HTTPHealthCheck: httpSettings,
		HealthCheck:     hcSettings,
		ForNEG:          false,
	}
}

// DefaultNEGHealthCheck simply returns the default health check.
func DefaultNEGHealthCheck(protocol annotations.AppProtocol) *HealthCheck {
	httpSettings := computealpha.HTTPHealthCheck{PortSpecification: useServingPortSpecification}
	klog.V(3).Infof("DefaultNEGHealthCheck(%v)", protocol)

	hcSettings := computealpha.HealthCheck{
		// How often to health check.
		CheckIntervalSec: int64(defaultNEGHealthCheckInterval.Seconds()),
		// How long to wait before claiming failure of a health check.
		TimeoutSec: int64(defaultNEGTimeout.Seconds()),
		// Number of healthchecks to pass for a vm to be deemed healthy.
		HealthyThreshold: defaultHealthyThreshold,
		// Number of healthchecks to fail before the vm is deemed unhealthy.
		UnhealthyThreshold: defaultNEGUnhealthyThreshold,
		Description:        "Default kubernetes L7 Loadbalancing health check for NEG.",
		Type:               string(protocol),
	}
	return &HealthCheck{
		HTTPHealthCheck: httpSettings,
		HealthCheck:     hcSettings,
		ForNEG:          true,
	}
}

func DefaultILBHealthCheck(protocol annotations.AppProtocol) *HealthCheck {
	httpSettings := computealpha.HTTPHealthCheck{PortSpecification: useServingPortSpecification}
	klog.V(3).Infof("DefaultILBHealthCheck(%v)", protocol)

	hcSettings := computealpha.HealthCheck{
		// How often to health check.
		CheckIntervalSec: int64(defaultNEGHealthCheckInterval.Seconds()),
		// How long to wait before claiming failure of a health check.
		TimeoutSec: int64(defaultNEGTimeout.Seconds()),
		// Number of healthchecks to pass for a vm to be deemed healthy.
		HealthyThreshold: defaultHealthyThreshold,
		// Number of healthchecks to fail before the vm is deemed unhealthy.
		UnhealthyThreshold: defaultNEGUnhealthyThreshold,
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

// ApplyProbeSettingsToHC takes the Pod healthcheck settings and applies it
// to the healthcheck.
//
// TODO: what if the port changes?
// TODO: does not handle protocol?
func ApplyProbeSettingsToHC(p *v1.Probe, hc *HealthCheck) {
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
	if hc.ForNEG {
		// For NEG mode, we can support more aggressive healthcheck interval.
		hc.CheckIntervalSec = int64(p.PeriodSeconds)
	} else {
		// For IG mode, short healthcheck interval may health check flooding problem.
		hc.CheckIntervalSec = int64(p.PeriodSeconds) + int64(defaultHealthCheckInterval.Seconds())
	}

	hc.Description = "Kubernetes L7 health check generated with readiness probe settings."
}
