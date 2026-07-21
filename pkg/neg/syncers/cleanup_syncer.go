package syncers

import (
	"fmt"
	"sync"
	"time"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/tools/cache"
	negbindingv1beta1 "k8s.io/ingress-gce/pkg/apis/negbinding/v1beta1"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/neg/syncers/negstatushandler"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/klog/v2"
)

type cleanupSyncer struct {
	negtypes.NegSyncerKey
	cloud         negtypes.NetworkEndpointGroupCloud
	statusHandler *negstatushandler.NEGBindingStatusHandler
	bindingLister cache.Indexer
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
	logger klog.Logger,
) negtypes.NegSyncer {
	return &cleanupSyncer{
		NegSyncerKey:  negSyncerKey,
		cloud:         cloud,
		statusHandler: statusHandler,
		bindingLister: bindingLister,
		logger:        logger.WithName("CleanupSyncer"),
		syncCh:        make(chan struct{}, 1),
		stopCh:        make(chan struct{}),
		stopped:       true,
		retryDelay:    minRetryDelay,
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

	var errs []error
	for _, ref := range binding.Status.NetworkEndpointGroups {
		err := s.cleanUpNEG(ref.ResourceURL)
		if err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) == 0 && binding.DeletionTimestamp != nil {
		s.logger.Info("Binding is deleting and cleanup succeeded, clearing status NEGs to trigger finalizer removal")
		err := s.statusHandler.ReportStatus(nil, nil)
		if err != nil {
			return fmt.Errorf("failed to clear status NEGs: %w", err)
		}
	}
	return utilerrors.NewAggregate(errs)
}

func (s *cleanupSyncer) cleanUpNEG(negURL string) error {
	negID, err := cloud.ParseResourceURL(negURL)
	if err != nil {
		return fmt.Errorf("failed to parse NEG URL %q: %w", negURL, err)
	}

	negName := negID.Key.Name
	zone := negID.Key.Zone
	apiVersion := s.GetAPIVersion()

	s.logger.Info("Cleaning up endpoints in NEG", "neg", negName, "zone", zone)

	endpointsWithStatus, err := s.cloud.ListNetworkEndpoints(negName, zone, false, apiVersion, s.logger)
	if err != nil {
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
		return fmt.Errorf("failed to detach endpoints from NEG %q in zone %q: %w", negName, zone, err)
	}

	return nil
}
