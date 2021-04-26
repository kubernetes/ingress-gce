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
	"strings"

	v1 "k8s.io/api/networking/v1"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/fuzz"
)

// Affinity is a feature in BackendConfig that supports using session affinity on GCP LBs.
var Affinity = &AffinityFeature{}

// AffinityFeature implements the associated feature.
type AffinityFeature struct{}

// NewValidator implements fuzz.Feature.
func (AffinityFeature) NewValidator() fuzz.FeatureValidator {
	return &affinityValidator{}
}

// Name implements fuzz.Feature.
func (*AffinityFeature) Name() string {
	return "Affinity"
}

// affinityValidator is a validator the CDN feature.
type affinityValidator struct {
	fuzz.NullValidator

	env fuzz.ValidatorEnv
	ing *v1.Ingress
}

// Name implements fuzz.FeatureValidator.
func (*affinityValidator) Name() string {
	return "Affinity"
}

// ConfigureAttributes implements fuzz.FeatureValidator.
func (v *affinityValidator) ConfigureAttributes(env fuzz.ValidatorEnv, ing *v1.Ingress, a *fuzz.IngressValidatorAttributes) error {
	// Capture the env for use later in CheckResponse.
	v.ing = ing
	v.env = env
	return nil
}

// CheckResponse implements fuzz.FeatureValidator.
func (v *affinityValidator) CheckResponse(host, path string, resp *http.Response, body []byte) (fuzz.CheckResponseAction, error) {
	backendConfig, err := fuzz.BackendConfigForPath(host, path, v.ing, v.env)
	if err != nil {
		if err == annotations.ErrBackendConfigAnnotationMissing {
			// Don't fail this test if the service associated
			// with the host + path has no BackendConfig annotation.
			return fuzz.CheckResponseContinue, nil
		}
		return fuzz.CheckResponseContinue, err
	}

	// only testing cookie presence, as NONE and CLIENT_IP aren't really visible client side
	haveCookie := haveGCLBCookie(resp)

	if backendConfig.Spec.SessionAffinity == nil {
		return fuzz.CheckResponseContinue, nil
	}

	if backendConfig.Spec.SessionAffinity.AffinityType == "GENERATED_COOKIE" && !haveCookie {
		return fuzz.CheckResponseContinue,
			fmt.Errorf("cookie based affinity is turned on but response did not contain a GCLB cookie")
	}

	if backendConfig.Spec.SessionAffinity.AffinityType == "NONE" && haveCookie {
		return fuzz.CheckResponseContinue,
			fmt.Errorf("affinity is NONE but response contains a GCLB cookie")
	}

	if backendConfig.Spec.SessionAffinity.AffinityType == "CLIENT_IP" && haveCookie {
		return fuzz.CheckResponseContinue,
			fmt.Errorf("affinity is CLIENT_IP but response contains a GCLB cookie")
	}

	return fuzz.CheckResponseContinue, nil
}

func haveGCLBCookie(resp *http.Response) bool {
	cookie := resp.Header.Get("set-cookie")
	if cookie == "" {
		return false
	}

	fmt.Printf("cookie = %+v\n", cookie)

	if strings.HasPrefix(cookie, "GCLB") || strings.HasPrefix(cookie, "GCILB") {
		return true
	}

	return false
}
