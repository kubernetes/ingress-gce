package mixedprotocolnetlbtest

import (
	"google.golang.org/api/compute/v1"
	"k8s.io/ingress-gce/pkg/loadbalancers/mixedprotocoltest"
)



// TCPResources returns GCE resources for a TCP IPv4 NetLB that listens on ports 80 and 443
func TCPResources() mixedprotocoltest.GCEResources2 {
	return mixedprotocoltest.GCEResources{
		FwdRuleIPv4: ForwardingRuleTCP([]string{"80", "443"}),
		FirewallIPv4: Firewall([]*compute.FirewallAllowed{
			{IPProtocol: "tcp", Ports: []string{"80", "443"}},
		}),
		BS:             BackendService("TCP"),
		HC:             HealthCheck(),
		HCFirewallIPv4: HealthCheckFirewall(),
	}
}

// UDPResources returns GCE resources for an UDP IPv4 NetLB that listens on port 53
func UDPResources() mixedprotocoltest.GCEResources {
	return mixedprotocoltest.GCEResources{
		FwdRuleIPv4: ForwardingRuleUDP([]string{"53"}),
		FirewallIPv4: Firewall([]*compute.FirewallAllowed{
			{IPProtocol: "udp", Ports: []string{"53"}},
		}),
		BS:             BackendService("UDP"),
		HC:             HealthCheck(),
		HCFirewallIPv4: HealthCheckFirewall(),
	}
}

// MixedResources returns GCE resources for a mixed protocol IPv4 NetLB, that listens on tcp:80, tcp:443 and udp:53
func MixedResources() mixedprotocoltest.GCEResources {
	return mixedprotocoltest.GCEResources{
		FwdRuleIPv4: ForwardingRuleL3(),
		FirewallIPv4: Firewall([]*compute.FirewallAllowed{
			{IPProtocol: "udp", Ports: []string{"53"}},
			{IPProtocol: "tcp", Ports: []string{"80", "443"}},
		}),
		BS:             BackendService("UNSPECIFIED"),
		HC:             HealthCheck(),
		HCFirewallIPv4: HealthCheckFirewall(),
	}
}

// HealthCheck returns shared HealthCheck
func HealthCheck() *compute.HealthCheck {
	return &compute.HealthCheck{
		Name:               "k8s2-axyqjz2d-l4-shared-hc",
		CheckIntervalSec:   8,
		TimeoutSec:         1,
		HealthyThreshold:   1,
		UnhealthyThreshold: 3,
		Type:               "HTTP",
		HttpHealthCheck:    &compute.HTTPHealthCheck{Port: 10256, RequestPath: "/healthz"},
		Description:        `{"networking.gke.io/service-name":"","networking.gke.io/api-version":"ga","networking.gke.io/resource-description":"This resource is shared by all L4 ILB Services using ExternalTrafficPolicy: Cluster."}`,
	}
}

// HealthCheckFirewall returns Firewall for HealthCheck
func HealthCheckFirewall() *compute.Firewall {
	return &compute.Firewall{
		Name:    "k8s2-axyqjz2d-l4-shared-hc-fw",
		Allowed: []*compute.FirewallAllowed{{IPProtocol: "TCP", Ports: []string{"10256"}}},
		// GCE defined health check ranges
		SourceRanges: []string{"130.211.0.0/22", "35.191.0.0/16", "209.85.152.0/22", "209.85.204.0/22"},
		TargetTags:   []string{mixedprotocoltest.TestNode},
		Description:  `{"networking.gke.io/service-name":"","networking.gke.io/api-version":"ga","networking.gke.io/resource-description":"This resource is shared by all L4  Services using ExternalTrafficPolicy: Cluster."}`,
	}
}

// ForwardingRuleUDP returns a UDP Forwarding Rule with specified ports
func ForwardingRuleUDP(ports []string) *compute.ForwardingRule {
	return &compute.ForwardingRule{
		Name:                "k8s2-udp-axyqjz2d-test-namespace-test-name-yuvhdy7i",
		Region:              "us-central1",
		IPProtocol:          "UDP",
		Ports:               ports,
		BackendService:      "https://www.googleapis.com/compute/v1/projects/test-project/regions/us-central1/backendServices/k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i",
		LoadBalancingScheme: "EXTERNAL",
		NetworkTier:         "PREMIUM",
		Description:         `{"networking.gke.io/service-name":"test-namespace/test-name","networking.gke.io/api-version":"ga"}`,
	}
}

// ForwardingRuleTCP returns a TCP Forwarding Rule with specified ports
func ForwardingRuleTCP(ports []string) *compute.ForwardingRule {
	return &compute.ForwardingRule{
		Name:                "k8s2-tcp-axyqjz2d-test-namespace-test-name-yuvhdy7i",
		Region:              "us-central1",
		IPProtocol:          "TCP",
		Ports:               ports,
		BackendService:      "https://www.googleapis.com/compute/v1/projects/test-project/regions/us-central1/backendServices/k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i",
		LoadBalancingScheme: "EXTERNAL",
		NetworkTier:         "PREMIUM",
		Description:         `{"networking.gke.io/service-name":"test-namespace/test-name","networking.gke.io/api-version":"ga"}`,
	}
}

// BackendService returns Backend Service for NetLB
// protocol should be set to:
// - `UNSPECIFIED` for mixed protocol (L3)
// - `TCP` for TCP only
// - `UDP` for UDP only
func BackendService(protocol string) *compute.BackendService {
	return &compute.BackendService{
		Name:                "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i",
		Region:              "us-central1",
		Protocol:            protocol,
		SessionAffinity:     "NONE",
		LoadBalancingScheme: "EXTERNAL",
		HealthChecks:        []string{"https://www.googleapis.com/compute/v1/projects/test-project/global/healthChecks/k8s2-axyqjz2d-l4-shared-hc"},
		Description:         `{"networking.gke.io/service-name":"test-namespace/test-name","networking.gke.io/api-version":"ga"}`,
	}
}

// Firewall returns NetLB Firewall with specified allowed rules
func Firewall(allowed []*compute.FirewallAllowed) *compute.Firewall {
	return &compute.Firewall{
		Name:         "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i",
		Allowed:      allowed,
		Description:  `{"networking.gke.io/service-name":"test-namespace/test-name","networking.gke.io/api-version":"ga"}`,
		SourceRanges: []string{"0.0.0.0/0"},
		TargetTags:   []string{mixedprotocoltest.TestNode},
	}
}
