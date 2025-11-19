package mixedprotocolilbtest

import (
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"google.golang.org/api/compute/v1"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/l4resources/mixedprotocoltest"
)

// TCPResources returns GCE resources for a TCP IPv4 ILB that listens on ports 80 and 443
func TCPResources() mixedprotocoltest.GCEResources {
	return mixedprotocoltest.GCEResources{
		ForwardingRules: map[string]*compute.ForwardingRule{
			mixedprotocoltest.ForwardingRuleTCPIPv4Name: ForwardingRuleTCP([]string{"80", "443"}),
		},
		Firewalls: map[string]*compute.Firewall{
			mixedprotocoltest.FirewallIPv4Name: mixedprotocoltest.Firewall([]*compute.FirewallAllowed{
				{IPProtocol: "tcp", Ports: []string{"80", "443"}},
			}),
			mixedprotocoltest.HealthCheckFirewallIPv4Name: mixedprotocoltest.HealthCheckFirewall(),
		},
		HealthCheck:    HealthCheck(),
		BackendService: BackendService("TCP"),
	}
}

// UDPResources returns GCE resources for an UDP IPv4 ILB that listens on port 53
func UDPResources() mixedprotocoltest.GCEResources {
	return mixedprotocoltest.GCEResources{
		ForwardingRules: map[string]*compute.ForwardingRule{
			mixedprotocoltest.ForwardingRuleUDPIPv4Name: ForwardingRuleUDP([]string{"53"}),
		},
		Firewalls: map[string]*compute.Firewall{
			mixedprotocoltest.FirewallIPv4Name: mixedprotocoltest.Firewall([]*compute.FirewallAllowed{
				{IPProtocol: "udp", Ports: []string{"53"}},
			}),
			mixedprotocoltest.HealthCheckFirewallIPv4Name: mixedprotocoltest.HealthCheckFirewall(),
		},
		HealthCheck:    HealthCheck(),
		BackendService: BackendService("UDP"),
	}
}

// L3Resources returns GCE resources for a mixed protocol IPv4 ILB, that listens on tcp:80, tcp:443 and udp:53
func L3Resources() mixedprotocoltest.GCEResources {
	return mixedprotocoltest.GCEResources{
		ForwardingRules: map[string]*compute.ForwardingRule{
			mixedprotocoltest.ForwardingRuleL3IPv4Name: ForwardingRuleL3(),
		},
		Firewalls: map[string]*compute.Firewall{
			mixedprotocoltest.FirewallIPv4Name: mixedprotocoltest.Firewall([]*compute.FirewallAllowed{
				{IPProtocol: "udp", Ports: []string{"53"}},
				{IPProtocol: "tcp", Ports: []string{"80", "443"}},
			}),
			mixedprotocoltest.HealthCheckFirewallIPv4Name: mixedprotocoltest.HealthCheckFirewall(),
		},
		HealthCheck:    HealthCheck(),
		BackendService: BackendService("UNSPECIFIED"),
	}
}

// TCPResourcesIPv6 returns GCE resources for a TCP IPv6 ILB that listens on ports 80 and 443
func TCPResourcesIPv6() mixedprotocoltest.GCEResources {
	return mixedprotocoltest.GCEResources{
		ForwardingRules: map[string]*compute.ForwardingRule{
			mixedprotocoltest.ForwardingRuleTCPIPv6Name: ForwardingrukeRuleTCPIPv6([]string{"80", "443"}),
		},
		Firewalls: map[string]*compute.Firewall{
			mixedprotocoltest.FirewallIPv6Name: mixedprotocoltest.FirewallIPv6([]*compute.FirewallAllowed{
				{IPProtocol: "tcp", Ports: []string{"80", "443"}},
			}),
			mixedprotocoltest.HealthCheckFirewallIPv6Name: mixedprotocoltest.HealthCheckFirewallIPv6(),
		},
		HealthCheck:    HealthCheck(),
		BackendService: BackendService("TCP"),
	}
}

// UDPResourcesIPv6 returns GCE resources for an UDP IPv6 ILB that listens on port 53
func UDPResourcesIPv6() mixedprotocoltest.GCEResources {
	return mixedprotocoltest.GCEResources{
		ForwardingRules: map[string]*compute.ForwardingRule{
			mixedprotocoltest.ForwardingRuleUDPIPv6Name: ForwardingRuleUDPIPv6([]string{"53"}),
		},
		Firewalls: map[string]*compute.Firewall{
			mixedprotocoltest.FirewallIPv6Name: mixedprotocoltest.FirewallIPv6([]*compute.FirewallAllowed{
				{IPProtocol: "udp", Ports: []string{"53"}},
			}),
			mixedprotocoltest.HealthCheckFirewallIPv6Name: mixedprotocoltest.HealthCheckFirewallIPv6(),
		},
		HealthCheck:    HealthCheck(),
		BackendService: BackendService("UDP"),
	}
}

// L3ResourcesIPv6 returns GCE resources for a mixed protocol IPv6 ILB, that listens on tcp:80, tcp:443 and udp:53
func L3ResourcesIPv6() mixedprotocoltest.GCEResources {
	return mixedprotocoltest.GCEResources{
		ForwardingRules: map[string]*compute.ForwardingRule{
			mixedprotocoltest.ForwardingRuleL3IPv6Name: ForwardingRuleL3IPv6(),
		},
		Firewalls: map[string]*compute.Firewall{
			mixedprotocoltest.FirewallIPv6Name: mixedprotocoltest.FirewallIPv6([]*compute.FirewallAllowed{
				{IPProtocol: "udp", Ports: []string{"53"}},
				{IPProtocol: "tcp", Ports: []string{"80", "443"}},
			}),
			mixedprotocoltest.HealthCheckFirewallIPv6Name: mixedprotocoltest.HealthCheckFirewallIPv6(),
		},
		HealthCheck:    HealthCheck(),
		BackendService: BackendService("UNSPECIFIED"),
	}
}

// TCPResourcesDualStack returns GCE resources for a TCP dual stack ILB that listens on ports 80 and 443
func TCPResourcesDualStack() mixedprotocoltest.GCEResources {
	return mixedprotocoltest.GCEResources{
		ForwardingRules: map[string]*compute.ForwardingRule{
			mixedprotocoltest.ForwardingRuleTCPIPv4Name: ForwardingRuleTCP([]string{"80", "443"}),
			mixedprotocoltest.ForwardingRuleTCPIPv6Name: ForwardingrukeRuleTCPIPv6([]string{"80", "443"}),
		},
		Firewalls: map[string]*compute.Firewall{
			mixedprotocoltest.FirewallIPv4Name: mixedprotocoltest.Firewall([]*compute.FirewallAllowed{
				{IPProtocol: "tcp", Ports: []string{"80", "443"}},
			}),
			mixedprotocoltest.FirewallIPv6Name: mixedprotocoltest.FirewallIPv6([]*compute.FirewallAllowed{
				{IPProtocol: "tcp", Ports: []string{"80", "443"}},
			}),
			mixedprotocoltest.HealthCheckFirewallIPv4Name: mixedprotocoltest.HealthCheckFirewall(),
			mixedprotocoltest.HealthCheckFirewallIPv6Name: mixedprotocoltest.HealthCheckFirewallIPv6(),
		},
		HealthCheck:    HealthCheck(),
		BackendService: BackendService("TCP"),
	}
}

// UDPResourcesDualStack returns GCE resources for an UDP dual stack ILB that listens on port 53
func UDPResourcesDualStack() mixedprotocoltest.GCEResources {
	return mixedprotocoltest.GCEResources{
		ForwardingRules: map[string]*compute.ForwardingRule{
			mixedprotocoltest.ForwardingRuleUDPIPv4Name: ForwardingRuleUDP([]string{"53"}),
			mixedprotocoltest.ForwardingRuleUDPIPv6Name: ForwardingRuleUDPIPv6([]string{"53"}),
		},
		Firewalls: map[string]*compute.Firewall{
			mixedprotocoltest.FirewallIPv4Name: mixedprotocoltest.Firewall([]*compute.FirewallAllowed{
				{IPProtocol: "udp", Ports: []string{"53"}},
			}),
			mixedprotocoltest.FirewallIPv6Name: mixedprotocoltest.FirewallIPv6([]*compute.FirewallAllowed{
				{IPProtocol: "udp", Ports: []string{"53"}},
			}),
			mixedprotocoltest.HealthCheckFirewallIPv4Name: mixedprotocoltest.HealthCheckFirewall(),
			mixedprotocoltest.HealthCheckFirewallIPv6Name: mixedprotocoltest.HealthCheckFirewallIPv6(),
		},
		HealthCheck:    HealthCheck(),
		BackendService: BackendService("UDP"),
	}
}

// L3ResourcesDualStack returns GCE resources for a mixed protocol dual stack ILB, that listens on tcp:80, tcp:443 and udp:53
func L3ResourcesDualStack() mixedprotocoltest.GCEResources {
	return mixedprotocoltest.GCEResources{
		ForwardingRules: map[string]*compute.ForwardingRule{
			mixedprotocoltest.ForwardingRuleL3IPv4Name: ForwardingRuleL3(),
			mixedprotocoltest.ForwardingRuleL3IPv6Name: ForwardingRuleL3IPv6(),
		},
		Firewalls: map[string]*compute.Firewall{
			mixedprotocoltest.FirewallIPv4Name: mixedprotocoltest.Firewall([]*compute.FirewallAllowed{
				{IPProtocol: "udp", Ports: []string{"53"}},
				{IPProtocol: "tcp", Ports: []string{"80", "443"}},
			}),
			mixedprotocoltest.FirewallIPv6Name: mixedprotocoltest.FirewallIPv6([]*compute.FirewallAllowed{
				{IPProtocol: "udp", Ports: []string{"53"}},
				{IPProtocol: "tcp", Ports: []string{"80", "443"}},
			}),
			mixedprotocoltest.HealthCheckFirewallIPv4Name: mixedprotocoltest.HealthCheckFirewall(),
			mixedprotocoltest.HealthCheckFirewallIPv6Name: mixedprotocoltest.HealthCheckFirewallIPv6(),
		},
		HealthCheck:    HealthCheck(),
		BackendService: BackendService("UNSPECIFIED"),
	}
}

// ForwardingRuleL3 returns an L3 Forwarding Rule for ILB
func ForwardingRuleL3() *compute.ForwardingRule {
	return &compute.ForwardingRule{
		Name:                mixedprotocoltest.ForwardingRuleL3IPv4Name,
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
		Name:                mixedprotocoltest.ForwardingRuleL3IPv6Name,
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
		Name:                mixedprotocoltest.ForwardingRuleUDPIPv4Name,
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
		Name:                mixedprotocoltest.ForwardingRuleUDPIPv6Name,
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
		Name:                mixedprotocoltest.ForwardingRuleTCPIPv4Name,
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
		Name:                mixedprotocoltest.ForwardingRuleTCPIPv6Name,
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
		Name:                mixedprotocoltest.BackendServiceName,
		Region:              "us-central1",
		Protocol:            protocol,
		SessionAffinity:     "NONE",
		LoadBalancingScheme: "INTERNAL",
		HealthChecks:        []string{"https://www.googleapis.com/compute/v1/projects/test-project/global/healthChecks/k8s2-axyqjz2d-l4-shared-hc"},
		Description:         `{"networking.gke.io/service-name":"test-namespace/test-name","networking.gke.io/api-version":"ga"}`,
	}
}

// HealthCheck returns shared HealthCheck
func HealthCheck() *composite.HealthCheck {
	return &composite.HealthCheck{
		Name:               mixedprotocoltest.HealthCheckName,
		CheckIntervalSec:   8,
		TimeoutSec:         1,
		HealthyThreshold:   1,
		UnhealthyThreshold: 3,
		Type:               "HTTP",
		Version:            meta.VersionGA,
		Scope:              meta.Global,
		HttpHealthCheck:    &composite.HTTPHealthCheck{Port: 10256, RequestPath: "/healthz"},
		Description:        `{"networking.gke.io/service-name":"","networking.gke.io/api-version":"ga","networking.gke.io/resource-description":"This resource is shared by all L4 ILB Services using ExternalTrafficPolicy: Cluster."}`,
	}
}
