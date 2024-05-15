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
	"errors"
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
	"k8s.io/ingress-gce/pkg/neg/metrics"
	"k8s.io/ingress-gce/pkg/neg/syncers/labels"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/ingress-gce/pkg/network"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/zonegetter"
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

	// managedByEPSControllerValue is a unique value used with LabelManagedBy to indicate
	// the EndpointSlice is managed by the endpoint slice controller.
	managedByEPSControllerValue = "endpointslice-controller.k8s.io"
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
func getService(serviceLister cache.Indexer, namespace, name string, logger klog.Logger) *apiv1.Service {
	if serviceLister == nil {
		return nil
	}
	service, exists, err := serviceLister.GetByKey(utils.ServiceKeyFunc(namespace, name))
	if exists && err == nil {
		return service.(*apiv1.Service)
	}
	if err != nil {
		logger.Error(err, "Failed to retrieve service from store", "namespace", namespace, "name", name)
		metrics.PublishNegControllerErrorCountMetrics(err, true)
	}
	return nil
}

// ensureNetworkEndpointGroup ensures corresponding NEG is configured correctly in the specified zone.
func ensureNetworkEndpointGroup(svcNamespace, svcName, negName, zone, negServicePortName, kubeSystemUID, port string, networkEndpointType negtypes.NetworkEndpointType, cloud negtypes.NetworkEndpointGroupCloud, serviceLister cache.Indexer, recorder record.EventRecorder, version meta.Version, customName bool, networkInfo network.NetworkInfo, logger klog.Logger) (negv1beta1.NegObjectReference, error) {
	negLogger := logger.WithValues("negName", negName, "zone", zone)
	var negRef negv1beta1.NegObjectReference
	neg, err := cloud.GetNetworkEndpointGroup(negName, zone, version, logger)
	if err != nil {
		if !utils.IsNotFoundError(err) {
			negLogger.Error(err, "Failed to get Neg")
			return negRef, err
		}
		negLogger.Info("Neg was not found", "err", err)
		metrics.PublishNegControllerErrorCountMetrics(err, true)
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
			negLogger.Error(nil, "Found Neg with custom name but empty description")
			return negv1beta1.NegObjectReference{}, fmt.Errorf("neg name %s is already in use, found a custom named neg with an empty description", negName)
		}
		if matches, err := utils.VerifyDescription(expectedDesc, neg.Description, negName, zone); !matches {
			negLogger.Error(err, "Neg Name is already in use")
			return negv1beta1.NegObjectReference{}, fmt.Errorf("neg name %s is already in use, found conflicting description: %w", negName, err)
		}

		if networkEndpointType != negtypes.NonGCPPrivateEndpointType &&
			// Only perform the following checks when the NEGs are not Non-GCP NEGs.
			// Non-GCP NEGs do not have associated network and subnetwork.
			(!utils.EqualResourceIDs(neg.Network, networkInfo.NetworkURL) ||
				!utils.EqualResourceIDs(neg.Subnetwork, networkInfo.SubnetworkURL)) {

			needToCreate = true
			negLogger.Info("NEG does not match network and subnetwork of the cluster. Deleting NEG")
			err = cloud.DeleteNetworkEndpointGroup(negName, zone, version, logger)
			if err != nil {
				return negRef, err
			}
			if recorder != nil && serviceLister != nil {
				if svc := getService(serviceLister, svcNamespace, svcName, logger); svc != nil {
					recorder.Eventf(svc, apiv1.EventTypeNormal, "Delete", "Deleted NEG %q for %s in %q.", negName, negServicePortName, zone)
				}
			}
		}
	}

	if needToCreate {
		var subnetwork string
		switch networkEndpointType {
		case negtypes.NonGCPPrivateEndpointType:
			subnetwork = ""
		default:
			subnetwork = networkInfo.SubnetworkURL
		}
		negLogger.Info("Creating NEG", "negServicePortName", negServicePortName, "network", networkInfo.NetworkURL, "subnetwork", subnetwork)
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
			Network:             networkInfo.NetworkURL,
			Subnetwork:          subnetwork,
			Description:         desc,
		}, zone, logger)
		if err != nil {
			return negRef, err
		}
		if recorder != nil && serviceLister != nil {
			if svc := getService(serviceLister, svcNamespace, svcName, logger); svc != nil {
				recorder.Eventf(svc, apiv1.EventTypeNormal, "Create", "Created NEG %q for %s in %q.", negName, negServicePortName, zone)
			}
		}
	}

	if neg == nil {
		var err error
		neg, err = cloud.GetNetworkEndpointGroup(negName, zone, version, logger)
		if err != nil {
			negLogger.Error(err, "Error while retrieving NEG after initialization")
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
	EPCount            negtypes.StateCountMap
	EPSCount           negtypes.StateCountMap
}

// toZoneNetworkEndpointMap translates addresses in endpoints object into zone and endpoints map, and also return the count for duplicated endpoints
func toZoneNetworkEndpointMap(eds []negtypes.EndpointsData, zoneGetter *zonegetter.ZoneGetter, podLister cache.Indexer, servicePortName string, networkEndpointType negtypes.NetworkEndpointType, enableDualStackNEG, enableMultiSubnetCluster bool, logger klog.Logger) (ZoneNetworkEndpointMapResult, error) {
	zoneNetworkEndpointMap := map[string]negtypes.NetworkEndpointSet{}
	networkEndpointPodMap := negtypes.EndpointPodMap{}
	ipsForPod := ipsForPod(eds)
	globalEPCount := make(negtypes.StateCountMap)
	globalEPSCount := make(negtypes.StateCountMap)
	if eds == nil {
		logger.Error(nil, "Endpoint object is nil")
		return ZoneNetworkEndpointMapResult{
			NetworkEndpointSet: zoneNetworkEndpointMap,
			EndpointPodMap:     networkEndpointPodMap,
			EPCount:            globalEPCount,
			EPSCount:           globalEPSCount,
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
		localEPCount := make(negtypes.StateCountMap)
		globalEPSCount[negtypes.Total] += 1
		for _, endpointAddress := range ed.Addresses {
			epLogger := logger.WithValues(
				"endpoint", endpointAddress.Addresses,
				"endpointSliceNamespace", ed.Meta.Namespace,
				"endpointSliceName", ed.Meta.Name,
			)
			if !enableDualStackNEG && endpointAddress.AddressType != discovery.AddressTypeIPv4 {
				epLogger.Info("Skipping non IPv4 address")
				continue
			}
			globalEPCount[negtypes.Total] += 1
			zone, _, getZoneErr := getEndpointZone(endpointAddress, zoneGetter, logger)
			if getZoneErr != nil {
				metrics.PublishNegControllerErrorCountMetrics(getZoneErr, true)
				if enableMultiSubnetCluster && errors.Is(getZoneErr, zonegetter.ErrNodeNotInDefaultSubnet) {
					epLogger.Error(getZoneErr, "Detected endpoint not from default subnet. Skipping")
					localEPCount[negtypes.NodeInNonDefaultSubnet]++
					continue
				}
				epLogger.Error(getZoneErr, "Detected unexpected error when getting zone for endpoint")
				return ZoneNetworkEndpointMapResult{}, fmt.Errorf("unexpected error when getting zone for endpoint %q in endpoint slice %s/%s: %w", endpointAddress.Addresses, ed.Meta.Namespace, ed.Meta.Name, getZoneErr)
			}

			_, _, getPodErr := getEndpointPod(endpointAddress, podLister)
			if getPodErr != nil {
				metrics.PublishNegControllerErrorCountMetrics(getPodErr, true)
				if flags.F.EnableDegradedMode {
					epLogger.Error(getPodErr, "Detected unexpected error when getting pod for endpoint")
					// when degraded mode is enabled, we want to trigger degraded mode so return the error
					return ZoneNetworkEndpointMapResult{}, fmt.Errorf("unexpected error when getting pod for endpoint %q in endpoint slice %s/%s: %w", endpointAddress.Addresses, ed.Meta.Namespace, ed.Meta.Name, getPodErr)
				}
				epLogger.V(2).Info("Endpoint does not have an associated pod. Skipping")
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
			neLogger := epLogger.WithValues(
				"ipv4Address", networkEndpoint.IP,
				"ipv6Address", networkEndpoint.IPv6,
				"enableDualStackNEG", enableDualStackNEG,
			)
			if networkEndpointType == negtypes.NonGCPPrivateEndpointType {
				// Non-GCP network endpoints don't have associated nodes.
				networkEndpoint.Node = ""
			}
			zoneNetworkEndpointMap[zone].Insert(networkEndpoint)

			// if existing name is alphabetically lower than current one, continue and don't replace
			if existingPod, contains := networkEndpointPodMap[networkEndpoint]; contains {
				localEPCount[negtypes.Duplicate] += 1
				if existingPod.Name < endpointAddress.TargetRef.Name {
					neLogger.Info("Found duplicate endpoints when processing endpoint slice, save the pod information from the alphabetically higher pod", "ignoredPod", endpointAddress.TargetRef.Name, "usePod", existingPod.Name)
					continue // if existing name is alphabetically lower than current one, continue and don't replace
				}
			}
			networkEndpointPodMap[networkEndpoint] = types.NamespacedName{Namespace: endpointAddress.TargetRef.Namespace, Name: endpointAddress.TargetRef.Name}
		}
		mergeWithGlobalCounts(localEPCount, globalEPCount, globalEPSCount)
	}
	if !foundMatchingPort {
		logger.Error(nil, "Service port name was not found in the endpoints object", "servicePortName", servicePortName, "endpointsObject", eds)
	}

	if len(zoneNetworkEndpointMap) == 0 || len(networkEndpointPodMap) == 0 {
		logger.V(3).Info("Generated empty endpoint maps from Endpoints object", "zoneNetworkEndpointMap", zoneNetworkEndpointMap, "networkEndpointPodMap", networkEndpointPodMap, "endpointsObject", eds)
	}
	return ZoneNetworkEndpointMapResult{
		NetworkEndpointSet: zoneNetworkEndpointMap,
		EndpointPodMap:     networkEndpointPodMap,
		EPCount:            globalEPCount,
		EPSCount:           globalEPSCount,
	}, nil
}

// mergeWithGlobalCounts update the overall endpoint and endpoint slice count based on the endpoint counts from an endpoint slice
func mergeWithGlobalCounts(localEPCount, globalEPCount, globalEPSCount negtypes.StateCountMap) {
	if localEPCount == nil || globalEPCount == nil || globalEPSCount == nil {
		return
	}
	for state, count := range localEPCount {
		if count > 0 {
			globalEPCount[state] += count
			globalEPSCount[state] += 1
		}
	}
}

// getEndpointZone use an endpoint's nodeName to get its corresponding zone
func getEndpointZone(endpointAddress negtypes.AddressData, zoneGetter *zonegetter.ZoneGetter, logger klog.Logger) (string, negtypes.StateCountMap, error) {
	count := make(negtypes.StateCountMap)
	if endpointAddress.NodeName == nil || len(*endpointAddress.NodeName) == 0 {
		count[negtypes.NodeMissing]++
		count[negtypes.ZoneMissing]++
		return "", count, negtypes.ErrEPNodeMissing
	}
	zone, err := zoneGetter.ZoneForNode(*endpointAddress.NodeName, logger)
	// Fail to get the node object.
	if errors.Is(err, zonegetter.ErrNodeNotFound) {
		count[negtypes.NodeNotFound]++
		return zone, count, fmt.Errorf("%w: %v", negtypes.ErrEPNodeNotFound, err)
	}
	// providerID missing in node or zone information missing in providerID.
	if errors.Is(err, zonegetter.ErrProviderIDNotFound) || errors.Is(err, zonegetter.ErrSplitProviderID) {
		count[negtypes.ZoneMissing]++
		return zone, count, fmt.Errorf("%w: zone is missing for node %v", negtypes.ErrEPZoneMissing, *endpointAddress.NodeName)
	}
	return zone, count, err
}

// getEndpointPod use an endpoint's pod name and namespace to get its corresponding pod object
func getEndpointPod(endpointAddress negtypes.AddressData, podLister cache.Indexer) (*apiv1.Pod, negtypes.StateCountMap, error) {
	count := make(negtypes.StateCountMap)
	if endpointAddress.TargetRef == nil {
		count[negtypes.PodInvalid]++
		return nil, count, negtypes.ErrEPPodMissing
	}
	key := fmt.Sprintf("%s/%s", endpointAddress.TargetRef.Namespace, endpointAddress.TargetRef.Name)
	obj, exists, err := podLister.GetByKey(key)
	if err != nil || !exists {
		count[negtypes.PodInvalid]++
		return nil, count, negtypes.ErrEPPodNotFound
	}
	pod, ok := obj.(*apiv1.Pod)
	if !ok {
		count[negtypes.OtherError]++
		return nil, count, negtypes.ErrEPPodTypeAssertionFailed
	}
	return pod, count, nil
}

// toZoneNetworkEndpointMap translates addresses in endpoints object into zone and endpoints map, and also return the count for duplicated endpoints
// we will not raise error in degraded mode for misconfigured endpoints, instead they will be filtered directly
func toZoneNetworkEndpointMapDegradedMode(eds []negtypes.EndpointsData, zoneGetter *zonegetter.ZoneGetter, podLister, nodeLister, serviceLister cache.Indexer, servicePortName string, networkEndpointType negtypes.NetworkEndpointType, enableDualStackNEG, enableMultiSubnetCluster bool, logger klog.Logger) ZoneNetworkEndpointMapResult {
	zoneNetworkEndpointMap := map[string]negtypes.NetworkEndpointSet{}
	networkEndpointPodMap := negtypes.EndpointPodMap{}
	ipsForPod := ipsForPod(eds)
	globalEPCount := make(negtypes.StateCountMap)
	globalEPSCount := make(negtypes.StateCountMap)
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
		localEPCount := make(negtypes.StateCountMap)
		globalEPSCount[negtypes.Total] += 1
		serviceName := ed.Meta.Labels[discovery.LabelServiceName]
		isCustomEPS := ed.Meta.Labels[discovery.LabelManagedBy] != managedByEPSControllerValue
		for _, endpointAddress := range ed.Addresses {
			epLogger := logger.WithValues(
				"endpoint", endpointAddress.Addresses,
				"endpointSliceNamespace", ed.Meta.Namespace,
				"endpointSliceName", ed.Meta.Name,
			)
			if !enableDualStackNEG && endpointAddress.AddressType != discovery.AddressTypeIPv4 {
				epLogger.Info("Skipping non IPv4 address in degraded mode")
				continue
			}
			globalEPCount[negtypes.Total] += 1
			pod, getPodStat, getPodErr := getEndpointPod(endpointAddress, podLister)
			if getPodErr != nil {
				epLogger.Error(getPodErr, "Endpoint receives error when getting pod, skipping")
				metrics.PublishNegControllerErrorCountMetrics(getPodErr, true)
				for state, count := range getPodStat {
					localEPCount[state] += count
				}
				continue
			}
			nodeName := pod.Spec.NodeName
			if nodeName == "" {
				epLogger.Error(negtypes.ErrEPNodeMissing, "Endpoint's corresponding pod does not have valid nodeName, skipping", "podName", pod.Name)
				metrics.PublishNegControllerErrorCountMetrics(negtypes.ErrEPNodeMissing, true)
				localEPCount[negtypes.NodeMissing]++
				continue
			}
			zone, getZoneErr := zoneGetter.ZoneForNode(nodeName, logger)
			if getZoneErr != nil {
				metrics.PublishNegControllerErrorCountMetrics(getZoneErr, true)
				if enableMultiSubnetCluster && errors.Is(getZoneErr, zonegetter.ErrNodeNotInDefaultSubnet) {
					epLogger.Error(getZoneErr, "Detected endpoint not from default subnet. Skipping", "nodeName", nodeName)
					localEPCount[negtypes.NodeInNonDefaultSubnet]++
					continue
				}
				epLogger.Error(getZoneErr, "Endpoint's corresponding node does not have valid zone information, skipping", "nodeName", nodeName)
				localEPCount[negtypes.NodeNotFound]++
				continue
			}
			if zoneNetworkEndpointMap[zone] == nil {
				zoneNetworkEndpointMap[zone] = negtypes.NewNetworkEndpointSet()
			}

			podIPs := ipsForPod[types.NamespacedName{Namespace: endpointAddress.TargetRef.Namespace, Name: endpointAddress.TargetRef.Name}]
			// TODO(cheungdavid): Remove this validation when single stack ipv6 endpoint is supported
			if parseIPAddress(podIPs.IP) == "" {
				epLogger.Error(negtypes.ErrEPIPInvalid, "Endpoint has an invalid IPv4 address, skipping", "podName", pod.ObjectMeta.Name)
				metrics.PublishNegControllerErrorCountMetrics(negtypes.ErrEPIPInvalid, true)
				localEPCount[negtypes.IPInvalid]++
				continue
			}
			networkEndpoint := negtypes.NetworkEndpoint{IP: podIPs.IP, Port: matchPort, Node: nodeName}
			if enableDualStackNEG {
				// Convert all addresses to a standard form as per rfc5952 to prevent
				// accidental diffs resulting from different formats.
				networkEndpoint.IPv6 = parseIPAddress(podIPs.IPv6)
			}
			neLogger := epLogger.WithValues(
				"ipv4Address", networkEndpoint.IP,
				"ipv6Address", networkEndpoint.IPv6,
				"enableDualStackNEG", enableDualStackNEG,
			)
			// endpoint address should match to the IP of its pod
			checkIPErr := podContainsEndpointAddress(networkEndpoint, pod)
			if checkIPErr != nil {
				neLogger.Error(checkIPErr, "Endpoint has at least one IP that not match to its pod, skipping", "podName", pod.Name)
				metrics.PublishNegControllerErrorCountMetrics(checkIPErr, true)
				localEPCount[negtypes.IPNotFromPod] += 1
				continue
			}
			validatePodStat, validateErr := validatePod(pod, nodeLister, serviceLister, networkEndpoint, serviceName, isCustomEPS, logger)
			if validateErr != nil {
				neLogger.Error(validateErr, "Endpoint correponds to an invalid pod, skipping")
				metrics.PublishNegControllerErrorCountMetrics(validateErr, true)
				for state, count := range validatePodStat {
					localEPCount[state] += count
				}
				continue
			}
			if networkEndpointType == negtypes.NonGCPPrivateEndpointType {
				// Non-GCP network endpoints don't have associated nodes.
				networkEndpoint.Node = ""
			}
			zoneNetworkEndpointMap[zone].Insert(networkEndpoint)

			// if existing name is alphabetically lower than current one, continue and don't replace
			if existingPod, contains := networkEndpointPodMap[networkEndpoint]; contains {
				localEPCount[negtypes.Duplicate] += 1
				if existingPod.Name < endpointAddress.TargetRef.Name {
					neLogger.Info("Found duplicate endpoints when processing endpoint slice, save the pod information from the alphabetically higher pod", "ignoredPod", endpointAddress.TargetRef.Name, "usePod", existingPod.Name)
					continue // if existing name is alphabetically lower than current one, continue and don't replace
				}
			}
			networkEndpointPodMap[networkEndpoint] = types.NamespacedName{Namespace: endpointAddress.TargetRef.Namespace, Name: endpointAddress.TargetRef.Name}
		}
		mergeWithGlobalCounts(localEPCount, globalEPCount, globalEPSCount)
	}

	return ZoneNetworkEndpointMapResult{
		NetworkEndpointSet: zoneNetworkEndpointMap,
		EndpointPodMap:     networkEndpointPodMap,
		EPCount:            globalEPCount,
		EPSCount:           globalEPSCount,
	}
}

// validatePod checks if this pod is a valid pod resource
// it returns error if the pod:
// 1. is in terminal state
// 2. corresponds to a non-existent node
// 3. have an IP that matches to a podIP, but is outside of the node's allocated IP range
// 4. has labels not matching to its service's label selector
func validatePod(pod *apiv1.Pod, nodeLister, serviceLister cache.Indexer, networkEndpoint negtypes.NetworkEndpoint, serviceName string, isCustomEPS bool, logger klog.Logger) (negtypes.StateCountMap, error) {
	count := make(negtypes.StateCountMap)
	// Terminal Pod means a pod is in PodFailed or PodSucceeded phase
	phase := pod.Status.Phase
	if phase == apiv1.PodFailed || phase == apiv1.PodSucceeded {
		count[negtypes.PodTerminal]++
		return count, negtypes.ErrEPPodTerminal
	}
	obj, exists, err := nodeLister.GetByKey(pod.Spec.NodeName)
	if err != nil || !exists {
		count[negtypes.NodeNotFound]++
		return count, negtypes.ErrEPNodeNotFound
	}
	node, ok := obj.(*apiv1.Node)
	if !ok {
		count[negtypes.OtherError]++
		return count, negtypes.ErrEPNodeTypeAssertionFailed
	}
	if err = nodeContainsPodIP(node, networkEndpoint); err != nil {
		count[negtypes.IPOutOfPodCIDR]++
		return count, err
	}
	service := getService(serviceLister, pod.ObjectMeta.Namespace, serviceName, logger)
	if service == nil {
		count[negtypes.OtherError]++
		return count, negtypes.ErrEPServiceNotFound
	}
	if isCustomEPS {
		return count, nil
	}
	if err = podBelongsToService(pod, service); err != nil {
		count[negtypes.PodLabelMismatch]++
		return count, err
	}
	return count, nil
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

// podContainsEndpointAddress checks if the given endpoint's IP
// matches one of the pod's IPs, and return if it doesn't.
// If this is a dual stack endpoint, we would validate both IPs.
func podContainsEndpointAddress(networkEndpoint negtypes.NetworkEndpoint, pod *apiv1.Pod) error {
	// TODO(cheungdavid): update ipv4 check when single stack ipv6 is supported
	endpointIPs := []string{networkEndpoint.IP}
	if networkEndpoint.IPv6 != "" {
		endpointIPs = append(endpointIPs, networkEndpoint.IPv6)
	}

	matching := 0
	for _, endpointIP := range endpointIPs {
		// a pod can have at most two PodIPs, one for ipv4 and one for ipv6
		for _, podIP := range pod.Status.PodIPs {
			if endpointIP == podIP.IP {
				matching += 1
			}
		}
	}
	if matching != len(endpointIPs) {
		return fmt.Errorf("%w: endpoint has at least one IP %v that does not match its pod's IP(s) %v", negtypes.ErrEPIPNotFromPod, endpointIPs, pod.Status.PodIPs)
	}
	return nil
}

// nodeContainsPodIP checks the node's existing PodCIDR(s),
// and return error if the pod IP used by the endpoint is not within one of the podCIDR ranges.
// If this is a dual stack endpoint, we would validate both pod IPs
func nodeContainsPodIP(node *apiv1.Node, networkEndpoint negtypes.NetworkEndpoint) error {
	ipnets := []*net.IPNet{}
	// a node can have at most two PodCIDRs, one for ipv4 and one for ipv6
	for _, podCIDR := range node.Spec.PodCIDRs {
		podCIDR = strings.TrimSpace(podCIDR)
		_, ipnet, err := net.ParseCIDR(podCIDR)
		if err != nil {
			// swallow errors for CIDRs that are invalid
			metrics.PublishNegControllerErrorCountMetrics(err, true)
			continue
		}
		ipnets = append(ipnets, ipnet)
	}
	podIPs := []net.IP{net.ParseIP(networkEndpoint.IP)}
	if networkEndpoint.IPv6 != "" {
		podIPs = append(podIPs, net.ParseIP(networkEndpoint.IPv6))
	}

	matching := 0
	for _, podIP := range podIPs {
		for _, net := range ipnets {
			if net.Contains(podIP) {
				matching += 1
				break
			}
		}
	}
	if matching != len(podIPs) {
		return fmt.Errorf("%w: podIP(s) used by endpoint %v not match to the node's PodCIDR range(s)", negtypes.ErrEPIPOutOfPodCIDR, podIPs)
	}
	return nil
}

// podBelongsToService checks the pod's labels
// and return error if any label specified in the service's label selector is not in the pod's labels
func podBelongsToService(pod *apiv1.Pod, service *apiv1.Service) error {
	podLabels := pod.ObjectMeta.Labels
	serviceLabels := service.Spec.Selector
	for key, val1 := range serviceLabels {
		if val2, contains := podLabels[key]; !contains || val1 != val2 {
			return fmt.Errorf("%w: pod %s/%s has labels not match to its service %s/%s's label selector", negtypes.ErrEPPodLabelMismatch, pod.Namespace, pod.Name, service.Namespace, service.Name)
		}
	}
	return nil
}

// retrieveExistingZoneNetworkEndpointMap lists existing network endpoints in the neg and return the zone and endpoints map
func retrieveExistingZoneNetworkEndpointMap(negName string, zoneGetter *zonegetter.ZoneGetter, cloud negtypes.NetworkEndpointGroupCloud, version meta.Version, mode negtypes.EndpointsCalculatorMode, enableDualStackNEG bool, logger klog.Logger) (map[string]negtypes.NetworkEndpointSet, labels.EndpointPodLabelMap, error) {
	// Include zones that have non-candidate nodes currently. It is possible that NEGs were created in those zones previously and the endpoints now became non-candidates.
	// Endpoints in those NEGs now need to be removed. This mostly applies to VM_IP_NEGs where the endpoints are nodes.
	zones, err := zoneGetter.ListZones(zonegetter.AllNodesFilter, logger)
	if err != nil {
		return nil, nil, err
	}

	candidateNodeZones, err := zoneGetter.ListZones(negtypes.NodeFilterForEndpointCalculatorMode(mode), logger)
	if err != nil {
		return nil, nil, err
	}
	candidateZonesMap := sets.NewString(candidateNodeZones...)

	zoneNetworkEndpointMap := map[string]negtypes.NetworkEndpointSet{}
	endpointPodLabelMap := labels.EndpointPodLabelMap{}
	for _, zone := range zones {
		networkEndpointsWithHealthStatus, err := cloud.ListNetworkEndpoints(negName, zone, false, version, logger)
		if err != nil {
			// It is possible for a NEG to be missing in a zone without candidate nodes. Log and ignore this error.
			// NEG not found in a candidate zone is an error.
			if utils.IsNotFoundError(err) && !candidateZonesMap.Has(zone) {
				logger.Info("Ignoring NotFound error for NEG", "negName", negName, "zone", zone)
				metrics.PublishNegControllerErrorCountMetrics(err, true)
				continue
			}
			return nil, nil, fmt.Errorf("Failed to lookup NEG in zone %q, candidate zones %v, err - %w", zone, candidateZonesMap, err)
		}
		zoneNetworkEndpointMap[zone] = negtypes.NewNetworkEndpointSet()
		for _, ne := range networkEndpointsWithHealthStatus {
			newNE := negtypes.NetworkEndpoint{IP: ne.NetworkEndpoint.IpAddress, Node: ne.NetworkEndpoint.Instance}
			if ne.NetworkEndpoint.Port != 0 {
				newNE.Port = strconv.FormatInt(ne.NetworkEndpoint.Port, 10)
			}
			if enableDualStackNEG {
				newNE.IPv6 = ne.NetworkEndpoint.Ipv6Address
			}
			zoneNetworkEndpointMap[zone].Insert(newNE)
			endpointPodLabelMap[newNE] = ne.NetworkEndpoint.Annotations
		}
	}
	return zoneNetworkEndpointMap, endpointPodLabelMap, nil
}

// makeEndpointBatch return a batch of endpoint from the input and remove the endpoints from input set
// The return map has the encoded endpoint as key and GCE network endpoint object as value
func makeEndpointBatch(endpoints negtypes.NetworkEndpointSet, negType negtypes.NetworkEndpointType, endpointPodLabelMap labels.EndpointPodLabelMap, logger klog.Logger) (map[negtypes.NetworkEndpoint]*composite.NetworkEndpoint, error) {
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
				Instance:    networkEndpoint.Node,
				IpAddress:   networkEndpoint.IP,
				Ipv6Address: networkEndpoint.IPv6,
				Port:        int64(portNum),
			}
			if flags.F.EnableNEGLabelPropagation {
				annotations, ok := endpointPodLabelMap[networkEndpoint]
				if !ok {
					logger.Info("Can not find annotations for endpoint from endpointPodLabelMap", "endpoint", networkEndpoint, "endpointPodLabelMap", endpointPodLabelMap)
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
