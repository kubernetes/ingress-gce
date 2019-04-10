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

package syncers

import (
	"fmt"
	"math"
	"strconv"
	"sync"
	"time"

	compute "google.golang.org/api/compute/v0.beta"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/clock"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/ingress-gce/pkg/neg/metrics"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/ingress-gce/pkg/neg/utils"
	"k8s.io/klog"
)

// batchSyncer handles synchorizing NEGs for one service port. It handles sync, resync and retry on error.
// It syncs NEG in batch and waits for all operation to complete before continue to the next batch.
type batchSyncer struct {
	NegSyncerKey
	negName string

	serviceLister  cache.Indexer
	endpointLister cache.Indexer

	recorder   record.EventRecorder
	cloud      negtypes.NetworkEndpointGroupCloud
	zoneGetter negtypes.ZoneGetter

	stateLock    sync.Mutex
	stopped      bool
	shuttingDown bool

	clock          clock.Clock
	syncCh         chan interface{}
	lastRetryDelay time.Duration
	retryCount     int
}

func NewBatchSyncer(svcPort NegSyncerKey, networkEndpointGroupName string, recorder record.EventRecorder, cloud negtypes.NetworkEndpointGroupCloud, zoneGetter negtypes.ZoneGetter, serviceLister cache.Indexer, endpointLister cache.Indexer) *batchSyncer {
	klog.V(2).Infof("New syncer for service %s/%s Port %s NEG %q", svcPort.Namespace, svcPort.Name, svcPort.TargetPort, networkEndpointGroupName)
	return &batchSyncer{
		NegSyncerKey:   svcPort,
		negName:        networkEndpointGroupName,
		recorder:       recorder,
		serviceLister:  serviceLister,
		cloud:          cloud,
		endpointLister: endpointLister,
		zoneGetter:     zoneGetter,
		stopped:        true,
		shuttingDown:   false,
		clock:          clock.RealClock{},
		lastRetryDelay: time.Duration(0),
		retryCount:     0,
	}
}

func (s *batchSyncer) init() {
	s.stateLock.Lock()
	defer s.stateLock.Unlock()
	s.stopped = false
	s.syncCh = make(chan interface{}, 1)
}

// Start starts the syncer go routine if it has not been started.
func (s *batchSyncer) Start() error {
	if !s.IsStopped() {
		return fmt.Errorf("NEG syncer for %s is already running.", s.NegSyncerKey.String())
	}
	if s.IsShuttingDown() {
		return fmt.Errorf("NEG syncer for %s is shutting down. ", s.NegSyncerKey.String())
	}

	klog.V(2).Infof("Starting NEG syncer for service port %s", s.NegSyncerKey.String())
	s.init()
	go func() {
		for {
			// equivalent to never retry
			retryCh := make(<-chan time.Time)
			err := s.sync()
			if err != nil {
				retryMesg := ""
				if s.retryCount > maxRetries {
					retryMesg = "(will not retry)"
				} else {
					retryCh = s.clock.After(s.nextRetryDelay())
					retryMesg = "(will retry)"
				}

				if svc := getService(s.serviceLister, s.Namespace, s.Name); svc != nil {
					s.recorder.Eventf(svc, apiv1.EventTypeWarning, "SyncNetworkEndpointGroupFailed", "Failed to sync NEG %q %s: %v", s.negName, retryMesg, err)
				}
			} else {
				s.resetRetryDelay()
			}

			select {
			case _, open := <-s.syncCh:
				if !open {
					s.stateLock.Lock()
					s.shuttingDown = false
					s.stateLock.Unlock()
					klog.V(2).Infof("Stopping NEG syncer for %s", s.NegSyncerKey.String())
					return
				}
			case <-retryCh:
				// continue to sync
			}
		}
	}()
	return nil
}

// Stop stops syncer and return only when syncer shutdown completes.
func (s *batchSyncer) Stop() {
	s.stateLock.Lock()
	defer s.stateLock.Unlock()
	if !s.stopped {
		klog.V(2).Infof("Stopping NEG syncer for service port %s", s.NegSyncerKey.String())
		s.stopped = true
		s.shuttingDown = true
		close(s.syncCh)
	}
}

// Sync informs syncer to run sync loop as soon as possible.
func (s *batchSyncer) Sync() bool {
	if s.IsStopped() {
		klog.Warningf("NEG syncer for %s is already stopped.", s.NegSyncerKey.String())
		return false
	}
	select {
	case s.syncCh <- struct{}{}:
		return true
	default:
		return false
	}
}

func (s *batchSyncer) IsStopped() bool {
	s.stateLock.Lock()
	defer s.stateLock.Unlock()
	return s.stopped
}

func (s *batchSyncer) IsShuttingDown() bool {
	s.stateLock.Lock()
	defer s.stateLock.Unlock()
	return s.shuttingDown
}

func (s *batchSyncer) sync() (err error) {
	if s.IsStopped() || s.IsShuttingDown() {
		klog.V(4).Infof("Skip syncing NEG %q for %s.", s.negName, s.NegSyncerKey.String())
		return nil
	}
	klog.V(2).Infof("Sync NEG %q for %s.", s.negName, s.NegSyncerKey.String())
	start := time.Now()
	defer metrics.ObserveNegSync(s.negName, metrics.AttachSync, err, start)
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

	err = s.ensureNetworkEndpointGroups()
	if err != nil {
		return err
	}

	targetMap, err := s.toZoneNetworkEndpointMap(ep.(*apiv1.Endpoints))
	if err != nil {
		return err
	}

	currentMap, err := s.retrieveExistingZoneNetworkEndpointMap()
	if err != nil {
		return err
	}

	addEndpoints, removeEndpoints := calculateDifference(targetMap, currentMap)
	if len(addEndpoints) == 0 && len(removeEndpoints) == 0 {
		klog.V(4).Infof("No endpoint change for %s/%s, skip syncing NEG. ", s.Namespace, s.Name)
		return nil
	}

	return s.syncNetworkEndpoints(addEndpoints, removeEndpoints)
}

// ensureNetworkEndpointGroups ensures negs are created in the related zones.
func (s *batchSyncer) ensureNetworkEndpointGroups() error {
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

// toZoneNetworkEndpointMap translates addresses in endpoints object into zone and endpoints map
// TODO: migrate to use the util function instead
func (s *batchSyncer) toZoneNetworkEndpointMap(endpoints *apiv1.Endpoints) (map[string]sets.String, error) {
	zoneNetworkEndpointMap := map[string]sets.String{}
	targetPort, _ := strconv.Atoi(s.TargetPort)
	for _, subset := range endpoints.Subsets {
		matchPort := ""
		// service spec allows target Port to be a named Port.
		// support both explicit Port and named Port.
		for _, port := range subset.Ports {
			if targetPort != 0 {
				// TargetPort is int
				if int(port.Port) == targetPort {
					matchPort = s.TargetPort
				}
			} else {
				// TargetPort is string
				if port.Name == s.TargetPort {
					matchPort = strconv.Itoa(int(port.Port))
				}
			}
			if len(matchPort) > 0 {
				break
			}
		}

		// subset does not contain target Port
		if len(matchPort) == 0 {
			continue
		}
		for _, address := range subset.Addresses {
			if address.NodeName == nil {
				klog.V(2).Infof("Endpoint %q in Endpoints %s/%s does not have an associated node. Skipping", address.IP, endpoints.Namespace, endpoints.Name)
				continue
			}
			zone, err := s.zoneGetter.GetZoneForNode(*address.NodeName)
			if err != nil {
				return nil, err
			}
			if zoneNetworkEndpointMap[zone] == nil {
				zoneNetworkEndpointMap[zone] = sets.String{}
			}
			zoneNetworkEndpointMap[zone].Insert(encodeEndpoint(address.IP, *address.NodeName, matchPort))
		}
	}
	return zoneNetworkEndpointMap, nil
}

// retrieveExistingZoneNetworkEndpointMap lists existing network endpoints in the neg and return the zone and endpoints map
// TODO: migrate to use the util function instead
func (s *batchSyncer) retrieveExistingZoneNetworkEndpointMap() (map[string]sets.String, error) {
	zones, err := s.zoneGetter.ListZones()
	if err != nil {
		return nil, err
	}

	zoneNetworkEndpointMap := map[string]sets.String{}
	for _, zone := range zones {
		zoneNetworkEndpointMap[zone] = sets.String{}
		networkEndpointsWithHealthStatus, err := s.cloud.ListNetworkEndpoints(s.negName, zone, false)
		if err != nil {
			return nil, err
		}
		for _, ne := range networkEndpointsWithHealthStatus {
			zoneNetworkEndpointMap[zone].Insert(encodeEndpoint(ne.NetworkEndpoint.IpAddress, ne.NetworkEndpoint.Instance, strconv.FormatInt(ne.NetworkEndpoint.Port, 10)))
		}
	}
	return zoneNetworkEndpointMap, nil
}

// syncNetworkEndpoints adds and removes endpoints for negs
func (s *batchSyncer) syncNetworkEndpoints(addEndpoints, removeEndpoints map[string]sets.String) error {
	var wg sync.WaitGroup
	errList := &utils.ErrorList{}

	// Detach Endpoints
	for zone, endpointSet := range removeEndpoints {
		for {
			if endpointSet.Len() == 0 {
				break
			}
			networkEndpoints, err := s.toNetworkEndpointBatch(endpointSet)
			if err != nil {
				return err
			}
			s.detachNetworkEndpoints(&wg, zone, networkEndpoints, errList)
		}
	}

	// Attach Endpoints
	for zone, endpointSet := range addEndpoints {
		for {
			if endpointSet.Len() == 0 {
				break
			}
			networkEndpoints, err := s.toNetworkEndpointBatch(endpointSet)
			if err != nil {
				return err
			}
			s.attachNetworkEndpoints(&wg, zone, networkEndpoints, errList)
		}
	}
	wg.Wait()
	return utilerrors.NewAggregate(errList.List())
}

// translate a endpoints set to a batch of network endpoints object
func (s *batchSyncer) toNetworkEndpointBatch(endpoints sets.String) ([]*compute.NetworkEndpoint, error) {
	var ok bool
	list := make([]string, int(math.Min(float64(endpoints.Len()), float64(MAX_NETWORK_ENDPOINTS_PER_BATCH))))
	for i := range list {
		list[i], ok = endpoints.PopAny()
		if !ok {
			break
		}
	}
	networkEndpointList := make([]*compute.NetworkEndpoint, len(list))
	for i, enc := range list {
		ip, instance, port := decodeEndpoint(enc)
		portNum, err := strconv.Atoi(port)
		if err != nil {
			return nil, fmt.Errorf("failed to decode endpoint %q: %v", enc, err)
		}
		networkEndpointList[i] = &compute.NetworkEndpoint{
			Instance:  instance,
			IpAddress: ip,
			Port:      int64(portNum),
		}
	}
	return networkEndpointList, nil
}

func (s *batchSyncer) attachNetworkEndpoints(wg *sync.WaitGroup, zone string, networkEndpoints []*compute.NetworkEndpoint, errList *utils.ErrorList) {
	wg.Add(1)
	klog.V(2).Infof("Attaching %d endpoint(s) for %s in NEG %s at %s.", len(networkEndpoints), s.NegSyncerKey.String(), s.negName, zone)
	go s.operationInternal(wg, zone, networkEndpoints, errList, s.cloud.AttachNetworkEndpoints, "Attach")
}

func (s *batchSyncer) detachNetworkEndpoints(wg *sync.WaitGroup, zone string, networkEndpoints []*compute.NetworkEndpoint, errList *utils.ErrorList) {
	wg.Add(1)
	klog.V(2).Infof("Detaching %d endpoint(s) for %s in NEG %s at %s.", len(networkEndpoints), s.NegSyncerKey.String(), s.negName, zone)
	go s.operationInternal(wg, zone, networkEndpoints, errList, s.cloud.DetachNetworkEndpoints, "Detach")
}

func (s *batchSyncer) operationInternal(wg *sync.WaitGroup, zone string, networkEndpoints []*compute.NetworkEndpoint, errList *utils.ErrorList, syncFunc func(name, zone string, endpoints []*compute.NetworkEndpoint) error, operationName string) {
	defer wg.Done()
	err := syncFunc(s.negName, zone, networkEndpoints)
	if err != nil {
		errList.Add(err)
	}
	if svc := getService(s.serviceLister, s.Namespace, s.Name); svc != nil {
		if err == nil {
			s.recorder.Eventf(svc, apiv1.EventTypeNormal, operationName, "%s %d network endpoint(s) (NEG %q in zone %q)", operationName, len(networkEndpoints), s.negName, zone)
		} else {
			s.recorder.Eventf(svc, apiv1.EventTypeWarning, operationName+"Failed", "Failed to %s %d network endpoint(s) (NEG %q in zone %q): %v", operationName, len(networkEndpoints), s.negName, zone, err)
		}
	}
}

func (s *batchSyncer) nextRetryDelay() time.Duration {
	s.retryCount += 1
	s.lastRetryDelay *= 2
	if s.lastRetryDelay < minRetryDelay {
		s.lastRetryDelay = minRetryDelay
	} else if s.lastRetryDelay > maxRetryDelay {
		s.lastRetryDelay = maxRetryDelay
	}
	return s.lastRetryDelay
}

func (s *batchSyncer) resetRetryDelay() {
	s.retryCount = 0
	s.lastRetryDelay = time.Duration(0)
}
