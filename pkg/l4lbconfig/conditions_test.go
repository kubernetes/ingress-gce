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
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestServiceNeedLoggingCondition(t *testing.T) {
	testCases := []struct {
		desc             string
		svc              *corev1.Service
		loggingCondition metav1.Condition
		expectNeed       bool
	}{
		{
			desc:             "Logging condition reason is not unmanaged",
			svc:              &corev1.Service{},
			loggingCondition: NewConditionLoggingReconciled(), // Reason: Reconciled
			expectNeed:       true,
		},
		{
			desc: "Logging condition reason is unmanaged, service has no logging condition",
			svc:  &corev1.Service{},
			loggingCondition: metav1.Condition{
				Type:   LoggingConditionType,
				Status: metav1.ConditionFalse,
				Reason: LoggingConditionUnmanagedReason,
			},
			expectNeed: false,
		},
		{
			desc: "Logging condition reason is unmanaged, service has existing logging condition",
			svc: &corev1.Service{
				Status: corev1.ServiceStatus{
					Conditions: []metav1.Condition{
						{
							Type:   LoggingConditionType,
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			loggingCondition: metav1.Condition{
				Type:   LoggingConditionType,
				Status: metav1.ConditionFalse,
				Reason: LoggingConditionUnmanagedReason,
			},
			expectNeed: true,
		},
		{
			desc: "Logging condition reason is unmanaged, service has other conditions",
			svc: &corev1.Service{
				Status: corev1.ServiceStatus{
					Conditions: []metav1.Condition{
						{
							Type:   "SomeOtherType",
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			loggingCondition: metav1.Condition{
				Type:   LoggingConditionType,
				Status: metav1.ConditionFalse,
				Reason: LoggingConditionUnmanagedReason,
			},
			expectNeed: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			got := ServiceNeedLoggingCondition(tc.svc, tc.loggingCondition)
			if got != tc.expectNeed {
				t.Errorf("ServiceNeedLoggingCondition(svc, condition) = %v; want %v", got, tc.expectNeed)
			}
		})
	}
}
