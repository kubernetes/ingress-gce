package forwardingrules

import (
	"fmt"

	api_v1 "k8s.io/api/core/v1"
)

const (
	// Protocol field is optional.
	// When it is empty the default is TCP
	defaultProtocol = api_v1.ProtocolTCP
)

// GetPorts returns slice of ports for the specified protocol.
func GetPorts(svcPorts []api_v1.ServicePort, protocol api_v1.Protocol) []string {
	ports := []string{}

	for _, p := range svcPorts {
		proto := p.Protocol
		if proto == "" {
			proto = defaultProtocol
		}

		if proto == protocol {
			ports = append(ports, fmt.Sprint(p.Port))
		}
	}

	return ports
}
