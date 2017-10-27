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

package networkendpointgroup

import (
	"fmt"
	"strings"
	"sync"

	"github.com/golang/glog"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
)

type serviceKey struct {
	namespace string
	name      string
}

// syncerManager contains all the active syncer goroutines and manage their lifecycle.
type syncerManager struct {
	namer      networkEndpointGroupNamer
	recorder   record.EventRecorder
	cloud      networkEndpointGroupCloud
	zoneGetter zoneGetter

	serviceLister  cache.Indexer
	endpointLister cache.Indexer

	// TODO: lock per service instead of global lock
	mu sync.Mutex
	// svcPortMap is the canonical indicator for whether a service needs NEG.
	// key consists of service namespace and name. Value is the list of target ports that requires NEG
	svcPortMap map[serviceKey]sets.String
	// syncerMap stores the NEG syncer
	// key consists of service namespace, name and targetPort. Value is the corresponding syncer.
	syncerMap map[servicePort]negSyncer
}

func newSyncerManager(namer networkEndpointGroupNamer, recorder record.EventRecorder, cloud networkEndpointGroupCloud, zoneGetter zoneGetter, serviceLister cache.Indexer, endpointLister cache.Indexer) *syncerManager {
	return &syncerManager{
		namer:          namer,
		recorder:       recorder,
		cloud:          cloud,
		zoneGetter:     zoneGetter,
		serviceLister:  serviceLister,
		endpointLister: endpointLister,
		svcPortMap:     make(map[serviceKey]sets.String),
		syncerMap:      make(map[servicePort]negSyncer),
	}
}

// EnsureSyncer starts and stops syncers based on the input service ports.
func (manager *syncerManager) EnsureSyncers(namespace, name string, targetPorts sets.String) error {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	key := getServiceKey(namespace, name)
	currentPorts, ok := manager.svcPortMap[key]
	if !ok {
		currentPorts = sets.NewString()
	}

	removes := currentPorts.Difference(targetPorts).List()
	adds := targetPorts.Difference(currentPorts).List()
	manager.svcPortMap[key] = targetPorts
	glog.V(3).Infof("EnsureSyncer %v/%v: removing %v ports, adding %v ports", namespace, name, removes, adds)

	// Stop syncer for removed ports
	for _, port := range removes {
		syncer, ok := manager.syncerMap[getSyncerKey(namespace, name, port)]
		if ok {
			syncer.Stop()
		}
	}

	errList := []error{}
	// Ensure a syncer is running for each port that is being added.
	for _, port := range adds {
		syncer, ok := manager.syncerMap[getSyncerKey(namespace, name, port)]
		if !ok {
			syncer = newSyncer(
				servicePort{
					namespace:  namespace,
					name:       name,
					targetPort: port,
				},
				manager.namer.NEGName(namespace, name, port),
				manager.recorder,
				manager.cloud,
				manager.zoneGetter,
				manager.serviceLister,
				manager.endpointLister,
			)
			manager.syncerMap[getSyncerKey(namespace, name, port)] = syncer
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
		for _, port := range ports.List() {
			if syncer, ok := manager.syncerMap[getSyncerKey(namespace, name, port)]; ok {
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
	if portList, ok := manager.svcPortMap[key]; ok {
		for _, port := range portList.List() {
			if syncer, ok := manager.syncerMap[getSyncerKey(namespace, name, port)]; ok {
				if !syncer.IsStopped() {
					syncer.Sync()
				}
			}
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
	glog.V(2).Infof("Start NEG garbage collection.")
	defer glog.V(2).Infof("NEG garbage collection finished.")
	for _, key := range manager.getAllStoppedSyncerKeys() {
		manager.garbageCollectSyncer(key)
	}

	// Garbage collect NEGs
	if err := manager.garbageCollectNEG(); err != nil {
		return fmt.Errorf("Failed to garbage collect negs: %v", err)
	}
	return nil
}

func (manager *syncerManager) garbageCollectSyncer(key servicePort) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.syncerMap[key].IsStopped() && !manager.syncerMap[key].IsShuttingDown() {
		delete(manager.syncerMap, key)
	}
}

func (manager *syncerManager) getAllStoppedSyncerKeys() []servicePort {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	ret := []servicePort{}
	for key, syncer := range manager.syncerMap {
		if syncer.IsStopped() {
			ret = append(ret, key)
		}
	}
	return ret
}

func (manager *syncerManager) garbageCollectNEG() error {
	// Retrieve aggregated NEG list from cloud
	// Compare against svcPortMap and Remove unintended NEGs by best effort
	zoneNEGList, err := manager.cloud.AggregatedListNetworkEndpointGroup()
	if err != nil {
		return fmt.Errorf("failed to retrieve aggregated NEG list: %v", err)
	}

	negNames := sets.String{}
	for _, list := range zoneNEGList {
		for _, neg := range list {
			if strings.HasPrefix(neg.Name, manager.namer.NEGPrefix()) {
				negNames.Insert(neg.Name)
			}
		}
	}

	func() {
		manager.mu.Lock()
		defer manager.mu.Unlock()
		for key, ports := range manager.svcPortMap {
			for _, port := range ports.List() {
				name := manager.namer.NEGName(key.namespace, key.name, port)
				negNames.Delete(name)
			}
		}
	}()

	// This section includes a potential race condition between deleting neg here and users adds the neg annotation.
	// The worst outcome of the race condition is that neg is deleted in the end but user actually specifies a neg.
	// This would be resolved (sync neg) when the next endpoint update or resync arrives.
	// TODO: avoid race condition here
	for zone := range zoneNEGList {
		for _, name := range negNames.List() {
			if err := manager.ensureDeleteNetworkEndpointGroup(name, zone); err != nil {
				return fmt.Errorf("failed to delete NEG %q in %q: %v", name, zone, err)
			}
		}
	}
	return nil
}

// ensureDeleteNetworkEndpointGroup ensures neg is delete from zone
func (manager *syncerManager) ensureDeleteNetworkEndpointGroup(name, zone string) error {
	_, err := manager.cloud.GetNetworkEndpointGroup(name, zone)
	if err != nil {
		// Assume error is caused by not existing
		return nil
	}
	glog.V(2).Infof("Deleting NEG %q in %q.", name, zone)
	return manager.cloud.DeleteNetworkEndpointGroup(name, zone)
}

// getSyncerKey encodes a service namespace, name and targetPort into a string key
func getSyncerKey(namespace, name, port string) servicePort {
	return servicePort{
		namespace:  namespace,
		name:       name,
		targetPort: port,
	}
}

func getServiceKey(namespace, name string) serviceKey {
	return serviceKey{
		namespace: namespace,
		name:      name,
	}
}
