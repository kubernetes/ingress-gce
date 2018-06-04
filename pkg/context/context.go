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
	backendconfigclient "k8s.io/ingress-gce/pkg/backendconfig/client/clientset/versioned"
	informerbackendconfig "k8s.io/ingress-gce/pkg/backendconfig/client/informers/externalversions/backendconfig/v1beta1"
)

// ControllerContext holds the state needed for the execution of the controller.
type ControllerContext struct {
	KubeClient kubernetes.Interface

	IngressInformer       cache.SharedIndexInformer
	ServiceInformer       cache.SharedIndexInformer
	BackendConfigInformer cache.SharedIndexInformer
	PodInformer           cache.SharedIndexInformer
	NodeInformer          cache.SharedIndexInformer
	EndpointInformer      cache.SharedIndexInformer

	NEGEnabled           bool
	BackendConfigEnabled bool

	// Map of namespace => record.EventRecorder.
	recorders map[string]record.EventRecorder
}

// NewControllerContext returns a new shared set of informers.
func NewControllerContext(
	kubeClient kubernetes.Interface,
	backendConfigClient backendconfigclient.Interface,
	namespace string,
	resyncPeriod time.Duration,
	enableNEG bool,
	enableBackendConfig bool) *ControllerContext {

	newIndexer := func() cache.Indexers {
		return cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}
	}
	context := &ControllerContext{
		KubeClient:           kubeClient,
		IngressInformer:      informerv1beta1.NewIngressInformer(kubeClient, namespace, resyncPeriod, newIndexer()),
		ServiceInformer:      informerv1.NewServiceInformer(kubeClient, namespace, resyncPeriod, newIndexer()),
		PodInformer:          informerv1.NewPodInformer(kubeClient, namespace, resyncPeriod, newIndexer()),
		NodeInformer:         informerv1.NewNodeInformer(kubeClient, resyncPeriod, newIndexer()),
		NEGEnabled:           enableNEG,
		BackendConfigEnabled: enableBackendConfig,
		recorders:            map[string]record.EventRecorder{},
	}
	if enableNEG {
		context.EndpointInformer = informerv1.NewEndpointsInformer(kubeClient, namespace, resyncPeriod, newIndexer())
	}
	if enableBackendConfig {
		context.BackendConfigInformer = informerbackendconfig.NewBackendConfigInformer(backendConfigClient, namespace, resyncPeriod, newIndexer())
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
	if ctx.BackendConfigInformer != nil {
		funcs = append(funcs, ctx.BackendConfigInformer.HasSynced)
	}
	for _, f := range funcs {
		if !f() {
			return false
		}
	}
	return true
}

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
	if ctx.BackendConfigInformer != nil {
		go ctx.BackendConfigInformer.Run(stopCh)
	}
}
