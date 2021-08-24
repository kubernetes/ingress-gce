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
	"k8s.io/apimachinery/pkg/util/clock"
	"strconv"
	"sync"
	"time"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/tools/cache"
	"k8s.io/ingress-gce/pkg/composite"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/klog"
)

const (
	healthyState = "HEALTHY"

	// retryDelay is the delay to retry health status polling.
	// GCE NEG API RPS quota is rate limited per every 100 seconds.
	// Make this retry delay to match the ratelimiting interval.
	// More detail: https://cloud.google.com/compute/docs/api-rate-limits
	retryDelay = 100 * time.Second
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
	// syncPod syncs the NEG readiness gate condition of the given pod.
	// podKey is the key to the pod. It is the namespaced name in the format of "namespace/name"
	// neg is the key of the NEG resource
	// backendService is the key of the BackendService resource.
	syncPod(podKey string, neg, backendService *meta.Key) error
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

	clock clock.Clock
}

func NewPoller(podLister cache.Indexer, lookup NegLookup, patcher podStatusPatcher, negCloud negtypes.NetworkEndpointGroupCloud) *poller {
	return &poller{
		pollMap:   make(map[negMeta]*pollTarget),
		podLister: podLister,
		lookup:    lookup,
		patcher:   patcher,
		negCloud:  negCloud,
		clock:     clock.RealClock{},
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

	klog.V(2).Infof("polling NEG %q in zone %q", key.Name, key.Zone)
	// TODO(freehan): filter the NEs that are in interest once the API supports it
	res, err := p.negCloud.ListNetworkEndpoints(key.Name, key.Zone /*showHealthStatus*/, true, key.SyncerKey.GetAPIVersion())
	if err != nil {
		// On receiving GCE API error, do not retry immediately. This is to prevent the reflector to overwhelm the GCE NEG API when
		// rate limiting is in effect. This will prevent readiness reflector to overwhelm the GCE NEG API and cause NEG syncers to backoff.
		// This will effectively batch NEG health status updates for 100s. The pods added into NEG in this 100s will not be marked ready
		// until the next status poll is executed. However, the pods are not marked as Ready and still passes the LB health check will
		// serve LB traffic. The side effect during the delay period is the workload (depending on rollout strategy) might slow down rollout.
		// TODO(freehan): enable exponential backoff.
		klog.Errorf("Failed to ListNetworkEndpoint in NEG %q, retry in %v", key.String(), retryDelay.String())
		<-p.clock.After(retryDelay)
		return true, err
	}

	return p.processHealthStatus(key, res)
}

// processHealthStatus updates Pod readiness gates based on the input health status response.
//
// We update the pod (using the patcher) when:
// 1. if the endpoint considered healthy with one of the backend service health check
// 2. if the NEG is not associated with any health checks
// It returns true if retry is needed.
func (p *poller) processHealthStatus(key negMeta, healthStatuses []*composite.NetworkEndpointWithHealthStatus) (bool, error) {
	p.lock.Lock()
	defer p.lock.Unlock()
	klog.V(4).Infof("processHealthStatus(%q, %+v)", key.String(), healthStatuses)

	var (
		errList []error
		// healthChecked indicates whether at least one of the endpoint in response has health status.
		// If a NEG is attached to a Backend Service with health check, all endpoints
		// in the NEG will be health checked. However, for new endpoints, it may take a while for the
		// health check to start. The assumption is that if at least one of the endpoint in a NEG has
		// health status, all the endpoints in the NEG should have health status eventually.
		healthChecked bool
		// patchCount is the count of the pod got patched
		patchCount    int
		unhealthyPods []types.NamespacedName
	)

	for _, healthStatus := range healthStatuses {
		if healthStatus == nil {
			klog.Warningf("healthStatus is nil from response %+v", healthStatuses)
			continue
		}

		if healthStatus.NetworkEndpoint == nil {
			klog.Warningf("Health status has nil associated network endpoint: %v", healthStatus)
			continue
		}

		healthChecked = healthChecked || hasSupportedHealthStatus(healthStatus)

		ne := negtypes.NetworkEndpoint{
			IP:   healthStatus.NetworkEndpoint.IpAddress,
			Port: strconv.FormatInt(healthStatus.NetworkEndpoint.Port, 10),
			Node: healthStatus.NetworkEndpoint.Instance,
		}

		podName, ok := p.getPod(key, ne)
		if !ok {
			// The pod is not in interest. Skip
			continue
		}

		bsKey := getHealthyBackendService(healthStatus)
		if bsKey == nil {
			unhealthyPods = append(unhealthyPods, podName)
			continue
		}

		err := p.patcher.syncPod(keyFunc(podName.Namespace, podName.Name), meta.ZonalKey(key.Name, key.Zone), bsKey)
		if err != nil {
			errList = append(errList, err)
			continue
		}
		patchCount++
	}

	// if the NEG is not health checked, signal the patcher to mark the unhealthy pods to be Ready.
	// This is most likely due to health check is not configured for the NEG. Hence none of the endpoints
	// in the NEG has health status.
	if !healthChecked {
		for _, podName := range unhealthyPods {
			err := p.patcher.syncPod(keyFunc(podName.Namespace, podName.Name), meta.ZonalKey(key.Name, key.Zone), nil)
			if err != nil {
				errList = append(errList, err)
				continue
			}
			patchCount++
		}
	}

	retry := false
	if target, ok := p.pollMap[key]; ok {
		if patchCount < len(target.endpointMap) {
			retry = true
		}
	}

	// If we didn't patch all of the endpoints, we must keep polling for health status
	return retry, utilerrors.NewAggregate(errList)
}

// getHealthyBackendService returns one of the first backend service key where the endpoint is considered healthy.
func getHealthyBackendService(healthStatus *composite.NetworkEndpointWithHealthStatus) *meta.Key {
	for _, hs := range healthStatus.Healths {
		if hs == nil {
			klog.Errorf("Health status is nil in health status of network endpoint %v ", healthStatus)
			continue
		}
		if hs.BackendService == nil {
			klog.Errorf("Backend service is nil in health status of network endpoint %v", healthStatus)
			continue
		}

		if hs.HealthState == healthyState {
			id, err := cloud.ParseResourceURL(hs.BackendService.BackendService)
			if err != nil {
				klog.Errorf("Failed to parse backend service reference from a Network Endpoint health status %v: %v", healthStatus, err)
				continue
			}
			if id != nil {
				return id.Key
			}
		}
	}
	return nil
}

// hasSupportedHealthStatus returns true if there is at least 1 backendService health status associated with the endpoint.
func hasSupportedHealthStatus(healthStatus *composite.NetworkEndpointWithHealthStatus) bool {
	if healthStatus == nil {
		return false
	}

	for _, health := range healthStatus.Healths {
		// TODO(freehan): Support more types of health status associated NEGs.
		if health.BackendService != nil {
			return true
		}
	}
	return false
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
