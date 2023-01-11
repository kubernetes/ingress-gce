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
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"google.golang.org/api/googleapi"
	apiv1 "k8s.io/api/core/v1"
	corev1 "k8s.io/api/core/v1"
	discovery "k8s.io/api/discovery/v1"
	"k8s.io/ingress-gce/pkg/utils/endpointslices"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
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
	"k8s.io/klog/v2"
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

	logger klog.Logger

	// inError indicates if the syncer is in any of 4 error scenarios
	// 1. Endpoint counts from EPS is different from calculated endpoint list
	// 2. EndpontSlice has missing or invalid data
	// 3. Attach/Detach EP fails due to incorrect batch information
	// 4. Endpoint count from EPS or calculated endpoint list is 0
	// Need to grab syncLock first for any reads or writes based on this value
	inError bool
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
	enableEndpointSlices bool,
	log klog.Logger) negtypes.NegSyncer {

	logger := log.WithName("Syncer").WithValues("service", klog.KRef(negSyncerKey.Namespace, negSyncerKey.Name), "negName", negSyncerKey.NegName)

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
		inError:              false,
		logger:               logger,
	}
	// Syncer implements life cycle logic
	syncer := newSyncer(negSyncerKey, serviceLister, recorder, ts, logger)
	// transactionSyncer needs syncer interface for internals
	ts.syncer = syncer
	ts.retry = NewDelayRetryHandler(func() { syncer.Sync() }, NewExponentialBackendOffHandler(maxRetries, minRetryDelay, maxRetryDelay))
	return syncer
}

func GetEndpointsCalculator(nodeLister, podLister cache.Indexer, zoneGetter negtypes.ZoneGetter, syncerKey negtypes.NegSyncerKey, mode negtypes.EndpointsCalculatorMode, logger klog.Logger) negtypes.NetworkEndpointsCalculator {
	serviceKey := strings.Join([]string{syncerKey.Name, syncerKey.Namespace}, "/")
	if syncerKey.NegType == negtypes.VmIpEndpointType {
		nodeLister := listers.NewNodeLister(nodeLister)
		switch mode {
		case negtypes.L4LocalMode:
			return NewLocalL4ILBEndpointsCalculator(nodeLister, zoneGetter, serviceKey, logger)
		default:
			return NewClusterL4ILBEndpointsCalculator(nodeLister, zoneGetter, serviceKey, logger)
		}
	}
	return NewL7EndpointsCalculator(zoneGetter, podLister, syncerKey.PortTuple.Name,
		syncerKey.SubsetLabels, syncerKey.NegType, logger)
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

	start := time.Now()
	err := s.syncInternalImpl()

	s.updateStatus(err)
	metrics.PublishNegSyncMetrics(string(s.NegSyncerKey.NegType), string(s.endpointsCalculator.Mode()), err, start)
	return err
}

func (s *transactionSyncer) syncInternalImpl() error {
	// TODO(cheungdavid): for now we reset the boolean so it is a no-op, but
	// in the future, it will be used to trigger degraded mode if the syncer is in error state.
	if s.inErrorState() {
		s.resetErrorState()
	}

	if s.needInit || s.isZoneChange() {
		if err := s.ensureNetworkEndpointGroups(); err != nil {
			return err
		}
		s.needInit = false
	}

	if s.syncer.IsStopped() || s.syncer.IsShuttingDown() {
		s.logger.V(3).Info("Skip syncing NEG", "negSyncerKey", s.NegSyncerKey.String())
		return nil
	}
	s.logger.V(2).Info("Sync NEG", "negSyncerKey", s.NegSyncerKey.String(), "endpointsCalculatorMode", s.endpointsCalculator.Mode())

	currentMap, err := retrieveExistingZoneNetworkEndpointMap(s.NegSyncerKey.NegName, s.zoneGetter, s.cloud, s.NegSyncerKey.GetAPIVersion(), s.endpointsCalculator.Mode())
	if err != nil {
		return err
	}
	s.logStats(currentMap, "current NEG endpoints")

	// Merge the current state from cloud with the transaction table together
	// The combined state represents the eventual result when all transactions completed
	mergeTransactionIntoZoneEndpointMap(currentMap, s.transactions, s.logger)
	s.logStats(currentMap, "after in-progress operations have completed, NEG endpoints")

	var targetMap map[string]negtypes.NetworkEndpointSet
	var endpointPodMap negtypes.EndpointPodMap
	var dupCount int

	if s.enableEndpointSlices {
		slices, err := s.endpointSliceLister.ByIndex(endpointslices.EndpointSlicesByServiceIndex, endpointslices.FormatEndpointSlicesServiceKey(s.Namespace, s.Name))
		if err != nil {
			return err
		}
		if len(slices) < 1 {
			s.logger.Error(nil, "Endpoint slices for the service doesn't exist. Skipping NEG sync")
			return nil
		}
		endpointSlices := make([]*discovery.EndpointSlice, len(slices))
		for i, slice := range slices {
			endpointSlices[i] = slice.(*discovery.EndpointSlice)
		}
		endpointsData := negtypes.EndpointsDataFromEndpointSlices(endpointSlices)
		targetMap, endpointPodMap, dupCount, err = s.endpointsCalculator.CalculateEndpoints(endpointsData, currentMap)
		if s.invalidEndpointInfo(endpointsData, endpointPodMap, dupCount) || s.isZoneMissing(targetMap) {
			s.setErrorState()
		}
		if err != nil {
			return fmt.Errorf("endpoints calculation error in mode %q, err: %w", s.endpointsCalculator.Mode(), err)
		}
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
			s.logger.Info("Endpoint does not exist. Skipping NEG sync", "endpoint", klog.KRef(s.Namespace, s.Name))
			return nil
		}
		endpointsData := negtypes.EndpointsDataFromEndpoints(ep.(*apiv1.Endpoints))
		targetMap, endpointPodMap, _, err = s.endpointsCalculator.CalculateEndpoints(endpointsData, currentMap)
		if err != nil {
			return fmt.Errorf("endpoints calculation error in mode %q, err: %w", s.endpointsCalculator.Mode(), err)
		}
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
	filterEndpointByTransaction(addEndpoints, s.transactions, s.logger)
	filterEndpointByTransaction(removeEndpoints, s.transactions, s.logger)
	// filter out the endpoints that are in transaction
	filterEndpointByTransaction(committedEndpoints, s.transactions, s.logger)

	if s.needCommit() {
		s.commitPods(committedEndpoints, endpointPodMap)
	}

	if len(addEndpoints) == 0 && len(removeEndpoints) == 0 {
		s.logger.V(3).Info("No endpoint change. Skip syncing NEG. ", s.Namespace, s.Name)
		return nil
	}
	s.logEndpoints(addEndpoints, "adding endpoint")
	s.logEndpoints(removeEndpoints, "removing endpoint")

	return s.syncNetworkEndpoints(addEndpoints, removeEndpoints)
}

// syncLock must already be acquired before execution
func (s *transactionSyncer) inErrorState() bool {
	return s.inError
}

// syncLock must already be acquired before execution
func (s *transactionSyncer) setErrorState() {
	s.inError = true
}

// syncLock must already be acquired before execution
func (s *transactionSyncer) resetErrorState() {
	s.inError = false
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

// invalidEndpointInfo checks if endpoint information is correct.
// It returns true if any of the following checks fails:
//
//  1. The endpoint count from endpointData doesn't equal to the one from endpointPodMap:
//     endpiontPodMap removes the duplicated endpoints, and dupCount stores the number of duplicated it removed
//     and we compare the endpoint counts with duplicates
//  2. There is at least one endpoint in endpointData with missing nodeName
//  3. The endpoint count from endpointData or the one from endpointPodMap is 0
func (s *transactionSyncer) invalidEndpointInfo(eds []negtypes.EndpointsData, endpointPodMap negtypes.EndpointPodMap, dupCount int) bool {
	// Endpoint count from EndpointPodMap
	countFromPodMap := len(endpointPodMap) + dupCount
	if countFromPodMap == 0 {
		s.logger.Info("Detected endpoint count from endpointPodMap going to zero", "endpointPodMap", endpointPodMap)
		return true
	}

	// Endpoint count from EndpointData
	countFromEndpointData := 0
	for _, ed := range eds {
		countFromEndpointData += len(ed.Addresses)
		for _, endpointAddress := range ed.Addresses {
			nodeName := endpointAddress.NodeName
			if nodeName == nil || len(*nodeName) == 0 {
				s.logger.Info("Detected error when checking nodeName", "endpoint", endpointAddress, "endpointData", eds)
				return true
			}
		}
	}
	if countFromEndpointData == 0 {
		s.logger.Info("Detected endpoint count from endpointData going to zero", "endpointData", eds)
		return true
	}

	if countFromEndpointData != countFromPodMap {
		s.logger.Info("Detected error when comparing endpoint counts", "endpointData", eds, "endpointPodMap", endpointPodMap, "dupCount", dupCount)
		return true
	}
	return false
}

// isZoneMissing returns true if there is invalid(empty) zone in zoneNetworkEndpointMap
func (s *transactionSyncer) isZoneMissing(zoneNetworkEndpointMap map[string]negtypes.NetworkEndpointSet) bool {
	if _, isPresent := zoneNetworkEndpointMap[""]; isPresent {
		s.logger.Info("Detected error when checking missing zone", "zoneNetworkEndpointMap", zoneNetworkEndpointMap)
		return true
	}
	return false
}

func (s *transactionSyncer) isInvalidEPBatch(err error, operation transactionOp, networkEndpoints []*composite.NetworkEndpoint) bool {
	apiErr, ok := err.(*googleapi.Error)
	if !ok {
		s.logger.Info("Detected error when parsing batch request error", "operation", operation, "error", err)
		return true
	}
	errCode := apiErr.Code
	if errCode == http.StatusBadRequest {
		s.logger.Info("Detected error when sending endpoint batch information", "operation", operation, "errorCode", errCode)
		return true
	}
	return false
}

// syncNetworkEndpoints spins off go routines to execute NEG operations
func (s *transactionSyncer) syncNetworkEndpoints(addEndpoints, removeEndpoints map[string]negtypes.NetworkEndpointSet) error {
	syncFunc := func(endpointMap map[string]negtypes.NetworkEndpointSet, operation transactionOp) error {
		for zone, endpointSet := range endpointMap {
			if endpointSet.Len() == 0 {
				s.logger.V(2).Info("0 endpoints in the endpoint list. Skipping operation", "operation", attachOp, "negSyncerKey", s.NegSyncerKey.String(), "zone", zone)
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
	s.logger.V(2).Info("Attaching endpoints to NEG.", "countOfEndpointsBeingAttached", len(networkEndpointMap), "negSyncerKey", s.NegSyncerKey.String(), "zone", zone)
	go s.operationInternal(attachOp, zone, networkEndpointMap)
}

// detachNetworkEndpoints creates go routine to run operations for detaching network endpoints
func (s *transactionSyncer) detachNetworkEndpoints(zone string, networkEndpointMap map[negtypes.NetworkEndpoint]*composite.NetworkEndpoint) {
	s.logger.V(2).Info("Detaching endpoints from NEG.", "countOfEndpointsBeingDetached", len(networkEndpointMap), "negSyncerKey", s.NegSyncerKey.String(), "zone", zone)
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
		if s.isInvalidEPBatch(err, operation, networkEndpoints) {
			s.setErrorState()
		}
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
			s.logger.Error(nil, "Endpoint was not found in the transaction table.", "endpoint", networkEndpoint)
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
				s.logger.Error(nil, "Endpoint is not included in the endpointPodMap", "endpoint", endpoint, "endpointPodMap", endpointPodMap)
				continue
			}
			zoneEndpointMap[endpoint] = podName
		}
		s.reflector.CommitPods(s.NegSyncerKey, s.NegSyncerKey.NegName, zone, zoneEndpointMap)
	}
}

// isZoneChange returns true if a zone change has occurred by comparing which zones the nodes are in
// with the zones that NEGs are initialized in
func (s *transactionSyncer) isZoneChange() bool {
	negCR, err := getNegFromStore(s.svcNegLister, s.Namespace, s.NegSyncerKey.NegName)
	if err != nil {
		s.logger.Error(err, "unable to retrieve neg from the store", "neg", klog.KRef(s.Namespace, s.NegName))
		return false
	}

	existingZones := sets.NewString()
	for _, ref := range negCR.Status.NetworkEndpointGroups {
		id, err := cloud.ParseResourceURL(ref.SelfLink)
		if err != nil {
			s.logger.Error(err, "unable to parse selflink", "selfLink", ref.SelfLink)
			continue
		}
		existingZones.Insert(id.Key.Zone)
	}

	zones, err := s.zoneGetter.ListZones(negtypes.NodePredicateForEndpointCalculatorMode(s.EpCalculatorMode))
	if err != nil {
		s.logger.Error(err, "unable to list zones")
		return false
	}
	currZones := sets.NewString(zones...)

	return !currZones.Equal(existingZones)
}

// filterEndpointByTransaction removes the all endpoints from endpoint map if they exists in the transaction table
func filterEndpointByTransaction(endpointMap map[string]negtypes.NetworkEndpointSet, table networkEndpointTransactionTable, logger klog.Logger) {
	for _, endpointSet := range endpointMap {
		for _, endpoint := range endpointSet.List() {
			if entry, ok := table.Get(endpoint); ok {
				logger.V(2).Info("Endpoint is removed from the endpoint set as transaction still exists.", "endpoint", endpoint, "transactionEntry", entry)
				endpointSet.Delete(endpoint)
			}
		}
	}
}

// mergeTransactionIntoZoneEndpointMap merges the ongoing transaction into the endpointMap.
// This converts the existing endpointMap to the state when all transactions completed
func mergeTransactionIntoZoneEndpointMap(endpointMap map[string]negtypes.NetworkEndpointSet, transactions networkEndpointTransactionTable, logger klog.Logger) {
	for _, endpointKey := range transactions.Keys() {
		entry, ok := transactions.Get(endpointKey)
		// If called in syncInternal, as the transaction table
		if !ok {
			logger.V(2).Info("Transaction entry of key was not found.", "endpointKey", endpointKey)
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
	var stats []interface{}
	stats = append(stats, "description", desc)
	for zone, endpointSet := range endpointMap {
		stats = append(stats, zone, fmt.Sprintf("%d endpoints", endpointSet.Len()))
	}
	s.logger.V(3).Info("Stats for NEG", stats...)
}

// logEndpoints logs individual endpoint in the input endpointMap
func (s *transactionSyncer) logEndpoints(endpointMap map[string]negtypes.NetworkEndpointSet, desc string) {
	s.logger.V(3).Info("Endpoints for NEG", "description", desc, "endpointMap", endpointMap)
}

// updateInitStatus queries the k8s api server for the current NEG CR and updates the Initialized condition and neg objects as appropriate.
// If neg client is nil, will return immediately
func (s *transactionSyncer) updateInitStatus(negObjRefs []negv1beta1.NegObjectReference, errList []error) {
	if s.svcNegClient == nil {
		return
	}

	origNeg, err := getNegFromStore(s.svcNegLister, s.Namespace, s.NegSyncerKey.NegName)
	if err != nil {
		s.logger.Error(err, "Error updating init status for neg, failed to get neg from store.")
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
		s.logger.Error(err, "Error updating Neg CR")
	}
}

// updateStatus will update the Synced condition as needed on the corresponding neg cr. If the Initialized condition or NetworkEndpointGroups are missing, needInit will be set to true. LastSyncTime will be updated as well.
func (s *transactionSyncer) updateStatus(syncErr error) {
	if s.svcNegClient == nil {
		return
	}
	origNeg, err := getNegFromStore(s.svcNegLister, s.Namespace, s.NegSyncerKey.NegName)
	if err != nil {
		s.logger.Error(err, "Error updating status for neg, failed to get neg from store")
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
		s.logger.Error(err, "Error updating Neg CR")
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
