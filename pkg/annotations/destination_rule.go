package annotations

import "encoding/json"

// PortSubsetNegMap is the mapping between subset to NEG name.
// Structure:
// {
// 	"subsetv1": {
// 				 "9080": "somehash-default-reviews-v1-9080",
// 	  }
// 	 "v2": {
// 				"9080": "somehash-default-reviews-v2-9080",
// 	   }
// }
type PortSubsetNegMap map[string]map[string]string

type DestinationRuleNEGStatus struct {
	NetworkEndpointGroups PortSubsetNegMap `json:"network_endpoint_groups,omitempty"`
	// Zones is a list of zones where the NEGs exist.
	Zones []string `json:"zones,omitempty"`
}

// NewNegStatus generates a NegStatus denoting the current NEGs
// associated with the given ports.
func NewDestinationRuleNegStatus(zones []string, portSubsetToNegs PortSubsetNegMap) DestinationRuleNEGStatus {
	res := DestinationRuleNEGStatus{}
	res.Zones = zones
	res.NetworkEndpointGroups = portSubsetToNegs
	return res
}

func (ns DestinationRuleNEGStatus) Marshal() (string, error) {
	ret := ""
	bytes, err := json.Marshal(ns)
	if err != nil {
		return ret, err
	}
	return string(bytes), err
}
