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

package fuzz

import (
	"net/http"

	v1 "k8s.io/api/networking/v1"
)

// Feature represents an extension to the "vanilla" behavior of Ingress.
type Feature interface {
	// Name of the feature.
	Name() string
	// NewValidator returns a new validator instance.
	NewValidator() FeatureValidator
}

// FeatureValidators returns a list of validators.
func FeatureValidators(fs []Feature) []FeatureValidator {
	var ret []FeatureValidator
	for _, f := range fs {
		ret = append(ret, f.NewValidator())
	}
	return ret
}

// CheckResponseAction is the action to be taken when evaluating the
// CheckResponse.
type CheckResponseAction int

const (
	// CheckResponseContinue continues with the standard response checking.
	CheckResponseContinue CheckResponseAction = iota
	// CheckResponseSkip skips the standard response checking.
	CheckResponseSkip CheckResponseAction = iota
)

// FeatureValidator is a validator for the Feature. It has various hooks to the
// standard validation routine.
type FeatureValidator interface {
	// Name of the feature.
	Name() string
	// ConfigureAttributes of the validation for given the environment and
	// the Ingress object.
	ConfigureAttributes(env ValidatorEnv, ing *v1.Ingress, a *IngressValidatorAttributes) error
	// ModifyRequest adds the appropriate headers for testing the feature, if
	// necessary.
	ModifyRequest(host, path string, req *http.Request)
	// CheckResponse checks the HTTP response from the validation for
	// correctness. Return (CheckResponseContinue, nil) if you wish to continue
	// with the standard Response validation. Return (CheckResponseSkip, nil)
	// if you wish to skip the standard Response validation for the current
	// request. If (_, err) is returned, then the response is considered to be
	// an error.
	CheckResponse(host, path string, resp *http.Response, body []byte) (CheckResponseAction, error)

	// TODO(shance): ideally we should use features.ResourceVersions and scope here
	HasAlphaResource(resourceType string) bool
	HasBetaResource(resourceType string) bool
}

// NullValidator is a feature that does nothing. Embed this object to reduce the
// amount of boilerplate required to implement a feature that doesn't require
// all of the hooks.
type NullValidator struct {
}

// ConfigureAttributes implements Feature.
func (*NullValidator) ConfigureAttributes(env ValidatorEnv, ing *v1.Ingress, a *IngressValidatorAttributes) error {
	return nil
}

// ModifyRequest implements Feature.
func (*NullValidator) ModifyRequest(string, string, *http.Request) {}

// CheckResponse implements Feature.
func (*NullValidator) CheckResponse(string, string, *http.Response, []byte) (CheckResponseAction, error) {
	return CheckResponseContinue, nil
}

// HasAlphaResource implements Feature.
func (*NullValidator) HasAlphaResource(resourceType string) bool {
	return false
}

// HasBetaResource implements Feature.
func (*NullValidator) HasBetaResource(resourceType string) bool {
	return false
}
