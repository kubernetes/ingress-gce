/*
Copyright 2021 The Kubernetes Authors.

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
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func getMultiplePorts() []v1.ServicePort {
	return []v1.ServicePort{
		{Name: "port1", Port: 8081, Protocol: "TCP", NodePort: 30323, TargetPort: intstr.FromInt(8080)},
		{Name: "port2", Port: 8082, Protocol: "TCP", NodePort: 30323, TargetPort: intstr.FromInt(8080)},
		{Name: "port3", Port: 8083, Protocol: "TCP", NodePort: 30323, TargetPort: intstr.FromInt(8084)},
	}
}

func TestPortsEqualForLBService(t *testing.T) {
	var svc1, svc2 v1.Service
	tests := []struct {
		name           string
		expectedResult bool
		ports1         []v1.ServicePort
		ports2         []v1.ServicePort
	}{
		{
			name:           "one port equal",
			expectedResult: true,
			ports1: []v1.ServicePort{
				{Name: "port1", Port: 8084, Protocol: "TCP", NodePort: 30323, TargetPort: intstr.FromInt(8080)}},
			ports2: []v1.ServicePort{
				{Name: "port1", Port: 8084, Protocol: "TCP", NodePort: 30323, TargetPort: intstr.FromInt(8080)}},
		},
		{
			name:           "multiple ports equal",
			expectedResult: true,
			ports1:         getMultiplePorts(),
			ports2:         getMultiplePorts(),
		},
		{
			name:           "ports empty",
			expectedResult: true,
			ports1:         []v1.ServicePort{},
			ports2:         []v1.ServicePort{},
		},
		{
			name:           "protocol mismatch",
			expectedResult: false,
			ports1: []v1.ServicePort{
				{Name: "port1", Port: 8084, Protocol: "TCP", NodePort: 30323, TargetPort: intstr.FromInt(8080)}},
			ports2: []v1.ServicePort{
				{Name: "port1", Port: 8084, Protocol: "UDP", NodePort: 30323, TargetPort: intstr.FromInt(8080)}},
		},
		{
			name:           "name mismatch",
			expectedResult: false,
			ports1: []v1.ServicePort{
				{Name: "port1", Port: 8084, Protocol: "TCP", NodePort: 30323, TargetPort: intstr.FromInt(8080)}},
			ports2: []v1.ServicePort{
				{Name: "port12", Port: 8084, Protocol: "UDP", NodePort: 30323, TargetPort: intstr.FromInt(8080)}},
		},
		{
			name:           "port mismatch",
			expectedResult: false,
			ports1: []v1.ServicePort{
				{Name: "port1", Port: 8084, Protocol: "TCP", NodePort: 30323, TargetPort: intstr.FromInt(8080)}},
			ports2: []v1.ServicePort{
				{Name: "port1", Port: 8004, Protocol: "UDP", NodePort: 30323, TargetPort: intstr.FromInt(8080)}},
		},
		{
			name:           "node port mismatch",
			expectedResult: false,
			ports1: []v1.ServicePort{
				{Name: "port1", Port: 8084, Protocol: "TCP", NodePort: 30323, TargetPort: intstr.FromInt(8080)}},
			ports2: []v1.ServicePort{
				{Name: "port1", Port: 8084, Protocol: "UDP", NodePort: 30300, TargetPort: intstr.FromInt(8080)}},
		},
		{
			name:           "target port mismatch",
			expectedResult: false,
			ports1: []v1.ServicePort{
				{Name: "port1", Port: 8084, Protocol: "TCP", NodePort: 30323, TargetPort: intstr.FromInt(8080)}},
			ports2: []v1.ServicePort{
				{Name: "port1", Port: 8084, Protocol: "UDP", NodePort: 30300, TargetPort: intstr.FromInt(8081)}},
		},
		{
			name:           "ports length mismatch",
			expectedResult: false,
			ports1:         getMultiplePorts(),
			ports2: []v1.ServicePort{
				{Name: "port1", Port: 8084, Protocol: "UDP", NodePort: 30300, TargetPort: intstr.FromInt(8080)}},
		},
		{
			name:           "compare with empty ports",
			expectedResult: false,
			ports1:         []v1.ServicePort{},
			ports2:         getMultiplePorts(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc1.Spec.Ports = tc.ports1
			svc2.Spec.Ports = tc.ports2
			if PortsEqualForLBService(&svc1, &svc2) != tc.expectedResult {
				t.Errorf("Unexpected result %v, %v should be equal: %v", tc.ports1, tc.ports2, tc.expectedResult)
			}
		})
	}
}
