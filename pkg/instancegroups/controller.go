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
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/zonegetter"
	"k8s.io/klog/v2"
)

// Controller synchronizes the state of the nodes to the unmanaged instance
// groups.
type Controller struct {
	// queue is the TaskQueue used to manage the node worker updates.
	queue utils.TaskQueue
	// igManager is an interface to manage instance groups.
	igManager Manager
	// hasSynced returns true if relevant caches have done their initial
	// synchronization.
	hasSynced func() bool

	enableMultiSubnetCluster bool

	stopCh     <-chan struct{}
	zoneGetter *zonegetter.ZoneGetter

	logger klog.Logger
}

type ControllerConfig struct {
	NodeInformer             cache.SharedIndexInformer
	ZoneGetter               *zonegetter.ZoneGetter
	IGManager                Manager
	HasSynced                func() bool
	EnableMultiSubnetCluster bool
	StopCh                   <-chan struct{}
}

var defaultNodeObj = &apiv1.Node{
	ObjectMeta: meta_v1.ObjectMeta{
		Name: "default",
	},
}

// NewController returns a new node update controller.
func NewController(config *ControllerConfig, logger klog.Logger) *Controller {
	logger = logger.WithName("InstanceGroupsController")
	c := &Controller{
		zoneGetter:               config.ZoneGetter,
		igManager:                config.IGManager,
		hasSynced:                config.HasSynced,
		enableMultiSubnetCluster: config.EnableMultiSubnetCluster,
		stopCh:                   config.StopCh,
		logger:                   logger,
	}
	c.queue = utils.NewPeriodicTaskQueue("", "nodes", c.sync, logger)

	config.NodeInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			c.queue.Enqueue(defaultNodeObj)
		},
		DeleteFunc: func(obj interface{}) {
			c.queue.Enqueue(defaultNodeObj)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			if nodeStatusChanged(oldObj.(*apiv1.Node), newObj.(*apiv1.Node)) {
				c.queue.Enqueue(defaultNodeObj)
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
		c.logger.V(2).Info("Waiting for hasSynced", "elapsedTime", time.Now().Sub(start))
		time.Sleep(1 * time.Second)
	}
	c.logger.V(2).Info("Caches synced", "timeTaken", time.Now().Sub(start))
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
	c.logger.V(4).Info("Instance groups controller: Start processing", "key", key)
	defer func() {
		c.logger.V(4).Info("Instance groups controller: Processing key finished", "key", key, "timeTaken", time.Since(start))
	}()

	nodes, err := c.zoneGetter.ListNodes(zonegetter.CandidateNodesFilter, c.logger)
	if err != nil {
		return err
	}
	return c.igManager.Sync(utils.GetNodeNames(nodes), c.logger)
}
