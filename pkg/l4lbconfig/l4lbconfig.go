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

package l4lbconfig

import (
	"errors"

	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/ingress-gce/pkg/composite"

	apisl4lbconfig "k8s.io/ingress-gce/pkg/apis/l4lbconfig"
	l4lbconfigv1 "k8s.io/ingress-gce/pkg/apis/l4lbconfig/v1"
	"k8s.io/ingress-gce/pkg/crd"
	"k8s.io/ingress-gce/pkg/l4annotations"
)

func CRDMeta() *crd.CRDMeta {
	meta := crd.NewCRDMeta(
		apisl4lbconfig.GroupName,
		"L4LBConfig",
		"L4LBConfigList",
		"l4lbconfig",
		"l4lbconfigs",
		[]*crd.Version{
			// latest version should be the first version
			crd.NewVersion("v1", "k8s.io/ingress-gce/pkg/apis/l4lbconfig/v1.L4LBConfig", l4lbconfigv1.GetOpenAPIDefinitions, false),
		},
	)
	return meta
}

var (
	ErrL4LBConfigDoesNotExist = errors.New("no L4LBConfig for service port exists.")
	ErrL4LBConfigFailedToGet  = errors.New("client had error getting L4LBConfig for service port.")
	ErrNoL4LBConfigForPort    = errors.New("no L4LBConfig name found for service port.")
)

// GetL4LBConfigForService returns the corresponding L4LBConfig for
// the given Service if specified.
func GetL4LBConfigForService(l4lbConfigLister cache.Store, svc *apiv1.Service) (*l4lbconfigv1.L4LBConfig, error) {
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

// ConvertL4LBConfigToBackendServiceLogConfig converts L4LBConfig logging config to BackendServiceLogConfig
func ConvertL4LBConfigToBackendServiceLogConfig(l4lbConfig *l4lbconfigv1.L4LBConfig) *composite.BackendServiceLogConfig {
	if l4lbConfig == nil || l4lbConfig.Spec.Logging == nil {
		return &composite.BackendServiceLogConfig{
			Enable: false,
		}
	}
	return &composite.BackendServiceLogConfig{
		Enable:         l4lbConfig.Spec.Logging.Enabled,
		SampleRate:     float64(*l4lbConfig.Spec.Logging.SampleRate) / 1000000,
		OptionalMode:   l4lbConfig.Spec.Logging.OptionalMode,
		OptionalFields: l4lbConfig.Spec.Logging.OptionalFields,
	}
}

// UpdateL4LBConfigWithBackendServiceLogConfig updates L4LBConfig logging config from BackendServiceLogConfig
func UpdateL4LBConfigWithBackendServiceLogConfig(l4lbConfig *l4lbconfigv1.L4LBConfig, bsLogConfig *composite.BackendServiceLogConfig) *l4lbconfigv1.L4LBConfig {
	if l4lbConfig == nil {
		l4lbConfig = &l4lbconfigv1.L4LBConfig{}
	}

	newL4LBConfig := l4lbConfig.DeepCopy()

	if bsLogConfig == nil {
		newL4LBConfig.Spec.Logging = &l4lbconfigv1.LoggingConfig{Enabled: false}
	} else {
		sampleRate := int32(bsLogConfig.SampleRate * 1000000)
		newL4LBConfig.Spec.Logging = &l4lbconfigv1.LoggingConfig{
			Enabled:        bsLogConfig.Enable,
			SampleRate:     &sampleRate,
			OptionalMode:   bsLogConfig.OptionalMode,
			OptionalFields: bsLogConfig.OptionalFields,
		}
	}

	return newL4LBConfig
}

// IsL4LBConfigEqual compares two L4LBConfig objects for functional equality.
// It returns true if the specs are effectively the same.
func IsL4LBConfigEqual(a, b *l4lbconfigv1.L4LBConfig) bool {
	if a == b {
		return true
	}
	// Handle nil pointers using default values
	if a == nil {
		a = &l4lbconfigv1.L4LBConfig{}
	}
	if b == nil {
		b = &l4lbconfigv1.L4LBConfig{}
	}

	return IsLoggingConfigEqual(a.Spec.Logging, b.Spec.Logging)
}

// IsLoggingConfigEqual handles the business logic for logging equivalence.
// If logging is disabled, other fields (SampleRate, Fields) are ignored.
func IsLoggingConfigEqual(a, b *l4lbconfigv1.LoggingConfig) bool {
	// 1. Handle nil pointers
	if a == b {
		return true
	}
	if a == nil || b == nil {
		// If one is nil and the other isn't, they are equal ONLY IF
		// the non-nil one has Enabled: false (default state).
		nonNil := a
		if a == nil {
			nonNil = b
		}
		return !nonNil.Enabled
	}

	// 2. If both are disabled, they are functionally equal regardless of other fields.
	if !a.Enabled && !b.Enabled {
		return true
	}

	// 3. If enabled states differ, they are not equal.
	if a.Enabled != b.Enabled {
		return false
	}

	// 4. Compare SampleRate (pointer comparison)
	if !equalInt32Pointers(a.SampleRate, b.SampleRate) {
		return false
	}

	// 5. Compare OptionalMode
	if a.OptionalMode != b.OptionalMode {
		return false
	}

	// 6. Compare OptionalFields (slice comparison)
	if len(a.OptionalFields) != len(b.OptionalFields) {
		return false
	}
	for i := range a.OptionalFields {
		if a.OptionalFields[i] != b.OptionalFields[i] {
			return false
		}
	}

	return true
}

// Helper to handle nullable int32 fields safely
func equalInt32Pointers(a, b *int32) bool {
	if a == b {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}
