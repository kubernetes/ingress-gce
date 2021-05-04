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

package features

import (
	"net/http"

	v1 "k8s.io/api/networking/v1"
	"k8s.io/ingress-gce/pkg/fuzz"
)

// ILB is an internal load balancer
var ILB = &ILBFeature{}

// ILBFeature implements the associated feature
type ILBFeature struct{}

// NewValidator implements fuzz.Feature.
func (*ILBFeature) NewValidator() fuzz.FeatureValidator {
	return &ILBValidator{}
}

// Name implements fuzz.Feature.
func (*ILBFeature) Name() string {
	return "ILB"
}

// ILBValidator is an example validator.
type ILBValidator struct {
	fuzz.NullValidator

	ing *v1.Ingress
	env fuzz.ValidatorEnv
}

// Name implements fuzz.FeatureValidator.
func (*ILBValidator) Name() string {
	return "ILB"
}

// ConfigureAttributes implements fuzz.FeatureValidator.
func (v *ILBValidator) ConfigureAttributes(env fuzz.ValidatorEnv, ing *v1.Ingress, a *fuzz.IngressValidatorAttributes) error {
	// Capture the env for use later in CheckResponse.
	v.ing = ing
	v.env = env
	return nil
}

// CheckResponse implements fuzz.FeatureValidator.
func (v *ILBValidator) CheckResponse(host, path string, resp *http.Response, body []byte) (fuzz.CheckResponseAction, error) {
	return fuzz.CheckResponseContinue, nil
}
