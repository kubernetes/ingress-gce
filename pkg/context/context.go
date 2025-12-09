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
	"time"

	firewallclient "github.com/GoogleCloudPlatform/gke-networking-api/client/gcpfirewall/clientset/versioned"
	informerfirewall "github.com/GoogleCloudPlatform/gke-networking-api/client/gcpfirewall/informers/externalversions/gcpfirewall/v1"
	networkclient "github.com/GoogleCloudPlatform/gke-networking-api/client/network/clientset/versioned"
	informernetwork "github.com/GoogleCloudPlatform/gke-networking-api/client/network/informers/externalversions/network/v1"
	nodetopologyclient "github.com/GoogleCloudPlatform/gke-networking-api/client/nodetopology/clientset/versioned"
	informernodetopology "github.com/GoogleCloudPlatform/gke-networking-api/client/nodetopology/informers/externalversions/nodetopology/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	informerv1 "k8s.io/client-go/informers/core/v1"
	discoveryinformer "k8s.io/client-go/informers/discovery/v1"
	informernetworking "k8s.io/client-go/informers/networking/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/cloud-provider-gcp/providers/gce"
	backendconfigclient "k8s.io/ingress-gce/pkg/backendconfig/client/clientset/versioned"
	informerbackendconfig "k8s.io/ingress-gce/pkg/backendconfig/client/informers/externalversions/backendconfig/v1"
	"k8s.io/ingress-gce/pkg/common/typed"
	"k8s.io/ingress-gce/pkg/controller/translator"
	"k8s.io/ingress-gce/pkg/flags"
	frontendconfigclient "k8s.io/ingress-gce/pkg/frontendconfig/client/clientset/versioned"
	informerfrontendconfig "k8s.io/ingress-gce/pkg/frontendconfig/client/informers/externalversions/frontendconfig/v1beta1"
	"k8s.io/ingress-gce/pkg/instancegroups"
	l4metrics "k8s.io/ingress-gce/pkg/l4lb/metrics"
	"k8s.io/ingress-gce/pkg/metrics"
	"k8s.io/ingress-gce/pkg/recorders"
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
	KubeClient          kubernetes.Interface
	SvcNegClient        svcnegclient.Interface
	SAClient            serviceattachmentclient.Interface
	FirewallClient      firewallclient.Interface
	EventRecorderClient kubernetes.Interface
	NodeTopologyClient  nodetopologyclient.Interface

	Cloud *gce.Cloud

	ClusterNamer  *namer.Namer
	KubeSystemUID types.UID
	L4Namer       namer.L4ResourcesNamer

	ControllerContextConfig

	IngressInformer          cache.SharedIndexInformer
	ServiceInformer          cache.SharedIndexInformer
	BackendConfigInformer    cache.SharedIndexInformer
	FrontendConfigInformer   cache.SharedIndexInformer
	PodInformer              cache.SharedIndexInformer
	NodeInformer             cache.SharedIndexInformer
	EndpointSliceInformer    cache.SharedIndexInformer
	ConfigMapInformer        cache.SharedIndexInformer
	SvcNegInformer           cache.SharedIndexInformer
	SAInformer               cache.SharedIndexInformer
	FirewallInformer         cache.SharedIndexInformer
	NetworkInformer          cache.SharedIndexInformer
	GKENetworkParamsInformer cache.SharedIndexInformer
	NodeTopologyInformer     cache.SharedIndexInformer

	ControllerMetrics *metrics.ControllerMetrics
	L4Metrics         *l4metrics.Collector

	// NOTE: If the flag GKEClusterType is empty, then cluster will default to zonal. This field should not be used for
	// controller logic and should only be used for providing additional information to the user.
	RegionalCluster bool

	InstancePool instancegroups.Manager
	Translator   *translator.Translator
	ZoneGetter   *zonegetter.ZoneGetter

	recordersManager *recorders.Manager

	logger klog.Logger
}

// ControllerContextConfig encapsulates some settings that are tunable via command line flags.
type ControllerContextConfig struct {
	Namespace         string
	ResyncPeriod      time.Duration
	NumL4Workers      int
	NumL4NetLBWorkers int
	ReadOnlyMode      bool
	// DefaultBackendSvcPortID is the ServicePort for the system default backend.
	DefaultBackendSvcPort                     utils.ServicePort
	HealthCheckPath                           string
	MaxIGSize                                 int
	RunL4ILBController                        bool
	RunL4NetLBController                      bool
	EnableL4ILBDualStack                      bool
	EnableL4NetLBDualStack                    bool
	EnableL4StrongSessionAffinity             bool
	EnableMultinetworking                     bool
	EnableIngressRegionalExternal             bool
	EnableWeightedL4ILB                       bool
	EnableWeightedL4NetLB                     bool
	EnableL4ILBZonalAffinity                  bool
	DisableL4LBFirewall                       bool
	EnableL4NetLBNEGs                         bool
	EnableL4NetLBNEGsDefault                  bool
	EnableL4ILBMixedProtocol                  bool
	EnableL4NetLBMixedProtocol                bool
	EnableL4NetLBForwardingRulesOptimizations bool
	EnableL4LBConditions                      bool
}

// NewControllerContext returns a new shared set of informers.
func NewControllerContext(
	kubeClient kubernetes.Interface,
	backendConfigClient backendconfigclient.Interface,
	frontendConfigClient frontendconfigclient.Interface,
	firewallClient firewallclient.Interface,
	svcnegClient svcnegclient.Interface,
	saClient serviceattachmentclient.Interface,
	networkClient networkclient.Interface,
	nodeTopologyClient nodetopologyclient.Interface,
	eventRecorderClient kubernetes.Interface,
	cloud *gce.Cloud,
	clusterNamer *namer.Namer,
	kubeSystemUID types.UID,
	config ControllerContextConfig,
	logger klog.Logger,
) (*ControllerContext, error) {
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
		KubeClient:              kubeClient,
		FirewallClient:          firewallClient,
		SvcNegClient:            svcnegClient,
		SAClient:                saClient,
		EventRecorderClient:     eventRecorderClient,
		NodeTopologyClient:      nodeTopologyClient,
		Cloud:                   cloud,
		ClusterNamer:            clusterNamer,
		L4Namer:                 namer.NewL4Namer(string(kubeSystemUID), clusterNamer),
		KubeSystemUID:           kubeSystemUID,
		ControllerMetrics:       metrics.NewControllerMetrics(flags.F.MetricsExportInterval, logger),
		ControllerContextConfig: config,
		L4Metrics:               l4metrics.NewCollector(flags.F.MetricsExportInterval, flags.F.L4NetLBProvisionDeadline, flags.F.EnableL4NetLBDualStack, flags.F.EnableL4ILBDualStack, logger),
		IngressInformer:         informernetworking.NewIngressInformer(kubeClient, config.Namespace, config.ResyncPeriod, utils.NewNamespaceIndexer()),
		ServiceInformer:         informerv1.NewServiceInformer(kubeClient, config.Namespace, config.ResyncPeriod, utils.NewNamespaceIndexer()),
		PodInformer:             podInformer,
		NodeInformer:            nodeInformer,
		SvcNegInformer:          informersvcneg.NewServiceNetworkEndpointGroupInformer(svcnegClient, config.Namespace, config.ResyncPeriod, utils.NewNamespaceIndexer()),
		recordersManager:        recorders.NewManager(eventRecorderClient, logger),
		logger:                  logger,
	}

	if backendConfigClient != nil {
		context.BackendConfigInformer = informerbackendconfig.NewBackendConfigInformer(backendConfigClient, config.Namespace, config.ResyncPeriod, utils.NewNamespaceIndexer())
	}

	if frontendConfigClient != nil {
		context.FrontendConfigInformer = informerfrontendconfig.NewFrontendConfigInformer(frontendConfigClient, config.Namespace, config.ResyncPeriod, utils.NewNamespaceIndexer())
	}

	if firewallClient != nil {
		context.FirewallInformer = informerfirewall.NewGCPFirewallInformer(firewallClient, config.ResyncPeriod, utils.NewNamespaceIndexer())
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

	if flags.F.EnableMultiSubnetClusterPhase1 {
		if nodeTopologyClient != nil {
			context.NodeTopologyInformer = informernodetopology.NewFilteredNodeTopologyInformer(nodeTopologyClient, config.ResyncPeriod, utils.NewNamespaceIndexer(), func(listOptions *metav1.ListOptions) {
				listOptions.FieldSelector = fmt.Sprintf("metadata.name=%s", flags.F.NodeTopologyCRName)
			})
		}
	}

	if flags.F.ManageL4LBLogging {
		context.ConfigMapInformer = informerv1.NewConfigMapInformer(kubeClient, config.Namespace, config.ResyncPeriod, utils.NewNamespaceIndexer())
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
	subnet := context.Cloud.SubnetworkURL()
	if subnet != "" {
		logger.Info("Detected a non-empty subnet, using regular zoneGetter", "subnetURL", subnet)

		var err error
		context.ZoneGetter, err = zonegetter.NewZoneGetter(context.NodeInformer, context.NodeTopologyInformer, context.Cloud.SubnetworkURL())
		if err != nil {
			return context, fmt.Errorf("Failed to create zone getter: %w", err)
		}

	} else {
		logger.Info("Detected a Legacy Network Cluster, using legacy zoneGetter", "subnetURL", subnet)
		context.ZoneGetter = zonegetter.NewLegacyZoneGetter(context.NodeInformer, context.NodeTopologyInformer)
	}

	context.InstancePool = instancegroups.NewManager(&instancegroups.ManagerConfig{
		Cloud:        context.Cloud,
		Namer:        context.ClusterNamer,
		Recorders:    context,
		BasePath:     utils.GetBasePath(context.Cloud),
		ZoneGetter:   context.ZoneGetter,
		MaxIGSize:    config.MaxIGSize,
		ReadOnlyMode: config.ReadOnlyMode,
	})

	return context, nil
}

func (ctx *ControllerContext) Recorder(ns string) record.EventRecorder {
	return ctx.recordersManager.Recorder(ns)
}

// HasSynced returns true if all relevant informers has been synced.
func (ctx *ControllerContext) HasSynced() bool {
	funcs := []func() bool{
		ctx.IngressInformer.HasSynced,
		ctx.ServiceInformer.HasSynced,
		ctx.PodInformer.HasSynced,
		ctx.NodeInformer.HasSynced,
		ctx.SvcNegInformer.HasSynced,
		ctx.EndpointSliceInformer.HasSynced,
	}

	if ctx.BackendConfigInformer != nil {
		funcs = append(funcs, ctx.BackendConfigInformer.HasSynced)
	}

	if ctx.FrontendConfigInformer != nil {
		funcs = append(funcs, ctx.FrontendConfigInformer.HasSynced)
	}

	if ctx.ConfigMapInformer != nil {
		funcs = append(funcs, ctx.ConfigMapInformer.HasSynced)
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

// Start all of the informers.
func (ctx *ControllerContext) Start(stopCh <-chan struct{}) {
	go ctx.IngressInformer.Run(stopCh)
	go ctx.ServiceInformer.Run(stopCh)
	go ctx.PodInformer.Run(stopCh)
	go ctx.NodeInformer.Run(stopCh)
	go ctx.EndpointSliceInformer.Run(stopCh)

	if ctx.ConfigMapInformer != nil {
		go ctx.ConfigMapInformer.Run(stopCh)
	}

	if ctx.FirewallInformer != nil {
		go ctx.FirewallInformer.Run(stopCh)
	}
	if ctx.BackendConfigInformer != nil {
		go ctx.BackendConfigInformer.Run(stopCh)
	}
	if ctx.FrontendConfigInformer != nil {
		go ctx.FrontendConfigInformer.Run(stopCh)
	}
	if ctx.SvcNegInformer != nil {
		go ctx.SvcNegInformer.Run(stopCh)
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
	if ctx.NodeTopologyInformer != nil {
		go ctx.NodeTopologyInformer.Run(stopCh)
	}
	// Export ingress usage metrics.
	go ctx.ControllerMetrics.Run(stopCh)

	if runL4 := ctx.RunL4ILBController || ctx.RunL4NetLBController; runL4 {
		// Export L4LB usage metrics
		go ctx.L4Metrics.Run(stopCh)
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
