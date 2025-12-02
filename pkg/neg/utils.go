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

	nodetopologyv1 "github.com/GoogleCloudPlatform/gke-networking-api/apis/nodetopology/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/ingress-gce/pkg/negannotation"
)

// NegSyncerType represents the neg syncer type
type NegSyncerType string

// negServicePorts returns the SvcPortTupleSet that matches the exposed service port in the NEG annotation.
// knownSvcTupleSet represents the known service port tuples that already exist on the service.
// This function returns an error if any of the service port from the annotation is not in knownSvcTupleSet.
func negServicePorts(ann *negannotation.NegAnnotation, knownSvcTupleSet types.SvcPortTupleSet) (types.SvcPortTupleSet, map[types.SvcPortTuple]string, error) {
	svcPortTupleSet := make(types.SvcPortTupleSet)
	customNameMap := make(map[types.SvcPortTuple]string)
	var errList []error
	for port, attr := range ann.ExposedPorts {
		// TODO: also validate ServicePorts in the exposed NEG annotation via webhook
		tuple, ok := knownSvcTupleSet.Get(port)
		if !ok {
			errList = append(errList, fmt.Errorf("port %v specified in %q doesn't exist in the service", port, negannotation.NEGAnnotationKey))
		} else {
			if attr.Name != "" {
				customNameMap[tuple] = attr.Name
			}
			svcPortTupleSet.Insert(tuple)
		}
	}

	return svcPortTupleSet, customNameMap, utilerrors.NewAggregate(errList)
}

func isZoneChanged(oldZones, newZones []string) bool {
	return !sets.NewString(oldZones...).Equal(sets.NewString(newZones...))
}

func isSubnetChanged(oldSubnets, newSubnets []nodetopologyv1.SubnetConfig) bool {
	oldSubnetSet := sets.NewString()
	newSubnetSet := sets.NewString()
	for _, oldSubnet := range oldSubnets {
		oldSubnetSet.Insert(oldSubnet.SubnetPath)
	}
	for _, newSubnet := range newSubnets {
		newSubnetSet.Insert(newSubnet.SubnetPath)
	}

	return !oldSubnetSet.Equal(newSubnetSet)
}
