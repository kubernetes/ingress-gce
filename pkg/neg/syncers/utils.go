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
	"strings"
	"time"

	"github.com/golang/glog"
	"google.golang.org/api/compute/v0.beta"
	apiv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/kubernetes/pkg/cloudprovider/providers/gce"
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

// NegSyncerKey includes information to uniquely identify a NEG
type NegSyncerKey struct {
	Namespace  string
	Name       string
	Port       int32
	TargetPort string
}

func (key NegSyncerKey) String() string {
	return fmt.Sprintf("%s/%s-%v/%s", key.Namespace, key.Name, key.Port, key.TargetPort)
}

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
		glog.Errorf("Failed to retrieve service %s/%s from store: %v", namespace, name, err)
	}
	return nil
}

// ensureNetworkEndpointGroup ensures corresponding NEG is configured correctly in the specified zone.
func ensureNetworkEndpointGroup(svcNamespace, svcName, negName, zone, negServicePortName string, cloud negtypes.NetworkEndpointGroupCloud, serviceLister cache.Indexer, recorder record.EventRecorder) error {
	neg, err := cloud.GetNetworkEndpointGroup(negName, zone)
	if err != nil {
		// Most likely to be caused by non-existed NEG
		glog.V(4).Infof("Error while retriving %q in zone %q: %v", negName, zone, err)
	}

	needToCreate := false
	if neg == nil {
		needToCreate = true
	} else if !utils.EqualResourceIDs(neg.LoadBalancer.Network, cloud.NetworkURL()) ||
		!utils.EqualResourceIDs(neg.LoadBalancer.Subnetwork, cloud.SubnetworkURL()) {
		needToCreate = true
		glog.V(2).Infof("NEG %q in %q does not match network and subnetwork of the cluster. Deleting NEG.", negName, zone)
		err = cloud.DeleteNetworkEndpointGroup(negName, zone)
		if err != nil {
			return err
		} else {
			if recorder != nil && serviceLister != nil {
				if svc := getService(serviceLister, svcNamespace, svcName); svc != nil {
					recorder.Eventf(svc, apiv1.EventTypeNormal, "Delete", "Deleted NEG %q for %s in %q.", negName, negServicePortName, zone)
				}
			}
		}
	}

	if needToCreate {
		glog.V(2).Infof("Creating NEG %q for %s in %q.", negName, negServicePortName, zone)
		err = cloud.CreateNetworkEndpointGroup(&compute.NetworkEndpointGroup{
			Name:                negName,
			NetworkEndpointType: gce.NEGIPPortNetworkEndpointType,
			LoadBalancer: &compute.NetworkEndpointGroupLbNetworkEndpointGroup{
				Network:    cloud.NetworkURL(),
				Subnetwork: cloud.SubnetworkURL(),
			},
		}, zone)
		if err != nil {
			return err
		} else {
			if recorder != nil && serviceLister != nil {
				if svc := getService(serviceLister, svcNamespace, svcName); svc != nil {
					recorder.Eventf(svc, apiv1.EventTypeNormal, "Create", "Created NEG %q for %s in %q.", negName, negServicePortName, zone)
				}
			}
		}
	}
	return nil
}
