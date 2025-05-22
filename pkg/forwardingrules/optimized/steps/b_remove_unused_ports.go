package steps

import (
	"fmt"
	"strconv"

	api_v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/ingress-gce/pkg/composite"
)

// RemoveUnusedDiscretePorts removes discrete ports from Forwarding Rules, that are not present in wantPorts.
// Objects from frs map are mutated.
func RemoveUnusedDiscretePorts(wantPorts []api_v1.ServicePort, frs map[ResourceName]*composite.ForwardingRule) error {
	wanted := wantedPortsMap(wantPorts)

	for _, fr := range frs {
		// Removing items from list has O(n) time complexity, and adding has O(1)
		// It should be faster to create a new list of ports skipping not used.
		updatedPorts := make([]string, 0, len(fr.Ports))

		for _, portStr := range fr.Ports {
			port, err := parsePort(portStr)
			if err != nil {
				return err
			}

			if wanted[api_v1.ProtocolTCP].Has(port) {
				updatedPorts = append(updatedPorts, portStr)
			}
		}

		fr.Ports = updatedPorts
	}

	return nil
}

func wantedPortsMap(ports []api_v1.ServicePort) map[api_v1.Protocol]sets.Set[int32] {
	portsInProtocol := make(map[api_v1.Protocol]sets.Set[int32])

	for _, p := range ports {
		protocol := api_v1.ProtocolTCP // default value
		if p.Protocol != "" {
			protocol = p.Protocol
		}

		if _, ok := portsInProtocol[protocol]; !ok {
			portsInProtocol[protocol] = sets.New[int32]()
		}

		portsInProtocol[protocol].Insert(p.Port)
	}

	return portsInProtocol
}

// parsePort parses port from string to int32.
// GCE Forwarding Rule API uses string for each port,
// while K8s Services use int32.
func parsePort(port string) (int32, error) {
	const decimal, bitSize = 10, 32
	v, err := strconv.ParseInt(port, decimal, bitSize)
	if err != nil {
		return 0, fmt.Errorf("failed to parse port %q: %w", port, err)
	}
	return int32(v), nil
}
