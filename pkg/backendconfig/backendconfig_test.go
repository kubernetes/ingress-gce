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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/cache"

	"k8s.io/ingress-gce/pkg/annotations"
	backendconfigv1beta1 "k8s.io/ingress-gce/pkg/apis/backendconfig/v1beta1"
)

var (
	testBackendConfig = &backendconfigv1beta1.BackendConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "config-test",
			Namespace: "test",
		},
	}

	svcWithoutConfig = &apiv1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc-no-config",
			Namespace: "test",
		},
		Spec: apiv1.ServiceSpec{
			Ports: []apiv1.ServicePort{
				{Name: "port1"},
			},
		},
	}

	svcWithTestConfig = &apiv1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc-test-config",
			Namespace: "test",
			Annotations: map[string]string{
				annotations.BackendConfigKey: `{"ports": {"port1": "config-test"}}`,
			},
		},
		Spec: apiv1.ServiceSpec{
			Ports: []apiv1.ServicePort{
				{Name: "port1"},
			},
		},
	}

	svcWithTestConfigPortNumber = &apiv1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc-test-config-port-number",
			Namespace: "test",
			Annotations: map[string]string{
				annotations.BackendConfigKey: `{"ports": {"443": "config-test"}}`,
			},
		},
		Spec: apiv1.ServiceSpec{
			Ports: []apiv1.ServicePort{
				{Port: 443},
			},
		},
	}

	svcWithTestConfigMismatchPort = &apiv1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc-test-config-mismatch-port",
			Namespace: "test",
			Annotations: map[string]string{
				annotations.BackendConfigKey: `{"ports": {"port2": "config-test"}}`,
			},
		},
		Spec: apiv1.ServiceSpec{
			Ports: []apiv1.ServicePort{
				{Name: "port1"},
			},
		},
	}

	svcWithDefaultTestConfig = &apiv1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc-default-test-config",
			Namespace: "test",
			Annotations: map[string]string{
				annotations.BackendConfigKey: `{"default": "config-test"}`,
			},
		},
		Spec: apiv1.ServiceSpec{
			Ports: []apiv1.ServicePort{
				{Name: "port1"},
			},
		},
	}

	svcWithTestConfigOtherNamespace = &apiv1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc-test-config",
			Namespace: "other-namespace",
			Annotations: map[string]string{
				annotations.BackendConfigKey: `{"ports": {"port1": "config-test"}}`,
			},
		},
		Spec: apiv1.ServiceSpec{
			Ports: []apiv1.ServicePort{
				{Name: "port1"},
			},
		},
	}

	svcWithOtherConfig = &apiv1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc-other-config",
			Namespace: "test",
			Annotations: map[string]string{
				annotations.BackendConfigKey: `{"ports": {"port1": "config-other"}}`,
			},
		},
		Spec: apiv1.ServiceSpec{
			Ports: []apiv1.ServicePort{
				{Name: "port1"},
			},
		},
	}

	svcWithDefaultOtherConfig = &apiv1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc-default-other-config",
			Namespace: "test",
			Annotations: map[string]string{
				annotations.BackendConfigKey: `{"default": "config-other"}`,
			},
		},
		Spec: apiv1.ServiceSpec{
			Ports: []apiv1.ServicePort{
				{Name: "port1"},
			},
		},
	}

	svcWithInvalidConfig = &apiv1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc-invalid-config",
			Namespace: "test",
			Annotations: map[string]string{
				annotations.BackendConfigKey: `invalid`,
			},
		},
		Spec: apiv1.ServiceSpec{
			Ports: []apiv1.ServicePort{
				{Name: "port1"},
			},
		},
	}
)

func TestGetServicesForBackendConfig(t *testing.T) {
	testCases := []struct {
		desc         string
		listFunc     func() []interface{}
		expectedSvcs []*apiv1.Service
	}{
		{
			desc: "should filter service with no backend config",
			listFunc: func() []interface{} {
				return []interface{}{svcWithoutConfig}
			},
		},
		{
			desc: "should include service with test backend config",
			listFunc: func() []interface{} {
				return []interface{}{svcWithTestConfig}
			},
			expectedSvcs: []*apiv1.Service{svcWithTestConfig},
		},
		{
			desc: "should include service with default test backend config",
			listFunc: func() []interface{} {
				return []interface{}{svcWithDefaultTestConfig}
			},
			expectedSvcs: []*apiv1.Service{svcWithDefaultTestConfig},
		},
		{
			desc: "should filter service with test backend config in a different namespace",
			listFunc: func() []interface{} {
				return []interface{}{svcWithTestConfigOtherNamespace}
			},
		},
		{
			desc: "should filter service with a different backend config",
			listFunc: func() []interface{} {
				return []interface{}{svcWithOtherConfig}
			},
		},
		{
			desc: "should filter service with a different default backend config",
			listFunc: func() []interface{} {
				return []interface{}{svcWithDefaultOtherConfig}
			},
		},
		{
			desc: "should filter service with invalid backend config",
			listFunc: func() []interface{} {
				return []interface{}{svcWithInvalidConfig}
			},
		},
		{
			desc: "mixed together: should only include services with corresponding backend config",
			listFunc: func() []interface{} {
				return []interface{}{
					svcWithoutConfig,
					svcWithTestConfig,
					svcWithDefaultTestConfig,
					svcWithTestConfigOtherNamespace,
					svcWithOtherConfig,
					svcWithDefaultOtherConfig,
					svcWithInvalidConfig,
				}
			},
			expectedSvcs: []*apiv1.Service{
				svcWithTestConfig,
				svcWithDefaultTestConfig,
			},
		},
	}

	for _, tc := range testCases {
		fakeStore := &cache.FakeCustomStore{
			ListFunc: tc.listFunc,
		}
		svcs := GetServicesForBackendConfig(fakeStore, testBackendConfig)
		if !sets.NewString(convertServicesToNamespacedNames(svcs)...).Equal(sets.NewString(convertServicesToNamespacedNames(tc.expectedSvcs)...)) {
			t.Errorf("%s: unexpected services, got %v, want %v", tc.desc, svcs, tc.expectedSvcs)
		}
	}
}

func convertServicesToNamespacedNames(svcs []*apiv1.Service) []string {
	svcNames := []string{}
	for _, svc := range svcs {
		svcNames = append(svcNames, fmt.Sprintf("%s:%s", svc.Namespace, svc.Name))
	}
	return svcNames
}

func TestGetBackendConfigForServicePort(t *testing.T) {
	testCases := []struct {
		desc           string
		svc            *apiv1.Service
		svcPort        *apiv1.ServicePort
		getFunc        func(obj interface{}) (item interface{}, exists bool, err error)
		expectedConfig *backendconfigv1beta1.BackendConfig
		expectedErr    error
	}{
		{
			desc:    "service port name with backend config",
			svc:     svcWithTestConfig,
			svcPort: &apiv1.ServicePort{Name: "port1"},
			getFunc: func(obj interface{}) (interface{}, bool, error) {
				return testBackendConfig, true, nil
			},
			expectedConfig: testBackendConfig,
		},
		{
			desc:    "service port number with backend config",
			svc:     svcWithTestConfigPortNumber,
			svcPort: &apiv1.ServicePort{Port: 443},
			getFunc: func(obj interface{}) (interface{}, bool, error) {
				return testBackendConfig, true, nil
			},
			expectedConfig: testBackendConfig,
		},
		{
			desc:    "service with default backend config",
			svc:     svcWithDefaultTestConfig,
			svcPort: &apiv1.ServicePort{Name: "port1"},
			getFunc: func(obj interface{}) (interface{}, bool, error) {
				return testBackendConfig, true, nil
			},
			expectedConfig: testBackendConfig,
		},
		{
			desc: "service with no backend config",
			svc:  svcWithoutConfig,
		},
		{
			desc:        "service with backend config but port doesn't match",
			svc:         svcWithTestConfigMismatchPort,
			svcPort:     &apiv1.ServicePort{Name: "port1"},
			expectedErr: ErrNoBackendConfigForPort,
		},
		{
			desc:    "service port matches backend config but config not exist",
			svc:     svcWithTestConfig,
			svcPort: &apiv1.ServicePort{Name: "port1"},
			getFunc: func(obj interface{}) (interface{}, bool, error) {
				return nil, false, nil
			},
			expectedErr: ErrBackendConfigDoesNotExist,
		},
		{
			desc:    "service port matches backend config but failed to retrieve config",
			svc:     svcWithTestConfig,
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
