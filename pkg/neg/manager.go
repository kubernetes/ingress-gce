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
	"reflect"
	"sync"
	"time"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	negv1beta1 "k8s.io/ingress-gce/pkg/apis/svcneg/v1beta1"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/neg/metrics"
	"k8s.io/ingress-gce/pkg/neg/readiness"
	negsyncer "k8s.io/ingress-gce/pkg/neg/syncers"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	svcnegclient "k8s.io/ingress-gce/pkg/svcneg/client/clientset/versioned"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/common"
	"k8s.io/klog"
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
	recorder   record.EventRecorder
	cloud      negtypes.NetworkEndpointGroupCloud
	zoneGetter negtypes.ZoneGetter

	nodeLister     cache.Indexer
	podLister      cache.Indexer
	serviceLister  cache.Indexer
	endpointLister cache.Indexer
	svcNegLister   cache.Indexer

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
	//svcNegClient handles lifecycle operations for NEG CRs
	svcNegClient svcnegclient.Interface

	// kubeSystemUID is used to by syncers when NEG CRD is enabled
	kubeSystemUID types.UID
}

func newSyncerManager(namer negtypes.NetworkEndpointGroupNamer, recorder record.EventRecorder, cloud negtypes.NetworkEndpointGroupCloud, zoneGetter negtypes.ZoneGetter, svcNegClient svcnegclient.Interface, kubeSystemUID types.UID, podLister, serviceLister, endpointLister, nodeLister, svcNegLister cache.Indexer) *syncerManager {
	return &syncerManager{
		namer:          namer,
		recorder:       recorder,
		cloud:          cloud,
		zoneGetter:     zoneGetter,
		nodeLister:     nodeLister,
		podLister:      podLister,
		serviceLister:  serviceLister,
		endpointLister: endpointLister,
		svcNegLister:   svcNegLister,
		svcPortMap:     make(map[serviceKey]negtypes.PortInfoMap),
		syncerMap:      make(map[negtypes.NegSyncerKey]negtypes.NegSyncer),
		svcNegClient:   svcNegClient,
		kubeSystemUID:  kubeSystemUID,
	}
}

// EnsureSyncer starts and stops syncers based on the input service ports.
func (manager *syncerManager) EnsureSyncers(namespace, name string, newPorts negtypes.PortInfoMap) error {
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
	removeCommonPorts(adds, removes)

	manager.svcPortMap[key] = newPorts
	klog.V(3).Infof("EnsureSyncer %v/%v: syncing %v ports, removing %v ports, adding %v ports", namespace, name, newPorts, removes, adds)

	errList := []error{}
	for svcPort, portInfo := range removes {
		syncer, ok := manager.syncerMap[getSyncerKey(namespace, name, svcPort, portInfo)]
		if ok {
			syncer.Stop()
		}

		err := manager.ensureDeleteSvcNegCR(namespace, portInfo.NegName)
		if err != nil {
			errList = append(errList, err)
		}
	}

	// Ensure a syncer is running for each port that is being added.
	for svcPort, portInfo := range adds {
		syncerKey := getSyncerKey(namespace, name, svcPort, portInfo)
		syncer, ok := manager.syncerMap[syncerKey]
		if !ok {

			// To ensure that a NEG CR always exists during the lifecyle of a NEG, do not create a syncer for the NEG until the NEG CR is successfully created. This will reduce the possibility of invalid states and reduces complexity of garbage collection
			if err := manager.ensureSvcNegCR(key, portInfo); err != nil {
				errList = append(errList, err)
				continue
			}

			// determine the implementation that calculates NEG endpoints on each sync.
			epc := negsyncer.GetEndpointsCalculator(manager.nodeLister, manager.podLister, manager.zoneGetter,
				syncerKey, portInfo.EpCalculatorMode)
			syncer = negsyncer.NewTransactionSyncer(
				syncerKey,
				manager.recorder,
				manager.cloud,
				manager.zoneGetter,
				manager.podLister,
				manager.serviceLister,
				manager.endpointLister,
				manager.nodeLister,
				manager.svcNegLister,
				manager.reflector,
				epc,
				string(manager.kubeSystemUID),
				manager.svcNegClient,
			)
			manager.syncerMap[syncerKey] = syncer
		}

		if syncer.IsStopped() {
			if err := syncer.Start(); err != nil {
				errList = append(errList, err)
			}
		}
	}
	err := utilerrors.NewAggregate(errList)
	metrics.PublishNegManagerProcessMetrics(metrics.SyncProcess, err, start)
	return err
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

// SyncNodes signals all GCE_VM_IP syncers to sync.
// Only these use nodes selected at random as endpoints and hence need to sync upon node updates.
func (manager *syncerManager) SyncNodes() {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	for key, syncer := range manager.syncerMap {
		if key.NegType == negtypes.VmIpEndpointType && !syncer.IsStopped() {
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
		err = fmt.Errorf("failed to garbage collect negs: %v", err)
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

// ensureDeleteSvcNegCR will set the deletion timestamp for the specified NEG CR based
// on the given neg name. If the Deletion timestamp has already been set on the CR, no
// change will occur.
func (manager *syncerManager) ensureDeleteSvcNegCR(namespace, negName string) error {
	if manager.svcNegClient == nil {
		return nil
	}
	obj, exists, err := manager.svcNegLister.GetByKey(fmt.Sprintf("%s/%s", namespace, negName))
	if err != nil {
		return fmt.Errorf("failed retrieving neg %s/%s to delete: %s", namespace, negName, err)
	}
	if !exists {
		return nil
	}
	neg := obj.(*negv1beta1.ServiceNetworkEndpointGroup)

	if neg.GetDeletionTimestamp().IsZero() {
		err = manager.svcNegClient.NetworkingV1beta1().ServiceNetworkEndpointGroups(namespace).Delete(context.Background(), negName, metav1.DeleteOptions{})
		return err
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

// garbageCollectNEGWithCRD uses the NEG CRs and the svcPortMap to determine which NEGs
// need to be garbage collected. Neg CRs that do not have a configuration in the svcPortMap will deleted
// along with all corresponding NEGs in the CR's list of NetworkEndpointGroups. If NEG deletion fails in
// the cloud, the corresponding Neg CR will not be deleted
func (manager *syncerManager) garbageCollectNEGWithCRD() error {
	deletionCandidates := map[string]*negv1beta1.ServiceNetworkEndpointGroup{}
	negCRs := manager.svcNegLister.List()
	for _, obj := range negCRs {
		neg := obj.(*negv1beta1.ServiceNetworkEndpointGroup)
		deletionCandidates[neg.Name] = neg
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
				if _, ok := deletionCandidates[portInfo.NegName]; ok {
					delete(deletionCandidates, portInfo.NegName)
				}
			}
		}
	}()

	var errList []error
	for _, cr := range deletionCandidates {
		shouldDeleteNegCR := true
		for _, negRef := range cr.Status.NetworkEndpointGroups {
			resourceID, err := cloud.ParseResourceURL(negRef.SelfLink)
			if err != nil {
				errList = append(errList, fmt.Errorf("failed to parse selflink for neg cr %s/%s: %s", cr.Namespace, cr.Name, err))
				continue
			}

			if err := manager.ensureDeleteNetworkEndpointGroup(resourceID.Key.Name, resourceID.Key.Zone); err != nil {
				err = fmt.Errorf("failed to delete NEG %s in %s: %s", resourceID.Key.Name, resourceID.Key.Zone, err)
				manager.recorder.Eventf(cr, v1.EventTypeWarning, negtypes.NegGCError, err.Error())
				errList = append(errList, err)

				// Error when deleting NEG, do not delete Neg CR
				shouldDeleteNegCR = false
			}
		}

		if !shouldDeleteNegCR {
			continue
		}

		if err := deleteSvcNegCR(manager.svcNegClient, cr); err != nil {
			errList = append(errList, err)
		}
	}
	return utilerrors.NewAggregate(errList)
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

// ensureSvcNegCR ensures that if neg crd is enabled, a Neg CR exists for every
// desired Neg if it does not already exist. If an NEG CR already exists, and has the required labels
// its Object Meta will be updated if necessary. If the NEG CR does not have required labels an error is thrown.
func (manager *syncerManager) ensureSvcNegCR(svcKey serviceKey, portInfo negtypes.PortInfo) error {
	if manager.svcNegClient == nil {
		return nil
	}

	obj, exists, err := manager.serviceLister.GetByKey(svcKey.Key())
	if err != nil {
		klog.Errorf("Failed to retrieve service %s from store: %v", svcKey.Key(), err)
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

	//TODO: Add finalizer after Neg CRD Garbage Collection is implemented.
	newCR := negv1beta1.ServiceNetworkEndpointGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:            portInfo.NegName,
			Namespace:       svcKey.namespace,
			OwnerReferences: []metav1.OwnerReference{*ownerReference},
			Labels:          labels,
			Finalizers:      []string{common.NegFinalizerKey},
		},
	}

	negCR, err := manager.svcNegClient.NetworkingV1beta1().ServiceNetworkEndpointGroups(svcKey.namespace).Get(context.Background(), portInfo.NegName, metav1.GetOptions{})
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("Error retrieving existing negs: %s", err)
		}

		// Neg does not exist so create it
		_, err = manager.svcNegClient.NetworkingV1beta1().ServiceNetworkEndpointGroups(svcKey.namespace).Create(context.Background(), &newCR, metav1.CreateOptions{})
		return err
	}

	needUpdate, err := ensureNegCRLabels(negCR, labels)
	if err != nil {
		return err
	}
	needUpdate = ensureNegCROwnerRef(negCR, newCR.OwnerReferences) || needUpdate

	if needUpdate {
		newCR.Status = negCR.Status
		_, err = manager.svcNegClient.NetworkingV1beta1().ServiceNetworkEndpointGroups(svcKey.namespace).Update(context.Background(), &newCR, metav1.UpdateOptions{})
		return err
	}
	return nil
}

func ensureNegCRLabels(negCR *negv1beta1.ServiceNetworkEndpointGroup, labels map[string]string) (bool, error) {
	//Check that required labels exist and are matching
	existingLabels := negCR.GetLabels()
	for key, value := range labels {
		if existingVal := existingLabels[key]; existingVal != value {
			return false, fmt.Errorf("Neg already exists with name %s but label %s has value %s instead of %s. Delete previous neg before creating this configuration", negCR.Name, key, existingVal, value)
		}
	}

	if !reflect.DeepEqual(existingLabels, labels) {
		negCR.Labels = labels
		return true, nil
	}
	return false, nil
}

func ensureNegCROwnerRef(negCR *negv1beta1.ServiceNetworkEndpointGroup, expectedOwnerRef []metav1.OwnerReference) bool {
	if !reflect.DeepEqual(negCR.OwnerReferences, expectedOwnerRef) {
		negCR.OwnerReferences = expectedOwnerRef
		return true
	}
	return false
}

// deleteSvcNegCR will remove finalizers on the given negCR and if deletion timestamp is not set, will delete it as well
func deleteSvcNegCR(svcNegClient svcnegclient.Interface, negCR *negv1beta1.ServiceNetworkEndpointGroup) error {
	updatedCR := negCR.DeepCopy()
	updatedCR.Finalizers = []string{}
	patchNegStatus(svcNegClient, *negCR, *updatedCR)

	// If CR does not have a deletion timestamp, delete
	if negCR.GetDeletionTimestamp().IsZero() {
		return svcNegClient.NetworkingV1beta1().ServiceNetworkEndpointGroups(negCR.Namespace).Delete(context.Background(), negCR.Name, metav1.DeleteOptions{})
	}
	return nil
}

// patchNegStatus patches the specified NegCR status with the provided new status
func patchNegStatus(svcNegClient svcnegclient.Interface, oldNeg, newNeg negv1beta1.ServiceNetworkEndpointGroup) (*negv1beta1.ServiceNetworkEndpointGroup, error) {
	patchBytes, err := utils.MergePatchBytes(oldNeg, newNeg)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare patch bytes: %s", err)
	}

	return svcNegClient.NetworkingV1beta1().ServiceNetworkEndpointGroups(oldNeg.Namespace).Patch(context.Background(), oldNeg.Name, types.MergePatchType, patchBytes, metav1.PatchOptions{})
}

// getSyncerKey encodes a service namespace, name, service port and targetPort into a string key
func getSyncerKey(namespace, name string, servicePortKey negtypes.PortInfoMapKey, portInfo negtypes.PortInfo) negtypes.NegSyncerKey {
	networkEndpointType := negtypes.VmIpPortEndpointType
	calculatorMode := negtypes.L7Mode
	if flags.F.EnableNonGCPMode {
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
		Subset:           servicePortKey.Subset,
		SubsetLabels:     portInfo.SubsetLabels,
		NegType:          networkEndpointType,
		EpCalculatorMode: calculatorMode,
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
