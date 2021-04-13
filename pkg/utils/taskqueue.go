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

package utils

import (
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"
)

var (
	KeyFunc = cache.DeletionHandlingMetaNamespaceKeyFunc
)

// TaskQueue is a rate limited operation queue.
type TaskQueue interface {
	Run()
	Enqueue(objs ...interface{})
	Shutdown()
	Len() int
	NumRequeues(obj interface{}) int
}

// PeriodicTaskQueueWithMultipleWorkers invokes the given sync function for every work item
// inserted, while running n parallel worker routines. If the sync() function results in an error, the item is put on
// the work queue after a rate-limit.
type PeriodicTaskQueueWithMultipleWorkers struct {
	// resource is used for logging to distinguish the queue being used.
	resource string
	// keyFunc translates an object to a string-based key.
	keyFunc func(obj interface{}) (string, error)
	// queue is the work queue the workers poll.
	queue workqueue.RateLimitingInterface
	// sync is called for each item in the queue.
	sync func(string) error
	// The respective workerDone channel is closed when the worker exits. There is one channel per worker.
	workerDone []chan struct{}
	// numWorkers indicates the number of worker routines processing the queue.
	numWorkers int
}

// Len returns the length of the queue.
func (t *PeriodicTaskQueueWithMultipleWorkers) Len() int {
	return t.queue.Len()
}

// NumRequeues returns the number of times the given item was requeued.
func (t *PeriodicTaskQueueWithMultipleWorkers) NumRequeues(obj interface{}) int {
	key, _ := t.keyFunc(obj)
	return t.queue.NumRequeues(key)
}

// runInternal invokes the worker routine to pick up and process an item from the queue. This blocks until ShutDown is called.
func (t *PeriodicTaskQueueWithMultipleWorkers) runInternal(workerId int) {
	for {
		key, quit := t.queue.Get()
		if quit {
			close(t.workerDone[workerId])
			return
		}
		klog.V(4).Infof("Worker-%d: Syncing %v (%v)", workerId, key, t.resource)
		if err := t.sync(key.(string)); err != nil {
			klog.Errorf("Worker-%d: Requeuing %q due to error: %v (%v)", workerId, key, err, t.resource)
			t.queue.AddRateLimited(key)
		} else {
			klog.V(4).Infof("Worker-%d: Finished syncing %v", workerId, key)
			t.queue.Forget(key)
		}
		t.queue.Done(key)
	}
}

// Run spawns off n parallel worker routines and returns immediately.
func (t *PeriodicTaskQueueWithMultipleWorkers) Run() {
	for worker := 0; worker < t.numWorkers; worker++ {
		klog.Infof("Spawning off Worker-%d for taskQueue %s", worker, t.resource)
		go t.runInternal(worker)
	}
}

// Enqueue adds one or more keys to the work queue.
func (t *PeriodicTaskQueueWithMultipleWorkers) Enqueue(objs ...interface{}) {
	for _, obj := range objs {
		key, err := t.keyFunc(obj)
		if err != nil {
			klog.Errorf("Couldn't get key for object %+v (type %T): %v", obj, obj, err)
			return
		}
		klog.V(4).Infof("Enqueue key=%q (%v)", key, t.resource)
		t.queue.Add(key)
	}
}

// Shutdown shuts down the work queue and waits for all the workers to ACK
func (t *PeriodicTaskQueueWithMultipleWorkers) Shutdown() {
	klog.V(2).Infof("Shutting down task queue for resource %s", t.resource)
	t.queue.ShutDown()
	// wait for all workers to shutdown.
	for _, workerDone := range t.workerDone {
		<-workerDone
	}
}

// NewPeriodicTaskQueueWithMultipleWorkers creates a new task queue with the default rate limiter and the given number of worker goroutines.
func NewPeriodicTaskQueueWithMultipleWorkers(name, resource string, numWorkers int, syncFn func(string) error) *PeriodicTaskQueueWithMultipleWorkers {
	if numWorkers <= 0 {
		klog.Errorf("Invalid worker count %d", numWorkers)
		return nil
	}
	rl := workqueue.DefaultControllerRateLimiter()
	var queue workqueue.RateLimitingInterface
	if name == "" {
		queue = workqueue.NewRateLimitingQueue(rl)
	} else {
		queue = workqueue.NewNamedRateLimitingQueue(rl, name)
	}
	taskQueue := &PeriodicTaskQueueWithMultipleWorkers{
		resource:   resource,
		keyFunc:    KeyFunc,
		queue:      queue,
		sync:       syncFn,
		numWorkers: numWorkers,
	}
	for worker := 0; worker < numWorkers; worker++ {
		taskQueue.workerDone = append(taskQueue.workerDone, make(chan struct{}))
	}
	return taskQueue
}

// PeriodicTaskQueue invokes the given sync function for every work item
// inserted. If the sync() function results in an error, the item is put on
// the work queue after a rate-limit.
type PeriodicTaskQueue struct {
	// resource is used for logging to distinguish the queue being used.
	resource string
	// keyFunc translates an object to a string-based key.
	keyFunc func(obj interface{}) (string, error)
	// queue is the work queue the worker polls.
	queue workqueue.RateLimitingInterface
	// sync is called for each item in the queue.
	sync func(string) error
	// workerDone is closed when the worker exits.
	workerDone chan struct{}
}

// Len returns the length of the queue.
func (t *PeriodicTaskQueue) Len() int {
	return t.queue.Len()
}

// NumRequeues returns the number of times the given item was requeued.
func (t *PeriodicTaskQueue) NumRequeues(obj interface{}) int {
	return t.queue.NumRequeues(obj)
}

// Run runs the task queue. This will block until the Shutdown() has been called.
func (t *PeriodicTaskQueue) Run() {
	for {
		key, quit := t.queue.Get()
		if quit {
			close(t.workerDone)
			return
		}
		klog.V(4).Infof("Syncing %v (%v)", key, t.resource)
		if err := t.sync(key.(string)); err != nil {
			klog.Errorf("Requeuing %q due to error: %v (%v)", key, err, t.resource)
			t.queue.AddRateLimited(key)
		} else {
			klog.V(4).Infof("Finished syncing %v", key)
			t.queue.Forget(key)
		}
		t.queue.Done(key)
	}
}

// Enqueue adds one or more keys to the work queue.
func (t *PeriodicTaskQueue) Enqueue(objs ...interface{}) {
	for _, obj := range objs {
		key, err := t.keyFunc(obj)
		if err != nil {
			klog.Errorf("Couldn't get key for object %+v (type %T): %v", obj, obj, err)
			return
		}
		klog.V(4).Infof("Enqueue key=%q (%v)", key, t.resource)
		t.queue.Add(key)
	}
}

// Shutdown shuts down the work queue and waits for the worker to ACK
func (t *PeriodicTaskQueue) Shutdown() {
	klog.V(2).Infof("Shutdown")
	t.queue.ShutDown()
	<-t.workerDone
}

// NewPeriodicTaskQueue creates a new task queue with the default rate limiter.
func NewPeriodicTaskQueue(name, resource string, syncFn func(string) error) *PeriodicTaskQueue {
	rl := workqueue.DefaultControllerRateLimiter()
	return NewPeriodicTaskQueueWithLimiter(name, resource, syncFn, rl)
}

// NewPeriodicTaskQueueWithLimiter creates a new task queue with the given sync function
// and rate limiter. The sync function is called for every element inserted into the queue.
func NewPeriodicTaskQueueWithLimiter(name, resource string, syncFn func(string) error, rl workqueue.RateLimiter) *PeriodicTaskQueue {
	var queue workqueue.RateLimitingInterface
	if name == "" {
		queue = workqueue.NewRateLimitingQueue(rl)
	} else {
		queue = workqueue.NewNamedRateLimitingQueue(rl, name)
	}

	return &PeriodicTaskQueue{
		resource:   resource,
		keyFunc:    KeyFunc,
		queue:      queue,
		sync:       syncFn,
		workerDone: make(chan struct{}),
	}
}
