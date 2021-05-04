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
	"context"
	"fmt"
	"net/http"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	v1 "k8s.io/api/networking/v1"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/fuzz"
)

// StaticIP is the "kubernetes.io/ingress.static-ip" annotation.
var StaticIP = &StaticIPFeature{}

// StaticIPFeature implements the associated feature.
type StaticIPFeature struct {
	fuzz.NullValidator
}

// Name implements fuzz.Feature.
func (*StaticIPFeature) Name() string {
	return "StaticIP"
}

// NewValidator implements fuzz.Feature.
func (f *StaticIPFeature) NewValidator() fuzz.FeatureValidator {
	return &staticIPValidator{}
}

// staticIPValidator is a validator for StaticIPFeature
type staticIPValidator struct {
	fuzz.NullValidator

	env fuzz.ValidatorEnv
	ing *v1.Ingress
}

// Name implements fuzz.FeatureValidator
func (*staticIPValidator) Name() string {
	return "StaticIP"
}

// ConfigureAttributes implements fuzz.Feature.
func (v *staticIPValidator) ConfigureAttributes(env fuzz.ValidatorEnv, ing *v1.Ingress, a *fuzz.IngressValidatorAttributes) error {
	// Capture the env for use later in CheckResponse
	v.ing = ing
	v.env = env

	return nil
}

// CheckResponse implements fuzz.FeatureValidator
func (v *staticIPValidator) CheckResponse(host, path string, resp *http.Response, body []byte) (fuzz.CheckResponseAction, error) {
	addrName := annotations.FromIngress(v.ing).GlobalStaticIPName()
	if addrName == "" {
		return fuzz.CheckResponseContinue, nil
	}

	addr, err := v.env.Cloud().GlobalAddresses().Get(context.Background(), meta.GlobalKey(addrName))
	if err != nil {
		return fuzz.CheckResponseContinue, fmt.Errorf("error getting GCP address %s: %v", addrName, err)
	}

	if got, want := v.ing.Status.LoadBalancer.Ingress[0].IP, addr.Address; got != want {
		return fuzz.CheckResponseContinue, fmt.Errorf("got IP %s but want %s", got, want)
	}

	return fuzz.CheckResponseContinue, nil
}
