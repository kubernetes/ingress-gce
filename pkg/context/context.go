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
	"sync"
	"time"

	apiv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	informerv1 "k8s.io/client-go/informers/core/v1"
	informerv1beta1 "k8s.io/client-go/informers/networking/v1beta1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	backendconfigclient "k8s.io/ingress-gce/pkg/backendconfig/client/clientset/versioned"
	informerbackendconfig "k8s.io/ingress-gce/pkg/backendconfig/client/informers/externalversions/backendconfig/v1beta1"
	"k8s.io/ingress-gce/pkg/common/typed"
	frontendconfigclient "k8s.io/ingress-gce/pkg/frontendconfig/client/clientset/versioned"
	informerfrontendconfig "k8s.io/ingress-gce/pkg/frontendconfig/client/informers/externalversions/frontendconfig/v1beta1"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/klog"
	"k8s.io/legacy-cloud-providers/gce"
)

const (
	// Frequency to poll on local stores to sync.
	StoreSyncPollPeriod = 5 * time.Second
)

// ControllerContext holds the state needed for the execution of the controller.
type ControllerContext struct {
	KubeClient            kubernetes.Interface
	DestinationRuleClient dynamic.NamespaceableResourceInterface

	Cloud *gce.Cloud

	ClusterNamer *namer.Namer

	ControllerContextConfig

	IngressInformer         cache.SharedIndexInformer
	ServiceInformer         cache.SharedIndexInformer
	BackendConfigInformer   cache.SharedIndexInformer
	FrontendConfigInformer  cache.SharedIndexInformer
	PodInformer             cache.SharedIndexInformer
	NodeInformer            cache.SharedIndexInformer
	EndpointInformer        cache.SharedIndexInformer
	DestinationRuleInformer cache.SharedIndexInformer

	healthChecks map[string]func() error

	lock sync.Mutex

	// Map of namespace => record.EventRecorder.
	recorders map[string]record.EventRecorder
}

// ControllerContextConfig encapsulates some settings that are tunable via command line flags.
type ControllerContextConfig struct {
	Namespace    string
	ResyncPeriod time.Duration
	// DefaultBackendSvcPortID is the ServicePort for the system default backend.
	DefaultBackendSvcPort         utils.ServicePort
	HealthCheckPath               string
	DefaultBackendHealthCheckPath string
	FrontendConfigEnabled         bool
	EnableCSM                     bool
}

// NewControllerContext returns a new shared set of informers.
func NewControllerContext(
	kubeClient kubernetes.Interface,
	dynamicClient dynamic.Interface,
	backendConfigClient backendconfigclient.Interface,
	frontendConfigClient frontendconfigclient.Interface,
	cloud *gce.Cloud,
	namer *namer.Namer,
	config ControllerContextConfig) *ControllerContext {

	context := &ControllerContext{
		KubeClient:              kubeClient,
		Cloud:                   cloud,
		ClusterNamer:            namer,
		ControllerContextConfig: config,
		IngressInformer:         informerv1beta1.NewIngressInformer(kubeClient, config.Namespace, config.ResyncPeriod, utils.NewNamespaceIndexer()),
		ServiceInformer:         informerv1.NewServiceInformer(kubeClient, config.Namespace, config.ResyncPeriod, utils.NewNamespaceIndexer()),
		BackendConfigInformer:   informerbackendconfig.NewBackendConfigInformer(backendConfigClient, config.Namespace, config.ResyncPeriod, utils.NewNamespaceIndexer()),
		EndpointInformer:        informerv1.NewEndpointsInformer(kubeClient, config.Namespace, config.ResyncPeriod, utils.NewNamespaceIndexer()),
		PodInformer:             informerv1.NewPodInformer(kubeClient, config.Namespace, config.ResyncPeriod, utils.NewNamespaceIndexer()),
		NodeInformer:            informerv1.NewNodeInformer(kubeClient, config.ResyncPeriod, utils.NewNamespaceIndexer()),
		recorders:               map[string]record.EventRecorder{},
		healthChecks:            make(map[string]func() error),
	}

	if config.EnableCSM && dynamicClient != nil {
		klog.Warning("The DestinationRule group version is v1alpha3 in group networking.istio.io. Need to update as istio API graduates.")
		destrinationGVR := schema.GroupVersionResource{Group: "networking.istio.io", Version: "v1alpha3", Resource: "destinationrules"}
		drDynamicInformer := dynamicinformer.NewFilteredDynamicInformer(dynamicClient, destrinationGVR, config.Namespace, config.ResyncPeriod,
			cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
			nil)
		context.DestinationRuleInformer = drDynamicInformer.Informer()
		context.DestinationRuleClient = dynamicClient.Resource(destrinationGVR)
	}

	if config.FrontendConfigEnabled {
		context.FrontendConfigInformer = informerfrontendconfig.NewFrontendConfigInformer(frontendConfigClient, config.Namespace, config.ResyncPeriod, utils.NewNamespaceIndexer())
	}

	return context
}

// HasSynced returns true if all relevant informers has been synced.
func (ctx *ControllerContext) HasSynced() bool {
	funcs := []func() bool{
		ctx.IngressInformer.HasSynced,
		ctx.ServiceInformer.HasSynced,
		ctx.BackendConfigInformer.HasSynced,
		ctx.PodInformer.HasSynced,
		ctx.NodeInformer.HasSynced,
		ctx.EndpointInformer.HasSynced,
	}

	if ctx.FrontendConfigInformer != nil {
		funcs = append(funcs, ctx.FrontendConfigInformer.HasSynced)
	}

	if ctx.DestinationRuleInformer != nil {
		funcs = append(funcs, ctx.DestinationRuleInformer.HasSynced)
	}

	for _, f := range funcs {
		if !f() {
			return false
		}
	}
	return true
}

// Recorder return the event recorder for the given namespace.
func (ctx *ControllerContext) Recorder(ns string) record.EventRecorder {
	if rec, ok := ctx.recorders[ns]; ok {
		return rec
	}

	broadcaster := record.NewBroadcaster()
	broadcaster.StartLogging(klog.Infof)
	broadcaster.StartRecordingToSink(&corev1.EventSinkImpl{
		Interface: ctx.KubeClient.CoreV1().Events(ns),
	})
	rec := broadcaster.NewRecorder(scheme.Scheme, apiv1.EventSource{Component: "loadbalancer-controller"})
	ctx.recorders[ns] = rec

	return rec
}

// AddHealthCheck registers function to be called for healthchecking.
func (ctx *ControllerContext) AddHealthCheck(id string, hc func() error) {
	ctx.lock.Lock()
	defer ctx.lock.Unlock()

	ctx.healthChecks[id] = hc
}

// HealthCheckResults contains a mapping of component -> health check results.
type HealthCheckResults map[string]error

// HealthCheck runs all registered healthcheck functions.
func (ctx *ControllerContext) HealthCheck() HealthCheckResults {
	ctx.lock.Lock()
	defer ctx.lock.Unlock()

	healthChecks := make(map[string]error)
	for component, f := range ctx.healthChecks {
		healthChecks[component] = f()
	}

	return healthChecks
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
	if ctx.FrontendConfigInformer != nil {
		go ctx.FrontendConfigInformer.Run(stopCh)
	}
	if ctx.DestinationRuleInformer != nil {
		go ctx.DestinationRuleInformer.Run(stopCh)
	}
}

// Ingresses returns the store of Ingresses.
func (ctx *ControllerContext) Ingresses() *typed.IngressStore {
	return typed.WrapIngressStore(ctx.IngressInformer.GetStore())
}

// Services returns the store of Services.
func (ctx *ControllerContext) Services() *typed.ServiceStore {
	return typed.WrapServiceStore(ctx.ServiceInformer.GetStore())
}

// BackendConfigs returns the store of BackendConfigs.
func (ctx *ControllerContext) BackendConfigs() *typed.BackendConfigStore {
	return typed.WrapBackendConfigStore(ctx.BackendConfigInformer.GetStore())
}

// FrontendConfigs returns the store of FrontendConfigs.
func (ctx *ControllerContext) FrontendConfigs() *typed.FrontendConfigStore {
	if ctx.FrontendConfigInformer == nil {
		return typed.WrapFrontendConfigStore(nil)
	}
	return typed.WrapFrontendConfigStore(ctx.FrontendConfigInformer.GetStore())
}
