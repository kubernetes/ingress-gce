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

	"k8s.io/api/networking/v1beta1"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/fuzz"
)

// Implements a whitebox test to check that the GCLB has the expected number of ForwardingRule's.
type numForwardingRulesTest struct {
}

// Name implements WhiteboxTest.
func (t *numForwardingRulesTest) Name() string {
	return "NumForwardingRulesTest"

}

// Test implements WhiteboxTest.
func (t *numForwardingRulesTest) Test(ing *v1beta1.Ingress, gclb *fuzz.GCLB) error {
	expectedForwardingRules := 1
	if len(ing.Spec.TLS) > 0 {
		expectedForwardingRules = 2
	}

	an := annotations.FromIngress(ing)
	if an.UseNamedTLS() != "" {
		expectedForwardingRules = 2
	}

	if len(gclb.ForwardingRule) != expectedForwardingRules {
		return fmt.Errorf("Expected %d ForwardingRule's but got %d", expectedForwardingRules, len(gclb.ForwardingRule))
	}

	return nil
}
