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

package backendconfig

import (
	"fmt"
	"reflect"
	"testing"

	apiv1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"

	v1beta1 "k8s.io/ingress-gce/pkg/apis/cloud/v1beta1"
)

func TestGetBackendConfigForServicePort(t *testing.T) {
	testCases := []struct {
		desc           string
		svc            *apiv1.Service
		svcPort        *apiv1.ServicePort
		getFunc        func(obj interface{}) (item interface{}, exists bool, err error)
		expectedConfig *v1beta1.BackendConfig
		expectedErr    error
	}{
		{
			desc:    "service port name with backend config",
			svc:     SvcWithTestConfig,
			svcPort: &apiv1.ServicePort{Name: "port1"},
			getFunc: func(obj interface{}) (interface{}, bool, error) {
				return TestBackendConfig, true, nil
			},
			expectedConfig: TestBackendConfig,
		},
		{
			desc:    "service port number with backend config",
			svc:     SvcWithTestConfigPortNumber,
			svcPort: &apiv1.ServicePort{Port: 443},
			getFunc: func(obj interface{}) (interface{}, bool, error) {
				return TestBackendConfig, true, nil
			},
			expectedConfig: TestBackendConfig,
		},
		{
			desc:    "service with default backend config",
			svc:     SvcWithDefaultTestConfig,
			svcPort: &apiv1.ServicePort{Name: "port1"},
			getFunc: func(obj interface{}) (interface{}, bool, error) {
				return TestBackendConfig, true, nil
			},
			expectedConfig: TestBackendConfig,
		},
		{
			desc:           "service with no backend config",
			svc:            SvcWithoutConfig,
			expectedConfig: nil,
		},
		{
			desc:        "service with backend config but port doesn't match",
			svc:         SvcWithTestConfigMismatchPort,
			svcPort:     &apiv1.ServicePort{Name: "port1"},
			expectedErr: ErrNoBackendConfigForPort,
		},
		{
			desc:    "service port matches backend config but config not exist",
			svc:     SvcWithTestConfig,
			svcPort: &apiv1.ServicePort{Name: "port1"},
			getFunc: func(obj interface{}) (interface{}, bool, error) {
				return nil, false, nil
			},
			expectedErr: ErrBackendConfigDoesNotExist,
		},
		{
			desc:    "service port matches backend config but failed to retrieve config",
			svc:     SvcWithTestConfig,
			svcPort: &apiv1.ServicePort{Name: "port1"},
			getFunc: func(obj interface{}) (interface{}, bool, error) {
				return nil, false, fmt.Errorf("internal error")
			},
			expectedErr: ErrBackendConfigFailedToGet,
		},
	}

	for _, tc := range testCases {
		fakeStore := &cache.FakeCustomStore{
			GetFunc: tc.getFunc,
		}
		config, err := GetBackendConfigForServicePort(fakeStore, tc.svc, tc.svcPort)
		if !reflect.DeepEqual(config, tc.expectedConfig) || tc.expectedErr != err {
			t.Errorf("%s: GetBackendConfigForServicePort() = %v, %v; want %v, %v", tc.desc, config, tc.expectedConfig, err, tc.expectedErr)
		}
	}
}
