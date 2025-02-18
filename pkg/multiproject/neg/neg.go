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
	"k8s.io/ingress-gce/pkg/neg"
	"k8s.io/ingress-gce/pkg/neg/syncers/labels"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/ingress-gce/pkg/network"
	svcnegclient "k8s.io/ingress-gce/pkg/svcneg/client/clientset/versioned"
	informersvcneg "k8s.io/ingress-gce/pkg/svcneg/client/informers/externalversions/svcneg/v1beta1"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/endpointslices"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/ingress-gce/pkg/utils/zonegetter"
	"k8s.io/klog/v2"
)

// StartNEGController creates and runs a NEG controller for the specified ProviderConfig.
// The returned channel is closed by StopControllersForProviderConfig to signal a shutdown
// specific to this ProviderConfig's controller.
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
	cloud *gce.Cloud,
	globalStopCh <-chan struct{},
	logger klog.Logger,
	providerConfig *providerconfig.ProviderConfig,
) (chan<- struct{}, error) {
	providerConfigName := providerConfig.Name
	logger.V(2).Info("Initializing NEG controller", "providerConfig", providerConfigName)

	// The ProviderConfig-specific stop channel. We close this in StopControllersForProviderConfig.
	providerConfigStopCh := make(chan struct{})

	// joinedStopCh will close when either the globalStopCh or providerConfigStopCh is closed.
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

	informers, hasSynced, err := initializeInformers(informersFactory, svcNegClient, networkClient, nodeTopologyClient, providerConfigName, logger, joinedStopCh)
	if err != nil {
		return nil, err
	}

	zoneGetter := zonegetter.NewZoneGetter(
		informers.nodeInformer,
		informers.providerConfigFilteredNodeTopologyInformer,
		cloud.SubnetworkURL(),
	)

	negController := createNEGController(
		kubeClient,
		svcNegClient,
		eventRecorderClient,
		kubeSystemUID,
		informers.ingressInformer,
		informers.serviceInformer,
		informers.podInformer,
		informers.nodeInformer,
		informers.endpointSliceInformer,
		informers.providerConfigFilteredSvcNegInformer,
		informers.providerConfigFilteredNetworkInformer,
		informers.providerConfigFilteredGkeNetworkParamsInformer,
		hasSynced,
		cloud,
		zoneGetter,
		clusterNamer,
		l4Namer,
		lpConfig,
		joinedStopCh,
		logger,
	)

	logger.V(2).Info("Starting NEG controller run loop", "providerConfig", providerConfigName)
	go negController.Run()
	return providerConfigStopCh, nil
}

type negInformers struct {
	ingressInformer                                cache.SharedIndexInformer
	serviceInformer                                cache.SharedIndexInformer
	podInformer                                    cache.SharedIndexInformer
	nodeInformer                                   cache.SharedIndexInformer
	endpointSliceInformer                          cache.SharedIndexInformer
	providerConfigFilteredSvcNegInformer           cache.SharedIndexInformer
	providerConfigFilteredNetworkInformer          cache.SharedIndexInformer
	providerConfigFilteredGkeNetworkParamsInformer cache.SharedIndexInformer
	providerConfigFilteredNodeTopologyInformer     cache.SharedIndexInformer
}

// initializeInformers wraps the base SharedIndexInformers in a providerConfig filter
// and runs them.
func initializeInformers(
	informersFactory informers.SharedInformerFactory,
	svcNegClient svcnegclient.Interface,
	networkClient networkclient.Interface,
	nodeTopologyClient nodetopologyclient.Interface,
	providerConfigName string,
	logger klog.Logger,
	joinedStopCh <-chan struct{},
) (*negInformers, func() bool, error) {
	ingressInformer := filteredinformer.NewProviderConfigFilteredInformer(informersFactory.Networking().V1().Ingresses().Informer(), providerConfigName)
	serviceInformer := filteredinformer.NewProviderConfigFilteredInformer(informersFactory.Core().V1().Services().Informer(), providerConfigName)
	podInformer := filteredinformer.NewProviderConfigFilteredInformer(informersFactory.Core().V1().Pods().Informer(), providerConfigName)
	nodeInformer := filteredinformer.NewProviderConfigFilteredInformer(informersFactory.Core().V1().Nodes().Informer(), providerConfigName)

	endpointSliceInformer := filteredinformer.NewProviderConfigFilteredInformer(
		informersFactory.Discovery().V1().EndpointSlices().Informer(),
		providerConfigName,
	)
	err := addIndexerIfNotPresent(endpointSliceInformer.GetIndexer(), endpointslices.EndpointSlicesByServiceIndex, endpointslices.EndpointSlicesByServiceFunc)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to add indexers to endpointSliceInformer: %v", err)
	}

	var providerConfigFilteredSvcNegInformer cache.SharedIndexInformer
	if svcNegClient != nil {
		svcNegInformer := informersvcneg.NewServiceNetworkEndpointGroupInformer(svcNegClient, flags.F.WatchNamespace, flags.F.ResyncPeriod, utils.NewNamespaceIndexer())
		svcNegInformer.GetIndexer()
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

	// Start them with the joinedStopCh so they properly stop
	hasSyncedList := []func() bool{
		ingressInformer.HasSynced,
		serviceInformer.HasSynced,
		podInformer.HasSynced,
		nodeInformer.HasSynced,
		endpointSliceInformer.HasSynced,
	}
	go ingressInformer.Run(joinedStopCh)
	go serviceInformer.Run(joinedStopCh)
	go podInformer.Run(joinedStopCh)
	go nodeInformer.Run(joinedStopCh)
	go endpointSliceInformer.Run(joinedStopCh)
	if providerConfigFilteredSvcNegInformer != nil {
		go providerConfigFilteredSvcNegInformer.Run(joinedStopCh)
		hasSyncedList = append(hasSyncedList, providerConfigFilteredSvcNegInformer.HasSynced)
	}
	if providerConfigFilteredNetworkInformer != nil {
		go providerConfigFilteredNetworkInformer.Run(joinedStopCh)
		hasSyncedList = append(hasSyncedList, providerConfigFilteredNetworkInformer.HasSynced)
	}
	if providerConfigFilteredGkeNetworkParamsInformer != nil {
		go providerConfigFilteredGkeNetworkParamsInformer.Run(joinedStopCh)
		hasSyncedList = append(hasSyncedList, providerConfigFilteredGkeNetworkParamsInformer.HasSynced)
	}
	if providerConfigFilteredNodeTopologyInformer != nil {
		go providerConfigFilteredNodeTopologyInformer.Run(joinedStopCh)
		hasSyncedList = append(hasSyncedList, providerConfigFilteredNodeTopologyInformer.HasSynced)
	}

	logger.V(2).Info("NEG informers initialized", "providerConfigName", providerConfigName)
	informers := &negInformers{
		ingressInformer:                                ingressInformer,
		serviceInformer:                                serviceInformer,
		podInformer:                                    podInformer,
		nodeInformer:                                   nodeInformer,
		endpointSliceInformer:                          endpointSliceInformer,
		providerConfigFilteredSvcNegInformer:           providerConfigFilteredSvcNegInformer,
		providerConfigFilteredNetworkInformer:          providerConfigFilteredNetworkInformer,
		providerConfigFilteredGkeNetworkParamsInformer: providerConfigFilteredGkeNetworkParamsInformer,
		providerConfigFilteredNodeTopologyInformer:     providerConfigFilteredNodeTopologyInformer,
	}
	hasSynced := func() bool {
		for _, hasSynced := range hasSyncedList {
			if !hasSynced() {
				return false
			}
		}
		return true
	}

	return informers, hasSynced, nil
}

func addIndexerIfNotPresent(indexer cache.Indexer, indexName string, indexFunc cache.IndexFunc) error {
	indexers := indexer.GetIndexers()
	if _, ok := indexers[indexName]; ok {
		return nil
	}
	return indexer.AddIndexers(cache.Indexers{indexName: indexFunc})
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

	// The adapter uses Network SelfLink
	adapter, err := network.NewAdapterNetworkSelfLink(cloud)
	if err != nil {
		logger.Error(err, "Failed to create network adapter with SelfLink, falling back to standard provider")
		adapter = cloud
	}

	noDefaultBackendServicePort := utils.ServicePort{}
	var noNodeTopologyInformer cache.SharedIndexInformer

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
