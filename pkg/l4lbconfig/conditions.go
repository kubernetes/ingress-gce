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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	LoggingConditionType = "LoggingConfigManaged"

	LoggingConditionReconciledReason  = "Reconciled"
	LoggingConditionReconciledMessage = "Logging configuration reconciled successfully."

	LoggingConditionErrorReason  = "Error"
	LoggingConditionErrorMessage = "Error reconciling logging configuration."

	LoggingConditionMissingReason        = "Missing"
	LoggingConditionMissingReasonMessage = "Logging configuration not found."

	LoggingConditionUnmanagedReason        = "Unmanaged"
	LoggingConditionUnmanagedReasonMessage = "Logging configuration not managed."
)

func NewConditionLoggingReconciled() metav1.Condition {
	return metav1.Condition{
		LastTransitionTime: metav1.Now(),
		Type:               LoggingConditionType,
		Status:             metav1.ConditionTrue,
		Reason:             LoggingConditionReconciledReason,
		Message:            LoggingConditionReconciledMessage,
	}
}

func NewConditionLoggingError() metav1.Condition {
	return metav1.Condition{
		LastTransitionTime: metav1.Now(),
		Type:               LoggingConditionType,
		Status:             metav1.ConditionFalse,
		Reason:             LoggingConditionErrorReason,
		Message:            LoggingConditionErrorMessage,
	}
}

func NewConditionLoggingMissing() metav1.Condition {
	return metav1.Condition{
		LastTransitionTime: metav1.Now(),
		Type:               LoggingConditionType,
		Status:             metav1.ConditionFalse,
		Reason:             LoggingConditionMissingReason,
		Message:            LoggingConditionMissingReasonMessage,
	}
}

func NewConditionLoggingUnmanaged() metav1.Condition {
	return metav1.Condition{
		LastTransitionTime: metav1.Now(),
		Type:               LoggingConditionType,
		Status:             metav1.ConditionFalse,
		Reason:             LoggingConditionUnmanagedReason,
		Message:            LoggingConditionUnmanagedReasonMessage,
	}
}

func ServiceNeedLoggingCondition(svc *corev1.Service, loggingCondition metav1.Condition) bool {
	if loggingCondition.Reason != LoggingConditionUnmanagedReason {
		return true
	}

	for _, condition := range svc.Status.Conditions {
		if condition.Type == LoggingConditionType {
			return true
		}
	}
	return false // return false only when the logging condition would be Unmanaged and the service does not have Logging Condition yet.
}
