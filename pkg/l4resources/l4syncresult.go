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
	"k8s.io/ingress-gce/pkg/l4annotations"
)

const (
	SyncTypeCreate = "create"
	SyncTypeUpdate = "update"
	SyncTypeDelete = "delete"
)

var L4ResourceAnnotationKeys = []string{
	l4annotations.BackendServiceKey,
	l4annotations.TCPForwardingRuleKey,
	l4annotations.UDPForwardingRuleKey,
	l4annotations.L3ForwardingRuleKey,
	l4annotations.HealthcheckKey,
	l4annotations.FirewallRuleKey,
	l4annotations.FirewallRuleForHealthcheckKey,
}

var l4IPv6ResourceAnnotationKeys = []string{
	l4annotations.FirewallRuleIPv6Key,
	l4annotations.FirewallRuleForHealthcheckIPv6Key,
	l4annotations.TCPForwardingRuleIPv6Key,
	l4annotations.UDPForwardingRuleIPv6Key,
	l4annotations.L3ForwardingRuleIPv6Key,
}
var L4DualStackResourceAnnotationKeys = append(L4ResourceAnnotationKeys, l4IPv6ResourceAnnotationKeys...)
