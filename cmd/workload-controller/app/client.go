/*
Copyright 2020 The Kubernetes Authors.

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

package app

import (
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/klog"

	// Register the GCP authorization provider.
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
)

// NewKubeConfigForProtobuf returns a Kubernetes client config that uses protobufs
// for given the command line settings.
func NewKubeConfigForProtobuf() (*rest.Config, error) {
	config, err := NewKubeConfig()
	if err != nil {
		return nil, err
	}
	// Use protobufs for communication with apiserver
	config.ContentType = "application/vnd.kubernetes.protobuf"
	return config, nil
}

// NewKubeConfig returns a Kubernetes client config given the command line settings.
func NewKubeConfig() (*rest.Config, error) {
	if flags.F.InCluster {
		klog.V(0).Infof("Using in cluster configuration")
		return rest.InClusterConfig()
	}

	klog.V(0).Infof("Using APIServerHost=%q, KubeConfig=%q", flags.F.APIServerHost, flags.F.KubeConfigFile)
	return clientcmd.BuildConfigFromFlags(flags.F.APIServerHost, flags.F.KubeConfigFile)
}
