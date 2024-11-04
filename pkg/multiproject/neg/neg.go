package neg

import (
	"fmt"

	networkparams "github.com/GoogleCloudPlatform/gke-networking-api/apis/network/v1"
	nodetopology "github.com/GoogleCloudPlatform/gke-networking-api/apis/nodetopology/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/cloud-provider-gcp/providers/gce"
	clusterslice "k8s.io/ingress-gce/pkg/apis/clusterslice/v1"
	svcneg "k8s.io/ingress-gce/pkg/apis/svcneg/v1beta1"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/multiproject/filteredinformer"
	multiprojectgce "k8s.io/ingress-gce/pkg/multiproject/gce"
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

var svcNegResource = schema.GroupVersionResource{Group: svcneg.SchemeGroupVersion.Group, Version: svcneg.SchemeGroupVersion.Version, Resource: "servicenetworkendpointgroups"}
var gkeNetworkParamsResource = schema.GroupVersionResource{Group: networkparams.GroupVersion.Group, Version: networkparams.GroupVersion.Version, Resource: "gkenetworkparamsets"}
var nodeTopologyResource = schema.GroupVersionResource{Group: nodetopology.GroupVersion.Group, Version: nodetopology.GroupVersion.Version, Resource: "nodetopologies"}

func StartNEGController(
	informersFactory informers.SharedInformerFactory,
	kubeClient kubernetes.Interface,
	svcNegClient svcnegclient.Interface,
	eventRecorderClient kubernetes.Interface,
	kubeSystemUID types.UID,
	clusterNamer *namer.Namer,
	l4Namer *namer.L4Namer,
	lpConfig labels.PodLabelPropagationConfig,
	defaultCloudConfig string,
	globalStopCh <-chan struct{},
	logger klog.Logger,
	clusterSlice *clusterslice.ClusterSlice,
) (chan struct{}, error) {
	cloud, err := multiprojectgce.NewGCEForClusterSlice(defaultCloudConfig, clusterSlice, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCE client for cluster slice %+v: %v", clusterSlice, err)
	}

	clusterSliceName := clusterSlice.Name

	// Using informer factory, create required namespaced informers for the NEG controller.
	ingressInformer := filteredinformer.NewClusterSliceFilteredInformer(informersFactory.Networking().V1().Ingresses().Informer(), clusterSliceName)
	serviceInformer := filteredinformer.NewClusterSliceFilteredInformer(informersFactory.Core().V1().Services().Informer(), clusterSliceName)
	podInformer := filteredinformer.NewClusterSliceFilteredInformer(informersFactory.Core().V1().Pods().Informer(), clusterSliceName)
	nodeInformer := filteredinformer.NewClusterSliceFilteredInformer(informersFactory.Core().V1().Nodes().Informer(), clusterSliceName)
	endpointSliceInformer := filteredinformer.NewClusterSliceFilteredInformer(informersFactory.Discovery().V1().EndpointSlices().Informer(), clusterSliceName)

	svcNegInformer, err := informersFactory.ForResource(svcNegResource)
	if err != nil {
		return nil, fmt.Errorf("failed to get svcNeg informer: %v", err)
	}
	clusterSliceFilteredSvcNegInformer := filteredinformer.NewClusterSliceFilteredInformer(svcNegInformer.Informer(), clusterSliceName)

	networkInformer, err := informersFactory.ForResource(gkeNetworkParamsResource)
	if err != nil {
		return nil, fmt.Errorf("failed to get network informer: %v", err)
	}
	clusterSliceFilteredNetworkInformer := filteredinformer.NewClusterSliceFilteredInformer(networkInformer.Informer(), clusterSliceName)

	gkeNetworkParamsInformer, err := informersFactory.ForResource(gkeNetworkParamsResource)
	if err != nil {
		return nil, fmt.Errorf("failed to get gkeNetworkParams informer: %v", err)
	}
	clusterSliceFilteredGkeNetworkParamsInformer := filteredinformer.NewClusterSliceFilteredInformer(gkeNetworkParamsInformer.Informer(), clusterSliceName)

	nodeTopologyInformer, err := informersFactory.ForResource(nodeTopologyResource)
	if err != nil {
		return nil, fmt.Errorf("failed to get nodeTopology informer: %v", err)
	}
	clusterSliceFilteredNodeTopologyInformer := filteredinformer.NewClusterSliceFilteredInformer(nodeTopologyInformer.Informer(), clusterSliceName)

	// Create a function to check if all the informers have synced.
	hasSynced := func() bool {
		return ingressInformer.HasSynced() &&
			serviceInformer.HasSynced() &&
			podInformer.HasSynced() &&
			nodeInformer.HasSynced() &&
			endpointSliceInformer.HasSynced() &&
			clusterSliceFilteredSvcNegInformer.HasSynced() &&
			clusterSliceFilteredNetworkInformer.HasSynced() &&
			clusterSliceFilteredGkeNetworkParamsInformer.HasSynced() &&
			clusterSliceFilteredNodeTopologyInformer.HasSynced()
	}

	zoneGetter := zonegetter.NewZoneGetter(nodeInformer, clusterSliceFilteredNodeTopologyInformer, cloud.SubnetworkURL())

	// Create a channel to stop the controller for this specific project.
	projectStopCh := make(chan struct{})
	defer close(projectStopCh)

	// joinedStopCh is a channel that will be closed when the global stop channel or the project stop channel is closed.
	joinedStopCh := make(chan struct{})
	go func() {
		defer close(joinedStopCh)
		select {
		case <-globalStopCh:
		case <-projectStopCh:
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
		clusterSliceFilteredSvcNegInformer,
		clusterSliceFilteredNetworkInformer,
		clusterSliceFilteredGkeNetworkParamsInformer,
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

	return projectStopCh, nil
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
		logger.Error(err, "Failed to create network adapter with SelfLink")
		// if it was not possible to retrieve network information use standard context as cloud network provider
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
