package mixedprotocoltest

import (
	api_v1 "k8s.io/api/core/v1"
)

// SpecIPv4 returns a specification for a IPv6 LB with specified tcp and udp ports
func SpecIPv4(tcpPorts []int32, udpPorts []int32) api_v1.ServiceSpec {
	return api_v1.ServiceSpec{
		Type:            api_v1.ServiceTypeLoadBalancer,
		Ports:           getPorts(tcpPorts, udpPorts),
		SessionAffinity: api_v1.ServiceAffinityNone,
	}
}

// SpecIPv6 returns a specification for a IPv6 LB with specified tcp and udp ports
func SpecIPv6(tcpPorts []int32, udpPorts []int32) api_v1.ServiceSpec {
	policy := api_v1.IPFamilyPolicySingleStack

	return api_v1.ServiceSpec{
		IPFamilies:      []api_v1.IPFamily{"IPv6"},
		IPFamilyPolicy:  &policy,
		Type:            api_v1.ServiceTypeLoadBalancer,
		Ports:           getPorts(tcpPorts, udpPorts),
		SessionAffinity: api_v1.ServiceAffinityNone,
	}
}

// SpecDualStack returns a specification for a Dual Stack LB with specified tcp and udp ports
func SpecDualStack(tcpPorts []int32, udpPorts []int32) api_v1.ServiceSpec {
	policy := api_v1.IPFamilyPolicyRequireDualStack

	return api_v1.ServiceSpec{
		IPFamilies:      []api_v1.IPFamily{"IPv4", "IPv6"},
		IPFamilyPolicy:  &policy,
		Type:            api_v1.ServiceTypeLoadBalancer,
		Ports:           getPorts(tcpPorts, udpPorts),
		SessionAffinity: api_v1.ServiceAffinityNone,
	}
}

func getPorts(tcpPorts []int32, udpPorts []int32) []api_v1.ServicePort {
	var ports []api_v1.ServicePort
	for _, port := range tcpPorts {
		ports = append(ports, api_v1.ServicePort{
			Name:     "tcp" + string(port),
			Port:     port,
			Protocol: api_v1.ProtocolTCP,
		})
	}
	for _, port := range udpPorts {
		ports = append(ports, api_v1.ServicePort{
			Name:     "udp" + string(port),
			Port:     port,
			Protocol: api_v1.ProtocolUDP,
		})
	}
	return ports
}
