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

func calculateDiff(old, new *translator.HealthCheck) *fieldDiffs {
	var changes fieldDiffs

	if old.Protocol() != new.Protocol() {
		changes.add("Protocol", string(old.Protocol()), string(new.Protocol()))
	}
	if old.PortSpecification != new.PortSpecification {
		changes.add("PortSpecification", old.PortSpecification, new.PortSpecification)
	}
	if old.CheckIntervalSec != new.CheckIntervalSec {
		changes.add("CheckIntervalSec", strconv.FormatInt(old.CheckIntervalSec, 10), strconv.FormatInt(new.CheckIntervalSec, 10))
	}
	if old.TimeoutSec != new.TimeoutSec {
		changes.add("TimeoutSec", strconv.FormatInt(old.TimeoutSec, 10), strconv.FormatInt(new.TimeoutSec, 10))
	}
	if old.HealthyThreshold != new.HealthyThreshold {
		changes.add("HeathyThreshold", strconv.FormatInt(old.HealthyThreshold, 10), strconv.FormatInt(new.HealthyThreshold, 10))
	}
	if old.UnhealthyThreshold != new.UnhealthyThreshold {
		changes.add("UnhealthyThreshold", strconv.FormatInt(old.UnhealthyThreshold, 10), strconv.FormatInt(new.UnhealthyThreshold, 10))
	}
	// c.Type is handled by Protocol above.
	if old.RequestPath != new.RequestPath {
		changes.add("RequestPath", old.RequestPath, new.RequestPath)
	}
	if old.Port != new.Port {
		changes.add("Port", strconv.FormatInt(old.Port, 10), strconv.FormatInt(new.Port, 10))
	}
	if old.Host != new.Host {
		changes.add("Host", old.Host, new.Host)
	}
	if old.Description != new.Description {
		changes.add("Description", old.Description, new.Description)
	}

	return &changes
}
