package mixedprotocoltest

// AnnotationsTCP returns annotations for TCP ILB
func AnnotationsTCP() map[string]string {
	return map[string]string{
		"service.kubernetes.io/healthcheck":          "k8s2-axyqjz2d-l4-shared-hc",
		"service.kubernetes.io/firewall-rule-for-hc": "k8s2-axyqjz2d-l4-shared-hc-fw",
		"service.kubernetes.io/backend-service":      "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i",
		"service.kubernetes.io/tcp-forwarding-rule":  "k8s2-tcp-axyqjz2d-test-namespace-test-name-yuvhdy7i",
		"service.kubernetes.io/firewall-rule":        "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i",
	}
}

// AnnotationsTCPIPv6 returns annotations for TCP ILB
func AnnotationsTCPIPv6() map[string]string {
	return map[string]string{
		"service.kubernetes.io/healthcheck":               "k8s2-axyqjz2d-l4-shared-hc",
		"service.kubernetes.io/firewall-rule-for-hc-ipv6": "k8s2-axyqjz2d-l4-shared-hc-fw-ipv6",
		"service.kubernetes.io/backend-service":           "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i",
		"service.kubernetes.io/tcp-forwarding-rule-ipv6":  "k8s2-tcp-axyqjz2d-test-namespace-test-name-yuvhdy7i-ipv6",
		"service.kubernetes.io/firewall-rule-ipv6":        "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i-ipv6",
	}
}

// AnnotationsTCPDualStack returns annotations for TCP ILB
func AnnotationsTCPDualStack() map[string]string {
	return map[string]string{
		"service.kubernetes.io/healthcheck":               "k8s2-axyqjz2d-l4-shared-hc",
		"service.kubernetes.io/firewall-rule-for-hc":      "k8s2-axyqjz2d-l4-shared-hc-fw",
		"service.kubernetes.io/backend-service":           "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i",
		"service.kubernetes.io/tcp-forwarding-rule":       "k8s2-tcp-axyqjz2d-test-namespace-test-name-yuvhdy7i",
		"service.kubernetes.io/firewall-rule":             "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i",
		"service.kubernetes.io/firewall-rule-for-hc-ipv6": "k8s2-axyqjz2d-l4-shared-hc-fw-ipv6",
		"service.kubernetes.io/tcp-forwarding-rule-ipv6":  "k8s2-tcp-axyqjz2d-test-namespace-test-name-yuvhdy7i-ipv6",
		"service.kubernetes.io/firewall-rule-ipv6":        "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i-ipv6",
	}
}

// AnnotationsUDP returns annotations for UDP ILB
func AnnotationsUDP() map[string]string {
	return map[string]string{
		"service.kubernetes.io/healthcheck":          "k8s2-axyqjz2d-l4-shared-hc",
		"service.kubernetes.io/firewall-rule-for-hc": "k8s2-axyqjz2d-l4-shared-hc-fw",
		"service.kubernetes.io/backend-service":      "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i",
		"service.kubernetes.io/udp-forwarding-rule":  "k8s2-udp-axyqjz2d-test-namespace-test-name-yuvhdy7i",
		"service.kubernetes.io/firewall-rule":        "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i",
	}
}

// AnnotationsUDPIPv6 returns annotations for UDP ILB
func AnnotationsUDPIPv6() map[string]string {
	return map[string]string{
		"service.kubernetes.io/healthcheck":               "k8s2-axyqjz2d-l4-shared-hc",
		"service.kubernetes.io/firewall-rule-for-hc-ipv6": "k8s2-axyqjz2d-l4-shared-hc-fw-ipv6",
		"service.kubernetes.io/backend-service":           "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i",
		"service.kubernetes.io/udp-forwarding-rule-ipv6":  "k8s2-udp-axyqjz2d-test-namespace-test-name-yuvhdy7i-ipv6",
		"service.kubernetes.io/firewall-rule-ipv6":        "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i-ipv6",
	}
}

// AnnotationsUDPDualStack returns annotations for UDP ILB
func AnnotationsUDPDualStack() map[string]string {
	return map[string]string{
		"service.kubernetes.io/healthcheck":               "k8s2-axyqjz2d-l4-shared-hc",
		"service.kubernetes.io/firewall-rule-for-hc":      "k8s2-axyqjz2d-l4-shared-hc-fw",
		"service.kubernetes.io/backend-service":           "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i",
		"service.kubernetes.io/udp-forwarding-rule":       "k8s2-udp-axyqjz2d-test-namespace-test-name-yuvhdy7i",
		"service.kubernetes.io/firewall-rule":             "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i",
		"service.kubernetes.io/firewall-rule-for-hc-ipv6": "k8s2-axyqjz2d-l4-shared-hc-fw-ipv6",
		"service.kubernetes.io/udp-forwarding-rule-ipv6":  "k8s2-udp-axyqjz2d-test-namespace-test-name-yuvhdy7i-ipv6",
		"service.kubernetes.io/firewall-rule-ipv6":        "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i-ipv6",
	}
}

// AnnotationsL3 returns annotations for mixed protocol ILB
func AnnotationsL3() map[string]string {
	return map[string]string{
		"service.kubernetes.io/healthcheck":          "k8s2-axyqjz2d-l4-shared-hc",
		"service.kubernetes.io/firewall-rule-for-hc": "k8s2-axyqjz2d-l4-shared-hc-fw",
		"service.kubernetes.io/backend-service":      "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i",
		"service.kubernetes.io/l3-forwarding-rule":   "k8s2-l3-axyqjz2d-test-namespace-test-name-yuvhdy7i",
		"service.kubernetes.io/firewall-rule":        "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i",
	}
}

// AnnotationsL3IPv6 returns annotations for mixed protocol ILB
func AnnotationsL3IPv6() map[string]string {
	return map[string]string{
		"service.kubernetes.io/healthcheck":               "k8s2-axyqjz2d-l4-shared-hc",
		"service.kubernetes.io/firewall-rule-for-hc-ipv6": "k8s2-axyqjz2d-l4-shared-hc-fw-ipv6",
		"service.kubernetes.io/backend-service":           "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i",
		"service.kubernetes.io/l3-forwarding-rule-ipv6":   "k8s2-l3-axyqjz2d-test-namespace-test-name-yuvhdy7i-ipv6",
		"service.kubernetes.io/firewall-rule-ipv6":        "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i-ipv6",
	}
}

// AnnotationsL3DualStack returns annotations for dual stack ILB
func AnnotationsL3DualStack() map[string]string {
	return map[string]string{
		"service.kubernetes.io/healthcheck":               "k8s2-axyqjz2d-l4-shared-hc",
		"service.kubernetes.io/firewall-rule-for-hc":      "k8s2-axyqjz2d-l4-shared-hc-fw",
		"service.kubernetes.io/backend-service":           "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i",
		"service.kubernetes.io/l3-forwarding-rule":        "k8s2-l3-axyqjz2d-test-namespace-test-name-yuvhdy7i",
		"service.kubernetes.io/firewall-rule":             "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i",
		"service.kubernetes.io/firewall-rule-for-hc-ipv6": "k8s2-axyqjz2d-l4-shared-hc-fw-ipv6",
		"service.kubernetes.io/l3-forwarding-rule-ipv6":   "k8s2-l3-axyqjz2d-test-namespace-test-name-yuvhdy7i-ipv6",
		"service.kubernetes.io/firewall-rule-ipv6":        "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i-ipv6",
	}
}
