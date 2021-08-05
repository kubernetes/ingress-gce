/*
Copyright 2018 The Kubernetes Authors.

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

package types

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	istioV1alpha3 "istio.io/api/networking/v1alpha3"
	apiv1 "k8s.io/api/core/v1"
	discovery "k8s.io/api/discovery/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/utils/namer"
)

type NetworkEndpointType string
type EndpointsCalculatorMode string

const (
	VmIpPortEndpointType      = NetworkEndpointType("GCE_VM_IP_PORT")
	VmIpEndpointType          = NetworkEndpointType("GCE_VM_IP")
	NonGCPPrivateEndpointType = NetworkEndpointType("NON_GCP_PRIVATE_IP_PORT")
	L7Mode                    = EndpointsCalculatorMode("L7")
	L4LocalMode               = EndpointsCalculatorMode("L4, ExternalTrafficPolicy:Local")
	L4ClusterMode             = EndpointsCalculatorMode("L4, ExternalTrafficPolicy:Cluster")

	// These keys are to be used as label keys for NEG CRs when enabled

	NegCRManagedByKey   = "networking.gke.io/managed-by"
	NegCRServiceNameKey = "networking.gke.io/service-name"
	NegCRServicePortKey = "networking.gke.io/service-port"
	// NegCRControllerValue is used as the value for the managed-by label on NEG CRs when enabled.
	NegCRControllerValue = "neg-controller"

	// NEG CR Condition Reasons
	NegSyncSuccessful           = "NegSyncSuccessful"
	NegSyncFailed               = "NegSyncFailed"
	NegInitializationSuccessful = "NegInitializationSuccessful"
	NegInitializationFailed     = "NegInitializationFailed"

	// NEG CRD Enabled Garbage Collection Event Reasons
	NegGCError = "NegCRError"
)

// SvcPortTuple is the tuple representing one service port
type SvcPortTuple struct {
	// Port is the service port number
	Port int32
	// Name is the service port name
	Name string
	// TargetPort is the service target port.
	// This can be a port number or named port
	TargetPort string
}

func (t SvcPortTuple) Empty() bool {
	return t.Port == 0 && t.Name == "" && t.TargetPort == ""
}

// String returns the string representation of SvcPortTuple
func (t SvcPortTuple) String() string {
	return fmt.Sprintf("%s/%v-%s", t.Name, t.Port, t.TargetPort)
}

// SvcPortTupleSet is a set of SvcPortTuple
type SvcPortTupleSet map[SvcPortTuple]struct{}

// NewSvcPortTupleSet returns SvcPortTupleSet with the input tuples
func NewSvcPortTupleSet(tuples ...SvcPortTuple) SvcPortTupleSet {
	set := SvcPortTupleSet{}
	set.Insert(tuples...)
	return set
}

// Insert inserts a SvcPortTuple into SvcPortTupleSet
func (set SvcPortTupleSet) Insert(tuples ...SvcPortTuple) {
	for _, tuple := range tuples {
		set[tuple] = struct{}{}
	}
}

// Get returns the SvcPortTuple with matching svc port if found
func (set SvcPortTupleSet) Get(svcPort int32) (SvcPortTuple, bool) {
	for tuple := range set {
		if svcPort == tuple.Port {
			return tuple, true
		}
	}
	return SvcPortTuple{}, false
}

// PortInfo contains information associated with service port
type PortInfo struct {
	// PortTuple is port tuple of a service.
	PortTuple SvcPortTuple

	// Subset name, subset is defined in Istio:DestinationRule, this is only used
	// when --enable-csm=true.
	Subset string

	// Subset label, should set together with Subset.
	SubsetLabels string

	// NegName is the name of the NEG
	NegName string
	// ReadinessGate indicates if the NEG associated with the port has NEG readiness gate enabled
	// This is enabled with service port is reference by ingress.
	// If the service port is only exposed as stand alone NEG, it should not be enbled.
	ReadinessGate bool
	// EpCalculatorMode indicates if the endpoints for the NEG associated with this port need to
	// be selected at random(L4ClusterMode), or by following service endpoints(L4LocalMode).
	// This is applicable in GCE_VM_IP NEGs where the endpoints are the nodes instead of pods.
	// L7 NEGs will have either "" or L7Mode.
	EpCalculatorMode EndpointsCalculatorMode
}

// PortInfoMapKey is the Key of PortInfoMap
type PortInfoMapKey struct {
	// ServicePort is the service port
	ServicePort int32

	// Istio:DestinationRule Subset, only used when --enable-csm=true
	Subset string
}

// PortInfoMap is a map of PortInfoMapKey:PortInfo
type PortInfoMap map[PortInfoMapKey]PortInfo

func NewPortInfoMap(namespace, name string, svcPortTupleSet SvcPortTupleSet, namer NetworkEndpointGroupNamer, readinessGate bool, customNegNames map[SvcPortTuple]string) PortInfoMap {
	ret := PortInfoMap{}
	for svcPortTuple := range svcPortTupleSet {
		negName, ok := customNegNames[svcPortTuple]
		if !ok {
			negName = namer.NEG(namespace, name, svcPortTuple.Port)
		}
		ret[PortInfoMapKey{svcPortTuple.Port, ""}] = PortInfo{
			PortTuple:     svcPortTuple,
			NegName:       negName,
			ReadinessGate: readinessGate,
		}
	}
	return ret
}

// NewPortInfoMapForVMIPNEG creates PortInfoMap with empty port tuple. Since VM_IP NEGs target
// the node instead of the pod, there is no port info to be stored.
func NewPortInfoMapForVMIPNEG(namespace, name string, namer namer.L4ResourcesNamer, local bool) PortInfoMap {
	ret := PortInfoMap{}
	svcPortSet := make(SvcPortTupleSet)
	svcPortSet.Insert(
		// Insert Empty PortTuple for VmIp NEGs.
		SvcPortTuple{},
	)
	for svcPortTuple := range svcPortSet {
		mode := L4ClusterMode
		if local {
			mode = L4LocalMode
		}
		negName, _ := namer.VMIPNEG(namespace, name)
		ret[PortInfoMapKey{svcPortTuple.Port, ""}] = PortInfo{
			PortTuple:        svcPortTuple,
			NegName:          negName,
			EpCalculatorMode: mode,
		}
	}
	return ret
}

// NewPortInfoMapWithDestinationRule create PortInfoMap based on a gaven DesinationRule.
// Return error message if the DestinationRule contains duplicated subsets.
func NewPortInfoMapWithDestinationRule(namespace, name string, svcPortTupleSet SvcPortTupleSet, namer NetworkEndpointGroupNamer, readinessGate bool,
	destinationRule *istioV1alpha3.DestinationRule) (PortInfoMap, error) {
	ret := PortInfoMap{}
	var duplicateSubset []string
	for _, subset := range destinationRule.Subsets {
		for tuple := range svcPortTupleSet {
			key := PortInfoMapKey{tuple.Port, subset.Name}
			if _, ok := ret[key]; ok {
				duplicateSubset = append(duplicateSubset, subset.Name)
			}
			ret[key] = PortInfo{
				PortTuple:     tuple,
				NegName:       namer.NEGWithSubset(namespace, name, subset.Name, tuple.Port),
				ReadinessGate: readinessGate,
				Subset:        subset.Name,
				SubsetLabels:  labels.Set(subset.Labels).String(),
			}
		}
	}
	if len(duplicateSubset) != 0 {
		return ret, fmt.Errorf("Duplicated subsets: %s", strings.Join(duplicateSubset, ", "))
	}
	return ret, nil
}

// Merge merges p2 into p1 PortInfoMap
// It assumes the same key (service port) will have the same target port and negName
// If not, it will throw error
// If a key in p1 or p2 has readiness gate enabled, the merged port info will also has readiness gate enabled
// The merged port info will have the same Endpoints Calculator mode as p1 and p2. This field is important for VM_IP NEGs.
// L7 NEGs can have an empty string or L7Mode value.
func (p1 PortInfoMap) Merge(p2 PortInfoMap) error {
	var err error
	for mapKey, portInfo := range p2 {
		mergedInfo := PortInfo{}
		if existingPortInfo, ok := p1[mapKey]; ok {
			if existingPortInfo.PortTuple != portInfo.PortTuple {
				return fmt.Errorf("for service port %v, port tuple in existing map is %q, but the merge map has %q", mapKey, existingPortInfo.PortTuple, portInfo.PortTuple)
			}
			if existingPortInfo.NegName != portInfo.NegName {
				return fmt.Errorf("for service port %v, NEG name in existing map is %q, but the merge map has %q", mapKey, existingPortInfo.NegName, portInfo.NegName)
			}
			if existingPortInfo.Subset != portInfo.Subset {
				return fmt.Errorf("for service port %v, Subset name in existing map is %q, but the merge map has %q", mapKey, existingPortInfo.Subset, portInfo.Subset)
			}
			if existingPortInfo.EpCalculatorMode != portInfo.EpCalculatorMode {
				return fmt.Errorf("For service port %v, Existing map has Calculator mode %v, but the merge map has %v", mapKey, existingPortInfo.EpCalculatorMode, portInfo.EpCalculatorMode)
			}
			mergedInfo.ReadinessGate = existingPortInfo.ReadinessGate
		}
		mergedInfo.PortTuple = portInfo.PortTuple
		mergedInfo.NegName = portInfo.NegName
		// Turn on the readiness gate if one of them is on
		mergedInfo.ReadinessGate = mergedInfo.ReadinessGate || portInfo.ReadinessGate
		mergedInfo.EpCalculatorMode = portInfo.EpCalculatorMode
		mergedInfo.Subset = portInfo.Subset
		mergedInfo.SubsetLabels = portInfo.SubsetLabels

		p1[mapKey] = mergedInfo
	}
	return err
}

// Difference returns the entries of PortInfoMap which satisfies one of the following 2 conditions:
// 1. portInfo entry is in p1 and not present in p2
// 2. or the portInfo entry is not the same in p1 and p2.
func (p1 PortInfoMap) Difference(p2 PortInfoMap) PortInfoMap {
	result := make(PortInfoMap)
	for mapKey, p1PortInfo := range p1 {
		p2PortInfo, ok := p2[mapKey]
		if ok && reflect.DeepEqual(p1[mapKey], p2PortInfo) {
			continue
		}
		result[mapKey] = p1PortInfo
	}
	return result
}

func (p1 PortInfoMap) ToPortNegMap() annotations.PortNegMap {
	ret := annotations.PortNegMap{}
	for mapKey, portInfo := range p1 {
		ret[strconv.Itoa(int(mapKey.ServicePort))] = portInfo.NegName
	}
	return ret
}

func (p1 PortInfoMap) ToPortSubsetNegMap() annotations.PortSubsetNegMap {
	ret := annotations.PortSubsetNegMap{}
	for mapKey, portInfo := range p1 {
		if m, ok := ret[mapKey.Subset]; ok {
			m[strconv.Itoa(int(mapKey.ServicePort))] = portInfo.NegName
		} else {
			ret[mapKey.Subset] = map[string]string{strconv.Itoa(int(mapKey.ServicePort)): portInfo.NegName}
		}
	}
	return ret
}

// NegsWithReadinessGate returns the NegNames which has readiness gate enabled
func (p1 PortInfoMap) NegsWithReadinessGate() sets.String {
	ret := sets.NewString()
	for _, info := range p1 {
		if info.ReadinessGate {
			ret.Insert(info.NegName)
		}
	}
	return ret
}

// NegSyncerKey includes information to uniquely identify a NEG syncer
type NegSyncerKey struct {
	// Namespace of service
	Namespace string
	// Name of service
	Name string
	// Name of neg
	NegName string
	// PortTuple is the port tuple of the service backing the NEG
	PortTuple SvcPortTuple

	// Subset name, subset is defined in Istio:DestinationRule, this is only used
	// when --enable-csm=true.
	Subset string

	// Subset label, should set together with Subset.
	SubsetLabels string

	// NegType is the type of the network endpoints in this NEG.
	NegType NetworkEndpointType

	// EpCalculatorMode indicates how the endpoints for the NEG are determined.
	// GCE_VM_IP_PORT NEGs get L7Mode or "".
	// In case of GCE_VM_IP NEGs:
	//   The endpoints are nodes selected at random in case of Cluster trafficPolicy(L4ClusterMode).
	//   The endpoints are nodes running backends of this service in case of Local trafficPolicy(L4LocalMode).
	EpCalculatorMode EndpointsCalculatorMode
}

func (key NegSyncerKey) String() string {
	return fmt.Sprintf("%s/%s-%s-%s-%s-%s-%s", key.Namespace, key.Name, key.NegName, key.Subset, key.PortTuple.String(), string(key.NegType), key.EpCalculatorMode)
}

// GetAPIVersion returns the compute API version to be used in order
// to create the negType specified in the given NegSyncerKey.
func (key NegSyncerKey) GetAPIVersion() meta.Version {
	switch key.NegType {
	case NonGCPPrivateEndpointType:
		return meta.VersionAlpha
	default:
		return meta.VersionGA
	}
}

// EndpointPodMap is a map from network endpoint to a namespaced name of a pod
type EndpointPodMap map[NetworkEndpoint]types.NamespacedName

// Abstraction over Endpoints and EndpointSlices.
// It contains all the information needed to set up
// GCP Load Balancer.
type EndpointsData struct {
	Meta      *metav1.ObjectMeta
	Ports     []PortData
	Addresses []AddressData
}

type PortData struct {
	Name string
	Port int32
}

type AddressData struct {
	TargetRef *apiv1.ObjectReference
	NodeName  *string
	Addresses []string
	Ready     bool
}

// Converts API Endpoints to the EndpointsData abstraction.
// All endpoints are converted, not ready addresses are converted with Reade=false.
func EndpointsDataFromEndpoints(ep *apiv1.Endpoints) []EndpointsData {
	result := make([]EndpointsData, 0, len(ep.Subsets))
	for _, subset := range ep.Subsets {
		ports := make([]PortData, 0)
		addresses := make([]AddressData, 0)
		for _, port := range subset.Ports {
			ports = append(ports, PortData{Name: port.Name, Port: port.Port})
		}
		for _, addr := range subset.Addresses {
			addresses = append(addresses, AddressData{TargetRef: addr.TargetRef, NodeName: addr.NodeName, Addresses: []string{addr.IP}, Ready: true})
		}
		for _, addr := range subset.NotReadyAddresses {
			addresses = append(addresses, AddressData{TargetRef: addr.TargetRef, NodeName: addr.NodeName, Addresses: []string{addr.IP}, Ready: false})
		}
		result = append(result, EndpointsData{Meta: &ep.ObjectMeta, Ports: ports, Addresses: addresses})
	}
	return result
}

// Converts API EndpointSlice list to the EndpointsData abstraction.
// Terminating endpoints are ignored.
//
func EndpointsDataFromEndpointSlices(slices []*discovery.EndpointSlice) []EndpointsData {
	result := make([]EndpointsData, 0, len(slices))
	for _, slice := range slices {
		ports := make([]PortData, 0)
		addresses := make([]AddressData, 0)
		for _, port := range slice.Ports {
			ports = append(ports, PortData{Name: *port.Name, Port: *port.Port})
		}
		for _, ep := range slice.Endpoints {
			// Ignore terminating endpoints. Nil means that endpoint is not terminating.
			if ep.Conditions.Terminating != nil && *ep.Conditions.Terminating {
				continue
			}
			// Endpoint is ready when the Ready is nil or when it's value is true.
			ready := ep.Conditions.Ready == nil || *ep.Conditions.Ready
			addresses = append(addresses, AddressData{TargetRef: ep.TargetRef, NodeName: ep.NodeName, Addresses: ep.Addresses, Ready: ready})
		}
		result = append(result, EndpointsData{Meta: &slice.ObjectMeta, Ports: ports, Addresses: addresses})
	}
	return result
}
