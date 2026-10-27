package syncers

import (
	"fmt"
	"sync"
	"time"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/cache"
	negbindingv1beta1 "k8s.io/ingress-gce/pkg/apis/negbinding/v1beta1"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/neg/syncers/negstatushandler"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog/v2"
)

type negToClean struct {
	negName string
	zone    string
	ref     *composite.NetworkEndpointGroup
}

type cleanupSyncer struct {
	negtypes.NegSyncerKey
	cloud         negtypes.NetworkEndpointGroupCloud
	statusHandler *negstatushandler.NEGBindingStatusHandler
	bindingLister cache.Indexer
	expectedDesc  utils.BoundNEGDescription
	logger        klog.Logger

	syncCh chan struct{}
	stopCh chan struct{}

	stateLock  sync.Mutex
	stopped    bool
	retryDelay time.Duration
}

// NewCleanupSyncer creates a new syncer that only detaches endpoints from NEGs listed in status.
func NewCleanupSyncer(
	negSyncerKey negtypes.NegSyncerKey,
	cloud negtypes.NetworkEndpointGroupCloud,
	statusHandler *negstatushandler.NEGBindingStatusHandler,
	bindingLister cache.Indexer,
	clusterURL string,
	logger klog.Logger,
) negtypes.NegSyncer {
	return &cleanupSyncer{
		NegSyncerKey:  negSyncerKey,
		cloud:         cloud,
		statusHandler: statusHandler,
		bindingLister: bindingLister,
		expectedDesc: utils.BoundNEGDescription{
			ClusterURL: clusterURL,
			Namespace:  negSyncerKey.Namespace,
			BackendRef: negSyncerKey.Name,
		},
		logger:     logger.WithName("CleanupSyncer"),
		syncCh:     make(chan struct{}, 1),
		stopCh:     make(chan struct{}),
		stopped:    true,
		retryDelay: minRetryDelay,
	}
}

func (s *cleanupSyncer) Start() error {
	s.stateLock.Lock()
	defer s.stateLock.Unlock()
	if !s.stopped {
		return nil
	}
	s.stopped = false
	s.stopCh = make(chan struct{})
	s.syncCh = make(chan struct{}, 1)
	go s.loop()
	return nil
}

func (s *cleanupSyncer) Stop() {
	s.stateLock.Lock()
	defer s.stateLock.Unlock()
	if !s.stopped {
		s.stopped = true
		close(s.stopCh)
	}
}

func (s *cleanupSyncer) Sync() bool {
	if s.IsStopped() {
		return false
	}
	select {
	case s.syncCh <- struct{}{}:
		return true
	default:
		return false
	}
}

func (s *cleanupSyncer) IsStopped() bool {
	s.stateLock.Lock()
	defer s.stateLock.Unlock()
	return s.stopped
}

func (s *cleanupSyncer) IsShuttingDown() bool {
	return false
}

func (s *cleanupSyncer) loop() {
	for {
		select {
		case <-s.stopCh:
			return
		case <-s.syncCh:
			err := s.cleanUp()
			_, reportErr := s.statusHandler.ReportSyncStatus(err)
			if reportErr != nil {
				s.logger.Error(reportErr, "Failed to report sync status")
			}
			if err != nil {
				s.logger.Error(err, "Cleanup failed, will retry", "delay", s.retryDelay)
				time.AfterFunc(s.retryDelay, func() { s.Sync() })
				s.retryDelay = s.retryDelay * 2
				if s.retryDelay > maxRetryDelay {
					s.retryDelay = maxRetryDelay
				}
			} else {
				s.retryDelay = minRetryDelay
				s.logger.Info("Cleanup successful")
				s.Stop()
			}
		}
	}
}

func (s *cleanupSyncer) cleanUp() error {
	bindingKey := fmt.Sprintf("%s/%s", s.Namespace, s.NEGBindingName)
	bindingObj, exists, err := s.bindingLister.GetByKey(bindingKey)
	if err != nil {
		return fmt.Errorf("failed to get NEGBinding from lister: %w", err)
	}
	if !exists {
		s.logger.Info("NEGBinding no longer exists, stopping cleanup", "binding", bindingKey)
		s.Stop()
		return nil
	}
	binding := bindingObj.(*negbindingv1beta1.NetworkEndpointGroupBinding)

	negsToClean, remainingNEGs, errs := s.getNEGsToClean(binding)

	for _, neg := range negsToClean {
		if err := s.cleanUpNEG(neg.negName, neg.zone); err != nil {
			errs = append(errs, err)
			remainingNEGs = append(remainingNEGs, neg.ref)
		}
	}

	if err := s.statusHandler.ReportStatus(remainingNEGs, errs); err != nil {
		errs = append(errs, fmt.Errorf("failed to report status NEGs: %w", err))
	}
	return utilerrors.NewAggregate(errs)
}

func (s *cleanupSyncer) getNEGsToClean(binding *negbindingv1beta1.NetworkEndpointGroupBinding) ([]negToClean, []*composite.NetworkEndpointGroup, []error) {
	var negsToClean []negToClean
	var statusOnlyNEGs []*composite.NetworkEndpointGroup
	var errs []error
	seenLocations := sets.New[negtypes.NEGLocation]()

	for _, statusRef := range binding.Status.NetworkEndpointGroups {
		negID, err := cloud.ParseResourceURL(statusRef.ResourceURL)
		if err != nil {
			s.logger.Error(err, "Failed to parse NEG URL, assuming already cleaned up", "negURL", statusRef.ResourceURL)
			continue
		}
		subnetID, err := cloud.ParseResourceURL(statusRef.SubnetURL)
		if err != nil {
			s.logger.Error(err, "Failed to parse subnet URL, assuming already cleaned up", "subnetURL", statusRef.SubnetURL)
			continue
		}

		loc := negtypes.NEGLocation{
			Subnet: subnetID.Key.Name,
			Zone:   negID.Key.Zone,
		}
		if seenLocations.Has(loc) {
			s.logger.Info("Duplicate location in status, ignoring", "subnet", loc.Subnet, "zone", loc.Zone, "negURL", statusRef.ResourceURL)
			continue
		}
		seenLocations.Insert(loc)

		negName := negID.Key.Name
		zone := negID.Key.Zone
		negRef := &composite.NetworkEndpointGroup{
			Name:       negName,
			SelfLink:   statusRef.ResourceURL,
			Subnetwork: statusRef.SubnetURL,
		}

		neg, err := s.cloud.GetNetworkEndpointGroup(negName, zone, s.GetAPIVersion(), s.logger)
		if err != nil {
			if utils.IsNotFoundError(err) {
				s.logger.Info("NEG was not found during cleanup, assuming already cleaned up", "neg", negName, "zone", zone)
				continue
			}
			errs = append(errs, fmt.Errorf("failed to get NEG %q in zone %q: %w", negName, zone, err))
			statusOnlyNEGs = append(statusOnlyNEGs, negRef)
			continue
		}

		if !s.verifyNEG(neg, zone, statusRef.SubnetURL) {
			continue
		}

		negsToClean = append(negsToClean, negToClean{
			negName: negName,
			zone:    zone,
			ref:     negRef,
		})
	}

	return negsToClean, statusOnlyNEGs, errs
}

func (s *cleanupSyncer) verifyNEG(neg *composite.NetworkEndpointGroup, zone, expectedSubnetURL string) bool {
	if err := verifyNEGDescription(neg, zone, s.expectedDesc, true, s.logger); err != nil {
		s.logger.Error(err, "Skipping NEG cleanup due to description mismatch", "neg", neg.Name, "zone", zone)
		return false
	}
	if err := verifyNEGNetwork(neg, zone, s.NegType, s.cloud.NetworkURL(), expectedSubnetURL); err != nil {
		s.logger.Error(err, "Skipping NEG cleanup due to network/subnetwork mismatch", "neg", neg.Name, "zone", zone)
		return false
	}
	return true
}

func (s *cleanupSyncer) cleanUpNEG(negName, zone string) error {
	apiVersion := s.GetAPIVersion()

	s.logger.Info("Cleaning up endpoints in NEG", "neg", negName, "zone", zone)

	endpointsWithStatus, err := s.cloud.ListNetworkEndpoints(negName, zone, false, apiVersion, s.logger)
	if err != nil {
		if utils.IsNotFoundError(err) {
			s.logger.Info("NEG was not found during cleanup, assuming already cleaned up", "neg", negName, "zone", zone)
			return nil
		}
		return fmt.Errorf("failed to list endpoints for NEG %q in zone %q: %w", negName, zone, err)
	}

	if len(endpointsWithStatus) == 0 {
		s.logger.V(3).Info("NEG is already empty", "neg", negName, "zone", zone)
		return nil
	}

	endpoints := []*composite.NetworkEndpoint{}
	for _, ep := range endpointsWithStatus {
		endpoints = append(endpoints, ep.NetworkEndpoint)
	}

	s.logger.Info("Detaching endpoints from NEG during cleanup", "count", len(endpoints), "neg", negName, "zone", zone)
	err = s.cloud.DetachNetworkEndpoints(negName, zone, endpoints, apiVersion, s.logger)
	if err != nil {
		if utils.IsNotFoundError(err) {
			s.logger.Info("NEG was not found during detach, assuming already cleaned up", "neg", negName, "zone", zone)
			return nil
		}
		return fmt.Errorf("failed to detach endpoints from NEG %q in zone %q: %w", negName, zone, err)
	}

	return nil
}
