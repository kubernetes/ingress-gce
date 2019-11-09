/*
Copyright 2019 The Kubernetes Authors.

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

package readiness

import (
	"fmt"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/tools/cache"
	"k8s.io/ingress-gce/pkg/composite"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/klog"
	"strconv"
	"strings"
	"sync"
)

const (
	healthyState = "HEALTHY"
)

// negMeta references a GCE NEG resource
type negMeta struct {
	SyncerKey negtypes.NegSyncerKey
	// Name is the name of the NEG
	Name string
	// Zone is the zone of the NEG resource
	Zone string
}

func (n negMeta) String() string {
	return fmt.Sprintf("%s-%s-%s", n.SyncerKey.String(), n.Name, n.Zone)
}

// podStatusPatcher interface allows patching pod status
type podStatusPatcher interface {
	// syncPod patches the neg condition in the pod status to be True.
	// key is the key to the pod. It is the namespaced name in the format of "namespace/name"
	// negName is the name of the NEG resource
	syncPod(key, negName string) error
}

// pollTarget is the target for polling
type pollTarget struct {
	// endpointMap maps network endpoint to namespaced name of pod
	endpointMap negtypes.EndpointPodMap
	// polling indicates if the NEG is being polled
	polling bool
}

// poller tracks the negs and corresponding targets needed to be polled.
type poller struct {
	lock sync.Mutex
	// pollMap contains negs and corresponding targets needed to be polled.
	// all operations(read, write) to the pollMap are lock protected.
	pollMap map[negMeta]*pollTarget

	podLister cache.Indexer
	lookup    NegLookup
	patcher   podStatusPatcher
	negCloud  negtypes.NetworkEndpointGroupCloud
}

func NewPoller(podLister cache.Indexer, lookup NegLookup, patcher podStatusPatcher, negCloud negtypes.NetworkEndpointGroupCloud) *poller {
	return &poller{
		pollMap:   make(map[negMeta]*pollTarget),
		podLister: podLister,
		lookup:    lookup,
		patcher:   patcher,
		negCloud:  negCloud,
	}
}

// RegisterNegEndpoints registered the endpoints that needed to be poll for the NEG with lock
func (p *poller) RegisterNegEndpoints(key negMeta, endpointMap negtypes.EndpointPodMap) {
	p.lock.Lock()
	defer p.lock.Unlock()
	p.registerNegEndpoints(key, endpointMap)
}

// registerNegEndpoints registered the endpoints that needed to be poll for the NEG
// It returns false if there is no endpoints needed to be polled, returns true if otherwise.
// Assumes p.lock is held when calling this method.
func (p *poller) registerNegEndpoints(key negMeta, endpointMap negtypes.EndpointPodMap) bool {
	endpointsToPoll := needToPoll(key.SyncerKey, endpointMap, p.lookup, p.podLister)
	if len(endpointsToPoll) == 0 {
		delete(p.pollMap, key)
		return false
	}

	if v, ok := p.pollMap[key]; ok {
		v.endpointMap = endpointsToPoll
	} else {
		p.pollMap[key] = &pollTarget{endpointMap: endpointsToPoll}
	}
	return true
}

// ScanForWork returns the list of NEGs that should be polled
func (p *poller) ScanForWork() []negMeta {
	p.lock.Lock()
	defer p.lock.Unlock()
	var ret []negMeta
	for key, target := range p.pollMap {
		if target.polling {
			continue
		}
		if p.registerNegEndpoints(key, target.endpointMap) {
			ret = append(ret, key)
		}
	}
	return ret
}

// Poll polls a NEG and returns error plus whether retry is needed
// This function is threadsafe.
func (p *poller) Poll(key negMeta) (retry bool, err error) {
	if !p.markPolling(key) {
		klog.V(4).Infof("NEG %q in zone %q as is already being polled or no longer needed to be polled.", key.Name, key.Zone)
		return true, nil
	}
	defer p.unMarkPolling(key)

	// TODO(freehan): refactor errList from pkg/neg/syncers to be reused here
	var errList []error
	klog.V(2).Infof("polling NEG %q in zone %q", key.Name, key.Zone)
	// TODO(freehan): filter the NEs that are in interest once the API supports it
	res, err := p.negCloud.ListNetworkEndpoints(key.Name, key.Zone /*showHealthStatus*/, true, key.SyncerKey.GetAPIVersion())
	if err != nil {
		return true, err
	}

	// Traverse the response and check if the endpoints in interest are HEALTHY
	func() {
		p.lock.Lock()
		defer p.lock.Unlock()
		var healthyCount int
		for _, r := range res {
			healthy, err := p.processHealthStatus(key, r)
			if healthy && err == nil {
				healthyCount++
			}
			if err != nil {
				errList = append(errList, err)
			}
		}
		if healthyCount != len(p.pollMap[key].endpointMap) {
			retry = true
		}
	}()
	return retry, utilerrors.NewAggregate(errList)
}

// processHealthStatus evaluates the health status of the input network endpoint.
// Assumes p.lock is held when calling this method.
func (p *poller) processHealthStatus(key negMeta, healthStatus *composite.NetworkEndpointWithHealthStatus) (healthy bool, err error) {
	ne := negtypes.NetworkEndpoint{
		IP:   healthStatus.NetworkEndpoint.IpAddress,
		Port: strconv.FormatInt(healthStatus.NetworkEndpoint.Port, 10),
		Node: healthStatus.NetworkEndpoint.Instance,
	}
	podName, ok := p.getPod(key, ne)
	if !ok {
		return false, nil
	}

	for _, hs := range healthStatus.Healths {
		if hs == nil {
			continue
		}
		if hs.BackendService == nil {
			klog.Warningf("Backend service is nil in health status of network endpoint %v: %v", ne, hs)
			continue
		}

		// This assumes the ingress backend service uses the NEG naming scheme. Hence the backend service share the same name as NEG.
		if strings.Contains(hs.BackendService.BackendService, key.Name) {
			if hs.HealthState == healthyState {
				healthy = true
				err := p.patcher.syncPod(keyFunc(podName.Namespace, podName.Name), key.Name)
				return healthy, err
			}
		}

	}
	return false, nil
}

// getPod returns the namespaced name of a pod corresponds to an endpoint and whether the pod is registered
// Assumes p.lock is held when calling this method.
func (p *poller) getPod(key negMeta, endpoint negtypes.NetworkEndpoint) (namespacedName types.NamespacedName, exists bool) {
	t, ok := p.pollMap[key]
	if !ok {
		return types.NamespacedName{}, false
	}
	ret, ok := t.endpointMap[endpoint]
	return ret, ok
}

// markPolling returns true if the NEG is successfully marked as polling
func (p *poller) markPolling(key negMeta) bool {
	p.lock.Lock()
	defer p.lock.Unlock()
	t, ok := p.pollMap[key]
	if !ok {
		return false
	}
	if t.polling {
		return false
	}
	t.polling = true
	return true
}

// unMarkPolling unmarks the NEG
func (p *poller) unMarkPolling(key negMeta) {
	p.lock.Lock()
	defer p.lock.Unlock()
	if t, ok := p.pollMap[key]; ok {
		t.polling = false
	}
}
