/*
Copyright 2026 The Kubernetes Authors.

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

package l4lbconfig

import (
	"errors"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	apisl4lbconfig "k8s.io/ingress-gce/pkg/apis/l4lbconfig"
	l4lbconfigv1 "k8s.io/ingress-gce/pkg/apis/l4lbconfig/v1"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/crd"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/l4annotations"
	common "k8s.io/kube-openapi/pkg/common"
)

var (
	ErrL4LBConfigDoesNotExist = errors.New("no L4LBConfig for service port exists.")
	ErrL4LBConfigFailedToGet  = errors.New("client had error getting L4LBConfig for service.")
	ErrL4LBConfigInvalidMode  = errors.New("invalid OptionalMode in L4LBConfig for service.")
	allowedOptionalModes      = []interface{}{"INCLUDE_ALL_OPTIONAL", "EXCLUDE_ALL_OPTIONAL", "CUSTOM"}
)

const (
	// ReasonL4LBConfigNotFound is used when the annotation exists but the object doesn't.
	ReasonL4LBConfigNotFound = "L4LBConfigNotFound"
	// ReasonL4LBConfigFetchFailed is used for API/Client errors.
	ReasonL4LBConfigFetchFailed = "L4LBConfigFetchFailed"
	// ReasonL4LBConfigInvalidMode is used when the OptionalMode in L4LBConfig is invalid.
	ReasonL4LBConfigInvalidMode = "L4LBConfigInvalidMode"

	// maxSampleRate is the maximum allowed value for LoggingConfig.SampleRate (100% in millionth).
	maxSampleRate = 1000000.0
	// minSampleRate is the minimum allowed value for LoggingConfig.SampleRate (0% in millionth).
	minSampleRate = 0.0
	// defaultSampleRate is the default value for composite.BackendServiceLogConfig.SampleRate when not specified.
	defaultSampleRate = 1.0
)

func CRDMeta() *crd.CRDMeta {
	meta := crd.NewCRDMeta(
		apisl4lbconfig.GroupName,
		"L4LBConfig",
		"L4LBConfigList",
		"l4lbconfig",
		"l4lbconfigs",
		[]*crd.Version{
			crd.NewVersion("v1", "k8s.io/ingress-gce/pkg/apis/l4lbconfig/v1.L4LBConfig", func(ref common.ReferenceCallback) map[string]common.OpenAPIDefinition {
				defs := l4lbconfigv1.GetOpenAPIDefinitions(ref)

				if def, ok := defs["k8s.io/ingress-gce/pkg/apis/l4lbconfig/v1.LoggingConfig"]; ok {
					// Patch SampleRate
					if prop, ok := def.Schema.SchemaProps.Properties["sampleRate"]; ok {
						min := minSampleRate
						max := maxSampleRate
						prop.SchemaProps.Minimum = &min
						prop.SchemaProps.Maximum = &max
						def.Schema.SchemaProps.Properties["sampleRate"] = prop
					}

					// Patch OptionalMode (Enum)
					if prop, ok := def.Schema.SchemaProps.Properties["optionalMode"]; ok {
						prop.SchemaProps.Enum = allowedOptionalModes
						def.Schema.SchemaProps.Properties["optionalMode"] = prop
					}

					defs["k8s.io/ingress-gce/pkg/apis/l4lbconfig/v1.LoggingConfig"] = def
				}
				return defs
			}, false),
		},
	)
	return meta
}

// GetL4LBConfigForService returns the corresponding L4LBConfig for
// the given Service if specified.
func GetL4LBConfigForService(l4lbConfigLister cache.Store, svc *corev1.Service) (*l4lbconfigv1.L4LBConfig, error) {
	l4lbConfigName, configReferenced := l4annotations.FromService(svc).GetL4LBConfigAnnotation()
	if !configReferenced {
		return nil, nil
	}

	obj, exists, err := l4lbConfigLister.Get(
		&l4lbconfigv1.L4LBConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      l4lbConfigName,
				Namespace: svc.Namespace,
			},
		})
	if err != nil {
		return nil, ErrL4LBConfigFailedToGet
	}
	if !exists {
		return nil, ErrL4LBConfigDoesNotExist
	}

	return obj.(*l4lbconfigv1.L4LBConfig), nil
}

// DetermineL4LoggingConfig resolves the logging configuration for L4 services.
// It returns the resolved config, a boolean indicating if logging control is enabled,
// a Condition describing the state, and any error encountered.
func DetermineL4LoggingConfig(
	service *corev1.Service,
	l4lbConfigLister cache.Store,
) (*composite.BackendServiceLogConfig, metav1.Condition, error) {

	// Check Global Gate
	if !flags.F.ManageL4LBLogging || l4lbConfigLister == nil {
		return nil, NewConditionLoggingUnmanaged(), nil
	}

	// Attempt to Fetch Config
	serviceL4LBConfig, err := GetL4LBConfigForService(l4lbConfigLister, service)
	if err != nil {
		if errors.Is(err, ErrL4LBConfigDoesNotExist) {
			return nil, NewConditionLoggingMissing(), err
		}
		return nil, NewConditionLoggingError(), err
	}

	// Validate Config Content
	if serviceL4LBConfig == nil || serviceL4LBConfig.Spec.Logging == nil {
		return nil, NewConditionLoggingUnmanaged(), nil
	}

	// Build Resolved Config
	slc := serviceL4LBConfig.Spec.Logging
	resolvedConfig := &composite.BackendServiceLogConfig{
		Enable:         slc.Enabled,
		OptionalFields: slc.OptionalFields,
	}

	resolvedConfig.OptionalMode = slc.OptionalMode
	if resolvedConfig.OptionalMode == "" { // Set default if not specified
		resolvedConfig.OptionalMode = "EXCLUDE_ALL_OPTIONAL"
	}

	switch resolvedConfig.OptionalMode {
	case "EXCLUDE_ALL_OPTIONAL", "INCLUDE_ALL_OPTIONAL", "CUSTOM":
		// Valid
	default:
		return nil, NewConditionLoggingError(), ErrL4LBConfigInvalidMode
	}

	if slc.SampleRate != nil {
		resolvedConfig.SampleRate = float64(*slc.SampleRate) / maxSampleRate
	} else {
		resolvedConfig.SampleRate = defaultSampleRate
	}

	return resolvedConfig, NewConditionLoggingReconciled(), nil
}

// GetReasonForError returns a machine-readable reason for K8s events.
func GetReasonForError(err error) string {
	if errors.Is(err, ErrL4LBConfigDoesNotExist) {
		return ReasonL4LBConfigNotFound
	} else if errors.Is(err, ErrL4LBConfigFailedToGet) {
		return ReasonL4LBConfigFetchFailed
	} else if errors.Is(err, ErrL4LBConfigInvalidMode) {
		return ReasonL4LBConfigInvalidMode
	}
	return "L4LBConfigUnknownError"
}
