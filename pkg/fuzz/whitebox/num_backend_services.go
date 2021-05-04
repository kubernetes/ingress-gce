/*
Copyright 2019 The Kubernetes Authors.

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

package whitebox

import (
	"fmt"

	v1 "k8s.io/api/networking/v1"
	frontendconfig "k8s.io/ingress-gce/pkg/apis/frontendconfig/v1beta1"
	"k8s.io/ingress-gce/pkg/fuzz"
	"k8s.io/ingress-gce/pkg/utils"
)

// Implements a whitebox test to check that the GCLB has the expected number of BackendService's.
type numBackendServicesTest struct {
	uniqSvcPorts map[utils.ServicePortID]bool
}

// Name implements WhiteboxTest.
func (t *numBackendServicesTest) Name() string {
	return "NumBackendServicesTest"

}

// Test implements WhiteboxTest.
func (t *numBackendServicesTest) Test(ing *v1.Ingress, fc *frontendconfig.FrontendConfig, gclb *fuzz.GCLB) error {
	t.uniqSvcPorts = make(map[utils.ServicePortID]bool)
	expectedBackendServices := 0

	if ing.Spec.DefaultBackend == nil {
		expectedBackendServices++
	}

	utils.TraverseIngressBackends(ing, func(id utils.ServicePortID) bool {
		if _, ok := t.uniqSvcPorts[id]; !ok {
			expectedBackendServices++
			t.uniqSvcPorts[id] = true
		}
		return false
	})

	if len(gclb.BackendService) != expectedBackendServices {
		return fmt.Errorf("Expected %d BackendService's but got %d", expectedBackendServices, len(gclb.BackendService))
	}

	// Verify that access logs are enabled for GA version.
	for _, cbe := range gclb.BackendService {
		if cbe.GA == nil {
			continue
		}
		if !cbe.GA.LogConfig.Enable {
			return fmt.Errorf("access logs are disabled, expected to be enabled")
		}
	}

	return nil
}
