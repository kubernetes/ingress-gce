/*
Copyright 2018 Google LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"fmt"
	"time"

	"github.com/golang/glog"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	"github.com/GoogleCloudPlatform/gke-managed-certs/pkg/client"
	"github.com/GoogleCloudPlatform/gke-managed-certs/pkg/client/ingress"
	"github.com/GoogleCloudPlatform/gke-managed-certs/pkg/client/ssl"
	"github.com/GoogleCloudPlatform/gke-managed-certs/pkg/clientgen/clientset/versioned"
	mcrtlister "github.com/GoogleCloudPlatform/gke-managed-certs/pkg/clientgen/listers/gke.googleapis.com/v1alpha1"
	"github.com/GoogleCloudPlatform/gke-managed-certs/pkg/controller/state"
)

type Controller struct {
	mcrt    *versioned.Clientset
	lister  mcrtlister.ManagedCertificateLister
	synced  cache.InformerSynced
	queue   workqueue.RateLimitingInterface
	ssl     *ssl.SSL
	state   *state.State
	ingress *ingress.Ingress
}

func New(clients *client.Clients) *Controller {
	mcrtInformer := clients.McrtInformerFactory.Gke().V1alpha1().ManagedCertificates()
	informer := mcrtInformer.Informer()

	controller := &Controller{
		mcrt:    clients.Mcrt,
		lister:  mcrtInformer.Lister(),
		synced:  informer.HasSynced,
		queue:   workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "queue"),
		ssl:     clients.SSL,
		state:   state.New(clients.ConfigMap),
		ingress: clients.Ingress,
	}

	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			controller.enqueue(obj)
		},
		UpdateFunc: func(old, new interface{}) {
			controller.enqueue(new)
		},
		DeleteFunc: func(obj interface{}) {
			controller.enqueue(obj)
		},
	})

	return controller
}

func (c *Controller) Run(stopChannel <-chan struct{}) error {
	defer runtime.HandleCrash()
	defer c.queue.ShutDown()

	glog.Info("Controller.Run()")

	glog.Info("Waiting for Managed Certificate cache sync")
	if !cache.WaitForCacheSync(stopChannel, c.synced) {
		return fmt.Errorf("Timed out waiting for Managed Certificate cache sync")
	}
	glog.Info("Managed Certificate cache synced")

	go wait.Until(c.runWorker, time.Second, stopChannel)
	go wait.Until(c.synchronizeAllMcrts, time.Minute, stopChannel)

	go wait.Until(c.updatePreSharedCertAnnotation, time.Minute, stopChannel)

	glog.Info("Controller waiting for stop signal or error")

	<-stopChannel
	glog.Info("Controller received stop signal")

	glog.Info("Controller shutting down")
	return nil
}
