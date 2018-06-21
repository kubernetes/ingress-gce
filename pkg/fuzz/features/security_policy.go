/*
Copyright 2018 The Kubernetes Authors.

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

package features

import (
	"net/http"

	"k8s.io/api/extensions/v1beta1"
	"k8s.io/ingress-gce/pkg/fuzz"
)

// SecurityPolicy is a feature in BackendConfig that supports using GCP
// Security Policy.
var SecurityPolicy = &SecurityPolicyFeature{}

// SecurityPolicyFeature implements the associated feature.
type SecurityPolicyFeature struct{}

// NewValidator implements fuzz.Feature.
func (SecurityPolicyFeature) NewValidator() fuzz.FeatureValidator {
	return &securityPolicyValidator{}
}

// Name implements fuzz.Feature.
func (*SecurityPolicyFeature) Name() string {
	return "SecurityPolicy"
}

// securityPolicyValidator is a validator for SecurityPolicyFeature.
type securityPolicyValidator struct {
	fuzz.NullValidator

	env fuzz.ValidatorEnv
	ing *v1beta1.Ingress
}

// Name implements fuzz.FeatureValidator.
func (*securityPolicyValidator) Name() string {
	return "SecurityPolicy"
}

// ConfigureAttributes implements fuzz.FeatureValidator.
func (v *securityPolicyValidator) ConfigureAttributes(env fuzz.ValidatorEnv, ing *v1beta1.Ingress, a *fuzz.IngressValidatorAttributes) error {
	// Capture the env for use later in CheckResponse.
	v.ing = ing
	v.env = env
	return nil
}

// CheckResponse implements fuzz.FeatureValidator.
func (v *securityPolicyValidator) CheckResponse(host, path string, resp *http.Response, body []byte) (fuzz.CheckResponseAction, error) {
	// There isn't anything interesting to check in response.
	return fuzz.CheckResponseContinue, nil
}

// HasBetaResource implements Feature. SecurityPolicy requires Beta
// resource.
func (v *securityPolicyValidator) HasBetaResource(resourceType string) bool {
	if resourceType == "backendService" {
		return true
	}
	return false
}
