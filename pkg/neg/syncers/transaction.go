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
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"google.golang.org/api/googleapi"
	v1 "k8s.io/api/core/v1"
	discovery "k8s.io/api/discovery/v1"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/network"
	"k8s.io/ingress-gce/pkg/nodetopology"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/endpointslices"

	nodetopologyv1 "github.com/GoogleCloudPlatform/gke-networking-api/apis/nodetopology/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/ingress-gce/pkg/backoff"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/neg/metrics"
	"k8s.io/ingress-gce/pkg/neg/metrics/metricscollector"
	"k8s.io/ingress-gce/pkg/neg/readiness"
	"k8s.io/ingress-gce/pkg/neg/syncers/dualstack"
	"k8s.io/ingress-gce/pkg/neg/syncers/labels"
	"k8s.io/ingress-gce/pkg/neg/syncers/resourcemanager"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
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
	recorder            record.EventRecorder
	cloud               negtypes.NetworkEndpointGroupCloud
	endpointsCalculator negtypes.NetworkEndpointsCalculator

	// retry handles back off retry for NEG API operations
	retry backoff.RetryHandler

	// reflector handles NEG readiness gate and conditions for pods in NEG.
	reflector readiness.Reflector

	//kubeSystemUID used to populate Cluster UID on Neg Description when using NEG CRD
	kubeSystemUID string

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
	// enableL4NEGDetachCancel enables re-attachment logic for endpoints that are
	// detaching. This will allow the controller to call attach even if a detach
	// operation is ongoing.
	enableL4NEGDetachCancel bool

	// podLabelPropagationConfig configures the pod label to be propagated to NEG endpoints
	podLabelPropagationConfig labels.PodLabelPropagationConfig

	dsMigrator *dualstack.Migrator

	// networkInfo contains the network information to use in GCP resources (VPC URL, Subnetwork URL).
	// and the k8s network name (can be used in endpoints calculation).
	networkInfo network.NetworkInfo

	// negMetrics is used to collect metrics per NEG instance
	negMetrics *metrics.NegMetrics

	negResourceManager resourcemanager.NegResourceManager
}

func NewTransactionSyncer(
	negSyncerKey negtypes.NegSyncerKey,
	recorder record.EventRecorder,
	cloud negtypes.NetworkEndpointGroupCloud,
	podLister cache.Indexer,
	serviceLister cache.Indexer,
	endpointSliceLister cache.Indexer,
	reflector readiness.Reflector,
	epc negtypes.NetworkEndpointsCalculator,
	kubeSystemUID string,
	syncerMetrics *metricscollector.SyncerMetrics,
	log klog.Logger,
	lpConfig labels.PodLabelPropagationConfig,
	enableDualStackNEG bool,
	networkInfo network.NetworkInfo,
	negMetrics *metrics.NegMetrics,
	negResourceManager resourcemanager.NegResourceManager,

) negtypes.NegSyncer {

	logger := log.WithName("Syncer").WithValues("service", klog.KRef(negSyncerKey.Namespace, negSyncerKey.Name), "primaryNEGName", negSyncerKey.NegName)

	// TransactionSyncer implements the syncer core
	ts := &transactionSyncer{
		NegSyncerKey:              negSyncerKey,
		needInit:                  true,
		transactions:              NewTransactionTable(),
		podLister:                 podLister,
		serviceLister:             serviceLister,
		endpointSliceLister:       endpointSliceLister,
		recorder:                  recorder,
		cloud:                     cloud,
		endpointsCalculator:       epc,
		reflector:                 reflector,
		kubeSystemUID:             kubeSystemUID,
		syncMetricsCollector:      syncerMetrics,
		errorState:                false,
		logger:                    logger,
		enableDegradedMode:        flags.F.EnableDegradedMode,
		enableDegradedModeMetrics: flags.F.EnableDegradedModeMetrics,
		enableL4NEGDetachCancel:   flags.F.EnableL4NEGDetachCancel,
		enableDualStackNEG:        enableDualStackNEG,
		podLabelPropagationConfig: lpConfig,
		networkInfo:               networkInfo,
		negMetrics:                negMetrics,
		negResourceManager:        negResourceManager,
	}
	// Syncer implements life cycle logic
	syncer := newSyncer(negSyncerKey, serviceLister, recorder, ts, logger, negMetrics)
	// transactionSyncer needs syncer interface for internals
	ts.syncer = syncer
	ts.retry = backoff.NewDelayRetryHandler(func() { syncer.Sync() }, backoff.NewExponentialBackoffHandler(maxRetries, minRetryDelay, maxRetryDelay))
	ts.dsMigrator = dualstack.NewMigrator(enableDualStackNEG, syncer, negSyncerKey, syncerMetrics, ts, logger)
	return syncer
}

func GetEndpointsCalculator(podLister, nodeLister, serviceLister cache.Indexer, zoneGetter *zonegetter.ZoneGetter, syncerKey negtypes.NegSyncerKey, mode negtypes.EndpointsCalculatorMode, logger klog.Logger, enableDualStackNEG bool, syncMetricsCollector *metricscollector.SyncerMetrics, networkInfo *network.NetworkInfo, l4LBType negtypes.L4LBType, negMetrics *metrics.NegMetrics) negtypes.NetworkEndpointsCalculator {
	serviceKey := strings.Join([]string{syncerKey.Name, syncerKey.Namespace}, "/")
	if syncerKey.NegType == negtypes.VmIpEndpointType {
		nodeLister := listers.NewNodeLister(nodeLister)
		switch mode {
		case negtypes.L4LocalMode:
			return NewLocalL4EndpointsCalculator(nodeLister, zoneGetter, serviceKey, logger, networkInfo, l4LBType, negMetrics)
		default:
			return NewClusterL4EndpointsCalculator(nodeLister, zoneGetter, serviceKey, logger, networkInfo, l4LBType, negMetrics)
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
		negMetrics,
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
				s.recordEvent(v1.EventTypeWarning, "EnterDegradedMode", fmt.Sprintf("Entering degraded mode for NEG %s due to sync err: %v", s.NegSyncerKey.String(), syncErr))
			}
			s.setErrorState()
		}
	}

	needInit := s.negResourceManager.UpdateStatusSyncCompleted(err)
	s.needInit = s.needInit || needInit

	s.negMetrics.PublishNegSyncMetrics(string(s.NegSyncerKey.NegType), string(s.endpointsCalculator.Mode()), err, start)
	s.syncMetricsCollector.UpdateSyncerStatusInMetrics(s.NegSyncerKey, err, s.inErrorState())
	return err
}

func (s *transactionSyncer) syncInternalImpl() error {
	isStopped := s.syncer.IsStopped()
	isShuttingDown := s.syncer.IsShuttingDown()
	if isStopped || isShuttingDown {
		s.logger.Info("Skip syncing NEG", "negSyncerKey", s.NegSyncerKey.String(), "syncerStopped", isStopped, "syncerShuttingDown", isShuttingDown)
		return nil
	}

	// Decide if endpoint health should be retrieved when listing endpoints.
	// Only matters for L4 Local mode.
	needInitDrainStatus := s.needInit && s.enableL4NEGDetachCancel && s.endpointsCalculator.Mode() == negtypes.L4LocalMode

	locationsChanged := s.negResourceManager.LocationsChanged()
	if s.needInit || locationsChanged {
		s.logger.Info("Need to ensure network endpoint groups", "needInit", s.needInit, "locationsChanged", locationsChanged)
		if err := s.ensureNetworkEndpointGroups(); err != nil {
			return fmt.Errorf("%w: %v", negtypes.ErrNegNotFound, err)
		}
		s.needInit = false
	}
	s.logger.V(2).Info("Sync NEG", "negSyncerKey", s.NegSyncerKey.String(), "endpointsCalculatorMode", s.endpointsCalculator.Mode())

	subnetConfigs := s.negResourceManager.ListSubnets()
	subnetToNegMapping, err := s.negResourceManager.GenerateSubnetToNegNameMap(subnetConfigs)
	if err != nil {
		s.logger.Error(err, "failed to generate subnet to neg name mapping")
		return err
	}

	currentMap, currentPodLabelMap, drainingEndpoints, err := retrieveExistingZoneNetworkEndpointMap(subnetToNegMapping, s.negResourceManager, s.cloud, s.NegSyncerKey.GetAPIVersion(), s.endpointsCalculator.Mode(), s.enableDualStackNEG, s.logger, s.negMetrics, needInitDrainStatus)
	if err != nil {
		return fmt.Errorf("%w: %w", negtypes.ErrCurrentNegEPNotFound, err)
	}
	s.logStats(currentMap, "current NEG endpoints")

	// Merge the current state from cloud with the transaction table together
	// The combined state represents the eventual result when all transactions completed
	mergeTransactionIntoZoneEndpointMap(currentMap, s.transactions, s.logger)
	s.logStats(currentMap, "after in-progress operations have completed, NEG endpoints")

	var targetMap map[negtypes.NEGLocation]negtypes.NetworkEndpointSet
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
	s.negResourceManager.ComputeEPSStaleness(endpointSlices)

	endpointsData := negtypes.EndpointsDataFromEndpointSlices(endpointSlices)
	targetMap, endpointPodMap, err = s.getEndpointsCalculation(endpointsData, currentMap)

	var degradedTargetMap, notInDegraded, onlyInDegraded map[negtypes.NEGLocation]negtypes.NetworkEndpointSet
	var degradedPodMap negtypes.EndpointPodMap
	var degradedModeErr error
	if s.enableDegradedModeMetrics || s.enableDegradedMode {
		degradedTargetMap, degradedPodMap, degradedModeErr = s.endpointsCalculator.CalculateEndpointsDegradedMode(endpointsData, currentMap)
		if degradedModeErr == nil { // we collect metrics when the normal calculation doesn't run into error
			s.logStats(targetMap, "normal mode desired NEG endpoints")
			s.logStats(degradedTargetMap, "degraded mode desired NEG endpoints")
			notInDegraded, onlyInDegraded = calculateNetworkEndpointDifference(targetMap, degradedTargetMap)
			if err == nil {
				computeDegradedModeCorrectness(notInDegraded, onlyInDegraded, string(s.NegSyncerKey.NegType), s.logger, s.negMetrics)
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
			s.recordEvent(v1.EventTypeNormal, "ExitDegradedMode", fmt.Sprintf("NEG %s is no longer in degraded mode", s.NegSyncerKey.String()))
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
	if s.enableL4NEGDetachCancel && s.endpointsCalculator.Mode() == negtypes.L4LocalMode {
		filterEndpointByTransactionExclDetach(addEndpoints, s.transactions, s.logger)
		if len(drainingEndpoints) > 0 {
			reAddDrainingEndpointsThatAreInTargetMap(addEndpoints, targetMap, drainingEndpoints, s.logger)
		}
	} else {
		filterEndpointByTransaction(addEndpoints, s.transactions, s.logger)
	}
	filterEndpointByTransaction(removeEndpoints, s.transactions, s.logger)
	// filter out the endpoints that are in transaction
	filterEndpointByTransaction(committedEndpoints, s.transactions, s.logger)

	var endpointPodLabelMap labels.EndpointPodLabelMap
	// Only fetch label from pod for L7 endpoints
	if flags.F.EnableNEGLabelPropagation && s.NegType == negtypes.VmIpPortEndpointType {
		endpointPodLabelMap = getEndpointPodLabelMap(addEndpoints, endpointPodMap, s.podLister, s.podLabelPropagationConfig, s.recorder, s.logger, s.negMetrics)
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

// reAddDrainingEndpointsThatAreInTargetMap will make sure that endpoints that are draining
// are added if they are in the targetMap (the intended state from k8s)
// There could be cases where there is no detach entry in the transactionTable
// but the endpoint is in fact detaching (after controller restart for example).
// We need the controller to be able to detect this, re-attach and effectively cancel the drain if needed.
func reAddDrainingEndpointsThatAreInTargetMap(addEndpoints, targetMap map[negtypes.NEGLocation]negtypes.NetworkEndpointSet, drainingEndpoints map[negtypes.NetworkEndpoint]string, logger klog.Logger) {
	for zoneSubnet, endpointSet := range targetMap {
		for _, endpoint := range endpointSet.List() {
			if healthStatus, ok := drainingEndpoints[endpoint]; ok {
				logger.V(2).Info("Re-adding endpoint since it has a health status indicating draining but should be kept in the NEG", "endpoint", endpoint, "healthStatus", healthStatus)
				if addEndpoints[zoneSubnet] == nil {
					addEndpoints[zoneSubnet] = negtypes.NewNetworkEndpointSet()
				}
				addEndpoints[zoneSubnet].Insert(endpoint)
			}
		}
	}
}

func (s *transactionSyncer) getEndpointsCalculation(
	endpointsData []negtypes.EndpointsData,
	currentMap map[negtypes.NEGLocation]negtypes.NetworkEndpointSet,
) (map[negtypes.NEGLocation]negtypes.NetworkEndpointSet, negtypes.EndpointPodMap, error) {
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
	var errList []error
	var negObjs []*composite.NetworkEndpointGroup

	updateNEGStatus := true
	negsByLocation := make(map[string]int)

	// Get default subnet from syncer's networkInfo.
	defaultSubnet, err := utils.KeyName(s.networkInfo.SubnetworkURL)
	if err != nil {
		s.logger.Error(err, "Errored getting default subnet from NetworkInfo when ensuring NEGs")
		return err
	}

	var subnetConfigs []nodetopologyv1.SubnetConfig
	// Handle scenario for multi-networking
	if s.networkInfo.IsDefault {
		// This is the non-multi-networking case. Evaluate all subnets using the
		// zoneGetter.

		// List all existing subnets from the cluster.
		subnetConfigs = s.negResourceManager.ListSubnets()
	} else {
		// This is the multi-networking case where the VPC under consideration
		// is not the default. Use the pre configured subnet from the
		// networkInfo, effectively disregarding any concept of multi-subnets in
		// this case.

		subnetConfig, err := nodetopology.SubnetConfigFromSubnetURL(s.networkInfo.SubnetworkURL)
		if err != nil {
			return err
		}
		subnetConfigs = []nodetopologyv1.SubnetConfig{subnetConfig}
	}

	for _, subnetConfig := range subnetConfigs {
		negName, err := s.negResourceManager.GetNegNameForSubnet(subnetConfig.Name)
		if err != nil {
			s.logger.Error(err, "Unable to get the name of the NEGs based on the subnet name", "subnetName", subnetConfig.Name)
			errList = append(errList, err)
			continue
		}

		networkInfo := s.networkInfo
		if subnetConfig.Name != defaultSubnet {
			// Determine the networkInfo for the non-default subnet NEGs.
			resourceID, err := cloud.ParseResourceURL(subnetConfig.SubnetPath)
			if err != nil {
				s.logger.Error(err, "Failed to parse subnet path", "subnetPath", subnetConfig.SubnetPath)
				errList = append(errList, err)
				continue
			}
			// Add compute and version GA prefix.
			networkInfo.SubnetworkURL = cloud.SelfLink(meta.VersionGA, resourceID.ProjectID, resourceID.Resource, resourceID.Key)
		}

		zones, err := s.negResourceManager.ListZonesForSubnet(subnetConfig.Name, negtypes.NodeFilterForEndpointCalculatorMode(s.EpCalculatorMode))
		if err != nil {
			return err
		}

		for _, zone := range zones {
			negObj, err := s.negResourceManager.EnsureNeg(negName, zone, networkInfo)
			if err != nil {
				errList = append(errList, err)
				// Do not modify NEG Status if there is conflict within the same cluster
				// and namespace because the CR is owned by a different syncer.
				if errors.Is(err, utils.ErrNEGUsedByAnotherSyncer) {
					updateNEGStatus = false
					break
				}
			}

			if err == nil {
				negObjs = append(negObjs, negObj)
				negsByLocation[zone]++
			}
		}
	}

	if updateNEGStatus {
		s.negResourceManager.UpdateStatusNegsEnsured(negObjs, errList)
	}

	s.syncMetricsCollector.UpdateSyncerNegCount(s.NegSyncerKey, negsByLocation)
	return utilerrors.NewAggregate(errList)
}

// syncNetworkEndpoints spins off go routines to execute NEG operations
func (s *transactionSyncer) syncNetworkEndpoints(addEndpoints, removeEndpoints map[negtypes.NEGLocation]negtypes.NetworkEndpointSet, endpointPodLabelMap labels.EndpointPodLabelMap, migrationZone negtypes.NEGLocation) error {
	syncFunc := func(endpointMap map[negtypes.NEGLocation]negtypes.NetworkEndpointSet, operation transactionOp) error {
		for negLocation, endpointSet := range endpointMap {
			zone := negLocation.Zone
			subnet := negLocation.Subnet
			if endpointSet.Len() == 0 {
				s.logger.V(2).Info("0 endpoints in the endpoint list. Skipping operation", "operation", attachOp, "negSyncerKey", s.NegSyncerKey.String(), "zone", zone, "subnet", subnet)
				continue
			}

			batch, err := makeEndpointBatch(endpointSet, s.NegType, endpointPodLabelMap, s.logger)
			if err != nil {
				return err
			}

			transEntry := transactionEntry{
				Operation: operation,
				Zone:      zone,
				Subnet:    subnet,
			}

			// Insert networkEndpoint into transaction table
			for networkEndpoint := range batch {
				s.transactions.Put(networkEndpoint, transEntry)
			}

			if operation == attachOp {
				go s.attachNetworkEndpoints(negLocation, batch)
			}
			if operation == detachOp {
				if zone == migrationZone.Zone && subnet == migrationZone.Subnet {
					// Prevent any further migration-detachments from starting while one
					// is already in progress.
					s.dsMigrator.Pause()
				}
				go s.detachNetworkEndpoints(negLocation, batch, zone == migrationZone.Zone && subnet == migrationZone.Subnet)
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
func (s *transactionSyncer) attachNetworkEndpoints(negLocation negtypes.NEGLocation, networkEndpointMap map[negtypes.NetworkEndpoint]*composite.NetworkEndpoint) {
	s.logger.V(2).Info("Attaching endpoints to NEG.", "countOfEndpointsBeingAttached", len(networkEndpointMap), "negSyncerKey", s.NegSyncerKey.String(), "zone", negLocation.Zone, "subnet", negLocation.Subnet)
	err := s.operationInternal(attachOp, negLocation, networkEndpointMap, s.logger)

	// WARNING: commitTransaction must be called at last for analyzing the operation result
	s.commitTransaction(attachOp, err, networkEndpointMap)
}

// detachNetworkEndpoints runs operation for detaching network endpoints.
func (s *transactionSyncer) detachNetworkEndpoints(negLocation negtypes.NEGLocation, networkEndpointMap map[negtypes.NetworkEndpoint]*composite.NetworkEndpoint, hasMigrationDetachments bool) {
	s.logger.V(2).Info("Detaching endpoints from NEG.", "countOfEndpointsBeingDetached", len(networkEndpointMap), "negSyncerKey", s.NegSyncerKey.String(), "zone", negLocation.Zone, "subnet", negLocation.Subnet)
	err := s.operationInternal(detachOp, negLocation, networkEndpointMap, s.logger)

	if hasMigrationDetachments {
		// Unpause the migration since the ongoing migration-detachments have
		// concluded.
		s.dsMigrator.Continue(err)
	}

	// WARNING: commitTransaction must be called at last for analyzing the operation result
	s.commitTransaction(detachOp, err, networkEndpointMap)
}

// operationInternal executes NEG API call and commits the transactions
// It will record events when operations are completed
// If error occurs or any transaction entry requires reconciliation, it will trigger resync
func (s *transactionSyncer) operationInternal(operation transactionOp, negLocation negtypes.NEGLocation, networkEndpointMap map[negtypes.NetworkEndpoint]*composite.NetworkEndpoint, logger klog.Logger) error {
	var err error
	start := time.Now()
	networkEndpoints := []*composite.NetworkEndpoint{}
	for _, ne := range networkEndpointMap {
		networkEndpoints = append(networkEndpoints, ne)
	}
	zone := negLocation.Zone
	negName, err := s.negResourceManager.GetNegNameForSubnet(negLocation.Subnet)
	if err != nil {
		s.logger.Error(err, "Errored getting NEG name for subnet when syncing NEG endpoints", "subnetName", negLocation.Subnet)
		return err
	}

	if operation == attachOp {
		err = s.cloud.AttachNetworkEndpoints(negName, zone, networkEndpoints, s.NegSyncerKey.GetAPIVersion(), logger)
	}
	if operation == detachOp {
		err = s.cloud.DetachNetworkEndpoints(negName, zone, networkEndpoints, s.NegSyncerKey.GetAPIVersion(), logger)
	}

	if err == nil {
		s.recordEvent(v1.EventTypeNormal, operation.String(), fmt.Sprintf("%s %d network endpoint(s) (NEG %q in zone %q)", operation.String(), len(networkEndpointMap), negName, zone))
		s.syncMetricsCollector.UpdateSyncerStatusInMetrics(s.NegSyncerKey, nil, s.inErrorState())
	} else {
		s.recordEvent(v1.EventTypeWarning, operation.String()+"Failed", fmt.Sprintf("Failed to %s %d network endpoint(s) (NEG %q in zone %q): %v", operation.String(), len(networkEndpointMap), negName, zone, err))
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
				s.recordEvent(v1.EventTypeWarning, "EnterDegradedMode", fmt.Sprintf("Entering degraded mode for NEG %s due to sync err: %v", s.NegSyncerKey.String(), syncErr))
			}
			s.setErrorState()
			s.syncLock.Unlock()
		}
		s.syncMetricsCollector.UpdateSyncerStatusInMetrics(s.NegSyncerKey, syncErr, s.inErrorState())
	}

	s.negMetrics.PublishNegOperationMetrics(operation.String(), string(s.NegSyncerKey.NegType), string(s.NegSyncerKey.GetAPIVersion()), err, len(networkEndpointMap), start)
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
	if svc := getService(s.serviceLister, s.Namespace, s.Name, s.logger, s.negMetrics); svc != nil {
		s.recorder.Event(svc, eventType, reason, eventDesc)
	}
}

// commitTransaction commits the transactions for the input endpoints.
// It will trigger syncer retry in the following conditions:
// 1. Any of the transaction committed needed to be reconciled
// 2. Input error was not nil
func (s *transactionSyncer) commitTransaction(op transactionOp, err error, networkEndpointMap map[negtypes.NetworkEndpoint]*composite.NetworkEndpoint) {
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
		s.negMetrics.PublishNegControllerErrorCountMetrics(err, false)
	}

	for networkEndpoint := range networkEndpointMap {
		tr, ok := s.transactions.Get(networkEndpoint)
		// do not remove if the operation types don't match. There is an edge case where the endpoint could be in an ongoing long detach but we allow an attach to stop detachment.
		// In this case the attach will overwrite the transaction entry. One of these operations will finish earlier and we don't want it to remove the entry.
		if s.enableL4NEGDetachCancel && s.endpointsCalculator.Mode() == negtypes.L4LocalMode && ok && tr.Operation != op {
			s.logger.Info("Mismatched transaction operation type. Likely re-attaching an endpoint.", "endpoint", networkEndpoint)
			continue
		}
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
				s.recordEvent(v1.EventTypeWarning, "RetryFailed", fmt.Sprintf("Failed to retry NEG sync for %q: %v", s.NegSyncerKey.String(), retryErr))
				s.negMetrics.PublishNegControllerErrorCountMetrics(retryErr, false)
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
func (s *transactionSyncer) commitPods(endpointMap map[negtypes.NEGLocation]negtypes.NetworkEndpointSet, endpointPodMap negtypes.EndpointPodMap) {
	for negLocation, endpointSet := range endpointMap {
		zoneEndpointMap := negtypes.EndpointPodMap{}
		for _, endpoint := range endpointSet.List() {
			podName, ok := endpointPodMap[endpoint]
			if !ok {
				s.logger.Error(nil, "Endpoint is not included in the endpointPodMap", "endpoint", endpoint, "endpointPodMap", fmt.Sprintf("%+v", endpointPodMap))
				continue
			}
			zoneEndpointMap[endpoint] = podName
		}

		negName := s.NegSyncerKey.NegName
		syncerKey := s.NegSyncerKey
		if flags.F.EnableMultiSubnetClusterPhase1 {
			var err error
			negName, err = s.negResourceManager.GetNegNameForSubnet(negLocation.Subnet)
			if err != nil {
				s.logger.Error(err, "Errored getting NEG name for subnet when commiting pods", "subnetName", negLocation.Subnet)
				continue
			}
			// To ensure syncerKey has the same information as the passed in NEG name.
			syncerKey.NegName = negName
		}
		s.reflector.CommitPods(syncerKey, negName, negLocation.Zone, zoneEndpointMap)
	}
}

// filterEndpointByTransaction removes the all endpoints from endpoint map if they exists in the transaction table
func filterEndpointByTransaction(endpointMap map[negtypes.NEGLocation]negtypes.NetworkEndpointSet, table networkEndpointTransactionTable, logger klog.Logger) {
	for _, endpointSet := range endpointMap {
		for _, endpoint := range endpointSet.List() {
			if entry, ok := table.Get(endpoint); ok {
				logger.V(2).Info("Endpoint is removed from the endpoint set as transaction still exists.", "endpoint", endpoint, "transactionEntry", entry)
				endpointSet.Delete(endpoint)
			}
		}
	}
}

func filterEndpointByTransactionExclDetach(endpointMap map[negtypes.NEGLocation]negtypes.NetworkEndpointSet, table networkEndpointTransactionTable, logger klog.Logger) {
	for zoneSubnet, endpointSet := range endpointMap {
		for _, endpoint := range endpointSet.List() {
			if entry, ok := table.Get(endpoint); ok {
				if entry.Operation == detachOp {
					logger.V(2).Info("Endpoint is NOT removed from the endpoint set as existing transaction is DELETE. Detach should be cancelled", "endpoint", endpoint, "transactionEntry", entry)
					continue
				}
				logger.V(2).Info("Endpoint is removed from the endpoint set as transaction still exists.", "endpoint", endpoint, "transactionEntry", entry)
				endpointSet.Delete(endpoint)
				if len(endpointSet) == 0 {
					delete(endpointMap, zoneSubnet)
				}
			}
		}
	}
}

// mergeTransactionIntoZoneEndpointMap merges the ongoing transaction into the endpointMap.
// This converts the existing endpointMap to the state when all transactions completed
func mergeTransactionIntoZoneEndpointMap(endpointMap map[negtypes.NEGLocation]negtypes.NetworkEndpointSet, transactions networkEndpointTransactionTable, logger klog.Logger) {
	for _, endpointKey := range transactions.Keys() {
		entry, ok := transactions.Get(endpointKey)
		// If called in syncInternal, as the transaction table
		if !ok {
			logger.V(2).Info("Transaction entry of key was not found.", "endpointKey", endpointKey)
			continue
		}
		key := negtypes.NEGLocation{Zone: entry.Zone, Subnet: entry.Subnet}
		// Add endpoints in attach transaction
		if entry.Operation == attachOp {
			if _, ok := endpointMap[key]; !ok {
				endpointMap[key] = negtypes.NewNetworkEndpointSet()
			}
			endpointMap[key].Insert(endpointKey)
		}
		// Remove endpoints in detach transaction
		if entry.Operation == detachOp {
			if _, ok := endpointMap[key]; !ok {
				continue
			}
			endpointMap[key].Delete(endpointKey)
		}
	}
}

// logStats logs aggregated stats of the input endpointMap
func (s *transactionSyncer) logStats(endpointMap map[negtypes.NEGLocation]negtypes.NetworkEndpointSet, desc string) {
	var keyAndValues []any
	keyAndValues = append(keyAndValues, "description" /* key */, desc /* value */)
	for negLocation, endpointSet := range endpointMap {
		key := fmt.Sprintf("{zone:'%v',subnet:'%v'}", negLocation.Zone, negLocation.Subnet)
		value := fmt.Sprintf("%d endpoints", endpointSet.Len())
		keyAndValues = append(keyAndValues, key, value)
	}
	s.logger.V(3).Info("Stats for NEGs", keyAndValues...)
}

// logEndpoints logs individual endpoint in the input endpointMap
func (s *transactionSyncer) logEndpoints(endpointMap map[negtypes.NEGLocation]negtypes.NetworkEndpointSet, desc string) {
	s.logger.V(3).Info("Endpoints for NEG", "description", desc, "endpointMap", fmt.Sprintf("%+v", endpointMap))
}

func convertUntypedToEPS(endpointSliceUntyped []interface{}) []*discovery.EndpointSlice {
	endpointSlices := make([]*discovery.EndpointSlice, len(endpointSliceUntyped))
	for i, slice := range endpointSliceUntyped {
		endpointslice := slice.(*discovery.EndpointSlice)
		endpointSlices[i] = endpointslice
	}
	return endpointSlices
}

// computeDegradedModeCorrectness computes degraded mode correctness metrics based on the difference between degraded mode and normal calculation
func computeDegradedModeCorrectness(notInDegraded, onlyInDegraded map[negtypes.NEGLocation]negtypes.NetworkEndpointSet, negType string, logger klog.Logger, m *metrics.NegMetrics) {
	logger.Info("Exporting degraded mode correctness metrics", "notInDegraded", fmt.Sprintf("%+v", notInDegraded), "onlyInDegraded", fmt.Sprintf("%+v", onlyInDegraded))
	notInDegradedEndpoints := 0
	for _, val := range notInDegraded {
		notInDegradedEndpoints += len(val)
	}
	m.PublishDegradedModeCorrectnessMetrics(notInDegradedEndpoints, metrics.NotInDegradedEndpoints, negType)
	onlyInDegradedEndpoints := 0
	for _, val := range onlyInDegraded {
		onlyInDegradedEndpoints += len(val)
	}
	m.PublishDegradedModeCorrectnessMetrics(onlyInDegradedEndpoints, metrics.OnlyInDegradedEndpoints, negType)
}

// getEndpointPodLabelMap goes through all the endpoints to be attached and fetches the labels from the endpoint pods.
func getEndpointPodLabelMap(endpoints map[negtypes.NEGLocation]negtypes.NetworkEndpointSet, endpointPodMap negtypes.EndpointPodMap, podLister cache.Store, lpConfig labels.PodLabelPropagationConfig, recorder record.EventRecorder, logger klog.Logger, m *metrics.NegMetrics) labels.EndpointPodLabelMap {
	endpointPodLabelMap := labels.EndpointPodLabelMap{}
	for _, endpointSet := range endpoints {
		for endpoint := range endpointSet {
			key := fmt.Sprintf("%s/%s", endpointPodMap[endpoint].Namespace, endpointPodMap[endpoint].Name)
			obj, ok, err := podLister.GetByKey(key)
			if err != nil || !ok {
				metrics.PublishLabelPropagationError(labels.OtherError)
				logger.Error(err, "getEndpointPodLabelMap: error getting pod", "pod", key, "exist", ok)
				m.PublishNegControllerErrorCountMetrics(err, true)
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
				recorder.Eventf(pod, v1.EventTypeWarning, "LabelsExceededLimit", "Label Propagation Error: %v", err)
				m.PublishNegControllerErrorCountMetrics(err, true)
			}
			endpointPodLabelMap[endpoint] = labelMap
		}
	}
	return endpointPodLabelMap
}

// publishAnnotationSizeMetrics goes through all the endpoints to be attached
// and publish annotation size metrics.
func publishAnnotationSizeMetrics(endpoints map[negtypes.NEGLocation]negtypes.NetworkEndpointSet, endpointPodLabelMap labels.EndpointPodLabelMap) {
	for _, endpointSet := range endpoints {
		for endpoint := range endpointSet {
			labelMap := endpointPodLabelMap[endpoint]
			metrics.PublishAnnotationMetrics(labels.GetPodLabelMapSize(labelMap), len(labelMap))
		}
	}
}

// collectLabelStats calculate the number of endpoints and the number of endpoints with annotations.
func collectLabelStats(currentPodLabelMap, addPodLabelMap labels.EndpointPodLabelMap, targetEndpointMap map[negtypes.NEGLocation]negtypes.NetworkEndpointSet) metricscollector.LabelPropagationStats {
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
