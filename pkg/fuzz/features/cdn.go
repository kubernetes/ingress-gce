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
)

// CDN is a feature in BackendConfig that supports using GCP CDN.
var CDN = &CDNFeature{}

// CDNFeature implements the associated feature.
type CDNFeature struct{}

// NewValidator implements fuzz.Feature.
func (CDNFeature) NewValidator() fuzz.FeatureValidator {
	return &cdnValidator{}
}

// Name implements fuzz.Feature.
func (*CDNFeature) Name() string {
	return "CDN"
}

// cdnValidator is a validator for CDNFeature.
type cdnValidator struct {
	fuzz.NullValidator

	env fuzz.ValidatorEnv
	ing *v1.Ingress
}

// Name implements fuzz.FeatureValidator.
func (*cdnValidator) Name() string {
	return "CDN"
}

// ConfigureAttributes implements fuzz.FeatureValidator.
func (v *cdnValidator) ConfigureAttributes(env fuzz.ValidatorEnv, ing *v1.Ingress, a *fuzz.IngressValidatorAttributes) error {
	// Capture the env for use later in CheckResponse.
	v.ing = ing
	v.env = env
	return nil
}

// ModifyRequest implements fuzz.FeatureValidator.
func (v *cdnValidator) ModifyRequest(host, path string, req *http.Request) {
	// Warm up the cache, regardless of whether this host + path has CDN enabled.
	values := req.URL.Query()
	// Note: cache=true tells the echo server to add caching headers.
	values.Add("cache", "true")
	req.URL.RawQuery = values.Encode()
}

// CheckResponse implements fuzz.FeatureValidator.
func (v *cdnValidator) CheckResponse(host, path string, resp *http.Response, body []byte) (fuzz.CheckResponseAction, error) {
	backendConfig, err := fuzz.BackendConfigForPath(host, path, v.ing, v.env)
	if err != nil {
		if err == annotations.ErrBackendConfigAnnotationMissing {
			// Don't fail this test if the service associated
			// with the host + path has no BackendConfig annotation.
			return fuzz.CheckResponseContinue, nil
		}
		return fuzz.CheckResponseContinue, err
	}
	var cdnEnabled bool
	if backendConfig.Spec.Cdn != nil && backendConfig.Spec.Cdn.Enabled == true {
		cdnEnabled = true
	}
	// If CDN is turned on, verify response header has "Age" key, which indicates
	// it was served from the cache.
	if cdnEnabled && resp.Header.Get("Age") == "" {
		return fuzz.CheckResponseContinue, fmt.Errorf("CDN is turned on but response w/ header %v was not served from cache", resp.Header)
	}
	// If CDN is turned off, verify response does not have "Age" key, which indicates
	// it was not served from the cache.
	if !cdnEnabled && resp.Header.Get("Age") != "" {
		return fuzz.CheckResponseContinue, fmt.Errorf("CDN is turned off but response w/ header %v was served from cache", resp.Header)
	}
	return fuzz.CheckResponseContinue, nil
}
