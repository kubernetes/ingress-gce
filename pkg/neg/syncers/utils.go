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

package syncers

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	apiv1 "k8s.io/api/core/v1"
	discovery "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	negv1beta1 "k8s.io/ingress-gce/pkg/apis/svcneg/v1beta1"
	"k8s.io/ingress-gce/pkg/composite"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog/v2"
)

const (
	MAX_NETWORK_ENDPOINTS_PER_BATCH = 500
	// For each NEG, only retries 15 times to process it.
	// This is a convention in kube-controller-manager.
	maxRetries    = 15
	minRetryDelay = 5 * time.Second
	maxRetryDelay = 600 * time.Second
	separator     = "||"
)

// encodeEndpoint encodes ip and instance into a single string
func encodeEndpoint(ip, instance, port string) string {
	return strings.Join([]string{ip, instance, port}, separator)
}

// decodeEndpoint decodes ip and instance from an encoded string
func decodeEndpoint(str string) (string, string, string) {
	strs := strings.Split(str, separator)
	return strs[0], strs[1], strs[2]
}

// calculateDifference determines what endpoints needs to be added and removed in order to move current state to target state.
func calculateDifference(targetMap, currentMap map[string]sets.String) (map[string]sets.String, map[string]sets.String) {
	addSet := map[string]sets.String{}
	removeSet := map[string]sets.String{}
	for zone, endpointSet := range targetMap {
		diff := endpointSet.Difference(currentMap[zone])
		if len(diff) > 0 {
			addSet[zone] = diff
		}
	}

	for zone, endpointSet := range currentMap {
		diff := endpointSet.Difference(targetMap[zone])
		if len(diff) > 0 {
			removeSet[zone] = diff
		}
	}
	return addSet, removeSet
}

// calculateNetworkEndpointDifference determines what endpoints needs to be added and removed in order to move current state to target state.
func calculateNetworkEndpointDifference(targetMap, currentMap map[string]negtypes.NetworkEndpointSet) (map[string]negtypes.NetworkEndpointSet, map[string]negtypes.NetworkEndpointSet) {
	addSet := map[string]negtypes.NetworkEndpointSet{}
	removeSet := map[string]negtypes.NetworkEndpointSet{}
	for zone, endpointSet := range targetMap {
		diff := endpointSet.Difference(currentMap[zone])
		if len(diff) > 0 {
			addSet[zone] = diff
		}
	}

	for zone, endpointSet := range currentMap {
		diff := endpointSet.Difference(targetMap[zone])
		if len(diff) > 0 {
			removeSet[zone] = diff
		}
	}
	return addSet, removeSet
}

// getService retrieves service object from serviceLister based on the input Namespace and Name
func getService(serviceLister cache.Indexer, namespace, name string) *apiv1.Service {
	if serviceLister == nil {
		return nil
	}
	service, exists, err := serviceLister.GetByKey(utils.ServiceKeyFunc(namespace, name))
	if exists && err == nil {
		return service.(*apiv1.Service)
	}
	if err != nil {
		klog.Errorf("Failed to retrieve service %s/%s from store: %v", namespace, name, err)
	}
	return nil
}

// ensureNetworkEndpointGroup ensures corresponding NEG is configured correctly in the specified zone.
func ensureNetworkEndpointGroup(svcNamespace, svcName, negName, zone, negServicePortName, kubeSystemUID, port string, networkEndpointType negtypes.NetworkEndpointType, cloud negtypes.NetworkEndpointGroupCloud, serviceLister cache.Indexer, recorder record.EventRecorder, version meta.Version, customName bool) (negv1beta1.NegObjectReference, error) {
	var negRef negv1beta1.NegObjectReference
	neg, err := cloud.GetNetworkEndpointGroup(negName, zone, version)
	if err != nil {
		if !utils.IsNotFoundError(err) {
			klog.Errorf("Failed to get Neg %q in zone %q: %s", negName, zone, err)
			return negRef, err
		}
		klog.V(4).Infof("Neg %q in zone %q was not found: %s", negName, zone, err)
	}

	needToCreate := false
	if neg == nil {
		needToCreate = true
	} else {
		expectedDesc := utils.NegDescription{
			ClusterUID:  kubeSystemUID,
			Namespace:   svcNamespace,
			ServiceName: svcName,
			Port:        port,
		}
		if customName && neg.Description == "" {
			klog.Errorf("Found Neg with custom name %s but empty description", negName)
			return negv1beta1.NegObjectReference{}, fmt.Errorf("neg name %s is already in use, found a custom named neg with an empty description", negName)
		}
		if matches, err := utils.VerifyDescription(expectedDesc, neg.Description, negName, zone); !matches {
			klog.Errorf("Neg Name %s is already in use: %s", negName, err)
			return negv1beta1.NegObjectReference{}, fmt.Errorf("neg name %s is already in use, found conflicting description: %w", negName, err)
		}

		if networkEndpointType != negtypes.NonGCPPrivateEndpointType &&
			// Only perform the following checks when the NEGs are not Non-GCP NEGs.
			// Non-GCP NEGs do not have associated network and subnetwork.
			(!utils.EqualResourceIDs(neg.Network, cloud.NetworkURL()) ||
				!utils.EqualResourceIDs(neg.Subnetwork, cloud.SubnetworkURL())) {

			needToCreate = true
			klog.V(2).Infof("NEG %q in %q does not match network and subnetwork of the cluster. Deleting NEG.", negName, zone)
			err = cloud.DeleteNetworkEndpointGroup(negName, zone, version)
			if err != nil {
				return negRef, err
			}
			if recorder != nil && serviceLister != nil {
				if svc := getService(serviceLister, svcNamespace, svcName); svc != nil {
					recorder.Eventf(svc, apiv1.EventTypeNormal, "Delete", "Deleted NEG %q for %s in %q.", negName, negServicePortName, zone)
				}
			}
		}
	}

	if needToCreate {
		klog.V(2).Infof("Creating NEG %q for %s in %q.", negName, negServicePortName, zone)
		var subnetwork string
		switch networkEndpointType {
		case negtypes.NonGCPPrivateEndpointType:
			subnetwork = ""
		default:
			subnetwork = cloud.SubnetworkURL()
		}

		desc := ""
		negDesc := utils.NegDescription{
			ClusterUID:  kubeSystemUID,
			Namespace:   svcNamespace,
			ServiceName: svcName,
			Port:        port,
		}
		desc = negDesc.String()

		err = cloud.CreateNetworkEndpointGroup(&composite.NetworkEndpointGroup{
			Version:             version,
			Name:                negName,
			NetworkEndpointType: string(networkEndpointType),
			Network:             cloud.NetworkURL(),
			Subnetwork:          subnetwork,
			Description:         desc,
		}, zone)
		if err != nil {
			return negRef, err
		}
		if recorder != nil && serviceLister != nil {
			if svc := getService(serviceLister, svcNamespace, svcName); svc != nil {
				recorder.Eventf(svc, apiv1.EventTypeNormal, "Create", "Created NEG %q for %s in %q.", negName, negServicePortName, zone)
			}
		}
	}

	if neg == nil {
		var err error
		neg, err = cloud.GetNetworkEndpointGroup(negName, zone, version)
		if err != nil {
			klog.Errorf("Error while retrieving %q in zone %q: %v after initialization", negName, zone, err)
			return negRef, err
		}
	}

	negRef = negv1beta1.NegObjectReference{
		Id:                  fmt.Sprint(neg.Id),
		SelfLink:            neg.SelfLink,
		NetworkEndpointType: negv1beta1.NetworkEndpointType(neg.NetworkEndpointType),
	}
	return negRef, nil
}

// toZoneNetworkEndpointMap translates addresses in endpoints object into zone and endpoints map, and also return the count for duplicated endpoints
func toZoneNetworkEndpointMap(eds []negtypes.EndpointsData, zoneGetter negtypes.ZoneGetter, servicePortName string, networkEndpointType negtypes.NetworkEndpointType, lpConfig negtypes.PodLabelPropagationConfig) (map[string]negtypes.NetworkEndpointSet, negtypes.EndpointPodMap, int, error) {
	zoneNetworkEndpointMap := map[string]negtypes.NetworkEndpointSet{}
	networkEndpointPodMap := negtypes.EndpointPodMap{}
	dupCount := 0
	if eds == nil {
		klog.Errorf("Endpoint object is nil")
		return zoneNetworkEndpointMap, networkEndpointPodMap, dupCount, nil
	}
	var foundMatchingPort bool
	for _, ed := range eds {
		matchPort := ""
		// service spec allows target Port to be a named Port.
		// support both explicit Port and named Port.
		for _, port := range ed.Ports {
			if port.Name == servicePortName {
				matchPort = strconv.Itoa(int(port.Port))
				break
			}
		}

		// subset does not contain target Port
		if len(matchPort) == 0 {
			continue
		}
		foundMatchingPort = true

		for _, endpointAddress := range ed.Addresses {
			if endpointAddress.AddressType != discovery.AddressTypeIPv4 {
				klog.Infof("Skipping non IPv4 address: %q, in endpoint slice %s/%s", endpointAddress.Addresses, ed.Meta.Namespace, ed.Meta.Name)
				continue
			}
			if endpointAddress.NodeName == nil || len(*endpointAddress.NodeName) == 0 {
				klog.V(2).Infof("Detected unexpected error when checking missing nodeName. Endpoint %q in Endpoints %s/%s does not have an associated node. Skipping", endpointAddress.Addresses, ed.Meta.Namespace, ed.Meta.Name)
				return nil, nil, dupCount, negtypes.ErrEPMissingNodeName
			}
			if endpointAddress.TargetRef == nil {
				klog.V(2).Infof("Endpoint %q in Endpoints %s/%s does not have an associated pod. Skipping", endpointAddress.Addresses, ed.Meta.Namespace, ed.Meta.Name)
				continue
			}
			zone, err := zoneGetter.GetZoneForNode(*endpointAddress.NodeName)
			if err != nil {
				return nil, nil, dupCount, negtypes.ErrNodeNotFound
			}
			if zone == "" {
				klog.V(2).Info("Detected unexpected error when checking missing zone")
				return nil, nil, dupCount, negtypes.ErrEPMissingZone
			}
			if zoneNetworkEndpointMap[zone] == nil {
				zoneNetworkEndpointMap[zone] = negtypes.NewNetworkEndpointSet()
			}

			for _, address := range endpointAddress.Addresses {
				networkEndpoint := negtypes.NetworkEndpoint{IP: address, Port: matchPort, Node: *endpointAddress.NodeName}
				if networkEndpointType == negtypes.NonGCPPrivateEndpointType {
					// Non-GCP network endpoints don't have associated nodes.
					networkEndpoint.Node = ""
				}
				zoneNetworkEndpointMap[zone].Insert(networkEndpoint)

				// increment the count for duplicate endpoint
				if existingPod, contains := networkEndpointPodMap[networkEndpoint]; contains {
					dupCount += 1
					if existingPod.Name < endpointAddress.TargetRef.Name {
						continue // if existing name is alphabetically lower than current one, continue and don't replace
					}
				}
				networkEndpointPodMap[networkEndpoint] = types.NamespacedName{Namespace: endpointAddress.TargetRef.Namespace, Name: endpointAddress.TargetRef.Name}
			}
		}
	}
	if !foundMatchingPort {
		klog.Errorf("Service port name %q was not found in the endpoints object %+v", servicePortName, eds)
	}

	if len(zoneNetworkEndpointMap) == 0 || len(networkEndpointPodMap) == 0 {
		klog.V(3).Infof("Generated empty endpoint maps (zoneNetworkEndpointMap: %+v, networkEndpointPodMap: %v) from Endpoints object: %+v", zoneNetworkEndpointMap, networkEndpointPodMap, eds)
	}
	return zoneNetworkEndpointMap, networkEndpointPodMap, dupCount, nil
}

func toZoneNetworkEndpointMapDegradedMode(eds []negtypes.EndpointsData, zoneGetter negtypes.ZoneGetter,
	podLister, nodeLister cache.Indexer, servicePortName string,
	networkEndpointType negtypes.NetworkEndpointType) (map[string]negtypes.NetworkEndpointSet, negtypes.EndpointPodMap, int, error) {
	targetMap := map[string]negtypes.NetworkEndpointSet{}
	endpointPodMap := negtypes.EndpointPodMap{}
	var dupCount int
	for _, ed := range eds {
		matchPort := ""
		for _, port := range ed.Ports {
			if port.Name == servicePortName {
				matchPort = strconv.Itoa(int(port.Port))
				break
			}
		}
		if len(matchPort) == 0 {
			continue
		}
		for _, endpointAddress := range ed.Addresses {
			if endpointAddress.AddressType != discovery.AddressTypeIPv4 {
				klog.Infof("Skipping non IPv4 address in degraded mode: %q, in endpoint slice %s/%s", endpointAddress.Addresses, ed.Meta.Namespace, ed.Meta.Name)
				continue
			}
			if endpointAddress.TargetRef == nil {
				klog.V(2).Infof("Endpoint %q in Endpoints %s/%s does not have an associated pod. Skipping", endpointAddress.Addresses, ed.Meta.Namespace, ed.Meta.Name)
				continue
			}
			dupCount += validateAndAddEndpoints(endpointAddress, zoneGetter, podLister, nodeLister, matchPort, networkEndpointType, targetMap, endpointPodMap)
		}
	}
	return targetMap, endpointPodMap, dupCount, nil
}

// validateAndAddEndpoints fills in missing information and creates network endpoint for each endpoint addresss
func validateAndAddEndpoints(ep negtypes.AddressData, zoneGetter negtypes.ZoneGetter, podLister, nodeLister cache.Indexer, matchPort string, endpointType negtypes.NetworkEndpointType, targetMap map[string]negtypes.NetworkEndpointSet, endpointPodMap negtypes.EndpointPodMap) int {
	var dupCount int
	for _, address := range ep.Addresses {
		key := fmt.Sprintf("%s/%s", ep.TargetRef.Namespace, ep.TargetRef.Name)
		obj, exists, err := podLister.GetByKey(key)
		if err != nil || !exists {
			klog.V(2).Infof("Endpoint %q does not correspond to an existing pod. Skipping", address)
			continue
		}
		pod, ok := obj.(*apiv1.Pod)
		if !ok {
			klog.V(2).Infof("Endpoint %q does not correspond to a pod object. Skipping", address)
			continue
		}
		if !validatePod(pod, nodeLister) {
			klog.V(2).Infof("Endpoint %q does not correspond to a valid pod resource. Skipping", address)
			continue
		}
		nodeName := pod.Spec.NodeName
		zone, err := zoneGetter.GetZoneForNode(nodeName)
		if err != nil {
			klog.V(2).Infof("Endpoint %q does not have valid zone information. Skipping", address)
			continue
		}

		if endpointType == negtypes.NonGCPPrivateEndpointType {
			// Non-GCP network endpoints don't have associated nodes.
			nodeName = ""
		}
		networkEndpoint := negtypes.NetworkEndpoint{IP: address, Port: matchPort, Node: nodeName}
		if targetMap[zone] == nil {
			targetMap[zone] = negtypes.NewNetworkEndpointSet()
		}
		targetMap[zone].Insert(networkEndpoint)
		// increment the count for duplicated endpoint
		if _, contains := endpointPodMap[networkEndpoint]; contains {
			dupCount += 1
		}
		endpointPodMap[networkEndpoint] = types.NamespacedName{Namespace: ep.TargetRef.Namespace, Name: ep.TargetRef.Name}
	}
	return dupCount
}

// validatePod checks if this pod is a valid pod resource
// it returns false if the pod:
// 1. is in terminal state
// 2. corresponds to a non-existent node
func validatePod(pod *apiv1.Pod, nodeLister cache.Indexer) bool {
	// Terminal Pod means a pod is in PodFailed or PodSucceeded phase
	phase := pod.Status.Phase
	if phase == apiv1.PodFailed || phase == apiv1.PodSucceeded {
		klog.V(2).Info("Pod %s/%s is a terminal pod with status %v, skipping", pod.ObjectMeta.Namespace, pod.ObjectMeta.Name, phase)
		return false
	}
	obj, exists, err := nodeLister.GetByKey(pod.Spec.NodeName)
	if err != nil || !exists {
		klog.V(2).Info("Pod %s/%s corresponds to a non-existing node %s, skipping", pod.ObjectMeta.Namespace, pod.ObjectMeta.Name, pod.Spec.NodeName)
		return false
	}
	_, isNode := obj.(*apiv1.Node)
	if !isNode {
		klog.V(2).Info("Pod %s/%s does not correspond to a valid node resource, skipping", pod.ObjectMeta.Namespace, pod.ObjectMeta.Name)
		return false
	}
	return true
}

// retrieveExistingZoneNetworkEndpointMap lists existing network endpoints in the neg and return the zone and endpoints map
func retrieveExistingZoneNetworkEndpointMap(negName string, zoneGetter negtypes.ZoneGetter, cloud negtypes.NetworkEndpointGroupCloud, version meta.Version, mode negtypes.EndpointsCalculatorMode) (map[string]negtypes.NetworkEndpointSet, error) {
	// Include zones that have non-candidate nodes currently. It is possible that NEGs were created in those zones previously and the endpoints now became non-candidates.
	// Endpoints in those NEGs now need to be removed. This mostly applies to VM_IP_NEGs where the endpoints are nodes.
	zones, err := zoneGetter.ListZones(utils.AllNodesPredicate)
	if err != nil {
		return nil, err
	}

	candidateNodeZones, err := zoneGetter.ListZones(negtypes.NodePredicateForEndpointCalculatorMode(mode))
	if err != nil {
		return nil, err
	}
	candidateZonesMap := sets.NewString(candidateNodeZones...)

	zoneNetworkEndpointMap := map[string]negtypes.NetworkEndpointSet{}
	for _, zone := range zones {
		networkEndpointsWithHealthStatus, err := cloud.ListNetworkEndpoints(negName, zone, false, version)
		if err != nil {
			// It is possible for a NEG to be missing in a zone without candidate nodes. Log and ignore this error.
			// NEG not found in a candidate zone is an error.
			if utils.IsNotFoundError(err) && !candidateZonesMap.Has(zone) {
				klog.Infof("Ignoring NotFound error for NEG %q in zone %q", negName, zone)
				continue
			}
			return nil, fmt.Errorf("Failed to lookup NEG in zone %q, candidate zones %v, err - %v", zone, candidateZonesMap, err)
		}
		zoneNetworkEndpointMap[zone] = negtypes.NewNetworkEndpointSet()
		for _, ne := range networkEndpointsWithHealthStatus {
			newNE := negtypes.NetworkEndpoint{IP: ne.NetworkEndpoint.IpAddress, Node: ne.NetworkEndpoint.Instance}
			if ne.NetworkEndpoint.Port != 0 {
				newNE.Port = strconv.FormatInt(ne.NetworkEndpoint.Port, 10)
			}
			zoneNetworkEndpointMap[zone].Insert(newNE)
		}
	}
	return zoneNetworkEndpointMap, nil
}

// makeEndpointBatch return a batch of endpoint from the input and remove the endpoints from input set
// The return map has the encoded endpoint as key and GCE network endpoint object as value
func makeEndpointBatch(endpoints negtypes.NetworkEndpointSet, negType negtypes.NetworkEndpointType) (map[negtypes.NetworkEndpoint]*composite.NetworkEndpoint, error) {
	endpointBatch := map[negtypes.NetworkEndpoint]*composite.NetworkEndpoint{}

	for i := 0; i < MAX_NETWORK_ENDPOINTS_PER_BATCH; i++ {
		networkEndpoint, ok := endpoints.PopAny()
		if !ok {
			break
		}
		if negType == negtypes.VmIpEndpointType {
			endpointBatch[networkEndpoint] = &composite.NetworkEndpoint{
				Instance:  networkEndpoint.Node,
				IpAddress: networkEndpoint.IP,
			}
		} else {
			portNum, err := strconv.Atoi(networkEndpoint.Port)
			if err != nil {
				return nil, fmt.Errorf("failed to decode endpoint port %v: %w", networkEndpoint, err)
			}
			endpointBatch[networkEndpoint] = &composite.NetworkEndpoint{
				Instance:  networkEndpoint.Node,
				IpAddress: networkEndpoint.IP,
				Port:      int64(portNum),
			}
		}
	}
	return endpointBatch, nil
}
