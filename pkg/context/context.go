/*
Copyright 2017 The Kubernetes Authors.

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

package context

import (
	"time"

	"github.com/golang/glog"

	apiv1 "k8s.io/api/core/v1"
	informerv1 "k8s.io/client-go/informers/core/v1"
	informerv1beta1 "k8s.io/client-go/informers/extensions/v1beta1"
	"k8s.io/client-go/kubernetes"
	scheme "k8s.io/client-go/kubernetes/scheme"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	crclient "k8s.io/cluster-registry/pkg/client/clientset_generated/clientset"
	crinformerv1alpha1 "k8s.io/cluster-registry/pkg/client/informers_generated/externalversions/clusterregistry/v1alpha1"
	"k8s.io/ingress-gce/pkg/informer"
	"k8s.io/ingress-gce/pkg/mapper"
	"k8s.io/ingress-gce/pkg/target"
	"k8s.io/ingress-gce/pkg/utils"
)

// ControllerContext holds resources necessary for the general
// workflow of the controller.
type ControllerContext struct {
	KubeClient kubernetes.Interface

	IngressInformer  cache.SharedIndexInformer
	ServiceInformer  cache.SharedIndexInformer
	PodInformer      cache.SharedIndexInformer
	NodeInformer     cache.SharedIndexInformer
	EndpointInformer cache.SharedIndexInformer

	// Map of namespace => record.EventRecorder.
	recorders map[string]record.EventRecorder

	MC MultiClusterContext
}

// MultiClusterContext holds resource necessary for MCI mode.
type MultiClusterContext struct {
	RegistryClient  crclient.Interface
	ClusterInformer cache.SharedIndexInformer

	MCIEnabled              bool
	ClusterClients          map[string]kubernetes.Interface
	ClusterInformerManagers map[string]informer.ClusterInformerManager
	ClusterResourceManagers map[string]target.TargetResourceManager
	ClusterServiceMappers   map[string]mapper.ClusterServiceMapper
}

// NewControllerContext returns a new shared set of informers.
func NewControllerContext(kubeClient kubernetes.Interface, registryClient crclient.Interface, namespace string, resyncPeriod time.Duration, enableEndpointsInformer bool) *ControllerContext {
	newIndexer := func() cache.Indexers {
		return cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}
	}

	context := &ControllerContext{
		KubeClient:      kubeClient,
		IngressInformer: informerv1beta1.NewIngressInformer(kubeClient, namespace, resyncPeriod, newIndexer()),
		ServiceInformer: informerv1.NewServiceInformer(kubeClient, namespace, resyncPeriod, newIndexer()),
		PodInformer:     informerv1.NewPodInformer(kubeClient, namespace, resyncPeriod, newIndexer()),
		NodeInformer:    informerv1.NewNodeInformer(kubeClient, resyncPeriod, newIndexer()),
		MC:              MultiClusterContext{RegistryClient: registryClient},
		recorders:       map[string]record.EventRecorder{},
	}
	if enableEndpointsInformer {
		context.EndpointInformer = informerv1.NewEndpointsInformer(kubeClient, namespace, resyncPeriod, newIndexer())
	}
	if context.MC.RegistryClient != nil {
		context.MC.ClusterInformer = crinformerv1alpha1.NewClusterInformer(registryClient, resyncPeriod, newIndexer())
		context.MC.MCIEnabled = true
		context.MC.ClusterClients = make(map[string]kubernetes.Interface)
		context.MC.ClusterInformerManagers = make(map[string]informer.ClusterInformerManager)
		context.MC.ClusterServiceMappers = make(map[string]mapper.ClusterServiceMapper)
		context.MC.ClusterResourceManagers = make(map[string]target.TargetResourceManager)
	}

	return context
}

// HasSynced returns true if all relevant informers has been synced.
func (ctx *ControllerContext) HasSynced() bool {
	funcs := []func() bool{
		ctx.IngressInformer.HasSynced,
		ctx.ServiceInformer.HasSynced,
		ctx.PodInformer.HasSynced,
		ctx.NodeInformer.HasSynced,
	}
	if ctx.EndpointInformer != nil {
		funcs = append(funcs, ctx.EndpointInformer.HasSynced)
	}
	if ctx.MC.ClusterInformer != nil {
		funcs = append(funcs, ctx.MC.ClusterInformer.HasSynced)
	}
	for _, f := range funcs {
		if !f() {
			return false
		}
	}
	return true
}

// Recorder creates an event recorder for the specified namespace.
func (ctx *ControllerContext) Recorder(ns string) record.EventRecorder {
	if rec, ok := ctx.recorders[ns]; ok {
		return rec
	}

	broadcaster := record.NewBroadcaster()
	broadcaster.StartLogging(glog.Infof)
	broadcaster.StartRecordingToSink(&corev1.EventSinkImpl{
		Interface: ctx.KubeClient.Core().Events(ns),
	})
	rec := broadcaster.NewRecorder(scheme.Scheme, apiv1.EventSource{Component: "loadbalancer-controller"})
	ctx.recorders[ns] = rec

	return rec
}

// Start all of the informers.
func (ctx *ControllerContext) Start(stopCh chan struct{}) {
	go ctx.IngressInformer.Run(stopCh)
	go ctx.ServiceInformer.Run(stopCh)
	go ctx.PodInformer.Run(stopCh)
	go ctx.NodeInformer.Run(stopCh)
	if ctx.EndpointInformer != nil {
		go ctx.EndpointInformer.Run(stopCh)
	}

	if ctx.MC.ClusterInformer != nil {
		go ctx.MC.ClusterInformer.Run(stopCh)
	}
}

func (ctx *ControllerContext) ResourceManagers() (resourceManagers map[string]target.TargetResourceManager) {
	if !ctx.MC.MCIEnabled {
		return nil
	}
	return ctx.MC.ClusterResourceManagers
}

// If in MCI mode, ServiceMappers() gets mappers for all clusters in the
// cluster registry. This is because for now, we assume that all ingresses live
// in all "target" clusters. This will change once we start taking the
// MultiClusterConfig into account. If we are not in MCI mode,
// then ServiceMappers() consists of the mapper for the local cluster
func (ctx *ControllerContext) ServiceMappers() (svcMappers map[string]mapper.ClusterServiceMapper) {
	if ctx.MC.MCIEnabled {
		return ctx.MC.ClusterServiceMappers
	}
	// Create a ClusterServiceMapper for the local cluster.
	svcGetter := utils.SvcGetter{Store: ctx.ServiceInformer.GetStore()}
	return map[string]mapper.ClusterServiceMapper{"local": mapper.NewClusterServiceMapper(svcGetter.Get, nil)}
}
