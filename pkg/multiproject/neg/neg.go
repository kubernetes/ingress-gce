package neg

import (
	"fmt"

	networkclient "github.com/GoogleCloudPlatform/gke-networking-api/client/network/clientset/versioned"
	informernetwork "github.com/GoogleCloudPlatform/gke-networking-api/client/network/informers/externalversions/network/v1"
	nodetopologyclient "github.com/GoogleCloudPlatform/gke-networking-api/client/nodetopology/clientset/versioned"
	informernodetopology "github.com/GoogleCloudPlatform/gke-networking-api/client/nodetopology/informers/externalversions/nodetopology/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/cloud-provider-gcp/providers/gce"
	providerconfig "k8s.io/ingress-gce/pkg/apis/providerconfig/v1"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/multiproject/filteredinformer"
	multiprojectgce "k8s.io/ingress-gce/pkg/multiproject/gce"
	"k8s.io/ingress-gce/pkg/neg"
	"k8s.io/ingress-gce/pkg/neg/syncers/labels"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/ingress-gce/pkg/network"
	svcnegclient "k8s.io/ingress-gce/pkg/svcneg/client/clientset/versioned"
	informersvcneg "k8s.io/ingress-gce/pkg/svcneg/client/informers/externalversions/svcneg/v1beta1"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/ingress-gce/pkg/utils/zonegetter"
	"k8s.io/klog/v2"
)

func StartNEGController(
	informersFactory informers.SharedInformerFactory,
	kubeClient kubernetes.Interface,
	svcNegClient svcnegclient.Interface,
	eventRecorderClient kubernetes.Interface,
	networkClient networkclient.Interface,
	nodeTopologyClient nodetopologyclient.Interface,
	kubeSystemUID types.UID,
	clusterNamer *namer.Namer,
	l4Namer *namer.L4Namer,
	lpConfig labels.PodLabelPropagationConfig,
	defaultCloudConfig string,
	globalStopCh <-chan struct{},
	logger klog.Logger,
	providerConfig *providerconfig.ProviderConfig,
) (chan<- struct{}, error) {
	cloud, err := multiprojectgce.NewGCEForProviderConfig(defaultCloudConfig, providerConfig, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCE client for provider config %+v: %v", providerConfig, err)
	}

	providerConfigName := providerConfig.Name

	// Using informer factory, create required namespaced informers for the NEG controller.
	ingressInformer := filteredinformer.NewProviderConfigFilteredInformer(informersFactory.Networking().V1().Ingresses().Informer(), providerConfigName)
	serviceInformer := filteredinformer.NewProviderConfigFilteredInformer(informersFactory.Core().V1().Services().Informer(), providerConfigName)
	podInformer := filteredinformer.NewProviderConfigFilteredInformer(informersFactory.Core().V1().Pods().Informer(), providerConfigName)
	nodeInformer := filteredinformer.NewProviderConfigFilteredInformer(informersFactory.Core().V1().Nodes().Informer(), providerConfigName)
	endpointSliceInformer := filteredinformer.NewProviderConfigFilteredInformer(informersFactory.Discovery().V1().EndpointSlices().Informer(), providerConfigName)

	var providerConfigFilteredSvcNegInformer cache.SharedIndexInformer
	if svcNegClient != nil {
		svcNegInformer := informersvcneg.NewServiceNetworkEndpointGroupInformer(svcNegClient, flags.F.WatchNamespace, flags.F.ResyncPeriod, utils.NewNamespaceIndexer())
		providerConfigFilteredSvcNegInformer = filteredinformer.NewProviderConfigFilteredInformer(svcNegInformer, providerConfigName)
	}

	var providerConfigFilteredNetworkInformer cache.SharedIndexInformer
	var providerConfigFilteredGkeNetworkParamsInformer cache.SharedIndexInformer
	if networkClient != nil {
		networkInformer := informernetwork.NewNetworkInformer(networkClient, flags.F.ResyncPeriod, utils.NewNamespaceIndexer())
		providerConfigFilteredNetworkInformer = filteredinformer.NewProviderConfigFilteredInformer(networkInformer, providerConfigName)

		gkeNetworkParamsInformer := informernetwork.NewGKENetworkParamSetInformer(networkClient, flags.F.ResyncPeriod, utils.NewNamespaceIndexer())
		providerConfigFilteredGkeNetworkParamsInformer = filteredinformer.NewProviderConfigFilteredInformer(gkeNetworkParamsInformer, providerConfigName)
	}

	var providerConfigFilteredNodeTopologyInformer cache.SharedIndexInformer
	if nodeTopologyClient != nil {
		nodeTopologyInformer := informernodetopology.NewNodeTopologyInformer(nodeTopologyClient, flags.F.ResyncPeriod, utils.NewNamespaceIndexer())
		providerConfigFilteredNodeTopologyInformer = filteredinformer.NewProviderConfigFilteredInformer(nodeTopologyInformer, providerConfigName)
	}

	// Create a function to check if all the informers have synced.
	hasSynced := func() bool {
		synced := ingressInformer.HasSynced() &&
			serviceInformer.HasSynced() &&
			podInformer.HasSynced() &&
			nodeInformer.HasSynced() &&
			endpointSliceInformer.HasSynced()

		if providerConfigFilteredSvcNegInformer != nil {
			synced = synced && providerConfigFilteredSvcNegInformer.HasSynced()
		}
		if providerConfigFilteredNetworkInformer != nil {
			synced = synced && providerConfigFilteredNetworkInformer.HasSynced()
		}
		if providerConfigFilteredGkeNetworkParamsInformer != nil {
			synced = synced && providerConfigFilteredGkeNetworkParamsInformer.HasSynced()
		}
		if providerConfigFilteredNodeTopologyInformer != nil {
			synced = synced && providerConfigFilteredNodeTopologyInformer.HasSynced()
		}
		return synced
	}

	zoneGetter := zonegetter.NewZoneGetter(nodeInformer, providerConfigFilteredNodeTopologyInformer, cloud.SubnetworkURL())

	// Create a channel to stop the controller for this specific provider config.
	providerConfigStopCh := make(chan struct{})

	// joinedStopCh is a channel that will be closed when the global stop channel or the provider config stop channel is closed.
	joinedStopCh := make(chan struct{})
	go func() {
		defer func() {
			close(joinedStopCh)
			logger.V(2).Info("NEG controller stop channel closed")
		}()
		select {
		case <-globalStopCh:
			logger.V(2).Info("Global stop channel triggered NEG controller shutdown")
		case <-providerConfigStopCh:
			logger.V(2).Info("Provider config stop channel triggered NEG controller shutdown")
		}
	}()

	negController := createNEGController(
		kubeClient,
		svcNegClient,
		eventRecorderClient,
		kubeSystemUID,
		ingressInformer,
		serviceInformer,
		podInformer,
		nodeInformer,
		endpointSliceInformer,
		providerConfigFilteredSvcNegInformer,
		providerConfigFilteredNetworkInformer,
		providerConfigFilteredGkeNetworkParamsInformer,
		hasSynced,
		cloud,
		zoneGetter,
		clusterNamer,
		l4Namer,
		lpConfig,
		joinedStopCh,
		logger,
	)

	go negController.Run()

	return providerConfigStopCh, nil
}

func createNEGController(
	kubeClient kubernetes.Interface,
	svcNegClient svcnegclient.Interface,
	eventRecorderClient kubernetes.Interface,
	kubeSystemUID types.UID,
	ingressInformer cache.SharedIndexInformer,
	serviceInformer cache.SharedIndexInformer,
	podInformer cache.SharedIndexInformer,
	nodeInformer cache.SharedIndexInformer,
	endpointSliceInformer cache.SharedIndexInformer,
	svcNegInformer cache.SharedIndexInformer,
	networkInformer cache.SharedIndexInformer,
	gkeNetworkParamsInformer cache.SharedIndexInformer,
	hasSynced func() bool,
	cloud *gce.Cloud,
	zoneGetter *zonegetter.ZoneGetter,
	clusterNamer *namer.Namer,
	l4Namer *namer.L4Namer,
	lpConfig labels.PodLabelPropagationConfig,
	stopCh <-chan struct{},
	logger klog.Logger,
) *neg.Controller {

	// The following adapter will use Network Selflink as Network Url instead of the NetworkUrl itself.
	// Network Selflink is always composed by the network name even if the cluster was initialized with Network Id.
	// All the components created from it will be consistent and always use the Url with network name and not the url with netowork Id
	adapter, err := network.NewAdapterNetworkSelfLink(cloud)
	if err != nil {
		logger.Error(err, "Failed to create network adapter with SelfLink, falling back to standard cloud network provider")
		adapter = cloud
	}

	noDefaultBackendServicePort := utils.ServicePort{} // we don't need default backend service port for standalone NEGs.

	var noNodeTopologyInformer cache.SharedIndexInformer = nil

	asmServiceNEGSkipNamespaces := []string{}
	enableASM := false

	negController := neg.NewController(
		kubeClient,
		svcNegClient,
		eventRecorderClient,
		kubeSystemUID,
		ingressInformer,
		serviceInformer,
		podInformer,
		nodeInformer,
		endpointSliceInformer,
		svcNegInformer,
		networkInformer,
		gkeNetworkParamsInformer,
		noNodeTopologyInformer,
		hasSynced,
		l4Namer,
		noDefaultBackendServicePort,
		negtypes.NewAdapterWithRateLimitSpecs(cloud, flags.F.GCERateLimit.Values(), adapter),
		zoneGetter,
		clusterNamer,
		flags.F.ResyncPeriod,
		flags.F.NegGCPeriod,
		flags.F.NumNegGCWorkers,
		flags.F.EnableReadinessReflector,
		flags.F.EnableL4NEG,
		flags.F.EnableNonGCPMode,
		flags.F.EnableDualStackNEG,
		enableASM,
		asmServiceNEGSkipNamespaces,
		lpConfig,
		flags.F.EnableMultiNetworking,
		flags.F.EnableIngressRegionalExternal,
		flags.F.EnableL4NetLBNEG,
		stopCh,
		logger,
	)

	return negController
}
