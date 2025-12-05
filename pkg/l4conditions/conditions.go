package l4conditions

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	BackendServiceConditionType = "ServiceLoadBalancerBackendService"

	TCPForwardingRuleConditionType = "ServiceLoadBalancerTCPForwardingRule"
	UDPForwardingRuleConditionType = "ServiceLoadBalancerUDPForwardingRule"
	L3ForwardingRuleConditionType  = "ServiceLoadBalancerL3ForwardingRule"

	TCPIPv6ForwardingRuleConditionType = "ServiceLoadBalancerTCPIPv6ForwardingRule"
	UDPIPv6ForwardingRuleConditionType = "ServiceLoadBalancerUDPIPv6ForwardingRule"
	L3IPv6ForwardingRuleConditionType  = "ServiceLoadBalancerL3IPv6ForwardingRule"

	HealthCheckConditionType             = "ServiceLoadBalancerHealthCheck"
	FirewallRuleConditionType            = "ServiceLoadBalancerFirewallRule"
	IPv6FirewallRuleConditionType        = "ServiceLoadBalancerIPv6FirewallRule"
	FirewallHealthCheckConditionType     = "ServiceLoadBalancerFirewallRuleForHealthCheck"
	FirewallHealthCheckIPv6ConditionType = "ServiceLoadBalancerFirewallRuleForHealthCheckIPv6"

	ConditionReason = "GCEResourceAllocated"
)

func NewConditionResourceAllocated(conditionType string, resourceName string) metav1.Condition {
	return metav1.Condition{
		LastTransitionTime: metav1.Now(),
		Type:               conditionType,
		Status:             metav1.ConditionTrue,
		Reason:             ConditionReason,
		Message:            resourceName,
	}
}

// NewBackendServiceCondition creates a condition for the backend service.
func NewBackendServiceCondition(bsName string) metav1.Condition {
	return NewConditionResourceAllocated(BackendServiceConditionType, bsName)
}

// NewTCPForwardingRuleCondition creates a condition for the TCP forwarding rule.
func NewTCPForwardingRuleCondition(frName string) metav1.Condition {
	return NewConditionResourceAllocated(TCPForwardingRuleConditionType, frName)
}

// NewUDPForwardingRuleCondition creates a condition for the UDP forwarding rule.
func NewUDPForwardingRuleCondition(frName string) metav1.Condition {
	return NewConditionResourceAllocated(UDPForwardingRuleConditionType, frName)
}

// NewL3ForwardingRuleCondition creates a condition for the L3 forwarding rule.
func NewL3ForwardingRuleCondition(frName string) metav1.Condition {
	return NewConditionResourceAllocated(L3ForwardingRuleConditionType, frName)
}

// NewTCPIPv6ForwardingRuleCondition creates a condition for the TCP forwarding rule.
func NewTCPIPv6ForwardingRuleCondition(frName string) metav1.Condition {
	return NewConditionResourceAllocated(TCPIPv6ForwardingRuleConditionType, frName)
}

// NewUDPIPv6ForwardingRuleCondition creates a condition for the UDP forwarding rule.
func NewUDPIPv6ForwardingRuleCondition(frName string) metav1.Condition {
	return NewConditionResourceAllocated(UDPIPv6ForwardingRuleConditionType, frName)
}

// NewL3IPv6ForwardingRuleCondition creates a condition for the L3 forwarding rule.
func NewL3IPv6ForwardingRuleCondition(frName string) metav1.Condition {
	return NewConditionResourceAllocated(L3IPv6ForwardingRuleConditionType, frName)
}

// NewHealthCheckCondition creates a condition for the health check.
func NewHealthCheckCondition(hcName string) metav1.Condition {
	return NewConditionResourceAllocated(HealthCheckConditionType, hcName)
}

// NewFirewallCondition creates a condition for the firewall.
func NewFirewallCondition(fwName string) metav1.Condition {
	return NewConditionResourceAllocated(FirewallRuleConditionType, fwName)
}

// NewIPv6FirewallCondition creates a condition for the IPv6 firewall.
func NewIPv6FirewallCondition(fwName string) metav1.Condition {
	return NewConditionResourceAllocated(IPv6FirewallRuleConditionType, fwName)
}

// NewFirewallHealthCheckCondition creates a condition for the firewall health check.
func NewFirewallHealthCheckCondition(fwName string) metav1.Condition {
	return NewConditionResourceAllocated(FirewallHealthCheckConditionType, fwName)
}

// NewFirewallHealthCheckIPv6Condition creates a condition for the firewall health check IPv6.
func NewFirewallHealthCheckIPv6Condition(fwName string) metav1.Condition {
	return NewConditionResourceAllocated(FirewallHealthCheckIPv6ConditionType, fwName)
}
