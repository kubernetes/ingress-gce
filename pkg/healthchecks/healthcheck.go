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
	"fmt"
	"strconv"
	"strings"

	backendconfigv1 "k8s.io/ingress-gce/pkg/apis/backendconfig/v1"
	"k8s.io/ingress-gce/pkg/translator"
)

// fieldDiffs encapsulate which fields are different between health checks.
type fieldDiffs struct {
	f []string
}

func (c *fieldDiffs) add(field, oldv, newv string) {
	c.f = append(c.f, fmt.Sprintf("%s:%s -> %s", field, oldv, newv))
}
func (c *fieldDiffs) String() string { return strings.Join(c.f, ", ") }
func (c *fieldDiffs) hasDiff() bool  { return len(c.f) > 0 }

func calculateDiff(old, new *translator.HealthCheck, c *backendconfigv1.HealthCheckConfig) *fieldDiffs {
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
func mergeUserSettings(existing, newHC *translator.HealthCheck) *translator.HealthCheck {
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
	if hc.ForNEG {
		hc.HTTPHealthCheck.Port = 0
		hc.PortSpecification = newHC.PortSpecification
	} else {
		hc.PortSpecification = ""
		hc.Port = newHC.Port
	}
	return &hc
}
