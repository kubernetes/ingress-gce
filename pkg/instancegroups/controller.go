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

package instancegroups

import (
	"time"

	apiv1 "k8s.io/api/core/v1"
	listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog/v2"
)

// Controller synchronizes the state of the nodes to the unmanaged instance
// groups.
type Controller struct {
	// lister is a cache of the k8s Node resources.
	lister cache.Indexer
	// queue is the TaskQueue used to manage the node worker updates.
	queue utils.TaskQueue
	// igManager is an interface to manage instance groups.
	igManager Manager
	// hasSynced returns true if relevant caches have done their initial
	// synchronization.
	hasSynced func() bool

	stopCh chan struct{}
}

type ControllerConfig struct {
	NodeInformer cache.SharedIndexInformer
	IGManager    Manager
	HasSynced    func() bool
	StopCh       chan struct{}
}

// NewController returns a new node update controller.
func NewController(config *ControllerConfig) *Controller {
	c := &Controller{
		lister:    config.NodeInformer.GetIndexer(),
		igManager: config.IGManager,
		hasSynced: config.HasSynced,
		stopCh:    config.StopCh,
	}
	c.queue = utils.NewPeriodicTaskQueue("", "nodes", c.sync)

	config.NodeInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			c.queue.Enqueue(obj)
		},
		DeleteFunc: func(obj interface{}) {
			c.queue.Enqueue(obj)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			if nodeStatusChanged(oldObj.(*apiv1.Node), newObj.(*apiv1.Node)) {
				c.queue.Enqueue(newObj)
			}
		},
	})
	return c
}

// Run the queue to process updates for the controller. This must be run in a
// separate goroutine (method will block until queue shutdown).
func (c *Controller) Run() {
	start := time.Now()
	for !c.hasSynced() {
		klog.V(2).Infof("Waiting for hasSynced (%s elapsed)", time.Now().Sub(start))
		time.Sleep(1 * time.Second)
	}
	klog.V(2).Infof("Caches synced (took %s)", time.Now().Sub(start))
	go c.queue.Run()
	<-c.stopCh
	c.Shutdown()
}

func nodeStatusChanged(old, cur *apiv1.Node) bool {
	if old.Spec.Unschedulable != cur.Spec.Unschedulable {
		return true
	}
	if utils.NodeIsReady(old) != utils.NodeIsReady(cur) {
		return true
	}
	return false
}

// Shutdown shuts down the goroutine that processes node updates.
func (c *Controller) Shutdown() {
	c.queue.Shutdown()
}

func (c *Controller) sync(key string) error {
	start := time.Now()
	klog.V(4).Infof("Instance groups controller: Start processing %s", key)
	defer klog.V(4).Infof("Instance groups controller: Processing key %s took %v", key, time.Since(start))

	nodeNames, err := utils.GetReadyNodeNames(listers.NewNodeLister(c.lister))
	if err != nil {
		return err
	}
	return c.igManager.Sync(nodeNames)
}
