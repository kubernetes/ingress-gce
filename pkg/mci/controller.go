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
	"fmt"
	"time"

	"github.com/golang/glog"
	extensions "k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	crv1alpha1 "k8s.io/cluster-registry/pkg/apis/clusterregistry/v1alpha1"
	"k8s.io/ingress-gce/pkg/context"
	"k8s.io/ingress-gce/pkg/informer"
	"k8s.io/ingress-gce/pkg/mapper"
	"k8s.io/ingress-gce/pkg/target"
	"k8s.io/ingress-gce/pkg/utils"
)

// Controller is a barebones multi-cluster ingress controller.
// For now, this controller only logs messages when CRUD operations are
// made on a crv1alpha1.Cluster.
type Controller struct {
	ctx          *context.ControllerContext
	resyncPeriod time.Duration

	clusterSynced cache.InformerSynced
	clusterLister cache.Indexer
	queueHandle   MCIEnqueue
}

// MCIEnqueue is a interface to allow the MCI controller to enqueue ingresses
// based on events it receives.
type MCIEnqueue interface {
	EnqueueAllIngresses() error
	EnqueueIngress(ing *extensions.Ingress)
}

func NewController(ctx *context.ControllerContext, resyncPeriod time.Duration, queueHandle MCIEnqueue) (*Controller, error) {
	mciController := &Controller{
		ctx:           ctx,
		resyncPeriod:  resyncPeriod,
		clusterSynced: ctx.MC.ClusterInformer.HasSynced,
		clusterLister: ctx.MC.ClusterInformer.GetIndexer(),
		queueHandle:   queueHandle,
	}

	ctx.MC.ClusterInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			c := obj.(*crv1alpha1.Cluster)
			glog.V(3).Infof("Cluster %v added", c.Name)
			err := mciController.handleClusterAdd(c)
			if err != nil {
				glog.V(3).Infof("Error bootstrapping resources for cluster %v: %v", c.Name, err)
			}
			// For now, queue up all ingresses
			err = queueHandle.EnqueueAllIngresses()
			if err != nil {
				glog.V(3).Infof("Error enqueuing ingresses on add of cluster %v: %v", c.Name, err)
			}
		},
		DeleteFunc: func(obj interface{}) {
			c := obj.(*crv1alpha1.Cluster)
			glog.V(3).Infof("Cluster %v deleted", c.Name)
			mciController.handleClusterDelete(c)
			// For now, queue up all ingresses
			err := queueHandle.EnqueueAllIngresses()
			if err != nil {
				glog.V(3).Infof("Error enqueuing ingresses on add of cluster %v: %v", c.Name, err)
			}
		},
		UpdateFunc: func(obj, cur interface{}) {
			c := obj.(*crv1alpha1.Cluster)
			glog.V(3).Infof("Cluster %v updated", c.Name)
		},
	})
	return mciController, nil
}

func (controller *Controller) handleClusterAdd(c *crv1alpha1.Cluster) error {
	client, err := buildClusterClient(c)
	if err != nil {
		return fmt.Errorf("Error building client for cluster %v: %v", c.Name, err)
	}
	// Keep track of the client
	controller.ctx.MC.ClusterClients[c.Name] = client
	// Create informers for this cluster.
	informerManager := informer.NewClusterInformerManager(client, controller.resyncPeriod)
	informerManager.CreateInformers()

	// For now, just add event handlers for Ingress.
	controller.addIngressEventHandlers(informerManager, c.Name)

	// Keep track of the informer manager.
	controller.ctx.MC.ClusterInformerManagers[c.Name] = informerManager
	// Create a service mapper for this cluster
	serviceInformer := informerManager.Informers().ServiceInformer
	svcGetter := utils.SvcGetter{Store: serviceInformer.GetStore()}
	svcMapper := mapper.NewClusterServiceMapper(svcGetter.Get, nil)
	// Keep track of the service mapper.
	controller.ctx.MC.ClusterServiceMappers[c.Name] = svcMapper
	// Create a target resource manager for this cluster
	targetResourceManager := target.NewTargetResourceManager(client)
	controller.ctx.MC.ClusterResourceManagers[c.Name] = targetResourceManager
	glog.V(3).Infof("Built client and informers for cluster %v", c.Name)
	return nil
}

func (controller *Controller) handleClusterDelete(c *crv1alpha1.Cluster) {
	// Remove all ingresses in the cluster
	controller.deleteIngressesFromCluster(c)
	// Remove client for this cluster
	delete(controller.ctx.MC.ClusterClients, c.Name)
	// Stop informers.
	informerManager := controller.ctx.MC.ClusterInformerManagers[c.Name]
	informerManager.DeleteInformers()
	delete(controller.ctx.MC.ClusterInformerManagers, c.Name)
	// Remove cluster service mapper.
	delete(controller.ctx.MC.ClusterServiceMappers, c.Name)
	// Remove target resource manager
	delete(controller.ctx.MC.ClusterResourceManagers, c.Name)
	glog.V(3).Infof("Removed client and informers for cluster %v", c.Name)
}

func (controller *Controller) addIngressEventHandlers(informerManager informer.ClusterInformerManager, clusterName string) {
	informerManager.AddHandlersForInformer(informer.IngressInformer, cache.ResourceEventHandlerFuncs{
		// Note: For now, we don't care about ingresses being added or deleted in "target" clusters.
		UpdateFunc: func(obj, cur interface{}) {
			ing := obj.(*extensions.Ingress)
			controller.queueHandle.EnqueueIngress(ing)
			glog.V(3).Infof("Target ingress %v/%v updated in cluster %v. Requeueing ingress...", ing.Namespace, ing.Name, clusterName)
		},
	})
}

func (controller *Controller) deleteIngressesFromCluster(c *crv1alpha1.Cluster) {
	ingLister := utils.StoreToIngressLister{Store: controller.ctx.IngressInformer.GetStore()}
	resourceManager := controller.ctx.MC.ClusterResourceManagers[c.Name]
	ings, err := ingLister.ListGCEIngresses()
	if err != nil {
		glog.V(3).Infof("Error listing ingresses before deletion of target ingresses from cluster %v", c.Name, err)
	}
	for _, ing := range ings.Items {
		err = resourceManager.DeleteTargetIngress(ing.Name, ing.Namespace)
		if err != nil {
			glog.V(3).Infof("Error deleting target ingress %v/%v in cluster %v: %v", ing.Namespace, ing.Name, c.Name, err)
			return
		}
	}
	glog.V(3).Infof("Deleted all target ingresses in cluster %v", c.Name)
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
