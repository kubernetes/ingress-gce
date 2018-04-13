/*
Copyright 2018 The Kubernetes Authors.

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

package mci

import (
	"time"

	"github.com/golang/glog"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	crv1alpha1 "k8s.io/cluster-registry/pkg/apis/clusterregistry/v1alpha1"
	"k8s.io/ingress-gce/pkg/context"
)

// Controller is a barebones multi-cluster ingress controller.
// For now, this controller only logs messages when CRUD operations are
// made on a crv1alpha1.Cluster.
type Controller struct {
	resyncPeriod time.Duration

	clusterSynced cache.InformerSynced
	clusterLister cache.Indexer

	// TODO(rramkumar): Add lister for service extension CRD.
}

func NewController(ctx *context.ControllerContext, resyncPeriod time.Duration) (*Controller, error) {
	mciController := &Controller{
		resyncPeriod:  resyncPeriod,
		clusterSynced: ctx.MC.ClusterInformer.HasSynced,
		clusterLister: ctx.MC.ClusterInformer.GetIndexer(),
	}

	ctx.MC.ClusterInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			c := obj.(*crv1alpha1.Cluster)
			glog.V(3).Infof("Cluster %v added", c.Name)
		},
		DeleteFunc: func(obj interface{}) {
			c := obj.(*crv1alpha1.Cluster)
			glog.V(3).Infof("Cluster %v deleted", c.Name)
		},
		UpdateFunc: func(obj, cur interface{}) {
			c := obj.(*crv1alpha1.Cluster)
			glog.V(3).Infof("Cluster %v updated", c.Name)
		},
	})
	return mciController, nil
}

func (c *Controller) Run(stopCh <-chan struct{}) {
	wait.PollUntil(5*time.Second, func() (bool, error) {
		glog.V(2).Infof("Waiting for initial sync")
		return c.synced(), nil
	}, stopCh)

	glog.V(2).Infof("Starting Multi-Cluster Ingress controller")
}

func (c *Controller) synced() bool {
	return c.clusterSynced()
}
