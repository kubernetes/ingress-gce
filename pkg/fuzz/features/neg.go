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

// Package features configures additional extra features for the Ingress.
// Examples for how to implement additional features can be found in the
// *_example.go files.

package features

import (
	"context"
	"fmt"
	"k8s.io/api/extensions/v1beta1"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/fuzz"
	"k8s.io/kubernetes/pkg/cloudprovider/providers/gce/cloud/meta"
	"net/http"
	"strings"
)

// NEG is a feature in GCP to support pod as Loadbalancer backends
var NEG = &NegFeature{}

// NegFeature implements the associated feature.
type NegFeature struct{}

// NewValidator implements fuzz.Feature.
func (*NegFeature) NewValidator() fuzz.FeatureValidator {
	return &negValidator{}
}

// Name implements fuzz.Feature.
func (*NegFeature) Name() string {
	return "NEG"
}

// negValidator is a validator for the NEG feature
type negValidator struct {
	fuzz.NullValidator

	ing *v1beta1.Ingress
	env fuzz.ValidatorEnv
}

// Name implements fuzz.FeatureValidator.
func (*negValidator) Name() string {
	return "NEG"
}

// ConfigureAttributes implements fuzz.FeatureValidator.
func (v *negValidator) ConfigureAttributes(env fuzz.ValidatorEnv, ing *v1beta1.Ingress, a *fuzz.IngressValidatorAttributes) error {
	// Capture the env for use later in CheckResponse.
	v.ing = ing
	v.env = env
	return nil
}

// CheckResponse implements fuzz.FeatureValidator.
// Check that the neg is being used for the path if it is configured
func (v *negValidator) CheckResponse(host, path string, resp *http.Response, body []byte) (fuzz.CheckResponseAction, error) {
	svc, svcPort, err := fuzz.ServiceForPath(host, path, v.ing, v.env)
	if err != nil {
		return fuzz.CheckResponseContinue, err
	}

	// Check neg status for ingress enabled
	// TODO: (shance) update this to support exposed_port as well
	annotationSvc := annotations.FromService(svc)
	negAnnotation, found, err := annotationSvc.NEGAnnotation()
	if err != nil {
		return fuzz.CheckResponseContinue, fmt.Errorf("Error getting NEG annotation for service %v: %v", svc, err)
	}

	// Path does not have a NEG annotation, nothing to check here
	if !found {
		return fuzz.CheckResponseContinue, nil
	}

	ctx := context.Background()

	negStatus, found, err := annotationSvc.NEGStatus()

	if err != nil {
		return fuzz.CheckResponseContinue, fmt.Errorf("Error obtaining Neg status for service: %v, error: %v", svc, err)
	}

	// Do whitebox verifications
	// Check if the corresponding backend service is targeting NEGs instead of IGs
	// WARNING: assumes the backend service naming scheme is using the NEG naming scheme
	if negAnnotation.NEGEnabledForIngress() {
		if found && len(negStatus.NetworkEndpointGroups) > 0 {
			// Check backend services as well
			if backendIsNeg, err := verifyNegBackend(&ctx, v.env, negStatus.NetworkEndpointGroups[svcPort.Port]); !backendIsNeg {
				return fuzz.CheckResponseContinue, err
			}
			return fuzz.CheckResponseContinue, nil
		} else {
			return fuzz.CheckResponseContinue, fmt.Errorf("Error, path %v for service %v is not targeting a neg", path, svc)
		}
	} else {
		if found {
			return fuzz.CheckResponseContinue, fmt.Errorf("Error, path %v for service %v should not be targetting a neg", path, svc)
		} else {
			return fuzz.CheckResponseContinue, nil
		}
	}
}

func verifyNegBackend(ctx *context.Context, env fuzz.ValidatorEnv, negName string) (bool, error) {
	beService, err := env.Cloud().BackendServices().Get(*ctx, &meta.Key{Name: negName})

	if beService == nil {
		return false, fmt.Errorf("No backend service returned for name %s", negName)
	}

	if err != nil {
		return false, err
	}

	for _, be := range beService.Backends {
		if !strings.Contains(be.Group, negName) {
			return false, fmt.Errorf("Backend %v group %v is not a NEG", be, be.Group)
		}
	}

	return true, nil
}
