/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package neg

import (
	"fmt"

	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/neg/types"
)

const (
	batchSyncer       = NegSyncerType("batch")
	transactionSyncer = NegSyncerType("transaction")
)

// NegSyncerType represents the the neg syncer type
type NegSyncerType string

// NEGServicePorts returns the parsed ServicePorts from the annotation.
// knownPorts represents the known Port:TargetPort attributes of servicePorts
// that already exist on the service. This function returns an error if
// any of the parsed ServicePorts from the annotation is not in knownPorts.
func NEGServicePorts(ann *annotations.NegAnnotation, knownPorts types.PortNameMap) (types.PortNameMap, error) {
	portSet := make(types.PortNameMap)
	var errList []error
	for port := range ann.ExposedPorts {
		// TODO: also validate ServicePorts in the exposed NEG annotation via webhook
		if _, ok := knownPorts[port]; !ok {
			errList = append(errList, fmt.Errorf("port %v specified in %q doesn't exist in the service", port, annotations.NEGAnnotationKey))
		}
		portSet[port] = knownPorts[port]
	}

	return portSet, utilerrors.NewAggregate(errList)
}

// GetNegStatus generates a NegStatus denoting the current NEGs
// associated with the given ports.
// NetworkEndpointGroups is a mapping between ServicePort and NEG name
// Zones is a list of zones where the NEGs exist.
func GetNegStatus(zones []string, portToNegs types.PortNameMap) types.NegStatus {
	res := types.NegStatus{}
	res.NetworkEndpointGroups = make(types.PortNameMap)
	res.Zones = zones
	res.NetworkEndpointGroups = portToNegs
	return res
}
