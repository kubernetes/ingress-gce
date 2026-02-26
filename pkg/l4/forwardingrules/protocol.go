package forwardingrules

import api_v1 "k8s.io/api/core/v1"

const (
	// ProtocolTCP is the value used for Protocol when creating TCP Forwarding Rule
	ProtocolTCP = "TCP"
	// ProtocolUDP is the value used for Protocol when creating UDP Forwarding Rule
	ProtocolUDP = "UDP"
	// ProtocolL3 is the value used for Protocol when creating L3 Forwarding Rule.
	// L3 Forwarding rule allows all the traffic to flow through it regardless of ports
	// and protocol. When using ProtocolL3 AllPorts option should be used as well.
	ProtocolL3 = "L3_DEFAULT"
)

// GetILBProtocol returns the protocol to be used for a Forwarding Rule for an ILB.
// For ELB please use forwardingrules.NeedsTCP or forwardingrules.NeedsUDP functions.
func GetILBProtocol(svcPorts []api_v1.ServicePort) string {
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
		return ProtocolL3
	case okUDP:
		return ProtocolUDP
	case okTCP:
		return ProtocolTCP
	default:
		return ProtocolTCP
	}
}
