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
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	negv1beta1 "k8s.io/ingress-gce/pkg/apis/svcneg/v1beta1"
	"k8s.io/ingress-gce/pkg/composite"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog"
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
			klog.Errorf("Error while retriving %q in zone %q: %v after initialization", negName, zone, err)
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

// toZoneNetworkEndpointMap translates addresses in endpoints object and Istio:DestinationRule subset into zone and endpoints map
func toZoneNetworkEndpointMap(eds []negtypes.EndpointsData, zoneGetter negtypes.ZoneGetter, servicePortName string, podLister cache.Indexer, subsetLabels string, networkEndpointType negtypes.NetworkEndpointType) (map[string]negtypes.NetworkEndpointSet, negtypes.EndpointPodMap, error) {
	zoneNetworkEndpointMap := map[string]negtypes.NetworkEndpointSet{}
	networkEndpointPodMap := negtypes.EndpointPodMap{}
	if eds == nil {
		klog.Errorf("Endpoint object is nil")
		return zoneNetworkEndpointMap, networkEndpointPodMap, nil
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

		// processAddressFunc adds the qualified endpoints from the input list into the endpointSet group by zone
		for _, endpointAddress := range ed.Addresses {
			// Apply the selector if Istio:DestinationRule subset labels provided.
			if subsetLabels != "" {
				if endpointAddress.TargetRef == nil || endpointAddress.TargetRef.Kind != "Pod" {
					klog.V(2).Infof("Endpoint %q in Endpoints %s/%s does not have a Pod as the TargetRef object. Skipping", endpointAddress.Addresses, ed.Meta.Namespace, ed.Meta.Name)
					continue
				}
				// Skip if the endpoint's pod not matching the subset lables.
				if !shouldPodBeInDestinationRuleSubset(podLister, endpointAddress.TargetRef.Namespace, endpointAddress.TargetRef.Name, subsetLabels) {
					continue
				}
			}
			if endpointAddress.NodeName == nil {
				klog.V(2).Infof("Endpoint %q in Endpoints %s/%s does not have an associated node. Skipping", endpointAddress.Addresses, ed.Meta.Namespace, ed.Meta.Name)
				continue
			}
			if endpointAddress.TargetRef == nil {
				klog.V(2).Infof("Endpoint %q in Endpoints %s/%s does not have an associated pod. Skipping", endpointAddress.Addresses, ed.Meta.Namespace, ed.Meta.Name)
				continue
			}
			zone, err := zoneGetter.GetZoneForNode(*endpointAddress.NodeName)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to retrieve associated zone of node %q: %w", *endpointAddress.NodeName, err)
			}
			if zoneNetworkEndpointMap[zone] == nil {
				zoneNetworkEndpointMap[zone] = negtypes.NewNetworkEndpointSet()
			}

			// TODO: This check and EndpoinsData.Ready field may be deleted once Endpoints support is removed.
			// The purpose of this check is to handle endpoints in terminating state.
			// The Endpoints API doesn't have terminating field. Terminating endpoints are marked as not ready.
			// This check support this case. For not ready endpoints it checks if the endpoint is not yet ready or terminating.
			// The EndpointSlices API has terminating field which solves this problem.
			if endpointAddress.Ready || shouldPodBeInNeg(podLister, endpointAddress.TargetRef.Namespace, endpointAddress.TargetRef.Name) {
				for _, address := range endpointAddress.Addresses {
					networkEndpoint := negtypes.NetworkEndpoint{IP: address, Port: matchPort, Node: *endpointAddress.NodeName}
					if networkEndpointType == negtypes.NonGCPPrivateEndpointType {
						// Non-GCP network endpoints don't have associated nodes.
						networkEndpoint.Node = ""
					}
					zoneNetworkEndpointMap[zone].Insert(networkEndpoint)
					networkEndpointPodMap[networkEndpoint] = types.NamespacedName{Namespace: endpointAddress.TargetRef.Namespace, Name: endpointAddress.TargetRef.Name}
				}
			}
		}
	}
	if !foundMatchingPort {
		klog.Errorf("Service port name %q was not found in the endpoints object %+v", servicePortName, eds)
	}

	if len(zoneNetworkEndpointMap) == 0 || len(networkEndpointPodMap) == 0 {
		klog.V(3).Infof("Generated empty endpoint maps (zoneNetworkEndpointMap: %+v, networkEndpointPodMap: %v) from Endpoints object: %+v", zoneNetworkEndpointMap, networkEndpointPodMap, eds)
	}
	return zoneNetworkEndpointMap, networkEndpointPodMap, nil
}

// retrieveExistingZoneNetworkEndpointMap lists existing network endpoints in the neg and return the zone and endpoints map
func retrieveExistingZoneNetworkEndpointMap(negName string, zoneGetter negtypes.ZoneGetter, cloud negtypes.NetworkEndpointGroupCloud, version meta.Version) (map[string]negtypes.NetworkEndpointSet, error) {
	zones, err := zoneGetter.ListZones()
	if err != nil {
		return nil, err
	}

	zoneNetworkEndpointMap := map[string]negtypes.NetworkEndpointSet{}
	for _, zone := range zones {
		zoneNetworkEndpointMap[zone] = negtypes.NewNetworkEndpointSet()
		networkEndpointsWithHealthStatus, err := cloud.ListNetworkEndpoints(negName, zone, false, version)
		if err != nil {
			return nil, err
		}
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

func keyFunc(namespace, name string) string {
	return fmt.Sprintf("%s/%s", namespace, name)
}

// shouldPodBeInNeg returns true if pod is not in graceful termination state
func shouldPodBeInNeg(podLister cache.Indexer, namespace, name string) bool {
	if podLister == nil {
		return false
	}
	key := keyFunc(namespace, name)
	obj, exists, err := podLister.GetByKey(key)
	if err != nil {
		klog.Errorf("Failed to retrieve pod %s from pod lister: %v", key, err)
		return false
	}
	if !exists {
		return false
	}
	pod, ok := obj.(*v1.Pod)
	if !ok {
		klog.Errorf("Failed to convert obj %s to v1.Pod. The object type is %T", key, obj)
		return false
	}

	// if pod has DeletionTimestamp, that means pod is in graceful termination state.
	if pod.DeletionTimestamp != nil {
		return false
	}
	return true
}

// shouldPodBeInDestinationRuleSubset return ture if pod match the DestinationRule subset lables.
func shouldPodBeInDestinationRuleSubset(podLister cache.Indexer, namespace, name string, subsetLabels string) bool {
	if podLister == nil {
		return false
	}
	key := keyFunc(namespace, name)
	obj, exists, err := podLister.GetByKey(key)
	if err != nil {
		klog.Errorf("Failed to retrieve pod %s from pod lister: %v", key, err)
		return false
	}
	if !exists {
		return false
	}
	pod, ok := obj.(*v1.Pod)
	if !ok {
		klog.Errorf("Failed to convert obj %s to v1.Pod. The object type is %T", key, obj)
		return false
	}

	selector, err := labels.Parse(subsetLabels)
	if err != nil {
		klog.Errorf("Failed to parse the subset selectors.")
		return false
	}
	return selector.Matches(labels.Set(pod.Labels))
}
