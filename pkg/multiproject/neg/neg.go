package neg

import (
	"fmt"

	networkclient "github.com/GoogleCloudPlatform/gke-networking-api/client/network/clientset/versioned"
	nodetopologyclient "github.com/GoogleCloudPlatform/gke-networking-api/client/nodetopology/clientset/versioned"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/cloud-provider-gcp/providers/gce"
	providerconfig "k8s.io/ingress-gce/pkg/apis/providerconfig/v1"
	"k8s.io/ingress-gce/pkg/flags"
	multiprojectinformers "k8s.io/ingress-gce/pkg/multiproject/informerset"
	"k8s.io/ingress-gce/pkg/neg"
	"k8s.io/ingress-gce/pkg/neg/syncers/labels"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/ingress-gce/pkg/network"
	svcnegclient "k8s.io/ingress-gce/pkg/svcneg/client/clientset/versioned"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/ingress-gce/pkg/utils/zonegetter"
	"k8s.io/klog/v2"
)

// newNEGController is a package-level indirection to allow tests to stub the
// actual NEG controller constructor. In production it points to neg.NewController.
var newNEGController = neg.NewController

// StartNEGController creates and runs a NEG controller for the specified ProviderConfig.
// The returned channel is closed by StopControllersForProviderConfig to signal a shutdown
// specific to this ProviderConfig's controller.
//
// Channel lifecycle management:
//   - globalStopCh: Closes on process/leader shutdown. Shared resources (base informers) use this
//     to survive individual ProviderConfig deletions.
//   - providerConfigStopCh: The returned channel that closes when this specific ProviderConfig
//     is deleted. Used to stop PC-specific controllers.
//   - joinedStopCh: Internal channel that closes when EITHER globalStopCh OR providerConfigStopCh
//     closes. Used for PC-specific resources that should stop in either case.
//
// IMPORTANT: Base informers are already running with globalStopCh. We wrap them with
// ProviderConfig filters that use globalStopCh to remain alive across PC changes.
func StartNEGController(
	informers *multiprojectinformers.InformerSet,
	kubeClient kubernetes.Interface,
	eventRecorderClient kubernetes.Interface,
	svcNegClient svcnegclient.Interface,
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

	// Wrap informers with provider config filter
	filteredInformers := informers.FilterByProviderConfig(providerConfigName)
	hasSynced := filteredInformers.CombinedHasSynced()

	zoneGetter, err := zonegetter.NewZoneGetter(filteredInformers.Node, filteredInformers.NodeTopology, cloud.SubnetworkURL())
	if err != nil {
		logger.Error(err, "failed to initialize zone getter")
		return nil, fmt.Errorf("failed to initialize zonegetter: %v", err)
	}

	negController, err := createNEGController(
		kubeClient,
		svcNegClient,
		eventRecorderClient,
		kubeSystemUID,
		filteredInformers.Ingress,
		filteredInformers.Service,
		filteredInformers.Pod,
		filteredInformers.Node,
		filteredInformers.EndpointSlice,
		filteredInformers.SvcNeg,
		filteredInformers.Network,
		filteredInformers.GkeNetworkParams,
		filteredInformers.NodeTopology,
		hasSynced,
		cloud,
		zoneGetter,
		clusterNamer,
		l4Namer,
		lpConfig,
		joinedStopCh,
		logger,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to create NEG controller: %w", err)
	}

	logger.V(2).Info("Starting NEG controller run loop", "providerConfig", providerConfigName)
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
	nodeTopologyInformer cache.SharedIndexInformer,
	hasSynced func() bool,
	cloud *gce.Cloud,
	zoneGetter *zonegetter.ZoneGetter,
	clusterNamer *namer.Namer,
	l4Namer *namer.L4Namer,
	lpConfig labels.PodLabelPropagationConfig,
	stopCh <-chan struct{},
	logger klog.Logger,
) (*neg.Controller, error) {

	// The adapter uses Network SelfLink
	adapter, err := network.NewAdapterNetworkSelfLink(cloud)
	if err != nil {
		logger.Error(err, "Failed to create network adapter with SelfLink, falling back to standard provider")
		adapter = cloud
	}

	noDefaultBackendServicePort := utils.ServicePort{}

	negController, err := newNEGController(
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
		nodeTopologyInformer,
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
		lpConfig,
		flags.F.EnableMultiNetworking,
		flags.F.EnableIngressRegionalExternal,
		flags.F.EnableL4NetLBNEG,
		flags.F.ReadOnlyMode,
		flags.F.EnableNEGsForIngress,
		stopCh,
		logger,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create NEG controller: %w", err)
	}
	return negController, nil
}
