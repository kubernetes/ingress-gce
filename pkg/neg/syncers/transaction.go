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
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	apiv1 "k8s.io/api/core/v1"
	corev1 "k8s.io/api/core/v1"
	discovery "k8s.io/api/discovery/v1beta1"
	"k8s.io/ingress-gce/pkg/utils/endpointslices"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	negv1beta1 "k8s.io/ingress-gce/pkg/apis/svcneg/v1beta1"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/neg/metrics"
	"k8s.io/ingress-gce/pkg/neg/readiness"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	svcnegclient "k8s.io/ingress-gce/pkg/svcneg/client/clientset/versioned"
	"k8s.io/ingress-gce/pkg/utils/patch"
	"k8s.io/klog"
)

type transactionSyncer struct {
	// metadata
	negtypes.NegSyncerKey

	// syncer provides syncer life cycle interfaces
	syncer negtypes.NegSyncer

	// syncLock ensures state transitions are thread safe
	syncLock sync.Mutex
	// needInit indicates if the NEG resources needed to be initialized.
	// needInit will be set to true under 3 conditions:
	// 1. transaction syncer is newly created
	// 2. the syncInternal function returns any error
	// 3. the operationInternal observed any error
	needInit bool
	// transactions stores each transaction
	transactions networkEndpointTransactionTable

	podLister           cache.Indexer
	serviceLister       cache.Indexer
	endpointLister      cache.Indexer
	endpointSliceLister cache.Indexer
	nodeLister          cache.Indexer
	svcNegLister        cache.Indexer
	recorder            record.EventRecorder
	cloud               negtypes.NetworkEndpointGroupCloud
	zoneGetter          negtypes.ZoneGetter
	endpointsCalculator negtypes.NetworkEndpointsCalculator

	// retry handles back off retry for NEG API operations
	retry retryHandler

	// reflector handles NEG readiness gate and conditions for pods in NEG.
	reflector readiness.Reflector

	//kubeSystemUID used to populate Cluster UID on Neg Description when using NEG CRD
	kubeSystemUID string

	//svcNegClient used to update status on corresponding NEG CRs when not nil
	svcNegClient svcnegclient.Interface

	// customName indicates whether the NEG name is a generated one or custom one
	customName bool

	enableEndpointSlices bool
}

func NewTransactionSyncer(
	negSyncerKey negtypes.NegSyncerKey,
	recorder record.EventRecorder,
	cloud negtypes.NetworkEndpointGroupCloud,
	zoneGetter negtypes.ZoneGetter,
	podLister cache.Indexer,
	serviceLister cache.Indexer,
	endpointLister cache.Indexer,
	endpointSliceLister cache.Indexer,
	nodeLister cache.Indexer,
	svcNegLister cache.Indexer,
	reflector readiness.Reflector,
	epc negtypes.NetworkEndpointsCalculator,
	kubeSystemUID string,
	svcNegClient svcnegclient.Interface,
	customName bool,
	enableEndpointSlices bool) negtypes.NegSyncer {
	// TransactionSyncer implements the syncer core
	ts := &transactionSyncer{
		NegSyncerKey:         negSyncerKey,
		needInit:             true,
		transactions:         NewTransactionTable(),
		nodeLister:           nodeLister,
		podLister:            podLister,
		serviceLister:        serviceLister,
		endpointLister:       endpointLister,
		endpointSliceLister:  endpointSliceLister,
		svcNegLister:         svcNegLister,
		recorder:             recorder,
		cloud:                cloud,
		zoneGetter:           zoneGetter,
		endpointsCalculator:  epc,
		reflector:            reflector,
		kubeSystemUID:        kubeSystemUID,
		svcNegClient:         svcNegClient,
		customName:           customName,
		enableEndpointSlices: enableEndpointSlices,
	}
	// Syncer implements life cycle logic
	syncer := newSyncer(negSyncerKey, serviceLister, recorder, ts)
	// transactionSyncer needs syncer interface for internals
	ts.syncer = syncer
	ts.retry = NewDelayRetryHandler(func() { syncer.Sync() }, NewExponentialBackendOffHandler(maxRetries, minRetryDelay, maxRetryDelay))
	return syncer
}

func GetEndpointsCalculator(nodeLister, podLister cache.Indexer, zoneGetter negtypes.ZoneGetter, syncerKey negtypes.NegSyncerKey, mode negtypes.EndpointsCalculatorMode) negtypes.NetworkEndpointsCalculator {
	serviceKey := strings.Join([]string{syncerKey.Name, syncerKey.Namespace}, "/")
	if syncerKey.NegType == negtypes.VmIpEndpointType {
		nodeLister := listers.NewNodeLister(nodeLister)
		switch mode {
		case negtypes.L4LocalMode:
			return NewLocalL4ILBEndpointsCalculator(nodeLister, zoneGetter, serviceKey)
		default:
			return NewClusterL4ILBEndpointsCalculator(nodeLister, zoneGetter, serviceKey)
		}
	}
	return NewL7EndpointsCalculator(zoneGetter, podLister, syncerKey.PortTuple.Name,
		syncerKey.SubsetLabels, syncerKey.NegType)
}

func (s *transactionSyncer) sync() error {
	err := s.syncInternal()
	if err != nil {
		s.syncLock.Lock()
		s.needInit = true
		s.syncLock.Unlock()
	}
	return err
}

func (s *transactionSyncer) syncInternal() error {
	s.syncLock.Lock()
	defer s.syncLock.Unlock()

	// NOTE: Error will be used to update the status on corresponding Neg CR if Neg CRD is enabled
	// Please reuse and set err before returning
	var err error
	defer s.updateStatus(err)
	start := time.Now()
	defer metrics.PublishNegSyncMetrics(string(s.NegSyncerKey.NegType), string(s.endpointsCalculator.Mode()), err, start)

	if s.needInit {
		if err := s.ensureNetworkEndpointGroups(); err != nil {
			return err
		}
		s.needInit = false
	}

	if s.syncer.IsStopped() || s.syncer.IsShuttingDown() {
		klog.V(4).Infof("Skip syncing NEG %q for %s.", s.NegSyncerKey.NegName, s.NegSyncerKey.String())
		return nil
	}
	klog.V(2).Infof("Sync NEG %q for %s, Endpoints Calculator mode %s", s.NegSyncerKey.NegName,
		s.NegSyncerKey.String(), s.endpointsCalculator.Mode())

	currentMap, err := retrieveExistingZoneNetworkEndpointMap(s.NegSyncerKey.NegName, s.zoneGetter, s.cloud, s.NegSyncerKey.GetAPIVersion(), s.endpointsCalculator.Mode())
	if err != nil {
		return err
	}
	s.logStats(currentMap, "current NEG endpoints")

	// Merge the current state from cloud with the transaction table together
	// The combined state represents the eventual result when all transactions completed
	mergeTransactionIntoZoneEndpointMap(currentMap, s.transactions)
	s.logStats(currentMap, "after in-progress operations have completed, NEG endpoints")

	var targetMap map[string]negtypes.NetworkEndpointSet
	var endpointPodMap negtypes.EndpointPodMap

	if s.enableEndpointSlices {
		slices, err := s.endpointSliceLister.ByIndex(endpointslices.EndpointSlicesByServiceIndex, endpointslices.FormatEndpointSlicesServiceKey(s.Namespace, s.Name))
		if err != nil {
			return err
		}
		if len(slices) < 1 {
			klog.Warningf("Endpoint slices for service %s/%s don't exist. Skipping NEG sync", s.Namespace, s.Name)
			return nil
		}
		endpointSlices := make([]*discovery.EndpointSlice, len(slices))
		for i, slice := range slices {
			endpointSlices[i] = slice.(*discovery.EndpointSlice)
		}
		endpointsData := negtypes.EndpointsDataFromEndpointSlices(endpointSlices)
		targetMap, endpointPodMap, err = s.endpointsCalculator.CalculateEndpoints(endpointsData, currentMap)
	} else {
		ep, exists, err := s.endpointLister.Get(
			&apiv1.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Name:      s.Name,
					Namespace: s.Namespace,
				},
			},
		)
		if err != nil {
			return err
		}
		if !exists {
			klog.Warningf("Endpoint %s/%s does not exist. Skipping NEG sync", s.Namespace, s.Name)
			return nil
		}
		endpointsData := negtypes.EndpointsDataFromEndpoints(ep.(*apiv1.Endpoints))
		targetMap, endpointPodMap, err = s.endpointsCalculator.CalculateEndpoints(endpointsData, currentMap)
	}

	if err != nil {
		err = fmt.Errorf("endpoints calculation error in mode %q, err: %w", s.endpointsCalculator.Mode(), err)
		return err
	}
	s.logStats(targetMap, "desired NEG endpoints")

	// Calculate the endpoints to add and delete to transform the current state to desire state
	addEndpoints, removeEndpoints := calculateNetworkEndpointDifference(targetMap, currentMap)
	// Calculate Pods that are already in the NEG
	_, committedEndpoints := calculateNetworkEndpointDifference(addEndpoints, targetMap)
	// Filter out the endpoints with existing transaction
	// This mostly happens when transaction entry require reconciliation but the transaction is still progress
	// e.g. endpoint A is in the process of adding to NEG N, and the new desire state is not to have A in N.
	// This ensures the endpoint that requires reconciliation to wait till the existing transaction to complete.
	filterEndpointByTransaction(addEndpoints, s.transactions)
	filterEndpointByTransaction(removeEndpoints, s.transactions)
	// filter out the endpoints that are in transaction
	filterEndpointByTransaction(committedEndpoints, s.transactions)

	if s.needCommit() {
		s.commitPods(committedEndpoints, endpointPodMap)
	}

	if len(addEndpoints) == 0 && len(removeEndpoints) == 0 {
		klog.V(4).Infof("No endpoint change for %s/%s, skip syncing NEG. ", s.Namespace, s.Name)
		return nil
	}
	s.logEndpoints(addEndpoints, "adding endpoint")
	s.logEndpoints(removeEndpoints, "removing endpoint")

	// set err instead of returning directly so that synced condition on neg crd is properly updated in defer
	err = s.syncNetworkEndpoints(addEndpoints, removeEndpoints)
	return err
}

// ensureNetworkEndpointGroups ensures NEGs are created and configured correctly in the corresponding zones.
func (s *transactionSyncer) ensureNetworkEndpointGroups() error {
	var err error
	// NEGs should be created in zones with candidate nodes only.
	zones, err := s.zoneGetter.ListZones(negtypes.NodePredicateForEndpointCalculatorMode(s.EpCalculatorMode))
	if err != nil {
		return err
	}

	var errList []error
	var negObjRefs []negv1beta1.NegObjectReference
	for _, zone := range zones {
		var negObj negv1beta1.NegObjectReference
		negObj, err = ensureNetworkEndpointGroup(
			s.Namespace,
			s.Name,
			s.NegSyncerKey.NegName,
			zone,
			s.NegSyncerKey.String(),
			s.kubeSystemUID,
			fmt.Sprint(s.NegSyncerKey.PortTuple.Port),
			s.NegSyncerKey.NegType,
			s.cloud,
			s.serviceLister,
			s.recorder,
			s.NegSyncerKey.GetAPIVersion(),
			s.customName,
		)
		if err != nil {
			errList = append(errList, err)
		}

		if s.svcNegClient != nil && err == nil {
			negObjRefs = append(negObjRefs, negObj)
		}
	}

	s.updateInitStatus(negObjRefs, errList)
	return utilerrors.NewAggregate(errList)
}

// syncNetworkEndpoints spins off go routines to execute NEG operations
func (s *transactionSyncer) syncNetworkEndpoints(addEndpoints, removeEndpoints map[string]negtypes.NetworkEndpointSet) error {
	syncFunc := func(endpointMap map[string]negtypes.NetworkEndpointSet, operation transactionOp) error {
		for zone, endpointSet := range endpointMap {
			if endpointSet.Len() == 0 {
				klog.V(2).Infof("0 endpoint for %v operation for %s in NEG %s at %s. Skipping", attachOp, s.NegSyncerKey.String(), s.NegSyncerKey.NegName, zone)
				continue
			}

			batch, err := makeEndpointBatch(endpointSet, s.NegType)
			if err != nil {
				return err
			}

			transEntry := transactionEntry{
				Operation: operation,
				Zone:      zone,
			}

			// Insert networkEndpoint into transaction table
			for networkEndpoint := range batch {
				s.transactions.Put(networkEndpoint, transEntry)
			}

			if operation == attachOp {
				s.attachNetworkEndpoints(zone, batch)
			}
			if operation == detachOp {
				s.detachNetworkEndpoints(zone, batch)
			}
		}
		return nil
	}

	if err := syncFunc(addEndpoints, attachOp); err != nil {
		return err
	}

	if err := syncFunc(removeEndpoints, detachOp); err != nil {
		return err
	}
	return nil
}

// attachNetworkEndpoints creates go routine to run operations for attaching network endpoints
func (s *transactionSyncer) attachNetworkEndpoints(zone string, networkEndpointMap map[negtypes.NetworkEndpoint]*composite.NetworkEndpoint) {
	klog.V(2).Infof("Attaching %d endpoint(s) for %s in NEG %s at %s.", len(networkEndpointMap), s.NegSyncerKey.String(), s.NegSyncerKey.NegName, zone)
	go s.operationInternal(attachOp, zone, networkEndpointMap)
}

// detachNetworkEndpoints creates go routine to run operations for detaching network endpoints
func (s *transactionSyncer) detachNetworkEndpoints(zone string, networkEndpointMap map[negtypes.NetworkEndpoint]*composite.NetworkEndpoint) {
	klog.V(2).Infof("Detaching %d endpoint(s) for %s in NEG %s at %s.", len(networkEndpointMap), s.NegSyncerKey.String(), s.NegSyncerKey.NegName, zone)
	go s.operationInternal(detachOp, zone, networkEndpointMap)
}

// operationInternal executes NEG API call and commits the transactions
// It will record events when operations are completed
// If error occurs or any transaction entry requires reconciliation, it will trigger resync
func (s *transactionSyncer) operationInternal(operation transactionOp, zone string, networkEndpointMap map[negtypes.NetworkEndpoint]*composite.NetworkEndpoint) {
	var err error
	start := time.Now()
	networkEndpoints := []*composite.NetworkEndpoint{}
	for _, ne := range networkEndpointMap {
		networkEndpoints = append(networkEndpoints, ne)
	}

	if operation == attachOp {
		err = s.cloud.AttachNetworkEndpoints(s.NegSyncerKey.NegName, zone, networkEndpoints, s.NegSyncerKey.GetAPIVersion())
	}
	if operation == detachOp {
		err = s.cloud.DetachNetworkEndpoints(s.NegSyncerKey.NegName, zone, networkEndpoints, s.NegSyncerKey.GetAPIVersion())
	}

	if err == nil {
		s.recordEvent(apiv1.EventTypeNormal, operation.String(), fmt.Sprintf("%s %d network endpoint(s) (NEG %q in zone %q)", operation.String(), len(networkEndpointMap), s.NegSyncerKey.NegName, zone))
	} else {
		s.recordEvent(apiv1.EventTypeWarning, operation.String()+"Failed", fmt.Sprintf("Failed to %s %d network endpoint(s) (NEG %q in zone %q): %v", operation.String(), len(networkEndpointMap), s.NegSyncerKey.NegName, zone, err))
	}

	// WARNING: commitTransaction must be called at last for analyzing the operation result
	s.commitTransaction(err, networkEndpointMap)
	metrics.PublishNegOperationMetrics(operation.String(), string(s.NegSyncerKey.NegType), string(s.NegSyncerKey.GetAPIVersion()), err, len(networkEndpointMap), start)
}

func (s *transactionSyncer) recordEvent(eventType, reason, eventDesc string) {
	if svc := getService(s.serviceLister, s.Namespace, s.Name); svc != nil {
		s.recorder.Eventf(svc, eventType, reason, eventDesc)
	}
}

// commitTransaction commits the transactions for the input endpoints.
// It will trigger syncer retry in the following conditions:
// 1. Any of the transaction committed needed to be reconciled
// 2. Input error was not nil
func (s *transactionSyncer) commitTransaction(err error, networkEndpointMap map[negtypes.NetworkEndpoint]*composite.NetworkEndpoint) {
	s.syncLock.Lock()
	defer s.syncLock.Unlock()

	// If error is not nil, trigger backoff retry
	// If any transaction needs reconciliation, trigger resync.
	// needRetry indicates if the transaction needs to backoff and retry
	needRetry := false

	if err != nil {
		// Trigger NEG initialization if error occurs
		// This is to prevent if the NEG object is deleted or misconfigured by user
		s.needInit = true
		needRetry = true
	}

	for networkEndpoint := range networkEndpointMap {
		_, ok := s.transactions.Get(networkEndpoint)
		// clear transaction
		if !ok {
			klog.Errorf("Endpoint %q was not found in the transaction table.", networkEndpoint)
			continue
		}
		s.transactions.Delete(networkEndpoint)
	}

	if needRetry {
		if retryErr := s.retry.Retry(); retryErr != nil {
			s.recordEvent(apiv1.EventTypeWarning, "RetryFailed", fmt.Sprintf("Failed to retry NEG sync for %q: %v", s.NegSyncerKey.String(), retryErr))
		}
		return
	}
	s.retry.Reset()
	// always trigger Sync to commit pods
	s.syncer.Sync()
}

// needCommit determines if commitPods need to be invoked.
func (s *transactionSyncer) needCommit() bool {
	// commitPods will be a no-op in case of VM_IP NEGs, but skip it to avoid printing non-relevant warning logs.
	return s.NegType != negtypes.VmIpEndpointType
}

// commitPods groups the endpoints by zone and signals the readiness reflector to poll pods of the NEG
func (s *transactionSyncer) commitPods(endpointMap map[string]negtypes.NetworkEndpointSet, endpointPodMap negtypes.EndpointPodMap) {
	for zone, endpointSet := range endpointMap {
		zoneEndpointMap := negtypes.EndpointPodMap{}
		for _, endpoint := range endpointSet.List() {
			podName, ok := endpointPodMap[endpoint]
			if !ok {
				klog.Warningf("Endpoint %v is not included in the endpointPodMap %v", endpoint, endpointPodMap)
				continue
			}
			zoneEndpointMap[endpoint] = podName
		}
		s.reflector.CommitPods(s.NegSyncerKey, s.NegSyncerKey.NegName, zone, zoneEndpointMap)
	}
}

// filterEndpointByTransaction removes the all endpoints from endpoint map if they exists in the transaction table
func filterEndpointByTransaction(endpointMap map[string]negtypes.NetworkEndpointSet, table networkEndpointTransactionTable) {
	for _, endpointSet := range endpointMap {
		for _, endpoint := range endpointSet.List() {
			if entry, ok := table.Get(endpoint); ok {
				klog.V(2).Infof("Endpoint %q is removed from the endpoint set as transaction %v still exists.", endpoint, entry)
				endpointSet.Delete(endpoint)
			}
		}
	}
}

// mergeTransactionIntoZoneEndpointMap merges the ongoing transaction into the endpointMap.
// This converts the existing endpointMap to the state when all transactions completed
func mergeTransactionIntoZoneEndpointMap(endpointMap map[string]negtypes.NetworkEndpointSet, transactions networkEndpointTransactionTable) {
	for _, endpointKey := range transactions.Keys() {
		entry, ok := transactions.Get(endpointKey)
		// If called in syncInternal, as the transaction table
		if !ok {
			klog.V(2).Infof("Transaction entry of key %q was not found.", endpointKey)
			continue
		}
		// Add endpoints in attach transaction
		if entry.Operation == attachOp {
			if _, ok := endpointMap[entry.Zone]; !ok {
				endpointMap[entry.Zone] = negtypes.NewNetworkEndpointSet()
			}
			endpointMap[entry.Zone].Insert(endpointKey)
		}
		// Remove endpoints in detach transaction
		if entry.Operation == detachOp {
			if _, ok := endpointMap[entry.Zone]; !ok {
				continue
			}
			endpointMap[entry.Zone].Delete(endpointKey)
		}
	}
	return
}

// logStats logs aggregated stats of the input endpointMap
func (s *transactionSyncer) logStats(endpointMap map[string]negtypes.NetworkEndpointSet, desc string) {
	stats := []string{}
	for zone, endpointSet := range endpointMap {
		stats = append(stats, fmt.Sprintf("%d endpoints in zone %q", endpointSet.Len(), zone))
	}
	klog.V(3).Infof("For NEG %q, %s: %s.", s.NegSyncerKey.NegName, desc, strings.Join(stats, ","))
}

// logEndpoints logs individual endpoint in the input endpointMap
func (s *transactionSyncer) logEndpoints(endpointMap map[string]negtypes.NetworkEndpointSet, desc string) {
	klog.V(3).Infof("For NEG %q, %s: %+v", s.NegSyncerKey.NegName, desc, endpointMap)
}

// updateInitStatus queries the k8s api server for the current NEG CR and updates the Initialized condition and neg objects as appropriate.
// If neg client is nil, will return immediately
func (s *transactionSyncer) updateInitStatus(negObjRefs []negv1beta1.NegObjectReference, errList []error) {
	if s.svcNegClient == nil {
		return
	}

	origNeg, err := getNegFromStore(s.svcNegLister, s.Namespace, s.NegSyncerKey.NegName)
	if err != nil {
		klog.Errorf("Error updating init status for neg %s, failed getting neg from store: %s", s.NegSyncerKey.NegName, err)
		return
	}

	neg := origNeg.DeepCopy()

	if len(negObjRefs) != 0 {
		neg.Status.NetworkEndpointGroups = negObjRefs
	}

	initializedCondition := getInitializedCondition(utilerrors.NewAggregate(errList))
	finalCondition := ensureCondition(neg, initializedCondition)
	metrics.PublishNegInitializationMetrics(finalCondition.LastTransitionTime.Sub(origNeg.GetCreationTimestamp().Time))

	_, err = patchNegStatus(s.svcNegClient, origNeg.Status, neg.Status, s.Namespace, s.NegSyncerKey.NegName)
	if err != nil {
		klog.Errorf("Error updating Neg CR %s : %s", s.NegSyncerKey.NegName, err)
	}
}

// updateStatus will update the Synced condition as needed on the corresponding neg cr. If the Initialized condition or NetworkEndpointGroups are missing, needInit will be set to true. LastSyncTime will be updated as well.
func (s *transactionSyncer) updateStatus(syncErr error) {
	if s.svcNegClient == nil {
		return
	}
	origNeg, err := getNegFromStore(s.svcNegLister, s.Namespace, s.NegSyncerKey.NegName)
	if err != nil {
		klog.Errorf("Error updating status for neg %s, failed getting neg from store: %s", s.NegSyncerKey.NegName, err)
		return
	}
	neg := origNeg.DeepCopy()

	ts := metav1.Now()
	if _, _, exists := findCondition(neg.Status.Conditions, negv1beta1.Initialized); !exists {
		s.needInit = true
	}

	ensureCondition(neg, getSyncedCondition(syncErr))
	neg.Status.LastSyncTime = ts

	if len(neg.Status.NetworkEndpointGroups) == 0 {
		s.needInit = true
	}

	_, err = patchNegStatus(s.svcNegClient, origNeg.Status, neg.Status, s.Namespace, s.NegSyncerKey.NegName)
	if err != nil {
		klog.Errorf("Error updating Neg CR %s : %s", s.NegSyncerKey.NegName, err)
	}
}

// getNegFromStore returns the neg associated with the provided namespace and neg name if it exists otherwise throws an error
func getNegFromStore(svcNegLister cache.Indexer, namespace, negName string) (*negv1beta1.ServiceNetworkEndpointGroup, error) {
	n, exists, err := svcNegLister.GetByKey(fmt.Sprintf("%s/%s", namespace, negName))
	if err != nil {
		return nil, fmt.Errorf("Error getting neg %s/%s from cache: %w", namespace, negName, err)
	}
	if !exists {
		return nil, fmt.Errorf("neg %s/%s is not in store", namespace, negName)
	}

	return n.(*negv1beta1.ServiceNetworkEndpointGroup), nil
}

// patchNegStatus patches the specified NegCR status with the provided new status
func patchNegStatus(svcNegClient svcnegclient.Interface, oldStatus, newStatus negv1beta1.ServiceNetworkEndpointGroupStatus, namespace, negName string) (*negv1beta1.ServiceNetworkEndpointGroup, error) {
	patchBytes, err := patch.MergePatchBytes(negv1beta1.ServiceNetworkEndpointGroup{Status: oldStatus}, negv1beta1.ServiceNetworkEndpointGroup{Status: newStatus})
	if err != nil {
		return nil, fmt.Errorf("failed to prepare patch bytes: %w", err)
	}

	return svcNegClient.NetworkingV1beta1().ServiceNetworkEndpointGroups(namespace).Patch(context.Background(), negName, types.MergePatchType, patchBytes, metav1.PatchOptions{})
}

// ensureCondition will update the condition on the neg object if necessary
func ensureCondition(neg *negv1beta1.ServiceNetworkEndpointGroup, expectedCondition negv1beta1.Condition) negv1beta1.Condition {
	condition, index, exists := findCondition(neg.Status.Conditions, expectedCondition.Type)
	if !exists {
		neg.Status.Conditions = append(neg.Status.Conditions, expectedCondition)
		return expectedCondition
	}

	if condition.Status == expectedCondition.Status {
		expectedCondition.LastTransitionTime = condition.LastTransitionTime
	}

	neg.Status.Conditions[index] = expectedCondition
	return expectedCondition
}

// getSyncedCondition returns the expected synced condition based on given error
func getSyncedCondition(err error) negv1beta1.Condition {
	if err != nil {
		return negv1beta1.Condition{
			Type:               negv1beta1.Synced,
			Status:             corev1.ConditionFalse,
			Reason:             negtypes.NegSyncFailed,
			LastTransitionTime: metav1.Now(),
			Message:            err.Error(),
		}
	}

	return negv1beta1.Condition{
		Type:               negv1beta1.Synced,
		Status:             corev1.ConditionTrue,
		Reason:             negtypes.NegSyncSuccessful,
		LastTransitionTime: metav1.Now(),
	}
}

// getInitializedCondition returns the expected initialized condition based on given error
func getInitializedCondition(err error) negv1beta1.Condition {
	if err != nil {
		return negv1beta1.Condition{
			Type:               negv1beta1.Initialized,
			Status:             corev1.ConditionFalse,
			Reason:             negtypes.NegInitializationFailed,
			LastTransitionTime: metav1.Now(),
			Message:            err.Error(),
		}
	}

	return negv1beta1.Condition{
		Type:               negv1beta1.Initialized,
		Status:             corev1.ConditionTrue,
		Reason:             negtypes.NegInitializationSuccessful,
		LastTransitionTime: metav1.Now(),
	}
}

// findCondition finds a condition in the given list of conditions that has the type conditionType and returns the condition and its index.
// If no condition is found, an empty condition, -1 and false will be returned to indicate the condition does not exist.
func findCondition(conditions []negv1beta1.Condition, conditionType string) (negv1beta1.Condition, int, bool) {
	for i, c := range conditions {
		if c.Type == conditionType {
			return c, i, true
		}
	}

	return negv1beta1.Condition{}, -1, false
}
