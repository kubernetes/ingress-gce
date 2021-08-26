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
	context2 "context"
	"fmt"
	"sync"
	"time"

	apiv1 "k8s.io/api/core/v1"
	apiextensionsclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	informerv1 "k8s.io/client-go/informers/core/v1"
	discoveryinformer "k8s.io/client-go/informers/discovery/v1beta1"
	informernetworking "k8s.io/client-go/informers/networking/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	sav1beta1 "k8s.io/ingress-gce/pkg/apis/serviceattachment/v1beta1"
	backendconfigclient "k8s.io/ingress-gce/pkg/backendconfig/client/clientset/versioned"
	informerbackendconfig "k8s.io/ingress-gce/pkg/backendconfig/client/informers/externalversions/backendconfig/v1"
	"k8s.io/ingress-gce/pkg/cmconfig"
	"k8s.io/ingress-gce/pkg/common/typed"
	"k8s.io/ingress-gce/pkg/flags"
	frontendconfigclient "k8s.io/ingress-gce/pkg/frontendconfig/client/clientset/versioned"
	informerfrontendconfig "k8s.io/ingress-gce/pkg/frontendconfig/client/informers/externalversions/frontendconfig/v1beta1"
	ingparamsclient "k8s.io/ingress-gce/pkg/ingparams/client/clientset/versioned"
	informeringparams "k8s.io/ingress-gce/pkg/ingparams/client/informers/externalversions/ingparams/v1beta1"
	"k8s.io/ingress-gce/pkg/metrics"
	serviceattachmentclient "k8s.io/ingress-gce/pkg/serviceattachment/client/clientset/versioned"
	informerserviceattachment "k8s.io/ingress-gce/pkg/serviceattachment/client/informers/externalversions/serviceattachment/v1beta1"
	svcnegclient "k8s.io/ingress-gce/pkg/svcneg/client/clientset/versioned"
	informersvcneg "k8s.io/ingress-gce/pkg/svcneg/client/informers/externalversions/svcneg/v1beta1"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/endpointslices"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/klog"
	"k8s.io/legacy-cloud-providers/gce"
)

const (
	// Frequency to poll on local stores to sync.
	StoreSyncPollPeriod = 5 * time.Second

	ClusterTypeZonal    = "ZONAL"
	ClusterTypeRegional = "REGIONAL"
)

// ControllerContext holds the state needed for the execution of the controller.
type ControllerContext struct {
	KubeConfig            *rest.Config
	KubeClient            kubernetes.Interface
	SvcNegClient          svcnegclient.Interface
	DestinationRuleClient dynamic.NamespaceableResourceInterface
	SAClient              serviceattachmentclient.Interface

	Cloud *gce.Cloud

	ClusterNamer  *namer.Namer
	KubeSystemUID types.UID
	L4Namer       namer.L4ResourcesNamer

	ControllerContextConfig
	ASMConfigController *cmconfig.ConfigMapConfigController

	IngressInformer         cache.SharedIndexInformer
	ServiceInformer         cache.SharedIndexInformer
	BackendConfigInformer   cache.SharedIndexInformer
	FrontendConfigInformer  cache.SharedIndexInformer
	PodInformer             cache.SharedIndexInformer
	NodeInformer            cache.SharedIndexInformer
	EndpointInformer        cache.SharedIndexInformer
	EndpointSliceInformer   cache.SharedIndexInformer
	UseEndpointSlices       bool
	DestinationRuleInformer cache.SharedIndexInformer
	ConfigMapInformer       cache.SharedIndexInformer
	SvcNegInformer          cache.SharedIndexInformer
	IngClassInformer        cache.SharedIndexInformer
	IngParamsInformer       cache.SharedIndexInformer
	SAInformer              cache.SharedIndexInformer

	ControllerMetrics *metrics.ControllerMetrics

	healthChecks map[string]func() error

	lock sync.Mutex

	// Map of namespace => record.EventRecorder.
	recorders map[string]record.EventRecorder

	// NOTE: If the flag GKEClusterType is empty, then cluster will default to zonal. This field should not be used for
	// controller logic and should only be used for providing additional information to the user.
	RegionalCluster bool
}

// ControllerContextConfig encapsulates some settings that are tunable via command line flags.
type ControllerContextConfig struct {
	Namespace    string
	ResyncPeriod time.Duration
	NumL4Workers int
	// DefaultBackendSvcPortID is the ServicePort for the system default backend.
	DefaultBackendSvcPort utils.ServicePort
	HealthCheckPath       string
	FrontendConfigEnabled bool
	EnableASMConfigMap    bool
	ASMConfigMapNamespace string
	ASMConfigMapName      string
	EndpointSlicesEnabled bool
}

// NewControllerContext returns a new shared set of informers.
func NewControllerContext(
	kubeConfig *rest.Config,
	kubeClient kubernetes.Interface,
	backendConfigClient backendconfigclient.Interface,
	frontendConfigClient frontendconfigclient.Interface,
	svcnegClient svcnegclient.Interface,
	ingParamsClient ingparamsclient.Interface,
	saClient serviceattachmentclient.Interface,
	cloud *gce.Cloud,
	clusterNamer *namer.Namer,
	kubeSystemUID types.UID,
	config ControllerContextConfig) *ControllerContext {
	context := &ControllerContext{
		KubeConfig:              kubeConfig,
		KubeClient:              kubeClient,
		SvcNegClient:            svcnegClient,
		SAClient:                saClient,
		Cloud:                   cloud,
		ClusterNamer:            clusterNamer,
		L4Namer:                 namer.NewL4Namer(string(kubeSystemUID), clusterNamer),
		KubeSystemUID:           kubeSystemUID,
		ControllerMetrics:       metrics.NewControllerMetrics(),
		ControllerContextConfig: config,
		IngressInformer:         informernetworking.NewIngressInformer(kubeClient, config.Namespace, config.ResyncPeriod, utils.NewNamespaceIndexer()),
		ServiceInformer:         informerv1.NewServiceInformer(kubeClient, config.Namespace, config.ResyncPeriod, utils.NewNamespaceIndexer()),
		BackendConfigInformer:   informerbackendconfig.NewBackendConfigInformer(backendConfigClient, config.Namespace, config.ResyncPeriod, utils.NewNamespaceIndexer()),
		PodInformer:             informerv1.NewPodInformer(kubeClient, config.Namespace, config.ResyncPeriod, utils.NewNamespaceIndexer()),
		NodeInformer:            informerv1.NewNodeInformer(kubeClient, config.ResyncPeriod, utils.NewNamespaceIndexer()),
		SvcNegInformer:          informersvcneg.NewServiceNetworkEndpointGroupInformer(svcnegClient, config.Namespace, config.ResyncPeriod, utils.NewNamespaceIndexer()),
		recorders:               map[string]record.EventRecorder{},
		healthChecks:            make(map[string]func() error),
	}
	if config.FrontendConfigEnabled {
		context.FrontendConfigInformer = informerfrontendconfig.NewFrontendConfigInformer(frontendConfigClient, config.Namespace, config.ResyncPeriod, utils.NewNamespaceIndexer())
	}
	if ingParamsClient != nil {
		context.IngClassInformer = informernetworking.NewIngressClassInformer(kubeClient, config.ResyncPeriod, utils.NewNamespaceIndexer())
		context.IngParamsInformer = informeringparams.NewGCPIngressParamsInformer(ingParamsClient, config.ResyncPeriod, utils.NewNamespaceIndexer())
	}

	if saClient != nil {
		context.SAInformer = informerserviceattachment.NewServiceAttachmentInformer(saClient, config.Namespace, config.ResyncPeriod, utils.NewNamespaceIndexer())
	}

	if flags.F.GKEClusterType == ClusterTypeRegional {
		context.RegionalCluster = true
	}

	context.UseEndpointSlices = config.EndpointSlicesEnabled
	// Do not trigger periodic resync on Endpoints or EndpointSlices object.
	// This aims improve NEG controller performance by avoiding unnecessary NEG sync that triggers for each NEG syncer.
	// As periodic resync may temporary starve NEG API ratelimit quota.
	if config.EndpointSlicesEnabled {
		context.EndpointSliceInformer = discoveryinformer.NewEndpointSliceInformer(kubeClient, config.Namespace, 0,
			cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc, endpointslices.EndpointSlicesByServiceIndex: endpointslices.EndpointSlicesByServiceFunc})
	} else {
		context.EndpointInformer = informerv1.NewEndpointsInformer(kubeClient, config.Namespace, 0, utils.NewNamespaceIndexer())
	}

	return context
}

// Init inits the Context, so that we can defers some config until the main thread enter actually get the leader lock.
func (ctx *ControllerContext) Init() {
	klog.V(2).Infof("Controller Context initializing with %+v", ctx.ControllerContextConfig)
	// Initialize controller context internals based on ASMConfigMap
	if ctx.EnableASMConfigMap {
		configMapInformer := informerv1.NewConfigMapInformer(ctx.KubeClient, ctx.Namespace, ctx.ResyncPeriod, utils.NewNamespaceIndexer())
		ctx.ConfigMapInformer = configMapInformer
		ctx.ASMConfigController = cmconfig.NewConfigMapConfigController(ctx.KubeClient, ctx.Recorder(ctx.ASMConfigMapNamespace), ctx.ASMConfigMapNamespace, ctx.ASMConfigMapName)

		cmConfig := ctx.ASMConfigController.GetConfig()
		if cmConfig.EnableASM {
			ctx.initEnableASM()
		} else {
			ctx.ASMConfigController.SetASMReadyFalse()
		}
	}

}

func (ctx *ControllerContext) initEnableASM() {
	const (
		destinationRuleGroup      = "networking.istio.io"
		destinationRuleAPIVersion = "v1alpha3"
		destinationRulePlural     = "destinationrules"
		// This must match the spec fields below, and be in the form: <plural>.<group>
		destinationRuleCRDName = "destinationrules.networking.istio.io"
	)

	apiextensionClient, err := apiextensionsclientset.NewForConfig(ctx.KubeConfig)
	if err != nil {
		msg := fmt.Sprintf("Failed to create ApiextensionClient for DestinationRule, disabling ASM Mode, error: %s", err)
		ctx.ASMConfigController.RecordEvent("Warning", "FailedValidateDestinationRuleCRD", msg)
		ctx.ASMConfigController.DisableASM()
		return
	}
	destinationRuleCRD, err := apiextensionClient.ApiextensionsV1().CustomResourceDefinitions().Get(context2.TODO(), destinationRuleCRDName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			ctx.ASMConfigController.RecordEvent("Warning", "FailedValidateDestinationRuleCRD", "Cannot find DestinationRule CRD, disabling ASM Mode, please check Istio setup.")
		} else {
			ctx.ASMConfigController.RecordEvent("Warning", "FailedValidateDestinationRuleCRD", fmt.Sprintf("Failed to load DestinationRule CRD, disabling the ASM Mode, please check Istio setup. Error: %s", err))
		}
		ctx.ASMConfigController.DisableASM()
		return
	}
	if destinationRuleCRD.Spec.Versions[0].Name != destinationRuleAPIVersion {
		ctx.ASMConfigController.RecordEvent("Warning", "FailedValidateDestinationRuleCRD", fmt.Sprintf("Only Support Istio API: %s, but found %s, disabling the ASM Mode, please check Istio setup.",
			destinationRuleAPIVersion, destinationRuleCRD.Spec.Versions[0].Name))
		ctx.ASMConfigController.DisableASM()
		return
	}

	dynamicClient, err := dynamic.NewForConfig(ctx.KubeConfig)
	if err != nil {
		msg := fmt.Sprintf("Failed to create kubernetes dynamic client, disabling ASM Mode, please retry. Error: %v", err)
		klog.Error(msg)
		ctx.ASMConfigController.RecordEvent("Warning", "FailedCreateDynamicClient", msg)
		ctx.ASMConfigController.DisableASM()
		return
	}

	klog.V(2).Infof("The supported DestinationRule group version is %s in group %s. Need to update as istio API graduates.", destinationRuleAPIVersion, destinationRuleGroup)
	destrinationGVR := schema.GroupVersionResource{Group: destinationRuleGroup, Version: destinationRuleAPIVersion, Resource: destinationRulePlural}
	drDynamicInformer := dynamicinformer.NewFilteredDynamicInformer(dynamicClient, destrinationGVR, ctx.Namespace, ctx.ResyncPeriod,
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
		nil)
	ctx.DestinationRuleInformer = drDynamicInformer.Informer()
	ctx.DestinationRuleClient = dynamicClient.Resource(destrinationGVR)
	ctx.ASMConfigController.RecordEvent("Normal", "ASMModeOn", fmt.Sprintf("NEG controller is running in ASM Mode with Istio API: %s.", destinationRuleAPIVersion))
	ctx.ASMConfigController.SetASMReadyTrue()
}

// HasSynced returns true if all relevant informers has been synced.
func (ctx *ControllerContext) HasSynced() bool {
	funcs := []func() bool{
		ctx.IngressInformer.HasSynced,
		ctx.ServiceInformer.HasSynced,
		ctx.BackendConfigInformer.HasSynced,
		ctx.PodInformer.HasSynced,
		ctx.NodeInformer.HasSynced,
		ctx.SvcNegInformer.HasSynced,
	}

	if ctx.EndpointInformer != nil {
		funcs = append(funcs, ctx.EndpointInformer.HasSynced)
	}

	if ctx.EndpointSliceInformer != nil {
		funcs = append(funcs, ctx.EndpointSliceInformer.HasSynced)
	}

	if ctx.FrontendConfigInformer != nil {
		funcs = append(funcs, ctx.FrontendConfigInformer.HasSynced)
	}

	if ctx.DestinationRuleInformer != nil {
		funcs = append(funcs, ctx.DestinationRuleInformer.HasSynced)
	}

	if ctx.ConfigMapInformer != nil {
		funcs = append(funcs, ctx.ConfigMapInformer.HasSynced)
	}

	if ctx.IngClassInformer != nil {
		funcs = append(funcs, ctx.IngClassInformer.HasSynced)
	}

	if ctx.IngParamsInformer != nil {
		funcs = append(funcs, ctx.IngParamsInformer.HasSynced)
	}

	if ctx.SAInformer != nil {
		funcs = append(funcs, ctx.SAInformer.HasSynced)
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
	rec := broadcaster.NewRecorder(ctx.generateScheme(), apiv1.EventSource{Component: "loadbalancer-controller"})
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
	if ctx.EndpointSliceInformer != nil {
		go ctx.EndpointSliceInformer.Run(stopCh)
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
	if ctx.EnableASMConfigMap && ctx.ConfigMapInformer != nil {
		go ctx.ConfigMapInformer.Run(stopCh)
	}
	if ctx.SvcNegInformer != nil {
		go ctx.SvcNegInformer.Run(stopCh)
	}
	if ctx.IngClassInformer != nil {
		go ctx.IngClassInformer.Run(stopCh)
	}
	if ctx.IngParamsInformer != nil {
		go ctx.IngParamsInformer.Run(stopCh)
	}
	if ctx.SAInformer != nil {
		go ctx.SAInformer.Run(stopCh)
	}
	// Export ingress usage metrics.
	go ctx.ControllerMetrics.Run(stopCh)
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

// generateScheme creates a scheme and adds relevant CRD schemes that will be used
// for events
func (ctx *ControllerContext) generateScheme() *runtime.Scheme {
	controllerScheme := scheme.Scheme

	if ctx.SAInformer != nil {
		if err := sav1beta1.AddToScheme(controllerScheme); err != nil {
			klog.Errorf("Failed to add ServiceAttachment CRD scheme to event recorder")
		}
	}
	return controllerScheme
}
