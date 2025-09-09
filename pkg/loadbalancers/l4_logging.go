/*
Copyright 2025 The Kubernetes Authors.

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

package loadbalancers

import (
	"fmt"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/composite"
)

const (
	LoggingEnableKey       = "enable"
	LoggingEnabledValue    = "true"
	LoggingDisabledValue   = "false"
	SampleRateKey          = "sample-rate"
	OptionalModeKey        = "optional-mode"
	OptionalModeExcludeAll = "EXCLUDE_ALL_OPTIONAL"
	OptionalModeIncludeAll = "INCLUDE_ALL_OPTIONAL"
	OptionalModeCustom     = "CUSTOM"
	OptionalFieldsKey      = "optional-fields"
)

// GetL4LoggingConfig retrieves config map responsible for logging config for service if contains annotation.
// If annotation not specified or config map doesn't exist returns nil.
func GetL4LoggingConfig(service *corev1.Service, configMapLister cache.Store) (*composite.BackendServiceLogConfig, error) {
	loggingConfigName, ok := annotations.FromService(service).GetL4LoggingConfigMapAnnotation()
	if !ok {
		return nil, nil
	}

	obj, exists, err := configMapLister.Get(
		&corev1.ConfigMap{
			ObjectMeta: v1.ObjectMeta{
				Name:      loggingConfigName,
				Namespace: service.Namespace,
			},
		},
	)
	if err != nil || !exists {
		return nil, err
	}

	configMap := obj.(*corev1.ConfigMap)
	logConfig, err := ParseL4LoggingConfigMap(configMap)
	if err != nil {
		return nil, fmt.Errorf("error during parsing of the L4 logging config map: %v", err)
	}

	return logConfig, nil
}

// ParseL4LoggingConfigMap parses logging configs from ConfigMap. Extra fields ignored.
// If any of fields doesn't pass validation - error returned.
func ParseL4LoggingConfigMap(configMap *corev1.ConfigMap) (*composite.BackendServiceLogConfig, error) {
	if configMap == nil {
		return nil, nil
	}

	enabled, err := parseL4LoggingEnabled(configMap)
	if err != nil || !enabled {
		return nil, err
	}

	sampleRate, err := parseL4LoggingSampleRate(configMap)
	if err != nil {
		return nil, err
	}

	optionalMode, err := parseL4LoggingOptionalMode(configMap)
	if err != nil {
		return nil, err
	}

	optionalFields := parseL4LoggingOptionalFields(configMap)

	return &composite.BackendServiceLogConfig{
		Enable:         enabled,
		SampleRate:     sampleRate,
		OptionalMode:   optionalMode,
		OptionalFields: optionalFields,
	}, nil
}

// parseL4LoggingEnabled parses "enable" field. Valid values: "true", "false"
func parseL4LoggingEnabled(configMap *corev1.ConfigMap) (bool, error) {
	val, ok := configMap.Data[LoggingEnableKey]
	if !ok {
		return false, nil
	}

	if val == LoggingEnabledValue || val == LoggingDisabledValue {
		return val == LoggingEnabledValue, nil
	}

	return false, fmt.Errorf("unexpected value of enabled field: %q, valid values are: %q, %q.", val, LoggingEnabledValue, LoggingDisabledValue)
}

// parseL4LoggingSampleRate parses sample rate. Should be float within [0, 1] range. Defaults to 1.
func parseL4LoggingSampleRate(configMap *corev1.ConfigMap) (float64, error) {
	strVal, ok := configMap.Data[SampleRateKey]
	if !ok {
		return 1, nil
	}

	val, err := strconv.ParseFloat(strVal, 64)
	if err != nil {
		return 1, fmt.Errorf("invalid sample rate: %q - should be a float within [0.0, 1.0] range", strVal)
	}

	if val < 0 || val > 1 {
		return 1, fmt.Errorf("invalid sample rate: %q - should be within [0.0, 1.0] range", strVal)
	}

	return val, nil
}

// parseL4LoggingOptionalMode parses optional mode.
// Available options: "EXCLUDE_ALL_OPTIONAL", "INCLUDE_ALL_OPTIONAL", "CUSTOM".
// Defaults to "EXCLUDE_ALL_OPTIONAL".
func parseL4LoggingOptionalMode(configMap *corev1.ConfigMap) (string, error) {
	val, ok := configMap.Data[OptionalModeKey]
	if !ok {
		return OptionalModeExcludeAll, nil
	}

	availableOptions := map[string]any{
		OptionalModeExcludeAll: nil,
		OptionalModeIncludeAll: nil,
		OptionalModeCustom:     nil,
	}

	_, validOption := availableOptions[val]
	if validOption {
		return val, nil
	}

	return "", fmt.Errorf("invalid optional mode: %q, available options are: %q, %q, %q", val, OptionalModeExcludeAll, OptionalModeIncludeAll, OptionalModeCustom)
}

// parseL4LoggingOptionalFields parses optional fields.
// Should be comma-separated list of values, e.g. "val1,val2,val3".
// No validation of values performed.
func parseL4LoggingOptionalFields(configMap *corev1.ConfigMap) []string {
	val, ok := configMap.Data[OptionalFieldsKey]
	if !ok {
		return []string{}
	}

	val = strings.ReplaceAll(val, " ", "") // allows to use ", " as separator
	return strings.Split(val, ",")
}
