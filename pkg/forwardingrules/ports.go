package forwardingrules

import (
	"fmt"

	api_v1 "k8s.io/api/core/v1"
)

func GetPorts(svcPorts []api_v1.ServicePort, protocol api_v1.Protocol) []string {
	ports := []string{}
	for _, p := range svcPorts {
		if p.Protocol == protocol {
			ports = append(ports, fmt.Sprint(p.Port))
		}
	}
	return ports
}
