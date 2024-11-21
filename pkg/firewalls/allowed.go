package firewalls

import (
	compute "google.golang.org/api/compute/v1"
	api_v1 "k8s.io/api/core/v1"
	"k8s.io/ingress-gce/pkg/forwardingrules"
)

// AllowedForService creates a slice of *compute.FirewallAllowed rules
// for given Kubernetes Serivice port definitions.
func AllowedForService(svcPorts []api_v1.ServicePort) []*compute.FirewallAllowed {
	var allowed []*compute.FirewallAllowed
	if udpPorts := forwardingrules.GetPorts(svcPorts, api_v1.ProtocolUDP); len(udpPorts) > 0 {
		allowed = append(allowed, &compute.FirewallAllowed{
			IPProtocol: "udp",
			Ports:      udpPorts,
		})
	}
	if tcpPorts := forwardingrules.GetPorts(svcPorts, api_v1.ProtocolTCP); len(tcpPorts) > 0 {
		allowed = append(allowed, &compute.FirewallAllowed{
			IPProtocol: "tcp",
			Ports:      tcpPorts,
		})
	}
	return allowed
}
