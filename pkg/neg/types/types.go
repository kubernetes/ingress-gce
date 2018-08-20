package types

// PortNameMap is a map of ServicePort:TargetPort.
type PortNameMap map[int32]string

// Union returns the union of the Port:TargetPort mappings
func (p1 PortNameMap) Union(p2 PortNameMap) PortNameMap {
	result := make(PortNameMap)
	for svcPort, targetPort := range p1 {
		result[svcPort] = targetPort
	}

	for svcPort, targetPort := range p2 {
		result[svcPort] = targetPort
	}

	return result
}

// Difference returns the set of Port:TargetPorts in p2 that aren't present in p1
func (p1 PortNameMap) Difference(p2 PortNameMap) PortNameMap {
	result := make(PortNameMap)
	for svcPort, targetPort := range p1 {
		if p1[svcPort] != p2[svcPort] {
			result[svcPort] = targetPort
		}
	}
	return result
}

// NegStatus contains name and zone of the Network Endpoint Group
// resources associated with this service
type NegStatus struct {
	// NetworkEndpointGroups returns the mapping between service port and NEG
	// resource. key is service port, value is the name of the NEG resource.
	NetworkEndpointGroups PortNameMap `json:"network_endpoint_groups,omitempty"`
	Zones                 []string    `json:"zones,omitempty"`
}
