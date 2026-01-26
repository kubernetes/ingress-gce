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
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	l4lbconfigv1 "k8s.io/ingress-gce/pkg/apis/l4lbconfig/v1"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/l4annotations"
)

func TestGetL4LBConfigForService(t *testing.T) {
	t.Parallel()

	testNamespace := "default"
	configName := "l4-logging-profile"

	// Mock L4LBConfig Object
	validConfig := &l4lbconfigv1.L4LBConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configName,
			Namespace: testNamespace,
		},
		Spec: l4lbconfigv1.L4LBConfigSpec{
			Logging: &l4lbconfigv1.LoggingConfig{
				Enabled:    true,
				SampleRate: ptrInt32(500000),
			},
		},
	}

	testCases := []struct {
		desc           string
		svc            *apiv1.Service
		getFunc        func(obj interface{}) (item interface{}, exists bool, err error)
		expectedConfig *l4lbconfigv1.L4LBConfig
		expectedErr    error
	}{
		{
			desc: "Service with no L4LBConfig annotation",
			svc: &apiv1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: "simple-svc", Namespace: testNamespace},
			},
			expectedConfig: nil,
			expectedErr:    nil,
		},
		{
			desc: "Service with annotation but config does not exist",
			svc: &apiv1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "missing-config-svc",
					Namespace: testNamespace,
					Annotations: map[string]string{
						l4annotations.L4LBConfigKey: configName,
					},
				},
			},
			getFunc: func(obj interface{}) (interface{}, bool, error) {
				return nil, false, nil
			},
			expectedConfig: nil,
			expectedErr:    ErrL4LBConfigDoesNotExist,
		},
		{
			desc: "Service with annotation and config exists",
			svc: &apiv1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "healthy-svc",
					Namespace: testNamespace,
					Annotations: map[string]string{
						l4annotations.L4LBConfigKey: configName,
					},
				},
			},
			getFunc: func(obj interface{}) (interface{}, bool, error) {
				return validConfig, true, nil
			},
			expectedConfig: validConfig,
			expectedErr:    nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			fakeStore := &cache.FakeCustomStore{
				GetFunc: tc.getFunc,
			}
			config, err := GetL4LBConfigForService(fakeStore, tc.svc)
			if err != tc.expectedErr {
				t.Fatalf("Expected error %v, got %v", tc.expectedErr, err)
			}
			if !reflect.DeepEqual(config, tc.expectedConfig) {
				t.Errorf("Config mismatch. Diff: %v", cmp.Diff(tc.expectedConfig, config))
			}
		})
	}
}

func TestConvertL4LBConfigToBackendServiceLogConfig(t *testing.T) {
	t.Parallel()

	sampleRate := int32(750000)
	testCases := []struct {
		desc     string
		input    *l4lbconfigv1.L4LBConfig
		expected *composite.BackendServiceLogConfig
	}{
		{
			desc:  "Nil input",
			input: nil,
			expected: &composite.BackendServiceLogConfig{
				Enable: false,
			},
		},
		{
			desc: "Full config conversion",
			input: &l4lbconfigv1.L4LBConfig{
				Spec: l4lbconfigv1.L4LBConfigSpec{
					Logging: &l4lbconfigv1.LoggingConfig{
						Enabled:        true,
						SampleRate:     &sampleRate,
						OptionalMode:   "INCLUDE_ALL_OPTIONAL",
						OptionalFields: []string{"field1", "field2"},
					},
				},
			},
			expected: &composite.BackendServiceLogConfig{
				Enable:         true,
				SampleRate:     0.75,
				OptionalMode:   "INCLUDE_ALL_OPTIONAL",
				OptionalFields: []string{"field1", "field2"},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			got := ConvertL4LBConfigToBackendServiceLogConfig(tc.input)
			if !reflect.DeepEqual(got, tc.expected) {
				t.Errorf("Conversion failed. Got %+v, want %+v", got, tc.expected)
			}
		})
	}
}

func TestUpdateL4LBConfigWithBackendServiceLogConfig(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		desc        string
		initial     *l4lbconfigv1.L4LBConfig
		gceState    *composite.BackendServiceLogConfig
		expectedLog *l4lbconfigv1.LoggingConfig
	}{
		{
			desc:    "Back-syncing from GCE enabled state",
			initial: &l4lbconfigv1.L4LBConfig{},
			gceState: &composite.BackendServiceLogConfig{
				Enable:     true,
				SampleRate: 0.1,
			},
			expectedLog: &l4lbconfigv1.LoggingConfig{
				Enabled:    true,
				SampleRate: ptrInt32(100000),
			},
		},
		{
			desc:     "Back-syncing when GCE state is nil",
			initial:  &l4lbconfigv1.L4LBConfig{},
			gceState: nil,
			expectedLog: &l4lbconfigv1.LoggingConfig{
				Enabled: false,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			result := UpdateL4LBConfigWithBackendServiceLogConfig(tc.initial, tc.gceState)
			if !reflect.DeepEqual(result.Spec.Logging, tc.expectedLog) {
				t.Errorf("Update failed. Got %+v, want %+v", result.Spec.Logging, tc.expectedLog)
			}
		})
	}
}

func ptrInt32(i int32) *int32 {
	return &i
}
