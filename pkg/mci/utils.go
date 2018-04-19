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

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	crv1alpha1 "k8s.io/cluster-registry/pkg/apis/clusterregistry/v1alpha1"
)

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
	// Ignore TLS for now.
	restConfig.TLSClientConfig = rest.TLSClientConfig{Insecure: true}
	// Specify auth for master. We assume that basic auth is used (username + password)
	authProviders := c.Spec.AuthInfo.Providers
	if len(authProviders) == 0 {
		return nil, fmt.Errorf("No auth providers provided")
	}
	providerConfig := c.Spec.AuthInfo.Providers[0]
	if providerConfig.Config == nil {
		return nil, fmt.Errorf("No auth config found in %+v", providerConfig)
	}
	restConfig.Username = providerConfig.Config["username"]
	restConfig.Password = providerConfig.Config["password"]
	return kubernetes.NewForConfig(restConfig)
}
