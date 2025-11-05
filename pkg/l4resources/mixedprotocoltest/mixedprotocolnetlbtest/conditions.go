package mixedprotocolnetlbtest

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/ingress-gce/pkg/l4conditions"
)

// ConditionsTCP returns expected conditions for TCP NetLB
func ConditionsTCP() []metav1.Condition {
	return []metav1.Condition{
		condition(l4conditions.BackendServiceConditionType, "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i"),
		condition(l4conditions.HealthCheckConditionType, "k8s2-axyqjz2d-l4-shared-hc"),
		condition(l4conditions.FirewallRuleConditionType, "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i"),
		condition(l4conditions.FirewallHealthCheckConditionType, "k8s2-axyqjz2d-l4-shared-hc-fw"),
		condition(l4conditions.TCPForwardingRuleConditionType, "a1234567890"),
	}
}

// ConditionsTCPIPv6 returns expected conditions for TCP IPv6 NetLB
func ConditionsTCPIPv6() []metav1.Condition {
	return []metav1.Condition{
		condition(l4conditions.BackendServiceConditionType, "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i"),
		condition(l4conditions.HealthCheckConditionType, "k8s2-axyqjz2d-l4-shared-hc"),
		condition(l4conditions.IPv6FirewallRuleConditionType, "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i-ipv6"),
		condition(l4conditions.FirewallHealthCheckIPv6ConditionType, "k8s2-axyqjz2d-l4-shared-hc-fw-ipv6"),
		condition(l4conditions.TCPIPv6ForwardingRuleConditionType, "a1234567890-ipv6"),
	}
}

// ConditionsTCPDualStack returns expected conditions for TCP DualStack NetLB
func ConditionsTCPDualStack() []metav1.Condition {
	return append(ConditionsTCP(),
		condition(l4conditions.IPv6FirewallRuleConditionType, "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i-ipv6"),
		condition(l4conditions.FirewallHealthCheckIPv6ConditionType, "k8s2-axyqjz2d-l4-shared-hc-fw-ipv6"),
		condition(l4conditions.TCPIPv6ForwardingRuleConditionType, "a1234567890-ipv6"),
	)
}

// ConditionsUDP returns expected conditions for UDP NetLB
func ConditionsUDP() []metav1.Condition {
	return []metav1.Condition{
		condition(l4conditions.BackendServiceConditionType, "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i"),
		condition(l4conditions.HealthCheckConditionType, "k8s2-axyqjz2d-l4-shared-hc"),
		condition(l4conditions.FirewallRuleConditionType, "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i"),
		condition(l4conditions.FirewallHealthCheckConditionType, "k8s2-axyqjz2d-l4-shared-hc-fw"),
		condition(l4conditions.UDPForwardingRuleConditionType, "a1234567890"),
	}
}

// ConditionsUDPIPv6 returns expected conditions for UDP IPv6 NetLB
func ConditionsUDPIPv6() []metav1.Condition {
	return []metav1.Condition{
		condition(l4conditions.BackendServiceConditionType, "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i"),
		condition(l4conditions.HealthCheckConditionType, "k8s2-axyqjz2d-l4-shared-hc"),
		condition(l4conditions.IPv6FirewallRuleConditionType, "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i-ipv6"),
		condition(l4conditions.FirewallHealthCheckIPv6ConditionType, "k8s2-axyqjz2d-l4-shared-hc-fw-ipv6"),
		condition(l4conditions.UDPIPv6ForwardingRuleConditionType, "a1234567890-ipv6"),
	}
}

// ConditionsUDPDualStack returns expected conditions for UDP DualStack NetLB
func ConditionsUDPDualStack() []metav1.Condition {
	return append(ConditionsUDP(),
		condition(l4conditions.IPv6FirewallRuleConditionType, "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i-ipv6"),
		condition(l4conditions.FirewallHealthCheckIPv6ConditionType, "k8s2-axyqjz2d-l4-shared-hc-fw-ipv6"),
		condition(l4conditions.UDPIPv6ForwardingRuleConditionType, "a1234567890-ipv6"),
	)
}

// ConditionsMixed returns expected conditions for Mixed Protocol NetLB
func ConditionsMixed() []metav1.Condition {
	return []metav1.Condition{
		condition(l4conditions.BackendServiceConditionType, "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i"),
		condition(l4conditions.HealthCheckConditionType, "k8s2-axyqjz2d-l4-shared-hc"),
		condition(l4conditions.FirewallRuleConditionType, "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i"),
		condition(l4conditions.FirewallHealthCheckConditionType, "k8s2-axyqjz2d-l4-shared-hc-fw"),
		condition(l4conditions.TCPForwardingRuleConditionType, "k8s2-tcp-axyqjz2d-test-namespace-test-name-yuvhdy7i"),
		condition(l4conditions.UDPForwardingRuleConditionType, "k8s2-udp-axyqjz2d-test-namespace-test-name-yuvhdy7i"),
	}
}

// ConditionsMixedIPv6 returns expected conditions for Mixed Protocol IPv6 NetLB
func ConditionsMixedIPv6() []metav1.Condition {
	return []metav1.Condition{
		condition(l4conditions.BackendServiceConditionType, "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i"),
		condition(l4conditions.HealthCheckConditionType, "k8s2-axyqjz2d-l4-shared-hc"),
		condition(l4conditions.IPv6FirewallRuleConditionType, "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i-ipv6"),
		condition(l4conditions.FirewallHealthCheckIPv6ConditionType, "k8s2-axyqjz2d-l4-shared-hc-fw-ipv6"),
		condition(l4conditions.TCPIPv6ForwardingRuleConditionType, "k8s2-tcp-axyqjz2d-test-namespace-test-name-yuvhdy7i-ipv6"),
		condition(l4conditions.UDPIPv6ForwardingRuleConditionType, "k8s2-udp-axyqjz2d-test-namespace-test-name-yuvhdy7i-ipv6"),
	}
}

// ConditionsMixedDualStack returns expected conditions for Mixed Protocol DualStack NetLB
func ConditionsMixedDualStack() []metav1.Condition {
	return append(ConditionsMixed(),
		condition(l4conditions.IPv6FirewallRuleConditionType, "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i-ipv6"),
		condition(l4conditions.FirewallHealthCheckIPv6ConditionType, "k8s2-axyqjz2d-l4-shared-hc-fw-ipv6"),
		condition(l4conditions.TCPIPv6ForwardingRuleConditionType, "k8s2-tcp-axyqjz2d-test-namespace-test-name-yuvhdy7i-ipv6"),
		condition(l4conditions.UDPIPv6ForwardingRuleConditionType, "k8s2-udp-axyqjz2d-test-namespace-test-name-yuvhdy7i-ipv6"),
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
