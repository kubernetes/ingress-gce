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
	"net/http"
	"strconv"
	"strings"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/fuzz"
	"k8s.io/ingress-gce/pkg/utils"
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

	negEnabled, negName, err := v.getNegNameForServicePort(svc, svcPort)
	if err != nil {
		return fuzz.CheckResponseContinue, err
	}

	key, err := utils.KeyFunc(v.ing)
	if err != nil {
		return fuzz.CheckResponseContinue, err
	}

	urlMapName := v.env.Namer().UrlMap(v.env.Namer().LoadBalancer(key))
	if negEnabled {
		return fuzz.CheckResponseContinue, verifyNegBackend(v.env, negName, urlMapName)
	} else {
		return fuzz.CheckResponseContinue, verifyIgBackend(v.env, v.env.Namer().IGBackend(int64(svcPort.NodePort)), urlMapName)
	}
}

// getNegNameForServicePort returns the NEG name for the service port if it exists.
// It returns true if neg is enabled on the service for Ingress. It returns false otherwise.
func (v *negValidator) getNegNameForServicePort(svc *v1.Service, svcPort *v1.ServicePort) (negEnabled bool, negName string, err error) {
	annotationSvc := annotations.FromService(svc)
	negAnnotation, negAnnotationFound, err := annotationSvc.NEGAnnotation()
	if err != nil {
		return false, negName, fmt.Errorf("error getting NEG annotation for service %v/%v: %v", svc.Namespace, svc.Name, err)
	}

	if !negAnnotationFound {
		return false, negName, nil
	}

	if !negAnnotation.NEGEnabledForIngress() {
		return false, negName, nil
	}

	status, negStatusFound, err := annotationSvc.NEGStatus()
	if err != nil {
		return true, negName, fmt.Errorf("error getting NEG status for service %v/%v: %v", svc.Namespace, svc.Name, err)
	}

	if !negStatusFound {
		return true, negName, fmt.Errorf("NEG status not found for service %v/%v", svc.Namespace, svc.Name)
	}

	negName, negFound := status.NetworkEndpointGroups[strconv.Itoa(int(svcPort.Port))]
	if !negFound {
		return true, negName, fmt.Errorf("NEG for service port %d not found for service %v/%v in NEG status %v", svcPort.Port, svc.Namespace, svc.Name, status)
	}
	return true, negName, nil
}

// verifyNegBackend verifies if the backend service is using network endpoint group
func verifyNegBackend(env fuzz.ValidatorEnv, negName string, urlMapName string) error {
	return verifyBackend(env, negName, negName, urlMapName)
}

// verifyNegBackend verifies if the backend service is using instance group
func verifyIgBackend(env fuzz.ValidatorEnv, bsName string, urlMapName string) error {
	return verifyBackend(env, bsName, "instanceGroup", urlMapName)
}

// verifyBackend verifies the backend service and check if the corresponding backend group has the keyword
func verifyBackend(env fuzz.ValidatorEnv, bsName string, backendKeyword string, urlMapName string) error {
	ctx := context.Background()
	beService, err := env.Cloud().BackendServices().Get(ctx, &meta.Key{Name: bsName})
	if err != nil {
		return err
	}

	if beService == nil {
		return fmt.Errorf("no backend service returned for name %s", bsName)
	}

	for _, be := range beService.Backends {
		if !strings.Contains(be.Group, backendKeyword) {
			return fmt.Errorf("backend group %q of backend service %q does not contain keyword %q", be.Group, bsName, backendKeyword)
		}
	}

	// Examine if ingress url map is targeting the backend service
	urlMap, err := env.Cloud().UrlMaps().Get(ctx, &meta.Key{Name: urlMapName})
	if err != nil {
		return err
	}

	if strings.Contains(urlMap.DefaultService, beService.Name) {
		return nil
	}
	for _, pathMatcher := range urlMap.PathMatchers {
		for _, rule := range pathMatcher.PathRules {
			if strings.Contains(rule.Service, beService.Name) {
				return nil
			}
		}
	}

	return fmt.Errorf("backend service %q is not used by UrlMap %q", bsName, urlMapName)
}
