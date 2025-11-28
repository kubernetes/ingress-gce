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
package l4resources

import (
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/l4conditions"
)

const (
	SyncTypeCreate = "create"
	SyncTypeUpdate = "update"
	SyncTypeDelete = "delete"
)

var L4ResourceAnnotationKeys = []string{
	annotations.BackendServiceKey,
	annotations.TCPForwardingRuleKey,
	annotations.UDPForwardingRuleKey,
	annotations.L3ForwardingRuleKey,
	annotations.HealthcheckKey,
	annotations.FirewallRuleKey,
	annotations.FirewallRuleForHealthcheckKey,
}

var l4IPv6ResourceAnnotationKeys = []string{
	annotations.FirewallRuleIPv6Key,
	annotations.FirewallRuleForHealthcheckIPv6Key,
	annotations.TCPForwardingRuleIPv6Key,
	annotations.UDPForwardingRuleIPv6Key,
	annotations.L3ForwardingRuleIPv6Key,
}
var L4DualStackResourceAnnotationKeys = append(L4ResourceAnnotationKeys, l4IPv6ResourceAnnotationKeys...)

var L4ResourceConditionTypes = []string{
	l4conditions.BackendServiceConditionType,
	l4conditions.HealthCheckConditionType,
	l4conditions.TCPForwardingRuleConditionType,
	l4conditions.UDPForwardingRuleConditionType,
	l4conditions.L3ForwardingRuleConditionType,
	l4conditions.FirewallRuleConditionType,
	l4conditions.FirewallHealthCheckConditionType,
}

var L4IPv6ResourceConditionTypes = []string{
	l4conditions.FirewallHealthCheckIPv6ConditionType,
	l4conditions.IPv6FirewallRuleConditionType,
	l4conditions.TCPIPv6ForwardingRuleConditionType,
	l4conditions.UDPIPv6ForwardingRuleConditionType,
	l4conditions.L3IPv6ForwardingRuleConditionType,
}

var L4DualStackResourceConditionTypes = append(L4ResourceConditionTypes, L4IPv6ResourceConditionTypes...)
