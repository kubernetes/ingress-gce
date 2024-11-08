package backends

import (
	api_v1 "k8s.io/api/core/v1"
	"k8s.io/ingress-gce/pkg/composite"
)

const (
	L3Protocol = "UNSPECIFIED"
)

// GetProtocol returns the protocol for the BackendService based on Kubernetes Service port definitions
// and existing backend service.
//
// If the service exists and service protocols match existingBS return that protocol.
// Otherwise prefer L3.
//
// See https://cloud.google.com/load-balancing/docs/internal#forwarding-rule-protocols
func GetProtocol(svcPorts []api_v1.ServicePort, existingBS *composite.BackendService) string {
	if doesntExist := existingBS == nil; doesntExist {
		return L3Protocol
	}

	if alreadyL3 := existingBS.Protocol == L3Protocol; alreadyL3 {
		return L3Protocol
	}

	if len(svcPorts) == 0 {
		return existingBS.Protocol
	}

	requiredProtocol := protocolRequiredForService(svcPorts)
	needsChangeCausingTrafficInterruption := existingBS.Protocol != requiredProtocol

	if needsChangeCausingTrafficInterruption {
		return L3Protocol
	}

	// service exists, we don't want to create traffic interruption
	return requiredProtocol
}

func protocolRequiredForService(svcPorts []api_v1.ServicePort) string {
	protocolSet := make(map[api_v1.Protocol]struct{})
	for _, port := range svcPorts {
		protocolSet[port.Protocol] = struct{}{}
	}

	_, okTCP := protocolSet[api_v1.ProtocolTCP]
	_, okUDP := protocolSet[api_v1.ProtocolUDP]

	switch {
	case okTCP && okUDP:
		// L3 Backend service is created with UNSPECIFIED protocol.
		return L3Protocol
	case okUDP:
		return string(api_v1.ProtocolUDP)
	case okTCP:
		return string(api_v1.ProtocolTCP)
	default:
		return L3Protocol
	}
}
