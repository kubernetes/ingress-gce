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
	"time"

	"k8s.io/api/core/v1"
	apiv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	unversionedcore "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/ingress-gce/pkg/context"
	"k8s.io/ingress-gce/pkg/neg/types/shared"
	"k8s.io/klog"
)

// patcher implements the podStatusPatcher interface.
// It handles patching NEG Readiness condition
type patcher struct {
	client kubernetes.Interface
	queue  workqueue.RateLimitingInterface

	podLister cache.Indexer

	tracker *podNegTracker

	eventBroadcaster record.EventBroadcaster
	eventRecorder    record.EventRecorder
}

func newPatcher(ctx *context.ControllerContext, tracker *podNegTracker) podStatusPatcher {
	broadcaster := record.NewBroadcaster()
	broadcaster.StartLogging(klog.Infof)
	broadcaster.StartRecordingToSink(&unversionedcore.EventSinkImpl{
		Interface: ctx.KubeClient.CoreV1().Events(""),
	})
	recorder := broadcaster.NewRecorder(scheme.Scheme, apiv1.EventSource{Component: "pod-status-patcher"})

	return &patcher{
		client:           ctx.KubeClient,
		queue:            workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		podLister:        ctx.PodInformer.GetIndexer(),
		tracker:          tracker,
		eventBroadcaster: broadcaster,
		eventRecorder:    recorder,
	}
}

func (p *patcher) Run(stopCh <-chan struct{}) {
	//wait.PollUntil(5*time.Second, func() (bool, error) {
	//	klog.V(2).Infof("Waiting for initial sync")
	//	return .synced(), nil
	//}, stopCh)
	defer p.queue.ShutDown()
	klog.V(2).Infof("Starting pod patcher")
	defer klog.V(2).Infof("Shutting down pod patcher")
	go wait.Until(p.worker, time.Second, stopCh)
	<-stopCh
}

func (p *patcher) Patch(namespace, name string) {
	klog.V(2).Infof("============================Patch %s/%s", namespace, name)
	p.queue.Add(keyFunc(namespace, name))
}

func (p *patcher) worker() {
	for p.processNextWorkItem() {
	}
}

func (p *patcher) processNextWorkItem() bool {
	key, quit := p.queue.Get()
	if quit {
		return false
	}
	defer p.queue.Done(key)

	err := p.syncPodNegReadiness(key.(string))
	p.handleErr(err, key)
	return true
}

// syncPodNegReadiness process a pod request for the input pod
func (p *patcher) syncPodNegReadiness(key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}
	objKey := newObjKey(namespace, name)
	negMap, ok := p.tracker.GetPodHealthStatus(objKey)
	// podReady represents if all NEGs the pod belongs to reports the corresponding endpoint is healthy
	podReady := true
	unhealthyNegs := []string{}

	if !ok {
		klog.Warningf("Pod %q could not be found in tracker", objKey.String())
	} else {
		for neg, healthy := range negMap {
			if healthy == false {
				unhealthyNegs = append(unhealthyNegs, neg.String())
				podReady = false
			}
		}
	}

	pod, exists, err := getPodFromStore(p.podLister, namespace, name)
	if err != nil {
		return err
	}
	if !exists {
		klog.Warningf("Pod %q no longer exists. Skipping patching. ", objKey.String())
		return nil
	}

	// calculate expected condition
	expectedCondition := v1.PodCondition{Type: shared.NegReadinessGate}
	message := ""
	reason := ""
	if podReady {
		expectedCondition.Status = v1.ConditionTrue
		reason = "LoadBalancerNegReady"
		message = fmt.Sprintf("Pod is healthy in all associated Google Cloud Loadbalancer managed by ingress. Marking condition %q to True.", shared.NegReadinessGate)
	} else {
		expectedCondition.Status = v1.ConditionFalse
		reason = "LoadBalancerNegNotReady"
		message = fmt.Sprintf("Waiting for pod to become healthy in NEG(s): %v", unhealthyNegs)

	}
	expectedCondition.Reason = reason
	expectedCondition.Message = message

	// check if it is necessary to patch
	condition, ok := GetNegReadinessConditionStatus(pod)
	if ok && reflect.DeepEqual(expectedCondition, condition) {
		klog.V(4).Infof("NEG condition for pod %q is expected, skip patching", objKey.String())
		return nil
	}

	// calculate patch bytes, send patch and record event
	oldStatus := pod.Status.DeepCopy()
	SetNegReadinessConditionStatus(pod, expectedCondition)
	patchBytes, err := preparePatchBytesforPodStatus(pod.Namespace, pod.Name, *oldStatus, pod.Status)
	if err != nil {
		return err
	}
	p.eventRecorder.Eventf(pod, v1.EventTypeNormal, reason, message)
	_, _, err = PatchPodStatus(p.client, namespace, name, patchBytes)
	return err
}

func (p *patcher) handleErr(err error, key interface{}) {
	if err == nil {
		p.queue.Forget(key)
		return
	}

	msg := fmt.Sprintf("failed to patch pod %q: %v", key, err)
	pod, exists, err := p.podLister.GetByKey(key.(string))
	if err != nil {
		klog.Warningf("Failed to retrieve pod %q from store: %v", key, err)
	} else if !exists {
		klog.Warningf("Pod %q no longer exists", key)
	} else {
		p.eventRecorder.Eventf(pod.(*apiv1.Pod), apiv1.EventTypeWarning, "PatchPodStatusFailed", msg)
	}

	if p.queue.NumRequeues(key) < maxRetries {
		klog.V(2).Infof("Error patching pod %q, retrying. Error: %v", key, err)
		p.queue.AddRateLimited(key)
		return
	}

	klog.Warningf("Dropping pod %q out of the queue: %v", key, err)
	p.queue.Forget(key)
}
