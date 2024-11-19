package forwardingrules

import api_v1 "k8s.io/api/core/v1"

// NeedsTCP returns true if the controller should have
// a TCP Forwarding rule for given set of service ports.
// Otherwise returns false.
func NeedsTCP(svcPorts []api_v1.ServicePort) bool {
	return needs(api_v1.ProtocolTCP, svcPorts)
}

// NeedsUDP returns true if the controller should have
// a UDP Forwarding rule for given set of service ports.
// Otherwise returns false.
func NeedsUDP(svcPorts []api_v1.ServicePort) bool {
	return needs(api_v1.ProtocolUDP, svcPorts)
}

func needs(protocol api_v1.Protocol, svcPorts []api_v1.ServicePort) bool {
	for _, port := range svcPorts {
		p := port.Protocol
		if p == "" {
			p = defaultProtocol
		}

		if p == protocol {
			return true
		}
	}
	return false
}
