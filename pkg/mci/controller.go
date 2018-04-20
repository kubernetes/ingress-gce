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
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	clientcmd "k8s.io/client-go/tools/clientcmd/api"
	crv1alpha1 "k8s.io/cluster-registry/pkg/apis/clusterregistry/v1alpha1"
	"k8s.io/ingress-gce/pkg/context"
	"k8s.io/ingress-gce/pkg/informer"
	"k8s.io/ingress-gce/pkg/mapper"
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
}

// MCIEnqueue is a interface to allow the MCI controller to enqueue ingresses
// based on events it receives.
type MCIEnqueue interface {
	EnqueueAllIngresses() error
}

func NewController(ctx *context.ControllerContext, resyncPeriod time.Duration, enqueue MCIEnqueue) (*Controller, error) {
	mciController := &Controller{
		ctx:           ctx,
		resyncPeriod:  resyncPeriod,
		clusterSynced: ctx.MC.ClusterInformer.HasSynced,
		clusterLister: ctx.MC.ClusterInformer.GetIndexer(),
	}

	ctx.MC.ClusterInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			c := obj.(*crv1alpha1.Cluster)
			glog.V(3).Infof("Cluster %v added", c.Name)
			mciController.handleClusterAdd(c)
			// For now, queue up all ingresses
			err := enqueue.EnqueueAllIngresses()
			if err != nil {
				glog.V(3).Infof("Error enqueuing ingresses on add of cluster %v: %v", c.Name, err)
			}
		},
		DeleteFunc: func(obj interface{}) {
			c := obj.(*crv1alpha1.Cluster)
			glog.V(3).Infof("Cluster %v deleted", c.Name)
			mciController.handleClusterDelete(c)
			err := enqueue.EnqueueAllIngresses()
			if err != nil {
				glog.V(3).Infof("Error enqueuing ingress on delete of cluster %v: %v", c.Name, err)
			}
		},
		UpdateFunc: func(obj, cur interface{}) {
			c := obj.(*crv1alpha1.Cluster)
			glog.V(3).Infof("Cluster %v updated", c.Name)
		},
	})
	return mciController, nil
}

func (controller *Controller) handleClusterAdd(c *crv1alpha1.Cluster) {
	client, err := buildClusterClient(c)
	if err != nil {
		glog.V(3).Infof("Error building client for cluster %v: %v", c.Name, err)
	}
	// Keep track of the client
	controller.ctx.MC.ClusterClients[c.Name] = client
	// Create informers for this cluster.
	informerManager := informer.NewClusterInformerManager(client, controller.resyncPeriod)
	informerManager.CreateInformers()

	// TODO(rramkumar): For now, just add event handlers for Ingress.

	// Keep track of the informer manager.
	controller.ctx.MC.ClusterInformerManagers[c.Name] = informerManager
	// Create a service mapper for this cluster
	serviceInformer := informerManager.Informers().ServiceInformer
	svcGetter := utils.SvcGetter{Store: serviceInformer.GetStore()}
	svcMapper := mapper.NewClusterServiceMapper(svcGetter.Get, nil)
	// Keep track of the service mapper.
	controller.ctx.MC.ClusterServiceMappers[c.Name] = svcMapper
	glog.V(3).Infof("Built client and informers for cluster %v", c.Name)
}

func (controller *Controller) handleClusterDelete(c *crv1alpha1.Cluster) {
	// Remove client for this cluster
	delete(controller.ctx.MC.ClusterClients, c.Name)
	// Stop informers.
	informerManager := controller.ctx.MC.ClusterInformerManagers[c.Name]
	informerManager.DeleteInformers()
	delete(controller.ctx.MC.ClusterInformerManagers, c.Name)
	// Remove cluster service mappers
	delete(controller.ctx.MC.ClusterServiceMappers, c.Name)
	glog.V(3).Infof("Removed client and informers for cluster %v", c.Name)
}

// buildClusterClient builds a k8s client given a cluster from the ClusterRegistry.
func buildClusterClient(c *crv1alpha1.Cluster) (kubernetes.Interface, error) {
	// Config used to instantiate a client
	restConfig := &rest.Config{}
	// Get endpoint for the master. For now, we only consider the first endpoint given.
	masterEndpoints := c.Spec.KubernetesAPIEndpoints.ServerEndpoints
	if len(masterEndpoints) == 0 {
		return nil, fmt.Errorf("No master endpoints provided")
	}
	// Populate config with master endpoint.
	restConfig.Host = masterEndpoints[0].ServerAddress
	// Don't verify TLS for now
	restConfig.TLSClientConfig = rest.TLSClientConfig{Insecure: true}
	// Get auth for the master. We assume auth mechanism is using a client cert.
	authProviders := c.Spec.AuthInfo.Providers
	if len(authProviders) == 0 {
		return nil, fmt.Errorf("No auth providers provided")
	}
	providerConfig := c.Spec.AuthInfo.Providers[0]
	// Populate config with client auth.
	restConfig.AuthProvider = &clientcmd.AuthProviderConfig{Name: providerConfig.Name, Config: providerConfig.Config}
	return kubernetes.NewForConfig(restConfig)
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
