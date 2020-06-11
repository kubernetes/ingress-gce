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
		{
			desc: "sample rate neither specified nor preexists, update needed",
			sp: utils.ServicePort{
				BackendConfig: &backendconfigv1.BackendConfig{
					Spec: backendconfigv1.BackendConfigSpec{
						Logging: &backendconfigv1.LogConfig{
							Enable: true,
						},
					},
				},
			},
			be: &composite.BackendService{
				LogConfig: &composite.BackendServiceLogConfig{
					Enable: true,
				},
			},
			expectUpdate: true,
		},
		{
			desc: "sample rate not specified but preexists, no update needed",
			sp: utils.ServicePort{
				BackendConfig: &backendconfigv1.BackendConfig{
					Spec: backendconfigv1.BackendConfigSpec{
						Logging: &backendconfigv1.LogConfig{
							Enable: true,
						},
					},
				},
			},
			be: &composite.BackendService{
				LogConfig: &composite.BackendServiceLogConfig{
					Enable:     true,
					SampleRate: 0.3,
				},
			},
			expectUpdate: false,
		},
		{
			desc: "sample rate specified but does not preexist, update needed",
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
			be: &composite.BackendService{
				LogConfig: &composite.BackendServiceLogConfig{
					Enable: true,
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

func TestEnsureBackendServiceLogConfig(t *testing.T) {
	for _, tc := range []struct {
		desc            string
		sp              utils.ServicePort
		logConfig       *composite.BackendServiceLogConfig
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
			logConfig: &composite.BackendServiceLogConfig{
				Enable: false,
			},
			expectLogConfig: &composite.BackendServiceLogConfig{
				Enable: false,
			},
		},
		{
			desc: "logging stays disabled, sample rate retained",
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
			logConfig: &composite.BackendServiceLogConfig{
				Enable:     false,
				SampleRate: 0.4,
			},
			expectLogConfig: &composite.BackendServiceLogConfig{
				Enable:     false,
				SampleRate: 0.4,
			},
		},
		{
			desc: "logging enabled, sample rate retained",
			sp: utils.ServicePort{
				BackendConfig: &backendconfigv1.BackendConfig{
					Spec: backendconfigv1.BackendConfigSpec{
						Logging: &backendconfigv1.LogConfig{
							Enable: true,
						},
					},
				},
			},
			logConfig: &composite.BackendServiceLogConfig{
				Enable:     false,
				SampleRate: 0.6,
			},
			expectLogConfig: &composite.BackendServiceLogConfig{
				Enable:     true,
				SampleRate: 0.6,
			},
		},
		{
			desc: "logging enabled, sample rate defaults to 1.0",
			sp: utils.ServicePort{
				BackendConfig: &backendconfigv1.BackendConfig{
					Spec: backendconfigv1.BackendConfigSpec{
						Logging: &backendconfigv1.LogConfig{
							Enable: true,
						},
					},
				},
			},
			logConfig: &composite.BackendServiceLogConfig{
				Enable: false,
			},
			expectLogConfig: &composite.BackendServiceLogConfig{
				Enable:     true,
				SampleRate: 1.0,
			},
		},
		{
			desc: "logging stays enabled, sample rate defaults to 1.0",
			sp: utils.ServicePort{
				BackendConfig: &backendconfigv1.BackendConfig{
					Spec: backendconfigv1.BackendConfigSpec{
						Logging: &backendconfigv1.LogConfig{
							Enable: true,
						},
					},
				},
			},
			logConfig: &composite.BackendServiceLogConfig{
				Enable: true,
			},
			expectLogConfig: &composite.BackendServiceLogConfig{
				Enable:     true,
				SampleRate: 1.0,
			},
		},
		{
			desc: "logging stays enabled, sample rate changed",
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
			logConfig: &composite.BackendServiceLogConfig{
				Enable:     true,
				SampleRate: 0.6,
			},
			expectLogConfig: &composite.BackendServiceLogConfig{
				Enable:     true,
				SampleRate: 0.4,
			},
		},
		{
			desc: "logging stays enabled, invalid sample rate",
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
			backendService := &composite.BackendService{
				LogConfig: tc.logConfig,
			}
			ensureBackendServiceLogConfig(tc.sp, backendService)
			if diff := cmp.Diff(tc.expectLogConfig, backendService.LogConfig); diff != "" {
				t.Errorf("ensureBackendServiceLogConfig() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
