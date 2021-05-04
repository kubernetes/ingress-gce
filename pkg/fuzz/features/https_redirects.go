/*
Copyright 2020 The Kubernetes Authors.

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
	"k8s.io/ingress-gce/pkg/fuzz"
)

// HTTPSRedirects
var HTTPSRedirects = &HTTPSRedirectsFeature{}

// responseCodeMap maps the GCE Response Code Names to their corresponding HTTP Response Codes
var responseCodeMap = map[string]int{
	"MOVED_PERMANENTLY_DEFAULT": 301,
	"FOUND":                     302,
	"TEMPORARY_REDIRECT":        307,
	"PERMANENT_REDIRECT":        308,
}

// HTTPSRedirectsFeature implements the associated feature.
type HTTPSRedirectsFeature struct {
	fuzz.NullValidator

	expectedResponseCode int
}

// Name implements fuzz.Feature.
func (*HTTPSRedirectsFeature) Name() string {
	return "HttpsRedirects"
}

// NewValidator implements fuzz.Feature.
func (f *HTTPSRedirectsFeature) NewValidator() fuzz.FeatureValidator {
	return f
}

// ConfigureAttributes implements fuzz.Feature.
func (v *HTTPSRedirectsFeature) ConfigureAttributes(env fuzz.ValidatorEnv, ing *v1.Ingress, a *fuzz.IngressValidatorAttributes) error {
	fc, err := fuzz.FrontendConfigForIngress(ing, env)
	if err != nil {
		return err
	}
	if fc == nil || fc.Spec.RedirectToHttps == nil || !fc.Spec.RedirectToHttps.Enabled {
		v.expectedResponseCode = 200
		return nil
	}

	// Default
	responseCodeName := "MOVED_PERMANENTLY_DEFAULT"
	if fc.Spec.RedirectToHttps.ResponseCodeName != "" {
		responseCodeName = fc.Spec.RedirectToHttps.ResponseCodeName
	}

	code, ok := responseCodeMap[responseCodeName]
	if !ok {
		return fmt.Errorf("error: not a valid response code name for HTTPS Redirects: %v", responseCodeName)
	}
	v.expectedResponseCode = code

	return nil
}

// CheckResponse implements fuzz.FeatureValidator.
func (v *HTTPSRedirectsFeature) CheckResponse(host, path string, resp *http.Response, body []byte) (fuzz.CheckResponseAction, error) {
	if v.expectedResponseCode != 200 && resp.Request.URL.Scheme == "http" {
		if resp.StatusCode == v.expectedResponseCode {
			return fuzz.CheckResponseSkip, nil
		} else {
			return fuzz.CheckResponseContinue, fmt.Errorf("want status code %d, got %d", v.expectedResponseCode, resp.StatusCode)
		}
	}

	return fuzz.CheckResponseContinue, nil
}
