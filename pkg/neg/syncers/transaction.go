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
	"sync"

	"fmt"

	"google.golang.org/api/compute/v0.beta"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/klog"
)

type transactionSyncer struct {
	// metadata
	NegSyncerKey
	negName string

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
	transactions transactionTable

	serviceLister  cache.Indexer
	endpointLister cache.Indexer
	recorder       record.EventRecorder
	cloud          negtypes.NetworkEndpointGroupCloud
	zoneGetter     negtypes.ZoneGetter

	// retry handles back off retry for NEG API operations
	retry retryHandler
}

func NewTransactionSyncer(negSyncerKey NegSyncerKey, networkEndpointGroupName string, recorder record.EventRecorder, cloud negtypes.NetworkEndpointGroupCloud, zoneGetter negtypes.ZoneGetter, serviceLister cache.Indexer, endpointLister cache.Indexer) negtypes.NegSyncer {
	// TransactionSyncer implements the syncer core
	ts := &transactionSyncer{
		NegSyncerKey:   negSyncerKey,
		negName:        networkEndpointGroupName,
		needInit:       true,
		transactions:   NewTransactionTable(),
		serviceLister:  serviceLister,
		endpointLister: endpointLister,
		recorder:       recorder,
		cloud:          cloud,
		zoneGetter:     zoneGetter,
	}
	// Syncer implements life cycle logic
	syncer := newSyncer(negSyncerKey, networkEndpointGroupName, serviceLister, recorder, ts)
	// transactionSyncer needs syncer interface for internals
	ts.syncer = syncer
	ts.retry = NewDelayRetryHandler(func() { syncer.Sync() }, NewExponentialBackendOffHandler(maxRetries, minRetryDelay, maxRetryDelay))
	return syncer
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
	if s.needInit {
		if err := s.ensureNetworkEndpointGroups(); err != nil {
			return err
		}
	}

	if s.syncer.IsStopped() || s.syncer.IsShuttingDown() {
		klog.V(4).Infof("Skip syncing NEG %q for %s.", s.negName, s.NegSyncerKey.String())
		return nil
	}
	klog.V(2).Infof("Sync NEG %q for %s.", s.negName, s.NegSyncerKey.String())

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

	targetMap, err := toZoneNetworkEndpointMap(ep.(*apiv1.Endpoints), s.zoneGetter, s.TargetPort)
	if err != nil {
		return err
	}

	currentMap, err := retrieveExistingZoneNetworkEndpointMap(s.negName, s.zoneGetter, s.cloud)
	if err != nil {
		return err
	}

	// Merge the current state from cloud with the transaction table together
	// The combined state represents the eventual result when all transactions completed
	mergeTransactionIntoZoneEndpointMap(currentMap, s.transactions)
	// Find transaction entries that needs to be reconciled
	reconcileTransactions(targetMap, s.transactions)
	// Calculate the endpoints to add and delete to transform the current state to desire state
	addEndpoints, removeEndpoints := calculateDifference(targetMap, currentMap)
	// Filter out the endpoints with existing transaction
	// This mostly happens when transaction entry require reconciliation but the transaction is still progress
	// e.g. endpoint A is in the process of adding to NEG N, and the new desire state is not to have A in N.
	// This ensures the endpoint that requires reconciliation to wait till the existing transaction to complete.
	filterEndpointByTransaction(addEndpoints, s.transactions)
	filterEndpointByTransaction(removeEndpoints, s.transactions)

	if len(addEndpoints) == 0 && len(removeEndpoints) == 0 {
		klog.V(4).Infof("No endpoint change for %s/%s, skip syncing NEG. ", s.Namespace, s.Name)
		return nil
	}

	return s.syncNetworkEndpoints(addEndpoints, removeEndpoints)
}

// ensureNetworkEndpointGroups ensures NEGs are created and configured correctly in the corresponding zones.
func (s *transactionSyncer) ensureNetworkEndpointGroups() error {
	var err error
	zones, err := s.zoneGetter.ListZones()
	if err != nil {
		return err
	}

	var errList []error
	for _, zone := range zones {
		if err := ensureNetworkEndpointGroup(s.Namespace, s.Name, s.negName, zone, s.NegSyncerKey.String(), s.cloud, s.serviceLister, s.recorder); err != nil {
			errList = append(errList, err)
		}
	}
	return utilerrors.NewAggregate(errList)
}

// syncNetworkEndpoints spins off go routines to execute NEG operations
func (s *transactionSyncer) syncNetworkEndpoints(addEndpoints, removeEndpoints map[string]sets.String) error {
	syncFunc := func(endpointMap map[string]sets.String, operation transactionOp) error {
		for zone, endpointSet := range endpointMap {
			if endpointSet.Len() == 0 {
				klog.V(2).Infof("0 endpoint for %v operation for %s in NEG %s at %s. Skipping", attachOp, s.NegSyncerKey.String(), s.negName, zone)
				continue
			}

			batch, err := makeEndpointBatch(endpointSet)
			if err != nil {
				return err
			}

			transEntry := transactionEntry{
				Operation:     operation,
				NeedReconcile: false,
				Zone:          zone,
			}

			// Insert encodedEndpoint into transaction table
			for encodedEndpoint := range batch {
				s.transactions.Put(encodedEndpoint, transEntry)
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
func (s *transactionSyncer) attachNetworkEndpoints(zone string, networkEndpointMap map[string]*compute.NetworkEndpoint) {
	klog.V(2).Infof("Attaching %d endpoint(s) for %s in NEG %s at %s.", len(networkEndpointMap), s.NegSyncerKey.String(), s.negName, zone)
	go s.operationInternal(attachOp, zone, networkEndpointMap)
}

// detachNetworkEndpoints creates go routine to run operations for detaching network endpoints
func (s *transactionSyncer) detachNetworkEndpoints(zone string, networkEndpointMap map[string]*compute.NetworkEndpoint) {
	klog.V(2).Infof("Detaching %d endpoint(s) for %s in NEG %s at %s.", len(networkEndpointMap), s.NegSyncerKey.String(), s.negName, zone)
	go s.operationInternal(detachOp, zone, networkEndpointMap)
}

// operationInternal executes NEG API call and commits the transactions
// It will record events when operations are completed
// If error occurs or any transaction entry requires reconciliation, it will trigger resync
func (s *transactionSyncer) operationInternal(operation transactionOp, zone string, networkEndpointMap map[string]*compute.NetworkEndpoint) {
	var err error
	networkEndpoints := []*compute.NetworkEndpoint{}
	for _, ne := range networkEndpointMap {
		networkEndpoints = append(networkEndpoints, ne)
	}

	if operation == attachOp {
		err = s.cloud.AttachNetworkEndpoints(s.negName, zone, networkEndpoints)
	}
	if operation == detachOp {
		err = s.cloud.DetachNetworkEndpoints(s.negName, zone, networkEndpoints)
	}

	if err == nil {
		s.recordEvent(apiv1.EventTypeNormal, operation.String(), fmt.Sprintf("%s %d network endpoint(s) (NEG %q in zone %q)", operation.String(), len(networkEndpointMap), s.negName, zone))
	} else {
		s.recordEvent(apiv1.EventTypeWarning, operation.String()+"Failed", fmt.Sprintf("Failed to %s %d network endpoint(s) (NEG %q in zone %q): %v", operation.String(), len(networkEndpointMap), s.negName, zone, err))
	}

	// WARNING: commitTransaction must be called at last for analyzing the operation result
	s.commitTransaction(err, networkEndpointMap)
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
func (s *transactionSyncer) commitTransaction(err error, networkEndpointMap map[string]*compute.NetworkEndpoint) {
	s.syncLock.Lock()
	defer s.syncLock.Unlock()

	// If error is not nil, trigger backoff retry
	// If any transaction needs reconciliation, trigger resync.
	// needRetry indicates if the transaction needs to backoff and retry
	needRetry := false
	// needSync indicates if the transaction needs to trigger resync immediately
	needSync := false

	if err != nil {
		// Trigger NEG initialization if error occurs
		// This is to prevent if the NEG object is deleted or misconfigured by user
		s.needInit = true
		needRetry = true
	}

	for encodedEndpoint := range networkEndpointMap {
		entry, ok := s.transactions.Get(encodedEndpoint)
		if !ok {
			klog.Errorf("Endpoint %q was not found in the transaction table.", encodedEndpoint)
			needSync = true
			continue
		}
		if entry.NeedReconcile == true {
			klog.Errorf("Endpoint %q in NEG %q need to be reconciled.", encodedEndpoint, s.NegSyncerKey.String())
			needSync = true
		}
		s.transactions.Delete(encodedEndpoint)
	}

	if needSync {
		s.syncer.Sync()
		return
	}

	if needRetry {
		if retryErr := s.retry.Retry(); retryErr != nil {
			s.recordEvent(apiv1.EventTypeWarning, "RetryFailed", fmt.Sprintf("Failed to retry NEG sync for %q: %v", s.NegSyncerKey.String(), retryErr))
		}
		return
	}

	s.retry.Reset()
}

// filterEndpointByTransaction removes the all endpoints from endpoint map if they exists in the transaction table
func filterEndpointByTransaction(endpointMap map[string]sets.String, table transactionTable) {
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
func mergeTransactionIntoZoneEndpointMap(endpointMap map[string]sets.String, transactions transactionTable) {
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
				endpointMap[entry.Zone] = sets.NewString()
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

// reconcileTransactions compares the endpoint map with existing transaction entries in transaction table
// if transaction does not cu
func reconcileTransactions(endpointMap map[string]sets.String, transactions transactionTable) {
	// Identify endpoints that should be in NEG but the current transaction does not match the intention
	for zone, endpointSet := range endpointMap {
		for _, endpointKey := range endpointSet.List() {
			entry, ok := transactions.Get(endpointKey)
			// if transaction does not contain endpoints, it will be handled in this sync
			if !ok {
				continue
			}
			needReconcile := false
			// if zone does not match, need to reconcile
			if entry.Zone != zone {
				needReconcile = true
			}
			// if the endpoint entry is in detach transaction but shows up endpointMap
			if entry.Operation == detachOp {
				needReconcile = true
			}

			if needReconcile {
				entry.NeedReconcile = true
				transactions.Put(endpointKey, entry)
			}
		}
	}

	// Identify endpoints that should not to be in NEG but the current transaction does not match the intention
	for _, endpointKey := range transactions.Keys() {
		entry, ok := transactions.Get(endpointKey)
		if !ok {
			continue
		}
		needReconcile := false
		if entry.Operation == attachOp {
			endpointSet, ok := endpointMap[entry.Zone]
			// If zone is not in endpointMap, then the endpoint must not exist in the endpointMap
			if !ok {
				needReconcile = true
			}

			// If the endpoint that is in attach transaction does not exists in the endpointMap,
			// Then it means the endpoint needs to be reconciled after attach operation completes.
			if !endpointSet.Has(endpointKey) {
				needReconcile = true
			}
		}

		if needReconcile {
			entry.NeedReconcile = true
			transactions.Put(endpointKey, entry)
		}
	}

	return
}
