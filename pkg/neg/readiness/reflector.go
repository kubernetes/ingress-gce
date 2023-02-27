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
	"reflect"
	"sync"
	"time"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/clock"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	unversionedcore "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/ingress-gce/pkg/neg/types/shared"
	"k8s.io/klog/v2"
)

const (
	maxRetries = 15
	// negReadyReason is the pod condition reason when pod becomes Healthy in NEG or pod no longer belongs to any NEG
	negReadyReason = "LoadBalancerNegReady"
	// negReadyTimedOutReason is the pod condition reason when timeout is reached but pod is still not healthy in NEG
	negReadyTimedOutReason = "LoadBalancerNegTimeout"
	// negReadyUnhealthCheckedReason is the pod condition reason when pod is in a NEG without associated health checking
	negReadyUnhealthCheckedReason = "LoadBalancerNegWithoutHealthCheck"
	// negNotReadyReason is the pod condition reason when pod is not healthy in NEG
	negNotReadyReason = "LoadBalancerNegNotReady"
	// unreadyTimeout is the timeout for health status feedback for pod readiness. If load balancer health
	// check is still not showing as Healthy for long than the time out since the pod is created. Skip waiting and mark
	// the pod as load balancer ready.
	// This is a fail-safe in case that should be longer than any reasonable amount of time for the healthy infrastructure catch up.
	unreadyTimeout = 10 * time.Minute
)

// readinessReflector implements the Reflector interface
type readinessReflector struct {
	// podUpdateLock ensures that at any time there is only one
	podUpdateLock sync.Mutex
	client        kubernetes.Interface
	clock         clock.Clock

	// pollerLock ensures there is only poll
	pollerLock sync.Mutex
	poller     *poller

	podLister cache.Indexer
	lookup    NegLookup

	eventBroadcaster record.EventBroadcaster
	eventRecorder    record.EventRecorder

	queue workqueue.RateLimitingInterface

	logger klog.Logger
}

func NewReadinessReflector(kubeClient kubernetes.Interface, podLister cache.Indexer, negCloud negtypes.NetworkEndpointGroupCloud, lookup NegLookup, logger klog.Logger) Reflector {
	broadcaster := record.NewBroadcaster()
	broadcaster.StartLogging(klog.Infof)
	broadcaster.StartRecordingToSink(&unversionedcore.EventSinkImpl{
		Interface: kubeClient.CoreV1().Events(""),
	})
	recorder := broadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: "neg-readiness-reflector"})
	logger = logger.WithName("ReadinessReflector")
	reflector := &readinessReflector{
		client:           kubeClient,
		podLister:        podLister,
		clock:            clock.RealClock{},
		lookup:           lookup,
		eventBroadcaster: broadcaster,
		eventRecorder:    recorder,
		queue:            workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		logger:           logger,
	}
	poller := NewPoller(podLister, lookup, reflector, negCloud, logger)
	reflector.poller = poller
	return reflector
}

func (r *readinessReflector) Run(stopCh <-chan struct{}) {
	defer r.queue.ShutDown()
	r.logger.V(2).Info("Starting NEG readiness reflector")
	defer r.logger.V(2).Info("Shutting down NEG readiness reflector")

	go wait.Until(r.worker, time.Second, stopCh)
	<-stopCh
}

func (r *readinessReflector) worker() {
	for r.processNextWorkItem() {
	}
}

func (r *readinessReflector) processNextWorkItem() bool {
	key, quit := r.queue.Get()
	if quit {
		return false
	}
	defer r.queue.Done(key)

	err := r.syncPod(key.(string), nil, nil)
	r.handleErr(err, key)
	return true
}

// handleErr handles errors from syncPod
func (r *readinessReflector) handleErr(err error, key interface{}) {
	if err == nil {
		r.queue.Forget(key)
		return
	}

	if r.queue.NumRequeues(key) < maxRetries {
		r.logger.V(2).Info("Error syncing pod. Retrying.", "pod", key, "err", err)
		r.queue.AddRateLimited(key)
		return
	}

	r.logger.Info("Dropping pod out of the queue", "pod", key, "err", err)
	r.queue.Forget(key)
}

// syncPod process pod and patch the NEG readiness condition if needed
// if neg and backendService is specified, it means pod is Healthy in the NEG attached to backendService.
func (r *readinessReflector) syncPod(podKey string, neg, backendService *meta.Key) (err error) {
	// podUpdateLock to ensure there is no race in pod status update
	r.podUpdateLock.Lock()
	defer r.podUpdateLock.Unlock()

	namespace, name, err := cache.SplitMetaNamespaceKey(podKey)
	if err != nil {
		return err
	}

	pod, exists, err := getPodFromStore(r.podLister, namespace, name)
	if err != nil {
		return err
	}
	if !exists {
		r.logger.V(3).Info("Pod no longer exists. Skipping", "pod", podKey)
		return nil
	}

	// This is to prevent if the pod got updated after being added to the queue
	if !needToProcess(pod) {
		return nil
	}

	r.logger.V(3).Info("Syncing pod", "pod", podKey, "neg", neg, "backendService", backendService)
	expectedCondition := r.getExpectedNegCondition(pod, neg, backendService)
	return r.ensurePodNegCondition(pod, expectedCondition)
}

// getExpectedCondition returns the expected NEG readiness condition for the given pod
func (r *readinessReflector) getExpectedNegCondition(pod *v1.Pod, neg, backendService *meta.Key) v1.PodCondition {
	expectedCondition := v1.PodCondition{Type: shared.NegReadinessGate}
	if pod == nil {
		expectedCondition.Message = fmt.Sprintf("Unknown status for unknown pod.")
		return expectedCondition
	}

	if neg != nil {
		if backendService != nil {
			expectedCondition.Status = v1.ConditionTrue
			expectedCondition.Reason = negReadyReason
			expectedCondition.Message = fmt.Sprintf("Pod has become Healthy in NEG %q attached to BackendService %q. Marking condition %q to True.", neg.String(), backendService.String(), shared.NegReadinessGate)
		} else {
			expectedCondition.Status = v1.ConditionTrue
			expectedCondition.Reason = negReadyUnhealthCheckedReason
			expectedCondition.Message = fmt.Sprintf("Pod is in NEG %q. NEG is not attached to any BackendService with health checking. Marking condition %q to True.", neg.String(), shared.NegReadinessGate)
		}
		return expectedCondition
	}

	negs := r.lookup.ReadinessGateEnabledNegs(pod.Namespace, pod.Labels)
	// mark pod as ready if it belongs to no NEGs
	if len(negs) == 0 {
		expectedCondition.Status = v1.ConditionTrue
		expectedCondition.Reason = negReadyReason
		expectedCondition.Message = fmt.Sprintf("Pod does not belong to any NEG. Marking condition %q to True.", shared.NegReadinessGate)
		return expectedCondition
	}

	// check if the pod has been waiting for the endpoint to show up as Healthy in NEG for too long
	if r.clock.Now().After(pod.CreationTimestamp.Add(unreadyTimeout)) {
		expectedCondition.Status = v1.ConditionTrue
		expectedCondition.Reason = negReadyTimedOutReason
		expectedCondition.Message = fmt.Sprintf("Timeout waiting for pod to become healthy in at least one of the NEG(s): %v. Marking condition %q to True.", negs, shared.NegReadinessGate)
		return expectedCondition
	}

	// do not patch condition status in this case to prevent race condition:
	// 1. poller marks a pod ready
	// 2. syncPod gets call and does not retrieve the updated pod spec with true neg readiness condition
	// 3. syncPod patches the neg readiness condition to be false
	expectedCondition.Reason = negNotReadyReason
	expectedCondition.Message = fmt.Sprintf("Waiting for pod to become healthy in at least one of the NEG(s): %v", negs)
	return expectedCondition
}

// SyncPod filter the pods that needed to be processed and put it into queue
func (r *readinessReflector) SyncPod(pod *v1.Pod) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(pod)
	if err != nil {
		r.logger.Error(err, "Failed to generate pod key")
		return
	}

	if !needToProcess(pod) {
		r.logger.V(3).Info("Skip processing pod", "pod", key)
	}
	r.queue.Add(key)
}

// CommitPods registers the current network endpoints in a NEG and starts polling them if needed
func (r *readinessReflector) CommitPods(syncerKey negtypes.NegSyncerKey, negName string, zone string, endpointMap negtypes.EndpointPodMap) {
	key := negMeta{
		SyncerKey: syncerKey,
		Name:      negName,
		Zone:      zone,
	}
	r.poller.RegisterNegEndpoints(key, endpointMap)
	r.poll()
}

// poll spins off go routines to poll NEGs
func (r *readinessReflector) poll() {
	r.pollerLock.Lock()
	defer r.pollerLock.Unlock()
	for _, key := range r.poller.ScanForWork() {
		go r.pollNeg(key)
	}
}

// pollNeg polls a NEG
func (r *readinessReflector) pollNeg(key negMeta) {
	r.logger.V(3).Info("Polling NEG", "neg", key.String())
	retry, err := r.poller.Poll(key)
	if err != nil {
		r.logger.Error(err, "Failed to poll neg", "neg", key)
	}
	if retry {
		r.poll()
	}
}

// ensurePodNegCondition ensures the pod neg condition is as expected
// TODO(freehan): also populate lastTransitionTime in the condition
func (r *readinessReflector) ensurePodNegCondition(pod *v1.Pod, expectedCondition v1.PodCondition) error {
	if pod == nil {
		return nil
	}
	// check if it is necessary to patch
	condition, ok := NegReadinessConditionStatus(pod)
	if ok && reflect.DeepEqual(expectedCondition, condition) {
		r.logger.V(3).Info("NEG condition for pod is expected, skip patching", "pod", klog.KRef(pod.Namespace, pod.Name))
		return nil
	}

	// calculate patch bytes, send patch and record event
	oldStatus := pod.Status.DeepCopy()
	SetNegReadinessConditionStatus(pod, expectedCondition)
	patchBytes, err := preparePatchBytesforPodStatus(*oldStatus, pod.Status)
	if err != nil {
		return fmt.Errorf("failed to prepare patch bytes for pod %v: %v", pod, err)
	}
	r.eventRecorder.Eventf(pod, v1.EventTypeNormal, expectedCondition.Reason, expectedCondition.Message)
	_, _, err = patchPodStatus(r.client, pod.Namespace, pod.Name, patchBytes)
	return err
}
