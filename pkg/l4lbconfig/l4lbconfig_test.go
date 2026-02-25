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
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	l4lbconfigv1 "k8s.io/ingress-gce/pkg/apis/l4lbconfig/v1"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/l4/annotations"
	"k8s.io/utils/ptr"
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
				SampleRate: ptr.To[int32](500000),
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
						annotations.L4LBConfigKey: configName,
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
			desc: "Service with annotation but client fails",
			svc: &apiv1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "client-error-svc",
					Namespace: testNamespace,
					Annotations: map[string]string{
						annotations.L4LBConfigKey: configName,
					},
				},
			},
			getFunc: func(obj interface{}) (interface{}, bool, error) {
				return nil, false, errors.New("client error")
			},
			expectedConfig: nil,
			expectedErr:    ErrL4LBConfigFailedToGet,
		},
		{
			desc: "Service with annotation and config exists",
			svc: &apiv1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "healthy-svc",
					Namespace: testNamespace,
					Annotations: map[string]string{
						annotations.L4LBConfigKey: configName,
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
			if !errors.Is(err, tc.expectedErr) {
				t.Fatalf("Expected error %v, got %v", tc.expectedErr, err)
			}
			if !reflect.DeepEqual(config, tc.expectedConfig) {
				t.Errorf("Config mismatch. Diff: %v", cmp.Diff(tc.expectedConfig, config))
			}
		})
	}
}

func TestDetermineL4LoggingConfig(t *testing.T) {
	t.Parallel()

	testNamespace := "test-ns"
	configName := "l4-config"

	// Helper to create a basic L4LBConfig CRD
	makeConfig := func(enabled bool, rate *int32) *l4lbconfigv1.L4LBConfig {
		return &l4lbconfigv1.L4LBConfig{
			ObjectMeta: metav1.ObjectMeta{Name: configName, Namespace: testNamespace},
			Spec: l4lbconfigv1.L4LBConfigSpec{
				Logging: &l4lbconfigv1.LoggingConfig{
					Enabled:    enabled,
					SampleRate: rate,
				},
			},
		}
	}

	testCases := []struct {
		desc              string
		manageLoggingFlag bool
		svc               *apiv1.Service
		storeObj          *l4lbconfigv1.L4LBConfig
		expectErr         error
		expectedConfig    *composite.BackendServiceLogConfig
	}{
		{
			desc:              "Global Gate, ManageL4LBLogging flag is OFF",
			manageLoggingFlag: false,
			svc: &apiv1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc",
					Namespace: testNamespace,
					Annotations: map[string]string{
						annotations.L4LBConfigKey: configName,
					},
				},
			},
			storeObj:       makeConfig(true, ptr.To[int32](1000000)),
			expectedConfig: nil, // Should exit early
		},
		{
			desc:              "Fail-Safe, Service references missing CRD",
			manageLoggingFlag: true,
			svc: &apiv1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-missing-crd",
					Namespace: testNamespace,
					Annotations: map[string]string{
						annotations.L4LBConfigKey: "non-existent-config",
					},
				},
			},
			storeObj:       nil,
			expectErr:      ErrL4LBConfigDoesNotExist,
			expectedConfig: nil,
		},
		{
			desc:              "Successful resolution: Enabled with 50% SampleRate",
			manageLoggingFlag: true,
			svc: &apiv1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-ok",
					Namespace: testNamespace,
					Annotations: map[string]string{
						annotations.L4LBConfigKey: configName,
					},
				},
			},
			storeObj: makeConfig(true, ptr.To[int32](500000)),
			expectedConfig: &composite.BackendServiceLogConfig{
				Enable:       true,
				SampleRate:   0.5,
				OptionalMode: "EXCLUDE_ALL_OPTIONAL",
			},
		},
		{
			desc:              "Successful resolution, Explicitly Disabled",
			manageLoggingFlag: true,
			svc: &apiv1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-disabled",
					Namespace: testNamespace,
					Annotations: map[string]string{
						annotations.L4LBConfigKey: configName,
					},
				},
			},
			storeObj: makeConfig(false, ptr.To[int32](1000000)),
			expectedConfig: &composite.BackendServiceLogConfig{
				Enable:       false,
				SampleRate:   1.0,
				OptionalMode: "EXCLUDE_ALL_OPTIONAL",
			},
		},
		{
			desc:              "SampleRate is nil in CRD",
			manageLoggingFlag: true,
			svc: &apiv1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-nil-rate",
					Namespace: testNamespace,
					Annotations: map[string]string{
						annotations.L4LBConfigKey: configName,
					},
				},
			},
			storeObj: makeConfig(true, nil),
			expectedConfig: &composite.BackendServiceLogConfig{
				Enable:       true,
				SampleRate:   1.0,
				OptionalMode: "EXCLUDE_ALL_OPTIONAL",
			},
		},
		{
			desc:              "CRD exists but Logging section is nil",
			manageLoggingFlag: true,
			svc: &apiv1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-nil-logging",
					Namespace: testNamespace,
					Annotations: map[string]string{
						annotations.L4LBConfigKey: configName,
					},
				},
			},
			storeObj: &l4lbconfigv1.L4LBConfig{
				ObjectMeta: metav1.ObjectMeta{Name: configName, Namespace: testNamespace},
				Spec:       l4lbconfigv1.L4LBConfigSpec{Logging: nil},
			},
			expectedConfig: nil,
		},
		{
			desc:              "CRD exists but Logging OptionalMode is invalid",
			manageLoggingFlag: true,
			svc: &apiv1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-invalid-mode",
					Namespace: testNamespace,
					Annotations: map[string]string{
						annotations.L4LBConfigKey: configName,
					},
				},
			},
			storeObj: &l4lbconfigv1.L4LBConfig{
				ObjectMeta: metav1.ObjectMeta{Name: configName, Namespace: testNamespace},
				Spec: l4lbconfigv1.L4LBConfigSpec{
					Logging: &l4lbconfigv1.LoggingConfig{
						Enabled:      true,
						SampleRate:   ptr.To[int32](1000000),
						OptionalMode: "INVALID",
					},
				},
			},
			expectedConfig: nil,
			expectErr:      ErrL4LBConfigInvalidMode,
		},
		{
			desc:              "Invalid, Custom Mode with No Fields",
			manageLoggingFlag: true,
			svc: &apiv1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-custom-no-fields",
					Namespace: testNamespace,
					Annotations: map[string]string{
						annotations.L4LBConfigKey: configName,
					},
				},
			},
			storeObj: &l4lbconfigv1.L4LBConfig{
				ObjectMeta: metav1.ObjectMeta{Name: configName, Namespace: testNamespace},
				Spec: l4lbconfigv1.L4LBConfigSpec{
					Logging: &l4lbconfigv1.LoggingConfig{
						Enabled:      true,
						OptionalMode: "CUSTOM",
					},
				},
			},
			expectedConfig: nil,
			expectErr:      ErrL4LBConfigInvalidMode,
		},
		{
			desc:              "Invalid, Non-Custom Mode with Fields",
			manageLoggingFlag: true,
			svc: &apiv1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-non-custom-fields",
					Namespace: testNamespace,
					Annotations: map[string]string{
						annotations.L4LBConfigKey: configName,
					},
				},
			},
			storeObj: &l4lbconfigv1.L4LBConfig{
				ObjectMeta: metav1.ObjectMeta{Name: configName, Namespace: testNamespace},
				Spec: l4lbconfigv1.L4LBConfigSpec{
					Logging: &l4lbconfigv1.LoggingConfig{
						Enabled:        true,
						OptionalMode:   "INCLUDE_ALL_OPTIONAL",
						OptionalFields: []string{"foo"},
					},
				},
			},
			expectedConfig: nil,
			expectErr:      ErrL4LBConfigInvalidMode,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			// Set the Global Flag state
			flags.F.ManageL4LBLogging = tc.manageLoggingFlag

			// Initialize fake lister
			lister := cache.NewStore(cache.MetaNamespaceKeyFunc)
			if tc.storeObj != nil {
				lister.Add(tc.storeObj)
			}

			// Execute logic
			result, condition, err := DetermineL4LoggingConfig(tc.svc, lister)

			// Assertions
			if !errors.Is(err, tc.expectErr) {
				t.Errorf("Unexpected error state. Expected error: %v, Got: %v", tc.expectErr, err)
			}
			if result != nil && condition.Status != metav1.ConditionTrue {
				t.Errorf("Expected condition.Status=true when result is not nil")
			}

			if diff := cmp.Diff(tc.expectedConfig, result); diff != "" {
				t.Errorf("Resolved config mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestGetReasonForError(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		err            error
		expectedReason string
	}{
		{err: ErrL4LBConfigDoesNotExist, expectedReason: ReasonL4LBConfigNotFound},
		{err: ErrL4LBConfigFailedToGet, expectedReason: ReasonL4LBConfigFetchFailed},
		{err: ErrL4LBConfigInvalidMode, expectedReason: ReasonL4LBConfigInvalidMode},
		{err: errors.New("generic error"), expectedReason: "L4LBConfigUnknownError"},
		{err: nil, expectedReason: "L4LBConfigUnknownError"},
	}

	for _, tc := range testCases {
		reason := GetReasonForError(tc.err)
		if reason != tc.expectedReason {
			t.Errorf("For error %v, expected reason %s, got %s", tc.err, tc.expectedReason, reason)
		}
	}
}
