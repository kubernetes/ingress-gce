package mixedprotocoltest

import (
	"google.golang.org/api/compute/v1"
)

// GCEResources collects all GCE resources that are managed by the mixed protocol ILB
type GCEResources struct {
	FwdRuleIPv4    *compute.ForwardingRule
	FirewallIPv4   *compute.Firewall
	FwdRuleIPv6    *compute.ForwardingRule
	FirewallIPv6   *compute.Firewall
	HC             *compute.HealthCheck
	HCFirewallIPv4 *compute.Firewall
	HCFirewallIPv6 *compute.Firewall
	BS             *compute.BackendService
}

// TCPResources returns GCE resources for a TCP IPv4 ILB that listens on ports 80 and 443
func TCPResources() GCEResources {
	return GCEResources{
		FwdRuleIPv4: ForwardingRuleTCP([]string{"80", "443"}),
		FirewallIPv4: Firewall([]*compute.FirewallAllowed{
			{IPProtocol: "tcp", Ports: []string{"80", "443"}},
		}),
		BS:             BackendService("TCP"),
		HC:             HealthCheck(),
		HCFirewallIPv4: HealthCheckFirewall(),
	}
}

// UDPResources returns GCE resources for an UDP IPv4 ILB that listens on port 53
func UDPResources() GCEResources {
	return GCEResources{
		FwdRuleIPv4: ForwardingRuleUDP([]string{"53"}),
		FirewallIPv4: Firewall([]*compute.FirewallAllowed{
			{IPProtocol: "udp", Ports: []string{"53"}},
		}),
		BS:             BackendService("UDP"),
		HC:             HealthCheck(),
		HCFirewallIPv4: HealthCheckFirewall(),
	}
}

// L3Resources returns GCE resources for a mixed protocol IPv4 ILB, that listens on tcp:80, tcp:443 and udp:53
func L3Resources() GCEResources {
	return GCEResources{
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

// TCPResourcesIPv6 returns GCE resources for a TCP IPv6 ILB that listens on ports 80 and 443
func TCPResourcesIPv6() GCEResources {
	return GCEResources{
		FwdRuleIPv6: ForwardingrukeRuleTCPIPv6([]string{"80", "443"}),
		FirewallIPv6: FirewallIPv6([]*compute.FirewallAllowed{
			{IPProtocol: "tcp", Ports: []string{"80", "443"}},
		}),
		BS:             BackendService("TCP"),
		HC:             HealthCheck(),
		HCFirewallIPv6: HealthCheckFirewallIPv6(),
	}
}

// UDPResourcesIPv6 returns GCE resources for an UDP IPv6 ILB that listens on port 53
func UDPResourcesIPv6() GCEResources {
	return GCEResources{
		FwdRuleIPv6: ForwardingRuleUDPIPv6([]string{"53"}),
		FirewallIPv6: FirewallIPv6([]*compute.FirewallAllowed{
			{IPProtocol: "udp", Ports: []string{"53"}},
		}),
		BS:             BackendService("UDP"),
		HC:             HealthCheck(),
		HCFirewallIPv6: HealthCheckFirewallIPv6(),
	}
}

// L3ResourcesIPv6 returns GCE resources for a mixed protocol IPv6 ILB, that listens on tcp:80, tcp:443 and udp:53
func L3ResourcesIPv6() GCEResources {
	return GCEResources{
		FwdRuleIPv6: ForwardingRuleL3IPv6(),
		FirewallIPv6: FirewallIPv6([]*compute.FirewallAllowed{
			{IPProtocol: "udp", Ports: []string{"53"}},
			{IPProtocol: "tcp", Ports: []string{"80", "443"}},
		}),
		BS:             BackendService("UNSPECIFIED"),
		HC:             HealthCheck(),
		HCFirewallIPv6: HealthCheckFirewallIPv6(),
	}
}

// TCPResourcesDualStack returns GCE resources for a TCP dual stack ILB that listens on ports 80 and 443
func TCPResourcesDualStack() GCEResources {
	return GCEResources{
		FwdRuleIPv4: ForwardingRuleTCP([]string{"80", "443"}),
		FirewallIPv4: Firewall([]*compute.FirewallAllowed{
			{IPProtocol: "tcp", Ports: []string{"80", "443"}},
		}),
		FwdRuleIPv6: ForwardingrukeRuleTCPIPv6([]string{"80", "443"}),
		FirewallIPv6: FirewallIPv6([]*compute.FirewallAllowed{
			{IPProtocol: "tcp", Ports: []string{"80", "443"}},
		}),
		BS:             BackendService("TCP"),
		HC:             HealthCheck(),
		HCFirewallIPv4: HealthCheckFirewall(),
		HCFirewallIPv6: HealthCheckFirewallIPv6(),
	}
}

// UDPResourcesDualStack returns GCE resources for an UDP dual stack ILB that listens on port 53
func UDPResourcesDualStack() GCEResources {
	return GCEResources{
		FwdRuleIPv4: ForwardingRuleUDP([]string{"53"}),
		FirewallIPv4: Firewall([]*compute.FirewallAllowed{
			{IPProtocol: "udp", Ports: []string{"53"}},
		}),
		FwdRuleIPv6: ForwardingRuleUDPIPv6([]string{"53"}),
		FirewallIPv6: FirewallIPv6([]*compute.FirewallAllowed{
			{IPProtocol: "udp", Ports: []string{"53"}},
		}),
		BS:             BackendService("UDP"),
		HC:             HealthCheck(),
		HCFirewallIPv4: HealthCheckFirewall(),
		HCFirewallIPv6: HealthCheckFirewallIPv6(),
	}
}

// L3ResourcesDualStack returns GCE resources for a mixed protocol dual stack ILB, that listens on tcp:80, tcp:443 and udp:53
func L3ResourcesDualStack() GCEResources {
	return GCEResources{
		FwdRuleIPv4: ForwardingRuleL3(),
		FirewallIPv4: Firewall([]*compute.FirewallAllowed{
			{IPProtocol: "udp", Ports: []string{"53"}},
			{IPProtocol: "tcp", Ports: []string{"80", "443"}},
		}),
		FwdRuleIPv6: ForwardingRuleL3IPv6(),
		FirewallIPv6: FirewallIPv6([]*compute.FirewallAllowed{
			{IPProtocol: "udp", Ports: []string{"53"}},
			{IPProtocol: "tcp", Ports: []string{"80", "443"}},
		}),
		BS:             BackendService("UNSPECIFIED"),
		HC:             HealthCheck(),
		HCFirewallIPv4: HealthCheckFirewall(),
		HCFirewallIPv6: HealthCheckFirewallIPv6(),
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
		TargetTags:   []string{TestNode},
		Description:  `{"networking.gke.io/service-name":"","networking.gke.io/api-version":"ga","networking.gke.io/resource-description":"This resource is shared by all L4  Services using ExternalTrafficPolicy: Cluster."}`,
	}
}

// HealthCheckFirewallIPv6 returns Firewall for HealthCheck
func HealthCheckFirewallIPv6() *compute.Firewall {
	return &compute.Firewall{
		Name:    "k8s2-axyqjz2d-l4-shared-hc-fw-ipv6",
		Allowed: []*compute.FirewallAllowed{{IPProtocol: "TCP", Ports: []string{"10256"}}},
		// GCE defined health check ranges
		SourceRanges: []string{"2600:2d00:1:b029::/64"},
		TargetTags:   []string{TestNode},
		Description:  `{"networking.gke.io/service-name":"","networking.gke.io/api-version":"ga","networking.gke.io/resource-description":"This resource is shared by all L4  Services using ExternalTrafficPolicy: Cluster."}`,
	}
}

// ForwardingRuleL3 returns an L3 Forwarding Rule for ILB
func ForwardingRuleL3() *compute.ForwardingRule {
	return &compute.ForwardingRule{
		Name:                "k8s2-l3-axyqjz2d-test-namespace-test-name-yuvhdy7i",
		Region:              "us-central1",
		IPProtocol:          "L3_DEFAULT",
		AllPorts:            true,
		BackendService:      "https://www.googleapis.com/compute/v1/projects/test-project/regions/us-central1/backendServices/k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i",
		LoadBalancingScheme: "INTERNAL",
		NetworkTier:         "PREMIUM",
		Description:         `{"networking.gke.io/service-name":"test-namespace/test-name","networking.gke.io/api-version":"ga"}`,
	}
}

// ForwardingRuleL3IPv6 returns an L3 Forwarding Rule for ILB
func ForwardingRuleL3IPv6() *compute.ForwardingRule {
	return &compute.ForwardingRule{
		Name:                "k8s2-l3-axyqjz2d-test-namespace-test-name-yuvhdy7i-ipv6",
		Region:              "us-central1",
		IPProtocol:          "L3_DEFAULT",
		IpVersion:           "IPV6",
		AllPorts:            true,
		BackendService:      "https://www.googleapis.com/compute/v1/projects/test-project/regions/us-central1/backendServices/k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i",
		LoadBalancingScheme: "INTERNAL",
		NetworkTier:         "PREMIUM",
		Description:         `{"networking.gke.io/service-name":"test-namespace/test-name"}`,
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
		LoadBalancingScheme: "INTERNAL",
		NetworkTier:         "PREMIUM",
		Description:         `{"networking.gke.io/service-name":"test-namespace/test-name","networking.gke.io/api-version":"ga"}`,
	}
}

// ForwardingRuleUDPIPv6 returns a UDP Forwarding Rule with specified ports
func ForwardingRuleUDPIPv6(ports []string) *compute.ForwardingRule {
	return &compute.ForwardingRule{
		Name:                "k8s2-udp-axyqjz2d-test-namespace-test-name-yuvhdy7i-ipv6",
		Region:              "us-central1",
		IPProtocol:          "UDP",
		IpVersion:           "IPV6",
		Ports:               ports,
		BackendService:      "https://www.googleapis.com/compute/v1/projects/test-project/regions/us-central1/backendServices/k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i",
		LoadBalancingScheme: "INTERNAL",
		NetworkTier:         "PREMIUM",
		Description:         `{"networking.gke.io/service-name":"test-namespace/test-name"}`,
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
		LoadBalancingScheme: "INTERNAL",
		NetworkTier:         "PREMIUM",
		Description:         `{"networking.gke.io/service-name":"test-namespace/test-name","networking.gke.io/api-version":"ga"}`,
	}
}

// ForwardingrukeRuleTCPIPv6 returns a TCP Forwarding Rule with specified ports
func ForwardingrukeRuleTCPIPv6(ports []string) *compute.ForwardingRule {
	return &compute.ForwardingRule{
		Name:                "k8s2-tcp-axyqjz2d-test-namespace-test-name-yuvhdy7i-ipv6",
		Region:              "us-central1",
		IPProtocol:          "TCP",
		IpVersion:           "IPV6",
		Ports:               ports,
		BackendService:      "https://www.googleapis.com/compute/v1/projects/test-project/regions/us-central1/backendServices/k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i",
		LoadBalancingScheme: "INTERNAL",
		NetworkTier:         "PREMIUM",
		Description:         `{"networking.gke.io/service-name":"test-namespace/test-name"}`,
	}
}

// BackendService returns Backend Service for ILB
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
		LoadBalancingScheme: "INTERNAL",
		HealthChecks:        []string{"https://www.googleapis.com/compute/v1/projects/test-project/global/healthChecks/k8s2-axyqjz2d-l4-shared-hc"},
		Description:         `{"networking.gke.io/service-name":"test-namespace/test-name","networking.gke.io/api-version":"ga"}`,
	}
}

// Firewall returns ILB Firewall with specified allowed rules
func Firewall(allowed []*compute.FirewallAllowed) *compute.Firewall {
	return &compute.Firewall{
		Name:         "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i",
		Allowed:      allowed,
		Description:  `{"networking.gke.io/service-name":"test-namespace/test-name","networking.gke.io/api-version":"ga"}`,
		SourceRanges: []string{"0.0.0.0/0"},
		TargetTags:   []string{TestNode},
	}
}

// FirewallIPv6 returns ILB Firewall with specified allowed rules
func FirewallIPv6(allowed []*compute.FirewallAllowed) *compute.Firewall {
	return &compute.Firewall{
		Name:         "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i-ipv6",
		Allowed:      allowed,
		Description:  `{"networking.gke.io/service-name":"test-namespace/test-name","networking.gke.io/api-version":"ga"}`,
		SourceRanges: []string{"0::0/0"},
		TargetTags:   []string{TestNode},
	}
}
