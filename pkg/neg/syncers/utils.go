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
	"net"
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
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/neg/syncers/labels"
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

// findAndFilterMigrationEndpoints will filter out the migration endpoints from
// the `addEndpoints` and `removeEndpoints` sets. The passed sets will get
// modified. The returned value will be two endpoints sets which will contain
// the values that were filtered out from the `addEndpoints` and
// `removeEndpoints` sets respectively.
//
// TODO(gauravkghildiyal): This function should be moved alongside
// DualStackMigrator once that gets merged.
func findAndFilterMigrationEndpoints(addEndpoints, removeEndpoints map[string]negtypes.NetworkEndpointSet) (map[string]negtypes.NetworkEndpointSet, map[string]negtypes.NetworkEndpointSet) {
	allEndpoints := make(map[string]negtypes.NetworkEndpointSet)
	for zone, endpointSet := range addEndpoints {
		allEndpoints[zone] = allEndpoints[zone].Union(endpointSet)
	}
	for zone, endpointSet := range removeEndpoints {
		allEndpoints[zone] = allEndpoints[zone].Union(endpointSet)
	}

	migrationEndpointsInAddSet := make(map[string]negtypes.NetworkEndpointSet)
	migrationEndpointsInRemoveSet := make(map[string]negtypes.NetworkEndpointSet)
	for zone, endpointSet := range allEndpoints {
		for endpoint := range endpointSet {
			if endpoint.IP == "" || endpoint.IPv6 == "" {
				// Endpoint is not dual-stack so continue.
				continue
			}

			// At this point, we know that `endpoint` is a dual-stack endpoint. An
			// endpoint can be identified as migrating if the IPs from the dual-stack
			// endpoint exist as individual single-stack endpoint inside
			// `addEndpoints` or `removeEndpoints`.

			// Construct single-stack endpoint corresponding to the dual-stack
			// endpoint. Their existence will determine if an endpoint is migrating.
			ipv4Only := endpoint
			ipv4Only.IPv6 = ""
			ipv6Only := endpoint
			ipv6Only.IP = ""

			isMigrating := false
			// Check if endpoint is migrating from dual-stack to single-stack.
			isMigrating = isMigrating || moveEndpoint(ipv4Only, addEndpoints, migrationEndpointsInAddSet, zone)
			isMigrating = isMigrating || moveEndpoint(ipv6Only, addEndpoints, migrationEndpointsInAddSet, zone)
			// Check if endpoint is migrating from single-stack to dual-stack.
			isMigrating = isMigrating || moveEndpoint(ipv4Only, removeEndpoints, migrationEndpointsInRemoveSet, zone)
			isMigrating = isMigrating || moveEndpoint(ipv6Only, removeEndpoints, migrationEndpointsInRemoveSet, zone)

			if isMigrating {
				moveEndpoint(endpoint, addEndpoints, migrationEndpointsInAddSet, zone)
				moveEndpoint(endpoint, removeEndpoints, migrationEndpointsInRemoveSet, zone)
			}
		}
	}

	return migrationEndpointsInAddSet, migrationEndpointsInRemoveSet
}

// moveEndpoint deletes endpoint `e` from `source[zone]` and adds it to
// `dest[zone]`. If the move was successful, `true` is returned. A return value
// of `false` denotes that nothing was moved and no input variable were
// modified.
func moveEndpoint(e negtypes.NetworkEndpoint, source, dest map[string]negtypes.NetworkEndpointSet, zone string) bool {
	if source == nil || dest == nil {
		return false
	}
	if source[zone].Has(e) {
		source[zone].Delete(e)
		if dest[zone] == nil {
			dest[zone] = negtypes.NewNetworkEndpointSet()
		}
		dest[zone].Insert(e)
		return true
	}
	return false
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

type ZoneNetworkEndpointMapResult struct {
	NetworkEndpointSet map[string]negtypes.NetworkEndpointSet
	EndpointPodMap     negtypes.EndpointPodMap
	DupCount           int
}

// toZoneNetworkEndpointMap translates addresses in endpoints object into zone and endpoints map, and also return the count for duplicated endpoints
func toZoneNetworkEndpointMap(eds []negtypes.EndpointsData, zoneGetter negtypes.ZoneGetter, podLister cache.Indexer, servicePortName string, networkEndpointType negtypes.NetworkEndpointType, enableDualStackNEG bool) (ZoneNetworkEndpointMapResult, error) {
	zoneNetworkEndpointMap := map[string]negtypes.NetworkEndpointSet{}
	networkEndpointPodMap := negtypes.EndpointPodMap{}
	ipsForPod := ipsForPod(eds)
	dupCount := 0
	if eds == nil {
		klog.Errorf("Endpoint object is nil")
		return ZoneNetworkEndpointMapResult{
			NetworkEndpointSet: zoneNetworkEndpointMap,
			EndpointPodMap:     networkEndpointPodMap,
			DupCount:           dupCount,
		}, nil
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
			if !enableDualStackNEG && endpointAddress.AddressType != discovery.AddressTypeIPv4 {
				klog.Infof("Skipping non IPv4 address: %q, in endpoint slice %s/%s", endpointAddress.Addresses, ed.Meta.Namespace, ed.Meta.Name)
				continue
			}
			zone, getZoneErr := getEndpointZone(endpointAddress, zoneGetter)
			if getZoneErr != nil {
				klog.Errorf("Detected unexpected error when getting zone, err: %v", getZoneErr)
				return ZoneNetworkEndpointMapResult{
						NetworkEndpointSet: nil,
						EndpointPodMap:     nil,
						DupCount:           dupCount,
					},
					fmt.Errorf("Unexpected error when getting zone for Endpoint %q in Endpoints %s/%s: %w", endpointAddress.Addresses, ed.Meta.Namespace, ed.Meta.Name, getZoneErr)
			}
			// pod is used for label propagation
			_, getPodErr := getEndpointPod(endpointAddress, podLister)
			if getPodErr != nil {
				if flags.F.EnableDegradedMode {
					klog.Errorf("Detected unexpected error when getting pod, err: %v", getPodErr)
					return ZoneNetworkEndpointMapResult{
							NetworkEndpointSet: nil,
							EndpointPodMap:     nil,
							DupCount:           dupCount,
						}, // when degraded mode is enabled, we want to trigger degraded mode so return the error
						fmt.Errorf("Unexpected error when getting pod for Endpoint %q in Endpoints %s/%s: %w", endpointAddress.Addresses, ed.Meta.Namespace, ed.Meta.Name, getPodErr)
				}
				klog.V(2).Infof("Endpoint %q in Endpoints %s/%s does not have an associated pod. Skipping", endpointAddress.Addresses, ed.Meta.Namespace, ed.Meta.Name)
				continue
			}
			if zoneNetworkEndpointMap[zone] == nil {
				zoneNetworkEndpointMap[zone] = negtypes.NewNetworkEndpointSet()
			}

			podIPs := ipsForPod[types.NamespacedName{Namespace: endpointAddress.TargetRef.Namespace, Name: endpointAddress.TargetRef.Name}]
			networkEndpoint := negtypes.NetworkEndpoint{IP: podIPs.IP, Port: matchPort, Node: *endpointAddress.NodeName}
			if enableDualStackNEG {
				// Convert all addresses to a standard form as per rfc5952 to prevent
				// accidental diffs resulting from different formats.
				networkEndpoint.IPv6 = parseIPAddress(podIPs.IPv6)
			}
			if networkEndpointType == negtypes.NonGCPPrivateEndpointType {
				// Non-GCP network endpoints don't have associated nodes.
				networkEndpoint.Node = ""
			}
			zoneNetworkEndpointMap[zone].Insert(networkEndpoint)

			// if existing name is alphabetically lower than current one, continue and don't replace
			if existingPod, contains := networkEndpointPodMap[networkEndpoint]; contains {
				dupCount += 1
				if existingPod.Name < endpointAddress.TargetRef.Name {
					klog.Infof("Found duplicate endpoints for %v, save the pod information from the alphabetically higher pod", networkEndpoint)
					continue // if existing name is alphabetically lower than current one, continue and don't replace
				}
			}
			networkEndpointPodMap[networkEndpoint] = types.NamespacedName{Namespace: endpointAddress.TargetRef.Namespace, Name: endpointAddress.TargetRef.Name}
		}
	}
	if !foundMatchingPort {
		klog.Errorf("Service port name %q was not found in the endpoints object %+v", servicePortName, eds)
	}

	if len(zoneNetworkEndpointMap) == 0 || len(networkEndpointPodMap) == 0 {
		klog.V(3).Infof("Generated empty endpoint maps (zoneNetworkEndpointMap: %+v, networkEndpointPodMap: %v) from Endpoints object: %+v", zoneNetworkEndpointMap, networkEndpointPodMap, eds)
	}
	return ZoneNetworkEndpointMapResult{
		NetworkEndpointSet: zoneNetworkEndpointMap,
		EndpointPodMap:     networkEndpointPodMap,
		DupCount:           dupCount,
	}, nil
}

// getEndpointZone use an endpoint's node information to get its corresponding zone
func getEndpointZone(
	endpointAddress negtypes.AddressData,
	zoneGetter negtypes.ZoneGetter,
) (string, error) {
	if endpointAddress.NodeName == nil || len(*endpointAddress.NodeName) == 0 {
		return "", negtypes.ErrEPNodeMissing
	}
	zone, err := zoneGetter.GetZoneForNode(*endpointAddress.NodeName)
	if err != nil {
		return zone, fmt.Errorf("%w: %v", negtypes.ErrEPNodeNotFound, err)
	}
	if zone == "" {
		return zone, fmt.Errorf("%w: zone is missing for node %v", negtypes.ErrEPZoneMissing, *endpointAddress.NodeName)
	}
	return zone, nil
}

// getEndpointPod use an endpoint's pod information to get its corresponding pod object
func getEndpointPod(
	endpointAddress negtypes.AddressData,
	podLister cache.Indexer,
) (*apiv1.Pod, error) {
	if endpointAddress.TargetRef == nil {
		return nil, negtypes.ErrEPPodMissing
	}
	key := fmt.Sprintf("%s/%s", endpointAddress.TargetRef.Namespace, endpointAddress.TargetRef.Name)
	obj, exists, err := podLister.GetByKey(key)
	if err != nil || !exists {
		return nil, negtypes.ErrEPPodNotFound
	}
	pod, ok := obj.(*apiv1.Pod)
	if !ok {
		return nil, negtypes.ErrEPPodTypeAssertionFailed
	}
	return pod, nil
}

// toZoneNetworkEndpointMap translates addresses in endpoints object into zone and endpoints map, and also return the count for duplicated endpoints
// we will not raise error in degraded mode for misconfigured endpoints, instead they will be filtered directly
func toZoneNetworkEndpointMapDegradedMode(
	eds []negtypes.EndpointsData,
	zoneGetter negtypes.ZoneGetter,
	podLister, nodeLister cache.Indexer,
	servicePortName string,
	networkEndpointType negtypes.NetworkEndpointType,
) ZoneNetworkEndpointMapResult {
	zoneNetworkEndpointMap := map[string]negtypes.NetworkEndpointSet{}
	networkEndpointPodMap := negtypes.EndpointPodMap{}
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
			pod, getPodErr := getEndpointPod(endpointAddress, podLister)
			if getPodErr != nil {
				klog.Errorf("Endpoint %q in Endpoints %s/%s receives error when getting pod, err: %v, skipping", endpointAddress.Addresses, ed.Meta.Namespace, ed.Meta.Name, getPodErr)
				continue
			}
			if !validatePod(pod, nodeLister) {
				klog.Errorf("Endpoint %q in Endpoints %s/%s correponds to an invalid pod: %v, skipping", endpointAddress.Addresses, ed.Meta.Namespace, ed.Meta.Name, getPodErr)
				continue
			}
			nodeName := pod.Spec.NodeName
			zone, err := zoneGetter.GetZoneForNode(nodeName)
			if err != nil {
				klog.V(2).Infof("For endpoint %q in pod %q, its corresponding node %q does not have valid zone information, skipping", endpointAddress.Addresses, pod.ObjectMeta.Name, nodeName)
				continue
			}
			if zoneNetworkEndpointMap[zone] == nil {
				zoneNetworkEndpointMap[zone] = negtypes.NewNetworkEndpointSet()
			}
			for _, address := range endpointAddress.Addresses {
				networkEndpoint := negtypes.NetworkEndpoint{IP: address, Port: matchPort, Node: nodeName}
				if networkEndpointType == negtypes.NonGCPPrivateEndpointType {
					// Non-GCP network endpoints don't have associated nodes.
					networkEndpoint.Node = ""
				}
				zoneNetworkEndpointMap[zone].Insert(networkEndpoint)

				if existingPod, contains := networkEndpointPodMap[networkEndpoint]; contains {
					// if existing name is alphabetically lower than current one, continue and don't replace
					if existingPod.Name < endpointAddress.TargetRef.Name {
						klog.Infof("Found duplicate endpoints for %q, save the pod information from the alphabetically higher pod", address)
						continue
					}
				}
				networkEndpointPodMap[networkEndpoint] = types.NamespacedName{Namespace: endpointAddress.TargetRef.Namespace, Name: endpointAddress.TargetRef.Name}
			}
		}
	}
	return ZoneNetworkEndpointMapResult{
		NetworkEndpointSet: zoneNetworkEndpointMap,
		EndpointPodMap:     networkEndpointPodMap,
	}
}

// validatePod checks if this pod is a valid pod resource
// it returns false if the pod:
// 1. is in terminal state
// 2. corresponds to a non-existent node
func validatePod(pod *apiv1.Pod, nodeLister cache.Indexer) bool {
	// Terminal Pod means a pod is in PodFailed or PodSucceeded phase
	phase := pod.Status.Phase
	if phase == apiv1.PodFailed || phase == apiv1.PodSucceeded {
		klog.V(2).Infof("Pod %s/%s is a terminal pod with status %v, skipping", pod.ObjectMeta.Namespace, pod.ObjectMeta.Name, phase)
		return false
	}
	obj, exists, err := nodeLister.GetByKey(pod.Spec.NodeName)
	if err != nil || !exists {
		klog.V(2).Infof("Pod %s/%s corresponds to a non-existing node %s, skipping", pod.ObjectMeta.Namespace, pod.ObjectMeta.Name, pod.Spec.NodeName)
		return false
	}
	_, isNode := obj.(*apiv1.Node)
	if !isNode {
		klog.V(2).Infof("Pod %s/%s does not correspond to a valid node resource, skipping", pod.ObjectMeta.Namespace, pod.ObjectMeta.Name)
		return false
	}
	return true
}

// ipsForPod will return a mapping of pods to their IPv4 and IPv6 addresses.
func ipsForPod(eds []negtypes.EndpointsData) map[types.NamespacedName]negtypes.NetworkEndpoint {
	result := make(map[types.NamespacedName]negtypes.NetworkEndpoint)
	for _, ed := range eds {
		for _, address := range ed.Addresses {
			if address.TargetRef == nil || len(address.Addresses) < 1 {
				continue
			}
			podNN := types.NamespacedName{Namespace: address.TargetRef.Namespace, Name: address.TargetRef.Name}
			ne := result[podNN]
			if address.AddressType == discovery.AddressTypeIPv4 {
				// See the discussion in https://issue.k8s.io/106267 regarding why it is
				// valid if we just pick the first IP.
				ne.IP = address.Addresses[0]
			}
			if address.AddressType == discovery.AddressTypeIPv6 {
				// See the discussion in https://issue.k8s.io/106267 regarding why it is
				// valid if we just pick the first IP.
				ne.IPv6 = address.Addresses[0]
			}
			result[podNN] = ne
		}
	}
	return result
}

// retrieveExistingZoneNetworkEndpointMap lists existing network endpoints in the neg and return the zone and endpoints map
func retrieveExistingZoneNetworkEndpointMap(negName string, zoneGetter negtypes.ZoneGetter, cloud negtypes.NetworkEndpointGroupCloud, version meta.Version, mode negtypes.EndpointsCalculatorMode) (map[string]negtypes.NetworkEndpointSet, labels.EndpointPodLabelMap, error) {
	// Include zones that have non-candidate nodes currently. It is possible that NEGs were created in those zones previously and the endpoints now became non-candidates.
	// Endpoints in those NEGs now need to be removed. This mostly applies to VM_IP_NEGs where the endpoints are nodes.
	zones, err := zoneGetter.ListZones(utils.AllNodesPredicate)
	if err != nil {
		return nil, nil, err
	}

	candidateNodeZones, err := zoneGetter.ListZones(negtypes.NodePredicateForEndpointCalculatorMode(mode))
	if err != nil {
		return nil, nil, err
	}
	candidateZonesMap := sets.NewString(candidateNodeZones...)

	zoneNetworkEndpointMap := map[string]negtypes.NetworkEndpointSet{}
	endpointPodLabelMap := labels.EndpointPodLabelMap{}
	for _, zone := range zones {
		networkEndpointsWithHealthStatus, err := cloud.ListNetworkEndpoints(negName, zone, false, version)
		if err != nil {
			// It is possible for a NEG to be missing in a zone without candidate nodes. Log and ignore this error.
			// NEG not found in a candidate zone is an error.
			if utils.IsNotFoundError(err) && !candidateZonesMap.Has(zone) {
				klog.Infof("Ignoring NotFound error for NEG %q in zone %q", negName, zone)
				continue
			}
			return nil, nil, fmt.Errorf("Failed to lookup NEG in zone %q, candidate zones %v, err - %v", zone, candidateZonesMap, err)
		}
		zoneNetworkEndpointMap[zone] = negtypes.NewNetworkEndpointSet()
		for _, ne := range networkEndpointsWithHealthStatus {
			newNE := negtypes.NetworkEndpoint{IP: ne.NetworkEndpoint.IpAddress, Node: ne.NetworkEndpoint.Instance}
			if ne.NetworkEndpoint.Port != 0 {
				newNE.Port = strconv.FormatInt(ne.NetworkEndpoint.Port, 10)
			}
			zoneNetworkEndpointMap[zone].Insert(newNE)
			endpointPodLabelMap[newNE] = ne.NetworkEndpoint.Annotations
		}
	}
	return zoneNetworkEndpointMap, endpointPodLabelMap, nil
}

// makeEndpointBatch return a batch of endpoint from the input and remove the endpoints from input set
// The return map has the encoded endpoint as key and GCE network endpoint object as value
func makeEndpointBatch(endpoints negtypes.NetworkEndpointSet, negType negtypes.NetworkEndpointType, endpointPodLabelMap labels.EndpointPodLabelMap) (map[negtypes.NetworkEndpoint]*composite.NetworkEndpoint, error) {
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
			cloudNetworkEndpoint := &composite.NetworkEndpoint{
				Instance:  networkEndpoint.Node,
				IpAddress: networkEndpoint.IP,
				Port:      int64(portNum),
			}
			if flags.F.EnableNEGLabelPropagation {
				annotations, ok := endpointPodLabelMap[networkEndpoint]
				if !ok {
					klog.Errorf("Can not find annotations for endpoint %v from endpointPodLabelMap: %v", networkEndpoint, err)
				} else {
					cloudNetworkEndpoint.Annotations = annotations
				}
			}
			endpointBatch[networkEndpoint] = cloudNetworkEndpoint
		}
	}
	return endpointBatch, nil
}

// parseIPAddress is used to normalize the given IPv4 or IPv6 address. If the
// address is invalid, an empty string is returned.
func parseIPAddress(address string) string {
	result := net.ParseIP(address).String()
	if result == "<nil>" {
		return ""
	}
	return result
}
