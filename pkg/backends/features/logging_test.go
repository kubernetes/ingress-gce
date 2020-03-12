/*
Copyright 2015 The Kubernetes Authors.

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
	"testing"

	"github.com/google/go-cmp/cmp"
	backendconfigv1 "k8s.io/ingress-gce/pkg/apis/backendconfig/v1"
	"k8s.io/ingress-gce/pkg/composite"
	testutils "k8s.io/ingress-gce/pkg/test"
	"k8s.io/ingress-gce/pkg/utils"
)

func TestEnsureLogging(t *testing.T) {
	for _, tc := range []struct {
		desc         string
		sp           utils.ServicePort
		be           *composite.BackendService
		expectUpdate bool
	}{
		{
			desc: "logging configurations are missing from spec, no update needed",
			sp: utils.ServicePort{
				BackendConfig: &backendconfigv1.BackendConfig{
					Spec: backendconfigv1.BackendConfigSpec{
						Logging: nil,
					},
				},
			},
			be: &composite.BackendService{
				LogConfig: &composite.BackendServiceLogConfig{
					Enable:     true,
					SampleRate: 0.5,
				},
			},
			expectUpdate: false,
		},
		{
			desc: "empty log config, updated to disable logging",
			sp: utils.ServicePort{
				BackendConfig: &backendconfigv1.BackendConfig{
					Spec: backendconfigv1.BackendConfigSpec{
						Logging: &backendconfigv1.LogConfig{},
					},
				},
			},
			be: &composite.BackendService{
				LogConfig: &composite.BackendServiceLogConfig{
					Enable:     true,
					SampleRate: 0.5,
				},
			},
			expectUpdate: true,
		},
		{
			desc: "settings are identical, no update needed",
			sp: utils.ServicePort{
				BackendConfig: &backendconfigv1.BackendConfig{
					Spec: backendconfigv1.BackendConfigSpec{
						Logging: &backendconfigv1.LogConfig{
							Enable:     true,
							SampleRate: testutils.Float64ToPtr(0.5),
						},
					},
				},
			},
			be: &composite.BackendService{
				LogConfig: &composite.BackendServiceLogConfig{
					Enable:     true,
					SampleRate: 0.5,
				},
			},
			expectUpdate: false,
		},
		{
			desc: "logging is disabled but config is different, no update needed",
			sp: utils.ServicePort{
				BackendConfig: &backendconfigv1.BackendConfig{
					Spec: backendconfigv1.BackendConfigSpec{
						Logging: &backendconfigv1.LogConfig{
							Enable:     false,
							SampleRate: testutils.Float64ToPtr(0.4),
						},
					},
				},
			},
			be: &composite.BackendService{
				LogConfig: &composite.BackendServiceLogConfig{
					Enable:     false,
					SampleRate: 1.0,
				},
			},
			expectUpdate: false,
		},
		{
			desc: "no existing settings, update needed",
			sp: utils.ServicePort{
				BackendConfig: &backendconfigv1.BackendConfig{
					Spec: backendconfigv1.BackendConfigSpec{
						Logging: &backendconfigv1.LogConfig{
							Enable:     true,
							SampleRate: testutils.Float64ToPtr(0.5),
						},
					},
				},
			},
			be: &composite.BackendService{
				LogConfig: nil,
			},
			expectUpdate: true,
		},
		{
			desc: "sample rate is different, update needed",
			sp: utils.ServicePort{
				BackendConfig: &backendconfigv1.BackendConfig{
					Spec: backendconfigv1.BackendConfigSpec{
						Logging: &backendconfigv1.LogConfig{
							Enable:     true,
							SampleRate: testutils.Float64ToPtr(0.5),
						},
					},
				},
			},
			be: &composite.BackendService{
				LogConfig: &composite.BackendServiceLogConfig{
					Enable:     true,
					SampleRate: 0.2,
				},
			},
			expectUpdate: true,
		},
		{
			desc: "enable logging, update needed",
			sp: utils.ServicePort{
				BackendConfig: &backendconfigv1.BackendConfig{
					Spec: backendconfigv1.BackendConfigSpec{
						Logging: &backendconfigv1.LogConfig{
							Enable:     true,
							SampleRate: testutils.Float64ToPtr(0.5),
						},
					},
				},
			},
			be: &composite.BackendService{
				LogConfig: &composite.BackendServiceLogConfig{
					Enable: false,
				},
			},
			expectUpdate: true,
		},
		{
			desc: "disable logging, update needed",
			sp: utils.ServicePort{
				BackendConfig: &backendconfigv1.BackendConfig{
					Spec: backendconfigv1.BackendConfigSpec{
						Logging: &backendconfigv1.LogConfig{
							Enable: false,
						},
					},
				},
			},
			be: &composite.BackendService{
				LogConfig: &composite.BackendServiceLogConfig{
					Enable:     true,
					SampleRate: 1.0,
				},
			},
			expectUpdate: true,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			hasUpdated := EnsureLogging(tc.sp, tc.be)
			if hasUpdated != tc.expectUpdate {
				t.Errorf("%v: expected %v but got %v", tc.desc, tc.expectUpdate, hasUpdated)
			}
		})
	}
}

func TestExpectedBackendServiceLogConfig(t *testing.T) {
	for _, tc := range []struct {
		desc            string
		sp              utils.ServicePort
		expectLogConfig *composite.BackendServiceLogConfig
	}{
		{
			desc: "empty log config",
			sp: utils.ServicePort{
				BackendConfig: &backendconfigv1.BackendConfig{
					Spec: backendconfigv1.BackendConfigSpec{
						Logging: &backendconfigv1.LogConfig{},
					},
				},
			},
			expectLogConfig: &composite.BackendServiceLogConfig{
				Enable: false,
			},
		},
		{
			desc: "logging disabled",
			sp: utils.ServicePort{
				BackendConfig: &backendconfigv1.BackendConfig{
					Spec: backendconfigv1.BackendConfigSpec{
						Logging: &backendconfigv1.LogConfig{
							Enable:     false,
							SampleRate: testutils.Float64ToPtr(0.5),
						},
					},
				},
			},
			expectLogConfig: &composite.BackendServiceLogConfig{
				Enable: false,
			},
		},
		{
			desc: "logging enabled",
			sp: utils.ServicePort{
				BackendConfig: &backendconfigv1.BackendConfig{
					Spec: backendconfigv1.BackendConfigSpec{
						Logging: &backendconfigv1.LogConfig{
							Enable:     true,
							SampleRate: testutils.Float64ToPtr(0.4),
						},
					},
				},
			},
			expectLogConfig: &composite.BackendServiceLogConfig{
				Enable:     true,
				SampleRate: 0.4,
			},
		},
		{
			desc: "logging enabled, invalid sample rate",
			sp: utils.ServicePort{
				BackendConfig: &backendconfigv1.BackendConfig{
					Spec: backendconfigv1.BackendConfigSpec{
						Logging: &backendconfigv1.LogConfig{
							Enable:     true,
							SampleRate: testutils.Float64ToPtr(1.4),
						},
					},
				},
			},
			expectLogConfig: &composite.BackendServiceLogConfig{
				Enable:     true,
				SampleRate: 1.4,
			},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			gotLogConfig := expectedBackendServiceLogConfig(tc.sp)
			if diff := cmp.Diff(tc.expectLogConfig, gotLogConfig); diff != "" {
				t.Errorf("expectedBackendServiceLogConfig(%#v) mismatch (-want +got):\n%s", tc.sp, diff)
			}
		})
	}
}
