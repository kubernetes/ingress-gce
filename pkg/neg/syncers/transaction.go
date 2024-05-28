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
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"google.golang.org/api/googleapi"
	apiv1 "k8s.io/api/core/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	discovery "k8s.io/api/discovery/v1"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/network"
	"k8s.io/ingress-gce/pkg/utils/endpointslices"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	negv1beta1 "k8s.io/ingress-gce/pkg/apis/svcneg/v1beta1"
	"k8s.io/ingress-gce/pkg/backoff"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/neg/metrics"
	"k8s.io/ingress-gce/pkg/neg/metrics/metricscollector"
	"k8s.io/ingress-gce/pkg/neg/readiness"
	"k8s.io/ingress-gce/pkg/neg/syncers/dualstack"
	"k8s.io/ingress-gce/pkg/neg/syncers/labels"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	svcnegclient "k8s.io/ingress-gce/pkg/svcneg/client/clientset/versioned"
	"k8s.io/ingress-gce/pkg/utils/patch"
	"k8s.io/ingress-gce/pkg/utils/zonegetter"
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
	endpointSliceLister cache.Indexer
	nodeLister          cache.Indexer
	svcNegLister        cache.Indexer
	recorder            record.EventRecorder
	cloud               negtypes.NetworkEndpointGroupCloud
	zoneGetter          *zonegetter.ZoneGetter
	endpointsCalculator negtypes.NetworkEndpointsCalculator

	// retry handles back off retry for NEG API operations
	retry backoff.RetryHandler

	// reflector handles NEG readiness gate and conditions for pods in NEG.
	reflector readiness.Reflector

	//kubeSystemUID used to populate Cluster UID on Neg Description when using NEG CRD
	kubeSystemUID string

	//svcNegClient used to update status on corresponding NEG CRs when not nil
	svcNegClient svcnegclient.Interface

	// customName indicates whether the NEG name is a generated one or custom one
	customName bool

	logger klog.Logger

	// errorState indicates if the syncer is in any of 4 error scenarios
	// 1. Endpoint counts from EPS is different from calculated endpoint list
	// 2. EndpontSlice has missing or invalid data
	// 3. Attach/Detach EP fails due to incorrect batch information
	// 4. Endpoint count from EPS or calculated endpoint list is 0
	// Need to grab syncLock first for any reads or writes based on this value
	errorState bool

	// syncMetricsCollector collect sync related metrics
	syncMetricsCollector metricscollector.SyncerMetricsCollector

	// enableDegradedMode indicates whether we do endpoint calculation using degraded mode procedures
	enableDegradedMode bool
	// enableDegradedModeMetrics indicates whether we enable metrics collection for degraded mode.
	// Degraded mode calculation results will not be used when error state is triggered.
	enableDegradedModeMetrics bool
	// Enables support for Dual-Stack NEGs within the NEG Controller.
	enableDualStackNEG bool

	// podLabelPropagationConfig configures the pod label to be propagated to NEG endpoints
	podLabelPropagationConfig labels.PodLabelPropagationConfig

	dsMigrator *dualstack.Migrator

	// networkInfo contains the network information to use in GCP resources (VPC URL, Subnetwork URL).
	// and the k8s network name (can be used in endpoints calculation).
	networkInfo network.NetworkInfo
}

func NewTransactionSyncer(
	negSyncerKey negtypes.NegSyncerKey,
	recorder record.EventRecorder,
	cloud negtypes.NetworkEndpointGroupCloud,
	zoneGetter *zonegetter.ZoneGetter,
	podLister cache.Indexer,
	serviceLister cache.Indexer,
	endpointSliceLister cache.Indexer,
	nodeLister cache.Indexer,
	svcNegLister cache.Indexer,
	reflector readiness.Reflector,
	epc negtypes.NetworkEndpointsCalculator,
	kubeSystemUID string,
	svcNegClient svcnegclient.Interface,
	syncerMetrics *metricscollector.SyncerMetrics,
	customName bool,
	log klog.Logger,
	lpConfig labels.PodLabelPropagationConfig,
	enableDualStackNEG bool,
	networkInfo network.NetworkInfo,
) negtypes.NegSyncer {

	logger := log.WithName("Syncer").WithValues("service", klog.KRef(negSyncerKey.Namespace, negSyncerKey.Name), "negName", negSyncerKey.NegName)

	// TransactionSyncer implements the syncer core
	ts := &transactionSyncer{
		NegSyncerKey:              negSyncerKey,
		needInit:                  true,
		transactions:              NewTransactionTable(),
		nodeLister:                nodeLister,
		podLister:                 podLister,
		serviceLister:             serviceLister,
		endpointSliceLister:       endpointSliceLister,
		svcNegLister:              svcNegLister,
		recorder:                  recorder,
		cloud:                     cloud,
		zoneGetter:                zoneGetter,
		endpointsCalculator:       epc,
		reflector:                 reflector,
		kubeSystemUID:             kubeSystemUID,
		svcNegClient:              svcNegClient,
		syncMetricsCollector:      syncerMetrics,
		customName:                customName,
		errorState:                false,
		logger:                    logger,
		enableDegradedMode:        flags.F.EnableDegradedMode,
		enableDegradedModeMetrics: flags.F.EnableDegradedModeMetrics,
		enableDualStackNEG:        enableDualStackNEG,
		podLabelPropagationConfig: lpConfig,
		networkInfo:               networkInfo,
	}
	// Syncer implements life cycle logic
	syncer := newSyncer(negSyncerKey, serviceLister, recorder, ts, logger)
	// transactionSyncer needs syncer interface for internals
	ts.syncer = syncer
	ts.retry = backoff.NewDelayRetryHandler(func() { syncer.Sync() }, backoff.NewExponentialBackoffHandler(maxRetries, minRetryDelay, maxRetryDelay))
	ts.dsMigrator = dualstack.NewMigrator(enableDualStackNEG, syncer, negSyncerKey, syncerMetrics, ts, logger)
	return syncer
}

func GetEndpointsCalculator(podLister, nodeLister, serviceLister cache.Indexer, zoneGetter *zonegetter.ZoneGetter, syncerKey negtypes.NegSyncerKey, mode negtypes.EndpointsCalculatorMode, logger klog.Logger, enableDualStackNEG bool, syncMetricsCollector *metricscollector.SyncerMetrics, networkInfo *network.NetworkInfo) negtypes.NetworkEndpointsCalculator {
	serviceKey := strings.Join([]string{syncerKey.Name, syncerKey.Namespace}, "/")
	if syncerKey.NegType == negtypes.VmIpEndpointType {
		nodeLister := listers.NewNodeLister(nodeLister)
		switch mode {
		case negtypes.L4LocalMode:
			return NewLocalL4ILBEndpointsCalculator(nodeLister, zoneGetter, serviceKey, logger, networkInfo)
		default:
			return NewClusterL4ILBEndpointsCalculator(nodeLister, zoneGetter, serviceKey, logger, networkInfo)
		}
	}
	return NewL7EndpointsCalculator(
		zoneGetter,
		podLister,
		nodeLister,
		serviceLister,
		syncerKey,
		logger,
		enableDualStackNEG,
		syncMetricsCollector,
	)
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
	if err != nil {
		if syncErr := negtypes.ClassifyError(err); syncErr.IsErrorState {
			s.logger.Info("Enter degraded mode", "reason", syncErr.Reason)
			if s.enableDegradedMode {
				s.recordEvent(apiv1.EventTypeWarning, "EnterDegradedMode", fmt.Sprintf("Entering degraded mode for NEG %s due to sync err: %v", s.NegSyncerKey.String(), syncErr))
			}
			s.setErrorState()
		}
	}
	s.updateStatus(err)
	metrics.PublishNegSyncMetrics(string(s.NegSyncerKey.NegType), string(s.endpointsCalculator.Mode()), err, start)
	s.syncMetricsCollector.UpdateSyncerStatusInMetrics(s.NegSyncerKey, err, s.inErrorState())
	return err
}

func (s *transactionSyncer) syncInternalImpl() error {
	if s.syncer.IsStopped() || s.syncer.IsShuttingDown() {
		s.logger.V(3).Info("Skip syncing NEG", "negSyncerKey", s.NegSyncerKey.String())
		return nil
	}
	if s.needInit || s.isZoneChange() {
		if err := s.ensureNetworkEndpointGroups(); err != nil {
			return fmt.Errorf("%w: %v", negtypes.ErrNegNotFound, err)
		}
		s.needInit = false
	}
	s.logger.V(2).Info("Sync NEG", "negSyncerKey", s.NegSyncerKey.String(), "endpointsCalculatorMode", s.endpointsCalculator.Mode())

	currentMap, currentPodLabelMap, err := retrieveExistingZoneNetworkEndpointMap(s.NegSyncerKey.NegName, s.zoneGetter, s.cloud, s.NegSyncerKey.GetAPIVersion(), s.endpointsCalculator.Mode(), s.enableDualStackNEG, s.logger)
	if err != nil {
		return fmt.Errorf("%w: %w", negtypes.ErrCurrentNegEPNotFound, err)
	}
	s.logStats(currentMap, "current NEG endpoints")

	// Merge the current state from cloud with the transaction table together
	// The combined state represents the eventual result when all transactions completed
	mergeTransactionIntoZoneEndpointMap(currentMap, s.transactions, s.logger)
	s.logStats(currentMap, "after in-progress operations have completed, NEG endpoints")

	var targetMap map[string]negtypes.NetworkEndpointSet
	var endpointPodMap negtypes.EndpointPodMap
	slices, err := s.endpointSliceLister.ByIndex(endpointslices.EndpointSlicesByServiceIndex, endpointslices.FormatEndpointSlicesServiceKey(s.Namespace, s.Name))
	if err != nil {
		return fmt.Errorf("%w: %v", negtypes.ErrEPSNotFound, err)
	}
	if len(slices) < 1 {
		s.logger.Error(nil, "Endpoint slices for the service doesn't exist. Skipping NEG sync")
		return nil
	}
	endpointSlices := convertUntypedToEPS(slices)
	s.computeEPSStaleness(endpointSlices)

	endpointsData := negtypes.EndpointsDataFromEndpointSlices(endpointSlices)
	targetMap, endpointPodMap, err = s.getEndpointsCalculation(endpointsData, currentMap)

	var degradedTargetMap, notInDegraded, onlyInDegraded map[string]negtypes.NetworkEndpointSet
	var degradedPodMap negtypes.EndpointPodMap
	var degradedModeErr error
	if s.enableDegradedModeMetrics || s.enableDegradedMode {
		degradedTargetMap, degradedPodMap, degradedModeErr = s.endpointsCalculator.CalculateEndpointsDegradedMode(endpointsData, currentMap)
		if degradedModeErr == nil { // we collect metrics when the normal calculation doesn't run into error
			s.logStats(targetMap, "normal mode desired NEG endpoints")
			s.logStats(degradedTargetMap, "degraded mode desired NEG endpoints")
			notInDegraded, onlyInDegraded = calculateNetworkEndpointDifference(targetMap, degradedTargetMap)
			if err == nil {
				computeDegradedModeCorrectness(notInDegraded, onlyInDegraded, string(s.NegSyncerKey.NegType), s.logger)
			}
		}
	}

	if !s.enableDegradedMode {
		if err != nil {
			return err
		}
		s.logger.Info("Using normal mode endpoint calculation")
	} else {
		if !s.inErrorState() && err != nil {
			return err // if we encounter an error, we will return and run the next sync in degraded mode
		}
		if degradedModeErr != nil {
			return degradedModeErr
		}
		if s.inErrorState() {
			s.logger.Info("Using degraded mode endpoint calculation")
			targetMap = degradedTargetMap
			endpointPodMap = degradedPodMap
		} else {
			s.logger.Info("Using normal mode endpoint calculation")
		}
	}
	// When the flags are not enabled, error state should be reset when no
	// error occurs in the sync.
	// notInDegraded and onlyInDegraded are not populated when the flags are
	// not enabled, so they would always be empty and reset error state.
	if len(notInDegraded) == 0 && len(onlyInDegraded) == 0 {
		s.logger.Info("Exit degraded mode")
		if s.enableDegradedMode && s.inErrorState() {
			s.recordEvent(apiv1.EventTypeNormal, "ExitDegradedMode", fmt.Sprintf("NEG %s is no longer in degraded mode", s.NegSyncerKey.String()))
		}
		s.resetErrorState()
	}
	s.logStats(targetMap, "desired NEG endpoints")

	// Calculate the endpoints to add and delete to transform the current state to desire state
	addEndpoints, removeEndpoints := calculateNetworkEndpointDifference(targetMap, currentMap)
	// Calculate Pods that are already in the NEG
	_, committedEndpoints := calculateNetworkEndpointDifference(addEndpoints, targetMap)

	// Filtering of migration endpoints should happen before we filter out
	// the transaction endpoints. Not doing so could result in an attempt to
	// attach an endpoint which is still undergoing detachment-due-to-migration.
	migrationZone := s.dsMigrator.Filter(addEndpoints, removeEndpoints, committedEndpoints)

	// Filter out the endpoints with existing transaction
	// This mostly happens when transaction entry require reconciliation but the transaction is still progress
	// e.g. endpoint A is in the process of adding to NEG N, and the new desire state is not to have A in N.
	// This ensures the endpoint that requires reconciliation to wait till the existing transaction to complete.
	filterEndpointByTransaction(addEndpoints, s.transactions, s.logger)
	filterEndpointByTransaction(removeEndpoints, s.transactions, s.logger)
	// filter out the endpoints that are in transaction
	filterEndpointByTransaction(committedEndpoints, s.transactions, s.logger)

	var endpointPodLabelMap labels.EndpointPodLabelMap
	// Only fetch label from pod for L7 endpoints
	if flags.F.EnableNEGLabelPropagation && s.NegType == negtypes.VmIpPortEndpointType {
		endpointPodLabelMap = getEndpointPodLabelMap(addEndpoints, endpointPodMap, s.podLister, s.podLabelPropagationConfig, s.recorder, s.logger)
		publishAnnotationSizeMetrics(addEndpoints, endpointPodLabelMap)
	}

	s.syncMetricsCollector.SetLabelPropagationStats(s.NegSyncerKey, collectLabelStats(currentPodLabelMap, endpointPodLabelMap, targetMap))

	if s.needCommit() {
		s.commitPods(committedEndpoints, endpointPodMap)
	}

	if len(addEndpoints) == 0 && len(removeEndpoints) == 0 {
		s.logger.V(3).Info("No endpoint change. Skip syncing NEG. ", s.Namespace, s.Name)
		return nil
	}
	s.logEndpoints(addEndpoints, "adding endpoint")
	s.logEndpoints(removeEndpoints, "removing endpoint")

	return s.syncNetworkEndpoints(addEndpoints, removeEndpoints, endpointPodLabelMap, migrationZone)
}

func (s *transactionSyncer) getEndpointsCalculation(
	endpointsData []negtypes.EndpointsData,
	currentMap map[string]negtypes.NetworkEndpointSet,
) (map[string]negtypes.NetworkEndpointSet, negtypes.EndpointPodMap, error) {
	targetMap, endpointPodMap, endpointsExcludedInCalculation, err := s.endpointsCalculator.CalculateEndpoints(endpointsData, currentMap)
	if err != nil {
		return nil, nil, err
	}
	if s.enableDegradedMode {
		err = s.endpointsCalculator.ValidateEndpoints(endpointsData, endpointPodMap, endpointsExcludedInCalculation)
		if err != nil {
			return nil, nil, err
		}
	}
	return targetMap, endpointPodMap, nil
}

// syncLock must already be acquired before execution
func (s *transactionSyncer) inErrorState() bool {
	return s.errorState
}

// InErrorState is a wrapper for exporting inErrorState().
func (s *transactionSyncer) InErrorState() bool {
	return s.inErrorState()
}

// syncLock must already be acquired before execution
func (s *transactionSyncer) setErrorState() {
	s.errorState = true
}

// syncLock must already be acquired before execution
func (s *transactionSyncer) resetErrorState() {
	s.errorState = false
}

// ensureNetworkEndpointGroups ensures NEGs are created and configured correctly in the corresponding zones.
func (s *transactionSyncer) ensureNetworkEndpointGroups() error {
	var err error
	// NEGs should be created in zones with candidate nodes only.
	zones, err := s.zoneGetter.ListZones(negtypes.NodeFilterForEndpointCalculatorMode(s.EpCalculatorMode), s.logger)
	if err != nil {
		return err
	}

	var errList []error
	var negObjRefs []negv1beta1.NegObjectReference
	negsByLocation := make(map[string]int)
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
			s.networkInfo,
			s.logger,
		)
		if err != nil {
			errList = append(errList, err)
		}

		if s.svcNegClient != nil && err == nil {
			negObjRefs = append(negObjRefs, negObj)
			negsByLocation[zone]++
		}
	}

	s.updateInitStatus(negObjRefs, errList)
	s.syncMetricsCollector.UpdateSyncerNegCount(s.NegSyncerKey, negsByLocation)
	return utilerrors.NewAggregate(errList)
}

// syncNetworkEndpoints spins off go routines to execute NEG operations
func (s *transactionSyncer) syncNetworkEndpoints(addEndpoints, removeEndpoints map[string]negtypes.NetworkEndpointSet, endpointPodLabelMap labels.EndpointPodLabelMap, migrationZone string) error {
	syncFunc := func(endpointMap map[string]negtypes.NetworkEndpointSet, operation transactionOp) error {
		for zone, endpointSet := range endpointMap {
			zone := zone
			if endpointSet.Len() == 0 {
				s.logger.V(2).Info("0 endpoints in the endpoint list. Skipping operation", "operation", attachOp, "negSyncerKey", s.NegSyncerKey.String(), "zone", zone)
				continue
			}

			batch, err := makeEndpointBatch(endpointSet, s.NegType, endpointPodLabelMap, s.logger)
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
				go s.attachNetworkEndpoints(zone, batch)
			}
			if operation == detachOp {
				if zone == migrationZone {
					// Prevent any further migration-detachments from starting while one
					// is already in progress.
					s.dsMigrator.Pause()
				}
				go s.detachNetworkEndpoints(zone, batch, zone == migrationZone)
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

// attachNetworkEndpoints runs operation for attaching network endpoints.
func (s *transactionSyncer) attachNetworkEndpoints(zone string, networkEndpointMap map[negtypes.NetworkEndpoint]*composite.NetworkEndpoint) {
	s.logger.V(2).Info("Attaching endpoints to NEG.", "countOfEndpointsBeingAttached", len(networkEndpointMap), "negSyncerKey", s.NegSyncerKey.String(), "zone", zone)
	err := s.operationInternal(attachOp, zone, networkEndpointMap, s.logger)

	// WARNING: commitTransaction must be called at last for analyzing the operation result
	s.commitTransaction(err, networkEndpointMap)
}

// detachNetworkEndpoints runs operation for detaching network endpoints.
func (s *transactionSyncer) detachNetworkEndpoints(zone string, networkEndpointMap map[negtypes.NetworkEndpoint]*composite.NetworkEndpoint, hasMigrationDetachments bool) {
	s.logger.V(2).Info("Detaching endpoints from NEG.", "countOfEndpointsBeingDetached", len(networkEndpointMap), "negSyncerKey", s.NegSyncerKey.String(), "zone", zone)
	err := s.operationInternal(detachOp, zone, networkEndpointMap, s.logger)

	if hasMigrationDetachments {
		// Unpause the migration since the ongoing migration-detachments have
		// concluded.
		s.dsMigrator.Continue(err)
	}

	// WARNING: commitTransaction must be called at last for analyzing the operation result
	s.commitTransaction(err, networkEndpointMap)
}

// operationInternal executes NEG API call and commits the transactions
// It will record events when operations are completed
// If error occurs or any transaction entry requires reconciliation, it will trigger resync
func (s *transactionSyncer) operationInternal(operation transactionOp, zone string, networkEndpointMap map[negtypes.NetworkEndpoint]*composite.NetworkEndpoint, logger klog.Logger) error {
	var err error
	start := time.Now()
	networkEndpoints := []*composite.NetworkEndpoint{}
	for _, ne := range networkEndpointMap {
		networkEndpoints = append(networkEndpoints, ne)
	}

	if operation == attachOp {
		err = s.cloud.AttachNetworkEndpoints(s.NegSyncerKey.NegName, zone, networkEndpoints, s.NegSyncerKey.GetAPIVersion(), logger)
	}
	if operation == detachOp {
		err = s.cloud.DetachNetworkEndpoints(s.NegSyncerKey.NegName, zone, networkEndpoints, s.NegSyncerKey.GetAPIVersion(), logger)
	}

	if err == nil {
		s.recordEvent(apiv1.EventTypeNormal, operation.String(), fmt.Sprintf("%s %d network endpoint(s) (NEG %q in zone %q)", operation.String(), len(networkEndpointMap), s.NegSyncerKey.NegName, zone))
		s.syncMetricsCollector.UpdateSyncerStatusInMetrics(s.NegSyncerKey, nil, s.inErrorState())
	} else {
		s.recordEvent(apiv1.EventTypeWarning, operation.String()+"Failed", fmt.Sprintf("Failed to %s %d network endpoint(s) (NEG %q in zone %q): %v", operation.String(), len(networkEndpointMap), s.NegSyncerKey.NegName, zone, err))
		err := checkEndpointBatchErr(err, operation)
		syncErr := negtypes.ClassifyError(err)
		// If the API call fails for invalid endpoint update request in any goroutine,
		// we would set error state and retry. For successful calls, we won't update
		// error state, so its value won't be overwritten within API call go routines.
		if syncErr.IsErrorState {
			s.logger.Error(err, "Detected unexpected error when checking endpoint update response", "operation", operation)
			s.syncLock.Lock()
			s.logger.Info("Enter degraded mode", "reason", syncErr.Reason)
			if s.enableDegradedMode {
				s.recordEvent(apiv1.EventTypeWarning, "EnterDegradedMode", fmt.Sprintf("Entering degraded mode for NEG %s due to sync err: %v", s.NegSyncerKey.String(), syncErr))
			}
			s.setErrorState()
			s.syncLock.Unlock()
		}
		s.syncMetricsCollector.UpdateSyncerStatusInMetrics(s.NegSyncerKey, syncErr, s.inErrorState())
	}

	metrics.PublishNegOperationMetrics(operation.String(), string(s.NegSyncerKey.NegType), string(s.NegSyncerKey.GetAPIVersion()), err, len(networkEndpointMap), start)
	return err
}

// checkEndpointBatchErr checks the error from endpoint batch response
// it returns an error-state error if NEG API error is due to bad endpoint batch request
// otherwise, it returns the error unmodified
func checkEndpointBatchErr(err error, operation transactionOp) error {
	var gerr *googleapi.Error
	if !errors.As(err, &gerr) {
		return fmt.Errorf("%w: %v", negtypes.ErrInvalidAPIResponse, err)
	}
	if gerr.Code == http.StatusBadRequest {
		if operation == attachOp {
			return fmt.Errorf("%w: %v", negtypes.ErrInvalidEPAttach, err)
		}
		if operation == detachOp {
			return fmt.Errorf("%w: %v", negtypes.ErrInvalidEPDetach, err)
		}
	}
	return err
}

func (s *transactionSyncer) recordEvent(eventType, reason, eventDesc string) {
	if svc := getService(s.serviceLister, s.Namespace, s.Name, s.logger); svc != nil {
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
		metrics.PublishNegControllerErrorCountMetrics(err, false)
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
		if negtypes.IsStrategyQuotaError(err) {
			s.syncer.Sync()
		} else {
			if retryErr := s.retry.Retry(); retryErr != nil {
				s.recordEvent(apiv1.EventTypeWarning, "RetryFailed", fmt.Sprintf("Failed to retry NEG sync for %q: %v", s.NegSyncerKey.String(), retryErr))
				metrics.PublishNegControllerErrorCountMetrics(retryErr, false)
			}
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
		metrics.PublishNegControllerErrorCountMetrics(err, true)
		return false
	}

	existingZones := sets.NewString()
	for _, ref := range negCR.Status.NetworkEndpointGroups {
		id, err := cloud.ParseResourceURL(ref.SelfLink)
		if err != nil {
			s.logger.Error(err, "unable to parse selflink", "selfLink", ref.SelfLink)
			metrics.PublishNegControllerErrorCountMetrics(err, true)
			continue
		}
		existingZones.Insert(id.Key.Zone)
	}

	zones, err := s.zoneGetter.ListZones(negtypes.NodeFilterForEndpointCalculatorMode(s.EpCalculatorMode), s.logger)
	if err != nil {
		s.logger.Error(err, "unable to list zones")
		metrics.PublishNegControllerErrorCountMetrics(err, true)
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
		metrics.PublishNegControllerErrorCountMetrics(err, true)
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
		metrics.PublishNegControllerErrorCountMetrics(err, true)
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
		metrics.PublishNegControllerErrorCountMetrics(err, true)
		return
	}
	neg := origNeg.DeepCopy()

	ts := metav1.Now()
	if _, _, exists := findCondition(neg.Status.Conditions, negv1beta1.Initialized); !exists {
		s.needInit = true
	}
	metrics.PublishNegSyncerStalenessMetrics(ts.Sub(neg.Status.LastSyncTime.Time))

	ensureCondition(neg, getSyncedCondition(syncErr))
	neg.Status.LastSyncTime = ts

	if len(neg.Status.NetworkEndpointGroups) == 0 {
		s.needInit = true
	}

	_, err = patchNegStatus(s.svcNegClient, origNeg.Status, neg.Status, s.Namespace, s.NegSyncerKey.NegName)
	if err != nil {
		s.logger.Error(err, "Error updating Neg CR")
		metrics.PublishNegControllerErrorCountMetrics(err, true)
	}
}

func convertUntypedToEPS(endpointSliceUntyped []interface{}) []*discovery.EndpointSlice {
	endpointSlices := make([]*discovery.EndpointSlice, len(endpointSliceUntyped))
	for i, slice := range endpointSliceUntyped {
		endpointslice := slice.(*discovery.EndpointSlice)
		endpointSlices[i] = endpointslice
	}
	return endpointSlices
}

func (s *transactionSyncer) computeEPSStaleness(endpointSlices []*discovery.EndpointSlice) {
	negCR, err := getNegFromStore(s.svcNegLister, s.Namespace, s.NegSyncerKey.NegName)
	if err != nil {
		s.logger.Error(err, "unable to retrieve neg from the store", "neg", klog.KRef(s.Namespace, s.NegName))
		metrics.PublishNegControllerErrorCountMetrics(err, true)
		return
	}
	lastSyncTimestamp := negCR.Status.LastSyncTime
	for _, endpointSlice := range endpointSlices {
		epsCreationTimestamp := endpointSlice.ObjectMeta.CreationTimestamp

		epsStaleness := time.Since(lastSyncTimestamp.Time)
		// if this endpoint slice is newly created/created after last sync
		if lastSyncTimestamp.Before(&epsCreationTimestamp) {
			epsStaleness = time.Since(epsCreationTimestamp.Time)
		}
		metrics.PublishNegEPSStalenessMetrics(epsStaleness)
		s.logger.V(3).Info("Endpoint slice syncs", "Namespace", endpointSlice.Namespace, "Name", endpointSlice.Name, "staleness", epsStaleness)
	}
}

// computeDegradedModeCorrectness computes degraded mode correctness metrics based on the difference between degraded mode and normal calculation
func computeDegradedModeCorrectness(notInDegraded, onlyInDegraded map[string]negtypes.NetworkEndpointSet, negType string, logger klog.Logger) {
	logger.Info("Exporting degraded mode correctness metrics", "notInDegraded", notInDegraded, "onlyInDegraded", onlyInDegraded)
	notInDegradedEndpoints := 0
	for _, val := range notInDegraded {
		notInDegradedEndpoints += len(val)
	}
	metrics.PublishDegradedModeCorrectnessMetrics(notInDegradedEndpoints, metrics.NotInDegradedEndpoints, negType)
	onlyInDegradedEndpoints := 0
	for _, val := range onlyInDegraded {
		onlyInDegradedEndpoints += len(val)
	}
	metrics.PublishDegradedModeCorrectnessMetrics(onlyInDegradedEndpoints, metrics.OnlyInDegradedEndpoints, negType)
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

	start := time.Now()
	neg, err := svcNegClient.NetworkingV1beta1().ServiceNetworkEndpointGroups(namespace).Patch(context.Background(), negName, types.MergePatchType, patchBytes, metav1.PatchOptions{})
	metrics.PublishK8sRequestCountMetrics(start, metrics.DeleteRequest, err)
	return neg, err
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

// getEndpointPodLabelMap goes through all the endpoints to be attached and fetches the labels from the endpoint pods.
func getEndpointPodLabelMap(endpoints map[string]negtypes.NetworkEndpointSet, endpointPodMap negtypes.EndpointPodMap, podLister cache.Store, lpConfig labels.PodLabelPropagationConfig, recorder record.EventRecorder, logger klog.Logger) labels.EndpointPodLabelMap {
	endpointPodLabelMap := labels.EndpointPodLabelMap{}
	for _, endpointSet := range endpoints {
		for endpoint := range endpointSet {
			key := fmt.Sprintf("%s/%s", endpointPodMap[endpoint].Namespace, endpointPodMap[endpoint].Name)
			obj, ok, err := podLister.GetByKey(key)
			if err != nil || !ok {
				metrics.PublishLabelPropagationError(labels.OtherError)
				logger.Error(err, "getEndpointPodLabelMap: error getting pod", "pod", key, "exist", ok)
				metrics.PublishNegControllerErrorCountMetrics(err, true)
				continue
			}
			pod, ok := obj.(*v1.Pod)
			if !ok {
				metrics.PublishLabelPropagationError(labels.OtherError)
				logger.Error(nil, "expected type *v1.Pod", "pod", key, "type", fmt.Sprintf("%T", obj))
				continue
			}
			labelMap, err := labels.GetPodLabelMap(pod, lpConfig)
			if err != nil {
				recorder.Eventf(pod, apiv1.EventTypeWarning, "LabelsExceededLimit", "Label Propagation Error: %v", err)
				metrics.PublishNegControllerErrorCountMetrics(err, true)
			}
			endpointPodLabelMap[endpoint] = labelMap
		}
	}
	return endpointPodLabelMap
}

// publishAnnotationSizeMetrics goes through all the endpoints to be attached
// and publish annotation size metrics.
func publishAnnotationSizeMetrics(endpoints map[string]negtypes.NetworkEndpointSet, endpointPodLabelMap labels.EndpointPodLabelMap) {
	for _, endpointSet := range endpoints {
		for endpoint := range endpointSet {
			labelMap := endpointPodLabelMap[endpoint]
			metrics.PublishAnnotationMetrics(labels.GetPodLabelMapSize(labelMap), len(labelMap))
		}
	}
}

// collectLabelStats calculate the number of endpoints and the number of endpoints with annotations.
func collectLabelStats(currentPodLabelMap, addPodLabelMap labels.EndpointPodLabelMap, targetEndpointMap map[string]negtypes.NetworkEndpointSet) metricscollector.LabelPropagationStats {
	labelPropagationStats := metricscollector.LabelPropagationStats{}
	for _, endpointSet := range targetEndpointMap {
		for endpoint := range endpointSet {
			labelPropagationStats.NumberOfEndpoints += 1
			if currentPodLabelMap[endpoint] != nil || addPodLabelMap[endpoint] != nil {
				labelPropagationStats.EndpointsWithAnnotation += 1
			}
		}
	}
	return labelPropagationStats
}
