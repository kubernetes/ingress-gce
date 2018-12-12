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
	"strconv"

	"k8s.io/ingress-gce/pkg/annotations"
	v1beta1 "k8s.io/ingress-gce/pkg/apis/cloud/v1beta1"

	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// The below vars are used for sharing unit testing types with multiple packages.
var (
	TestBackendConfig = &v1beta1.BackendConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "config-test",
			Namespace: "test",
		},
	}

	SvcWithoutConfig = &apiv1.Service{
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

	SvcWithTestConfig = &apiv1.Service{
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

	SvcWithTestConfigPortNumber = &apiv1.Service{
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

	SvcWithTestConfigMismatchPort = &apiv1.Service{
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

	SvcWithDefaultTestConfig = &apiv1.Service{
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

	SvcWithTestConfigOtherNamespace = &apiv1.Service{
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

	SvcWithOtherConfig = &apiv1.Service{
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

	SvcWithDefaultOtherConfig = &apiv1.Service{
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

	SvcWithInvalidConfig = &apiv1.Service{
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

// BackendConfigName returns the name of the BackendConfig which is associated
// with the given ServicePort.
func BackendConfigName(backendConfigs annotations.BackendConfigs, svcPort apiv1.ServicePort) string {
	configName := ""
	// Both port name and port number are allowed.
	if name, ok := backendConfigs.Ports[svcPort.Name]; ok {
		configName = name
	} else if name, ok := backendConfigs.Ports[strconv.Itoa(int(svcPort.Port))]; ok {
		configName = name
	} else if len(backendConfigs.Default) != 0 {
		configName = backendConfigs.Default
	}
	return configName
}
