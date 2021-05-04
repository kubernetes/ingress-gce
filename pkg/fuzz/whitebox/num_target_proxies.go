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
	"k8s.io/ingress-gce/pkg/annotations"
	frontendconfig "k8s.io/ingress-gce/pkg/apis/frontendconfig/v1beta1"
	"k8s.io/ingress-gce/pkg/fuzz"
)

// Implements a whitebox test to check that the GCLB has the expected number of TargetHTTPSProxy's.
type numTargetProxiesTest struct {
}

// Name implements WhiteboxTest.
func (t *numTargetProxiesTest) Name() string {
	return "NumTargetProxiesTest"

}

// Test implements WhiteboxTest.
func (t *numTargetProxiesTest) Test(ing *v1.Ingress, fc *frontendconfig.FrontendConfig, gclb *fuzz.GCLB) error {
	expectedHTTPTargetProxies, expectedHTTPSTargetProxies := 0, 0

	an := annotations.FromIngress(ing)
	if an.AllowHTTP() {
		expectedHTTPTargetProxies = 1
	}
	if len(ing.Spec.TLS) > 0 || an.UseNamedTLS() != "" {
		expectedHTTPSTargetProxies = 1
	}

	if l := len(gclb.TargetHTTPProxy); l != expectedHTTPTargetProxies {
		return fmt.Errorf("expected %d TargetHTTPProxy's but got %d", expectedHTTPTargetProxies, l)
	}
	if l := len(gclb.TargetHTTPSProxy); l != expectedHTTPSTargetProxies {
		return fmt.Errorf("expected %d TargetHTTPSProxy's but got %d", expectedHTTPSTargetProxies, l)
	}

	return nil
}
