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

package standaloneneg

import (
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/ingress-gce/pkg/l4/resources"
	l4utils "k8s.io/ingress-gce/pkg/l4/utils"
	"k8s.io/ingress-gce/pkg/utils"
)

const (
	// externalIPProgrammed is the type of the condition reporting if the IPs
	// of the forwarding rules are programmed in the service status.
	externalIPProgrammed = "ExternalIPProgrammed"

	reasonIPProgrammed          = lbConditionReason("IPProgrammed")
	reasonNoForwardingRuleRef   = lbConditionReason("NoForwardingRuleRef")
	reasonUnsupportedLBType     = lbConditionReason("UnsupportedLBType")
	reasonInvalidForwardingRule = lbConditionReason("InvalidForwardingRule")
	reasonBackendNotAttached    = lbConditionReason("BackendNotAttached")
	reasonProviderError         = lbConditionReason("ProviderError")
)

// lbConditionReason represents the reason for conditions created by the Standalone NEG Controller.
type lbConditionReason string

func classifyError(err error) lbConditionReason {
	if err == nil {
		return reasonProviderError
	}
	if l4utils.IsBackendNotAttachedError(err) {
		return reasonBackendNotAttached
	}
	if utils.IsNotFoundError(err) {
		return reasonInvalidForwardingRule
	}
	if l4utils.IsUnsupportedLoadBalancingSchemeError(err) {
		return reasonUnsupportedLBType
	}
	if l4utils.IsUnsupportedProtocolError(err) {
		return reasonInvalidForwardingRule
	}
	if resources.IsUserError(err) {
		return reasonInvalidForwardingRule
	}
	return reasonProviderError
}

func newConditionExternalIPProgrammedTrue(ips []string) metav1.Condition {
	return metav1.Condition{
		Type:               externalIPProgrammed,
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		Reason:             string(reasonIPProgrammed),
		Message:            fmt.Sprintf("IPs programmed: %s", strings.Join(ips, ", ")),
	}
}

func newConditionExternalIPProgrammedFalse(reason lbConditionReason) metav1.Condition {
	return metav1.Condition{
		Type:               externalIPProgrammed,
		Status:             metav1.ConditionFalse,
		LastTransitionTime: metav1.Now(),
		Reason:             string(reason),
		Message:            messageForReason(reason),
	}
}

func messageForReason(reason lbConditionReason) string {
	switch reason {
	case reasonNoForwardingRuleRef:
		return "Service is missing the custom forwarding rule reference"
	case reasonUnsupportedLBType:
		return "The referenced forwarding rule has an unsupported load balancing scheme"
	case reasonInvalidForwardingRule:
		return "The custom forwarding rule reference is invalid"
	case reasonBackendNotAttached:
		return "The service NEGs are not attached to the load balancer backend service"
	case reasonProviderError:
		return "GCE provider error encountered while retrieving forwarding rules"
	default:
		return "An unexpected error occurred while programming external IPs"
	}
}
