package readiness

import (
	"fmt"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/workqueue"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/ingress-gce/pkg/neg/utils"
	"k8s.io/klog"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// maxPolling is a big enough number that the poller should stop polling after the number of retry exceeds it.
	maxPolling = 10000000
)

type retryStatus struct {
	retry bool
	lock sync.Mutex
}

func (rs *retryStatus) Retry() {
	rs.lock.Lock()
	defer rs.lock.Unlock()
	rs.retry = true
}

func (rs *retryStatus) Status() bool {
	rs.lock.Lock()
	defer rs.lock.Unlock()
	return rs.retry
}

type negHealthStatusPoller struct {
	tracker  *podNegTracker
	patcher  podStatusPatcher
	negCloud negtypes.NetworkEndpointGroupCloud

	queue workqueue.RateLimitingInterface
}

func newNegHealthStatusPoller(tracker *podNegTracker, patcher podStatusPatcher, negCloud negtypes.NetworkEndpointGroupCloud) healthStatusPoller {
	return &negHealthStatusPoller{
		tracker:  tracker,
		patcher:  patcher,
		negCloud: negCloud,
		queue:    workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
	}
}

func (p *negHealthStatusPoller) Run(stopCh <-chan struct{}) {
	defer p.queue.ShutDown()
	klog.V(2).Infof("Starting NEG healthstatus poller")
	defer klog.V(2).Infof("Shutting down NEG healthstatus poller")
	// Run 2 copy of the worker
	for i := 0; i < 2; i++ {
		go wait.Until(p.worker, time.Second, stopCh)
	}
	<-stopCh
}

func (p *negHealthStatusPoller) Poll(neg negName) {
	klog.V(2).Infof("============================Poll %s", neg)
	p.queue.Add(neg)
}

func (p *negHealthStatusPoller) worker() {
	for p.processNextWorkItem() {
	}
}

func (p *negHealthStatusPoller) processNextWorkItem() bool {
	key, quit := p.queue.Get()
	if quit {
		return false
	}
	defer p.queue.Done(key)
	neg := key.(negName)

	err, retry := p.pollNeg(neg)
	p.handleErrAndRetry(err, retry, key)
	return true
}

func (p *negHealthStatusPoller) handleErrAndRetry(err error, retry bool, key interface{}) {
	if err == nil && !retry {
		p.queue.Forget(key)
		return
	}

	neg := key.(negName)
	if p.queue.NumRequeues(key) < maxPolling {
		if err != nil {
			klog.Errorf("Error polling pod %q, retrying. Error: %v", neg.String(), err)
		}

		klog.V(2).Infof("Retry polling neg %q.", neg.String())
		p.queue.AddRateLimited(key)
		return
	}
	klog.Warningf("Dropping pod %q out of the queue: %v", neg.String(), err)
	p.queue.Forget(key)
}

func (p *negHealthStatusPoller) pollNeg(neg negName) (error, bool) {
	zoneEndpointMap, ok := p.tracker.GetUnhealthyEndpointsByZone(neg)
	if !ok {
		klog.Warningf("NEG %q is no longer tracked. Skip polling neg health status", neg.String())
		return nil, false
	}
	var wg sync.WaitGroup
	errList := &utils.ErrorList{}
	retryStatus := &retryStatus{}
	for zone, endpiontMap := range zoneEndpointMap {
		// only poll the ones that has zone information
		if len(zone) > 0 {
			wg.Add(1)
			go p.poll(neg, zone, endpiontMap, errList, retryStatus)
		}
	}
	wg.Wait()
	return utilerrors.NewAggregate(errList.List()), retryStatus.Status()
}

// TODO: apply endpoint filtering when the filter is supported in the ListNetworkEndpoint API
func (p *negHealthStatusPoller) poll(neg negName, zone string, endpoints endpointInfoMap, errList *utils.ErrorList, rs *retryStatus) {
	klog.V(2).Infof("Polling NEG %q in zone %q", neg.String(), zone)
	res, err := p.negCloud.ListNetworkEndpoints(neg.String(), zone, true)
	if err != nil {
		errList.Add(err)
		return
	}
	klog.V(2).Infof("=============================Polling NEG %q in zone %q", neg.String(), zone)
	for _, r := range res {
		if podKey, ok := endpoints[endpointKey{IP: r.NetworkEndpoint.IpAddress, Port: strconv.FormatInt(r.NetworkEndpoint.Port, 10)}]; ok {
			healthy := false
			for _, hs := range r.Healths {
				if hs.BackendService != nil {
					// This assumes the ingress backend service uses the NEG naming scheme
					if strings.Contains(hs.BackendService.BackendService, neg.String()) {
						if hs.HealthState == "HEALTHY" {
							if negMap, ok := p.tracker.GetPodHealthStatus(podKey); ok {
								if !negMap[neg] {
									if err := p.tracker.UpdatePodHealthStatus(podKey, neg, true); err != nil {
										errList.Add(fmt.Errorf("failed to UpdatePodHealthStatus for pod %q: %v", podKey.String(), err))
									}
									p.patcher.Patch(podKey.Namespace, podKey.Name)
									healthy = true
								}
							}

						}
					}
				}
			}
			if !healthy {
				rs.Retry()
			}
		}

	}
}
