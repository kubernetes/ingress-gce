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
	"fmt"
	"net/http"

	v1 "k8s.io/api/networking/v1"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/fuzz"
	"k8s.io/klog"
)

// IAP is a feature in BackendConfig that supports using GCP Identity-Aware Proxy (IAP).
var IAP = &IAPFeature{}

// IAPFeature implements the associated feature.
type IAPFeature struct{}

// NewValidator implements fuzz.Feature.
func (IAPFeature) NewValidator() fuzz.FeatureValidator {
	return &iapValidator{}
}

// Name implements fuzz.Feature.
func (*IAPFeature) Name() string {
	return "IAP"
}

// iapValidator is a validator for the IAP feature.
type iapValidator struct {
	fuzz.NullValidator

	env fuzz.ValidatorEnv
	ing *v1.Ingress
}

// Name implements fuzz.FeatureValidator.
func (*iapValidator) Name() string {
	return "IAP"
}

// ConfigureAttributes implements fuzz.FeatureValidator.
func (v *iapValidator) ConfigureAttributes(env fuzz.ValidatorEnv, ing *v1.Ingress, a *fuzz.IngressValidatorAttributes) error {
	// Capture the env for use later in CheckResponse.
	v.ing = ing
	v.env = env
	return nil
}

// CheckResponse implements fuzz.FeatureValidator.
func (v *iapValidator) CheckResponse(host, path string, resp *http.Response, body []byte) (fuzz.CheckResponseAction, error) {
	backendConfig, err := fuzz.BackendConfigForPath(host, path, v.ing, v.env)
	if err != nil {
		if err == annotations.ErrBackendConfigAnnotationMissing {
			// Don't fail this test if the service associated
			// with the host + path has no BackendConfig annotation.
			return fuzz.CheckResponseContinue, nil
		}
		return fuzz.CheckResponseContinue, err
	}
	var iapEnabled bool
	if backendConfig.Spec.Iap != nil && backendConfig.Spec.Iap.Enabled == true {
		iapEnabled = true
	}
	// If IAP is turned on, verify response header contains "x-goog-iap-generated-response" key
	// and that the response code was a 302.
	if iapEnabled {
		if resp.StatusCode != http.StatusFound {
			klog.V(2).Infof("The response was %v", resp)
			return fuzz.CheckResponseContinue, fmt.Errorf("IAP is turned on but response %v did not return a 302", resp)
		}
		if resp.Header.Get("x-goog-iap-generated-response") == "" {
			return fuzz.CheckResponseContinue, fmt.Errorf("IAP is turned on but response w/ header %v did not contain IAP header", resp.Header)
		}
		return fuzz.CheckResponseSkip, nil
	}
	// If IAP is turned off, verify that the response code was not 302.
	if !iapEnabled && resp.StatusCode == http.StatusFound {
		return fuzz.CheckResponseContinue, fmt.Errorf("IAP is turned off but response w/ header %v returned a 302", resp.Header)
	}
	return fuzz.CheckResponseContinue, nil
}
