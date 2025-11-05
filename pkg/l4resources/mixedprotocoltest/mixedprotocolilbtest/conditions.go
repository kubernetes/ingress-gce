package mixedprotocolilbtest

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/ingress-gce/pkg/l4conditions"
)

// ConditionsTCP returns expected conditions for TCP ILB
func ConditionsTCP() []metav1.Condition {
	return []metav1.Condition{
		condition(l4conditions.BackendServiceConditionType, "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i"),
		condition(l4conditions.HealthCheckConditionType, "k8s2-axyqjz2d-l4-shared-hc"),
		condition(l4conditions.FirewallRuleConditionType, "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i"),
		condition(l4conditions.FirewallHealthCheckConditionType, "k8s2-axyqjz2d-l4-shared-hc-fw"),
		condition(l4conditions.TCPForwardingRuleConditionType, "k8s2-tcp-axyqjz2d-test-namespace-test-name-yuvhdy7i"),
	}
}

// ConditionsTCPIPv6 returns expected conditions for TCP IPv6 ILB
func ConditionsTCPIPv6() []metav1.Condition {
	return []metav1.Condition{
		condition(l4conditions.BackendServiceConditionType, "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i"),
		condition(l4conditions.HealthCheckConditionType, "k8s2-axyqjz2d-l4-shared-hc"),
		condition(l4conditions.IPv6FirewallRuleConditionType, "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i-ipv6"),
		condition(l4conditions.FirewallHealthCheckIPv6ConditionType, "k8s2-axyqjz2d-l4-shared-hc-fw-ipv6"),
		condition(l4conditions.TCPIPv6ForwardingRuleConditionType, "k8s2-tcp-axyqjz2d-test-namespace-test-name-yuvhdy7i-ipv6"),
	}
}

// ConditionsTCPDualStack returns expected conditions for TCP DualStack ILB
func ConditionsTCPDualStack() []metav1.Condition {
	return append(ConditionsTCP(),
		condition(l4conditions.IPv6FirewallRuleConditionType, "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i-ipv6"),
		condition(l4conditions.FirewallHealthCheckIPv6ConditionType, "k8s2-axyqjz2d-l4-shared-hc-fw-ipv6"),
		condition(l4conditions.TCPIPv6ForwardingRuleConditionType, "k8s2-tcp-axyqjz2d-test-namespace-test-name-yuvhdy7i-ipv6"),
	)
}

// ConditionsUDP returns expected conditions for UDP ILB
func ConditionsUDP() []metav1.Condition {
	return []metav1.Condition{
		condition(l4conditions.BackendServiceConditionType, "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i"),
		condition(l4conditions.HealthCheckConditionType, "k8s2-axyqjz2d-l4-shared-hc"),
		condition(l4conditions.FirewallRuleConditionType, "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i"),
		condition(l4conditions.FirewallHealthCheckConditionType, "k8s2-axyqjz2d-l4-shared-hc-fw"),
		condition(l4conditions.UDPForwardingRuleConditionType, "k8s2-udp-axyqjz2d-test-namespace-test-name-yuvhdy7i"),
	}
}

// ConditionsUDPIPv6 returns expected conditions for UDP IPv6 ILB
func ConditionsUDPIPv6() []metav1.Condition {
	return []metav1.Condition{
		condition(l4conditions.BackendServiceConditionType, "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i"),
		condition(l4conditions.HealthCheckConditionType, "k8s2-axyqjz2d-l4-shared-hc"),
		condition(l4conditions.IPv6FirewallRuleConditionType, "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i-ipv6"),
		condition(l4conditions.FirewallHealthCheckIPv6ConditionType, "k8s2-axyqjz2d-l4-shared-hc-fw-ipv6"),
		condition(l4conditions.UDPIPv6ForwardingRuleConditionType, "k8s2-udp-axyqjz2d-test-namespace-test-name-yuvhdy7i-ipv6"),
	}
}

// ConditionsUDPDualStack returns expected conditions for UDP DualStack ILB
func ConditionsUDPDualStack() []metav1.Condition {
	return append(ConditionsUDP(),
		condition(l4conditions.IPv6FirewallRuleConditionType, "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i-ipv6"),
		condition(l4conditions.FirewallHealthCheckIPv6ConditionType, "k8s2-axyqjz2d-l4-shared-hc-fw-ipv6"),
		condition(l4conditions.UDPIPv6ForwardingRuleConditionType, "k8s2-udp-axyqjz2d-test-namespace-test-name-yuvhdy7i-ipv6"),
	)
}

// ConditionsL3 returns expected conditions for Mixed Protocol (L3) ILB
func ConditionsL3() []metav1.Condition {
	return []metav1.Condition{
		condition(l4conditions.BackendServiceConditionType, "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i"),
		condition(l4conditions.HealthCheckConditionType, "k8s2-axyqjz2d-l4-shared-hc"),
		condition(l4conditions.FirewallRuleConditionType, "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i"),
		condition(l4conditions.FirewallHealthCheckConditionType, "k8s2-axyqjz2d-l4-shared-hc-fw"),
		condition(l4conditions.L3ForwardingRuleConditionType, "k8s2-l3-axyqjz2d-test-namespace-test-name-yuvhdy7i"),
	}
}

// ConditionsL3IPv6 returns expected conditions for Mixed Protocol (L3) IPv6 ILB
func ConditionsL3IPv6() []metav1.Condition {
	return []metav1.Condition{
		condition(l4conditions.BackendServiceConditionType, "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i"),
		condition(l4conditions.HealthCheckConditionType, "k8s2-axyqjz2d-l4-shared-hc"),
		condition(l4conditions.IPv6FirewallRuleConditionType, "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i-ipv6"),
		condition(l4conditions.FirewallHealthCheckIPv6ConditionType, "k8s2-axyqjz2d-l4-shared-hc-fw-ipv6"),
		condition(l4conditions.L3IPv6ForwardingRuleConditionType, "k8s2-l3-axyqjz2d-test-namespace-test-name-yuvhdy7i-ipv6"),
	}
}

// ConditionsL3DualStack returns expected conditions for Mixed Protocol (L3) DualStack ILB
func ConditionsL3DualStack() []metav1.Condition {
	return append(ConditionsL3(),
		condition(l4conditions.IPv6FirewallRuleConditionType, "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i-ipv6"),
		condition(l4conditions.FirewallHealthCheckIPv6ConditionType, "k8s2-axyqjz2d-l4-shared-hc-fw-ipv6"),
		condition(l4conditions.L3IPv6ForwardingRuleConditionType, "k8s2-l3-axyqjz2d-test-namespace-test-name-yuvhdy7i-ipv6"),
	)
}

func condition(cType, message string) metav1.Condition {
	return metav1.Condition{
		Type:    cType,
		Status:  metav1.ConditionTrue,
		Reason:  l4conditions.ConditionReason,
		Message: message,
	}
}
