package firewalls

import (
	compute "google.golang.org/api/compute/v1"
	api_v1 "k8s.io/api/core/v1"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/utils/ptr"
)

var (
	// AllowTrafficPriority is the priority for node traffic firewall rules, when --enable-l4-deny-firewall is enabled
	AllowTrafficPriority = ptr.To(999)
	// DenyTrafficPriority is the priority for node traffic deny firewall rules, when --enable-l4-deny-firewall is enabled
	DenyTrafficPriority = ptr.To(1000)
)

// AllowedForService creates a slice of *compute.FirewallAllowed rules
// for given Kubernetes Service port definitions.
func AllowedForService(svcPorts []api_v1.ServicePort) []*compute.FirewallAllowed {
	var allowed []*compute.FirewallAllowed
	if udpPorts := getPorts(svcPorts, api_v1.ProtocolUDP); len(udpPorts) > 0 {
		allowed = append(allowed, &compute.FirewallAllowed{
			IPProtocol: "udp",
			Ports:      udpPorts,
		})
	}
	if tcpPorts := getPorts(svcPorts, api_v1.ProtocolTCP); len(tcpPorts) > 0 {
		allowed = append(allowed, &compute.FirewallAllowed{
			IPProtocol: "tcp",
			Ports:      tcpPorts,
		})
	}
	return allowed
}

// DeniedAll creates a []*compute.FirewallDenied rules
// to block all traffic an the Firewall Rule
func DeniedAll() []*compute.FirewallDenied {
	return []*compute.FirewallDenied{{IPProtocol: "all"}}
}

func getPorts(svcPorts []api_v1.ServicePort, protocol api_v1.Protocol) []string {
	var ports []int
	for _, p := range svcPorts {
		proto := p.Protocol
		if proto == "" {
			proto = api_v1.ProtocolTCP // default
		}

		if proto == protocol {
			ports = append(ports, int(p.Port))
		}
	}
	// Compress ports that are consecutive into port ranges.
	return utils.GetPortRanges(ports)
}
