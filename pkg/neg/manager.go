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
	"reflect"
	"sync"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/neg/readiness"
	negsyncer "k8s.io/ingress-gce/pkg/neg/syncers"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/klog"
)

type serviceKey struct {
	namespace string
	name      string
}

func (k serviceKey) Key() string {
	return fmt.Sprintf("%s/%s", k.namespace, k.name)
}

// syncerManager contains all the active syncer goroutines and manage their lifecycle.
type syncerManager struct {
	namer      negtypes.NetworkEndpointGroupNamer
	recorder   record.EventRecorder
	cloud      negtypes.NetworkEndpointGroupCloud
	zoneGetter negtypes.ZoneGetter

	nodeLister     cache.Indexer
	podLister      cache.Indexer
	serviceLister  cache.Indexer
	endpointLister cache.Indexer

	// TODO: lock per service instead of global lock
	mu sync.Mutex
	// svcPortMap is the canonical indicator for whether a service needs NEG.
	// key consists of service namespace and name. Value is a map of ServicePort
	// Port:TargetPort, which represents ports that require NEG
	svcPortMap map[serviceKey]negtypes.PortInfoMap
	// syncerMap stores the NEG syncer
	// key consists of service namespace, name and targetPort. Value is the corresponding syncer.
	syncerMap map[negtypes.NegSyncerKey]negtypes.NegSyncer
	// reflector handles NEG readiness gate and conditions for pods in NEG.
	reflector readiness.Reflector
}

func newSyncerManager(namer negtypes.NetworkEndpointGroupNamer, recorder record.EventRecorder, cloud negtypes.NetworkEndpointGroupCloud, zoneGetter negtypes.ZoneGetter, podLister, serviceLister, endpointLister, nodeLister cache.Indexer) *syncerManager {
	return &syncerManager{
		namer:          namer,
		recorder:       recorder,
		cloud:          cloud,
		zoneGetter:     zoneGetter,
		nodeLister:     nodeLister,
		podLister:      podLister,
		serviceLister:  serviceLister,
		endpointLister: endpointLister,
		svcPortMap:     make(map[serviceKey]negtypes.PortInfoMap),
		syncerMap:      make(map[negtypes.NegSyncerKey]negtypes.NegSyncer),
	}
}

// EnsureSyncer starts and stops syncers based on the input service ports.
func (manager *syncerManager) EnsureSyncers(namespace, name string, newPorts negtypes.PortInfoMap) error {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	key := getServiceKey(namespace, name)
	currentPorts, ok := manager.svcPortMap[key]
	if !ok {
		currentPorts = make(negtypes.PortInfoMap)
	}

	removes := currentPorts.Difference(newPorts)
	adds := newPorts.Difference(currentPorts)
	// There may be duplicate ports in adds and removes due to difference in readinessGate flag
	// Service/Ingress config changes can cause readinessGate to be turn on or off for the same service port.
	// By removing the duplicate ports in removes and adds, this prevents disruption of NEG syncer due to the config changes
	// Hence, Existing NEG syncer for the service port will always work
	removeCommonPorts(adds, removes)

	manager.svcPortMap[key] = newPorts
	klog.V(3).Infof("EnsureSyncer %v/%v: syncing %v ports, removing %v ports, adding %v ports", namespace, name, newPorts, removes, adds)

	for svcPort, portInfo := range removes {
		syncer, ok := manager.syncerMap[getSyncerKey(namespace, name, svcPort, portInfo)]
		if ok {
			syncer.Stop()
		}
	}

	errList := []error{}
	// Ensure a syncer is running for each port that is being added.
	for svcPort, portInfo := range adds {
		syncerKey := getSyncerKey(namespace, name, svcPort, portInfo)
		syncer, ok := manager.syncerMap[syncerKey]
		if !ok {
			// determine the implementation that calculates NEG endpoints on each sync.
			epc := negsyncer.GetEndpointsCalculator(manager.nodeLister, manager.podLister, manager.zoneGetter,
				syncerKey, portInfo.RandomizeEndpoints)
			syncer = negsyncer.NewTransactionSyncer(
				syncerKey,
				portInfo.NegName,
				manager.recorder,
				manager.cloud,
				manager.zoneGetter,
				manager.podLister,
				manager.serviceLister,
				manager.endpointLister,
				manager.nodeLister,
				manager.reflector,
				epc,
			)
			manager.syncerMap[syncerKey] = syncer
		}

		if syncer.IsStopped() {
			if err := syncer.Start(); err != nil {
				errList = append(errList, err)
			}
		}
	}
	return utilerrors.NewAggregate(errList)
}

// StopSyncer stops all syncers for the input service.
func (manager *syncerManager) StopSyncer(namespace, name string) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	key := getServiceKey(namespace, name)
	if ports, ok := manager.svcPortMap[key]; ok {
		for svcPort, portInfo := range ports {
			if syncer, ok := manager.syncerMap[getSyncerKey(namespace, name, svcPort, portInfo)]; ok {
				syncer.Stop()
			}
		}
		delete(manager.svcPortMap, key)
	}
	return
}

// Sync signals all syncers related to the service to sync.
func (manager *syncerManager) Sync(namespace, name string) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	key := getServiceKey(namespace, name)
	if portInfoMap, ok := manager.svcPortMap[key]; ok {
		for svcPort, portInfo := range portInfoMap {
			if syncer, ok := manager.syncerMap[getSyncerKey(namespace, name, svcPort, portInfo)]; ok {
				if !syncer.IsStopped() {
					syncer.Sync()
				}
			}
		}
	}
}

// SyncNodes signals all GCE_VM_PRIMARY_IP syncers to sync.
// Only these use nodes selected at random as endpoints and hence need to sync upon node updates.
func (manager *syncerManager) SyncNodes() {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	for key, syncer := range manager.syncerMap {
		if key.NegType == negtypes.VmPrimaryIpEndpointType && !syncer.IsStopped() {
			syncer.Sync()
		}
	}
}

// ShutDown signals all syncers to stop
func (manager *syncerManager) ShutDown() {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	for _, s := range manager.syncerMap {
		s.Stop()
	}
}

// GC garbage collects syncers and NEGs.
func (manager *syncerManager) GC() error {
	klog.V(2).Infof("Start NEG garbage collection.")
	defer klog.V(2).Infof("NEG garbage collection finished.")
	// Garbage collect Syncers
	manager.garbageCollectSyncer()

	// Garbage collect NEGs
	if err := manager.garbageCollectNEG(); err != nil {
		return fmt.Errorf("failed to garbage collect negs: %v", err)
	}
	return nil
}

// ReadinessGateEnabledNegs returns a list of NEGs which has readiness gate enabled for the input pod's namespace and labels.
func (manager *syncerManager) ReadinessGateEnabledNegs(namespace string, podLabels map[string]string) []string {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	ret := sets.NewString()
	for svcKey, portMap := range manager.svcPortMap {
		if svcKey.namespace != namespace {
			continue
		}

		obj, exists, err := manager.serviceLister.GetByKey(svcKey.Key())
		if err != nil {
			klog.Errorf("Failed to retrieve service %s from store: %v", svcKey.Key(), err)
			continue
		}

		if !exists {
			continue
		}

		service := obj.(*v1.Service)

		if service.Spec.Selector == nil {
			// services with nil selectors match nothing, not everything.
			continue
		}

		selector := labels.Set(service.Spec.Selector).AsSelectorPreValidated()
		if selector.Matches(labels.Set(podLabels)) {
			ret = ret.Union(portMap.NegsWithReadinessGate())
		}
	}
	return ret.List()
}

// ReadinessGateEnabled returns true if the NEG requires readiness feedback
func (manager *syncerManager) ReadinessGateEnabled(syncerKey negtypes.NegSyncerKey) bool {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if v, ok := manager.svcPortMap[serviceKey{namespace: syncerKey.Namespace, name: syncerKey.Name}]; ok {
		if info, ok := v[negtypes.PortInfoMapKey{ServicePort: syncerKey.PortTuple.Port, Subset: syncerKey.Subset}]; ok {
			return info.ReadinessGate
		}
	}
	return false
}

// garbageCollectSyncer removes stopped syncer from syncerMap
func (manager *syncerManager) garbageCollectSyncer() {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	for key, syncer := range manager.syncerMap {
		if syncer.IsStopped() && !syncer.IsShuttingDown() {
			delete(manager.syncerMap, key)
		}
	}
}

func (manager *syncerManager) garbageCollectNEG() error {
	// Retrieve aggregated NEG list from cloud
	// Compare against svcPortMap and Remove unintended NEGs by best effort
	negList, err := manager.cloud.AggregatedListNetworkEndpointGroup(meta.VersionGA)
	if err != nil {
		return fmt.Errorf("failed to retrieve aggregated NEG list: %v", err)
	}

	deleteCandidates := map[string][]string{}
	for key, neg := range negList {
		if key.Type() != meta.Zonal {
			// covers the case when key.Zone is not populated
			klog.V(4).Infof("Ignoring key %v as it is not zonal", key)
			continue
		}
		if manager.namer.IsNEG(neg.Name) {
			if _, ok := deleteCandidates[neg.Name]; !ok {
				deleteCandidates[neg.Name] = []string{}
			}
			deleteCandidates[neg.Name] = append(deleteCandidates[neg.Name], key.Zone)
		}
	}

	func() {
		manager.mu.Lock()
		defer manager.mu.Unlock()
		for _, portInfoMap := range manager.svcPortMap {
			for _, portInfo := range portInfoMap {
				delete(deleteCandidates, portInfo.NegName)
			}
		}
	}()

	// This section includes a potential race condition between deleting neg here and users adds the neg annotation.
	// The worst outcome of the race condition is that neg is deleted in the end but user actually specifies a neg.
	// This would be resolved (sync neg) when the next endpoint update or resync arrives.
	// TODO: avoid race condition here
	for name, zones := range deleteCandidates {
		for _, zone := range zones {
			if err := manager.ensureDeleteNetworkEndpointGroup(name, zone); err != nil {
				return fmt.Errorf("failed to delete NEG %q in %q: %v", name, zone, err)
			}
		}
	}
	return nil
}

// ensureDeleteNetworkEndpointGroup ensures neg is delete from zone
func (manager *syncerManager) ensureDeleteNetworkEndpointGroup(name, zone string) error {
	_, err := manager.cloud.GetNetworkEndpointGroup(name, zone, meta.VersionGA)
	if err != nil {
		// Assume error is caused by not existing
		return nil
	}
	klog.V(2).Infof("Deleting NEG %q in %q.", name, zone)
	return manager.cloud.DeleteNetworkEndpointGroup(name, zone, meta.VersionGA)
}

// getSyncerKey encodes a service namespace, name, service port and targetPort into a string key
func getSyncerKey(namespace, name string, servicePortKey negtypes.PortInfoMapKey, portInfo negtypes.PortInfo) negtypes.NegSyncerKey {
	networkEndpointType := negtypes.VmIpPortEndpointType
	if flags.F.EnableNonGCPMode {
		networkEndpointType = negtypes.NonGCPPrivateEndpointType
	}
	if portInfo.PortTuple.Empty() {
		networkEndpointType = negtypes.VmPrimaryIpEndpointType
	}

	return negtypes.NegSyncerKey{
		Namespace:    namespace,
		Name:         name,
		PortTuple:    portInfo.PortTuple,
		Subset:       servicePortKey.Subset,
		SubsetLabels: portInfo.SubsetLabels,
		NegType:      networkEndpointType,
	}
}

func getServiceKey(namespace, name string) serviceKey {
	return serviceKey{
		namespace: namespace,
		name:      name,
	}
}

// removeCommonPorts removes duplicate ports in p1 and p2 if the corresponding port info is converted to the same syncerKey.
// When both ports can be converted to the same syncerKey, that means the underlying NEG syncer and NEG configuration is exactly the same.
// For example, this function effectively removes duplicate port with different readiness gate flag if the rest of the field in port info is the same.
func removeCommonPorts(p1, p2 negtypes.PortInfoMap) {
	for port, portInfo1 := range p1 {
		portInfo2, ok := p2[port]
		if !ok {
			continue
		}

		syncerKey1 := getSyncerKey("", "", port, portInfo1)
		syncerKey2 := getSyncerKey("", "", port, portInfo2)
		if reflect.DeepEqual(syncerKey1, syncerKey2) {
			delete(p1, port)
			delete(p2, port)
		}
	}
}
