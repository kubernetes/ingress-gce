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
	"strings"

	v1 "k8s.io/api/networking/v1"
	frontendconfig "k8s.io/ingress-gce/pkg/apis/frontendconfig/v1beta1"
	"k8s.io/ingress-gce/pkg/fuzz"
)

// Implements a whitebox test to check that the GCLB has the expected number of ForwardingRule's.
type redirectURLMapTest struct {
}

// Name implements WhiteboxTest.
func (t *redirectURLMapTest) Name() string {
	return "RedirectURLMapTest"

}

// Test implements WhiteboxTest.
func (t *redirectURLMapTest) Test(ing *v1.Ingress, fc *frontendconfig.FrontendConfig, gclb *fuzz.GCLB) error {
	expectedMaps := 0

	if fc != nil && fc.Spec.RedirectToHttps != nil && fc.Spec.RedirectToHttps.Enabled {
		expectedMaps = 1
	}

	foundMaps := 0
	for k := range gclb.URLMap {
		if strings.Contains(k.Name, "-rm-") {
			foundMaps += 1
		}
	}

	if expectedMaps != foundMaps {
		return fmt.Errorf("expectedMaps = %d, got %d", expectedMaps, foundMaps)
	}

	return nil
}
