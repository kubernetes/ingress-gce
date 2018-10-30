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

	"github.com/golang/glog"
	compute "google.golang.org/api/compute/v0.alpha"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"

	api "github.com/GoogleCloudPlatform/gke-managed-certs/pkg/apis/gke.googleapis.com/v1alpha1"
	"github.com/GoogleCloudPlatform/gke-managed-certs/pkg/controller/translate"
	"github.com/GoogleCloudPlatform/gke-managed-certs/pkg/utils/equal"
	"github.com/GoogleCloudPlatform/gke-managed-certs/pkg/utils/random"
)

func (c *Controller) createSslCertificateIfNeeded(sslCertificateName string, mcrt *api.ManagedCertificate) (*compute.SslCertificate, error) {
	exists, err := c.ssl.Exists(sslCertificateName)
	if err != nil {
		return nil, err
	}

	if !exists {
		// SslCertificate does not yet exist, create it
		glog.Infof("Controller: create a new SslCertificate %s associated with Managed Certificate %s, based on state", sslCertificateName, mcrt.Name)
		if err := c.ssl.Create(sslCertificateName, mcrt.Spec.Domains); err != nil {
			return nil, err
		}
	}

	sslCert, err := c.ssl.Get(sslCertificateName)
	if err != nil {
		return nil, err
	}

	if !equal.Certificates(*mcrt, *sslCert) {
		glog.Infof("Controller: ManagedCertificate %v and SslCertificate %v are different, removing the SslCertificate", mcrt, sslCert)
		err := c.ssl.Delete(sslCertificateName)
		if err != nil {
			return nil, err
		}

		glog.Infof("Controller: SslCertificate %s deleted", sslCertificateName)
	}

	return sslCert, nil
}

func (c *Controller) createSslCertificateNameIfNeeded(mcrt *api.ManagedCertificate) (string, error) {
	sslCertificateName, exists := c.state.Get(mcrt.Namespace, mcrt.Name)

	if exists && sslCertificateName != "" {
		return sslCertificateName, nil
	}

	// State does not have anything for this Managed Certificate or no SslCertificate is associated with it
	sslCertificateName, err := c.randomName()
	if err != nil {
		return "", err
	}

	glog.Infof("Controller: add to state SslCertificate name %s associated with Managed Certificate %s:%s", sslCertificateName, mcrt.Namespace, mcrt.Name)
	c.state.Put(mcrt.Namespace, mcrt.Name, sslCertificateName)
	return sslCertificateName, nil
}

func (c *Controller) handle(key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}

	mcrt, ok := c.getMcrt(namespace, name)
	if !ok {
		return nil
	}

	glog.Infof("Controller: handling Managed Certificate %s:%s", mcrt.Namespace, mcrt.Name)

	sslCertificateName, err := c.createSslCertificateNameIfNeeded(mcrt)
	if err != nil {
		return err
	}

	sslCert, err := c.createSslCertificateIfNeeded(sslCertificateName, mcrt)
	if err != nil {
		return err
	}

	if err := translate.Certificate(*sslCert, mcrt); err != nil {
		return err
	}

	_, err = c.mcrt.GkeV1alpha1().ManagedCertificates(mcrt.Namespace).Update(mcrt)
	return err
}

func (c *Controller) processNext() bool {
	obj, shutdown := c.queue.Get()

	if shutdown {
		return false
	}

	defer c.queue.Done(obj)

	key, ok := obj.(string)
	if !ok {
		c.queue.Forget(obj)
		runtime.HandleError(fmt.Errorf("Expected string in queue but got %T", obj))
		return true
	}

	err := c.handle(key)
	if err == nil {
		c.queue.Forget(obj)
		return true
	}

	c.queue.AddRateLimited(obj)
	runtime.HandleError(err)
	return true
}

func (c *Controller) runWorker() {
	for c.processNext() {
	}
}

func (c *Controller) randomName() (string, error) {
	name, err := random.Name()
	if err != nil {
		return "", err
	}

	exists, err := c.ssl.Exists(name)
	if err != nil {
		return "", err
	}

	if exists {
		// Name taken, choose a new one
		name, err = random.Name()
		if err != nil {
			return "", err
		}
	}

	return name, nil
}

func (c *Controller) getMcrt(namespace, name string) (*api.ManagedCertificate, bool) {
	mcrt, err := c.lister.ManagedCertificates(namespace).Get(name)
	if err == nil {
		return mcrt, true
	}

	//TODO(krzyk) generate k8s event - can't fetch mcrt
	runtime.HandleError(err)
	return nil, false
}

// Deletes SslCertificate mapped to already deleted Managed Certificate.
func (c *Controller) removeSslCertificate(namespace, name string) error {
	sslCertificateName, exists := c.state.Get(namespace, name)
	if !exists {
		glog.Infof("Controller: Can't find in state SslCertificate mapped to Managed Certificate %s:%s", namespace, name)
		return nil
	}

	// SslCertificate (still) exists in state, check if it exists in GCE
	exists, err := c.ssl.Exists(sslCertificateName)
	if err != nil {
		return err
	}

	if !exists {
		// SslCertificate already deleted
		glog.Infof("Controller: SslCertificate %s mapped to Managed Certificate %s:%s already deleted", sslCertificateName, namespace, name)
		return nil
	}

	// SslCertificate exists in GCE, remove it and delete entry from state
	if err := c.ssl.Delete(sslCertificateName); err != nil {
		// Failed to delete SslCertificate
		// TODO(krzyk): generate k8s event
		runtime.HandleError(err)
		return err
	}

	// Successfully deleted SslCertificate
	glog.Infof("Controller: successfully deleted SslCertificate %s mapped to Managed Certificate %s:%s", sslCertificateName, namespace, name)
	return nil
}

/*
* Handles clean up. If a Managed Certificate gets deleted, it's noticed here and acted upon.
 */
func (c *Controller) synchronizeState() {
	for _, key := range c.state.GetAllKeys() {
		// key identifies a Managed Certificate known in state
		if _, ok := c.getMcrt(key.Namespace, key.Name); !ok {
			// Managed Certificate exists in state, but does not exist in cluster
			glog.Infof("Controller: Managed Certificate %s:%s in state but not in cluster", key.Namespace, key.Name)

			if c.removeSslCertificate(key.Namespace, key.Name) == nil {
				// SslCertificate mapped to Managed Certificate is already deleted. Remove entry from state.
				c.state.Delete(key.Namespace, key.Name)
			}
		}
	}
}

func (c *Controller) synchronizeAllMcrts() {
	c.synchronizeState()
	c.enqueueAll()
}
