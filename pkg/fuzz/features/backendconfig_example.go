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

// BackendConfigExample is an example of how a Feature will integrate with the
// BackendConfig referenced from a Service.
var BackendConfigExample = &BackendConfigExampleFeature{}

// BackendConfigExampleFeature is the example BackendConfig feature.
type BackendConfigExampleFeature struct{}

// NewValidator implements fuzz.Feature.
func (BackendConfigExampleFeature) NewValidator() fuzz.FeatureValidator {
	return &backendConfigExampleValidator{}
}

// Name implements fuzz.Feature.
func (*BackendConfigExampleFeature) Name() string {
	return "BackendConfigExample"
}

// backendConfigExampleValidator is an example validator.
type backendConfigExampleValidator struct {
	fuzz.NullValidator

	env fuzz.ValidatorEnv
	ing *v1.Ingress
}

// Name implements fuzz.FeatureValidator.
func (*backendConfigExampleValidator) Name() string {
	return "BackendConfigExample"
}

// ConfigureAttributes implements fuzz.FeatureValidator.
func (v *backendConfigExampleValidator) ConfigureAttributes(env fuzz.ValidatorEnv, ing *v1.Ingress, a *fuzz.IngressValidatorAttributes) error {
	// Capture the env for use later in CheckResponse.
	v.ing = ing
	v.env = env
	return nil
}

// CheckResponse implements fuzz.FeatureValidator.
func (v *backendConfigExampleValidator) CheckResponse(host, path string, resp *http.Response, body []byte) (fuzz.CheckResponseAction, error) {
	sm := fuzz.ServiceMapFromIngress(v.ing)
	hp := fuzz.HostPath{Host: host, Path: path}
	b, ok := sm[hp]
	if !ok {
		return fuzz.CheckResponseContinue, fmt.Errorf("hostPath %v not found in Ingress", hp)
	}

	serviceMap, err := v.env.Services()
	if err != nil {
		return fuzz.CheckResponseContinue, err
	}

	service, ok := serviceMap[b.Service.Name]
	if !ok {
		return fuzz.CheckResponseContinue, fmt.Errorf("service %q not found in environment", b.Service.Name)
	}

	anno := annotations.FromService(service)
	bc, err := anno.GetBackendConfigs()
	if err != nil {
		return fuzz.CheckResponseContinue, err
	}

	klog.Infof("Relevant BackendConfigs: %+v", bc)

	return fuzz.CheckResponseContinue, nil
}
