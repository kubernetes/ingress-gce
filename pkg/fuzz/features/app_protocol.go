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
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	v1 "k8s.io/api/networking/v1"
	echo "k8s.io/ingress-gce/cmd/echo/app"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/fuzz"
)

// AppProtocol is the "service.alpha.kubernetes.io/app-protocols" annotation
var AppProtocol = &AppProtocolFeature{}

// AppProtocolFeature implements the associated feature.
type AppProtocolFeature struct{}

// Name implements fuzz.Feature.
func (*AppProtocolFeature) Name() string {
	return "AppProtocol"
}

// NewValidator implements fuzz.Feature.
func (f *AppProtocolFeature) NewValidator() fuzz.FeatureValidator {
	return &appProtocolValidator{}
}

// appProtocolValidator is a validator for AppProtocolFeature
type appProtocolValidator struct {
	fuzz.NullValidator

	env fuzz.ValidatorEnv
	ing *v1.Ingress
}

// Name implements fuzz.FeatureValidator
func (*appProtocolValidator) Name() string {
	return "AppProtocol"
}

// ConfigureAttributes implements fuzz.FeatureValidator.
func (v *appProtocolValidator) ConfigureAttributes(env fuzz.ValidatorEnv, ing *v1.Ingress, a *fuzz.IngressValidatorAttributes) error {
	// Capture the env for use later in CheckResponse
	v.ing = ing
	v.env = env

	return nil
}

// CheckResponse implements fuzz.FeatureValidator
func (v *appProtocolValidator) CheckResponse(host, path string, resp *http.Response, body []byte) (fuzz.CheckResponseAction, error) {
	service, servicePort, err := fuzz.ServiceForPath(host, path, v.ing, v.env)
	if err != nil {
		return fuzz.CheckResponseContinue, err
	}
	ap, err := annotations.FromService(service).ApplicationProtocols()
	if err != nil {
		return fuzz.CheckResponseContinue, err
	}
	var proto annotations.AppProtocol
	if protoStr, ok := ap[servicePort.Name]; ok {
		proto = annotations.AppProtocol(protoStr)
	}

	if resp.StatusCode != 200 {
		// This test is contingent on a proper response being returned so
		// bail if don't get a 200.
		return fuzz.CheckResponseContinue, nil
	}

	var r echo.ResponseBody
	if err := json.Unmarshal(body, &r); err != nil {
		return fuzz.CheckResponseContinue, err
	}

	switch proto {
	case annotations.ProtocolHTTPS:
		if !(strings.HasPrefix(r.HTTPVersion, "1.") && r.TLS) {
			return fuzz.CheckResponseContinue, fmt.Errorf("expected HTTP/1.x response with TLS configured on request: %+v", r)
		}
	case annotations.ProtocolHTTP2:
		if !strings.HasPrefix(r.HTTPVersion, "2.") {
			return fuzz.CheckResponseContinue, fmt.Errorf("expected HTTP/2.x response: %+v", resp)
		}
	default:
		if !(strings.HasPrefix(r.HTTPVersion, "1.") && !r.TLS) {
			return fuzz.CheckResponseContinue, fmt.Errorf("expected HTTP/1.x response with no TLS configured on request: %+v", r)
		}
	}

	return fuzz.CheckResponseContinue, nil
}
