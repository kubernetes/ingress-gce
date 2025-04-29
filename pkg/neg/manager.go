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
	"context"
	"fmt"
	"net/http"
	"reflect"
	"sync"
	"time"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	negv1beta1 "k8s.io/ingress-gce/pkg/apis/svcneg/v1beta1"
	"k8s.io/ingress-gce/pkg/neg/metrics"
	"k8s.io/ingress-gce/pkg/neg/metrics/metricscollector"
	"k8s.io/ingress-gce/pkg/neg/readiness"
	negsyncer "k8s.io/ingress-gce/pkg/neg/syncers"
	podlabels "k8s.io/ingress-gce/pkg/neg/syncers/labels"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	svcnegclient "k8s.io/ingress-gce/pkg/svcneg/client/clientset/versioned"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/common"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/ingress-gce/pkg/utils/patch"
	"k8s.io/ingress-gce/pkg/utils/zonegetter"
	"k8s.io/klog/v2"
	utilpointer "k8s.io/utils/pointer"
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
	l4Namer    namer.L4ResourcesNamer
	recorder   record.EventRecorder
	cloud      negtypes.NetworkEndpointGroupCloud
	zoneGetter *zonegetter.ZoneGetter

	nodeLister          cache.Indexer
	podLister           cache.Indexer
	serviceLister       cache.Indexer
	endpointSliceLister cache.Indexer
	svcNegLister        cache.Indexer

	// TODO: lock per service instead of global lock
	mu sync.Mutex
	// svcPortMap is the canonical indicator for whether a service needs NEG.
	// key consists of service namespace and name. Value is a map of ServicePort
	// Port:TargetPort, which represents ports that require NEG
	svcPortMap map[serviceKey]negtypes.PortInfoMap
	// syncerMap stores the NEG syncer
	// key consists of service namespace, name and targetPort. Value is the corresponding syncer.
	syncerMap map[negtypes.NegSyncerKey]negtypes.NegSyncer
	// syncCollector collect sync related metrics
	syncerMetrics *metricscollector.SyncerMetrics

	// reflector handles NEG readiness gate and conditions for pods in NEG.
	reflector readiness.Reflector
	//svcNegClient handles lifecycle operations for NEG CRs
	svcNegClient svcnegclient.Interface

	// kubeSystemUID is used to by syncers when NEG CRD is enabled
	kubeSystemUID types.UID

	// enableNonGcpMode indicates whether nonGcpMode have been enabled
	// This will make all NEGs created by NEG controller to be NON_GCP_PRIVATE_IP_PORT type.
	enableNonGcpMode bool

	// enableDualStackNEG indicates if IPv6 endpoints should be considered while
	// reconciling NEGs.
	enableDualStackNEG bool

	// Number of goroutines created for NEG garbage collection. This value
	// controls the maximum number of concurrent calls that can be made to the GCE
	// NEG Delete API.
	numGCWorkers int

	logger klog.Logger

	// zone maps keep track of the last set of zones the neg controller has seen
	// for their respective NEG types. zone maps are protected by the mu mutex.
	vmIpPortZoneMap map[string]struct{}

	// lpConfig configures the pod label to be propagated to NEG endpoints.
	lpConfig podlabels.PodLabelPropagationConfig
}

func newSyncerManager(namer negtypes.NetworkEndpointGroupNamer,
	l4Namer namer.L4ResourcesNamer,
	recorder record.EventRecorder,
	cloud negtypes.NetworkEndpointGroupCloud,
	zoneGetter *zonegetter.ZoneGetter,
	svcNegClient svcnegclient.Interface,
	kubeSystemUID types.UID,
	podLister cache.Indexer,
	serviceLister cache.Indexer,
	endpointSliceLister cache.Indexer,
	nodeLister cache.Indexer,
	svcNegLister cache.Indexer,
	syncerMetrics *metricscollector.SyncerMetrics,
	enableNonGcpMode bool,
	enableDualStackNEG bool,
	numGCWorkers int,
	lpConfig podlabels.PodLabelPropagationConfig,
	logger klog.Logger) *syncerManager {

	var vmIpPortZoneMap map[string]struct{}
	updateZoneMap(&vmIpPortZoneMap, negtypes.NodeFilterForNetworkEndpointType(negtypes.VmIpPortEndpointType), zoneGetter, logger)

	return &syncerManager{
		namer:               namer,
		l4Namer:             l4Namer,
		recorder:            recorder,
		cloud:               cloud,
		zoneGetter:          zoneGetter,
		nodeLister:          nodeLister,
		podLister:           podLister,
		serviceLister:       serviceLister,
		endpointSliceLister: endpointSliceLister,
		svcNegLister:        svcNegLister,
		svcPortMap:          make(map[serviceKey]negtypes.PortInfoMap),
		syncerMap:           make(map[negtypes.NegSyncerKey]negtypes.NegSyncer),
		syncerMetrics:       syncerMetrics,
		svcNegClient:        svcNegClient,
		kubeSystemUID:       kubeSystemUID,
		enableNonGcpMode:    enableNonGcpMode,
		enableDualStackNEG:  enableDualStackNEG,
		numGCWorkers:        numGCWorkers,
		logger:              logger,
		vmIpPortZoneMap:     vmIpPortZoneMap,
		lpConfig:            lpConfig,
	}
}

// EnsureSyncer starts and stops syncers based on the input service ports. Returns the number of
// syncers that were successfully created and the number of syncers that failed to be created
func (manager *syncerManager) EnsureSyncers(namespace, name string, newPorts negtypes.PortInfoMap) (int, int, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	start := time.Now()
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
	manager.removeCommonPorts(adds, removes)
	manager.svcPortMap[key] = newPorts
	manager.logger.V(3).Info("EnsureSyncer is syncing ports", "service", klog.KRef(namespace, name), "ports", fmt.Sprintf("%v", newPorts), "portsToRemove", fmt.Sprintf("%v", removes), "portsToAdd", fmt.Sprintf("%v", adds))

	errList := []error{}
	successfulSyncers := 0
	errorSyncers := 0
	for svcPort, portInfo := range removes {
		syncer, ok := manager.syncerMap[manager.getSyncerKey(namespace, name, svcPort, portInfo)]
		if ok {
			syncer.Stop()
		}

		err := manager.ensureDeleteSvcNegCR(namespace, portInfo.NegName)
		if err != nil {
			errList = append(errList, err)
		}
	}

	// Ensure a syncer is running for each port in newPorts.
	for svcPort, portInfo := range newPorts {
		syncerKey := manager.getSyncerKey(namespace, name, svcPort, portInfo)
		syncer, ok := manager.syncerMap[syncerKey]
		// To ensure that a NEG CR always exists during the lifecycle of a NEG, do not create a
		// syncer for the NEG until the NEG CR is successfully created. This will reduce the
		// possibility of invalid states and reduces complexity of garbage collection
		// To reduce the possibility of NEGs being leaked, ensure a SvcNeg CR exists for every
		// desired port.
		if err := manager.ensureSvcNegCR(key, portInfo); err != nil {
			errList = append(errList, fmt.Errorf("failed to ensure svc neg cr %s/%s/%d for port: %w ", namespace, portInfo.NegName, svcPort.ServicePort, err))
			errorSyncers += 1
			continue
		}
		if !ok {
			// determine the implementation that calculates NEG endpoints on each sync.
			epc := negsyncer.GetEndpointsCalculator(
				manager.podLister,
				manager.nodeLister,
				manager.serviceLister,
				manager.zoneGetter,
				syncerKey,
				portInfo.EpCalculatorMode,
				manager.logger.WithValues("service", klog.KRef(syncerKey.Namespace, syncerKey.Name), "negName", syncerKey.NegName),
				manager.enableDualStackNEG,
				manager.syncerMetrics,
				&portInfo.NetworkInfo,
				portInfo.L4LBType,
			)
			nonDefaultSubnetNEGNamer := manager.namer
			if syncerKey.NegType == negtypes.VmIpEndpointType {
				nonDefaultSubnetNEGNamer = manager.l4Namer
			}

			syncer = negsyncer.NewTransactionSyncer(
				syncerKey,
				manager.recorder,
				manager.cloud,
				manager.zoneGetter,
				manager.podLister,
				manager.serviceLister,
				manager.endpointSliceLister,
				manager.nodeLister,
				manager.svcNegLister,
				manager.reflector,
				epc,
				string(manager.kubeSystemUID),
				manager.svcNegClient,
				manager.syncerMetrics,
				syncerKey.NegType == negtypes.VmIpPortEndpointType && !manager.namer.IsNEG(portInfo.NegName),
				manager.logger,
				manager.lpConfig,
				manager.enableDualStackNEG,
				portInfo.NetworkInfo,
				nonDefaultSubnetNEGNamer,
			)
			manager.syncerMap[syncerKey] = syncer
		}

		if syncer.IsStopped() {
			if err := syncer.Start(); err != nil {
				errList = append(errList, err)
				errorSyncers += 1
				continue
			}
		}
		successfulSyncers += 1
	}
	err := utilerrors.NewAggregate(errList)
	metrics.PublishNegManagerProcessMetrics(metrics.SyncProcess, err, start)

	return successfulSyncers, errorSyncers, err
}

// StopSyncer stops all syncers for the input service.
func (manager *syncerManager) StopSyncer(namespace, name string) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	key := getServiceKey(namespace, name)
	if ports, ok := manager.svcPortMap[key]; ok {
		for svcPort, portInfo := range ports {
			if syncer, ok := manager.syncerMap[manager.getSyncerKey(namespace, name, svcPort, portInfo)]; ok {
				syncer.Stop()
			}
		}
		delete(manager.svcPortMap, key)
	}
}

// Sync signals all syncers related to the service to sync.
func (manager *syncerManager) Sync(namespace, name string) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	key := getServiceKey(namespace, name)
	if portInfoMap, ok := manager.svcPortMap[key]; ok {
		for svcPort, portInfo := range portInfoMap {
			if syncer, ok := manager.syncerMap[manager.getSyncerKey(namespace, name, svcPort, portInfo)]; ok {
				if !syncer.IsStopped() {
					syncer.Sync()
				}
			}
		}
	}
}

// SyncNodes signals all GCE_VM_IP syncers to sync.
// Only these use nodes selected at random as endpoints and hence need to sync upon node updates.
func (manager *syncerManager) SyncNodes() {
	manager.mu.Lock()
	defer manager.mu.Unlock()

	// When a zone change occurs (new zone is added or deleted), a sync should be triggered
	isVmIpPortZoneChange := updateZoneMap(&manager.vmIpPortZoneMap, negtypes.NodeFilterForNetworkEndpointType(negtypes.VmIpPortEndpointType), manager.zoneGetter, manager.logger)

	for key, syncer := range manager.syncerMap {
		manager.logger.V(1).Info("SyncNodes: Evaluating sync decision for syncer", "negSyncerKey", key.String())

		if syncer.IsStopped() {
			manager.logger.V(1).Info("SyncNodes: Syncer is already stopped; not syncing.", "negSyncerKey", key.String())
			continue
		}

		switch key.NegType {

		case negtypes.VmIpEndpointType:
			manager.logger.V(1).Info("SyncNodes: Triggering sync", "negSyncerKey", key.String(), "negSyncerType", key.NegType)
			syncer.Sync()

		case negtypes.VmIpPortEndpointType, negtypes.NonGCPPrivateEndpointType:
			if isVmIpPortZoneChange {
				manager.logger.V(1).Info("SyncNodes: Triggering sync because of zone change", "negSyncerKey", key.String(), "negSyncerType", key.NegType)
				syncer.Sync()
			} else {
				manager.logger.V(1).Info("SyncNodes: Not triggering sync since no zone change", "negSyncerKey", key.String(), "negSyncerType", key.NegType)
			}

		default:
			manager.logger.Error(nil, "SyncNodes: Not triggering sync for syncer of unknown type", "negSyncerType", key.NegType)
		}
	}
}

// SyncAllSyncers signals all syncers to sync.
func (manager *syncerManager) SyncAllSyncers() {
	manager.mu.Lock()
	defer manager.mu.Unlock()

	for key, syncer := range manager.syncerMap {
		if syncer.IsStopped() {
			manager.logger.V(1).Info("SyncAllSyncers: Syncer is already stopped; not syncing.", "negSyncerKey", key.String())
			continue
		}
		syncer.Sync()
	}
}

// updateZoneMap updates the existingZoneMap with the latest zones and returns
// true if the zones have changed. The caller must obtain mu mutex of the
// manager before calling this function since it modifies the passed
// existingZoneMap.
func updateZoneMap(existingZoneMap *map[string]struct{}, nodeFilter zonegetter.Filter, zoneGetter *zonegetter.ZoneGetter, logger klog.Logger) bool {
	zones, err := zoneGetter.ListZones(nodeFilter, logger)
	if err != nil {
		logger.Error(err, "Unable to list zones")
		metrics.PublishNegControllerErrorCountMetrics(err, true)
		return false
	}

	newZoneMap := make(map[string]struct{})
	for _, zone := range zones {
		newZoneMap[zone] = struct{}{}
	}

	zoneChange := !reflect.DeepEqual(*existingZoneMap, newZoneMap)
	*existingZoneMap = newZoneMap

	return zoneChange
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
	manager.logger.V(2).Info("Start NEG garbage collection.")
	defer manager.logger.V(2).Info("NEG garbage collection finished.")
	start := time.Now()
	// Garbage collect Syncers
	manager.garbageCollectSyncer()

	// Garbage collect NEGs
	var err error
	if manager.svcNegClient != nil {
		err = manager.garbageCollectNEGWithCRD()
	} else {
		err = manager.garbageCollectNEG()
	}
	if err != nil {
		err = fmt.Errorf("failed to garbage collect negs: %w", err)
	}
	metrics.PublishNegManagerProcessMetrics(metrics.GCProcess, err, start)
	return err
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
			manager.logger.Error(err, "Failed to retrieve service from store", "service", svcKey.Key())
			metrics.PublishNegControllerErrorCountMetrics(err, true)
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
		if info, ok := v[negtypes.PortInfoMapKey{ServicePort: syncerKey.PortTuple.Port}]; ok {
			return info.ReadinessGate
		}
	}
	return false
}

// ensureDeleteSvcNegCR will set the deletion timestamp for the specified NEG CR based
// on the given neg name. If the Deletion timestamp has already been set on the CR, no
// change will occur.
func (manager *syncerManager) ensureDeleteSvcNegCR(namespace, negName string) error {
	if manager.svcNegClient == nil {
		return nil
	}
	obj, exists, err := manager.svcNegLister.GetByKey(fmt.Sprintf("%s/%s", namespace, negName))
	if err != nil {
		return fmt.Errorf("failed retrieving neg %s/%s to delete: %w", namespace, negName, err)
	}
	if !exists {
		return nil
	}
	neg := obj.(*negv1beta1.ServiceNetworkEndpointGroup)

	if neg.GetDeletionTimestamp().IsZero() {
		start := time.Now()
		err = manager.svcNegClient.NetworkingV1beta1().ServiceNetworkEndpointGroups(namespace).Delete(context.Background(), negName, metav1.DeleteOptions{})
		metrics.PublishK8sRequestCountMetrics(start, metrics.DeleteRequest, err)
		if err != nil {
			return fmt.Errorf("errored while deleting neg cr %s/%s: %w", negName, namespace, err)
		}
		manager.logger.V(2).Info("Deleted neg cr", "svcneg", klog.KRef(namespace, negName))
	}
	return nil
}

// garbageCollectSyncer removes stopped syncer from syncerMap
func (manager *syncerManager) garbageCollectSyncer() {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	for key, syncer := range manager.syncerMap {
		if syncer.IsStopped() && !syncer.IsShuttingDown() {
			delete(manager.syncerMap, key)
			manager.syncerMetrics.DeleteSyncer(key)
		}
	}
}

func (manager *syncerManager) garbageCollectNEG() error {
	// Retrieve aggregated NEG list from cloud
	// Compare against svcPortMap and Remove unintended NEGs by best effort
	negList, err := manager.cloud.AggregatedListNetworkEndpointGroup(meta.VersionGA, manager.logger)
	if err != nil {
		return fmt.Errorf("failed to retrieve aggregated NEG list: %w", err)
	}

	deleteCandidates := map[string][]string{}
	for key, neg := range negList {
		if key.Type() != meta.Zonal {
			// covers the case when key.Zone is not populated
			manager.logger.V(4).Info("Ignoring key as it is not zonal", "key", key)
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
			if err := manager.ensureDeleteNetworkEndpointGroup(name, zone, nil); err != nil {
				return fmt.Errorf("failed to delete NEG %q in %q: %w", name, zone, err)
			}
		}
	}
	return nil
}

type deletionCandidate struct {
	neg *negv1beta1.ServiceNetworkEndpointGroup
	// tbdOnly indicates that only the NEGs that are in the TBD state should be deleted
	tbdOnly bool
}

// garbageCollectNEGWithCRD uses the NEG CRs and the svcPortMap to determine which NEGs
// need to be garbage collected. Neg CRs that do not have a configuration in the svcPortMap will deleted
// along with all corresponding NEGs in the CR's list of NetworkEndpointGroups. If NEG deletion fails in
// the cloud, the corresponding Neg CR will not be deleted
func (manager *syncerManager) garbageCollectNEGWithCRD() error {

	deletionCandidates := map[string]deletionCandidate{}
	negCRs := manager.svcNegLister.List()
	for _, obj := range negCRs {
		neg := obj.(*negv1beta1.ServiceNetworkEndpointGroup)
		deletionCandidates[neg.Name] = deletionCandidate{neg: neg, tbdOnly: false}
	}

	func() {
		manager.mu.Lock()
		defer manager.mu.Unlock()
		for _, portInfoMap := range manager.svcPortMap {
			for _, portInfo := range portInfoMap {
				// Manager svcPortMap replicates the desired state of services, so svcPortMap is the source of truth
				// and determining factor to find deletion candidates. In the best case, neg cr will have a deletion
				// timestamp, the neg config will not exist in the svcPortMap, and both CR and neg will be deleted.
				// In the situation a neg config is in the svcPortMap but the CR has a deletion timestamp, then
				// neither the neg nor CR will not be deleted. In the situation a neg config is not in the svcPortMap,
				// but the CR does not have a deletion timestamp, both CR and neg will be deleted.
				candidate, ok := deletionCandidates[portInfo.NegName]
				if ok {
					if !containsTBDNeg(candidate.neg) {
						delete(deletionCandidates, portInfo.NegName)
					} else {
						deletionCandidates[portInfo.NegName] = deletionCandidate{neg: candidate.neg, tbdOnly: true}
					}
				}
			}
		}
	}()

	// This section includes a potential race condition between deleting neg here and users adds the neg annotation.
	// The worst outcome of the race condition is that neg is deleted in the end but user actually specifies a neg.
	// This would be resolved (sync neg) when the next endpoint update or resync arrives.
	// TODO: avoid race condition here

	var errList []error
	// errListMutex protects writes to errList. It should be acquired when one
	// expects concurrent writes to errList.
	var errListMutex sync.Mutex

	// Deletion candidate NEGs should be deleted from all zones, even ones that
	// currently don't have any Ready nodes.
	zones, err := manager.zoneGetter.ListZones(zonegetter.AllNodesFilter, manager.logger)
	if err != nil {
		errList = append(errList, fmt.Errorf("failed to get zones during garbage collection: %w", err))
	}

	deletionCandidatesChan := make(chan deletionCandidate, len(deletionCandidates))
	for _, dc := range deletionCandidates {
		deletionCandidatesChan <- dc
	}
	close(deletionCandidatesChan)

	wg := sync.WaitGroup{}
	wg.Add(len(deletionCandidates))
	for i := 0; i < manager.numGCWorkers; i++ {
		go func() {
			for svcNegCR := range deletionCandidatesChan {
				errs := manager.processNEGDeletionCandidate(svcNegCR, zones)

				errListMutex.Lock()
				errList = append(errList, errs...)
				errListMutex.Unlock()

				wg.Done()
			}
		}()
	}
	wg.Wait()

	return utilerrors.NewAggregate(errList)
}

func containsTBDNeg(negCR *negv1beta1.ServiceNetworkEndpointGroup) bool {
	for _, negRef := range negCR.Status.NetworkEndpointGroups {
		if negRef.State == negv1beta1.ToBeDeletedState {
			return true
		}
	}
	return false
}

// processNEGDeletionCandidate attempts to delete `svcNegCR` and all NEGs
// associated with it. In case when `svcNegCR` does not have ample information
// about the zones associated with this NEG, it will attempt to delete the NEG
// from all zones specified through the `zones` slice. If the candidate has the
// tbdObly flag set to true, then only the NEGs in the candidate that have the
// state TO_BE_DELETED will be deleted. At the end, the SvcNeg resource is updated
// with the current state of NEGs that the neg controller is aware about.
func (manager *syncerManager) processNEGDeletionCandidate(candidate deletionCandidate, zones []string) []error {
	svcNegCR := candidate.neg
	manager.logger.Info("Count of NEGs referenced by SvcNegCR", "svcneg", klog.KObj(svcNegCR), "count", len(svcNegCR.Status.NetworkEndpointGroups), "tbdOnly", candidate.tbdOnly)
	var errList []error
	shouldDeleteNegCR := true

	deleteByZone := len(svcNegCR.Status.NetworkEndpointGroups) == 0
	// Change this to a map from NEG name to sets once we allow multiple NEGs
	// in a specific zone(multi-subnet cluster).
	deletedNegs := make(map[negtypes.NegInfo]struct{})

	for _, negRef := range svcNegCR.Status.NetworkEndpointGroups {
		if candidate.tbdOnly && negRef.State != negv1beta1.ToBeDeletedState {
			continue
		}
		resourceID, err := cloud.ParseResourceURL(negRef.SelfLink)
		if err != nil {
			errList = append(errList, fmt.Errorf("failed to parse selflink for neg cr %s/%s: %s", svcNegCR.Namespace, svcNegCR.Name, err))
			if candidate.tbdOnly {

				// This case should not be possible and likely means someone has manually manipulated the
				// svcneg resource. We do not have a good way of detecting what is wrong and believe user
				// intervention is safer than accidentally deleting a NEG that is being used.
				eventMsg := fmt.Sprintf("Detected TO_BE_DELETED NEGs, but unable to parse selflink %s. Please manually delete the NEG and remove the corresponding reference from the svcneg resource", negRef.SelfLink)
				manager.recorder.Eventf(svcNegCR, v1.EventTypeWarning, negtypes.NegGCError, eventMsg)
				continue
			}
			deleteByZone = true
			continue
		}
		negDeleted := manager.deleteNegOrReportErr(resourceID.Key.Name, resourceID.Key.Zone, svcNegCR, &errList)
		if negDeleted {
			deletedNegs[negtypes.NegInfo{Name: resourceID.Key.Name, Zone: resourceID.Key.Zone}] = struct{}{}
		}
		shouldDeleteNegCR = shouldDeleteNegCR && negDeleted
	}

	if deleteByZone {
		manager.logger.V(2).Info("Deletion candidate has 0 NEG reference", "svcneg", klog.KObj(svcNegCR), "svcNegCR", svcNegCR)
		for _, zone := range zones {
			negDeleted := manager.deleteNegOrReportErr(svcNegCR.Name, zone, svcNegCR, &errList)
			if negDeleted {
				deletedNegs[negtypes.NegInfo{Name: svcNegCR.Name, Zone: zone}] = struct{}{}
			}
			shouldDeleteNegCR = shouldDeleteNegCR && negDeleted
		}
	}

	if !shouldDeleteNegCR || candidate.tbdOnly {
		// Since no more NEG deletion will be happening at this point, and NEG
		// CR will not be deleted, clear the reference for deleted NEGs in the
		// NEG CR.
		if len(deletedNegs) != 0 {
			updatedCR := svcNegCR.DeepCopy()

			if errs := ensureExistingNegRef(updatedCR, deletedNegs); len(errs) != 0 {
				errList = append(errList, errs...)
			}
			if _, err := patchNegStatus(manager.svcNegClient, *svcNegCR, *updatedCR); err != nil {
				errList = append(errList, err)
			}
		}
		return errList
	}

	func() {
		manager.mu.Lock()
		defer manager.mu.Unlock()

		// Verify that the NEG is still not wanted before deleting the CR. Mitigates the possibility of the race
		// condition mentioned in garbageCollectNEGWithCRD()
		svcKey := getServiceKey(svcNegCR.Namespace, svcNegCR.GetLabels()[negtypes.NegCRServiceNameKey])
		portInfoMap := manager.svcPortMap[svcKey]
		for _, portInfo := range portInfoMap {
			if portInfo.NegName == svcNegCR.Name {
				manager.logger.V(2).Info("NEG CR is still desired, skipping deletion", "svcneg", klog.KObj(svcNegCR))
				return
			}
		}

		manager.logger.V(2).Info("Deleting NEG CR", "svcneg", klog.KObj(svcNegCR))
		err := deleteSvcNegCR(manager.svcNegClient, svcNegCR, manager.logger)
		if err != nil {
			manager.logger.V(2).Error(err, "Error when deleting NEG CR", "svcneg", klog.KObj(svcNegCR))
			errList = append(errList, err)
			return
		}
		manager.logger.V(2).Info("Deleted NEG CR", "svcneg", klog.KObj(svcNegCR))
	}()

	return errList
}

// deleteNegOrReportErr will attempt to delete the specified NEG resource in the
// cloud. Successful deletion is indicated by returning `true` and a failure
// would return `false`. In addition, if the deletion failed, the error will be
// reported as an event on the given CR and added to the passed `errList`.
func (manager *syncerManager) deleteNegOrReportErr(name, zone string, svcNegCR *negv1beta1.ServiceNetworkEndpointGroup, errList *[]error) bool {
	expectedDesc := &utils.NegDescription{
		ClusterUID:  string(manager.kubeSystemUID),
		Namespace:   svcNegCR.Namespace,
		ServiceName: svcNegCR.GetLabels()[negtypes.NegCRServiceNameKey],
		Port:        svcNegCR.GetLabels()[negtypes.NegCRServicePortKey],
	}
	if err := manager.ensureDeleteNetworkEndpointGroup(name, zone, expectedDesc); err != nil {
		err = fmt.Errorf("failed to delete NEG %s in %s: %s", name, zone, err)
		manager.recorder.Eventf(svcNegCR, v1.EventTypeWarning, negtypes.NegGCError, err.Error())
		*errList = append(*errList, err)

		return false
	}

	return true
}

// ensureExistingNegRef removes NEG refs in NEG CR for NEGs that have been
// deleted successfully.
func ensureExistingNegRef(neg *negv1beta1.ServiceNetworkEndpointGroup, deletedNegs map[negtypes.NegInfo]struct{}) []error {
	var updatedNegRef []negv1beta1.NegObjectReference
	var errList []error
	for _, negRef := range neg.Status.NetworkEndpointGroups {
		negInfo, err := negtypes.NegInfoFromNegRef(negRef)
		if err != nil {
			errList = append(errList, err)
			continue
		}
		if _, exists := deletedNegs[negInfo]; !exists {
			updatedNegRef = append(updatedNegRef, negRef)
		}
	}
	neg.Status.NetworkEndpointGroups = updatedNegRef
	return errList
}

// ensureDeleteNetworkEndpointGroup ensures neg is delete from zone
func (manager *syncerManager) ensureDeleteNetworkEndpointGroup(name, zone string, expectedDesc *utils.NegDescription) error {
	neg, err := manager.cloud.GetNetworkEndpointGroup(name, zone, meta.VersionGA, manager.logger)
	if err != nil {
		if utils.IsNotFoundError(err) || utils.IsHTTPErrorCode(err, http.StatusBadRequest) {
			manager.logger.V(2).Info("Ignoring error when querying for neg during GC", "negName", name, "zone", zone, "err", err)
			metrics.PublishNegControllerErrorCountMetrics(err, true)
			return nil
		}
		return err
	}

	if expectedDesc != nil {
		// Controller managed custom named negs will always have a populated description, so do not delete custom named
		// negs with empty descriptions.
		if !manager.namer.IsNEG(name) && neg.Description == "" {
			manager.logger.V(2).Info("Skipping deletion of Neg because name was not generated and empty description", "negName", name, "zone", zone)
			return nil
		}
		if matches, err := utils.VerifyDescription(*expectedDesc, neg.Description, name, zone); !matches {
			manager.logger.V(2).Info("Skipping deletion of Neg because of conflicting description", "negName", name, "zone", zone, "err", err)
			return nil
		}
	}

	manager.logger.V(2).Info("Deleting NEG", "negName", name, "zone", zone)
	return manager.cloud.DeleteNetworkEndpointGroup(name, zone, meta.VersionGA, manager.logger)
}

// ensureSvcNegCR ensures that if neg crd is enabled, a Neg CR exists for every
// desired Neg if it does not already exist. If an NEG CR already exists, and has the required labels
// its Object Meta will be updated if necessary. If the NEG CR does not have required labels an error is thrown.
func (manager *syncerManager) ensureSvcNegCR(svcKey serviceKey, portInfo negtypes.PortInfo) error {
	if manager.svcNegClient == nil {
		return nil
	}

	obj, exists, err := manager.serviceLister.GetByKey(svcKey.Key())
	if err != nil {
		manager.logger.Error(err, "Failed to retrieve service from store", "service", svcKey.Key())
	}

	if !exists {
		return fmt.Errorf("Service not found")
	}

	service := obj.(*v1.Service)

	gvk := schema.GroupVersionKind{Version: "v1", Kind: "Service"}
	ownerReference := metav1.NewControllerRef(service, gvk)
	ownerReference.BlockOwnerDeletion = utilpointer.BoolPtr(false)
	labels := map[string]string{
		negtypes.NegCRManagedByKey:   negtypes.NegCRControllerValue,
		negtypes.NegCRServiceNameKey: svcKey.name,
		negtypes.NegCRServicePortKey: fmt.Sprint(portInfo.PortTuple.Port),
	}

	newCR := negv1beta1.ServiceNetworkEndpointGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:            portInfo.NegName,
			Namespace:       svcKey.namespace,
			OwnerReferences: []metav1.OwnerReference{*ownerReference},
			Labels:          labels,
			Finalizers:      []string{common.NegFinalizerKey},
		},
	}

	obj, exists, err = manager.svcNegLister.GetByKey(fmt.Sprintf("%s/%s", svcKey.namespace, portInfo.NegName))
	if err != nil {
		return fmt.Errorf("Error retrieving existing negs: %s", err)
	}
	if !exists {
		// Neg does not exist so create it
		start := time.Now()
		_, err = manager.svcNegClient.NetworkingV1beta1().ServiceNetworkEndpointGroups(svcKey.namespace).Create(context.Background(), &newCR, metav1.CreateOptions{})
		metrics.PublishK8sRequestCountMetrics(start, metrics.CreateRequest, err)
		manager.logger.V(2).Info("Created ServiceNetworkEndpointGroup CR for neg", "svcneg", klog.KRef(svcKey.namespace, portInfo.NegName))
		return err
	}
	negCR := obj.(*negv1beta1.ServiceNetworkEndpointGroup)

	needUpdate, err := ensureNegCRLabels(negCR, labels, manager.logger)
	if err != nil {
		manager.logger.Error(err, "failed to ensure labels for neg", "svcneg", klog.KRef(negCR.Namespace, negCR.Name), "service", service.Name)
		return err
	}
	needUpdate = ensureNegCROwnerRef(negCR, newCR.OwnerReferences) || needUpdate

	if needUpdate {
		start := time.Now()
		_, err = manager.svcNegClient.NetworkingV1beta1().ServiceNetworkEndpointGroups(svcKey.namespace).Update(context.Background(), negCR, metav1.UpdateOptions{})
		metrics.PublishK8sRequestCountMetrics(start, metrics.UpdateRequest, err)
		return err
	}
	return nil
}

func ensureNegCRLabels(negCR *negv1beta1.ServiceNetworkEndpointGroup, labels map[string]string, logger klog.Logger) (bool, error) {
	needsUpdate := false
	existingLabels := negCR.GetLabels()
	logger.V(4).Info("Ensuring NEG CR labels", "svcneg", klog.KRef(negCR.Namespace, negCR.Name), "existingLabels", existingLabels)

	//Check that required labels exist and are matching
	for key, value := range labels {
		existingVal, ok := existingLabels[key]
		if !ok {
			negCR.Labels[key] = value
			needsUpdate = true
		} else if existingVal != value {
			svcName := ""
			if len(negCR.OwnerReferences) == 1 {
				svcName = negCR.OwnerReferences[0].Name
			}
			return false, fmt.Errorf("Neg %s/%s has a label mismatch. Expected key %s to have the value %s but found %s. Neg is likely taken by % service. Please remove previous neg before creating this configuration", negCR.Namespace, negCR.Name, key, value, existingVal, svcName)
		}
	}

	return needsUpdate, nil
}

func ensureNegCROwnerRef(negCR *negv1beta1.ServiceNetworkEndpointGroup, expectedOwnerRef []metav1.OwnerReference) bool {
	if !reflect.DeepEqual(negCR.OwnerReferences, expectedOwnerRef) {
		negCR.OwnerReferences = expectedOwnerRef
		return true
	}
	return false
}

// deleteSvcNegCR will remove finalizers on the given negCR and if deletion timestamp is not set, will delete it as well
func deleteSvcNegCR(svcNegClient svcnegclient.Interface, negCR *negv1beta1.ServiceNetworkEndpointGroup, logger klog.Logger) error {
	updatedCR := negCR.DeepCopy()
	updatedCR.Finalizers = []string{}
	if _, err := patchNegStatus(svcNegClient, *negCR, *updatedCR); err != nil {
		return fmt.Errorf("failed to patch neg status: %w", err)
	}

	logger.Info("Removed finalizer on ServiceNetworkEndpointGroup CR", "svcneg", klog.KRef(negCR.Namespace, negCR.Name))

	// If CR does not have a deletion timestamp, delete
	if negCR.GetDeletionTimestamp().IsZero() {
		logger.Info("Deleting ServiceNetworkEndpointGroup CR", "svcneg", klog.KRef(negCR.Namespace, negCR.Name))
		start := time.Now()
		err := svcNegClient.NetworkingV1beta1().ServiceNetworkEndpointGroups(negCR.Namespace).Delete(context.Background(), negCR.Name, metav1.DeleteOptions{})
		metrics.PublishK8sRequestCountMetrics(start, metrics.DeleteRequest, err)
		if err != nil {
			logger.Error(err, "Failed to delete ServiceNetworkEndpointGroup CR", "svcneg", klog.KRef(negCR.Namespace, negCR.Name))
			return err
		}
	}
	return nil
}

// patchNegStatus patches the specified NegCR status with the provided new status
func patchNegStatus(svcNegClient svcnegclient.Interface, oldNeg, newNeg negv1beta1.ServiceNetworkEndpointGroup) (*negv1beta1.ServiceNetworkEndpointGroup, error) {
	patchBytes, err := patch.MergePatchBytes(oldNeg, newNeg)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare patch bytes: %s", err)
	}
	start := time.Now()
	neg, err := svcNegClient.NetworkingV1beta1().ServiceNetworkEndpointGroups(oldNeg.Namespace).Patch(context.Background(), oldNeg.Name, types.MergePatchType, patchBytes, metav1.PatchOptions{})
	metrics.PublishK8sRequestCountMetrics(start, metrics.PatchRequest, err)
	return neg, err
}

// getSyncerKey encodes a service namespace, name, service port and targetPort into a string key
func (manager *syncerManager) getSyncerKey(namespace, name string, servicePortKey negtypes.PortInfoMapKey, portInfo negtypes.PortInfo) negtypes.NegSyncerKey {
	networkEndpointType := negtypes.VmIpPortEndpointType
	calculatorMode := negtypes.L7Mode
	if manager.enableNonGcpMode {
		networkEndpointType = negtypes.NonGCPPrivateEndpointType
	}
	if portInfo.PortTuple.Empty() {
		networkEndpointType = negtypes.VmIpEndpointType
		calculatorMode = portInfo.EpCalculatorMode
	}

	return negtypes.NegSyncerKey{
		Namespace:        namespace,
		Name:             name,
		NegName:          portInfo.NegName,
		PortTuple:        portInfo.PortTuple,
		NegType:          networkEndpointType,
		EpCalculatorMode: calculatorMode,
		L4LBType:         portInfo.L4LBType,
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
func (manager *syncerManager) removeCommonPorts(p1, p2 negtypes.PortInfoMap) {
	for port, portInfo1 := range p1 {
		portInfo2, ok := p2[port]
		if !ok {
			continue
		}

		syncerKey1 := manager.getSyncerKey("", "", port, portInfo1)
		syncerKey2 := manager.getSyncerKey("", "", port, portInfo2)
		if reflect.DeepEqual(syncerKey1, syncerKey2) {
			delete(p1, port)
			delete(p2, port)
		}
	}
}
