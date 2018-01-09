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

package controller

import (
	"time"

	listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/ingress-gce/pkg/context"
	"k8s.io/ingress-gce/pkg/utils"
)

const (
	nodeResyncPeriod = 1 * time.Second
)

// NodeController synchronizes the state of the nodes to the unmanaged instance
// groups.
type NodeController struct {
	lister cache.Indexer
	queue  utils.TaskQueue
	cm     *ClusterManager
}

// NewNodeController returns a new node update controller.
func NewNodeController(ctx *context.ControllerContext, cm *ClusterManager) *NodeController {
	c := &NodeController{
		lister: ctx.NodeInformer.GetIndexer(),
		cm:     cm,
	}
	c.queue = utils.NewPeriodicTaskQueue("nodes", c.sync)

	ctx.NodeInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.queue.Enqueue,
		DeleteFunc: c.queue.Enqueue,
	})

	return c
}

// Run a go routine to process updates for the controller.
func (c *NodeController) Run(stopCh chan struct{}) {
	go c.queue.Run(nodeResyncPeriod, stopCh)
}

// Run a go routine to process updates for the controller.
func (c *NodeController) Shutdown() {
	c.queue.Shutdown()
}

func (c *NodeController) sync(key string) error {
	nodeNames, err := getReadyNodeNames(listers.NewNodeLister(c.lister))
	if err != nil {
		return err
	}
	return c.cm.instancePool.Sync(nodeNames)
}
