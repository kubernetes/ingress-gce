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
	"fmt"
	"sync"
	"time"

	apiv1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	informers "k8s.io/client-go/informers"
	informerv1 "k8s.io/client-go/informers/core/v1"
	discoveryinformer "k8s.io/client-go/informers/discovery/v1"
	informernetworking "k8s.io/client-go/informers/networking/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	firewallclient "k8s.io/cloud-provider-gcp/crd/client/gcpfirewall/clientset/versioned"
	informerfirewall "k8s.io/cloud-provider-gcp/crd/client/gcpfirewall/informers/externalversions/gcpfirewall/v1"
	networkclient "k8s.io/cloud-provider-gcp/crd/client/network/clientset/versioned"
	informernetwork "k8s.io/cloud-provider-gcp/crd/client/network/informers/externalversions/network/v1"
	"k8s.io/cloud-provider-gcp/providers/gce"
	sav1 "k8s.io/ingress-gce/pkg/apis/serviceattachment/v1"
	sav1beta1 "k8s.io/ingress-gce/pkg/apis/serviceattachment/v1beta1"
	backendconfigclient "k8s.io/ingress-gce/pkg/backendconfig/client/clientset/versioned"
	informerbackendconfig "k8s.io/ingress-gce/pkg/backendconfig/client/informers/externalversions/backendconfig/v1"
	"k8s.io/ingress-gce/pkg/cmconfig"
	"k8s.io/ingress-gce/pkg/common/typed"
	"k8s.io/ingress-gce/pkg/controller/translator"
	"k8s.io/ingress-gce/pkg/flags"
	frontendconfigclient "k8s.io/ingress-gce/pkg/frontendconfig/client/clientset/versioned"
	informerfrontendconfig "k8s.io/ingress-gce/pkg/frontendconfig/client/informers/externalversions/frontendconfig/v1beta1"
	ingparamsclient "k8s.io/ingress-gce/pkg/ingparams/client/clientset/versioned"
	informeringparams "k8s.io/ingress-gce/pkg/ingparams/client/informers/externalversions/ingparams/v1beta1"
	"k8s.io/ingress-gce/pkg/instancegroups"
	"k8s.io/ingress-gce/pkg/metrics"
	serviceattachmentclient "k8s.io/ingress-gce/pkg/serviceattachment/client/clientset/versioned"
	informerserviceattachment "k8s.io/ingress-gce/pkg/serviceattachment/client/informers/externalversions/serviceattachment/v1"
	svcnegclient "k8s.io/ingress-gce/pkg/svcneg/client/clientset/versioned"
	informersvcneg "k8s.io/ingress-gce/pkg/svcneg/client/informers/externalversions/svcneg/v1beta1"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/endpointslices"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/ingress-gce/pkg/utils/zonegetter"
	"k8s.io/klog/v2"
)

const (
	// Frequency to poll on local stores to sync.
	StoreSyncPollPeriod = 5 * time.Second

	ClusterTypeZonal    = "ZONAL"
	ClusterTypeRegional = "REGIONAL"
)

// ControllerContext holds the state needed for the execution of the controller.
type ControllerContext struct {
	KubeConfig     *rest.Config
	KubeClient     kubernetes.Interface
	SvcNegClient   svcnegclient.Interface
	SAClient       serviceattachmentclient.Interface
	FirewallClient firewallclient.Interface

	Cloud *gce.Cloud

	ClusterNamer  *namer.Namer
	KubeSystemUID types.UID
	L4Namer       namer.L4ResourcesNamer

	ControllerContextConfig
	ASMConfigController *cmconfig.ConfigMapConfigController

	IngressInformer          cache.SharedIndexInformer
	ServiceInformer          cache.SharedIndexInformer
	BackendConfigInformer    cache.SharedIndexInformer
	FrontendConfigInformer   cache.SharedIndexInformer
	PodInformer              cache.SharedIndexInformer
	NodeInformer             cache.SharedIndexInformer
	EndpointSliceInformer    cache.SharedIndexInformer
	ConfigMapInformer        cache.SharedIndexInformer
	SvcNegInformer           cache.SharedIndexInformer
	IngClassInformer         cache.SharedIndexInformer
	IngParamsInformer        cache.SharedIndexInformer
	SAInformer               cache.SharedIndexInformer
	FirewallInformer         cache.SharedIndexInformer
	NetworkInformer          cache.SharedIndexInformer
	GKENetworkParamsInformer cache.SharedIndexInformer

	ControllerMetrics *metrics.ControllerMetrics

	hcLock       sync.Mutex
	healthChecks map[string]func() error

	recorderLock sync.Mutex
	// Map of namespace => record.EventRecorder.
	recorders map[string]record.EventRecorder

	// NOTE: If the flag GKEClusterType is empty, then cluster will default to zonal. This field should not be used for
	// controller logic and should only be used for providing additional information to the user.
	RegionalCluster bool

	InstancePool instancegroups.Manager
	Translator   *translator.Translator
	ZoneGetter   *zonegetter.ZoneGetter

	logger klog.Logger
}

// ControllerContextConfig encapsulates some settings that are tunable via command line flags.
type ControllerContextConfig struct {
	Namespace         string
	ResyncPeriod      time.Duration
	NumL4Workers      int
	NumL4NetLBWorkers int
	// DefaultBackendSvcPortID is the ServicePort for the system default backend.
	DefaultBackendSvcPort         utils.ServicePort
	HealthCheckPath               string
	FrontendConfigEnabled         bool
	EnableASMConfigMap            bool
	ASMConfigMapNamespace         string
	ASMConfigMapName              string
	MaxIGSize                     int
	EnableL4ILBDualStack          bool
	EnableL4NetLBDualStack        bool
	EnableL4StrongSessionAffinity bool // flag that enables strong session affinity feature
	EnableMultinetworking         bool
	EnableIngressRegionalExternal bool
	EnableWeightedL4ILB           bool
	EnableWeightedL4NetLB         bool
}

// NewControllerContext returns a new shared set of informers.
func NewControllerContext(
	kubeConfig *rest.Config,
	kubeClient kubernetes.Interface,
	backendConfigClient backendconfigclient.Interface,
	frontendConfigClient frontendconfigclient.Interface,
	firewallClient firewallclient.Interface,
	svcnegClient svcnegclient.Interface,
	ingParamsClient ingparamsclient.Interface,
	saClient serviceattachmentclient.Interface,
	networkClient networkclient.Interface,
	cloud *gce.Cloud,
	clusterNamer *namer.Namer,
	kubeSystemUID types.UID,
	config ControllerContextConfig,
	logger klog.Logger) *ControllerContext {
	logger = logger.WithName("ControllerContext")

	podInformer := informerv1.NewPodInformer(kubeClient, config.Namespace, config.ResyncPeriod, utils.NewNamespaceIndexer())
	nodeInformer := informerv1.NewNodeInformer(kubeClient, config.ResyncPeriod, utils.NewNamespaceIndexer())

	// Error in SetTransform() doesn't affect the correctness but the memory efficiency
	if err := podInformer.SetTransform(preserveNeeded); err != nil {
		logger.Error(err, "unable to SetTransForm")
	}
	if err := nodeInformer.SetTransform(preserveNeeded); err != nil {
		logger.Error(err, "unable to SetTransForm")
	}

	context := &ControllerContext{
		KubeConfig:              kubeConfig,
		KubeClient:              kubeClient,
		FirewallClient:          firewallClient,
		SvcNegClient:            svcnegClient,
		SAClient:                saClient,
		Cloud:                   cloud,
		ClusterNamer:            clusterNamer,
		L4Namer:                 namer.NewL4Namer(string(kubeSystemUID), clusterNamer),
		KubeSystemUID:           kubeSystemUID,
		ControllerMetrics:       metrics.NewControllerMetrics(flags.F.MetricsExportInterval, flags.F.L4NetLBProvisionDeadline, flags.F.EnableL4NetLBDualStack, flags.F.EnableL4ILBDualStack, logger),
		ControllerContextConfig: config,
		IngressInformer:         informernetworking.NewIngressInformer(kubeClient, config.Namespace, config.ResyncPeriod, utils.NewNamespaceIndexer()),
		ServiceInformer:         informerv1.NewServiceInformer(kubeClient, config.Namespace, config.ResyncPeriod, utils.NewNamespaceIndexer()),
		BackendConfigInformer:   informerbackendconfig.NewBackendConfigInformer(backendConfigClient, config.Namespace, config.ResyncPeriod, utils.NewNamespaceIndexer()),
		PodInformer:             podInformer,
		NodeInformer:            nodeInformer,
		SvcNegInformer:          informersvcneg.NewServiceNetworkEndpointGroupInformer(svcnegClient, config.Namespace, config.ResyncPeriod, utils.NewNamespaceIndexer()),
		recorders:               map[string]record.EventRecorder{},
		healthChecks:            make(map[string]func() error),
		logger:                  logger,
	}
	if firewallClient != nil {
		context.FirewallInformer = informerfirewall.NewGCPFirewallInformer(firewallClient, config.ResyncPeriod, utils.NewNamespaceIndexer())
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

	if networkClient != nil {
		context.NetworkInformer = informernetwork.NewNetworkInformer(networkClient, config.ResyncPeriod, utils.NewNamespaceIndexer())
		context.GKENetworkParamsInformer = informernetwork.NewGKENetworkParamSetInformer(networkClient, config.ResyncPeriod, utils.NewNamespaceIndexer())
	}

	if flags.F.GKEClusterType == ClusterTypeRegional {
		context.RegionalCluster = true
	}

	// Do not trigger periodic resync on EndpointSlices object.
	// This aims improve NEG controller performance by avoiding unnecessary NEG sync that triggers for each NEG syncer.
	// As periodic resync may temporary starve NEG API ratelimit quota.
	context.EndpointSliceInformer = discoveryinformer.NewEndpointSliceInformer(kubeClient, config.Namespace, 0,
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc, endpointslices.EndpointSlicesByServiceIndex: endpointslices.EndpointSlicesByServiceFunc})

	context.Translator = translator.NewTranslator(
		context.ServiceInformer,
		context.BackendConfigInformer,
		context.NodeInformer,
		context.PodInformer,
		context.EndpointSliceInformer,
		context.KubeClient,
		context,
		flags.F.EnableTransparentHealthChecks,
		context.EnableIngressRegionalExternal,
		logger,
	)
	// The subnet specified in gce.conf is considered as the default subnet.
	context.ZoneGetter = zonegetter.NewZoneGetter(context.NodeInformer, context.Cloud.SubnetworkURL())
	context.InstancePool = instancegroups.NewManager(&instancegroups.ManagerConfig{
		Cloud:      context.Cloud,
		Namer:      context.ClusterNamer,
		Recorders:  context,
		BasePath:   utils.GetBasePath(context.Cloud),
		ZoneGetter: context.ZoneGetter,
		MaxIGSize:  config.MaxIGSize,
	}, logger)

	return context
}

// Init inits the Context, so that we can defers some config until the main thread enter actually get the leader hcLock.
func (ctx *ControllerContext) Init() {
	ctx.logger.V(2).Info(fmt.Sprintf("Controller Context initializing with %+v", ctx.ControllerContextConfig))
	// Initialize controller context internals based on ASMConfigMap
	if ctx.EnableASMConfigMap {
		ctx.logger.Info("ASMConfigMap is enabled")

		informerFactory := informers.NewSharedInformerFactoryWithOptions(
			ctx.KubeClient,
			ctx.ResyncPeriod,
			informers.WithNamespace(ctx.ASMConfigMapNamespace),
			informers.WithTweakListOptions(func(listOptions *metav1.ListOptions) {
				listOptions.FieldSelector = fmt.Sprintf("metadata.name=%s", ctx.ASMConfigMapName)
			}))
		ctx.ConfigMapInformer = informerFactory.Core().V1().ConfigMaps().Informer()
		ctx.ASMConfigController = cmconfig.NewConfigMapConfigController(ctx.KubeClient, ctx.Recorder(ctx.ASMConfigMapNamespace), ctx.ASMConfigMapNamespace, ctx.ASMConfigMapName, ctx.logger)

		cmConfig := ctx.ASMConfigController.GetConfig()
		if cmConfig.EnableASM {
			ctx.initEnableASM()
		} else {
			ctx.ASMConfigController.SetASMReadyFalse()
		}
	}
}

func (ctx *ControllerContext) initEnableASM() {
	ctx.logger.V(0).Info("ASM mode is enabled from ConfigMap")

	ctx.ASMConfigController.RecordEvent("Normal", "ASMModeOn", "NEG controller is running in ASM Mode")
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
		ctx.EndpointSliceInformer.HasSynced,
	}

	if ctx.FrontendConfigInformer != nil {
		funcs = append(funcs, ctx.FrontendConfigInformer.HasSynced)
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
	if ctx.NetworkInformer != nil {
		funcs = append(funcs, ctx.NetworkInformer.HasSynced)
	}
	if ctx.GKENetworkParamsInformer != nil {
		funcs = append(funcs, ctx.GKENetworkParamsInformer.HasSynced)
	}

	if ctx.FirewallInformer != nil {
		funcs = append(funcs, ctx.FirewallInformer.HasSynced)
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
	ctx.recorderLock.Lock()
	defer ctx.recorderLock.Unlock()
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
	ctx.hcLock.Lock()
	defer ctx.hcLock.Unlock()

	ctx.healthChecks[id] = hc
}

// HealthCheckResults contains a mapping of component -> health check results.
type HealthCheckResults map[string]error

// HealthCheck runs all registered healthcheck functions.
func (ctx *ControllerContext) HealthCheck() HealthCheckResults {
	ctx.hcLock.Lock()
	defer ctx.hcLock.Unlock()

	healthChecks := make(map[string]error)
	for component, f := range ctx.healthChecks {
		healthChecks[component] = f()
	}

	return healthChecks
}

// Start all of the informers.
func (ctx *ControllerContext) Start(stopCh <-chan struct{}) {
	go ctx.IngressInformer.Run(stopCh)
	go ctx.ServiceInformer.Run(stopCh)
	go ctx.PodInformer.Run(stopCh)
	go ctx.NodeInformer.Run(stopCh)
	go ctx.EndpointSliceInformer.Run(stopCh)

	if ctx.FirewallInformer != nil {
		go ctx.FirewallInformer.Run(stopCh)
	}
	if ctx.BackendConfigInformer != nil {
		go ctx.BackendConfigInformer.Run(stopCh)
	}
	if ctx.FrontendConfigInformer != nil {
		go ctx.FrontendConfigInformer.Run(stopCh)
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
	if ctx.NetworkInformer != nil {
		go ctx.NetworkInformer.Run(stopCh)
	}
	if ctx.GKENetworkParamsInformer != nil {
		go ctx.GKENetworkParamsInformer.Run(stopCh)
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
			ctx.logger.Error(err, "Failed to add v1beta1 ServiceAttachment CRD scheme to event recorder")
		}
		if err := sav1.AddToScheme(controllerScheme); err != nil {
			ctx.logger.Error(err, "Failed to add v1 ServiceAttachment CRD scheme to event recorder: %s")
		}
	}
	return controllerScheme
}

// preserveNeeded returns the obj preserving fields needed for memory efficiency.
// Corresponding lint check is in hack/golangci.yaml to avoid using those fields.
func preserveNeeded(obj interface{}) (interface{}, error) {
	if pod, ok := obj.(*v1.Pod); ok {
		pod.Annotations = nil
		pod.OwnerReferences = nil
		pod.Finalizers = nil
		pod.ManagedFields = nil

		pod.Spec.Volumes = nil
		pod.Spec.InitContainers = nil
		pod.Spec.EphemeralContainers = nil
		pod.Spec.ImagePullSecrets = nil
		pod.Spec.HostAliases = nil
		pod.Spec.SchedulingGates = nil
		pod.Spec.ResourceClaims = nil
		pod.Spec.Tolerations = nil
		pod.Spec.Affinity = nil

		pod.Status.InitContainerStatuses = nil
		pod.Status.ContainerStatuses = nil
		pod.Status.EphemeralContainerStatuses = nil

		for i := 0; i < len(pod.Spec.Containers); i++ {
			c := &pod.Spec.Containers[i]
			c.Image = ""
			c.Command = nil
			c.Args = nil
			c.EnvFrom = nil
			c.Env = nil
			c.Resources = v1.ResourceRequirements{}
			c.VolumeMounts = nil
			c.VolumeDevices = nil
			c.SecurityContext = nil
		}
	}
	if node, ok := obj.(*v1.Node); ok {
		node.ManagedFields = nil
		node.Status.Images = nil
	}
	return obj, nil
}
