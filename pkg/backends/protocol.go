package backends

import (
	api_v1 "k8s.io/api/core/v1"
)

const (
	// ProtocolL3 is the value used for Protocol when creating L3 Backend Service
	ProtocolL3 = "UNSPECIFIED"
	// ProtocolTCP is the value used for Protocol when creating TCP Backend Service
	ProtocolTCP = "TCP"
	// ProtocolUDP is the value used for Protocol when creating UDP Backend Service
	ProtocolUDP = "UDP"
)

// GetProtocol returns the protocol for the BackendService based on Kubernetes Service port definitions.
//
// See https://cloud.google.com/load-balancing/docs/internal#forwarding-rule-protocols
func GetProtocol(svcPorts []api_v1.ServicePort) string {
	protocolSet := make(map[api_v1.Protocol]struct{})
	for _, port := range svcPorts {
		protocol := port.Protocol
		if protocol == "" {
			// Protocol is optional, defaults to TCP
			protocol = api_v1.ProtocolTCP
		}
		protocolSet[protocol] = struct{}{}
	}

	_, okTCP := protocolSet[api_v1.ProtocolTCP]
	_, okUDP := protocolSet[api_v1.ProtocolUDP]

	switch {
	case okTCP && okUDP:
		// L3 Backend service is created with UNSPECIFIED protocol.
		return ProtocolL3
	case okUDP:
		return ProtocolUDP
	case okTCP:
		return ProtocolTCP
	default:
		return ProtocolTCP
	}
}
