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

package backends

import (
	api_v1 "k8s.io/api/core/v1"

	"k8s.io/ingress-gce/pkg/utils"
)

// FakeProbeProvider implements the probeProvider interface for tests.
type FakeProbeProvider struct {
	probes map[utils.ServicePort]*api_v1.Probe
}

// NewFakeProbeProvider returns a struct which satisfies probeProvider interface
func NewFakeProbeProvider(probes map[utils.ServicePort]*api_v1.Probe) *FakeProbeProvider {
	return &FakeProbeProvider{probes}
}

// GetProbe returns the probe for a given nodePort
func (pp *FakeProbeProvider) GetProbe(port utils.ServicePort) (*api_v1.Probe, error) {
	if probe, exists := pp.probes[port]; exists && probe.HTTPGet != nil {
		return probe, nil
	}
	return nil, nil
}
