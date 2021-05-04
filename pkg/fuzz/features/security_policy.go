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
	"context"
	"fmt"
	"net/http"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	v1 "k8s.io/api/networking/v1"
	"k8s.io/ingress-gce/pkg/annotations"
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
	ing *v1.Ingress
}

// Name implements fuzz.FeatureValidator.
func (*securityPolicyValidator) Name() string {
	return "SecurityPolicy"
}

// ConfigureAttributes implements fuzz.FeatureValidator.
func (v *securityPolicyValidator) ConfigureAttributes(env fuzz.ValidatorEnv, ing *v1.Ingress, a *fuzz.IngressValidatorAttributes) error {
	// Capture the env for use later in CheckResponse.
	v.ing = ing
	v.env = env
	return nil
}

// CheckResponse implements fuzz.FeatureValidator.
func (v *securityPolicyValidator) CheckResponse(host, path string, resp *http.Response, body []byte) (fuzz.CheckResponseAction, error) {
	backendConfig, err := fuzz.BackendConfigForPath(host, path, v.ing, v.env)
	if err != nil {
		if err == annotations.ErrBackendConfigAnnotationMissing {
			// Don't fail this test if the service associated
			// with the host + path has no BackendConfig annotation.
			return fuzz.CheckResponseContinue, nil
		}
		return fuzz.CheckResponseContinue, err
	}
	if backendConfig.Spec.SecurityPolicy == nil || backendConfig.Spec.SecurityPolicy.Name == "" {
		// Don't check on response if security policy isn't configured
		// in BackendConfig or is set to none.
		return fuzz.CheckResponseContinue, nil
	}

	policy, err := v.env.Cloud().BetaSecurityPolicies().Get(context.Background(), meta.GlobalKey(backendConfig.Spec.SecurityPolicy.Name))
	if err != nil {
		return fuzz.CheckResponseContinue, fmt.Errorf("error getting security policy %q: %v", backendConfig.Spec.SecurityPolicy.Name, err)
	}
	// Check for the exact response code we are expecting.
	// For simplicity, the test assumes only the default rule exists.
	if len(policy.Rules) == 0 {
		return fuzz.CheckResponseContinue, fmt.Errorf("found 0 rule for security policy %q", backendConfig.Spec.SecurityPolicy.Name)
	}
	var expectedCode int
	switch policy.Rules[0].Action {
	case "allow":
		expectedCode = 200
	case "deny(403)":
		expectedCode = 403
	case "deny(404)":
		expectedCode = 404
	case "deny(502)":
		expectedCode = 502
	default:
		return fuzz.CheckResponseContinue, fmt.Errorf("unrecognized rule %q", policy.Rules[0].Action)
	}
	if resp.StatusCode != expectedCode {
		return fuzz.CheckResponseContinue, fmt.Errorf("unexpected status code %d, want %d", resp.StatusCode, expectedCode)
	}
	// Skip standard check in case of deny policy.
	if expectedCode != 200 {
		return fuzz.CheckResponseSkip, nil
	}
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
