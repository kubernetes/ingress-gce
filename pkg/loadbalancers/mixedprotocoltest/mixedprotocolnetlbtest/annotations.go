package mixedprotocolnetlbtest

// AnnotationsTCP returns annotations for TCP NetLB
func AnnotationsTCP() map[string]string {
	return map[string]string{
		"service.kubernetes.io/healthcheck":          "k8s2-axyqjz2d-l4-shared-hc",
		"service.kubernetes.io/firewall-rule-for-hc": "k8s2-axyqjz2d-l4-shared-hc-fw",
		"service.kubernetes.io/backend-service":      "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i",
		"service.kubernetes.io/tcp-forwarding-rule":  "a1234567890",
		"service.kubernetes.io/firewall-rule":        "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i",
	}
}

// AnnotationsTCPIPv6 returns annotations for TCP NetLB
func AnnotationsTCPIPv6() map[string]string {
	return map[string]string{
		"service.kubernetes.io/healthcheck":               "k8s2-axyqjz2d-l4-shared-hc",
		"service.kubernetes.io/firewall-rule-for-hc-ipv6": "k8s2-axyqjz2d-l4-shared-hc-fw-ipv6",
		"service.kubernetes.io/backend-service":           "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i",
		"service.kubernetes.io/tcp-forwarding-rule-ipv6":  "a1234567890-ipv6",
		"service.kubernetes.io/firewall-rule-ipv6":        "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i-ipv6",
	}
}

// AnnotationsTCPDualStack returns annotations for TCP NetLB
func AnnotationsTCPDualStack() map[string]string {
	return map[string]string{
		"service.kubernetes.io/healthcheck":               "k8s2-axyqjz2d-l4-shared-hc",
		"service.kubernetes.io/firewall-rule-for-hc":      "k8s2-axyqjz2d-l4-shared-hc-fw",
		"service.kubernetes.io/backend-service":           "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i",
		"service.kubernetes.io/tcp-forwarding-rule":       "a1234567890",
		"service.kubernetes.io/firewall-rule":             "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i",
		"service.kubernetes.io/firewall-rule-for-hc-ipv6": "k8s2-axyqjz2d-l4-shared-hc-fw-ipv6",
		"service.kubernetes.io/tcp-forwarding-rule-ipv6":  "a1234567890-ipv6",
		"service.kubernetes.io/firewall-rule-ipv6":        "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i-ipv6",
	}
}

// AnnotationsUDP returns annotations for UDP NetLB
func AnnotationsUDP() map[string]string {
	return map[string]string{
		"service.kubernetes.io/healthcheck":          "k8s2-axyqjz2d-l4-shared-hc",
		"service.kubernetes.io/firewall-rule-for-hc": "k8s2-axyqjz2d-l4-shared-hc-fw",
		"service.kubernetes.io/backend-service":      "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i",
		"service.kubernetes.io/udp-forwarding-rule":  "a1234567890",
		"service.kubernetes.io/firewall-rule":        "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i",
	}
}

// AnnotationsUDPIPv6 returns annotations for UDP NetLB
func AnnotationsUDPIPv6() map[string]string {
	return map[string]string{
		"service.kubernetes.io/healthcheck":               "k8s2-axyqjz2d-l4-shared-hc",
		"service.kubernetes.io/firewall-rule-for-hc-ipv6": "k8s2-axyqjz2d-l4-shared-hc-fw-ipv6",
		"service.kubernetes.io/backend-service":           "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i",
		"service.kubernetes.io/udp-forwarding-rule-ipv6":  "a1234567890-ipv6",
		"service.kubernetes.io/firewall-rule-ipv6":        "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i-ipv6",
	}
}

// AnnotationsUDPDualStack returns annotations for UDP NetLB
func AnnotationsUDPDualStack() map[string]string {
	return map[string]string{
		"service.kubernetes.io/healthcheck":               "k8s2-axyqjz2d-l4-shared-hc",
		"service.kubernetes.io/firewall-rule-for-hc":      "k8s2-axyqjz2d-l4-shared-hc-fw",
		"service.kubernetes.io/backend-service":           "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i",
		"service.kubernetes.io/udp-forwarding-rule":       "a1234567890",
		"service.kubernetes.io/firewall-rule":             "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i",
		"service.kubernetes.io/firewall-rule-for-hc-ipv6": "k8s2-axyqjz2d-l4-shared-hc-fw-ipv6",
		"service.kubernetes.io/udp-forwarding-rule-ipv6":  "a1234567890-ipv6",
		"service.kubernetes.io/firewall-rule-ipv6":        "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i-ipv6",
	}
}

// AnnotationsMixed returns annotations for mixed protocol NetLB
func AnnotationsMixed() map[string]string {
	return map[string]string{
		"service.kubernetes.io/healthcheck":          "k8s2-axyqjz2d-l4-shared-hc",
		"service.kubernetes.io/firewall-rule-for-hc": "k8s2-axyqjz2d-l4-shared-hc-fw",
		"service.kubernetes.io/backend-service":      "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i",
		"service.kubernetes.io/udp-forwarding-rule":  "k8s2-udp-axyqjz2d-test-namespace-test-name-yuvhdy7i",
		"service.kubernetes.io/tcp-forwarding-rule":  "k8s2-tcp-axyqjz2d-test-namespace-test-name-yuvhdy7i",
		"service.kubernetes.io/firewall-rule":        "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i",
	}
}

// AnnotationsMixedIPv6 returns annotations for mixed protocol NetLB
func AnnotationsMixedIPv6() map[string]string {
	return map[string]string{
		"service.kubernetes.io/healthcheck":               "k8s2-axyqjz2d-l4-shared-hc",
		"service.kubernetes.io/firewall-rule-for-hc-ipv6": "k8s2-axyqjz2d-l4-shared-hc-fw-ipv6",
		"service.kubernetes.io/backend-service":           "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i",
		"service.kubernetes.io/udp-forwarding-rule-ipv6":  "k8s2-udp-axyqjz2d-test-namespace-test-name-yuvhdy7i-ipv6",
		"service.kubernetes.io/tcp-forwarding-rule-ipv6":  "k8s2-tcp-axyqjz2d-test-namespace-test-name-yuvhdy7i-ipv6",
		"service.kubernetes.io/firewall-rule-ipv6":        "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i-ipv6",
	}
}

// AnnotationsMixedDualStack returns annotations for dual stack NetLB
func AnnotationsMixedDualStack() map[string]string {
	return map[string]string{
		"service.kubernetes.io/healthcheck":               "k8s2-axyqjz2d-l4-shared-hc",
		"service.kubernetes.io/firewall-rule-for-hc":      "k8s2-axyqjz2d-l4-shared-hc-fw",
		"service.kubernetes.io/backend-service":           "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i",
		"service.kubernetes.io/udp-forwarding-rule":       "k8s2-udp-axyqjz2d-test-namespace-test-name-yuvhdy7i",
		"service.kubernetes.io/tcp-forwarding-rule":       "k8s2-tcp-axyqjz2d-test-namespace-test-name-yuvhdy7i",
		"service.kubernetes.io/firewall-rule":             "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i",
		"service.kubernetes.io/firewall-rule-for-hc-ipv6": "k8s2-axyqjz2d-l4-shared-hc-fw-ipv6",
		"service.kubernetes.io/udp-forwarding-rule-ipv6":  "k8s2-udp-axyqjz2d-test-namespace-test-name-yuvhdy7i-ipv6",
		"service.kubernetes.io/tcp-forwarding-rule-ipv6":  "k8s2-tcp-axyqjz2d-test-namespace-test-name-yuvhdy7i-ipv6",
		"service.kubernetes.io/firewall-rule-ipv6":        "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i-ipv6",
	}
}
