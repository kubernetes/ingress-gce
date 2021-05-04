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
	v1 "k8s.io/api/networking/v1"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/fuzz"
)

// AllowHTTP is the "kubernetes.io/ingress.allow-http" annotation. If the
// annotation is set to false, then disable serving HTTP.
var AllowHTTP = &AllowHTTPFeature{}

// AllowHTTPFeature implements the associated feature.
type AllowHTTPFeature struct {
	fuzz.NullValidator
}

// Name implements fuzz.Feature.
func (*AllowHTTPFeature) Name() string {
	return "AllowHTTP"
}

// NewValidator implements fuzz.Feature.
func (f *AllowHTTPFeature) NewValidator() fuzz.FeatureValidator {
	return f
}

// ConfigureAttributes implements fuzz.Feature.
func (*AllowHTTPFeature) ConfigureAttributes(env fuzz.ValidatorEnv, ing *v1.Ingress, a *fuzz.IngressValidatorAttributes) error {
	an := annotations.FromIngress(ing)
	if !an.AllowHTTP() {
		a.CheckHTTP = false
	}

	//TODO(rramkumar): Verify that all HTTPS resources were garbage collected.

	return nil
}
