/*
Copyright 2015 The Kubernetes Authors.

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

package controller

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/golang/glog"

	compute "google.golang.org/api/compute/v1"

	api_v1 "k8s.io/api/core/v1"
	extensions "k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/backends"
)

// isGCEIngress returns true if the given Ingress either doesn't specify the
// ingress.class annotation, or it's set to "gce".
func isGCEIngress(ing *extensions.Ingress) bool {
	class := annotations.IngAnnotations(ing.ObjectMeta.Annotations).IngressClass()
	return class == "" || class == annotations.GceIngressClass
}

// isGCEMultiClusterIngress returns true if the given Ingress has
// ingress.class annotation set to "gce-multi-cluster".
func isGCEMultiClusterIngress(ing *extensions.Ingress) bool {
	class := annotations.IngAnnotations(ing.ObjectMeta.Annotations).IngressClass()
	return class == annotations.GceMultiIngressClass
}

// errorNodePortNotFound is an implementation of error.
type errorNodePortNotFound struct {
	backend extensions.IngressBackend
	origErr error
}

func (e errorNodePortNotFound) Error() string {
	return fmt.Sprintf("Could not find nodeport for backend %+v: %v",
		e.backend, e.origErr)
}

type errorSvcAppProtosParsing struct {
	svc     *api_v1.Service
	origErr error
}

func (e errorSvcAppProtosParsing) Error() string {
	return fmt.Sprintf("could not parse %v annotation on Service %v/%v, err: %v", annotations.ServiceApplicationProtocolKey, e.svc.Namespace, e.svc.Name, e.origErr)
}

// taskQueue manages a work queue through an independent worker that
// invokes the given sync function for every work item inserted.
type taskQueue struct {
	// queue is the work queue the worker polls
	queue workqueue.RateLimitingInterface
	// sync is called for each item in the queue
	sync func(string) error
	// workerDone is closed when the worker exits
	workerDone chan struct{}
}

func (t *taskQueue) run(period time.Duration, stopCh <-chan struct{}) {
	wait.Until(t.worker, period, stopCh)
}

// enqueue enqueues ns/name of the given api object in the task queue.
func (t *taskQueue) enqueue(obj interface{}) {
	key, err := keyFunc(obj)
	if err != nil {
		glog.Infof("Couldn't get key for object %+v: %v", obj, err)
		return
	}
	t.queue.Add(key)
}

// worker processes work in the queue through sync.
func (t *taskQueue) worker() {
	for {
		key, quit := t.queue.Get()
		if quit {
			close(t.workerDone)
			return
		}
		glog.V(3).Infof("Syncing %v", key)
		if err := t.sync(key.(string)); err != nil {
			glog.Errorf("Requeuing %v, err %v", key, err)
			t.queue.AddRateLimited(key)
		} else {
			t.queue.Forget(key)
		}
		t.queue.Done(key)
	}
}

// shutdown shuts down the work queue and waits for the worker to ACK
func (t *taskQueue) shutdown() {
	t.queue.ShutDown()
	<-t.workerDone
}

// NewTaskQueue creates a new task queue with the given sync function.
// The sync function is called for every element inserted into the queue.
func NewTaskQueue(syncFn func(string) error) *taskQueue {
	return &taskQueue{
		queue:      workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		sync:       syncFn,
		workerDone: make(chan struct{}),
	}
}

// compareLinks returns true if the 2 self links are equal.
func compareLinks(l1, l2 string) bool {
	// TODO: These can be partial links
	return l1 == l2 && l1 != ""
}

// StoreToIngressLister makes a Store that lists Ingress.
// TODO: Move this to cache/listers post 1.1.
type StoreToIngressLister struct {
	cache.Store
}

// StoreToNodeLister makes a Store that lists Node.
type StoreToNodeLister struct {
	cache.Indexer
}

// StoreToServiceLister makes a Store that lists Service.
type StoreToServiceLister struct {
	cache.Indexer
}

// StoreToPodLister makes a Store that lists Pods.
type StoreToPodLister struct {
	cache.Indexer
}

// StoreToPodLister makes a Store that lists Endpoints.
type StoreToEndpointLister struct {
	cache.Indexer
}

// List lists all Ingress' in the store (both single and multi cluster ingresses).
func (s *StoreToIngressLister) ListAll() (ing extensions.IngressList, err error) {
	for _, m := range s.Store.List() {
		newIng := m.(*extensions.Ingress)
		if isGCEIngress(newIng) || isGCEMultiClusterIngress(newIng) {
			ing.Items = append(ing.Items, *newIng)
		}
	}
	return ing, nil
}

// ListGCEIngresses lists all GCE Ingress' in the store.
func (s *StoreToIngressLister) ListGCEIngresses() (ing extensions.IngressList, err error) {
	for _, m := range s.Store.List() {
		newIng := m.(*extensions.Ingress)
		if isGCEIngress(newIng) {
			ing.Items = append(ing.Items, *newIng)
		}
	}
	return ing, nil
}

// GetServiceIngress gets all the Ingress' that have rules pointing to a service.
// Note that this ignores services without the right nodePorts.
func (s *StoreToIngressLister) GetServiceIngress(svc *api_v1.Service) (ings []extensions.Ingress, err error) {
IngressLoop:
	for _, m := range s.Store.List() {
		ing := *m.(*extensions.Ingress)
		if ing.Namespace != svc.Namespace {
			continue
		}

		// Check service of default backend
		if ing.Spec.Backend != nil && ing.Spec.Backend.ServiceName == svc.Name {
			ings = append(ings, ing)
			continue
		}

		// Check the target service for each path rule
		for _, rule := range ing.Spec.Rules {
			if rule.IngressRuleValue.HTTP == nil {
				continue
			}
			for _, p := range rule.IngressRuleValue.HTTP.Paths {
				if p.Backend.ServiceName == svc.Name {
					ings = append(ings, ing)
					// Skip the rest of the rules to avoid duplicate ingresses in list
					continue IngressLoop
				}
			}
		}
	}
	if len(ings) == 0 {
		err = fmt.Errorf("no ingress for service %v", svc.Name)
	}
	return
}

// PodsByCreationTimestamp sorts a list of Pods by creation timestamp, using their names as a tie breaker.
type PodsByCreationTimestamp []*api_v1.Pod

func (o PodsByCreationTimestamp) Len() int      { return len(o) }
func (o PodsByCreationTimestamp) Swap(i, j int) { o[i], o[j] = o[j], o[i] }

func (o PodsByCreationTimestamp) Less(i, j int) bool {
	if o[i].CreationTimestamp.Equal(&o[j].CreationTimestamp) {
		return o[i].Name < o[j].Name
	}
	return o[i].CreationTimestamp.Before(&o[j].CreationTimestamp)
}

// setInstanceGroupsAnnotation sets the instance-groups annotation with names of the given instance groups.
func setInstanceGroupsAnnotation(existing map[string]string, igs []*compute.InstanceGroup) error {
	type Value struct {
		Name string
		Zone string
	}
	var instanceGroups []Value
	for _, ig := range igs {
		instanceGroups = append(instanceGroups, Value{Name: ig.Name, Zone: ig.Zone})
	}
	jsonValue, err := json.Marshal(instanceGroups)
	if err != nil {
		return err
	}
	existing[annotations.InstanceGroupsAnnotationKey] = string(jsonValue)
	return nil
}

// uniq returns an array of unique service ports from the given array.
func uniq(nodePorts []backends.ServicePort) []backends.ServicePort {
	portMap := map[int64]backends.ServicePort{}
	for _, p := range nodePorts {
		portMap[p.Port] = p
	}
	nodePorts = make([]backends.ServicePort, 0, len(portMap))
	for _, sp := range portMap {
		nodePorts = append(nodePorts, sp)
	}
	return nodePorts
}
