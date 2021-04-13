/*
Copyright 2021 The Kubernetes Authors.

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
package adapterclient

import (
	"fmt"

	"k8s.io/client-go/kubernetes"
	networkingv1 "k8s.io/client-go/kubernetes/typed/networking/v1"
	networkingv1beta1 "k8s.io/client-go/kubernetes/typed/networking/v1beta1"
	"k8s.io/klog"
)

type AdapterClient struct {
	kubernetes.Interface
}

// NewAdapterKubeClient checks if the V1 API is available and will return an adapter
// client. If the V1 API is not available, the provided kubeClient will be returned
func NewAdapterKubeClient(kubeClient kubernetes.Interface) (kubernetes.Interface, bool, error) {
	useV1, err := supportsIngressV1(kubeClient)
	if err != nil {
		return nil, false, fmt.Errorf("errored checking for v1 api: %q", err)
	}
	if useV1 {
		klog.Infof("Using Adapter Client, all networking.k8s.io/ingress requests will use the V1 API")
		return &AdapterClient{
			Interface: kubeClient,
		}, true, nil
	}
	return kubeClient, false, nil
}

// NetworkingV1beta1 returns an custom NetworkingV1beta1 Client
func (c *AdapterClient) NetworkingV1beta1() networkingv1beta1.NetworkingV1beta1Interface {
	return &NetworkingClient{
		NetworkingV1beta1Interface: c.Interface.NetworkingV1beta1(),
		v1:                         c.Interface.NetworkingV1(),
	}
}

type NetworkingClient struct {
	networkingv1beta1.NetworkingV1beta1Interface
	v1 networkingv1.NetworkingV1Interface
}

// Ingresses returns a custom v1beta1 Ingress client that uses the V1 API
func (c *NetworkingClient) Ingresses(namespace string) networkingv1beta1.IngressInterface {
	return &v1IngressAdapterClient{v1: c.v1.Ingresses(namespace)}
}

// IngresClasses returns a custom v1beta1 Ingress Class client that uses the V1 API
func (c *NetworkingClient) IngressClasses() networkingv1beta1.IngressClassInterface {
	return &v1IngressClassAdapterClient{v1: c.v1.IngressClasses()}
}

// supportsIngressV1 checks to see if the Ingress V1 API exists on the cluster
func supportsIngressV1(client kubernetes.Interface) (bool, error) {
	if apiList, err := client.Discovery().ServerResourcesForGroupVersion("networking.k8s.io/v1"); err == nil {
		for _, r := range apiList.APIResources {
			if r.Kind == "Ingress" {
				return true, nil
			}
		}
	} else {
		return false, err
	}
	return false, nil
}
