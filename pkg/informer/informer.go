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

package informer

import (
	"time"

	informerv1 "k8s.io/client-go/informers/core/v1"
	informerv1beta1 "k8s.io/client-go/informers/extensions/v1beta1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

const (
	ServiceInformer = "Service"
	IngressInformer = "Ingress"
)

type InformerPair struct {
	IngressInformer cache.SharedIndexInformer
	ServiceInformer cache.SharedIndexInformer
}

type clusterInformerManager struct {
	informers    InformerPair
	stopChan     chan struct{}
	kubeClient   kubernetes.Interface
	resyncPeriod time.Duration
}

var _ ClusterInformerManager = &clusterInformerManager{}

func NewClusterInformerManager(kubeClient kubernetes.Interface, resyncPeriod time.Duration) ClusterInformerManager {
	return &clusterInformerManager{
		stopChan:     make(chan struct{}),
		kubeClient:   kubeClient,
		resyncPeriod: resyncPeriod,
	}
}

func (m *clusterInformerManager) CreateInformers() {
	IngressInformer := informerv1beta1.NewIngressInformer(m.kubeClient, "", m.resyncPeriod, newIndexer())
	ServiceInformer := informerv1.NewServiceInformer(m.kubeClient, "", m.resyncPeriod, newIndexer())
	m.informers = InformerPair{IngressInformer, ServiceInformer}
	go IngressInformer.Run(m.stopChan)
	go ServiceInformer.Run(m.stopChan)
}

func (m *clusterInformerManager) DeleteInformers() {
	close(m.stopChan)
}

func (m *clusterInformerManager) HasSynced() bool {
	informerPair := m.informers
	return informerPair.IngressInformer.HasSynced() && informerPair.ServiceInformer.HasSynced()
}

func (m *clusterInformerManager) AddHandlersForInformer(informerType string, handlers cache.ResourceEventHandlerFuncs) {
	switch informerType {
	case ServiceInformer:
		m.informers.ServiceInformer.AddEventHandler(handlers)
	case IngressInformer:
		m.informers.IngressInformer.AddEventHandler(handlers)
	default:
		return
	}
}

func (m *clusterInformerManager) Informers() InformerPair {
	return m.informers
}

func newIndexer() cache.Indexers {
	return cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}
}
