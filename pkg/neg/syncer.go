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
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang/glog"
	compute "google.golang.org/api/compute/v0.beta"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/clock"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/kubernetes/pkg/cloudprovider/providers/gce"
)

const (
	MAX_NETWORK_ENDPOINTS_PER_BATCH = 500
	// For each NEG, only retries 15 times to process it.
	// This is a convention in kube-controller-manager.
	maxRetries    = 15
	minRetryDelay = 5 * time.Second
	maxRetryDelay = 600 * time.Second
)

// servicePort includes information to uniquely identify a NEG
type servicePort struct {
	namespace  string
	name       string
	port       int32
	targetPort string
}

// syncer handles synchorizing NEGs for one service port. It handles sync, resync and retry on error.
type syncer struct {
	servicePort
	negName string

	serviceLister  cache.Indexer
	endpointLister cache.Indexer

	recorder   record.EventRecorder
	cloud      NetworkEndpointGroupCloud
	zoneGetter zoneGetter

	stateLock    sync.Mutex
	stopped      bool
	shuttingDown bool

	clock          clock.Clock
	syncCh         chan interface{}
	lastRetryDelay time.Duration
	retryCount     int
}

func newSyncer(svcPort servicePort, networkEndpointGroupName string, recorder record.EventRecorder, cloud NetworkEndpointGroupCloud, zoneGetter zoneGetter, serviceLister cache.Indexer, endpointLister cache.Indexer) *syncer {
	glog.V(2).Infof("New syncer for service %s/%s port %s NEG %q", svcPort.namespace, svcPort.name, svcPort.targetPort, networkEndpointGroupName)
	return &syncer{
		servicePort:    svcPort,
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

func (s *syncer) init() {
	s.stateLock.Lock()
	defer s.stateLock.Unlock()
	s.stopped = false
	s.syncCh = make(chan interface{}, 1)
}

// Start starts the syncer go routine if it has not been started.
func (s *syncer) Start() error {
	if !s.IsStopped() {
		return fmt.Errorf("NEG syncer for %s is already running.", s.formattedName())
	}
	if s.IsShuttingDown() {
		return fmt.Errorf("NEG syncer for %s is shutting down. ", s.formattedName())
	}

	glog.V(2).Infof("Starting NEG syncer for service port %s", s.formattedName())
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

				if svc := getService(s.serviceLister, s.namespace, s.name); svc != nil {
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
					glog.V(2).Infof("Stopping NEG syncer for %s", s.formattedName())
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
func (s *syncer) Stop() {
	s.stateLock.Lock()
	defer s.stateLock.Unlock()
	if !s.stopped {
		glog.V(2).Infof("Stopping NEG syncer for service port %s", s.formattedName())
		s.stopped = true
		s.shuttingDown = true
		close(s.syncCh)
	}
}

// Sync informs syncer to run sync loop as soon as possible.
func (s *syncer) Sync() bool {
	if s.IsStopped() {
		glog.Warningf("NEG syncer for %s is already stopped.", s.formattedName())
		return false
	}
	select {
	case s.syncCh <- struct{}{}:
		return true
	default:
		return false
	}
}

func (s *syncer) IsStopped() bool {
	s.stateLock.Lock()
	defer s.stateLock.Unlock()
	return s.stopped
}

func (s *syncer) IsShuttingDown() bool {
	s.stateLock.Lock()
	defer s.stateLock.Unlock()
	return s.shuttingDown
}

func (s *syncer) sync() (err error) {
	if s.IsStopped() || s.IsShuttingDown() {
		glog.V(4).Infof("Skip syncing NEG %q for %s.", s.negName, s.formattedName())
		return nil
	}
	glog.V(2).Infof("Sync NEG %q for %s.", s.negName, s.formattedName())
	start := time.Now()
	defer observeNegSync(s.negName, attachSync, err, start)
	ep, exists, err := s.endpointLister.Get(
		&apiv1.Endpoints{
			ObjectMeta: metav1.ObjectMeta{
				Name:      s.name,
				Namespace: s.namespace,
			},
		},
	)
	if err != nil {
		return err
	}

	if !exists {
		glog.Warningf("Endpoint %s/%s does not exist. Skipping NEG sync", s.namespace, s.name)
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
		glog.V(4).Infof("No endpoint change for %s/%s, skip syncing NEG. ", s.namespace, s.name)
		return nil
	}

	return s.syncNetworkEndpoints(addEndpoints, removeEndpoints)
}

// ensureNetworkEndpointGroups ensures negs are created in the related zones.
func (s *syncer) ensureNetworkEndpointGroups() error {
	var err error
	zones, err := s.zoneGetter.ListZones()
	if err != nil {
		return err
	}

	var errList []error
	for _, zone := range zones {
		// Assume error is caused by not existing
		neg, err := s.cloud.GetNetworkEndpointGroup(s.negName, zone)
		if err != nil {
			// Most likely to be caused by non-existed NEG
			glog.V(4).Infof("Error while retriving %q in zone %q: %v", s.negName, zone, err)
		}

		needToCreate := false
		if neg == nil {
			needToCreate = true
		} else if !utils.EqualResourceIDs(neg.LoadBalancer.Network, s.cloud.NetworkURL()) ||
			!utils.EqualResourceIDs(neg.LoadBalancer.Subnetwork, s.cloud.SubnetworkURL()) {
			needToCreate = true
			glog.V(2).Infof("NEG %q in %q does not match network and subnetwork of the cluster. Deleting NEG.", s.negName, zone)
			err = s.cloud.DeleteNetworkEndpointGroup(s.negName, zone)
			if err != nil {
				errList = append(errList, err)
			} else {
				if svc := getService(s.serviceLister, s.namespace, s.name); svc != nil {
					s.recorder.Eventf(svc, apiv1.EventTypeNormal, "Delete", "Deleted NEG %q for %s in %q.", s.negName, s.formattedName(), zone)
				}
			}
		}

		if needToCreate {
			glog.V(2).Infof("Creating NEG %q for %s in %q.", s.negName, s.formattedName(), zone)
			err = s.cloud.CreateNetworkEndpointGroup(&compute.NetworkEndpointGroup{
				Name:                s.negName,
				NetworkEndpointType: gce.NEGIPPortNetworkEndpointType,
				LoadBalancer: &compute.NetworkEndpointGroupLbNetworkEndpointGroup{
					Network:    s.cloud.NetworkURL(),
					Subnetwork: s.cloud.SubnetworkURL(),
				},
			}, zone)
			if err != nil {
				errList = append(errList, err)
			} else {
				if svc := getService(s.serviceLister, s.namespace, s.name); svc != nil {
					s.recorder.Eventf(svc, apiv1.EventTypeNormal, "Create", "Created NEG %q for %s in %q.", s.negName, s.formattedName(), zone)
				}
			}
		}
	}
	return utilerrors.NewAggregate(errList)
}

// toZoneNetworkEndpointMap translates addresses in endpoints object into zone and endpoints map
func (s *syncer) toZoneNetworkEndpointMap(endpoints *apiv1.Endpoints) (map[string]sets.String, error) {
	zoneNetworkEndpointMap := map[string]sets.String{}
	targetPort, _ := strconv.Atoi(s.targetPort)
	for _, subset := range endpoints.Subsets {
		matchPort := ""
		// service spec allows target port to be a named port.
		// support both explicit port and named port.
		for _, port := range subset.Ports {
			if targetPort != 0 {
				// targetPort is int
				if int(port.Port) == targetPort {
					matchPort = s.targetPort
				}
			} else {
				// targetPort is string
				if port.Name == s.targetPort {
					matchPort = strconv.Itoa(int(port.Port))
				}
			}
			if len(matchPort) > 0 {
				break
			}
		}

		// subset does not contain target port
		if len(matchPort) == 0 {
			continue
		}
		for _, address := range subset.Addresses {
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
func (s *syncer) retrieveExistingZoneNetworkEndpointMap() (map[string]sets.String, error) {
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

type ErrorList struct {
	errList []error
	lock    sync.Mutex
}

func (e *ErrorList) Add(err error) {
	e.lock.Lock()
	defer e.lock.Unlock()
	e.errList = append(e.errList, err)
}

func (e *ErrorList) List() []error {
	e.lock.Lock()
	defer e.lock.Unlock()
	return e.errList
}

// syncNetworkEndpoints adds and removes endpoints for negs
func (s *syncer) syncNetworkEndpoints(addEndpoints, removeEndpoints map[string]sets.String) error {
	var wg sync.WaitGroup
	errList := &ErrorList{}

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
func (s *syncer) toNetworkEndpointBatch(endpoints sets.String) ([]*compute.NetworkEndpoint, error) {
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
			return nil, fmt.Errorf("Failed to decode endpoint %q: %v", enc, err)
		}
		networkEndpointList[i] = &compute.NetworkEndpoint{
			Instance:  instance,
			IpAddress: ip,
			Port:      int64(portNum),
		}
	}
	return networkEndpointList, nil
}

func (s *syncer) attachNetworkEndpoints(wg *sync.WaitGroup, zone string, networkEndpoints []*compute.NetworkEndpoint, errList *ErrorList) {
	wg.Add(1)
	glog.V(2).Infof("Attaching %d endpoint(s) for %s in NEG %s at %s.", len(networkEndpoints), s.formattedName(), s.negName, zone)
	go s.operationInternal(wg, zone, networkEndpoints, errList, s.cloud.AttachNetworkEndpoints, "Attach")
}

func (s *syncer) detachNetworkEndpoints(wg *sync.WaitGroup, zone string, networkEndpoints []*compute.NetworkEndpoint, errList *ErrorList) {
	wg.Add(1)
	glog.V(2).Infof("Detaching %d endpoint(s) for %s in NEG %s at %s.", len(networkEndpoints), s.formattedName(), s.negName, zone)
	go s.operationInternal(wg, zone, networkEndpoints, errList, s.cloud.DetachNetworkEndpoints, "Detach")
}

func (s *syncer) operationInternal(wg *sync.WaitGroup, zone string, networkEndpoints []*compute.NetworkEndpoint, errList *ErrorList, syncFunc func(name, zone string, endpoints []*compute.NetworkEndpoint) error, operationName string) {
	defer wg.Done()
	err := syncFunc(s.negName, zone, networkEndpoints)
	if err != nil {
		errList.Add(err)
	}
	if svc := getService(s.serviceLister, s.namespace, s.name); svc != nil {
		if err == nil {
			s.recorder.Eventf(svc, apiv1.EventTypeNormal, operationName, "%s %d network endpoint(s) (NEG %q in zone %q)", operationName, len(networkEndpoints), s.negName, zone)
		} else {
			s.recorder.Eventf(svc, apiv1.EventTypeWarning, operationName+"Failed", "Failed to %s %d network endpoint(s) (NEG %q in zone %q): %v", operationName, len(networkEndpoints), s.negName, zone, err)
		}
	}
}

func (s *syncer) nextRetryDelay() time.Duration {
	s.retryCount += 1
	s.lastRetryDelay *= 2
	if s.lastRetryDelay < minRetryDelay {
		s.lastRetryDelay = minRetryDelay
	} else if s.lastRetryDelay > maxRetryDelay {
		s.lastRetryDelay = maxRetryDelay
	}
	return s.lastRetryDelay
}

func (s *syncer) resetRetryDelay() {
	s.retryCount = 0
	s.lastRetryDelay = time.Duration(0)
}

func (s *syncer) formattedName() string {
	return fmt.Sprintf("%s/%s-%v/%s", s.namespace, s.name, s.port, s.targetPort)
}

// encodeEndpoint encodes ip and instance into a single string
func encodeEndpoint(ip, instance, port string) string {
	return fmt.Sprintf("%s||%s||%s", ip, instance, port)
}

// decodeEndpoint decodes ip and instance from an encoded string
func decodeEndpoint(str string) (string, string, string) {
	strs := strings.Split(str, "||")
	return strs[0], strs[1], strs[2]
}

// calculateDifference determines what endpoints needs to be added and removed in order to move current state to target state.
func calculateDifference(targetMap, currentMap map[string]sets.String) (map[string]sets.String, map[string]sets.String) {
	addSet := map[string]sets.String{}
	removeSet := map[string]sets.String{}
	for zone, endpointSet := range targetMap {
		diff := endpointSet.Difference(currentMap[zone])
		if len(diff) > 0 {
			addSet[zone] = diff
		}
	}

	for zone, endpointSet := range currentMap {
		diff := endpointSet.Difference(targetMap[zone])
		if len(diff) > 0 {
			removeSet[zone] = diff
		}
	}
	return addSet, removeSet
}

// getService retrieves service object from serviceLister based on the input namespace and name
func getService(serviceLister cache.Indexer, namespace, name string) *apiv1.Service {
	service, exists, err := serviceLister.GetByKey(serviceKeyFunc(namespace, name))
	if exists && err == nil {
		return service.(*apiv1.Service)
	}
	if err != nil {
		glog.Errorf("Failed to retrieve service %s/%s from store: %v", namespace, name, err)
	}
	return nil
}
