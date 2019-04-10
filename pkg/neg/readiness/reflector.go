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
	"sync"
	"time"

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes/scheme"
	unversionedcore "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/ingress-gce/pkg/context"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/klog"
)

// readinessReflector implements the PodReadinessReflector interface
type readinessReflector struct {
	// lock to protect internal states
	lock sync.Mutex
	// serviceNegMap maps service to a set of Negs belong to the service.
	// Namespace -> Service -> Negs
	serviceNegMap serviceInfoMap

	// tracker tracks pods and negs
	tracker *podNegTracker

	// patcher is responsible for patching pods and generating relevant events
	patcher podStatusPatcher

	// poller polls the health status and trigger readinessReflector to Sync Pod
	poller healthStatusPoller

	podLister cache.Indexer
	svcLister cache.Indexer

	eventBroadcaster record.EventBroadcaster
	eventRecorder    record.EventRecorder

	queue workqueue.RateLimitingInterface
}

func NewReadienssReflector(ctx *context.ControllerContext) Reflector {
	tracker := newNegTracker()
	patcher := newPatcher(ctx, tracker)
	poller := newNegHealthStatusPoller(tracker, patcher, ctx.Cloud)
	broadcaster := record.NewBroadcaster()
	broadcaster.StartLogging(klog.Infof)
	broadcaster.StartRecordingToSink(&unversionedcore.EventSinkImpl{
		Interface: ctx.KubeClient.CoreV1().Events(""),
	})
	recorder := broadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: "neg-readiness-reflector"})
	return &readinessReflector{
		serviceNegMap:    serviceInfoMap{},
		tracker:          tracker,
		patcher:          patcher,
		poller:           poller,
		podLister:        ctx.PodInformer.GetIndexer(),
		svcLister:        ctx.ServiceInformer.GetIndexer(),
		eventBroadcaster: broadcaster,
		eventRecorder:    recorder,
		queue:            workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
	}
}

func (r *readinessReflector) Run(stopCh <-chan struct{}) {
	defer r.queue.ShutDown()
	klog.V(2).Infof("Starting NEG readiness reflector")
	defer klog.V(2).Infof("Shutting down NEG readiness reflector")

	go wait.Until(r.worker, time.Second, stopCh)
	go r.patcher.Run(stopCh)
	go r.poller.Run(stopCh)
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

	err := r.syncPod(key.(string))
	r.handleErr(err, key)
	return true
}

func (r *readinessReflector) handleErr(err error, key interface{}) {
	if err == nil {
		r.queue.Forget(key)
		return
	}

	if r.queue.NumRequeues(key) < maxRetries {
		klog.V(2).Infof("Error syncing pod %q, retrying. Error: %v", key, err)
		r.queue.AddRateLimited(key)
		return
	}

	klog.Warningf("Dropping pod %q out of the queue: %v", key, err)
	r.queue.Forget(key)
}

// SyncNegService synchronize internal states based on the input service and associated Negs
func (r *readinessReflector) SyncNegService(namespace, name string, negNames sets.String) {
	r.lock.Lock()
	defer r.lock.Unlock()
	existNegNames := sets.NewString()
	svcInfo, ok := r.serviceNegMap.GetSvcInfo(namespace, name)
	if ok {
		existNegNames = sets.NewString(svcInfo.GetNegNameStrings()...)
	}

	if existNegNames.Equal(negNames) {
		klog.V(4).Infof("No difference NEGs for service %s/%s. Skip", namespace, name)
		return
	}

	klog.V(2).Infof("Sync NEG service %s/%s in Readiness Reflector: %v", namespace, name, negNames.List())
	// remove Negs no longer wanted
	removes := existNegNames.Difference(negNames)
	for _, neg := range removes.List() {
		r.removeNeg(negName(neg))
	}

	// adds the new negs in negPodMap
	adds := negNames.Difference(existNegNames)
	for _, neg := range adds.List() {
		r.tracker.AddNeg(negName(neg))

	}

	// only stores the NEG service when there is Negs associated
	if negNames.Len() > 0 {
		r.serviceNegMap.SetSvcInfo(namespace, name, newSvcInfo(negNames.List()))
	} else {
		r.serviceNegMap.RemoveSvcInfo(namespace, name)
	}
}

// syncPod registers the pod and ask readiness reflector to feedback NEG readiness.
func (r *readinessReflector) syncPod(key string) (err error) {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}

	r.lock.Lock()
	defer r.lock.Unlock()
	pod, exists, err := getPodFromStore(r.podLister, namespace, name)
	if err != nil {
		return err
	}

	objkey := newObjKey(namespace, name)
	// if pod no longer exists, remove it from internal state
	if !exists {
		r.removePod(objkey)
		return nil
	}

	// if pod is deleted, remove it from internal state
	if pod.DeletionTimestamp != nil {
		r.removePod(objkey)
		return nil
	}
	// if the NEG readiness gate does not exists or the NEG readiness condition is true, remove it from internal state
	negReady, negReadinessGateExists := evalNegReadinessGate(pod)
	if !negReadinessGateExists || negReady {
		klog.V(2).Infof("Pod %v became ready, unregistering from readiness reflector", objkey.String())
		r.removePod(objkey)
		return nil
	}
	// always try to patch pod after sync
	defer r.patcher.Patch(namespace, name)
	if r.tracker.PodIsTracked(objkey) {
		klog.V(4).Infof("Pod %v is already registered. Skip processing", objkey.String())
		return nil
	}
	klog.V(2).Infof("Registering pod %v with readiness reflector", objkey.String())
	// map pod to the corresponding negs
	for _, svcName := range r.serviceNegMap.GetSvcsInNamespace(namespace) {
		svc, exists, err := getServiceFromStore(r.svcLister, namespace, svcName)
		if err != nil {
			klog.Errorf("Failed to retrieve %v/%v service from store: %v", namespace, svcName, err)
			continue
		}
		if !exists {
			klog.Warningf("Service %v/%v no longer exists.", namespace, svcName)
			continue
		}

		if podBelongToSvc(pod, svc) {
			svcInfo, ok := r.serviceNegMap.GetSvcInfo(namespace, svcName)
			if !ok {
				klog.Errorf("Service %s/%s not found in service neg map.", namespace, svcName)
				continue
			}
			if err := r.tracker.AddPodToNegs(objkey, svcInfo.Negs); err != nil {
				return err
			}
		}
	}
	return nil
}

// CommitPod signals the readiness reflector that the list of endpoints associated with pods have been added to a NEG
func (r *readinessReflector) CommitPod(neg string, zone string, endpoints []*negtypes.EndpointMeta) {
	klog.V(2).Info("============================CommitPod(%v, %v, %v)", neg, zone, endpoints)
	if endpoints == nil {
		klog.Warningf("ReadinessReflector for neg %q in zone %q got nil endpoints.", neg, zone)
		return
	}
	negKey := negName(neg)
	for _, endpoint := range endpoints {
		if endpoint == nil {
			continue
		}
		podKey := newObjKey(endpoint.Pod.Namespace, endpoint.Pod.Name)
		if !r.tracker.PodIsInNeg(podKey, negKey) {
			continue
		}

		func() {
			r.lock.Lock()
			defer r.lock.Unlock()
			if err := r.tracker.UpdatePodInfo(podKey, negKey, zone, endpoint.NetworkEndpoint.IP, endpoint.NetworkEndpoint.Port); err != nil {
				klog.Warningf("failed to update info of endpoint %q: %v", podKey.String(), err)
			}
		}()
	}
	r.poller.Poll(negKey)
}

func (r *readinessReflector) SyncPod(obj interface{}) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		klog.Errorf("Failed to generate pod key: %v", err)
		return
	}

	pod, ok := obj.(*v1.Pod)
	if !ok {
		klog.Errorf("obj type is %T, expect *v1.Pod", obj)
		return
	}

	negConditionReady, readinessGateExists := evalNegReadinessGate(obj.(*v1.Pod))

	if !readinessGateExists {
		klog.V(5).Infof("Pod %v does not have neg readiness gate. Skipping", key)
		return
	}

	if negConditionReady {
		if !r.tracker.PodIsTracked(newObjKey(pod.Namespace, pod.Name)) {
			klog.V(5).Infof("Pod %v neg readiness condition is already True. Skipping", key)
			return
		}
	}

	r.queue.Add(key)
}

// removeNeg removes all reference of the neg in the internal state.
// This function assume the caller of has the lock.
func (r *readinessReflector) removeNeg(neg negName) {
	pods, ok := r.tracker.GetPodsInNeg(neg)
	if !ok {
		klog.Warningf("NEG %q not found in tracker.", neg.String())
		return
	}
	r.tracker.RemoveNeg(neg)
	for _, pod := range pods {
		// try patching the pods in case no other NEG is associated with pod
		r.patcher.Patch(pod.Namespace, pod.Name)
	}
}

// removePod removes all reference of the neg in the internal state
// This function assume the caller of has the lock.
func (r *readinessReflector) removePod(key objKey) {
	klog.V(2).Info("============================removePod key", key)
	if r.tracker.PodIsTracked(key) {
		if err := r.tracker.RemovePod(key); err != nil {
			klog.Errorf("Error while removing pod %q from tracker: %v", key.String(), err)
		}
	}
}

func podBelongToSvc(pod *v1.Pod, svc *v1.Service) bool {
	if svc.Spec.Selector == nil {
		// services with nil selectors match nothing, not everything.
		return false
	}
	selector := labels.Set(svc.Spec.Selector).AsSelectorPreValidated()
	if selector.Matches(labels.Set(pod.Labels)) {
		return true
	}
	return false
}
