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

// Package client provides clients which are used to communicate with api server and GCLB.
package client

import (
	"fmt"

	"k8s.io/client-go/rest"

	"github.com/GoogleCloudPlatform/gke-managed-certs/pkg/client/configmap"
	"github.com/GoogleCloudPlatform/gke-managed-certs/pkg/client/ingress"
	"github.com/GoogleCloudPlatform/gke-managed-certs/pkg/client/ssl"
	"github.com/GoogleCloudPlatform/gke-managed-certs/pkg/clientgen/clientset/versioned"
	"github.com/GoogleCloudPlatform/gke-managed-certs/pkg/clientgen/informers/externalversions"
)

// Clients are used to communicate with api server and GCLB
type Clients struct {
	// ConfigMap manages ConfigMap objects
	ConfigMap configmap.Client

	// Ingress manages Ingress objects
	Ingress *ingress.Ingress

	// Mcrt manages ManagedCertificate custom resources
	Mcrt *versioned.Clientset

	// McrtInfomerFactory produces informers and listers which handle ManagedCertificate custom resources
	McrtInformerFactory externalversions.SharedInformerFactory

	// SSL manages SslCertificate GCP resources
	SSL *ssl.SSL
}

func New(cloudConfig string) (*Clients, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("Could not fetch cluster config, err: %v", err)
	}

	mcrt := versioned.NewForConfigOrDie(config)
	factory := externalversions.NewSharedInformerFactory(mcrt, 0)

	ssl, err := ssl.New(cloudConfig)
	if err != nil {
		return nil, err
	}

	return &Clients{
		ConfigMap:           configmap.New(config),
		Ingress:             ingress.New(config),
		Mcrt:                mcrt,
		McrtInformerFactory: factory,
		SSL:                 ssl,
	}, nil
}
