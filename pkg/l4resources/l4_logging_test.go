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

package l4resources

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
	"k8s.io/ingress-gce/pkg/l4annotations"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/ingress-gce/pkg/composite"
)

func TestGetL4LoggingConfig(t *testing.T) {
	testCases := []struct {
		desc            string
		service         *v1.Service
		configMapGetter func(obj interface{}) (item interface{}, exists bool, err error)
		wantConfig      *composite.BackendServiceLogConfig
		wantErr         bool
	}{
		{
			desc: "Service without annotation",
			service: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{},
				},
			},
			wantConfig: nil,
		},
		{
			desc: "Service references non-existent config map",
			service: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:   "ns",
					Annotations: map[string]string{l4annotations.L4LoggingConfigMapKey: "invalid"},
				},
			},
			configMapGetter: func(obj interface{}) (item interface{}, exists bool, err error) { return nil, false, nil },
			wantConfig:      nil,
			wantErr:         false,
		},
		{
			desc: "Error during retrieval of config map",
			service: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:   "ns",
					Annotations: map[string]string{l4annotations.L4LoggingConfigMapKey: "invalid"},
				},
			},
			configMapGetter: func(obj interface{}) (item interface{}, exists bool, err error) {
				return nil, false, fmt.Errorf("some error")
			},
			wantConfig: nil,
			wantErr:    true,
		},
		{
			desc: "Service references valid config map",
			service: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:   "ns",
					Annotations: map[string]string{l4annotations.L4LoggingConfigMapKey: "config-map-name"},
				},
			},
			configMapGetter: func(obj interface{}) (item interface{}, exists bool, err error) {
				configMap := &v1.ConfigMap{
					Data: map[string]string{
						LoggingEnableKey:  LoggingEnabledValue,
						SampleRateKey:     "0.4",
						OptionalModeKey:   OptionalModeCustom,
						OptionalFieldsKey: "field1,field2",
					},
				}
				return configMap, true, nil
			},
			wantConfig: &composite.BackendServiceLogConfig{
				Enable:         true,
				SampleRate:     0.4,
				OptionalMode:   OptionalModeCustom,
				OptionalFields: []string{"field1", "field2"},
			},
		},
		{
			desc: "Service references invalid config map",
			service: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:   "ns",
					Annotations: map[string]string{l4annotations.L4LoggingConfigMapKey: "config-map-name"},
				},
			},
			configMapGetter: func(obj interface{}) (item interface{}, exists bool, err error) {
				configMap := &v1.ConfigMap{
					Data: map[string]string{LoggingEnableKey: "invalid"},
				}
				return configMap, true, nil
			},
			wantConfig: nil,
			wantErr:    true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			configMapLister := &cache.FakeCustomStore{GetFunc: tc.configMapGetter}
			loggingConfig, err := GetL4LoggingConfig(tc.service, configMapLister)

			if err != nil && !tc.wantErr {
				t.Errorf("no error expected, got %v", err)
			}
			if err == nil && tc.wantErr {
				t.Errorf("error expected, got %v", err)
			}

			if !reflect.DeepEqual(loggingConfig, tc.wantConfig) {
				t.Errorf("ParseL4LoggingConfigMap() returned %v, expected %v. Diff(res, expected): %s", loggingConfig, tc.wantConfig, cmp.Diff(loggingConfig, tc.wantConfig))
			}
		})
	}
}

func TestParseL4LoggingConfigMap(t *testing.T) {
	testCases := []struct {
		desc       string
		configMap  *v1.ConfigMap
		wantConfig *composite.BackendServiceLogConfig
		wantError  string
	}{
		{
			desc:       "No config map",
			configMap:  nil,
			wantConfig: nil,
		},
		{
			desc:       "Empty config map",
			configMap:  &v1.ConfigMap{},
			wantConfig: nil,
		},
		{
			desc: "Config map without enable field",
			configMap: &v1.ConfigMap{
				Data: map[string]string{
					SampleRateKey:     "1",
					OptionalModeKey:   OptionalModeCustom,
					OptionalFieldsKey: "field1",
				},
			},
			wantConfig: nil,
		},
		{
			desc: "Explicitly disabled logging",
			configMap: &v1.ConfigMap{
				Data: map[string]string{
					LoggingEnableKey: LoggingDisabledValue,
					SampleRateKey:    "1",
					OptionalModeKey:  OptionalModeIncludeAll,
				},
			},
			wantConfig: nil,
		},
		{
			desc: "Default logging enabled",
			configMap: &v1.ConfigMap{
				Data: map[string]string{
					LoggingEnableKey: LoggingEnabledValue,
				},
			},
			wantConfig: &composite.BackendServiceLogConfig{
				Enable:         true,
				SampleRate:     1,
				OptionalMode:   OptionalModeExcludeAll,
				OptionalFields: []string{},
			},
		},
		{
			desc: "Custom sample rate",
			configMap: &v1.ConfigMap{
				Data: map[string]string{
					LoggingEnableKey: LoggingEnabledValue,
					SampleRateKey:    "0.2",
				},
			},
			wantConfig: &composite.BackendServiceLogConfig{
				Enable:         true,
				SampleRate:     0.2,
				OptionalMode:   OptionalModeExcludeAll,
				OptionalFields: []string{},
			},
		},
		{
			desc: "Invalid sample rate - not numeric value",
			configMap: &v1.ConfigMap{
				Data: map[string]string{
					LoggingEnableKey: LoggingEnabledValue,
					SampleRateKey:    "invalid",
				},
			},
			wantError: `invalid sample rate: "invalid" - should be a float within [0.0, 1.0] range`,
		},
		{
			desc: "Invalid sample rate - greater than 1 (100%)",
			configMap: &v1.ConfigMap{
				Data: map[string]string{
					LoggingEnableKey: LoggingEnabledValue,
					SampleRateKey:    "2",
				},
			},
			wantError: `invalid sample rate: "2" - should be within [0.0, 1.0] range`,
		},
		{
			desc: "Invalid sample rate - lower than 0 (0%)",
			configMap: &v1.ConfigMap{
				Data: map[string]string{
					LoggingEnableKey: LoggingEnabledValue,
					SampleRateKey:    "-1",
				},
			},
			wantError: `invalid sample rate: "-1" - should be within [0.0, 1.0] range`,
		},
		{
			desc: "Optional mode - EXCLUDE_ALL_OPTIONAL",
			configMap: &v1.ConfigMap{
				Data: map[string]string{
					LoggingEnableKey: LoggingEnabledValue,
					OptionalModeKey:  OptionalModeExcludeAll,
				},
			},
			wantConfig: &composite.BackendServiceLogConfig{
				Enable:         true,
				SampleRate:     1,
				OptionalMode:   OptionalModeExcludeAll,
				OptionalFields: []string{},
			},
		},
		{
			desc: "Optional mode - INCLUDE_ALL_OPTIONAL",
			configMap: &v1.ConfigMap{
				Data: map[string]string{
					LoggingEnableKey: LoggingEnabledValue,
					OptionalModeKey:  OptionalModeIncludeAll,
				},
			},
			wantConfig: &composite.BackendServiceLogConfig{
				Enable:         true,
				SampleRate:     1,
				OptionalMode:   OptionalModeIncludeAll,
				OptionalFields: []string{},
			},
		},
		{
			desc: "Optional mode - CUSTOM",
			configMap: &v1.ConfigMap{
				Data: map[string]string{
					LoggingEnableKey: LoggingEnabledValue,
					OptionalModeKey:  OptionalModeCustom,
				},
			},
			wantConfig: &composite.BackendServiceLogConfig{
				Enable:         true,
				SampleRate:     1,
				OptionalMode:   OptionalModeCustom,
				OptionalFields: []string{},
			},
		},
		{
			desc: "Optional mode - invalid",
			configMap: &v1.ConfigMap{
				Data: map[string]string{
					LoggingEnableKey: LoggingEnabledValue,
					OptionalModeKey:  "invalid",
				},
			},
			wantError: `invalid optional mode: "invalid", available options are: "EXCLUDE_ALL_OPTIONAL", "INCLUDE_ALL_OPTIONAL", "CUSTOM"`,
		},
		{
			desc: "Optional fields - comma-separated",
			configMap: &v1.ConfigMap{
				Data: map[string]string{
					LoggingEnableKey:  LoggingEnabledValue,
					OptionalModeKey:   OptionalModeCustom,
					OptionalFieldsKey: "field1,field2",
				},
			},
			wantConfig: &composite.BackendServiceLogConfig{
				Enable:         true,
				SampleRate:     1,
				OptionalMode:   OptionalModeCustom,
				OptionalFields: []string{"field1", "field2"},
			},
		},
		{
			desc: `Optional fields - ", " as separator`,
			configMap: &v1.ConfigMap{
				Data: map[string]string{
					LoggingEnableKey:  LoggingEnabledValue,
					OptionalModeKey:   OptionalModeCustom,
					OptionalFieldsKey: "field1, field2",
				},
			},
			wantConfig: &composite.BackendServiceLogConfig{
				Enable:         true,
				SampleRate:     1,
				OptionalMode:   OptionalModeCustom,
				OptionalFields: []string{"field1", "field2"},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			loggingConfig, err := ParseL4LoggingConfigMap(tc.configMap)

			if err != nil && err.Error() != tc.wantError {
				t.Errorf("unexpected error %v, expected %v", err, tc.wantError)
			}
			if err == nil && tc.wantError != "" {
				t.Errorf("got no error, want %q", tc.wantError)
			}

			if !reflect.DeepEqual(loggingConfig, tc.wantConfig) {
				t.Errorf("ParseL4LoggingConfigMap() returned %v, expected %v. Diff(res, expected): %s", loggingConfig, tc.wantConfig, cmp.Diff(loggingConfig, tc.wantConfig))
			}
		})
	}
}
