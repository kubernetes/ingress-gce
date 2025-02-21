package mixedprotocolnetlbtest

import (
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"google.golang.org/api/compute/v1"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/loadbalancers/mixedprotocoltest"
)

// TCPResources returns GCE resources for a TCP IPv4 NetLB that listens on ports 80 and 443
func TCPResources() mixedprotocoltest.GCEResources {
	return mixedprotocoltest.GCEResources{
		ForwardingRules: map[string]*compute.ForwardingRule{
			mixedprotocoltest.ForwardingRuleLegacyName: ForwardingRuleTCP(
				mixedprotocoltest.ForwardingRuleLegacyName, []string{"80", "443"},
			),
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

// UDPResources returns GCE resources for an UDP IPv4 NetLB that listens on port 53
func UDPResources() mixedprotocoltest.GCEResources {
	return mixedprotocoltest.GCEResources{
		ForwardingRules: map[string]*compute.ForwardingRule{
			mixedprotocoltest.ForwardingRuleLegacyName: ForwardingRuleUDP(
				mixedprotocoltest.ForwardingRuleLegacyName, []string{"53"},
			),
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

// MixedResources returns GCE resources for a mixed protocol IPv4 NetLB, that listens on tcp:80, tcp:443 and udp:53
func MixedResources() mixedprotocoltest.GCEResources {
	return mixedprotocoltest.GCEResources{
		ForwardingRules: map[string]*compute.ForwardingRule{
			mixedprotocoltest.ForwardingRuleTCPIPv4Name: ForwardingRuleTCP(
				mixedprotocoltest.ForwardingRuleTCPIPv4Name, []string{"80", "443"},
			),
			mixedprotocoltest.ForwardingRuleUDPIPv4Name: ForwardingRuleUDP(
				mixedprotocoltest.ForwardingRuleUDPIPv4Name, []string{"53"},
			),
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

// ForwardingRuleUDP returns a UDP Forwarding Rule with specified ports
func ForwardingRuleUDP(name string, ports []string) *compute.ForwardingRule {
	return &compute.ForwardingRule{
		Name:                name,
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
func ForwardingRuleTCP(name string, ports []string) *compute.ForwardingRule {
	return &compute.ForwardingRule{
		Name:                name,
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
		HealthChecks:        []string{"https://www.googleapis.com/compute/v1/projects/test-project/regions/us-central1/healthChecks/k8s2-axyqjz2d-l4-shared-hc"},
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
		Region:             "us-central1",
		Scope:              meta.Regional,
		Version:            meta.VersionGA,
		HttpHealthCheck:    &composite.HTTPHealthCheck{Port: 10256, RequestPath: "/healthz"},
		Description:        `{"networking.gke.io/service-name":"","networking.gke.io/api-version":"ga","networking.gke.io/resource-description":"This resource is shared by all L4 XLB Services using ExternalTrafficPolicy: Cluster."}`,
	}
}
