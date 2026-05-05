package informerset

import (
	"fmt"

	networkclient "github.com/GoogleCloudPlatform/gke-networking-api/client/network/clientset/versioned"
	networkinformers "github.com/GoogleCloudPlatform/gke-networking-api/client/network/informers/externalversions"
	nodetopologyclient "github.com/GoogleCloudPlatform/gke-networking-api/client/nodetopology/clientset/versioned"
	nodetopologyinformers "github.com/GoogleCloudPlatform/gke-networking-api/client/nodetopology/informers/externalversions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/ingress-gce/pkg/multiproject/common/filteredinformer"
	svcnegclient "k8s.io/ingress-gce/pkg/svcneg/client/clientset/versioned"
	svcneginformers "k8s.io/ingress-gce/pkg/svcneg/client/informers/externalversions"
	"k8s.io/ingress-gce/pkg/utils/endpointslices"
	"k8s.io/klog/v2"
)

// InformerSet manages all shared informers used by multiproject controllers.
// It provides centralized initialization and lifecycle management.
type InformerSet struct {
	kubeFactory         informers.SharedInformerFactory
	svcNegFactory       svcneginformers.SharedInformerFactory
	networkFactory      networkinformers.SharedInformerFactory
	nodetopologyFactory nodetopologyinformers.SharedInformerFactory

	// Core Kubernetes informers (always present)
	Ingress       cache.SharedIndexInformer
	Service       cache.SharedIndexInformer
	Pod           cache.SharedIndexInformer
	Node          cache.SharedIndexInformer
	EndpointSlice cache.SharedIndexInformer

	// Custom resource informers (may be nil)
	SvcNeg           cache.SharedIndexInformer // ServiceNetworkEndpointGroups CRD
	Network          cache.SharedIndexInformer // GKE Network CRD
	GkeNetworkParams cache.SharedIndexInformer // GKENetworkParamSets CRD
	NodeTopology     cache.SharedIndexInformer // NodeTopology CRD

	// State tracking
	started bool
}

// NewInformerSet creates and initializes a new InformerSet with all required informers.
// Optional CRD informers are created only when corresponding clients are provided;
// those fields remain nil otherwise.
func NewInformerSet(
	kubeClient kubernetes.Interface,
	svcNegClient svcnegclient.Interface,
	networkClient networkclient.Interface,
	nodeTopologyClient nodetopologyclient.Interface,
	resyncPeriod metav1.Duration,
) *InformerSet {
	infSet := &InformerSet{}

	infSet.kubeFactory = informers.NewSharedInformerFactory(kubeClient, resyncPeriod.Duration)
	if svcNegClient != nil {
		infSet.svcNegFactory = svcneginformers.NewSharedInformerFactory(svcNegClient, resyncPeriod.Duration)
	}
	if networkClient != nil {
		infSet.networkFactory = networkinformers.NewSharedInformerFactory(networkClient, resyncPeriod.Duration)
	}
	if nodeTopologyClient != nil {
		infSet.nodetopologyFactory = nodetopologyinformers.NewSharedInformerFactory(nodeTopologyClient, resyncPeriod.Duration)
	}

	// Create core Kubernetes informers from factory
	infSet.Ingress = infSet.kubeFactory.Networking().V1().Ingresses().Informer()
	infSet.Service = infSet.kubeFactory.Core().V1().Services().Informer()
	infSet.Pod = infSet.kubeFactory.Core().V1().Pods().Informer()
	infSet.Node = infSet.kubeFactory.Core().V1().Nodes().Informer()

	// EndpointSlice informer with custom indexers for NEG controller
	endpointSliceInformer := infSet.kubeFactory.Discovery().V1().EndpointSlices().Informer()
	if err := endpointSliceInformer.AddIndexers(cache.Indexers{
		endpointslices.EndpointSlicesByServiceIndex: endpointslices.EndpointSlicesByServiceFunc,
	}); err != nil {
		klog.Fatalf("failed to add indexers to endpointSlice informer: %v", err)
	}
	infSet.EndpointSlice = endpointSliceInformer

	// Create CRD informers if factories are available
	if infSet.svcNegFactory != nil {
		infSet.SvcNeg = infSet.svcNegFactory.Networking().V1beta1().ServiceNetworkEndpointGroups().Informer()
	}

	if infSet.networkFactory != nil {
		infSet.Network = infSet.networkFactory.Networking().V1().Networks().Informer()
		infSet.GkeNetworkParams = infSet.networkFactory.Networking().V1().GKENetworkParamSets().Informer()
	}

	if infSet.nodetopologyFactory != nil {
		infSet.NodeTopology = infSet.nodetopologyFactory.Networking().V1().NodeTopologies().Informer()
	}
	return infSet
}

// Start starts all informers and waits for their caches to sync.
// It is idempotent: repeated calls return nil once informers have started.
// If the provided stop channel is already closed, it returns an error after
// marking the set as started to prevent subsequent re-runs.
func (i *InformerSet) Start(stopCh <-chan struct{}, logger klog.Logger) error {
	if i.started {
		logger.V(3).Info("InformerSet already started; skipping")
		return nil
	}

	// If the stop channel is already closed, report error but mark started so
	// subsequent Start calls are a no-op (idempotent behavior expected by callers).
	select {
	case <-stopCh:
		err := fmt.Errorf("stop channel closed before start")
		logger.Error(err, "Cannot start informers")
		return err
	default:
	}

	// Start factories
	if i.kubeFactory != nil {
		i.kubeFactory.Start(stopCh)
	}
	if i.svcNegFactory != nil {
		i.svcNegFactory.Start(stopCh)
	}
	if i.networkFactory != nil {
		i.networkFactory.Start(stopCh)
	}
	if i.nodetopologyFactory != nil {
		i.nodetopologyFactory.Start(stopCh)
	}

	i.started = true

	// Wait for initial sync
	logger.V(2).Info("Waiting for informer caches to sync")
	if !cache.WaitForCacheSync(stopCh, i.CombinedHasSynced()) {
		err := fmt.Errorf("failed to sync informer caches")
		logger.Error(err, "Failed to sync informer caches")
		return err
	}

	logger.V(2).Info("Informer caches synced successfully")
	return nil
}

// FilterByProviderConfig creates a new InformerSet with all informers wrapped in a ProviderConfig filter.
// This is used for provider-config-specific controllers.
func (i *InformerSet) FilterByProviderConfig(providerConfigName string) *InformerSet {
	filteredInformers := &InformerSet{
		started: i.started,
	}

	// Wrap core informers
	if i.Ingress != nil {
		filteredInformers.Ingress = newProviderConfigFilteredInformer(i.Ingress, providerConfigName)
	}
	if i.Service != nil {
		filteredInformers.Service = newProviderConfigFilteredInformer(i.Service, providerConfigName)
	}
	if i.Pod != nil {
		filteredInformers.Pod = newProviderConfigFilteredInformer(i.Pod, providerConfigName)
	}
	if i.Node != nil {
		filteredInformers.Node = newProviderConfigFilteredInformer(i.Node, providerConfigName)
	}
	if i.EndpointSlice != nil {
		filteredInformers.EndpointSlice = newProviderConfigFilteredInformer(i.EndpointSlice, providerConfigName)
	}

	// Wrap optional informers
	if i.SvcNeg != nil {
		filteredInformers.SvcNeg = newProviderConfigFilteredInformer(i.SvcNeg, providerConfigName)
	}
	if i.Network != nil {
		filteredInformers.Network = newProviderConfigFilteredInformer(i.Network, providerConfigName)
	}
	if i.GkeNetworkParams != nil {
		filteredInformers.GkeNetworkParams = newProviderConfigFilteredInformer(i.GkeNetworkParams, providerConfigName)
	}
	if i.NodeTopology != nil {
		filteredInformers.NodeTopology = newProviderConfigFilteredInformer(i.NodeTopology, providerConfigName)
	}

	return filteredInformers
}

// newProviderConfigFilteredInformer wraps an informer with a provider config filter.
// The filtered informer shares the same underlying cache and indexers.
func newProviderConfigFilteredInformer(informer cache.SharedIndexInformer, providerConfigName string) cache.SharedIndexInformer {
	return filteredinformer.NewProviderConfigFilteredInformer(informer, providerConfigName)
}

// CombinedHasSynced returns a function that checks if all informers have synced.
func (i *InformerSet) CombinedHasSynced() func() bool {
	syncFuncs := i.hasSyncedFuncs()
	return func() bool {
		for _, hasSynced := range syncFuncs {
			if !hasSynced() {
				return false
			}
		}
		return true
	}
}

// hasSyncedFuncs returns a list of HasSynced functions for all non-nil informers.
func (i *InformerSet) hasSyncedFuncs() []func() bool {
	var funcs []func() bool

	// Core informers (always present)
	if i.Ingress != nil {
		funcs = append(funcs, i.Ingress.HasSynced)
	}
	if i.Service != nil {
		funcs = append(funcs, i.Service.HasSynced)
	}
	if i.Pod != nil {
		funcs = append(funcs, i.Pod.HasSynced)
	}
	if i.Node != nil {
		funcs = append(funcs, i.Node.HasSynced)
	}
	if i.EndpointSlice != nil {
		funcs = append(funcs, i.EndpointSlice.HasSynced)
	}

	// Optional informers
	if i.SvcNeg != nil {
		funcs = append(funcs, i.SvcNeg.HasSynced)
	}
	if i.Network != nil {
		funcs = append(funcs, i.Network.HasSynced)
	}
	if i.GkeNetworkParams != nil {
		funcs = append(funcs, i.GkeNetworkParams.HasSynced)
	}
	if i.NodeTopology != nil {
		funcs = append(funcs, i.NodeTopology.HasSynced)
	}

	return funcs
}
