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

// Package ingress provides operations for manipulating Ingress objects.
package ingress

import (
	api "k8s.io/api/extensions/v1beta1"
	"k8s.io/client-go/kubernetes/typed/extensions/v1beta1"
	"k8s.io/client-go/rest"
)

const (
	resource = "ingresses"
)

type Ingress struct {
	client *v1beta1.ExtensionsV1beta1Client
}

func New(config *rest.Config) *Ingress {
	return &Ingress{
		client: v1beta1.NewForConfigOrDie(config),
	}
}

// List fetches all Ingress objects in the cluster, from all namespaces.
func (c *Ingress) List() (*api.IngressList, error) {
	var result api.IngressList
	err := c.client.RESTClient().Get().Resource(resource).Do().Into(&result)
	return &result, err
}

// Update updates a given Ingress object.
func (c *Ingress) Update(ingress *api.Ingress) (*api.Ingress, error) {
	return c.client.Ingresses(ingress.Namespace).Update(ingress)
}
