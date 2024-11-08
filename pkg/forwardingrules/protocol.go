package forwardingrules

import api_v1 "k8s.io/api/core/v1"

func NeedsTCP(svcPorts []api_v1.ServicePort) bool {
	return needs(api_v1.ProtocolTCP, svcPorts)
}

func NeedsUDP(svcPorts []api_v1.ServicePort) bool {
	return needs(api_v1.ProtocolUDP, svcPorts)
}

func needs(protocol api_v1.Protocol, svcPorts []api_v1.ServicePort) bool {
	for _, port := range svcPorts {
		if port.Protocol == protocol {
			return true
		}
	}
	return false
}
